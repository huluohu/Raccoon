package engine

// engine_test.go：URL 路由与引擎选型的单元测试。
//
// 为什么这些必须锁死：
//   - IsGalleryURL 决定「走 gallery-dl 还是 yt-dlp」，判错会直接让任务失败
//     （gallery-dl 对视频站没有 extractor，yt-dlp 对图集站覆盖不全）；
//   - hostOf 是所有域名判定的地基：端口 / userinfo / 大小写任一处理错，
//     后面所有 Contains 判定都会漂；
//   - parseGalleryProgress 的 total=0 必须保持 Percent=-1 —— 禁估算，
//     拿 0 当 0% 会让 UI 显示「已完成」。

import "testing"

// TestIsGalleryURL_图片站判定 图片/图集类站点归 gallery-dl。
func TestIsGalleryURL_图片站判定(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"https://www.xiaohongshu.com/explore/123", true},
		{"https://xhslink.com/abc/", true},
		{"https://x.com/user/status/1", true},
		{"https://twitter.com/someone/status/1", true},
		{"https://www.instagram.com/p/ABC/", true},
		{"https://www.pixiv.net/artworks/12345", true},
		{"https://weibo.com/0/0", true},
		{"https://www.reddit.com/r/golang/", true},
		// 视频/音频站一律不归 gallery-dl（yt-dlp 的 extractor 更全）。
		{"https://www.youtube.com/watch?v=abc", false},
		{"https://www.bilibili.com/video/BV1xx", false},
		{"https://music.163.com/song?id=123", false},
		{"https://music.apple.com/cn/album/1", false},
		// 非 http(s) 协议：hostOf 返回空 → 判不定 → 不走 gallery-dl。
		{"magnet:?xt=urn:btih:abcdef", false},
		{"ftp://example.com/x.jpg", false},
		{"file:///etc/passwd", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsGalleryURL(c.url); got != c.want {
			t.Errorf("IsGalleryURL(%q) = %v，期望 %v", c.url, got, c.want)
		}
	}
}

// TestHostOf_域名提取 域名提取必须剥离端口、userinfo、路径、查询串并统一小写。
func TestHostOf_域名提取(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"https://example.com/a/b", "example.com"},
		{"https://example.com:8443/a", "example.com"},
		{"https://user:pass@example.com/a", "example.com"},
		{"https://user:pass@example.com:8443/a", "example.com"},
		{"https://example.com/a?b=c&d=e", "example.com"},
		{"https://example.com/a#frag", "example.com"},
		{"HTTPS://EXAMPLE.COM/Path", "example.com"},
		{"https://Example.COM", "example.com"},
		{"http://example.com", "example.com"},
		// 非 http(s) 与空串 → 空。
		{"", ""},
		{"   ", ""},
		{"magnet:?xt=urn:btih:abcdef", ""},
		{"btih:abcdef", ""},
		{"example.com/a", ""},
	}
	for _, c := range cases {
		if got := hostOf(c.raw); got != c.want {
			t.Errorf("hostOf(%q) = %q，期望 %q", c.raw, got, c.want)
		}
	}
}

// TestIsMagnet_磁力链接判定 磁力任务走 qB 通道，不进下载引擎。
func TestIsMagnet_磁力链接判定(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"magnet:?xt=urn:btih:0123456789abcdef", true},
		{"MAGNET:?xt=urn:btih:0123456789abcdef", true},
		{"  magnet:?xt=urn:btih:0123456789abcdef  ", true},
		{"magnet:?dn=test&xt=urn:btih:0123456789abcdef", true},
		{"http://example.com/x", false},
		{"https://example.com/x.torrent", false},
		{"", false},
		{" btih:0123456789abcdef", false},
	}
	for _, c := range cases {
		if got := IsMagnet(c.raw); got != c.want {
			t.Errorf("IsMagnet(%q) = %v，期望 %v", c.raw, got, c.want)
		}
	}
}

