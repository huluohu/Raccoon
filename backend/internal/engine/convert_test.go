package engine

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestConvertSupported(t *testing.T) {
	for _, ok := range []string{"mp4", "mp3"} {
		if !ConvertSupported(ok) {
			t.Errorf("%s 应受支持", ok)
		}
	}
	for _, bad := range []string{"", "avi", "mkv", "mp4 -evil", "../mp4"} {
		if ConvertSupported(bad) {
			t.Errorf("%q 不应受支持", bad)
		}
	}
}

func TestConvertArgs(t *testing.T) {
	// mp4 remux：流复制 + faststart。
	got := convertArgs("/a/b.mkv", "/a/b.mp4", "mp4", false)
	want := []string{"-y", "-i", "/a/b.mkv", "-c", "copy", "-movflags", "+faststart", "/a/b.mp4"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("remux 参数不符: %v", got)
	}
	// mp4 转码：H.264/AAC。
	got = convertArgs("/a/b.webm", "/a/b.mp4", "mp4", true)
	if !strings.Contains(strings.Join(got, " "), "libx264") || !strings.Contains(strings.Join(got, " "), "aac") {
		t.Errorf("转码参数缺编码器: %v", got)
	}
	// mp3：去视频轨 + libmp3lame。
	got = convertArgs("/a/b.mkv", "/a/b.mp3", "mp3", false)
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "-vn") || !strings.Contains(joined, "libmp3lame") {
		t.Errorf("mp3 参数不符: %v", got)
	}
}

// fakeFFmpeg 造一个假 ffmpeg：把最后一个参数当输出文件 touch 出来。
// 让「参数组装 → 进程执行 → 产物校验」的整条链路在无真 ffmpeg 的环境也可测。
// 对 .mp4 目标第一次调用失败（模拟 remux 编解码不兼容），第二次（转码）成功。
func fakeFFmpeg(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "ffmpeg")
	body := "#!/bin/sh\nCOUNT_F=\"$0.count\"\nC=$(cat \"$COUNT_F\" 2>/dev/null || echo 0)\nC=$((C+1)); echo $C > \"$COUNT_F\"\nLAST=\"\"\nfor a in \"$@\"; do LAST=\"$a\"; done\ncase \"$LAST\" in *.mp4) if [ \"$C\" = \"1\" ]; then exit 1; fi;; esac\nprintf fake-media > \"$LAST\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

func TestConvertFile_Mp4RemuxFallbackTranscode(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "video.mkv")
	_ = os.WriteFile(src, []byte("fake-mkv"), 0o644)
	dst, mode, err := ConvertFile(context.Background(), fakeFFmpeg(t), src, "mp4")
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	if mode != "transcode" {
		t.Fatalf("首跑 remux 失败应降级转码，实际 mode=%q", mode)
	}
	if dst != filepath.Join(dir, "video.mp4") {
		t.Fatalf("产物路径不符: %s", dst)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("产物应存在: %v", err)
	}
}

func TestConvertFile_AlreadyTarget(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "video.mp4")
	_ = os.WriteFile(src, []byte("x"), 0o644)
	dst, mode, err := ConvertFile(context.Background(), fakeFFmpeg(t), src, "mp4")
	if err != nil || mode != "skip" || dst != src {
		t.Fatalf("同格式应跳过: dst=%q mode=%q err=%v", dst, mode, err)
	}
}

func TestConvertFile_Mp3(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "video.mkv")
	_ = os.WriteFile(src, []byte("fake"), 0o644)
	dst, mode, err := ConvertFile(context.Background(), fakeFFmpeg(t), src, "mp3")
	if err != nil || mode != "audio-extract" {
		t.Fatalf("mp3 提取失败: mode=%q err=%v", mode, err)
	}
	if !strings.HasSuffix(dst, ".mp3") {
		t.Fatalf("产物应为 mp3: %s", dst)
	}
}

func TestConvertFile_EmptyOutputRejected(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "video.mkv")
	_ = os.WriteFile(src, []byte("fake"), 0o644)
	// 假 ffmpeg 什么都不产出：退出码 0 也不算成功。
	script := filepath.Join(t.TempDir(), "ffmpeg")
	_ = os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	if _, _, err := ConvertFile(context.Background(), script, src, "mp4"); err == nil {
		t.Fatal("产物缺失应报错（不造假成功）")
	}
}
