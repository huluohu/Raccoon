package server

// ssrf_test.go：出站 URL 校验用例集（docs/08 W5.4 安全用例）。
//
// 这些用例锁死的是「服务端能替用户请求到哪里」的边界：
// 回环、云元数据、内网、非 HTTP 协议是四条硬红线。

import (
	"testing"
)

// TestValidateOutboundURL_一律拒绝的红线 与策略无关的拒绝项。
func TestValidateOutboundURL_一律拒绝的红线(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"回环IPv4", "http://127.0.0.1/x"},
		{"回环IPv4另一段", "http://127.8.9.10/admin"},
		{"回环IPv6", "http://[::1]/x"},
		{"localhost域名", "http://localhost:8080/x"},
		{"localhost大小写", "http://LOCALHOST/x"},
		{"localhost子域", "http://app.localhost/x"},
		{"云元数据端点", "http://169.254.169.254/latest/meta-data"},
		{"链路本地其它", "http://169.254.1.1/x"},
		{"IPv6链路本地", "http://[fe80::1]/x"},
		{"未指定地址", "http://0.0.0.0/x"},
		{"file协议", "file:///etc/passwd"},
		{"javascript协议", "javascript:alert(1)"},
		{"gopher协议", "gopher://127.0.0.1:6379/_INFO"},
		{"ftp协议", "ftp://10.0.0.1/x"},
		{"空URL", ""},
		{"只有空格", "   "},
		{"无主机名", "http:///path"},
	}
	for _, p := range []OutboundPolicy{{AllowPrivate: false}, {AllowPrivate: true}} {
		for _, c := range cases {
			if _, err := ValidateOutboundURL(c.raw, p); err == nil {
				t.Errorf("%s（AllowPrivate=%v）应被拒绝，实际放行", c.name, p.AllowPrivate)
			}
		}
	}
}

// TestValidateOutboundURL_内网分档 私网地址按策略分档放行/拒绝。
func TestValidateOutboundURL_内网分档(t *testing.T) {
	private := []struct {
		name string
		raw  string
	}{
		{"10段", "http://10.0.0.1/x"},
		{"17216段", "http://172.16.3.4/x"},
		{"192168段", "http://192.168.1.1:8080/x"},
		{"CGNAT", "http://100.100.1.1/x"},
		{"IPv6ULA", "http://[fc00::1]/x"},
		{"IPv4-mapped回环", "http://[::ffff:127.0.0.1]/x"},
	}
	// 严格模式：全部拒绝。
	for _, c := range private {
		if _, err := ValidateOutboundURL(c.raw, OutboundPolicy{}); err == nil {
			t.Errorf("严格模式下 %s 应被拒绝，实际放行", c.name)
		}
	}
	// 连接测试模式：内网放行（用户自配 qB 常在局域网），回环仍拒。
	lan := []string{"http://10.0.0.1/x", "http://172.16.3.4/x", "http://192.168.1.1:8080/x", "http://100.100.1.1/x"}
	for _, raw := range lan {
		if _, err := ValidateOutboundURL(raw, OutboundPolicy{AllowPrivate: true}); err != nil {
			t.Errorf("连接测试模式下 %s 应放行（自托管基础设施常在内网），实际拒绝: %v", raw, err)
		}
	}
	if _, err := ValidateOutboundURL("http://[::ffff:127.0.0.1]/x", OutboundPolicy{AllowPrivate: true}); err == nil {
		t.Error("IPv4-mapped 回环即使在 AllowPrivate 模式下也应拒绝（防绕过）")
	}
}

// TestValidateOutboundURL_放行公网 正常出站目标必须放行。
func TestValidateOutboundURL_放行公网(t *testing.T) {
	public := []string{
		"https://www.bilibili.com/video/BV1xx411c7mD",
		"https://music.163.com/song?id=1",
		"http://93.184.216.34/x", // 公网 IPv4 字面量
		"https://qbit.example.com:8443/",
		"https://api.telegram.org/bot123/getMe",
	}
	for _, raw := range public {
		if _, err := ValidateOutboundURL(raw, OutboundPolicy{}); err != nil {
			t.Errorf("%s 应放行，实际拒绝: %v", raw, err)
		}
	}
}

// TestValidateOutboundURL_内容类域名黑名单 .internal/.lan 等后缀仅在严格模式拒。
func TestValidateOutboundURL_内容类域名黑名单(t *testing.T) {
	if _, err := ValidateOutboundURL("http://nas.internal/x", OutboundPolicy{}); err == nil {
		t.Error("严格模式应拒绝 .internal 域名")
	}
	if _, err := ValidateOutboundURL("http://nas.lan/x", OutboundPolicy{}); err == nil {
		t.Error("严格模式应拒绝 .lan 域名")
	}
	// 连接测试模式不查域名后缀（用户可能用内网域名指向自配服务）。
	if _, err := ValidateOutboundURL("http://nas.internal/x", OutboundPolicy{AllowPrivate: true}); err != nil {
		t.Errorf("连接测试模式应放行内网域名，实际拒绝: %v", err)
	}
}

// TestValidateOutboundURL_返回可用URL 放行时返回的 *url.URL 字段可被调用方直接用。
func TestValidateOutboundURL_返回可用URL(t *testing.T) {
	u, err := ValidateOutboundURL("https://qbit.example.com:8443/base", OutboundPolicy{AllowPrivate: true})
	if err != nil {
		t.Fatalf("应放行: %v", err)
	}
	if u.Scheme != "https" || u.Hostname() != "qbit.example.com" || u.Port() != "8443" {
		t.Errorf("返回的 URL 字段解析不对: %+v", u)
	}
}
