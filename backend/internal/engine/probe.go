package engine

// probe.go：产物完整性校验 + 下载失败后孤儿文件清理。
//
// 设计取舍（docs/05 §4）：
//   - ffprobe 仅校验视频/音频产物；图片/字幕/元数据不需要。
//   - 校验结果为 warn 级别（不影响任务 done），但写进事件流便于排障。
//     理由：极罕见情况 ffprobe 本身不准确（某些直播流容器信息缺失），
//     直接把任务打成 failed 代价过大；warn + 详情留档才是合适的粒度。
//   - 孤儿清理只清 .part / .ytdl 后缀（yt-dlp 的临时产物），不碰其他文件。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ProbeResult ffprobe 对单文件的检查结果。
type ProbeResult struct {
	Path     string
	Duration float64 // 秒；0 表示未知或流媒体
	HasVideo bool
	HasAudio bool
	// Warn 非空表示校验发现问题（任务仍可 done，但要记事件）。
	Warn string
}

// ProbeFile 用 ffprobe 校验媒体文件基础完整性：
//   - ffprobeBin 为空时直接返回"不校验"（无 ffprobe 降级）。
//   - 不校验字幕 / 图片 / 元数据文件（kind != video && kind != audio 直接跳过）。
//   - 校验失败只记 Warn，不返回 error（调用方决定是否影响任务状态）。
func ProbeFile(ctx context.Context, ffprobeBin, path string) ProbeResult {
	res := ProbeResult{Path: path}
	ext := strings.ToLower(filepath.Ext(path))
	if !isMediaExt(ext) {
		return res // 非媒体文件不校验
	}
	if ffprobeBin == "" {
		return res // 无工具降级
	}

	// ffprobe -v quiet -print_format json -show_format -show_streams
	// 只关心两件事：duration 不为 0，以及至少有一条可用流（video 或 audio）。
	var raw strings.Builder
	err := RunCmd(ctx, ffprobeBin, []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	}, func(l string) {
		raw.WriteString(l)
		raw.WriteByte('\n')
	}, "")
	if err != nil {
		res.Warn = fmt.Sprintf("ffprobe 执行失败：%v", err)
		return res
	}

	var out struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
		} `json:"streams"`
	}
	if jsonErr := json.Unmarshal([]byte(raw.String()), &out); jsonErr != nil {
		res.Warn = "ffprobe 输出解析失败：" + jsonErr.Error()
		return res
	}

	for _, s := range out.Streams {
		switch s.CodecType {
		case "video":
			res.HasVideo = true
		case "audio":
			res.HasAudio = true
		}
	}

	// 流媒体录制产物 duration 可能为 0 或缺失——不把它当问题。
	// 真正有问题的是「连流都没有」（容器为空 / 完全损坏）。
	if !res.HasVideo && !res.HasAudio {
		res.Warn = "产物文件不含任何可识别的音视频流（可能已损坏）"
	}
	return res
}

// CleanOrphanFiles 清理目录内由 yt-dlp 下载失败留下的临时文件
// （.part / .ytdl / .tmp 后缀）。只清 before 快照之后新增的文件，
// 不触碰下载开始前已存在的文件。
//
// 调用场景：引擎 Download 返回 error 时，worker 在 handleEngineErr 之前调用，
// 避免孤儿文件无限累积。
func CleanOrphanFiles(dir string, before map[string]bool) int {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var cleaned int
	for _, e := range ents {
		if e.IsDir() || before[e.Name()] {
			continue
		}
		name := e.Name()
		if isOrphanExt(name) {
			_ = os.Remove(filepath.Join(dir, name))
			cleaned++
		}
	}
	return cleaned
}

// isMediaExt 需要 ffprobe 校验的媒体扩展名。
func isMediaExt(ext string) bool {
	switch ext {
	case ".mp4", ".mkv", ".webm", ".mov", ".flv", ".ts", ".m4s", ".avi",
		".mp3", ".m4a", ".aac", ".flac", ".opus", ".wav", ".ogg":
		return true
	}
	return false
}

// isOrphanExt yt-dlp 下载中途失败留下的临时文件后缀。
func isOrphanExt(name string) bool {
	return strings.HasSuffix(name, ".part") ||
		strings.HasSuffix(name, ".ytdl") ||
		strings.HasSuffix(name, ".part.ytdl")
}

// DirSnapshot 记录目录当前文件集（快照），供 CleanOrphanFiles 用于判定
// 哪些文件是本次下载开始后新增的。对外暴露以便 worker 在 Download 前取快照。
func DirSnapshot(dir string) map[string]bool {
	return dirSnapshot(dir)
}
