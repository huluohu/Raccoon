// Package bilibili：B 站账号态的免密获取（扫码登录）。
//
// 为什么需要它：会员/付费内容在服务端要求登录态，凭据中心过去只能
// 「手动复制浏览器 Cookie 粘贴进来」。扫码登录把这条路压到零摩擦——
// 用户手机上本来就登着 B 站 App，扫一下码，Cookie 就进凭据中心。
//
// 流程（B 站 passport 开放接口，BBDown 等工具同源）：
//  1. Generate 拿二维码内容 url + qrcode_key（前端渲染二维码）；
//  2. Poll 轮询 qrcode_key：86101 未扫 / 86090 已扫未确认 / 86038 过期；
//  3. 确认后响应 Set-Cookie 里带 SESSDATA / bili_jct / DedeUserID 等，
//     拼成 Cookie 头字符串，由凭据中心加密落库。
package bilibili

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PassportBase B 站 passport 接口地址（测试可注入）。
var PassportBase = "https://passport.bilibili.com"

// 轮询状态码（B 站协议约定）。
const (
	CodeWaitingScan  = 86101 // 未扫码
	CodeScanned      = 86090 // 已扫码未确认
	CodeQRExpired    = 86038 // 二维码已过期
	CodeSuccess      = 0     // 登录成功（Set-Cookie 带凭据）
	defaultTimeout   = 10 * time.Second
	maxBodyBytes     = 1 << 20
	credentialExpiry = 180 * 24 * time.Hour // SESSDATA 实际有效期以平台为准，这里只做展示级估值
)

// QRStart Generate 的结果。
type QRStart struct {
	URL       string // 二维码内容（前端渲染成图）
	QrcodeKey string // 轮询凭据
}

// PollResult Poll 的结果；Code=0 时 Cookie 非空。
type PollResult struct {
	Code    int
	Message string
	Cookie  string // "SESSDATA=…; bili_jct=…; DedeUserID=…"（成功时）
}

// Client 扫码登录客户端。
type Client struct {
	hc *http.Client
}

// NewClient 构造；hc 为 nil 时用默认超时。
func NewClient(hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{hc: hc}
}

// Generate 申请二维码（POST 更贴近网页端行为，GET 亦可）。
func (c *Client) Generate(ctx context.Context) (*QRStart, error) {
	raw, err := c.get(ctx, PassportBase+"/x/passport-login/web/qrcode/generate")
	if err != nil {
		return nil, err
	}
	var out struct {
		Code int `json:"code"`
		Data struct {
			URL       string `json:"url"`
			QrcodeKey string `json:"qrcode_key"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("解析二维码响应失败: %w", err)
	}
	if out.Code != 0 || out.Data.URL == "" || out.Data.QrcodeKey == "" {
		return nil, fmt.Errorf("获取二维码失败（code=%d）", out.Code)
	}
	return &QRStart{URL: out.Data.URL, QrcodeKey: out.Data.QrcodeKey}, nil
}

// Poll 轮询扫码状态。成功时从响应 Set-Cookie 提取登录 Cookie。
//
// 协议形状（真机验证过，2026-10）：状态码在**内层** data.code——外层 code
// 恒为 0，只有接口本身出错才非 0。状态语义：86101 未扫 / 86090 已扫未确认 /
// 86038 过期 / 0 成功（Set-Cookie 带凭据 + data.url 回跳地址）。
// 只保留登录必需的四枚 Cookie：SESSDATA（会话）、bili_jct（CSRF）、
// DedeUserID / DedeUserID__ckMd5（账号标识）。其余（sid、buvid 等
// 设备指纹类）对下载鉴权无用，带出去反而徒增凭据面。
func (c *Client) Poll(ctx context.Context, qrcodeKey string) (*PollResult, error) {
	if strings.TrimSpace(qrcodeKey) == "" {
		return nil, errors.New("qrcode_key 不能为空")
	}
	u := PassportBase + "/x/passport-login/web/qrcode/poll?qrcode_key=" + url.QueryEscape(qrcodeKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 Raccoon/1.0")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, err
	}
	var out struct {
		Code int `json:"code"` // 外层：接口级错误（非 0 = 接口本身挂了）
		Data struct {
			Code    int    `json:"code"` // 内层：扫码状态机（86101/86090/86038/0）
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("解析轮询响应失败: %w", err)
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("B 站轮询接口异常（code=%d）", out.Code)
	}
	res := &PollResult{Code: out.Data.Code, Message: out.Data.Message}
	if out.Data.Code != CodeSuccess {
		return res, nil
	}
	cookie := pickLoginCookies(resp.Cookies())
	if cookie == "" {
		return nil, errors.New("登录成功但响应未携带 SESSDATA（B 站协议变更？）")
	}
	res.Cookie = cookie
	return res, nil
}

// pickLoginCookies 从 Set-Cookie 里挑登录必需项拼 Cookie 头。
func pickLoginCookies(cookies []*http.Cookie) string {
	want := map[string]bool{
		"SESSDATA": true, "bili_jct": true,
		"DedeUserID": true, "DedeUserID__ckMd5": true,
	}
	var parts []string
	for _, ck := range cookies {
		if want[ck.Name] && ck.Value != "" {
			parts = append(parts, ck.Name+"="+ck.Value)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ")
}

// CredentialExpiry 凭据展示级有效期估值（扫码 Cookie 常态约半年）。
func CredentialExpiry() int64 { return time.Now().Add(credentialExpiry).UnixMilli() }

// get 取 JSON（B 站接口 GET 为主）。
func (c *Client) get(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 Raccoon/1.0")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("B 站接口返回 HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
}
