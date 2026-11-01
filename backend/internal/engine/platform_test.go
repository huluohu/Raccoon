package engine

// platform_test.go：平台标识与图站路由的边界判定。
//
// 2026-10-30 修的两类错：
//
//  1. **子串匹配当域名匹配**。原先用 strings.Contains(host, "x.com")，
//     max.com / linux.com / matrix.com 全被标成 twitter → 凭据中心会去
//     **取 twitter 的 Cookie 发给这些站**（凭据注入按 platform 精确匹配）。
//  2. **平台标识缺失导致凭据送不到**。知乎/小红书短链/图站们一律落到
//     "web"，而 cookieDomainFor 认识 "zhihu" —— 用户配了知乎 Cookie，
//     任务因 platform="web" 永远取不到。

import "testing"

// TestHostMatches_按点分边界匹配 域名相等或为其子域才算命中。
func TestHostMatches_按点分边界匹配(t *testing.T) {
	cases := []struct {
		host, domain string
		want         bool
	}{
		{"x.com", "x.com", true},
		{"twitter.com", "twitter.com", true},
		{"mobile.twitter.com", "twitter.com", true},
		{"www.bilibili.com", "bilibili.com", true},
		// 这些都不该命中 x.com —— 原先用 Contains 会误判
		{"max.com", "x.com", false},
		{"linux.com", "x.com", false},
		{"matrix.com", "x.com", false},
		{"xcom", "x.com", false},
		{"", "x.com", false},
		{"x.com", "", false},
	}
	for _, c := range cases {
		if got := hostMatches(c.host, c.domain); got != c.want {
			t.Errorf("hostMatches(%q, %q) = %v，期望 %v", c.host, c.domain, got, c.want)
		}
	}
}

// TestPlatformOf_不把无关域名误判成twitter 子串匹配的历史坑。
func TestPlatformOf_不把无关域名误判成twitter(t *testing.T) {
	for _, u := range []string{
		"https://max.com/some/video",
		"https://linux.com/watch?v=1",
		"https://matrix.com/video/1",
	} {
		if got := platformOf("", u); got == "twitter" {
			t.Errorf("%s 被误判成 twitter（会取 twitter 凭据发过去）", u)
		}
	}
}

// TestPlatformOf_图站平台标识可用 这些域名以前一律落到 "web"，
// 凭据注入按 platform 取 → 用户配的 Cookie 永远送不到。
func TestPlatformOf_图站平台标识可用(t *testing.T) {
	cases := []struct{ url, want string }{
		{"https://www.zhihu.com/question/123", "zhihu"},
		{"https://xhslink.com/a/abc123", "xiaohongshu"},
		{"https://www.xiaohongshu.com/explore/1", "xiaohongshu"},
		{"https://www.pixiv.net/artworks/1", "pixiv"},
		{"https://www.deviantart.com/user/art", "deviantart"},
		{"https://www.artstation.com/artwork/1", "artstation"},
		{"https://imgur.com/gallery/1", "imgur"},
		{"https://www.reddit.com/r/x/comments/1", "reddit"},
		{"https://movie.douban.com/subject/1/", "douban"},
		{"https://www.tumblr.com/x/1", "tumblr"},
		{"https://www.bilibili.tv/video/1", "bilibili"},
	}
	for _, c := range cases {
		if got := platformOf("", c.url); got != c.want {
			t.Errorf("platformOf(%q) = %q，期望 %q", c.url, got, c.want)
		}
	}
}

// TestPlatformOf_既有平台不受影响 修域名匹配不能误伤原有平台。
func TestPlatformOf_既有平台不受影响(t *testing.T) {
	cases := []struct{ url, want string }{
		{"https://www.youtube.com/watch?v=x", "youtube"},
		{"https://youtu.be/x", "youtube"},
		{"https://www.bilibili.com/video/BV1", "bilibili"},
		{"https://b23.tv/abc", "bilibili"},
		{"https://www.douyin.com/video/1", "douyin"},
		{"https://www.tiktok.com/@u/video/1", "tiktok"},
		{"https://music.163.com/song?id=1", "netease"},
		{"https://x.com/u/status/1", "twitter"},
		{"https://twitter.com/u/status/1", "twitter"},
		{"https://www.instagram.com/p/1", "instagram"},
		{"https://v.qq.com/x/cover/a/b.html", "tencent"},
		{"https://weixin.qq.com/s/abc", "wechat"},
		{"https://www.iqiyi.com/v_19rr7depxo.html", "iqiyi"},
		{"https://www.youku.com/v_show/id_x", "youku"},
		{"https://example.com/video.mp4", "web"},
	}
	for _, c := range cases {
		if got := platformOf("", c.url); got != c.want {
			t.Errorf("platformOf(%q) = %q，期望 %q", c.url, got, c.want)
		}
	}
}

// TestIsGalleryURL_不把无关域名误判成图站 同样的子串匹配坑。
func TestIsGalleryURL_不把无关域名误判成图站(t *testing.T) {
	for _, u := range []string{
		"https://max.com/gallery",
		"https://matrix.com/pic",
		"https://notx.com/gallery",
	} {
		if IsGalleryURL(u) {
			t.Errorf("%s 不应被判成图站（会路由到 gallery-dl）", u)
		}
	}
}

// TestIsGalleryURL_图站仍正确命中 修边界不能误伤真正的图站。
func TestIsGalleryURL_图站仍正确命中(t *testing.T) {
	for _, u := range []string{
		"https://www.zhihu.com/question/1",
		"https://xhslink.com/a/1",
		"https://x.com/user/status/1",
		"https://www.instagram.com/p/1",
		"https://www.pixiv.net/artworks/1",
	} {
		if !IsGalleryURL(u) {
			t.Errorf("%s 应被判成图站", u)
		}
	}
}
