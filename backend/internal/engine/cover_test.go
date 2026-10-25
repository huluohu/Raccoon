package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFormatClock(t *testing.T) {
	cases := []struct {
		sec  float64
		want string
	}{
		{0, "00:00"},
		{5, "00:05"},
		{65, "01:05"},
		{3661, "01:01:01"},
		{-3, "00:00"},
	}
	for _, tc := range cases {
		if got := formatClock(tc.sec); got != tc.want {
			t.Fatalf("formatClock(%v) = %q，期望 %q", tc.sec, got, tc.want)
		}
	}
}

func TestExtractCoverCandidates_无ffmpeg报明确错误(t *testing.T) {
	_, err := ExtractCoverCandidates(context.Background(), "", "/tmp/x.mp4", t.TempDir(), 100, 6)
	if err == nil {
		t.Fatal("无 ffmpeg 时应报错")
	}
	if got := err.Error(); got == "" {
		t.Fatal("错误应说明原因")
	}
}

func TestExtractCoverCandidates_时长未知报明确错误(t *testing.T) {
	ffmpeg := Lookup("ffmpeg")
	if ffmpeg == "" {
		t.Skip("本机无 ffmpeg，跳过")
	}
	_, err := ExtractCoverCandidates(context.Background(), ffmpeg, "/tmp/x.mp4", t.TempDir(), 0, 6)
	if err == nil {
		t.Fatal("时长未知时应报错")
	}
}

func TestExtractCoverCandidates_真实视频抽帧(t *testing.T) {
	ffmpeg := Lookup("ffmpeg")
	if ffmpeg == "" {
		t.Skip("本机无 ffmpeg，跳过")
	}
	dir := t.TempDir()
	video := filepath.Join(dir, "sample.mp4")
	// 造一个 3 秒带颜色变化的视频
	if err := RunCmd(context.Background(), ffmpeg, []string{
		"-y", "-f", "lavfi", "-i", "testsrc=size=320x240:rate=10:duration=3",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", video,
	}, nil, ""); err != nil {
		t.Skipf("生成测试视频失败: %v", err)
	}

	outDir := filepath.Join(dir, "cands")
	cands, err := ExtractCoverCandidates(context.Background(), ffmpeg, video, outDir, 3, 4)
	if err != nil {
		t.Fatalf("抽帧失败: %v", err)
	}
	if len(cands) == 0 {
		t.Fatal("应抽出至少一帧")
	}
	// 关键回归：ffmpeg 单帧输出必须带 -update 1，否则退出码非 0，
	// 抽帧会被当成失败跳过（实测：真实 webm 抽出 0 帧）。
	if len(cands) != 4 {
		t.Fatalf("4 个时间点应抽出 4 帧，实际 %d 帧", len(cands))
	}
	for i, c := range cands {
		if c.AtSec <= 0 || c.AtSec >= 3 {
			t.Fatalf("第 %d 帧时间点 %v 应落在视频时长内", i, c.AtSec)
		}
		if c.Label == "" {
			t.Fatalf("第 %d 帧应有时间标签", i)
		}
		st, err := os.Stat(c.Path)
		if err != nil || st.Size() == 0 {
			t.Fatalf("第 %d 帧文件应存在且非空", i)
		}
	}
}
