package server

// ssrf_api_test.go：出站 URL 校验的**接口级**回归（2026-10-30 加固）。
//
// 锁死三条服务端出站边界：
//  1. 入口 URL 指向内网/环回/链路本地 → 拒绝；
//  2. 入口 URL 是公网地址，但它 302 到内网 → 拒绝（逐跳复查）；
//  3. 入口 URL 正常 → 放行（不能把正常下载搞挂）。
//
// 用注入的传输层做密闭测试：既能脚本化重定向链，又不受本机 DNS 影响
// （本机是 Clash fake-ip 模式，域名一律解析到 198.18.x.x 假地址）。

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/huluohu/raccoon/internal/netguard"
)

// ssrfFakeRT 脚本化传输层。
type ssrfFakeRT struct {
	responses map[string]*http.Response
}

func (f *ssrfFakeRT) RoundTrip(req *http.Request) (*http.Response, error) {
	if resp, ok := f.responses[req.URL.String()]; ok {
		return resp, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     http.Header{},
		Request:    req,
	}, nil
}

func ssrfRedirect(location string) *http.Response {
	h := http.Header{}
	h.Set("Location", location)
	return &http.Response{
		StatusCode: http.StatusFound,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     h,
	}
}

// installSSRFStub 装上脚本化传输层与固定的公网 DNS 解析。
func installSSRFStub(t *testing.T, responses map[string]*http.Response) {
	t.Helper()
	restore := netguard.SetTransportForTest(&ssrfFakeRT{responses: responses})
	t.Cleanup(restore)
	restoreDNS := netguard.SetResolverForTest(func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	})
	t.Cleanup(restoreDNS)
}

// Test任务提交_拒绝内网入口 URL 指向内网/环回/云元数据的，一律拒绝。
func Test任务提交_拒绝内网入口URL(t *testing.T) {
	installSSRFStub(t, map[string]*http.Response{})
	baseURL := newAuthDisabledEnv(t)
	for _, bad := range []string{
		"http://169.254.169.254/latest/meta-data",
		"http://127.0.0.1:8530/admin",
		"http://192.168.1.1/x",
		"http://10.0.0.1/x",
		"file:///etc/passwd",
	} {
		body := `{"items":[{"url":"` + bad + `"}]}`
		code, raw, _ := secDoBody(t, http.MethodPost, baseURL+"/api/v1/tasks", body, secOpts{})
		if code != http.StatusBadRequest {
			t.Errorf("%s 应被拒绝（400），实际 %d（%s）", bad, code, raw)
		}
	}
}

// Test任务提交_拒绝跳向内网的重定向 入口是公网地址（校验通过），
// 但它 302 到云元数据端点——逐跳复查必须拦下。
func Test任务提交_拒绝跳向内网的重定向(t *testing.T) {
	installSSRFStub(t, map[string]*http.Response{
		"http://jump.example/r": ssrfRedirect("http://169.254.169.254/latest/meta-data/"),
	})
	baseURL := newAuthDisabledEnv(t)
	body := `{"items":[{"url":"http://jump.example/r"}]}`
	code, raw, _ := secDoBody(t, http.MethodPost, baseURL+"/api/v1/tasks", body, secOpts{})
	if code != http.StatusBadRequest {
		t.Fatalf("跳向内网的重定向应被拒绝（400），实际 %d（%s）", code, raw)
	}
	if !strings.Contains(raw, "SSRF") {
		t.Errorf("错误信息应点明是 SSRF 防护，实际 %s", raw)
	}
}

// Test任务提交_拒绝多跳后跳向内网 第一跳是公网地址，第二跳才指向内网。
func Test任务提交_拒绝多跳后跳向内网(t *testing.T) {
	installSSRFStub(t, map[string]*http.Response{
		"http://jump.example/h1": ssrfRedirect("http://cdn.example/h2"),
		"http://cdn.example/h2":  ssrfRedirect("http://127.0.0.1:8530/admin"),
	})
	baseURL := newAuthDisabledEnv(t)
	body := `{"items":[{"url":"http://jump.example/h1"}]}`
	code, raw, _ := secDoBody(t, http.MethodPost, baseURL+"/api/v1/tasks", body, secOpts{})
	if code != http.StatusBadRequest {
		t.Fatalf("多跳后指向内网的重定向应被拒绝（400），实际 %d（%s）", code, raw)
	}
}

// Test任务提交_正常公网URL放行 不能把正常下载搞挂（这是最该守住的一条：
// 误伤会让所有用户都下不了东西）。
func Test任务提交_正常公网URL放行(t *testing.T) {
	installSSRFStub(t, map[string]*http.Response{
		"http://site.example/video": {StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}},
	})
	baseURL := newAuthDisabledEnv(t)
	body := `{"items":[{"url":"http://site.example/video"}]}`
	code, raw, _ := secDoBody(t, http.MethodPost, baseURL+"/api/v1/tasks", body, secOpts{})
	if code != http.StatusCreated {
		t.Fatalf("正常公网 URL 应放行（201），实际 %d（%s）", code, raw)
	}
}

// Test任务提交_站内重定向放行 站点常见的路径改写重定向不能被误伤。
func Test任务提交_站内重定向放行(t *testing.T) {
	installSSRFStub(t, map[string]*http.Response{
		"http://site.example/old": ssrfRedirect("/new"),
	})
	baseURL := newAuthDisabledEnv(t)
	body := `{"items":[{"url":"http://site.example/old"}]}`
	code, raw, _ := secDoBody(t, http.MethodPost, baseURL+"/api/v1/tasks", body, secOpts{})
	if code != http.StatusCreated {
		t.Fatalf("站内重定向应放行（201），实际 %d（%s）", code, raw)
	}
}
