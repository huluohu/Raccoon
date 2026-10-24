package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestProbeFile_无工具降级 ffprobeBin 为空时直接返回零值，不报错、不阻断。
func TestProbeFile_无工具降级(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(f, []byte("fake"), 0o600); err != nil {
		t.Fatal(err)
	}
	res := ProbeFile(context.Background(), "", f)
	if res.Warn != "" {
		t.Fatalf("无工具时不应有 Warn，得到 %q", res.Warn)
	}
	if res.HasVideo || res.HasAudio {
		t.Fatal("无工具时 HasVideo/HasAudio 应为 false")
	}
}

// TestProbeFile_非媒体文件跳过 图片/字幕/元数据不应触发 ffprobe 校验。
func TestProbeFile_非媒体文件跳过(t *testing.T) {
	// ffprobeBin 传一个必然失败的值；若真调了会有 Warn
	for _, name := range []string{"cover.jpg", "sub.srt", "info.json", "desc.txt", "thumb.webp"} {
		res := ProbeFile(context.Background(), "/nonexistent/ffprobe", "/fake/"+name)
		if res.Warn != "" {
			t.Fatalf("非媒体文件 %s 不应触发 ffprobe，得到 Warn=%q", name, res.Warn)
		}
	}
}

// TestProbeFile_真实mp4文件 用系统 ffprobe（如果可用）校验 convert_test.go 创建的真实 mp3。
// 本机无 ffprobe 时跳过（不 skip 所有平台，只在环境可用时跑）。
func TestProbeFile_真实mp3文件(t *testing.T) {
	ffprobe := Lookup("ffprobe")
	if ffprobe == "" {
		t.Skip("ffprobe 不可用，跳过真实文件校验测试")
	}
	// 借 convert_test 的能力创建一个 1s 静音 mp3——最小真实媒体文件。
	ffmpeg := Lookup("ffmpeg")
	if ffmpeg == "" {
		t.Skip("ffmpeg 不可用，无法生成测试文件")
	}
	dir := t.TempDir()
	mp3 := filepath.Join(dir, "test.mp3")
	// 生成 1s 静音 mp3
	if err := RunCmd(context.Background(), ffmpeg,
		[]string{"-y", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono", "-t", "1", "-q:a", "2", mp3},
		nil, ""); err != nil {
		t.Skipf("生成测试 mp3 失败：%v", err)
	}
	res := ProbeFile(context.Background(), ffprobe, mp3)
	if res.Warn != "" {
		t.Fatalf("有效 mp3 不应有 Warn，得到 %q", res.Warn)
	}
	if !res.HasAudio {
		t.Fatal("有效 mp3 应有音频流")
	}
}

// TestProbeFile_工具执行失败返回Warn ffprobe 路径不可执行时 Warn 非空，不 panic。
func TestProbeFile_工具执行失败返回Warn(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(f, []byte("not a real video"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 用一个确定不存在的 ffprobe 路径；RunCmd 会返回 err，classify 包在 engine 层。
	res := ProbeFile(context.Background(), "/tmp/nonexistent_ffprobe_12345", f)
	if res.Warn == "" {
		t.Fatal("ffprobe 不可执行时应有 Warn")
	}
}

// ---- 孤儿文件清理 ----

// TestCleanOrphanFiles_只清新增的part文件 before 快照存在的文件不触碰。
func TestCleanOrphanFiles_只清新增的part文件(t *testing.T) {
	dir := t.TempDir()
	// 下载开始前已存在的文件（before 快照）
	old := filepath.Join(dir, "old.part")
	if err := os.WriteFile(old, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := map[string]bool{"old.part": true, "existing.mp4": true}

	// 下载后新产生的孤儿文件
	for _, name := range []string{"new.part", "video.ytdl", "chunk.part.ytdl"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("tmp"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// 下载成功的真实产物（不应被清理）
	good := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(good, []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}

	cleaned := CleanOrphanFiles(dir, before)
	if cleaned != 3 {
		t.Fatalf("期望清理 3 个孤儿文件，实际清理 %d", cleaned)
	}

	// old.part 不应被删
	if _, err := os.Stat(old); err != nil {
		t.Fatal("before 快照中的 old.part 不应被删除")
	}
	// 真实产物不应被删
	if _, err := os.Stat(good); err != nil {
		t.Fatal("真实产物 video.mp4 不应被删除")
	}
	// 孤儿文件应已删除
	for _, name := range []string{"new.part", "video.ytdl", "chunk.part.ytdl"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Fatalf("孤儿文件 %s 应已被清理", name)
		}
	}
}

// TestCleanOrphanFiles_目录不存在不panic 对不存在的目录调用不崩溃。
func TestCleanOrphanFiles_目录不存在不panic(t *testing.T) {
	n := CleanOrphanFiles("/nonexistent/dir/xyz", nil)
	if n != 0 {
		t.Fatalf("目录不存在时应返回 0，得到 %d", n)
	}
}

// TestCleanOrphanFiles_空目录返回0 空目录没有东西可清。
func TestCleanOrphanFiles_空目录返回0(t *testing.T) {
	dir := t.TempDir()
	if n := CleanOrphanFiles(dir, map[string]bool{}); n != 0 {
		t.Fatalf("空目录应返回 0，得到 %d", n)
	}
}
