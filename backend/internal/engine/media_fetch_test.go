package engine

// media_fetch_test.go：媒体直取（ffmpeg 拉 HLS/DASH 清单）。
//
// 关键不变式（2026-10-28 实测 bad.news 得出）：
// 1. Referer 必须指向**媒体源自己**，不是来源页——来源页的 Referer 会被媒体 CDN
//    的防盗链策略拒绝（403）。
// 2. 只有清单型媒体（HLS/DASH）走这条路；普通直链不该绕（会丢进度与断点续传）。
// 3. 进度要报「已写入字节数」：直取时媒体时长常常未知，百分比算不出来，
//    但字节数是实的——不然用户看到的是一条不动的进度条。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsManifestURL(t *testing.T) {
	cases := map[string]bool{
		"https://video.twimg.com/amplify_video/211/pl/avc1/720x1280/P0YoT.mp4.m3u8": true,
		"https://example.com/stream.m3u8":                                           true,
		"https://example.com/stream.mpd":                                            true,
		"https://video.twimg.com/amplify_video/211/pl/avc1/720x1280/x.m3u":          true,
		"https://example.com/video.mp4":                                             false,
		"https://example.com/photo.jpg":                                             false,
		"https://example.com/index.html":                                            false,
	}
	for raw, want := range cases {
		if got := IsManifestURL(raw); got != want {
			t.Errorf("IsManifestURL(%q) = %v，期望 %v", raw, got, want)
		}
	}
}

func TestParseFFmpegTime(t *testing.T) {
	line := `frame=  120 fps=25 q=28.0 size=    1024kB time=00:00:04.00 bitrate=2097.2kbits/s speed=1.02x`
	sec, ok := ParseFFmpegTime(line)
	if !ok || sec != 4 {
		t.Fatalf("应解析出 4 秒，实际 %.2f（ok=%v）", sec, ok)
	}
	if _, ok := ParseFFmpegTime("没有进度的行"); ok {
		t.Fatal("无关的行不应解析出时间")
	}
}

func TestParseFFmpegSize(t *testing.T) {
	line := `frame=  120 fps=25 q=28.0 size=    1024kB time=00:00:04.00 bitrate=2097.2kbits/s speed=1.02x`
	size, ok := ParseFFmpegSize(line)
	if !ok || size != 1024*1024 {
		t.Fatalf("应解析出 1MiB，实际 %d（ok=%v）", size, ok)
	}
}

func TestMediaOrigin(t *testing.T) {
	if got := mediaOrigin("https://video.twimg.com/amplify_video/x/pl/avc1/y.m3u8"); got != "https://video.twimg.com" {
		t.Fatalf("媒体源应为 https://video.twimg.com，实际 %q", got)
	}
	if got := mediaOrigin("不是 URL"); got != "" {
		t.Fatalf("非法 URL 应返回空串，实际 %q", got)
	}
}

// TestFetchMediaDirect_Referer指向媒体源 真正的关键：ffmpeg 的 -headers 里
// 必须是**媒体源自己**。用假 ffmpeg 脚本把 argv 落盘来断言。
func TestFetchMediaDirect_Referer指向媒体源(t *testing.T) {
	ffmpeg := Lookup("ffmpeg")
	if ffmpeg == "" {
		t.Skip("本地无 ffmpeg，跳过")
	}
	dir := t.TempDir()
	recorder := filepath.Join(dir, "ff")
	argsFile := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$FAKE_ARGS\"\n" +
		"for a in \"$@\"; do :; done\n" +
		// 模拟 ffmpeg 产出：把最后一个参数（输出路径）写成非空文件
		"eval last=\\${$#}; eval \"printf 'x' > \\$last\"\n"
	if err := os.WriteFile(recorder, []byte(script), 0o755); err != nil {
		t.Fatalf("写假 ffmpeg 失败：%v", err)
	}
	t.Setenv("FAKE_ARGS", argsFile)

	dst := filepath.Join(dir, "out.mp4")
	err := FetchMediaDirect(context.Background(), recorder, []MediaSource{
		{URL: "https://video.twimg.com/amplify_video/211/pl/avc1/720x1280/x.m3u8"},
		{URL: "https://video.twimg.com/amplify_video/211/pl/mp4a/128000/y.m3u8", Audio: true},
	}, dst, 0, nil)
	if err != nil {
		t.Fatalf("直取失败：%v", err)
	}
	raw, rerr := os.ReadFile(argsFile)
	if rerr != nil {
		t.Fatalf("读参数失败：%v", rerr)
	}
	args := string(raw)
	if !strings.Contains(args, "Referer: https://video.twimg.com/") {
		t.Fatalf("Referer 必须指向媒体源自己，实际参数：%q", args)
	}
	if strings.Contains(args, "bad.news") {
		t.Fatalf("Referer 不得指向来源页，实际参数：%q", args)
	}
	// 假脚本按行写参数，这里按「行」判定（参数是一行一个的）
	has := func(s string) bool {
		for _, line := range strings.Split(args, "\n") {
			if strings.TrimSpace(line) == s {
				return true
			}
		}
		return false
	}
	if !has("-map") || !has("0:v:0") || !has("1:a:0") {
		t.Fatalf("视频流与音轨都应被映射，实际参数：%q", args)
	}
	if !has("-c") || !has("copy") {
		t.Fatalf("应为流复制（不重编码），实际参数：%q", args)
	}
}
