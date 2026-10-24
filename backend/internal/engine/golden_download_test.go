package engine

// golden_download_test.go：下载成功路径的 Golden Fixture 回放。
//
// 锁死的不变式（真实 yt-dlp 2026.08.19 录制，见 testdata/README.md）：
//
//	1) 合并场景下，Destination 行给出的是**分片**（.f401.mp4 / .f251.webm），
//	   真正的产物是 [Merger] 行里的合并文件（.webm）——
//	   合并产物从未出现在任何 Destination 行里；
//	2) 分片不能被当成产物收进媒体库（否则库里出现一堆 .f401.mp4 碎片，
//	   真正的合并文件反而缺失）；
//	3) 进度行的 percent 为 NA 时必须自算（引擎实测 percent 常为 NA）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGolden_合并产物识别 解析 [Merger] 行拿到真正的产物路径。
func TestGolden_合并产物识别(t *testing.T) {
	out := readFixture(t, "ytdlp_download_merge.stdout")
	merged := parseMergedPaths(out)
	if len(merged) != 1 {
		t.Fatalf("应解析出 1 个合并产物，得到 %d：%v", len(merged), merged)
	}
	if !strings.HasSuffix(merged[0], ".webm") {
		t.Fatalf("合并产物应为 .webm，得到 %q", merged[0])
	}
	if strings.Contains(merged[0], ".f401.") || strings.Contains(merged[0], ".f251.") {
		t.Fatalf("合并产物不应是分片：%q", merged[0])
	}
}

// TestGolden_分片不被当成产物 目录里同时有分片和合并产物时，
// 收集到的产物只能是合并文件。
func TestGolden_分片不被当成产物(t *testing.T) {
	out := readFixture(t, "ytdlp_download_merge.stdout")
	dir := t.TempDir()

	// 用 fixture 里的真实文件名（相对路径，collectFiles 会拼到 dir 下）
	base := "Rick Astley - Never Gonna Give You Up (Official Video) (4K Remaster) [dQw4w9WgXcQ]"
	fragVideo := filepath.Join(dir, base+".f401.mp4")
	fragAudio := filepath.Join(dir, base+".f251.webm")
	merged := filepath.Join(dir, base+".webm")
	// 分片残留（--keep-video 或合并失败时的真实状态）
	if err := os.WriteFile(fragVideo, []byte("v"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fragAudio, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(merged, []byte("merged-content"), 0o600); err != nil {
		t.Fatal(err)
	}

	before := map[string]bool{}
	destinations := parseDestinationPaths(out)
	files := collectFiles(destinations, before, dir, parseMergedPaths(out))

	if len(files) != 1 {
		t.Fatalf("应只收集到 1 个产物（合并文件），得到 %d：%+v", len(files), files)
	}
	if filepath.Base(files[0].Path) != filepath.Base(merged) {
		t.Fatalf("收集到的应是合并产物 %q，得到 %q", filepath.Base(merged), filepath.Base(files[0].Path))
	}
	if files[0].Size != int64(len("merged-content")) {
		t.Fatalf("产物大小应为 %d，得到 %d", len("merged-content"), files[0].Size)
	}
}

// TestGolden_无合并时收集Destination 普通单文件下载：Destination 就是产物。
func TestGolden_无合并时收集Destination(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(video, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	destinations := []string{"video.mp4"}
	files := collectFiles(destinations, map[string]bool{}, dir, nil)
	if len(files) != 1 {
		t.Fatalf("应收集到 1 个产物，得到 %d", len(files))
	}
	if filepath.Base(files[0].Path) != "video.mp4" {
		t.Fatalf("产物应为 video.mp4，得到 %q", filepath.Base(files[0].Path))
	}
}

// TestGolden_进度percent为NA时自算 引擎实测 percent 常输出 NA，
// 必须由 downloaded/total 自算，不能当 0。
func TestGolden_进度percent为NA时自算(t *testing.T) {
	out := readFixture(t, "ytdlp_download_merge.stdout")
	var last Progress
	var got bool
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, progressPrefix) {
			continue
		}
		p, ok := parseProgress(line)
		if !ok {
			t.Fatalf("进度行应能解析：%q", line)
		}
		last = p
		got = true
	}
	if !got {
		t.Fatal("fixture 里应有进度行")
	}
	if last.Percent < 0 {
		t.Fatalf("percent 为 NA 时应自算出非负值，得到 %v", last.Percent)
	}
	if last.Total <= 0 {
		t.Fatalf("total 应被解析出来，得到 %d", last.Total)
	}
	if last.Downloaded <= 0 {
		t.Fatalf("downloaded 应被解析出来，得到 %d", last.Downloaded)
	}
}

// TestGolden_已存在文件的提示行 从真实输出里识别「has already been downloaded」。
func TestGolden_已存在文件的提示行(t *testing.T) {
	line := "[download] /path/to/video.mp4 has already been downloaded"
	if got := alreadyDownloadedPath(line); got != "/path/to/video.mp4" {
		t.Fatalf("应提取出路径，得到 %q", got)
	}
	line2 := "[download] /path/to/video.mkv has already been downloaded and merged"
	if got := alreadyDownloadedPath(line2); got != "/path/to/video.mkv" {
		t.Fatalf("应提取出路径（去掉 and merged 后缀），得到 %q", got)
	}
}
