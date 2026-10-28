package server

// proxy_validate.go：代理地址的统一校验。
//
// 为什么代理要单独一套（不能直接用 ValidateOutboundURL）：
//   - 代理**本来就在本机或内网**——http://127.0.0.1:7890 是最常见的用法，
//     所以不能像下载内容那样禁私网；
//   - 但链路本地（169.254.169.254 这类云元数据地址）必须禁：否则
//     「给任务指定代理」就成了把服务器内部地址喂出去的跳板；
//   - scheme 白名单要和「设置页保存 net.proxy」同一份口径——此前两处不一致
//     （配置校验拒绝 socks5h://，连通性测试却接受），同一个地址两套说法。

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// proxySchemes 允许的代理协议（yt-dlp 原生支持 socks5/socks5h；Go 的 http.Transport
// 只认 socks5，socks5h 会在出站时降级成本地解析）。
var proxySchemes = map[string]bool{
	"http":    true,
	"https":   true,
	"socks5":  true,
	"socks5h": true,
}

// ValidateProxyURL 校验代理地址；空串视为「未配置」（合法）。
func ValidateProxyURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("代理地址解析失败: %v", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if !proxySchemes[scheme] {
		return fmt.Errorf("代理地址需以 http:// / https:// / socks5:// / socks5h:// 开头")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("代理地址缺少主机名")
	}
	if ip := net.ParseIP(host); ip != nil {
		if err := checkProxyIP(ip); err != nil {
			return err
		}
	}
	return nil
}

// checkProxyIP 拒绝不该作为代理目标的地址。
//
// 允许：回环（127.0.0.1，本机代理）、私有网段（内网代理）。
// 拒绝：链路本地（含 169.254.169.254 云元数据）、未指定、组播。
func checkProxyIP(ip net.IP) error {
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("代理地址不能指向链路本地地址（含云元数据地址）")
	}
	if ip.IsUnspecified() || ip.IsMulticast() {
		return fmt.Errorf("代理地址不能指向未指定或组播地址")
	}
	return nil
}
