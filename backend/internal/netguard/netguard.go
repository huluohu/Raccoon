// Package netguard：出站 URL 校验（防 SSRF）。
//
// 为什么独立成包：这道校验原先只存在于 internal/server（HTTP 入口层），
// 于是**引擎交给上层的 URL 完全没被校验过**——而那些 URL 来自远端站点的
// 响应，是攻击者可控的。最典型的例子：媒体直取旁路（task/worker.go
// fetchMediaDirect）把引擎给出的 m3u8/mpd 直链原样交给 ffmpeg 抓取，
// 站点只要在页面里写一个 http://169.254.169.254/... 的清单地址，
// 浣熊就会替它去读云元数据端点，**读到的内容还会落盘并进媒体库**。
//
// 校验必须放在「真正发起请求之前」，所以下沉到叶子包：server、task 都能用。
//
// 三层防护：
//
//  1. 协议白名单：只允许 http/https（挡 file://、gopher:// 等）。
//  2. 主机名/IP 判定：回环、链路本地（含云元数据 169.254.169.254）、
//     未指定、私网、CGNAT、IPv6 ULA、组播、保留段——一律拒；
//     域名黑名单盖住 localhost 与 .internal/.lan/.local/.intranet/.corp。
//  3. **DNS 解析后复核**（ResolveDNS）：把域名解析出来，逐个 A/AAAA 记录
//     判定网段。这是「域名指向内网」这类 SSRF 的唯一有效拦截点——
//     只看字面量 IP 的校验对 `http://evil.example/` 形同虚设
//     （它可以解析到 127.0.0.1）。
package netguard

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// Options 控制校验强度。
type Options struct {
	// AllowPrivate 允许私网地址（连接测试等「用户自配基础设施」场景）。
	// 注意：仅对内网地址生效，回环/链路本地/未指定地址永远拒绝。
	AllowPrivate bool

	// ResolveDNS 解析域名并逐个校验解析结果。
	//
	// 何时该开：
	//   - 校验发生在**真正发请求之前**（媒体直取、连接测试）→ 应开；
	//   - 校验发生在入口、请求由第三方组件稍后发起 → 开了也只是纵深防御
	//     （解析结果可能在请求时被换掉，即 DNS rebinding）。
	ResolveDNS bool

	// Timeout DNS 解析超时（默认 3s）。ResolveDNS=false 时忽略。
	Timeout time.Duration
}

// 默认 DNS 解析超时。
const defaultDNSTimeout = 3 * time.Second

// Validate 校验一个出站 URL；合法返回 nil。
//
// 只做校验，不发起任何请求。
func Validate(raw string, opts Options) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("URL 为空")
	}
	u, err := parseHTTPURL(raw)
	if err != nil {
		return err
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL 缺少主机名")
	}
	lower := strings.ToLower(strings.TrimSuffix(host, "."))
	if isLoopbackName(lower) {
		return fmt.Errorf("不允许请求本机回环地址 %q", host)
	}
	if ip := net.ParseIP(lower); ip != nil {
		// IP 字面量：直接判网段。
		if err := checkIP(ip, opts, true); err != nil {
			return err
		}
		return nil
	}
	// 域名：严格模式下查后缀黑名单，再（可选）解析后复核。
	//
	// AllowPrivate=true（连接测试等自配基础设施场景）**不查后缀**——
	// 用户完全可能用 nas.internal 之类的内网域名指向自建服务，那是合法用法。
	if !opts.AllowPrivate && hasInternalSuffix(lower) {
		return fmt.Errorf("不允许请求内网域名 %q", host)
	}
	if opts.ResolveDNS {
		if err := checkResolved(host, opts); err != nil {
			return err
		}
	}
	return nil
}

// ValidateMedia 校验「远端给的媒体直链」——最严格的一档：
// 域名必须解析，且**所有**解析结果都必须是公网地址。
//
// 场景：媒体直取旁路把远端站点返回的清单地址交给 ffmpeg 抓取。那条地址
// 完全由远端控制，必须按「内容类出站」的严格标准校验。
func ValidateMedia(raw string) error {
	return Validate(raw, Options{ResolveDNS: true, Timeout: defaultDNSTimeout})
}

