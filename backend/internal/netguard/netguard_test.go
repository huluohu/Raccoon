package netguard

// netguard_test.go：出站 URL 校验用例集。
//
// 这些用例锁死的是「服务端能替用户/远端站点请求到哪里」的边界。
// 重点覆盖两类真实攻击形态：
//   - DNS 型 SSRF：域名解析到回环/云元数据地址；
//   - 远端可控 URL：引擎从站点响应里拿到的直链（媒体直取旁路）指向内网。

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

// withResolver 临时替换 DNS 解析器（测试用）。
func withResolver(t *testing.T, fn func(ctx context.Context, host string) ([]net.IP, error)) {
	t.Helper()
	orig := resolver
	t.Cleanup(func() { resolver = orig })
	resolver = fn
}

// resolveTo 让指定域名解析到固定 IP。
func resolveTo(ips ...string) func(ctx context.Context, host string) ([]net.IP, error) {
	return func(ctx context.Context, host string) ([]net.IP, error) {
		var out []net.IP
		for _, s := range ips {
			ip := net.ParseIP(s)
			if ip == nil {
				return nil, errors.New("测试用 IP 非法: " + s)
			}
			out = append(out, ip)
		}
		return out, nil
	}
}

// ---- 红线：与策略无关一律拒绝 ----

func TestValidate_红线一律拒绝(t *testing.T) {
	cases := []struct{ name, raw string }{
		{"回环IPv4", "http://127.0.0.1/x"},
		{"回环IPv4另一段", "http://127.8.9.10/admin"},
		{"回环IPv6", "http://[::1]/x"},
		{"localhost域名", "http://localhost:8080/x"},
		{"localhost大写", "http://LOCALHOST/x"},
		{"localhost子域", "http://app.localhost/x"},
		{"云元数据端点", "http://169.254.169.254/latest/meta-data"},
		{"链路本地其它", "http://169.254.1.1/x"},
		{"IPv6链路本地", "http://[fe80::1]/x"},
		{"未指定地址", "http://0.0.0.0/x"},
		{"IPv4映射回环", "http://[::ffff:127.0.0.1]/x"},
		{"file协议", "file:///etc/passwd"},
		{"javascript协议", "javascript:alert(1)"},
		{"gopher协议", "gopher://127.0.0.1:6379/_INFO"},
		{"ftp协议", "ftp://10.0.0.1/x"},
		{"空URL", ""},
		{"无主机名", "http:///path"},
	}
	for _, p := range []Options{{AllowPrivate: false}, {AllowPrivate: true}} {
		for _, c := range cases {
			if err := Validate(c.raw, p); err == nil {
				t.Errorf("%s（AllowPrivate=%v）应被拒绝，实际放行", c.name, p.AllowPrivate)
			}
		}
	}
}

// ---- 私网按策略分档 ----

func TestValidate_私网按策略分档(t *testing.T) {
	private := []struct{ name, raw string }{
		{"RFC1918十段", "http://10.0.0.1/x"},
		{"RFC1918百七十二", "http://172.16.0.1/x"},
		{"RFC1918百九十二", "http://192.168.1.1/x"},
		{"CGNAT", "http://100.64.0.1/x"},
		{"IPv6 ULA", "http://[fc00::1]/x"},
		{"零段", "http://0.1.2.3/x"},
		{"基准测试段", "http://198.18.0.1/x"},
	}
	for _, c := range private {
		if err := Validate(c.raw, Options{AllowPrivate: false}); err == nil {
			t.Errorf("%s 严格模式应被拒绝", c.name)
		}
		// 连接测试模式允许内网（用户自配基础设施）。
		if err := Validate(c.raw, Options{AllowPrivate: true}); err != nil {
			t.Errorf("%s 连接测试模式应放行，实际: %v", c.name, err)
		}
	}
}

// TestValidate_组播两种模式都拒 组播不是「内网」而是红线：连接测试模式
// 也不该放行（没有任何自配基础设施会放在组播地址上）。
func TestValidate_组播两种模式都拒(t *testing.T) {
	for _, p := range []Options{{AllowPrivate: false}, {AllowPrivate: true}} {
		if err := Validate("http://224.0.0.1/x", p); err == nil {
			t.Errorf("组播地址应始终被拒绝（AllowPrivate=%v）", p.AllowPrivate)
		}
	}
}

// ---- DNS 型 SSRF（本轮加固的核心）----

// TestValidate_域名解析到内网被拒 「域名指向内网」是只看字面量 IP 的
// 校验完全拦不住的形态——攻击者把 evil.example 解析到 127.0.0.1 即可。
func TestValidate_域名解析到内网被拒(t *testing.T) {
	cases := []struct{ name, ip string }{
		{"解析到回环", "127.0.0.1"},
		{"解析到云元数据", "169.254.169.254"},
		{"解析到私网", "192.168.1.10"},
		{"解析到CGNAT", "100.64.0.1"},
	}
	for _, c := range cases {
		withResolver(t, resolveTo(c.ip))
		err := Validate("http://evil.example/x", Options{ResolveDNS: true})
		if err == nil {
			t.Errorf("%s 应被拒绝（域名解析到 %s）", c.name, c.ip)
		}
	}
}

