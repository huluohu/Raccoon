package engine

// imgproc.go：封面/缩略图的图像处理（去黑边 + 缩放）。
//
// 为什么要去黑边：平台给的缩略图常把 2.35:1 的画面垫在 16:9 画布里
//（上下两条黑边）。CSS 的 object-fit:cover 只能裁掉「画布之外」的部分，
// 裁不掉「画布之内、平台自己加的」黑边——于是列表里的小图看起来像「画面没适配」
//（2026-10-28 用户截图：任务列表里的缩略图上下各一条黑边）。
//
// 做法：ffmpeg 的 cropdetect 检测四周的近黑边缘 → 拿到 crop=W:H:X:Y → 裁 + 缩放。
//
// ⚠️ 两个实测踩过的坑：
//   1. cropdetect 默认 skip=2（跳过前两帧才开始分析），单帧输入时它一帧都不看、
//      什么也不报。必须显式 skip=0。
//   2. cropdetect 的结论只打在日志里（"crop=W:H:X:Y"），不在输出流上，
//      所以要从 stderr 里解析。

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// cropRe 匹配 cropdetect 输出的裁剪区：crop=宽:高:横移:纵移。
var cropRe = regexp.MustCompile(`crop=(\d+):(\d+):(\d+):(\d+)`)

// sizeRe 匹配 ffprobe 的 csv 输出：宽,高。
var sizeRe = regexp.MustCompile(`^\s*(\d+)\s*,\s*(\d+)\s*$`)

// letterbox 裁剪的合理性阈值（防止把「本来就暗」的画面裁坏）：
//   - 裁掉的面积太小（<5%）→ 多半是暗角，不该裁；
//   - 裁掉的面积太大（>70%）→ 多半是纯黑图，裁了会毁掉内容；
//   - 某一侧被削掉的比例太小（<4% 或 <8px）→ 抗锯齿/暗角，不算黑边。
//
// 比例阈值是必需的：cropdetect 只看「边是不是近黑」，测试图（testsrc）
// 这种自带暗边的正常图片也会被判成黑边——按像素数（4px）放行会把它们裁坏。
const (
	letterboxMinTrim = 0.05
	letterboxMaxTrim = 0.70
	letterboxMinEdge = 8
	letterboxMinFrac = 0.04
)

// DetectLetterbox 检测图片四周的黑边，返回裁剪区（w/h/x/y）。
//
// ok=false 表示「没有可裁的黑边」——调用方应保持原图。
// err 非空表示 ffmpeg 不可用或执行失败（调用方可以降级为「不处理」）。
func DetectLetterbox(ctx context.Context, ffmpegBin, path string) (w, h, x, y int, ok bool, err error) {
	if strings.TrimSpace(ffmpegBin) == "" {
		return 0, 0, 0, 0, false, fmt.Errorf("ffmpeg 不可用")
	}
	var out strings.Builder
	// -f null -：只解码不输出，cropdetect 的结论打在日志里。
	args := []string{
		"-hide_banner",
		"-i", path,
		"-vf", "cropdetect=round=2:reset=0:skip=0",
		"-f", "null", "-",
	}
	// 退出码非 0 不算失败（cropdetect 本身不产出画面），日志里有结论即可。
	_ = RunCmd(ctx, ffmpegBin, args, func(l string) { out.WriteString(l); out.WriteByte('\n') }, "")

	m := cropRe.FindStringSubmatch(out.String())
	if len(m) != 5 {
		return 0, 0, 0, 0, false, nil
	}
	cw, _ := strconv.Atoi(m[1])
	ch, _ := strconv.Atoi(m[2])
	cx, _ := strconv.Atoi(m[3])
	cy, _ := strconv.Atoi(m[4])
	if cw <= 0 || ch <= 0 {
		return 0, 0, 0, 0, false, nil
	}
	return cw, ch, cx, cy, true, nil
}