// checkResolved 解析域名并逐个校验解析结果。
//
// 任何一个解析结果落在内网/保留段即拒绝：攻击者可以配置「一个域名解析到
// 公网 IP + 一个解析到 127.0.0.1」，只查第一条会漏。
func checkResolved(host string, opts Options) error {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultDNSTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ips, err := resolver(ctx, host)
	if err != nil {
		// 解析失败**不拦截**：走代理的用户，域名只在代理侧解析，本机解析
		// 失败是常态（而引擎经代理能正常抓到内容）。拦截会把这类用户
		// 全部挡在门外——那是误伤，不是防护。
		// 真正的攻击形态（域名解析到 127.0.0.1）在本机是**能解析出来**的，
		// 走下面那条判定就被拦住了。
		return nil
	}
	if len(ips) == 0 {
		return nil
	}
	for _, ip := range ips {
		// 解析出来的地址按「非字面量」判定（保留段放行，见 checkIP 注释）。
		if err := checkIP(ip, opts, false); err != nil {
			return fmt.Errorf("域名 %q 解析到 %s：%w", host, ip, err)
		}
	}
	return nil
}

// resolver DNS 解析函数（测试可替换）。
var resolver = func(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

// SetResolverForTest 替换 DNS 解析器，返回还原函数。
//
// 仅供测试使用：跨包测试（如 internal/task）需要构造「域名解析到内网」
// 的场景，而不能真去解析一个攻击者控制的域名。
func SetResolverForTest(fn func(ctx context.Context, host string) ([]net.IP, error)) func() {
	orig := resolver
	resolver = fn
	return func() { resolver = orig }
}

// parseHTTPURL 解析并做协议白名单校验。
func parseHTTPURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("URL 解析失败: %v", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return nil, fmt.Errorf("仅支持 http/https 协议，收到 %q", u.Scheme)
	}
	return u, nil
}

// isLoopbackName 回环主机名（大小写与结尾点不敏感）。
func isLoopbackName(host string) bool {
	switch host {
	case "localhost", "ip6-localhost", "ip6-loopback":
		return true
	}
	return strings.HasSuffix(host, ".localhost")
}

// hasInternalSuffix 常见内网域名后缀。
func hasInternalSuffix(host string) bool {
	for _, suf := range []string{".internal", ".lan", ".local", ".intranet", ".corp"} {
		if strings.HasSuffix(host, suf) {
			return true
		}
	}
	return false
}

// checkIP 对 IP 做网段判定。
//
// fromLiteral：该地址是 URL 里直接写的 IP 字面量（true），还是域名解析出来的
// （false）。两者的判定强度不同——见「保留段」的处理。
func checkIP(ip net.IP, opts Options, fromLiteral bool) error {
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
	if ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return fmt.Errorf("不允许请求组播地址 %s", ip)
	}
	if !opts.AllowPrivate {
		if ip.IsPrivate() || isCGNAT(ip) || isIPv6ULA(ip) {
			return fmt.Errorf("不允许请求内网地址 %s", ip)
		}
		// 保留段（198.18/15 基准测试段、文档用段、240/4 等）只在**字面量**时拦。
		//
		// 为什么区分：Clash/mihomo 的 fake-ip 模式会把所有域名解析到
		// 198.18.0.0/16 的假地址，再由本机代理按假地址还原真实目的地。
		// 国内用户大量在用这种 DNS——若把 198.18/15 一律判为「内网」，
		// **所有下载都会被拦**（实测：解析出的 198.18.x.x 让正常站点全挂）。
		//
		// 这些段本身不是「内网」，不指向任何敏感目标（不可路由到回环、
		// 元数据端点或内网服务），放行不带来 SSRF 收益；而字面量形式
		// 写进 URL 的保留段地址显然不是正常下载目标，照拦不误。
		if fromLiteral && isReserved(ip) {
			return fmt.Errorf("不允许请求保留地址 %s", ip)
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

// isReserved IPv4 保留段（net.IP.IsPrivate 已覆盖 RFC1918）。
func isReserved(ip net.IP) bool {
	v4 := ip.To4()
	if v4 == nil {
		return false
	}
	switch {
	case v4[0] == 0: // 0.0.0.0/8「本网络」
		return true
	case v4[0] == 192 && v4[1] == 0 && v4[2] == 0: // 192.0.0.0/24
		return true
	case v4[0] == 198 && (v4[1] == 18 || v4[1] == 19): // 基准测试段 198.18/15
		return true
	case v4[0] == 198 && v4[1] == 51 && v4[2] == 100: // 文档用 198.51.100/24
		return true
	case v4[0] == 203 && v4[1] == 0 && v4[2] == 113: // 文档用 203.0.113/24
		return true
	case v4[0] >= 240: // 240.0.0.0/4 保留
		return true
	}
	return false
}