// TestMusicPlatform_音乐平台标识 平台标识只用于 UI 徽章与调度分组。
func TestMusicPlatform_音乐平台标识(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"https://music.163.com/song?id=123", "netease"},
		{"https://music.apple.com/cn/album/1", "applemusic"},
		{"https://itunes.apple.com/cn/album/1", "applemusic"},
		{"https://music.youtube.com/watch?v=abc", "ytmusic"},
		// 非音乐站 → 空串。
		{"https://www.bilibili.com/video/BV1xx", ""},
		{"https://www.youtube.com/watch?v=abc", ""},
		{"https://www.163.com/", ""},
		{"magnet:?xt=urn:btih:abc", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := MusicPlatform(c.raw); got != c.want {
			t.Errorf("MusicPlatform(%q) = %q，期望 %q", c.raw, got, c.want)
		}
	}
}

// TestEngineFor_按内容形态选引擎 图片站 → gallery-dl，其余 → yt-dlp。
//
// 这里显式构造 Set（不走 NewSet）是为了不依赖本机是否装了 yt-dlp/gallery-dl，
// 测试只关心路由结果，不关心二进制是否存在。
func TestEngineFor_按内容形态选引擎(t *testing.T) {
	s := &Set{
		YTDLP:     NewYTDLP("yt-dlp", "ffmpeg"),
		GalleryDL: NewGalleryDL("gallery-dl"),
	}
	cases := []struct {
		url string
		g   bool // 期望走 gallery-dl
	}{
		{"https://www.xiaohongshu.com/explore/1", true},
		{"https://x.com/user/status/1", true},
		{"https://www.pixiv.net/artworks/1", true},
		{"https://www.youtube.com/watch?v=abc", false},
		{"https://www.bilibili.com/video/BV1xx", false},
		{"https://example.com/unknown", false},
		{"magnet:?xt=urn:btih:abc", false},
	}
	for _, c := range cases {
		got := s.EngineFor(c.url)
		if c.g {
			if got != Engine(s.GalleryDL) {
				t.Errorf("EngineFor(%q) 应走 gallery-dl，实际 %T", c.url, got)
			}
		} else {
			if got != Engine(s.YTDLP) {
				t.Errorf("EngineFor(%q) 应走 yt-dlp，实际 %T", c.url, got)
			}
		}
	}
}

// TestPlatformGuess_URL推断平台 未解析出 extractor 时的平台兜底。
func TestPlatformGuess_URL推断平台(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"https://www.bilibili.com/video/BV1xx", "bilibili"},
		{"https://b23.tv/abc", "bilibili"},
		{"https://www.youtube.com/watch?v=abc", "youtube"},
		{"https://youtu.be/abc", "youtube"},
		{"https://music.163.com/song?id=1", "netease"},
		{"https://example.com/whatever", "web"},
	}
	for _, c := range cases {
		if got := PlatformGuess(c.raw); got != c.want {
			t.Errorf("PlatformGuess(%q) = %q，期望 %q", c.raw, got, c.want)
		}
	}
}

// TestParseGalleryProgress_进度解析 gallery-dl 的进度行格式与 yt-dlp 不同，
// 单独解析；total 未知时 percent 必须保持 -1（禁估算）。
func TestParseGalleryProgress_进度解析(t *testing.T) {
	p, ok := parseGalleryProgress("PROGRESS|1000|250|2048|30")
	if !ok {
		t.Fatal("合法进度行应解析成功")
	}
	if p.Total != 1000 {
		t.Errorf("Total = %d，期望 1000", p.Total)
	}
	if p.Downloaded != 250 {
		t.Errorf("Downloaded = %d，期望 250", p.Downloaded)
	}
	if p.SpeedBps != 2048 {
		t.Errorf("SpeedBps = %d，期望 2048", p.SpeedBps)
	}
	if p.ETASec != 30 {
		t.Errorf("ETASec = %d，期望 30", p.ETASec)
	}
	if p.Percent != 25 {
		t.Errorf("Percent = %v，期望 25", p.Percent)
	}
}

