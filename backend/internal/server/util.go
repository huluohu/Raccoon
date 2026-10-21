package server

// util.go：server 包通用小工具（错误断言、时间戳、连接测试）。

import (
	"errors"
	"net"
	"net/http"
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
	default:
		res.Message = "未知的连接目标: " + target
	}
	return res
}
