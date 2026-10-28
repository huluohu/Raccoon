package engine

// imgproc_test.go：去黑边（cropdetect）+ 缩放。
//
// 用真实 ffmpeg 造一张「2.35:1 画面垫在 16:9 画布里」的图（上下黑边），
// 验证裁剪后黑边真的没了、画面高度变成内容高度。

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestTrimLetterbox_裁掉上下黑边(t *testing.T) {
	ffmpeg := Lookup("ffmpeg")
	ffprobe := Lookup("ffprobe")
	if ffmpeg == "" {
		t.Skip("本地无 ffmpeg，跳过去黑边测试")
	}
	if ffprobe == "" {
		t.Skip("本地无 ffprobe，跳过去黑边测试")
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "lb.jpg")
	out := filepath.Join(dir, "out.jpg")

	// 1280×544 的橙色画面垫进 1280×720 的黑底 → 上下各 88px 黑边
	if err := RunCmd(context.Background(), ffmpeg, []string{
		"-y", "-v", "error",
		"-f", "lavfi", "-i", "color=c=orange:s=1280x544",
		"-vf", "pad=1280:720:0:88:black",
		"-frames:v", "1", src,
	}, nil, ""); err != nil {
		t.Skipf("造测试图失败：%v", err)
	}

	ow, oh, ok := ImageSize(context.Background(), ffprobe, src)
	if !ok {
		t.Fatalf("读不到原图尺寸")
	}
	if ow != 1280 || oh != 720 {
		t.Fatalf("原图尺寸应为 1280x720，实际 %dx%d", ow, oh)
	}

	cw, ch, cx, cy, detected, err := DetectLetterbox(context.Background(), ffmpeg, src)
	if err != nil {
		t.Fatalf("检测黑边失败：%v", err)
	}
	if !detected {
		t.Fatal("应检测到上下黑边")
	}
	if cy != 88 || ch != 544 {
		t.Fatalf("检测到的裁剪区应为 544 高、纵移 88，实际 %d/%d", ch, cy)
	}
	if !worthTrimming(cw, ch, cx, cy, ow, oh) {
		t.Fatalf("裁剪区应判定为值得裁：%d×%d+%d+%d", cw, ch, cx, cy)
	}

	if err := TrimLetterbox(context.Background(), ffmpeg, ffprobe, src, out, 640); err != nil {
		t.Fatalf("去黑边失败：%v", err)
	}
	nw, nh, ok := ImageSize(context.Background(), ffprobe, out)
	if !ok {
		t.Fatal("读不到输出图尺寸")
	}
	// 黑边裁掉后高宽比接近 544/1280 = 0.425（16:9 是 0.5625）
	if nh*1280 > nw*544 {
		t.Fatalf("黑边应被裁掉：输出 %dx%d 仍含黑边", nw, nh)
	}
	if nw != 640 {
		t.Fatalf("输出宽度应为 640，实际 %d", nw)
	}
}

func TestTrimLetterbox_无黑边不动画面(t *testing.T) {
	ffmpeg := Lookup("ffmpeg")
	ffprobe := Lookup("ffprobe")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("本地无 ffmpeg/ffprobe，跳过")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "plain.jpg")
	out := filepath.Join(dir, "out.jpg")

	// 纯色画面：四边都不是黑的，cropdetect 不该报出裁剪区。
	// （别用 testsrc：那个图案左侧本来就有一条黑边，裁掉是对的）
	if err := RunCmd(context.Background(), ffmpeg, []string{
		"-y", "-v", "error",
		"-f", "lavfi", "-i", "color=c=orange:s=640x360",
		"-frames:v", "1", src,
	}, nil, ""); err != nil {
		t.Skipf("造测试图失败：%v", err)
	}
	if err := TrimLetterbox(context.Background(), ffmpeg, ffprobe, src, out, 320); err != nil {
		t.Fatalf("处理失败：%v", err)
	}
	nw, nh, ok := ImageSize(context.Background(), ffprobe, out)
	if !ok {
		t.Fatal("读不到输出图尺寸")
	}
	// 没有黑边 → 比例保持（320×180），不该被裁成别的比例
	if nw != 320 || nh != 180 {
		t.Fatalf("无黑边图片应保持比例，实际 %dx%d", nw, nh)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("输出文件不存在：%v", err)
	}
}