// TestParseGalleryProgress_总量未知时禁估算 total=0 时 percent 必须是 -1，
// 不能当成 0%（否则 UI 会显示「已完成」）。
func TestParseGalleryProgress_总量未知时禁估算(t *testing.T) {
	p, ok := parseGalleryProgress("PROGRESS|0|0|0|0")
	if !ok {
		t.Fatal("合法进度行应解析成功")
	}
	if p.Total != 0 {
		t.Errorf("Total = %d，期望 0", p.Total)
	}
	if p.Percent != -1 {
		t.Errorf("total=0 时 Percent 应为 -1（未知），实际 %v", p.Percent)
	}
}

// TestParseGalleryProgress_非法行拒绝 段数不对的行必须判否，不得解析出脏进度。
func TestParseGalleryProgress_非法行拒绝(t *testing.T) {
	bad := []string{
		"",
		"PROGRESS|1000|250",
		"PROGRESS|1000|250|2048",
		"PROGRESS|1000|250|2048|30|extra",
		"1000|250|2048|30",
	}
	for _, line := range bad {
		if p, ok := parseGalleryProgress(line); ok {
			t.Errorf("非法进度行 %q 不应解析成功，实际得到 %+v", line, p)
		}
	}
}

// TestParseGalleryProgress_非数字字段按零处理 NA/空值不得让整行失败。
func TestParseGalleryProgress_非数字字段按零处理(t *testing.T) {
	p, ok := parseGalleryProgress("PROGRESS|NA|NA|NA|NA")
	if !ok {
		t.Fatal("字段为 NA 的进度行应解析成功（按 0 处理）")
	}
	if p.Total != 0 || p.Downloaded != 0 {
		t.Errorf("NA 字段应按 0 处理，实际 Total=%d Downloaded=%d", p.Total, p.Downloaded)
	}
	if p.Percent != -1 {
		t.Errorf("总量未知时 Percent 应为 -1，实际 %v", p.Percent)
	}
}

// TestPlatformOf_extractor优先 解析成功后以 extractor 为准，覆盖 URL 兜底。
func TestPlatformOf_extractor优先(t *testing.T) {
	cases := []struct {
		extractor string
		url       string
		want      string
	}{
		{"Youtube", "https://example.com/x", "youtube"},
		{"BiliBili", "https://example.com/x", "bilibili"},
		{"TikTok", "https://example.com/x", "tiktok"},
		{"Twitter", "https://example.com/x", "twitter"},
		{"Instagram", "https://example.com/x", "instagram"},
		{"Netease", "https://example.com/x", "netease"},
		// extractor 与 URL 冲突时以 extractor 为准。
		{"Youtube", "https://www.bilibili.com/video/BV1", "youtube"},
	}
	for _, c := range cases {
		if got := platformOf(c.extractor, c.url); got != c.want {
			t.Errorf("platformOf(%q, %q) = %q，期望 %q", c.extractor, c.url, got, c.want)
		}
	}
}

// TestPlatformOf_URL域名兜底 extractor 缺失时按 URL 域名推断。
func TestPlatformOf_URL域名兜底(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"https://www.bilibili.com/video/BV1", "bilibili"},
		{"https://b23.tv/abc", "bilibili"},
		{"https://www.youtube.com/watch?v=1", "youtube"},
		{"https://youtu.be/abc", "youtube"},
		{"https://music.163.com/song?id=1", "netease"},
		{"https://twitter.com/a/status/1", "twitter"},
		{"https://x.com/a/status/1", "twitter"},
		{"https://www.instagram.com/p/1", "instagram"},
		{"https://example.com/unknown", "web"},
		{"", "web"},
	}
	for _, c := range cases {
		if got := platformOf("", c.url); got != c.want {
			t.Errorf("platformOf(\"\", %q) = %q，期望 %q", c.url, got, c.want)
		}
	}
}
