package server

// ssrf.go：出站 URL 校验（防 SSRF）——HTTP 入口层的薄封装。
//
// 校验逻辑本体在 internal/netguard（叶子包）：它必须能被 internal/task
// 也用上，因为**远端站点给的媒体直链**同样需要校验（入口 URL 校验过了，
// 不代表引擎返回的直链也安全）。
//
// 为什么需要：连接测试（testConnection）会按用户填的地址发起服务端请求，
// 订阅/通知等出站点同理。若不校验，用户（或能接触到设置页的 XSS）
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
//   - DNS 解析后的结果可能在真正发请求时被换掉（DNS rebinding）；
//     ResolveDNS=true 是纵深防御，彻底解决需要接管引擎的 HTTP 传输层。
//   - 只校验「当前这一次」URL，不跟踪重定向后的落点。

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/huluohu/raccoon/internal/netguard"
)

// OutboundPolicy 出站 URL 校验策略。
type OutboundPolicy struct {
	// AllowPrivate 允许内网地址（连接测试等自配基础设施场景）。
	AllowPrivate bool

	// ResolveDNS 解析域名并逐条校验解析结果（防「域名指向内网」型 SSRF）。
	// 内容类出站（订阅源、直播源）应开启。
	ResolveDNS bool
}

// ValidateOutboundURL 校验出站 URL，合法时返回解析后的 *url.URL。
//
// 一律拒绝（与策略无关）：
//   - 非 http/https 协议（file://、javascript://、gopher:// 等）
//   - 回环地址（127.0.0.0/8、::1）与 localhost 域名
//   - 链路本地地址（169.254.0.0/16，含云元数据端点）与 fe80::/10
//   - 未指定地址（0.0.0.0、::）
//
// AllowPrivate=false 时额外拒绝：
//   - 私网（10/8、172.16/12、192.168/16）、CGNAT（100.64/10）、
//     IPv6 ULA（fc00::/7）、IPv4-mapped IPv6、组播与保留段
//
// ResolveDNS=true 时：域名会被解析，逐条 A/AAAA 记录都要通过上述判定。
func ValidateOutboundURL(raw string, p OutboundPolicy) (*url.URL, error) {
	if err := netguard.Validate(raw, netguard.Options{
		AllowPrivate: p.AllowPrivate,
		ResolveDNS:   p.ResolveDNS,
	}); err != nil {
		return nil, err
	}
	return url.Parse(strings.TrimSpace(raw))
}

// validateOutboundURLStrict 内容类出站的严格校验（含 DNS 复核）。
func validateOutboundURLStrict(raw string) error {
	_, err := ValidateOutboundURL(raw, OutboundPolicy{ResolveDNS: true})
	return err
}

// wrapSSRFError 把 netguard 的错误包装成对外可读的校验错误。
func wrapSSRFError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("出站 URL 校验失败: %w", err)
}
