package engine

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

// ---- TLS 指纹伪装（impersonation）----
//
// 站点按 TLS 指纹特征决定给不给内容：PornHub 等对非浏览器指纹直接返回
// 410 Gone。yt-dlp 具备该能力但**必须显式 --impersonate 才生效**，
// 且依赖运行时装了 curl_cffi。两个前提缺一不可，缺了都不报错、只是解析不出来。

func TestHasCurlCffi_探测输出含curl_cffi才算有(t *testing.T) {
	orig := curlCffiProbe
	t.Cleanup(func() {
		curlCffiProbe = orig
		curlCffiOnce = sync.Once{}
		curlCffiHas = false
	})

	curlCffiOnce = sync.Once{}
	curlCffiProbe = func(bin string) bool { return true }
	if !hasCurlCffi("/bin/true") {
		t.Fatal("探测返回 true 时应判定为具备伪装能力")
	}

	curlCffiOnce = sync.Once{}
	curlCffiProbe = func(bin string) bool { return false }
	if hasCurlCffi("/bin/true") {
		t.Fatal("探测返回 false 时应判定为不具备伪装能力")
	}
}

func TestImpersonateArgs_有curl_cffi才加参数(t *testing.T) {
	orig := curlCffiProbe
	t.Cleanup(func() {
		curlCffiProbe = orig
		curlCffiOnce = sync.Once{}
		curlCffiHas = false
	})

	y := NewYTDLP("/usr/local/bin/yt-dlp", "")

	// 缺 curl_cffi：绝不能加 --impersonate，否则 yt-dlp 会直接报错退出，
	// 导致所有站点都不可用（不只是需要伪装的那个）。
	curlCffiOnce = sync.Once{}
	curlCffiProbe = func(bin string) bool { return false }
	if got := y.impersonateArgs(); len(got) != 0 {
		t.Fatalf("缺 curl_cffi 时不应加伪装参数，实际 %v", got)
	}

	// 有 curl_cffi：必须显式指定目标，否则 yt-dlp 不会启用伪装。
	curlCffiOnce = sync.Once{}
	curlCffiProbe = func(bin string) bool { return true }
	got := y.impersonateArgs()
	if len(got) != 2 || got[0] != "--impersonate" || got[1] == "" {
		t.Fatalf("应产出 --impersonate <target>，实际 %v", got)
	}
}

func TestClassify_410归类为指纹拦截而非内容不存在(t *testing.T) {
	// 410 Gone 与 404 语义完全不同：404 是内容没了，410 是站点按请求指纹拒了。
	out := "ERROR: [PornHub] ph6137798b0f906: Unable to download webpage: HTTP Error 410: Gone\n"
	err := classify(&ExitError{Code: 1}, out)
	var ee *EngineError
	if !errors.As(err, &ee) {
		t.Fatalf("期望 EngineError，实际 %T", err)
	}
	if ee.Code != "BLOCKED_BY_FINGERPRINT" {
		t.Fatalf("错误码应为 BLOCKED_BY_FINGERPRINT，实际 %s（message=%s）", ee.Code, ee.Message)
	}
}

func TestClassify_缺伪装能力归类为IMPERSONATION_UNAVAILABLE(t *testing.T) {
	out := "WARNING: [PornHub] The extractor is attempting impersonation, but no impersonate target is available.\n"
	err := classify(&ExitError{Code: 1}, out)
	var ee *EngineError
	if !errors.As(err, &ee) {
		t.Fatalf("期望 EngineError，实际 %T", err)
	}
	if ee.Code != "IMPERSONATION_UNAVAILABLE" {
		t.Fatalf("错误码应为 IMPERSONATION_UNAVAILABLE，实际 %s", ee.Code)
	}
	if !strings.Contains(ee.Message, "curl_cffi") {
		t.Fatalf("提示应点明缺少 curl_cffi，实际 %s", ee.Message)
	}
}

// TestPlatformOf_新增站点映射 平台标识决定前端徽章与下载目录，
// 新增站点若漏映射会退化成 "web"（界面上显示「通用网页」）。
func TestPlatformOf_新增站点映射(t *testing.T) {
	cases := []struct {
		extractor string
		url       string
		want      string
	}{
		{"PornHub", "https://example.com/x", "pornhub"},
		{"cn.pornhub.com", "https://example.com/x", "pornhub"},
		{"", "https://cn.pornhub.com/view_video.php?viewkey=x", "pornhub"},
		{"Vimeo", "https://example.com/x", "vimeo"},
		{"", "https://vimeo.com/12345", "vimeo"},
		{"soundcloud", "https://example.com/x", "soundcloud"},
		{"Bandcamp", "https://example.com/x", "bandcamp"},
		{"Douyin", "https://example.com/x", "douyin"},
		{"TikTok", "https://example.com/x", "tiktok"},
		{"", "https://www.douyin.com/video/123", "douyin"},
		{"", "https://www.tiktok.com/@user/video/123", "tiktok"},
		{"", "https://music.163.com/song?id=1", "netease"},
	}
	for _, c := range cases {
		if got := platformOf(c.extractor, c.url); got != c.want {
			t.Errorf("platformOf(%q, %q) = %q，期望 %q", c.extractor, c.url, got, c.want)
		}
	}
}
