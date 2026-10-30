package task

import (
	"strings"
	"testing"

	"github.com/huluohu/raccoon/internal/engine"
)

// indexByte 返回分隔符位置（-1 表示没有）。
func indexByte(s string, c byte) int { return strings.IndexByte(s, c) }

// TestMediaTargetsFor_4K档位归属正确 媒体域名归属用于失败时点名「是哪一段路断了」。
// 档位上限必须与引擎一致（engine.ParseHeightQuality）：若这里仍用硬编码表，
// 选了 1440p 会退化成「不设限」，把域名记成 2160p 那档，提示就点名错了 CDN。
func TestMediaTargetsFor_4K档位归属正确(t *testing.T) {
	formats := []engine.FormatInfo{
		{Quality: "2160p", URL: "https://v4k.example.com/a.m3u8", Host: "v4k.example.com"},
		{Quality: "1080p", URL: "https://v1080.example.com/b.m3u8", Host: "v1080.example.com"},
		{Quality: "720p", URL: "https://v720.example.com/c.m3u8", Host: "v720.example.com"},
		{Quality: "audio", URL: "https://a.example.com/d.m4a", Host: "a.example.com"},
	}
	cases := []struct{ quality, want string }{
		{"1080p", "v1080.example.com"},
		{"2160p", "v4k.example.com"},
		{"1440p", "v1080.example.com"}, // 无 1440p 档 → 退到不超过 1440p 的最高档
		{"720p", "v720.example.com"},
		{"best", "v4k.example.com"},
	}
	for _, c := range cases {
		_, hosts := mediaTargetsFor(c.quality, formats)
		// hosts = 视频流域名 + 音频流域名（DASH 站点音频是另一条 CDN），
		// 视频流域名排第一——失败提示点名的是它。
		first := hosts
		if i := indexByte(hosts, ','); i >= 0 {
			first = hosts[:i]
		}
		if first != c.want {
			t.Errorf("mediaTargetsFor(%q) 视频域名归属 = %q，期望 %q（完整：%q）", c.quality, first, c.want, hosts)
		}
	}
}
