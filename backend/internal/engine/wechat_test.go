package engine

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsWeChatChannelsURL(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"https://weixin.qq.com/sph/AbCdEf123", true},
		{"http://weixin.qq.com/sph/xyz", true},
		{"https://channels.weixin.qq.com/web/pages/feed?id=123", true},
		{"https://finder.video.qq.com/251/20302/stodownload?encfilekey=xxx", true},
		{"https://www.bilibili.com/video/BV1xx411c7mD", false},
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsWeChatChannelsURL(tc.url); got != tc.want {
			t.Fatalf("IsWeChatChannelsURL(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

func TestWeChatEngine_PlatformGuess(t *testing.T) {
	if p := PlatformGuess("https://weixin.qq.com/sph/AbCdEf"); p != "wechat" {
		t.Fatalf("PlatformGuess weixin.qq.com/sph/ should be 'wechat', got %q", p)
	}
	if p := PlatformGuess("https://channels.weixin.qq.com/feed/123"); p != "wechat" {
		t.Fatalf("PlatformGuess channels.weixin.qq.com should be 'wechat', got %q", p)
	}
}

func TestWeChatEngine_ResolveDirectFinderLink(t *testing.T) {
	eng := NewWeChatEngine(nil)
	res, err := eng.Resolve(context.Background(), "https://finder.video.qq.com/direct/video.mp4", ResolveOpts{})
	if err != nil {
		t.Fatalf("解析直链不应报错: %v", err)
	}
	if res.Platform != "wechat" {
		t.Fatalf("Platform 应为 wechat，实际 %s", res.Platform)
	}
	if len(res.Formats) == 0 {
		t.Fatal("解析直链应有可用画质格式")
	}
}

func TestWeChatEngine_ResolveRequiresCookie(t *testing.T) {
	eng := NewWeChatEngine(nil)
	// 无 cookie 且非直链
	_, err := eng.Resolve(context.Background(), "https://weixin.qq.com/sph/AbCdEf", ResolveOpts{})
	if err == nil {
		t.Fatal("无元宝 Cookie 解析视频号应返回 AUTH_REQUIRED 错误")
	}
	var ee *EngineError
	if !strings.Contains(err.Error(), "AUTH_REQUIRED") && !errors.As(err, &ee) {
		t.Fatalf("错误应指示 AUTH_REQUIRED，实际: %v", err)
	}
	if !strings.Contains(err.Error(), "元宝") {
		t.Fatalf("错误提示中应包含腾讯元宝引导，实际: %v", err)
	}
}

func TestScrapeOpenGraph(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html><html><head>
			<meta property="og:title" content="精彩视频号内容测试标题" />
			<meta property="og:image" content="https://finder.video.qq.com/thumb.jpg" />
			<meta property="og:description" content="视频号描述" />
		</head><body></body></html>`
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	}))
	defer server.Close()

	info, err := scrapeOpenGraph(context.Background(), server.URL, "")
	if err != nil {
		t.Fatalf("scrapeOpenGraph 失败: %v", err)
	}
	if info.Title != "精彩视频号内容测试标题" {
		t.Fatalf("标题解析错误: %q", info.Title)
	}
	if info.CoverURL != "https://finder.video.qq.com/thumb.jpg" {
		t.Fatalf("封面解析错误: %q", info.CoverURL)
	}
}
