package engine

// routing.go：URL → 引擎/平台 路由（docs/08 W4.2/W4.3）。
//
// 路由原则：
//   - 按「内容形态」而非「平台名单」判断（图片站 vs 音视频站）；
//   - 判不准一律回 yt-dlp（extractor 覆盖更广，回退不致任务直接失败）；
//   - 平台标识仅用于 UI 徽章与调度分组，不参与能力判定。
//
// 这三条是硬约束：早期版本把「平台名单」写死成白名单，结果新站点一律落到 web 组，
// 既无法显示正确徽章，也拿不到平台级并发隔离。

import "strings"

// galleryHosts 走 gallery-dl 的站点域名片段（小写）。
//
// 覆盖的是「以图片/图集为主体」的站点。刻意不含视频站：yt-dlp 对视频站的
// extractor 更全，误路由到 gallery-dl 会让本可下载的任务直接失败。
var galleryHosts = []string{
	"xiaohongshu.com", // 小红书
	"xhslink.com",     // 小红书短链
	"x.com",           // X（原 Twitter）
	"twitter.com",     // X 旧域名
	"instagram.com",   // Instagram
	"tumblr.com",      // Tumblr
	"weibo.com",       // 微博
	"zhihu.com",       // 知乎
	"douban.com",      // 豆瓣
	"pixiv.net",       // Pixiv
	"artstation.com",  // ArtStation
	"deviantart.com",  // DeviantArt
	"imgur.com",       // Imgur
	"reddit.com",      // Reddit
	"9gag.com",        // 9GAG
}

// IsGalleryURL 判断 URL 是否应由 gallery-dl 处理。
//
// 判定顺序：先看主机名（精确片段匹配），再看路径特征（/user/、/post/ 等
// 「单页多图」的路径模式）。主机名命中即返回，不做路径推断。
func IsGalleryURL(rawURL string) bool {
	host := hostOf(rawURL)
	if host == "" {
		return false
	}
	for _, h := range galleryHosts {
		if strings.Contains(host, h) {
			return true
		}
	}
	return false
}

// hostOf 提取 URL 主机名（小写）；非 http(s) 或解析失败返回空串。
func hostOf(rawURL string) string {
	s := strings.TrimSpace(rawURL)
	if s == "" {
		return ""
	}
	s = strings.TrimPrefix(s, "magnet:")
	s = strings.TrimPrefix(s, "btih:")
	lower := strings.ToLower(s)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return ""
	}
	rest := s[strings.Index(s, "://")+3:]
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.Index(rest, "@"); i >= 0 { // 去掉 userinfo
		rest = rest[i+1:]
	}
	if i := strings.Index(rest, ":"); i >= 0 { // 去掉端口
		rest = rest[:i]
	}
	return strings.ToLower(rest)
}

// IsMagnet 判断是否为 magnet 链接（磁力任务走 qB 通道，不进下载引擎）。
func IsMagnet(rawURL string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(rawURL)), "magnet:")
}

// musicHosts 音乐平台域名 → 平台标识。
//
// 说明：Apple Music 与 YT Music 的内容 yt-dlp 已能解析（其 extractor 覆盖这两个站），
// 因此它们不需要独立引擎，只需要正确的平台标识（UI 徽章 + 调度分组）。
// 网易云音乐 yt-dlp 无可用 extractor，须走独立通道（见 docs/13 音乐平台说明）。
var musicHosts = map[string]string{
	"music.163.com":        "netease",
	"music.apple.com":      "applemusic",
	"itunes.apple.com":     "applemusic",
	"music.youtube.com":    "ytmusic",
	"youtube.com/playlist": "ytmusic",
}

// MusicPlatform 返回 URL 所属音乐平台标识；非音乐平台返回空串。
func MusicPlatform(rawURL string) string {
	host := hostOf(rawURL)
	if host == "" {
		return ""
	}
	if p, ok := musicHosts[host]; ok {
		return p
	}
	// music.youtube.com 走 musicHosts 已覆盖；music.youtube.com 的 watch?v= 亦命中。
	for h, p := range musicHosts {
		if strings.HasPrefix(h, "youtube.com/") && strings.Contains(host, h) {
			return p
		}
	}
	return ""
}