// ImageSize 读图片宽高（ffprobe；不可用时返回 false）。
func ImageSize(ctx context.Context, ffprobeBin, path string) (int, int, bool) {
	if strings.TrimSpace(ffprobeBin) == "" {
		return 0, 0, false
	}
	var out strings.Builder
	err := RunCmd(ctx, ffprobeBin, []string{
		"-v", "quiet",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "csv=p=0",
		path,
	}, func(l string) { out.WriteString(l); out.WriteByte('\n') }, "")
	if err != nil {
		return 0, 0, false
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if m := sizeRe.FindStringSubmatch(line); len(m) == 3 {
			w, _ := strconv.Atoi(m[1])
			h, _ := strconv.Atoi(m[2])
			if w > 0 && h > 0 {
				return w, h, true
			}
		}
	}
	return 0, 0, false
}

// TrimLetterbox 去黑边并把图片缩到 maxWidth 宽（maxWidth<=0 表示不缩放）。
//
// 流程：cropdetect 检黑边 → 合理性校验 → 有则裁 → 统一缩放（缩放对所有图都做，
// 免得前端拉到 4K 原图）。输出写 out（扩展名决定编码，.jpg 出 JPEG）。
//
// 检不到黑边不算错误：直接缩放原图即可。
func TrimLetterbox(ctx context.Context, ffmpegBin, ffprobeBin, in, out string, maxWidth int) error {
	if strings.TrimSpace(ffmpegBin) == "" {
		return fmt.Errorf("ffmpeg 不可用")
	}
	filters := make([]string, 0, 2)
	cw, ch, cx, cy, ok, err := DetectLetterbox(ctx, ffmpegBin, in)
	if err == nil && ok {
		ow, oh, hasSize := ImageSize(ctx, ffprobeBin, in)
		if hasSize && worthTrimming(cw, ch, cx, cy, ow, oh) {
			filters = append(filters, fmt.Sprintf("crop=%d:%d:%d:%d", cw, ch, cx, cy))
		}
	}
	if maxWidth > 0 {
		filters = append(filters, fmt.Sprintf("scale=%d:-2", maxWidth))
	}
	args := []string{"-y", "-hide_banner", "-i", in}
	if len(filters) > 0 {
		args = append(args, "-vf", strings.Join(filters, ","))
	}
	args = append(args, "-frames:v", "1", "-q:v", "3", out)
	return RunCmd(ctx, ffmpegBin, args, nil, "")
}

// worthTrimming 判断检测到的裁剪区是否值得裁（避免把「本来就暗」的画面裁坏）。
func worthTrimming(cw, ch, cx, cy, ow, oh int) bool {
	if ow <= 0 || oh <= 0 {
		return false
	}
	// 裁剪区必须在原图内。注意不能用「cw>=ow 就放弃」来判「没裁」——
	// 上下黑边的图裁剪后**宽度不变**（cw==ow），那样会把正常的去黑边误杀。
	if cw > ow || ch > oh || cx < 0 || cy < 0 || cx+cw > ow || cy+ch > oh {
		return false
	}
	trimmed := 1 - float64(cw*ch)/float64(ow*oh)
	if trimmed < letterboxMinTrim || trimmed > letterboxMaxTrim {
		return false
	}
	// 上下或左右至少有一侧被削掉足够多才算黑边。
	// 注意不能要求「两侧同时很薄才不算」——上下黑边的图，左右两侧本来就一点没削。
	removedTop := cy
	removedBottom := oh - cy - ch
	removedLeft := cx
	removedRight := ow - cx - cw
	thick := func(px, full int) bool {
		return px >= letterboxMinEdge && float64(px) >= letterboxMinFrac*float64(full)
	}
	horizontal := thick(removedTop, oh) || thick(removedBottom, oh)
	vertical := thick(removedLeft, ow) || thick(removedRight, ow)
	return horizontal || vertical
}
