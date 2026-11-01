package task

// continuity_test.go：解析预览 → 下载 的连续性。
//
// 2026-10-30 定位到的三处「预览与下载对不上」：
//
//  1. **列表条目数**：预览用 --playlist-items 1 只解析第一条，下载阶段却会
//     拉**整个列表**——用户看到「1 个视频」，点下去下了 25 个。修复是让
//     预览如实报出条目数（playlist_count），界面据此告知。
//  2. **体积口径**：预览报的是**视频流**体积，成品是「视频流 + 音轨」合并
//     （B 站 1080p：视频 66.58MB + 音频 5.16MB = 71.74MB）。数字对不上
//     会让用户以为下载被截断。修复是预览按成品口径报。
//  3. **静默降档**：解析与下载是两次独立引擎调用，站点的格式列表会变；
//     formatSelector 的 /b 兜底保证「宁可降档也别失败」，但用户不知道
//     自己选的档位没了。修复是如实告知。

import (
	"strings"
	"testing"

	"github.com/huluohu/raccoon/internal/engine"
)

// TestQualityFallbackNote_档位还在时不提示 正常路径不该有多余噪音。
func TestQualityFallbackNote_档位还在时不提示(t *testing.T) {
	formats := []engine.FormatInfo{
		{Quality: "1080p"}, {Quality: "720p"}, {Quality: "480p"},
	}
	if note := qualityFallbackNote("1080p", formats); note != "" {
		t.Errorf("档位仍在时不应提示，实际 %q", note)
	}
}

// TestQualityFallbackNote_档位消失时如实告知 站点格式列表变了（两次独立
// 引擎调用之间），用户选的档位没了——必须告知，否则「我要的 4K 变 1080p」
// 这件事只有用户自己猜。
func TestQualityFallbackNote_档位消失时如实告知(t *testing.T) {
	formats := []engine.FormatInfo{
		{Quality: "1080p"}, {Quality: "720p"}, {Quality: "480p"},
	}
	note := qualityFallbackNote("2160p", formats)
	if note == "" {
		t.Fatal("档位消失时应给出提示")
	}
	if !strings.Contains(note, "2160p") || !strings.Contains(note, "1080p") {
		t.Errorf("提示应点明「所选 2160p → 实得 1080p」，实际 %q", note)
	}
}

// TestQualityFallbackNote_有更高档位时不提示 站点给了更高档位是好事。
func TestQualityFallbackNote_有更高档位时不提示(t *testing.T) {
	formats := []engine.FormatInfo{
		{Quality: "2160p"}, {Quality: "1080p"},
	}
	if note := qualityFallbackNote("1080p", formats); note != "" {
		t.Errorf("有更高档位可用时不应提示，实际 %q", note)
	}
}

// TestQualityFallbackNote_best与audio不提示 这两个没有固定高度。
func TestQualityFallbackNote_best与audio不提示(t *testing.T) {
	formats := []engine.FormatInfo{{Quality: "1080p"}}
	for _, q := range []string{"best", "audio", ""} {
		if note := qualityFallbackNote(q, formats); note != "" {
			t.Errorf("quality=%q 不应提示，实际 %q", q, note)
		}
	}
}

// TestQualityFallbackNote_站点没给档位信息时不猜 没有信息就不下结论。
func TestQualityFallbackNote_站点没给档位信息时不猜(t *testing.T) {
	if note := qualityFallbackNote("2160p", nil); note != "" {
		t.Errorf("无档位信息时不应猜，实际 %q", note)
	}
}
