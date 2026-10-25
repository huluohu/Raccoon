package server

// util.go：server 包通用小工具（错误断言、时间戳、连接测试）。

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// nowMillis 当前 Unix 毫秒。
func nowMillis() int64 { return time.Now().UnixMilli() }

// errorsAs 通用 errors.As 包装。
func errorsAs(err error, target any) bool { return errors.As(err, target) }

// connTestResult 连接测试结果。
type connTestResult struct {
	OK        bool   `json:"ok"`
	Target    string `json:"target"`
	Message   string `json:"message"`
	LatencyMS int64  `json:"latency_ms"`
}

// testConnection 连接类设置健康检查（qb / 115 / telegram）。
//
// 真发一次请求到目标服务；不模拟、不假装成功。
func (s *Server) testConnection(target string, params map[string]any) connTestResult {
	res := connTestResult{Target: target}
	str := func(k string) string {
		if v, ok := params[k].(string); ok {
			return strings.TrimSpace(v)
		}
		return ""
	}
	switch target {
	case "qbittorrent":
		base := str("url")
		if base == "" {
			res.Message = "未配置 qBittorrent 地址"
			return res
		}
		// SSRF 防线：回环/云元数据端点一律拒绝；内网放行（自托管 qB 常在局域网）。
		if _, err := ValidateOutboundURL(base, OutboundPolicy{AllowPrivate: true}); err != nil {
			res.Message = "地址不被允许: " + err.Error()
			return res
		}
		start := time.Now()
		// qB WebUI 登录端点：未授权也返回 403，说明服务在。
		req, _ := http.NewRequest(http.MethodGet, strings.TrimRight(base, "/")+"/api/v2/app/version", nil)
		resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
		res.LatencyMS = time.Since(start).Milliseconds()
		if err != nil {
			res.Message = "无法连接: " + err.Error()
			return res
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			res.OK = true
			res.Message = "连接正常"
		} else if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
			res.OK = true
			res.Message = "服务可达（需要用户名密码，版本接口返回 " + resp.Status + "）"
		} else {
			res.Message = "服务可达但返回异常状态: " + resp.Status
		}
	case "telegram":
		token := str("bot_token")
		if token == "" {
			res.Message = "未配置 Telegram Bot Token"
			return res
		}
		start := time.Now()
		url := "https://api.telegram.org/bot" + token + "/getMe"
		resp, err := (&http.Client{Timeout: 8 * time.Second}).Get(url)
		res.LatencyMS = time.Since(start).Milliseconds()
		if err != nil {
			res.Message = "无法连接: " + err.Error()
			return res
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			res.OK = true
			res.Message = "Bot 可用"
		} else {
			res.Message = "Telegram 返回异常状态: " + resp.Status
		}
	case "n115":
		cookie := str("cookie_uid")
		if cookie == "" {
			res.Message = "未配置 115 账号标识（cookie_uid）"
			return res
		}
		// 115 开放接口需完整鉴权参数，这里只做连通性探测（如实说明限制）。
		start := time.Now()
		conn, err := net.DialTimeout("tcp", "115.com:443", 8*time.Second)
		res.LatencyMS = time.Since(start).Milliseconds()
		if err != nil {
			res.Message = "无法连接 115: " + err.Error()
			return res
		}
		_ = conn.Close()
		res.OK = true
		res.Message = "网络可达（鉴权需在真实下载任务中验证）"
	case "proxy":
		raw := str("proxy")
		if raw == "" && s.cfg != nil {
			raw = s.cfg.GetString("net.proxy")
		}
		if strings.TrimSpace(raw) == "" {
			res.Message = "未配置代理地址（可在下方输入代理后点击测试）"
			return res
		}
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" && u.Scheme != "socks5h") {
			res.Message = "代理地址格式非法，需以 http://、https:// 或 socks5:// 开头"
			return res
		}
		start := time.Now()
		client := &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyURL(u),
			},
			Timeout: 8 * time.Second,
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://cp.cloudflare.com/generate_204", nil)
		if err != nil {
			res.Message = "构造探测请求失败: " + err.Error()
			return res
		}
		resp, err := client.Do(req)
		res.LatencyMS = time.Since(start).Milliseconds()
		if err != nil {
			// 备用探测地址（防单个节点间歇性抖动）
			ctx2, cancel2 := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel2()
			start2 := time.Now()
			req2, _ := http.NewRequestWithContext(ctx2, http.MethodGet, "http://www.gstatic.com/generate_204", nil)
			resp2, err2 := client.Do(req2)
			if err2 != nil {
				res.Message = "代理连接失败: " + err.Error()
				return res
			}
			_ = resp2.Body.Close()
			res.LatencyMS = time.Since(start2).Milliseconds()
			res.OK = true
			res.Message = fmt.Sprintf("代理可用（延迟 %dms）", res.LatencyMS)
			return res
		}
		_ = resp.Body.Close()
		res.OK = true
		res.Message = fmt.Sprintf("代理可用（延迟 %dms）", res.LatencyMS)
	default:
		res.Message = "未知的连接目标: " + target
	}
	return res
}
