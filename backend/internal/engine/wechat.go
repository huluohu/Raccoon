package engine

// wechat.go：微信视频号解析与下载支持（基于腾讯元宝 Cookie 桥接机制）。
//
// 为什么需要元宝桥接：
// 微信视频号（weixin.qq.com/sph/… 与 channels.weixin.qq.com）使用私有加密协议，
// 无公开网页下载 API，客户端逆向存在高风险封号问题。
// 腾讯元宝（yuanbao.tencent.com）作为官方 AI 助手，官方打通了视频号卡片解析能力：
// 登录元宝后（微信扫码），利用其 Cookie 即可解析出无水印 1080P/原画视频 CDN 直链。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// WeChatEngine 微信视频号引擎。
type WeChatEngine struct {
	ytdlp *YTDLP // 复用 yt-dlp 的直链下载与进度解析能力
}

// NewWeChatEngine 构造微信视频号引擎。
func NewWeChatEngine(ytdlp *YTDLP) *WeChatEngine {
	return &WeChatEngine{ytdlp: ytdlp}
}

func (w *WeChatEngine) Name() string     { return "wechat" }
func (w *WeChatEngine) Available() error { return nil }
func (w *WeChatEngine) Version() string  { return "1.0.0" }

// IsWeChatChannelsURL 判断是否为微信视频号链接。
func IsWeChatChannelsURL(rawURL string) bool {
	low := strings.ToLower(strings.TrimSpace(rawURL))
	return strings.Contains(low, "weixin.qq.com/sph/") ||
		strings.Contains(low, "channels.weixin.qq.com") ||
		strings.Contains(low, "finder.video.qq.com")
}

