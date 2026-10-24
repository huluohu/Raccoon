package engine

// convert.go：产物格式转换（docs/05 §4 post 阶段动作；convert_to = mp4 / mp3）。
//
// 策略：MP4 目标先 remux（-c copy，容器与编解码兼容时无损、秒级完成），
// remux 失败（如 webm/vp9 无法直进 mp4）再降级为 H.264 转码；MP3 目标
// 固定 libmp3lame 提取音轨。转换失败不推翻下载成功的事实——调用方保留
// 源格式文件并在事件流里记 warn，用户拿到的是「原格式成品」而不是失败任务。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConvertSupported 目标格式是否在支持面内（白名单防拼参数注入）。
func ConvertSupported(target string) bool {
	return target == "mp4" || target == "mp3"
}

// ConvertFile 把 srcPath 转成 target 格式，返回产物路径与转换方式
// （remux / transcode / audio-extract）。src 已是目标格式时原样返回（mode=skip）。
//
// 转码可能耗时数分钟，调用方传可取消的 ctx（RunCmd 杀进程组）。
func ConvertFile(ctx context.Context, ffmpegBin, srcPath, target string) (dst, mode string, err error) {
	if !ConvertSupported(target) {
		return "", "", fmt.Errorf("不支持的目标格式: %s", target)
	}
	ext := strings.ToLower(filepath.Ext(srcPath))
	if ext == "."+target {
		return srcPath, "skip", nil
	}
	dst = strings.TrimSuffix(srcPath, ext) + "." + target
	// run 一轮转换并校验产物：退出码 0 但产物缺失/为空也不算成功
	//（不把这种「假成功」交出去）。
	run := func(transcode bool) error {
		if err := runFFmpeg(ctx, ffmpegBin, convertArgs(srcPath, dst, target, transcode)); err != nil {
			return err
		}
		st, statErr := os.Stat(dst)
		if statErr != nil || st.Size() == 0 {
			_ = os.Remove(dst)
			return fmt.Errorf("ffmpeg 退出码 0 但产物缺失或为空（stat_err=%v）", statErr)
		}
		return nil
	}
	err = run(false)
	if err != nil && target == "mp4" {
		// remux 不通（编解码器与 mp4 容器不兼容）：清掉半成品再转码。
		_ = os.Remove(dst)
		if err = run(true); err != nil {
			return "", "", err
		}
		return dst, "transcode", nil
	}
	if err != nil {
		return "", "", err
	}
	if target == "mp3" {
		return dst, "audio-extract", nil
	}
	return dst, "remux", nil
}

// convertArgs 组装 ffmpeg 参数。
//
// mp4 remux：流复制 + faststart（元数据前置，流媒体点开即播）。
// mp4 transcode：H.264 veryfast/CRF23 是「兼容性与速度的务实点」——
// 转码分支只在 remux 不通时走，选预设的依据是别让转换比下载还久。
// mp3：丢弃视频轨，V2 高音质（~190kbps VBR）。
func convertArgs(src, dst, target string, transcode bool) []string {
	switch target {
	case "mp4":
		if transcode {
			return []string{"-y", "-i", src, "-c:v", "libx264", "-preset", "veryfast", "-crf", "23",
				"-c:a", "aac", "-b:a", "192k", "-movflags", "+faststart", dst}
		}
		return []string{"-y", "-i", src, "-c", "copy", "-movflags", "+faststart", dst}
	case "mp3":
		return []string{"-y", "-i", src, "-vn", "-c:a", "libmp3lame", "-q:a", "2", dst}
	}
	return nil
}

// runFFmpeg 执行 ffmpeg；错误信息带输出尾部（编解码器不匹配等原因在这里）。
func runFFmpeg(ctx context.Context, bin string, args []string) error {
	const tailLen = 12
	tail := make([]string, 0, tailLen)
	err := RunCmd(ctx, bin, args, func(line string) {
		if line = strings.TrimSpace(line); line == "" {
			return
		}
		if len(tail) == tailLen {
			copy(tail, tail[1:])
			tail[tailLen-1] = line
			return
		}
		tail = append(tail, line)
	}, "")
	if err != nil {
		if len(tail) > 0 {
			return fmt.Errorf("%w｜ffmpeg 输出尾部: %s", err, strings.Join(tail, " / "))
		}
		return err
	}
	return nil
}
