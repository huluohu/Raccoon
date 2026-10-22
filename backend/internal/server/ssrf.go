package server

// ssrf.go：出站 URL 校验（防 SSRF）。
//
// 为什么需要：连接测试（testConnection）会按用户填的地址发起服务端请求，
// 订阅/通知等未来出站点同理。若不校验，用户（或能接触到设置页的 XSS）
// 可让服务端请求回环地址与云元数据端点：
//   - 127.0.0.1 / ::1：探测本机管理端口（含本应用自身 admin 面）
//   - 169.254.169.254：云厂商元数据端点，可窃取临时凭据
//   - 0.0.0.0：未指定地址，行为随平台漂移
//
// 策略取舍：自托管场景里「用户自配基础设施」常常就在内网（qB 跑在
// 192.168.x.x 是常态），所以内网地址不能一律拒绝 —— 否则连接测试对
// 最主要的用户群体直接失效。因此分两档：
//   - AllowPrivate=false：面向「内容类」出站（订阅源、Webhook 等），
//     拒绝全部内网/回环/链路本地地址；
//   - AllowPrivate=true ：面向「连接测试」，仅拒绝回环与链路本地
//     （元数据端点），内网地址放行。
//
// 已知限制（显式声明，不隐瞒）：
//   - 不做 DNS 解析后的复核，DNS Rebinding（解析到内网的域名）不在
//     本函数防护范围内；对自托管单用户工具，该风险由部署网络边界兜底。
//   - 只校验「当前这一次」URL，不跟踪重定向后的落点。

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// OutboundPolicy 出站 URL 校验策略。
type OutboundPolicy struct {
	// AllowPrivate 允许内网地址（连接测试等自配基础设施场景）。
	AllowPrivate bool
}

// ValidateOutboundURL 校验出站 URL，合法时返回解析后的 *url.URL。
//
// 一律拒绝（与策略无关）：
//   - 非 http/https 协议（file://、javascript:、gopher: 等）
//   - 回环地址（127.0.0.0/8、::1）与 localhost 域名
//   - 链路本地地址（169.254.0.0/16，含云元数据端点）与 fe80::/10
//   - 未指定地址（0.0.0.0、::）
//
// AllowPrivate=false 时额外拒绝：
//   - 私网（10/8、172.16/12、192.168/16）、CGNAT（100.64/10）、
//     IPv6 ULA（fc00::/7）、IPv4-mapped IPv6
func ValidateOutboundURL(raw string, p OutboundPolicy) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("URL 为空")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("URL 解析失败: %v", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("仅支持 http/https 协议，收到 %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("URL 缺少主机名")
	}
	// 域名黑名单：localhost 及其常见变体（大小写不敏感）。
	lower := strings.ToLower(strings.TrimSuffix(host, "."))
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return nil, fmt.Errorf("不允许请求本机回环地址")
	}

	// 能解析成 IP 字面量的才做网段判定；域名交给上面的黑名单 + 部署边界。
	if ip := net.ParseIP(host); ip != nil {
		if err := checkIP(ip, p); err != nil {
			return nil, err
		}
	} else if !p.AllowPrivate {
		// 内容类出站：域名也要过一遍常见内网后缀黑名单（.internal/.lan 等）。
		if hasInternalSuffix(lower) {
			return nil, fmt.Errorf("不允许请求内网域名 %q", host)
		}
	}
	return u, nil
}

// checkIP 对 IP 字面量做网段判定。
func checkIP(ip net.IP, p OutboundPolicy) error {
	// IPv4-mapped IPv6（::ffff:127.0.0.1）还原成 v4 再判，防止绕过。
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsLoopback() {
		return fmt.Errorf("不允许请求回环地址 %s", ip)
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		// 169.254.169.254（云元数据）与 fe80::/10 都在这里被拒。
		return fmt.Errorf("不允许请求链路本地地址 %s（含云元数据端点）", ip)
	}
	if ip.IsUnspecified() {
		return fmt.Errorf("不允许请求未指定地址 %s", ip)
	}
	if !p.AllowPrivate {
		if ip.IsPrivate() || isCGNAT(ip) || isIPv6ULA(ip) {
			return fmt.Errorf("不允许请求内网地址 %s", ip)
		}
	}
	return nil
}

// isCGNAT 100.64.0.0/10（运营商级 NAT，RFC 6598）。
func isCGNAT(ip net.IP) bool {
	v4 := ip.To4()
	if v4 == nil {
		return false
	}
	return v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127
}

// isIPv6ULA fc00::/7（IPv6 唯一本地地址）。
func isIPv6ULA(ip net.IP) bool {
	if ip.To4() != nil {
		return false
	}
	return len(ip) == net.IPv6len && (ip[0]&0xfe) == 0xfc
}

// hasInternalSuffix 常见内网域名后缀（内容类出站场景的域名黑名单）。
func hasInternalSuffix(host string) bool {
	for _, suf := range []string{".internal", ".lan", ".local", ".intranet", ".corp"} {
		if strings.HasSuffix(host, suf) {
			return true
		}
	}
	return false
}
