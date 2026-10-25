package engine

// cover.go：视频封面抽取（ffmpeg 抽帧）。
//
// 为什么要自己抽帧而不只用平台封面：
//   - 平台封面（yt-dlp --write-thumbnail）不一定有，且质量/裁剪不可控；
//   - 用户常想要「视频里某一帧」而不是站点的固定图；
//   - 抽帧是纯本地操作，不依赖任何外部 API。
//
// 做法：按时长均分取 N 帧（跳过最前/最后的黑场），输出 jpg 供用户挑。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CoverCandidate 一张候选封面。
type CoverCandidate struct {
	// Path 图片绝对路径（临时文件，用户选中后才转为正式封面）。
	Path string `json:"path"`
	// AtSec 该帧在视频中的时间点（秒）。
	AtSec float64 `json:"at_sec"`
	// Label 人类可读的时间标签（如 "00:01:23"）。
	Label string `json:"label"`
}

// ExtractCoverCandidates 从视频里抽 N 帧候选封面。
//
// ffmpegBin 为空 → 返回明确错误（不假装成功）。
// durationSec <= 0 → 无法定位时间点，返回错误。
// outDir 为空时用系统临时目录。
func ExtractCoverCandidates(ctx context.Context, ffmpegBin, videoPath, outDir string, durationSec float64, n int) ([]CoverCandidate, error) {
	if strings.TrimSpace(ffmpegBin) == "" {
		return nil, fmt.Errorf("ffmpeg 不可用，无法抽取封面")
	}
	if durationSec <= 0 {
		return nil, fmt.Errorf("媒体时长未知，无法定位抽帧时间点")
	}
	if n <= 0 {
		n = 6
	}
	if n > 12 {
		n = 12
	}
	if outDir == "" {
		outDir = os.TempDir()
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))

	out := make([]CoverCandidate, 0, n)
	for i := 0; i < n; i++ {
		// 均分取点，跳过 5% 头尾（常见黑场/片尾）。
		ratio := 0.05 + 0.9*float64(i)/float64(n)
		at := durationSec * ratio
		dst := filepath.Join(outDir, fmt.Sprintf("%s.cover.%d.jpg", base, i))
		// -ss 放在 -i 前：快速定位（先 seek 再解码），比输出端 seek 快得多。
		// -update 1：image2 复用器默认要求文件名是序列模板（%03d），
		// 单帧输出必须显式声明「不是序列」，否则 ffmpeg 报
		// "The specified filename does not contain an image sequence pattern" 并返回非 0 退出码。
		args := []string{
			"-y", "-ss", formatSeconds(at), "-i", videoPath,
			"-frames:v", "1", "-q:v", "3", "-vf", "scale=640:-2", "-update", "1", dst,
		}
		if err := RunCmd(ctx, ffmpegBin, args, nil, ""); err != nil {
			// 单帧失败不终止整批：继续抽剩下的（用户少一个候选也好过没有）。
			continue
		}
		st, err := os.Stat(dst)
		if err != nil || st.Size() == 0 {
			continue
		}
		out = append(out, CoverCandidate{
			Path:  dst,
			AtSec: at,
			Label: formatClock(at),
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("未能从该视频抽取任何画面（文件可能损坏或编码不受支持）")
	}
	return out, nil
}

// formatSeconds 秒 → ffmpeg 接受的秒数串（保留 3 位小数）。
func formatSeconds(s float64) string {
	return strconv.FormatFloat(s, 'f', 3, 64)
}

// formatClock 秒 → mm:ss 或 hh:mm:ss。
func formatClock(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	total := int(sec)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}