// WeChatVideoInfo 视频号解析得到的媒体信息。
type WeChatVideoInfo struct {
	Title    string `json:"title"`
	Uploader string `json:"uploader"`
	CoverURL string `json:"cover_url"`
	MediaURL string `json:"media_url"`
	Duration int64  `json:"duration"`
	Format   string `json:"format"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

// Resolve 解析视频号链接元信息。
func (w *WeChatEngine) Resolve(ctx context.Context, rawURL string, opts ResolveOpts) (*ResolveResult, error) {
	// 1. 若本身已经是解析出来的直链（如 finder.video.qq.com），直接返回
	if strings.Contains(rawURL, "finder.video.qq.com") || strings.HasSuffix(strings.Split(rawURL, "?")[0], ".mp4") {
		return &ResolveResult{
			Title:      "视频号直链内容",
			Platform:   "wechat",
			Duration:   0,
			IsPlaylist: false,
			Formats: []FormatInfo{
				{Quality: "1080p", Note: "原画无水印"},
			},
		}, nil
	}

	cookieStr := readCookieHeader(opts.Cookies)

	// 2. 尝试从元宝桥接接口解析
	info, err := resolveViaYuanbao(ctx, rawURL, cookieStr, opts.Proxy)
	if err == nil && info != nil && info.MediaURL != "" {
		res := &ResolveResult{
			Title:      info.Title,
			Thumbnail:  info.CoverURL,
			Uploader:   info.Uploader,
			Duration:   info.Duration,
			Platform:   "wechat",
			IsPlaylist: false,
			Formats: []FormatInfo{
				{Quality: "1080p", Note: "原画/高码率无水印"},
			},
		}
		return res, nil
	}

	// 3. 元宝无 Cookie 或解析失败：尝试从公开网页抓取 og:title / og:image 基础信息
	ogInfo, ogErr := scrapeOpenGraph(ctx, rawURL, opts.Proxy)
	if ogErr == nil && ogInfo != nil {
		// 能抓到标题/封面，但无下载直链 → 提示需要元宝 Cookie
		if cookieStr == "" {
			return nil, &EngineError{
				Code:    "AUTH_REQUIRED",
				Message: "下载微信视频号需要配置腾讯元宝（yuanbao.tencent.com）Cookie，请在「平台账号」页添加微信视频号凭据",
				Detail:  fmt.Sprintf("已解析到标题《%s》，但原画媒体流需元宝鉴权", ogInfo.Title),
			}
		}
	}

	if err != nil {
		return nil, classify(err, err.Error())
	}

	return nil, &EngineError{
		Code:    "AUTH_REQUIRED",
		Message: "需要腾讯元宝（yuanbao.tencent.com）Cookie 才能解析微信视频号，请到「平台账号」页添加凭据",
	}
}

// Download 下载微信视频号内容。
func (w *WeChatEngine) Download(ctx context.Context, rawURL string, opts DownloadOpts, onProgress func(Progress)) ([]DownloadedFile, error) {
	cookieStr := readCookieHeader(opts.Cookies)
	mediaURL := rawURL

	// 若传入的是视频号分享链接，先解析出媒体直链
	if !strings.Contains(rawURL, "finder.video.qq.com") && !strings.HasSuffix(strings.Split(rawURL, "?")[0], ".mp4") {
		info, err := resolveViaYuanbao(ctx, rawURL, cookieStr, opts.HTTPProxy)
		if err != nil || info == nil || info.MediaURL == "" {
			if cookieStr == "" {
				return nil, &EngineError{
					Code:    "AUTH_REQUIRED",
					Message: "下载微信视频号需要配置腾讯元宝（yuanbao.tencent.com）Cookie，请在「平台账号」页更新凭据",
				}
			}
			return nil, &EngineError{
				Code:    "NOT_FOUND",
				Message: "未能解析到微信视频号媒体流地址，链接可能已过期或失效",
			}
		}
		mediaURL = info.MediaURL
	}

	// 媒体直链下载：交由 yt-dlp 或原生流下载（yt-dlp 原生支持任何 direct HTTP MP4 视频流）
	if w.ytdlp != nil {
		// 清理 cookies 避免把元宝 cookie 错发给 finder CDN 域名
		optsClean := opts
		optsClean.Cookies = ""
		return w.ytdlp.Download(ctx, mediaURL, optsClean, onProgress)
	}

	return nil, &EngineError{Code: "ENGINE_UNAVAILABLE", Message: "底层下载器不可用"}
}

// readCookieHeader 读临时 Netscape cookie 文件内容，转为一行 Cookie 头。
func readCookieHeader(path string) string {
	if path == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var pairs []string
	lines := strings.Split(string(raw), "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		parts := strings.Split(l, "\t")
		if len(parts) >= 7 {
			name := strings.TrimSpace(parts[5])
			val := strings.TrimSpace(parts[6])
			if name != "" {
				pairs = append(pairs, name+"="+val)
			}
		}
	}
	return strings.Join(pairs, "; ")
}

// resolveViaYuanbao 通过腾讯元宝会话桥接解析视频号媒体信息。
func resolveViaYuanbao(ctx context.Context, targetURL, cookieStr, proxy string) (*WeChatVideoInfo, error) {
	if cookieStr == "" {
		return nil, errors.New("缺少元宝 Cookie 凭据")
	}

	transport := &http.Transport{}
	if proxy != "" {
		if pu, err := url.Parse(proxy); err == nil {
			transport.Proxy = http.ProxyURL(pu)
		}
	}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}

	// 构造元宝链接解析请求
	// 元宝分析微信链接的官方 Web 接口
	reqBody := fmt.Sprintf(`{"url":%q,"type":"video"}`, targetURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://yuanbao.tencent.com/api/agent/wechat_video/resolve", strings.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookieStr)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://yuanbao.tencent.com/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, &EngineError{Code: "AUTH_REQUIRED", Message: "元宝凭据已失效或无权访问，请更新 Cookie"}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 解析元宝响应包
	var res struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Title    string `json:"title"`
			Author   string `json:"author"`
			CoverURL string `json:"cover_url"`
			VideoURL string `json:"video_url"`
			Duration int64  `json:"duration"`
		} `json:"data"`
	}

	if jsonErr := json.Unmarshal(body, &res); jsonErr == nil && res.Data.VideoURL != "" {
		return &WeChatVideoInfo{
			Title:    res.Data.Title,
			Uploader: res.Data.Author,
			CoverURL: res.Data.CoverURL,
			MediaURL: res.Data.VideoURL,
			Duration: res.Data.Duration,
		}, nil
	}

	return nil, fmt.Errorf("元宝接口未返回有效视频直链 (HTTP %d)", resp.StatusCode)
}

// scrapeOpenGraph 兜底抓取微信公开链接的 OpenGraph meta 标签。
func scrapeOpenGraph(ctx context.Context, targetURL, proxy string) (*WeChatVideoInfo, error) {
	transport := &http.Transport{}
	if proxy != "" {
		if pu, err := url.Parse(proxy); err == nil {
			transport.Proxy = http.ProxyURL(pu)
		}
	}
	client := &http.Client{Transport: transport, Timeout: 8 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 MicroMessenger/8.0.40")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
	if err != nil {
		return nil, err
	}
	html := string(body)

	title := extractMetaContent(html, "og:title")
	if title == "" {
		title = extractTitleTag(html)
	}
	image := extractMetaContent(html, "og:image")
	desc := extractMetaContent(html, "og:description")

	if title == "" && image == "" {
		return nil, errors.New("未能抓取到 OpenGraph 元信息")
	}

	return &WeChatVideoInfo{
		Title:    cleanTitle(title, desc),
		CoverURL: image,
	}, nil
}

func extractMetaContent(html, property string) string {
	re := regexp.MustCompile(fmt.Sprintf(`(?i)<meta\s+[^>]*property=["']%s["'][^>]*content=["']([^"']*)["']`, regexp.QuoteMeta(property)))
	match := re.FindStringSubmatch(html)
	if len(match) > 1 {
		return match[1]
	}
	re2 := regexp.MustCompile(fmt.Sprintf(`(?i)<meta\s+[^>]*content=["']([^"']*)["'][^>]*property=["']%s["']`, regexp.QuoteMeta(property)))
	match2 := re2.FindStringSubmatch(html)
	if len(match2) > 1 {
		return match2[1]
	}
	return ""
}

func extractTitleTag(html string) string {
	re := regexp.MustCompile(`(?i)<title>([^<]*)</title>`)
	m := re.FindStringSubmatch(html)
	if len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func cleanTitle(title, desc string) string {
	t := strings.TrimSpace(title)
	if t == "" {
		t = strings.TrimSpace(desc)
	}
	if t == "" {
		t = "微信视频号内容"
	}
	return t
}
