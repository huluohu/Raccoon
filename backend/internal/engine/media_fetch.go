package engine

// media_fetch.go：媒体直取（用 ffmpeg 拉 HLS/DASH 清单）。
//
// 为什么需要这条旁路（2026-10-28 实测 bad.news 得出）：
//
//	通用提取器（generic）会把**页面自己的请求头**原样抄到媒体请求上——
//	包括 Referer 与 Sec-Fetch-Mode。于是媒体 CDN 收到的 Referer 指向的是
//	「聚合页」而不是媒体源自己；不少 CDN 有自己的防盗链策略（只认自家源、
//	或不认任何 Referer），这个 Referer 就是错的 → HTTP 403。
//
//	浏览器侧的下载插件（cat-catch 之类）是在**页面上下文**里拦截浏览器自己
//	发出的媒体请求，因此天然带着对的上下文；服务端下载器没有页面上下文，
//	只能自己定：这里显式把 Referer 设成**媒体源自己**，并用 ffmpeg 拉流。
//
//	实测（video.twimg.com 的 amplify_video）：
//	  带 Referer: bad.news      → 403
//	  带 Referer: twitter.com  → 200
//	  不带 Referer              → 200
//	而 yt-dlp 侧的 --referer / --load-info-json 都绕不开（前者被格式自带的
//	http_headers 覆盖，后者会重新解析页面）。
//
// 只用于**清单型**媒体（HLS m3u8 / DASH mpd）：普通直链文件用 HTTP 直取更省事，
// 且 yt-dlp 自己的下载器在这类场景本来就没问题。

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// MediaSource 一条待直取的媒体流。
type MediaSource struct {
	// URL 媒体直链（m3u8 / mpd）。
	URL string
	// Audio 是否为纯音轨（视频流之外的第二条流）。
	Audio bool
}

// IsManifestURL 判断直链是否是清单型媒体（HLS/DASH）。
func IsManifestURL(raw string) bool {
	low := strings.ToLower(raw)
	if strings.Contains(low, ".m3u8") || strings.Contains(low, ".mpd") {
		return true
	}
	// Twitter 的 amplify_video 清单不带扩展名，但路径里有 /pl/ 或 /vid/ 段。
	return strings.Contains(low, "/pl/") && strings.Contains(low, "amplify_video")
}

// ffmpegTimeRe 匹配 ffmpeg 进度行里的 time=HH:MM:SS.ss。
var ffmpegTimeRe = regexp.MustCompile(`time=(\d+):(\d\d):(\d\d(?:\.\d+)?)`)

// ffmpegSizeRe 匹配 ffmpeg 进度行里的 size=1234kB。
var ffmpegSizeRe = regexp.MustCompile(`size=\s*(\d+)\s*kB`)

// ParseFFmpegSize 从 ffmpeg 进度行解析已写入字节数（size=1234kB）。
//
// 为什么需要：直取时媒体时长常常未知（播放列表条目常不带 duration），
// 百分比算不出来，但「已经写了多少字节」是实打实的——报出来用户才知道它在动。
func ParseFFmpegSize(line string) (int64, bool) {
	m := ffmpegSizeRe.FindStringSubmatch(line)
	if len(m) != 2 {
		return 0, false
	}
	kb, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return kb * 1024, true
}

// ParseFFmpegTime 从 ffmpeg 进度行解析已下载秒数；解析不出返回 (-1, false)。
func ParseFFmpegTime(line string) (float64, bool) {
	m := ffmpegTimeRe.FindStringSubmatch(line)
	if len(m) != 4 {
		return 0, false
	}
	h, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	sec, _ := strconv.ParseFloat(m[3], 64)
	return float64(h)*3600 + float64(min)*60 + sec, true
}

// mediaOrigin 取媒体直链的源（scheme://host）。
func mediaOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// FetchMediaDirect 用 ffmpeg 直取媒体流并落盘。
//
// sources：第一条为视频流，其后为音轨（可空）。
// durationSec：已知时长（秒）；>0 时进度百分比才准确（禁估算）。
// dst：输出文件路径（容器与编码由扩展名决定；建议 .mp4）。
//
// 进度：ffmpeg 的 time= 解析出的已下载秒数 → 百分比 = 秒/时长。
// 体积不报（流式拉取期间拿不到可靠字节数），置 0 由调用方在完成后按实际文件大小补。
func FetchMediaDirect(ctx context.Context, ffmpegBin string, sources []MediaSource, dst string, durationSec float64, onProgress func(Progress)) error {
	if strings.TrimSpace(ffmpegBin) == "" {
		return fmt.Errorf("ffmpeg 不可用")
	}
	var video, audio string
	for _, s := range sources {
		if s.URL == "" {
			continue
		}
		if s.Audio {
			if audio == "" {
				audio = s.URL
			}
			continue
		}
		if video == "" {
			video = s.URL
		}
	}
	if video == "" && audio == "" {
		return fmt.Errorf("没有可直取的媒体直链")
	}

	args := []string{"-y", "-hide_banner", "-loglevel", "info"}
	addInput := func(u string) {
		if origin := mediaOrigin(u); origin != "" {
			// Referer 指向**媒体源自己**（不是来源页）——这正是被 403 拒的那个头。
			args = append(args, "-headers", "Referer: "+origin+"/\r\n")
		}
		args = append(args, "-i", u)
	}
	addInput(video)
	audioIdx := -1
	if audio != "" {
		addInput(audio)
		audioIdx = 1
	}

	switch {
	case video != "" && audioIdx >= 0:
		args = append(args, "-map", "0:v:0", "-map", "1:a:0")
	case video != "":
		args = append(args, "-map", "0:v:0", "-map", "0:a:0?")
	default:
		args = append(args, "-map", "0:a:0")
	}
	args = append(args, "-c", "copy", "-movflags", "+faststart", dst)

	// ffmpeg 进度：stderr 的 time= 字段。
	start := time.Now()
	err := RunCmd(ctx, ffmpegBin, args, func(line string) {
		if onProgress == nil {
			return
		}
		sec, ok := ParseFFmpegTime(line)
		size, hasSize := ParseFFmpegSize(line)
		if !ok && !hasSize {
			return
		}
		p := Progress{Percent: -1, Downloaded: 0, Total: 0}
		if hasSize {
			p.Downloaded = size
		}
		if durationSec > 0 {
			p.Percent = sec / durationSec * 100
			if p.Percent > 100 {
				p.Percent = 100
			}
		}
		elapsed := time.Since(start).Seconds()
		if elapsed > 0 && sec > 0 && durationSec > 0 {
			// 剩余时间 = 剩余秒数 / 已用速率（用真实耗时推算，不猜）
			if remaining := durationSec - sec; remaining > 0 {
				p.ETASec = int64(remaining / (sec / elapsed))
			}
		}
		onProgress(p)
	}, "")

	if err != nil {
		_ = os.Remove(dst)
		return err
	}
	st, statErr := os.Stat(dst)
	if statErr != nil || st.Size() == 0 {
		_ = os.Remove(dst)
		return fmt.Errorf("ffmpeg 直取结束但产物缺失或为空")
	}
	return nil
}
