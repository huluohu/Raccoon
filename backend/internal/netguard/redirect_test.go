package netguard

// redirect_test.go：重定向链逐跳校验的用例。
//
// 锁死的攻击形态：入口 URL 是公网地址（校验通过），但它 302 到内网地址。
// 逐跳复查必须拦下；正常站点的重定向链不能被误伤。
//
// 用注入的传输层做**密闭**测试：不依赖真实网络，也不受本机回环地址
// 必然被判为内网的影响（真实站点都在公网）。

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

// fakeRT 脚本化的 HTTP 传输层：按 URL 返回预设响应。
type fakeRT struct {
	// responses URL → 响应（状态码 + Location）。
	responses map[string]*http.Response
	// seen 记录被请求过的 URL（断言用）。
	seen []string
}

func (f *fakeRT) RoundTrip(req *http.Request) (*http.Response, error) {
	f.seen = append(f.seen, req.URL.String())
	if resp, ok := f.responses[req.URL.String()]; ok {
		return resp, nil
	}
	// 未知 URL 一律 200（链结束）。
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     http.Header{},
		Request:    req,
	}, nil
}

func redirectResp(status int, location string) *http.Response {
	h := http.Header{}
	if location != "" {
		h.Set("Location", location)
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     h,
	}
}

// useFakeRT 装上脚本化传输层，并让 DNS 解析固定返回公网地址。
func useFakeRT(t *testing.T, responses map[string]*http.Response) *fakeRT {
	t.Helper()
	rt := &fakeRT{responses: responses}
	restore := SetTransportForTest(rt)
	t.Cleanup(restore)
	// 域名一律解析到公网 IP——这样「跳向内网」只能由 Location 直接写 IP 触发，
	// 与 DNS 无关，两类攻击各自独立可测。
	restoreDNS := SetResolverForTest(func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	})
	t.Cleanup(restoreDNS)
	return rt
}

func TestCheckRedirectChain_跳向云元数据被拒(t *testing.T) {
	useFakeRT(t, map[string]*http.Response{
		"http://evil.example/x": redirectResp(http.StatusFound, "http://169.254.169.254/latest/meta-data/iam/"),
	})
	err := CheckRedirectChain(context.Background(), "http://evil.example/x", DefaultMaxRedirects)
	if err == nil {
		t.Fatal("跳向云元数据端点的重定向应被拒绝")
	}
	if !strings.Contains(err.Error(), "169.254.169.254") {
		t.Errorf("错误信息应点明目标地址，实际: %v", err)
	}
}

func TestCheckRedirectChain_跳向回环被拒(t *testing.T) {
	useFakeRT(t, map[string]*http.Response{
		"http://evil.example/x": redirectResp(http.StatusFound, "http://127.0.0.1:8530/admin"),
	})
	if err := CheckRedirectChain(context.Background(), "http://evil.example/x", DefaultMaxRedirects); err == nil {
		t.Fatal("跳向回环地址的重定向应被拒绝")
	}
}

func TestCheckRedirectChain_多跳后跳向内网被拒(t *testing.T) {
	// 第一跳指向另一个公网地址，第二跳才指向内网——只看第一跳会漏。
	useFakeRT(t, map[string]*http.Response{
		"http://evil.example/hop1": redirectResp(http.StatusFound, "http://cdn.example/hop2"),
		"http://cdn.example/hop2":  redirectResp(http.StatusFound, "http://169.254.169.254/latest/"),
	})
	if err := CheckRedirectChain(context.Background(), "http://evil.example/hop1", DefaultMaxRedirects); err == nil {
		t.Fatal("多跳后指向内网的重定向应被拒绝")
	}
}

func TestCheckRedirectChain_域名跳向内网被拒(t *testing.T) {
	// DNS 型：Location 写的是域名，但该域名解析到内网。
	useFakeRT(t, map[string]*http.Response{
		"http://evil.example/x": redirectResp(http.StatusFound, "http://metadata.example/latest/"),
	})
	restoreDNS := SetResolverForTest(func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("169.254.169.254")}, nil
	})
	t.Cleanup(restoreDNS)
	if err := CheckRedirectChain(context.Background(), "http://evil.example/x", DefaultMaxRedirects); err == nil {
		t.Fatal("跳向解析到内网的域名的重定向应被拒绝")
	}
}

func TestCheckRedirectChain_正常重定向放行(t *testing.T) {
	// 站内改写路径的重定向不能被误伤。
	rt := useFakeRT(t, map[string]*http.Response{
		"http://site.example/old": redirectResp(http.StatusFound, "/new"),
	})
	if err := CheckRedirectChain(context.Background(), "http://site.example/old", DefaultMaxRedirects); err != nil {
		t.Fatalf("正常站内重定向应放行: %v", err)
	}
	if len(rt.seen) < 2 {
		t.Errorf("应跟随重定向至少一跳，实际请求 %d 次: %v", len(rt.seen), rt.seen)
	}
}

func TestCheckRedirectChain_首跳非重定向放行(t *testing.T) {
	useFakeRT(t, map[string]*http.Response{
		"http://site.example/x": redirectResp(http.StatusOK, ""),
	})
	if err := CheckRedirectChain(context.Background(), "http://site.example/x", DefaultMaxRedirects); err != nil {
		t.Fatalf("非重定向响应应放行: %v", err)
	}
}

func TestCheckRedirectChain_站点不可达不阻断(t *testing.T) {
	// 请求失败不是攻击特征，不能因此拒绝用户的正常任务。
	err := CheckRedirectChain(context.Background(), "http://unreachable.example/x", DefaultMaxRedirects)
	if err != nil {
		t.Fatalf("站点不可达不应阻断: %v", err)
	}
}