// TestValidate_多A记录含内网即拒 攻击者可以配「公网 IP + 127.0.0.1」
// 两条记录，只查第一条会漏——任一条落在内网即拒。
func TestValidate_多A记录含内网即拒(t *testing.T) {
	withResolver(t, resolveTo("93.184.216.34", "127.0.0.1"))
	if err := Validate("http://evil.example/x", Options{ResolveDNS: true}); err == nil {
		t.Fatal("解析结果含内网地址时应被拒绝")
	}
}

// TestValidate_解析到公网放行 正常的公网域名不能被误伤。
func TestValidate_解析到公网放行(t *testing.T) {
	withResolver(t, resolveTo("93.184.216.34"))
	if err := Validate("http://www.example.com/x", Options{ResolveDNS: true}); err != nil {
		t.Fatalf("公网域名应放行: %v", err)
	}
}

// TestValidate_解析失败不拦截 走代理的用户，域名只在代理侧解析，本机解析
// 失败是常态——拦下来是误伤。真正的攻击形态在本机是能解析出来的。
func TestValidate_解析失败不拦截(t *testing.T) {
	withResolver(t, func(ctx context.Context, host string) ([]net.IP, error) {
		return nil, errors.New("no such host")
	})
	if err := Validate("http://via-proxy.example/x", Options{ResolveDNS: true}); err != nil {
		t.Fatalf("解析失败不应拦截（代理用户兼容）: %v", err)
	}
}

// ---- 媒体直链（远端可控 URL）----

// TestValidateMedia_远端给的直链指向内网被拒 媒体直取旁路把远端站点
// 返回的清单地址交给 ffmpeg 抓取——那条地址完全由远端控制。
func TestValidateMedia_远端给的直链指向内网被拒(t *testing.T) {
	withResolver(t, resolveTo("169.254.169.254"))
	err := ValidateMedia("http://metadata.example.com/latest/meta-data/x.m3u8")
	if err == nil {
		t.Fatal("指向云元数据的媒体直链应被拒绝")
	}
	if !strings.Contains(err.Error(), "169.254.169.254") {
		t.Errorf("错误信息应点明解析到的内网地址，实际: %v", err)
	}
}

// TestValidateMedia_正常CDN放行 不能把正常下载搞挂。
func TestValidateMedia_正常CDN放行(t *testing.T) {
	withResolver(t, resolveTo("104.18.32.47"))
	if err := ValidateMedia("https://cdn.example.com/video/seg.m3u8"); err != nil {
		t.Fatalf("正常 CDN 直链应放行: %v", err)
	}
}

// TestValidateMedia_非HTTP协议拒绝 清单地址只允许 http/https。
func TestValidateMedia_非HTTP协议拒绝(t *testing.T) {
	if err := ValidateMedia("file:///etc/passwd"); err == nil {
		t.Fatal("file:// 应被拒绝")
	}
}

// ---- fake-ip 兼容（Clash/mihomo）----

// TestValidate_fakeip解析不被误判 关键回归：Clash/mihomo 的 fake-ip 模式
// 把所有域名解析到 198.18.0.0/16 的假地址。若把该段一律判为「内网」，
// 国内用户（大量使用这种 DNS）的**所有下载都会被拦**——实测本机正是这种
// DNS（httpbin.org → 198.18.1.58）。
//
// 该段不指向任何敏感目标（不可路由到回环/元数据/内网服务），放行不带来
// SSRF 收益；但作为 URL 字面量时仍然拒绝（那显然不是正常下载目标）。
func TestValidate_fakeip解析不被误判(t *testing.T) {
	withResolver(t, resolveTo("198.18.1.58"))
	if err := Validate("http://www.example.com/x", Options{ResolveDNS: true}); err != nil {
		t.Fatalf("fake-ip 段解析结果不应被误判为内网: %v", err)
	}
	// 但字面量形式照拦。
	if err := Validate("http://198.18.1.58/x", Options{ResolveDNS: true}); err == nil {
		t.Fatal("保留段地址作为字面量时应被拒绝")
	}
}

// TestValidate_fakeip下内网仍被拒 fake-ip 兼容不能放松真正的内网判定。
func TestValidate_fakeip下内网仍被拒(t *testing.T) {
	withResolver(t, resolveTo("127.0.0.1"))
	if err := Validate("http://evil.example/x", Options{ResolveDNS: true}); err == nil {
		t.Fatal("解析到回环的域名应被拒绝（fake-ip 兼容不能放松这条）")
	}
}
