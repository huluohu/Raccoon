package engine

// ytdlp.go：yt-dlp 适配器（docs/08 W2.3）。进度模板经本机实测修正：
// %(progress.percent)s 在部分站点输出 NA，percent 必须由 downloaded/total 自算；
// 产物路径优先取 [download] Destination: 行，兜底扫描保存目录新文件。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// YTDLP 是 yt-dlp 引擎实现。
type YTDLP struct {
	Bin      string
	FFmpeg   string // 合并/转码用（缺失时降级：依赖无需合并的源）
	mu       sync.Mutex
	version  string
	checkErr error
}

// NewYTDLP 构造；二进制缺失不在此报错，由 Available() 统一自检（503 语义）。
func NewYTDLP(bin, ffmpeg string) *YTDLP { return &YTDLP{Bin: bin, FFmpeg: ffmpeg} }

// impersonateTarget TLS 指纹伪装目标。
//
// 部分站点（PornHub 等）会对非浏览器 TLS 指纹直接返回 410 Gone，yt-dlp 报
// "The extractor is attempting impersonation, but no impersonate target is available"。
// 装上 curl_cffi 只是解锁了这项能力——**不显式指定 --impersonate 就不生效**
// （实测：仅装 curl_cffi 仍报 "No video formats found"，加上后正常）。
//
// 伪装只改 TLS 指纹，不改请求语义；对不支持的站点无副作用。
const impersonateTarget = "chrome"

// impersonateArgs 伪装参数组。
//
// 仅在运行环境具备 curl_cffi 时追加：curl_cffi 缺失时 yt-dlp 遇到 --impersonate
// 会直接报错退出，反而让所有站点都不可用。缺失时降级为「不伪装」，
// 由 classify 把「requires impersonation」归类成可读提示。
func (y *YTDLP) impersonateArgs() []string {
	if !hasCurlCffi(y.Bin) {
		return nil
	}
	return []string{"--impersonate", impersonateTarget}
}

func (y *YTDLP) Name() string { return "yt-dlp" }

// Version 返回 yt-dlp 版本（未自检触发一次探测）。
func (y *YTDLP) Version() string {
	return y.versionOnce()
}

// Available 启动自检：二进制存在且可执行；ffmpeg 缺失不阻断（降级提示）。
func (y *YTDLP) Available() error {
	y.mu.Lock()
	defer y.mu.Unlock()
	if y.checkErr != nil {
		return y.checkErr
	}
	if y.version != "" {
		return nil
	}
	var lines []string
	if err := RunCmd(context.Background(), y.Bin, []string{"--version"}, func(l string) {
		if len(lines) < 3 {
			lines = append(lines, l)
		}
	}, ""); err != nil {
		y.checkErr = &UnavailableError{Engine: "yt-dlp", Reason: "无法执行: " + err.Error()}
		return y.checkErr
	}
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		y.checkErr = &UnavailableError{Engine: "yt-dlp", Reason: "版本输出为空"}
		return y.checkErr
	}
	y.version = strings.TrimSpace(lines[0])
	return nil
}

func (y *YTDLP) versionOnce() string {
	_ = y.Available()
	y.mu.Lock()
	defer y.mu.Unlock()
	return y.version
}

// FFmpegAvailable ffmpeg 自检（后处理能力）。
func (y *YTDLP) FFmpegAvailable() error {
	if y.FFmpeg == "" {
		return &UnavailableError{Engine: "ffmpeg", Reason: "未配置路径"}
	}
	var got bool
	_ = RunCmd(context.Background(), y.FFmpeg, []string{"-version"}, func(l string) {
		if !got && strings.HasPrefix(l, "ffmpeg version") {
			got = true
		}
	}, "")
	if !got {
		return &UnavailableError{Engine: "ffmpeg", Reason: "无法执行或版本输出异常"}
	}
	return nil
}

// UnavailableError 引擎不可用（上层转 503 ENGINE_UNAVAILABLE）。
type UnavailableError struct {
	Engine string
	Reason string
}

func (e *UnavailableError) Error() string { return e.Engine + " 不可用: " + e.Reason }

// ---- Resolve ----

// Resolve 拉元信息（--dump-single-json）。
func (y *YTDLP) Resolve(ctx context.Context, url string, opts ResolveOpts) (*ResolveResult, error) {
	// --playlist-items 1：合集/频道/播放列表链接只解析第一个条目。
	//
	// 为什么必须加：--no-playlist 对「频道首页」这类链接不生效，yt-dlp 会
	// 逐个拉取列表里每个视频的完整信息（实测 @laoming 频道耗时 2 分 32 秒，
	// 且每个会员专属视频都报一次错），远超调用方的 30–40 秒超时。
	// 加上后同一链接降到 6 秒。解析预览只需要来源的标题/封面/条目数，
	// 完整条目信息由下载阶段单独拉，不受影响。
	args := []string{"--dump-single-json", "--no-warnings", "--no-playlist", "--playlist-items", "1", "--socket-timeout", "20"}
	args = append(args, y.impersonateArgs()...)
	args = append(args, jsRuntimeArgs()...)
	args = append(args, formatSortArgs()...)
	if opts.Cookies != "" {
		args = append(args, "--cookies", opts.Cookies)
	}
	if opts.Proxy != "" {
		args = append(args, "--proxy", opts.Proxy)
	}
	args = append(args, url)
	var raw strings.Builder
	err := RunCmd(ctx, y.Bin, args, func(l string) {
		raw.WriteString(l)
		raw.WriteByte('\n')
	}, "")
	if err != nil {
		return nil, classify(err, raw.String())
	}
	var d ytdlpJSON
	if err := json.Unmarshal([]byte(raw.String()), &d); err != nil {
		// 单行 JSON 混入日志：倒序找第一段可解析的完整行。
		for _, line := range reverseLines(raw.String()) {
			if !strings.HasPrefix(strings.TrimSpace(line), "{") {
				continue
			}
			if json.Unmarshal([]byte(line), &d) == nil {
				break
			}
		}
		if d.Title == "" && d.ID == "" {
			return nil, &EngineError{Code: "PARSE_FAILED", Message: "解析引擎输出失败: " + err.Error()}
		}
	}
	// 播放列表/合集：真实内容在第一个条目里（顶层 formats 为空）。
	// 顶层没内容时用条目兜底——否则这类链接预览无清晰度可选、媒体域名也记不上。
	if len(d.Formats) == 0 && len(d.Entries) > 0 {
		e := d.Entries[0]
		d.Formats = e.Formats
		if d.Title == "" || d.Title == e.ID {
			d.Title = e.Title
		}
		if d.Duration == 0 {
			d.Duration = e.Duration
		}
		if d.Thumbnail == "" {
			d.Thumbnail = e.Thumbnail
		}
	}
	res := &ResolveResult{
		Title:      d.Title,
		Thumbnail:  firstNonEmpty(d.Thumbnail, d.Thumbnails()),
		Uploader:   firstNonEmpty(d.Uploader, d.UploaderID, d.Channel),
		Duration:   int64(d.Duration),
		Platform:   platformOf(d.ExtractorKey, url),
		IsPlaylist: d.Type == "playlist",
	}
	res.Formats = collectFormats(d.Formats)
	return res, nil
}

// ytdlpFormat yt-dlp formats[] 中 adapter 关心的字段（不建全量模型）。
type ytdlpFormat struct {
	FormatID   string `json:"format_id"`
	FormatNote string `json:"format_note"`
	Ext        string `json:"ext"`
	// URL 是该格式的**真实直链**（媒体 CDN，带签名有时效）。
	// 解析域名与媒体域名往往不是同一个（YouTube: www.youtube.com vs googlevideo.com），
	// 「解析成功但下载失败」十有八九出在媒体域名上——留着它才能归因（2026-10-28）。
	URL            string   `json:"url"`
	Protocol       string   `json:"protocol"`
	Height         int      `json:"height"`
	Width          int      `json:"width"`
	VCodec         string   `json:"vcodec"`
	ACodec         string   `json:"acodec"`
	Filesize       *int64   `json:"filesize"`
	FilesizeApprox *int64   `json:"filesize_approx"`
	TBR            *float64 `json:"tbr"`
	Language       string   `json:"language"`
}

// ytdlpJSON 只取 adapter 需要的字段（yt-dlp 输出上百字段，不建全量模型）。
type ytdlpJSON struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Uploader     string  `json:"uploader"`
	UploaderID   string  `json:"uploader_id"`
	Channel      string  `json:"channel"`
	Duration     float64 `json:"duration"`
	Thumbnail    string  `json:"thumbnail"`
	ThumbnailArr []struct {
		URL    string `json:"url"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	} `json:"thumbnails"`
	ExtractorKey string        `json:"extractor_key"`
	Extractor    string        `json:"extractor"`
	Type         string        `json:"_type"`
	Formats      []ytdlpFormat `json:"formats"`
	// Entries 播放列表/合集的条目（--playlist-items 1 时只有一条）。
	// 这类链接（bad.news 这类「页面里嵌了一个视频」）的真实格式与标题
	// 都在条目里，顶层是空的——不取条目，预览就没有清晰度可选，
	// 媒体直链域名也记不上（2026-10-28 定位）。
	Entries []ytdlpEntry `json:"entries"`
}

// ytdlpEntry 播放列表的一个条目。
type ytdlpEntry struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	URL       string        `json:"url"`
	Webpage   string        `json:"webpage_url"`
	Duration  float64       `json:"duration"`
	Thumbnail string        `json:"thumbnail"`
	Formats   []ytdlpFormat `json:"formats"`
}

func (j ytdlpJSON) Thumbnails() string {
	if len(j.ThumbnailArr) == 0 {
		return ""
	}
	best := j.ThumbnailArr[0]
	for _, t := range j.ThumbnailArr {
		if t.Width > best.Width {
			best = t
		}
	}
	return best.URL
}

// collectFormats 每分辨率取**画质最好**的候选（界面只展示一版），附加音频档。
//
// 取哪一条必须与下载阶段一致（formatSortArgs 定的排序：同档内码率从高到低）。
// 早先这里取的是**最小体积**那条，与下载阶段实际选中的流对不上：
// B 站 1080p 预览显示 15.28MiB（AV1），下载却拿到 75.23MiB（AVC）；
// 用户看到的体积与实际产物对不上号，等于预览在说谎。
//
// 「画质最好」的判据用码率（tbr），与 yt-dlp 的 br 排序字段同源：
//  1. 码率高者胜（同分辨率下码率越高画质越好）；
//  2. 码率缺失（0）时退回比体积，大者胜；
//  3. 都拿不到时保留先到的那条（站点没给体积信息的常见情况）。
//
// 音频档同理取码率最高的一条（bestaudio 的口径），不再取最小体积。
func collectFormats(fs []ytdlpFormat) []FormatInfo {
	type cand struct {
		id   string
		size int64
		br   float64
		note string
		url  string
	}
	// betterB 判定 a 是否比 b 更好（码率优先，体积兜底）。
	betterB := func(a, b cand) bool {
		if a.br > 0 && b.br > 0 {
			return a.br > b.br
		}
		if a.br > 0 && b.br <= 0 {
			return true
		}
		if a.br <= 0 && b.br > 0 {
			return false
		}
		if a.size > 0 && b.size > 0 {
			return a.size > b.size
		}
		if a.size > 0 && b.size <= 0 {
			return true
		}
		return false
	}
	byHeight := map[int]cand{}
	var audioBest *cand
	for _, f := range fs {
		var sz int64
		if f.Filesize != nil {
			sz = *f.Filesize
		} else if f.FilesizeApprox != nil {
			sz = *f.FilesizeApprox
		}
		var br float64
		if f.TBR != nil {
			br = *f.TBR
		}
		if f.VCodec != "none" && f.Height > 0 {
			c := cand{id: f.FormatID, size: sz, br: br, note: f.FormatNote, url: f.URL}
			old, ok := byHeight[f.Height]
			if !ok || betterB(c, old) {
				byHeight[f.Height] = c
			}
			continue
		}
		if f.VCodec == "none" && f.ACodec != "none" {
			c := cand{id: f.FormatID, size: sz, br: br, note: "audio", url: f.URL}
			if audioBest == nil || betterB(c, *audioBest) {
				best := c
				audioBest = &best
			}
		}
	}
	heights := make([]int, 0, len(byHeight))
	for h := range byHeight {
		heights = append(heights, h)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(heights)))
	out := make([]FormatInfo, 0, len(heights)+1)
	for _, h := range heights {
		c := byHeight[h]
		var size *int64
		if c.size > 0 {
			s := c.size
			size = &s
		}
		out = append(out, FormatInfo{
			Quality: fmt.Sprintf("%dp", h),
			Size:    size,
			Note:    c.note,
			URL:     c.url,
			Host:    urlHost(c.url),
		})
		if len(out) >= 6 {
			break // 界面只展示前 6 档
		}
	}
	if audioBest != nil {
		var size *int64
		if (*audioBest).size > 0 {
			s := (*audioBest).size
			size = &s
		}
		out = append(out, FormatInfo{
			Quality: "audio",
			Size:    size,
			Note:    "仅音频",
			URL:     (*audioBest).url,
			Host:    urlHost((*audioBest).url),
		})
	}
	return out
}

// ---- Download ----

const progressPrefix = "PROGRESS|"

// Download 执行下载；onProgress 每个进度行回调一次（节流由 worker 负责）。
func (y *YTDLP) Download(ctx context.Context, url string, opts DownloadOpts, onProgress func(Progress)) ([]DownloadedFile, error) {
	if err := os.MkdirAll(opts.SaveDir, 0o755); err != nil {
		return nil, &EngineError{Code: "IO", Message: "创建保存目录失败: " + err.Error()}
	}
	before := dirSnapshot(opts.SaveDir)

	args := []string{
		"--newline",
		"--no-warnings",
		"--no-playlist",
		"--socket-timeout", "20",
		"--progress-template", progressPrefix + "%(progress.percent)s|%(progress.downloaded_bytes)s|%(progress.total_bytes)s|%(progress.speed)s|%(progress.eta)s",
		"-o", opts.NameTmpl,
	}
	args = append(args, y.impersonateArgs()...)
	args = append(args, jsRuntimeArgs()...)
	args = append(args, formatSortArgs()...)
	if sel := formatSelector(opts.Format); sel != "" {
		args = append(args, "-f", sel)
	}
	if opts.RateLimit > 0 {
		args = append(args, "-r", strconv.FormatInt(opts.RateLimit/1024, 10)+"K")
	}
	if opts.Cookies != "" {
		args = append(args, "--cookies", opts.Cookies)
	}
	if y.FFmpeg != "" {
		args = append(args, "--ffmpeg-location", filepath.Dir(y.FFmpeg))
	}
	// 产物增强（docs/06 配置开关，默认关）：
	// 封面落盘无额外依赖；转 jpg 需要 ffmpeg（缺 ffmpeg 时保留原始格式）。
	// 元数据内嵌对 mp4/m4a 走 mutagen、mkv/webm 走 ffmpeg，两者缺位时
	// yt-dlp 会以后处理器告警收场（不中断下载本身）。
	if opts.WriteThumbnail {
		args = append(args, "--write-thumbnail")
		if y.FFmpeg != "" {
			args = append(args, "--convert-thumbnails", "jpg")
		}
	}
	if opts.EmbedMetadata {
		args = append(args, "--embed-metadata")
	}
	if opts.HTTPProxy != "" {
		args = append(args, "--proxy", opts.HTTPProxy)
	}
	args = append(args, url)

	var tail []string     // 错误分类用的输出尾部（末 40 行）
	var allLines []string // 产物解析用：Destination / Merger 行可能出现在很靠前的位置
	var destinations []string
	merging := false
	itemIndex, itemCount := 0, 0 // 播放列表条目游标（"Downloading item X of Y" 行）
	err := RunCmd(ctx, y.Bin, args, func(line string) {
		if strings.HasPrefix(line, progressPrefix) {
			if p, ok := parseProgress(line); ok && onProgress != nil {
				p.ItemIndex, p.ItemCount = itemIndex, itemCount
				onProgress(p)
			}
			return
		}
		// 播放列表逐条目下载时 yt-dlp 输出 "[download] Downloading item 3 of 25"
		// （老版本用 video 一词，两种都收）。worker 靠它把「当前视频的百分比」
		// 折算成合集的全局进度——否则进度条在每个视频开始时清零回跳。
		if n, total, ok := parseItemOf(line); ok {
			itemIndex, itemCount = n, total
		}
		tail = append(tail, line)
		if len(tail) > 40 {
			tail = tail[1:]
		}
		// 产物相关行单独收：Destination / Merger / 「已存在」提示。
		// 不受 tail 长度限制——合集任务里 Merger 行可能在很靠前的位置，
		// 只留末尾 40 行会把真正的产物路径截掉。
		if strings.HasPrefix(line, "[download] Destination:") ||
			strings.Contains(line, "[Merger]") ||
			strings.Contains(line, "[FixupM3u8]") ||
			strings.Contains(line, "has already been downloaded") {
			allLines = append(allLines, line)
		}
		if strings.HasPrefix(line, "[download] Destination:") {
			destinations = append(destinations, strings.TrimSpace(strings.TrimPrefix(line, "[download] Destination:")))
		}
		// yt-dlp 默认不覆盖已存在文件：命中时打印「xxx has already been downloaded」
		// （合并场景带 and merged 后缀）并正常退出——既不打印 Destination 行，
		// 也不产生新文件。不接住这一行，重复下载同一个视频会被误判 NO_OUTPUT：
		// 用户看到「下载失败」，而文件明明早就躺在下载目录里。
		if p := alreadyDownloadedPath(line); p != "" {
			destinations = append(destinations, p)
		}
		if strings.Contains(line, "[Merger]") || strings.Contains(line, "[FixupM3u8]") {
			merging = true
		}
	}, opts.SaveDir)
	_ = merging
	if err != nil {
		return nil, classify(err, strings.Join(tail, "\n"))
	}
	// 产物：Merger 合并产物 > Destination > 全量收集（.part 过滤）> 目录新文件兜底。
	// Destination 行输出的是相对路径（-o 为相对模板、cwd=SaveDir），须拼回 SaveDir。
	productLines := strings.Join(allLines, "\n")
	files := collectFiles(destinations, before, opts.SaveDir, parseMergedPaths(productLines))
	if len(files) == 0 {
		files = newFilesSince(opts.SaveDir, before)
	}
	if len(files) == 0 {
		return nil, &EngineError{Code: "NO_OUTPUT", Message: "引擎退出成功但未发现产物文件"}
	}
	return files, nil
}

// parseDestinationPaths 从引擎输出里提取所有 "[download] Destination: <路径>" 行。
// 供 Download 与 golden 测试共用——避免两处解析规则漂移。
func parseDestinationPaths(output string) []string {
	var out []string
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, "[download] Destination:") {
			continue
		}
		out = append(out, strings.TrimSpace(strings.TrimPrefix(line, "[download] Destination:")))
	}
	return out
}

// parseMergedPaths 从引擎输出里提取合并产物路径。
//
// 合并场景下 Destination 行给出的是**分片**（.f401.mp4 / .f251.webm），
// 真正的产物是 [Merger] 行里的合并文件（.webm）——它从未出现在任何
// Destination 行里。只认 Destination 会漏掉合并产物（走到目录兜底才碰巧捡到，
// 而一旦分片残留就会把碎片当产物收进媒体库）。
//
// 格式：[Merger] Merging formats into "<路径>"
// 另有 [FixupM3u8] 用 = 分隔（无引号），一并处理。
func parseMergedPaths(output string) []string {
	var out []string
	for _, line := range strings.Split(output, "\n") {
		var path string
		switch {
		case strings.Contains(line, "[Merger]") && strings.Contains(line, "Merging formats into"):
			rest := line[strings.Index(line, "Merging formats into")+len("Merging formats into"):]
			path = strings.TrimSpace(rest)
			path = strings.Trim(path, `"`)
		case strings.Contains(line, "[FixupM3u8]") && strings.Contains(line, "FixupM3u8:"):
			rest := line[strings.Index(line, "FixupM3u8:")+len("FixupM3u8:"):]
			path = strings.TrimSpace(rest)
			path = strings.Trim(path, `"`)
		default:
			continue
		}
		if path != "" {
			out = append(out, path)
		}
	}
	return out
}

// formatSelector 内部质量值 → yt-dlp format 表达式（docs/06 默认 best）。
//
// 注意：内部默认值 "best" 不能透传给 yt-dlp —— 纯 DASH 站点（如 bilibili）
// 没有 combined 流，`-f best` 必然 FORMAT_UNAVAILABLE，须展开为 视频+音频合并。
//
// 表达式必须**站点无关**。原先写的 `bv*[height<=720]+ba/...` 里的 bv* 是
// Bilibili 专有 format id，只有 B 站认；其它站点（YouTube 的 137/398、
// PornHub 的 hls-383）全匹配不上，只能靠末尾的 /b 兜底。
//
// 通用写法：`bv*+ba/b[height<=N]+ba/b[height<=N]/b`
//
//	bv*+ba      → 优先取「视频流 + 音频流」合并（B 站/YouTube 等 DASH 站）
//	b[height<=N] → 站点若直接提供 combined 流且高度达标，取它
//	b           → 最后兜底取 best
//
// 为什么末尾必须有 `/b`：解析预览与下载是两次独立的引擎调用，站点的
// 格式列表可能变化（PornHub 实测：同一视频解析时列 240p/480p/720p 三档，
// 下载那次只剩 720p 的 hls-383）。若用户选了此时已不存在的档位，
// 严格的高度匹配会直接 FORMAT_UNAVAILABLE —— 宁可降档下载，也不能让任务失败。
//
// 表达式只管「选哪一档分辨率」，**同分辨率下选哪条流**由 formatSortArgs
// 决定（见该函数注释）—— 两件事必须分开，否则没法单独调整排序策略。
func formatSelector(q string) string {
	switch q {
	case "audio":
		return "bestaudio/best"
	case "best", "":
		return "bv*+ba/b"
	case "1080p":
		return "bv*[height<=1080]+ba/b[height<=1080]/b"
	case "720p":
		return "bv*[height<=720]+ba/b[height<=720]/b"
	case "480p":
		return "bv*[height<=480]+ba/b[height<=480]/b"
	case "360p":
		return "bv*[height<=360]+ba/b[height<=360]/b"
	default:
		// 已知 format id 透传（解析预览 Agent 回来具体 id 的场景）。
		return q
	}
}

// formatSortArgs 同分辨率下的格式排序参数（--format-sort）。
//
// 为什么需要它（2026-10-29 实测，B 站免登录）：
//
// 同一档 1080p，B 站同时提供三条流：
//
//	30080  avc1.640032   2629k   ≈66.58MiB
//	100113 hev1.1.6.L150 749k    ≈18.97MiB
//	100026 av01.0.08M.08 604k    ≈15.28MiB
//
// yt-dlp 自带的默认排序把 **vcodec 排在 br 前面**，且其 vcodec 偏好序是
// av01 > vp9 > hevc > avc —— 于是「同分辨率下码率最高的那条」恰恰排在最后：
// 默认 `-f bv*+ba/b` 实测选中 100026（AV1，21MB 成品），而不是 30080
// （AVC，75MB 成品）。同一分辨率、同样的 1080P，用户拿到的画质差 3.5 倍。
//
// 这里显式指定排序：**先按分辨率/帧率，同档内按码率从高到低**，
// 让「best」真的等于「同分辨率下画质最好的那条」。
//
// 为什么用 --format-sort 而不是改 -f 表达式：-f 只能按属性做**过滤**
// （如 [vcodec^=avc1]），做不到「同属性下取码率最大」——那必须靠排序。
// 而排序字段写成站点无关的形式（res/fps/br），对所有站点都成立，
// 不像 [vcodec^=avc1] 那样只对某些站点有意义。
//
// 兼容性：--format-sort 是 yt-dlp 的稳定选项（2021 年起就有），
// 老版本不认会直接报错退出的风险由引擎版本探测覆盖；
// 且排序只影响「同档内挑哪条」，不会把不匹配的视频流选出来。
func formatSortArgs() []string {
	return []string{"--format-sort", "res,fps,br,size"}
}

// parseItemOf 解析播放列表条目行 "[download] Downloading item 3 of 25"
// （老版本 yt-dlp 用 "Downloading video 3 of 25"）。非条目行返回 ok=false。
func parseItemOf(line string) (index, total int, ok bool) {
	s := strings.TrimPrefix(line, "[download] ")
	s = strings.TrimSpace(s)
	for _, verb := range []string{"Downloading item ", "Downloading video "} {
		if !strings.HasPrefix(s, verb) {
			continue
		}
		rest := strings.TrimPrefix(s, verb)
		i, after, found := strings.Cut(rest, " of ")
		if !found {
			return 0, 0, false
		}
		n, err := strconv.Atoi(strings.TrimSpace(i))
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		t, err := strconv.Atoi(strings.TrimSpace(strings.TrimSpace(after)))
		if err != nil || t < n {
			return 0, 0, false
		}
		return n, t, true
	}
	return 0, 0, false
}

// parseProgress 解析 PROGRESS 行；percent 实测可能为 NA → 自算。
func parseProgress(line string) (Progress, bool) {
	parts := strings.Split(strings.TrimSpace(line), "|")
	if len(parts) != 6 {
		return Progress{}, false
	}
	p := Progress{Percent: -1}
	p.Downloaded = atoiOr0(parts[2])
	p.Total = atoiOr0(parts[3])
	p.SpeedBps = int64(atofOr0(parts[4]))
	p.ETASec = atoiOr0(parts[5])
	if pct := atofOr0(parts[1]); pct > 0 {
		p.Percent = pct
	} else if p.Total > 0 && p.Downloaded > 0 {
		p.Percent = math.Round(float64(p.Downloaded)/float64(p.Total)*1000) / 10
	}
	return p, true
}

func atoiOr0(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "NA" || s == "Unknown" {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func atofOr0(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "NA" || s == "Unknown" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func reverseLines(s string) []string {
	lines := strings.Split(s, "\n")
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	return lines
}

// dirSnapshot 记录目录当前文件集（用于新文件兜底）。
func dirSnapshot(dir string) map[string]bool {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return map[string]bool{}
	}
	m := make(map[string]bool, len(ents))
	for _, e := range ents {
		m[e.Name()] = true
	}
	return m
}

// alreadyDownloadedPath 从 yt-dlp 输出行里提取「文件已存在」的路径。
//
// yt-dlp 默认不覆盖已存在文件，命中时打印其中一种并正常退出（exit 0）：
//
//	[download] /path/file.mp4 has already been downloaded
//	[download] /path/file.mkv has already been downloaded and merged
//
// 这两种行都不带 "Destination:" 前缀，也不产生新文件——只认 Destination 的
// 收集逻辑会把这种情况误判成 NO_OUTPUT（引擎退出成功但未发现产物文件）。
// 用户重复下同一个视频时看到的是「这单没成」，属于纯粹的误报。
// 返回空串表示这一行不是「已存在」提示。路径可能是相对路径（-o 为相对模板、
// 子进程 cwd=SaveDir），由 collectFiles 负责拼回与存在性校验。
func alreadyDownloadedPath(line string) string {
	if !strings.Contains(line, "has already been downloaded") {
		return ""
	}
	p := strings.TrimSpace(strings.TrimPrefix(line, "[download]"))
	p = strings.TrimSpace(strings.TrimSuffix(p, "and merged"))
	p = strings.TrimSpace(strings.TrimSuffix(p, "has already been downloaded"))
	return p
}

// collectFiles 收集产物：存在的合并产物 + 存在的 Destination + 目录内非 .part 新文件。
// dir 为 SaveDir：Destination / Merger 路径为相对路径时拼回（子进程 cwd 即 SaveDir）。
//
// merged 为合并产物路径（[Merger]/[FixupM3u8] 行解析而来）。合并场景下
// Destination 给的是分片，真产物只在 merged 里——所以 merged 优先，且
// 分片路径一律不作为产物（否则媒体库里会出现一堆 .f401.mp4 碎片，
// 真正的合并文件反而缺失）。
func collectFiles(destinations []string, before map[string]bool, dir string, merged []string) []DownloadedFile {
	seen := map[string]bool{}
	var out []DownloadedFile
	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		if strings.HasSuffix(path, ".part") || strings.HasSuffix(path, ".ytdl") {
			return
		}
		if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() && st.Size() > 0 {
			out = append(out, DownloadedFile{Path: path, Size: st.Size()})
		}
	}
	// 合并产物优先（它是最终成品），再收 Destination。
	for _, m := range merged {
		add(m)
	}
	for _, d := range destinations {
		if isStreamFragment(d, merged) {
			continue // 分片不是产物
		}
		add(d)
	}
	return out
}

// isStreamFragment 判断路径是否为某个合并产物的流分片。
//
// 精确比对，不靠猜文件名形状：yt-dlp 的分片名是
// `%(title)s [%(id)s].f%(format_id)s.%(ext)s`，合并产物是
// `%(title)s [%(id)s].%(ext)s` —— 分片去掉扩展名后 = 合并产物去掉扩展名
// + ".f" + format_id（纯数字）。
//
// 为什么不用「文件名里出现 .f 就算分片」：那会把用户视频里正常的
// ".file" 之类片段误伤（早先按字母数字段判定，"my.file.name.mp4" 被当成碎片，
// 表现为「下载成功但媒体库里没有文件」）。比对合并产物名是零猜测的。
//
// merged 为合并产物路径（可多个）；merged 为空时返回 false（没有合并就没有分片）。
func isStreamFragment(path string, merged []string) bool {
	base := filepath.Base(path)
	destStem := stemOf(base)
	for _, m := range merged {
		mergedStem := stemOf(filepath.Base(m))
		if mergedStem == "" || destStem == "" {
			continue
		}
		prefix := mergedStem + ".f"
		if !strings.HasPrefix(destStem, prefix) {
			continue
		}
		// ".f" 之后必须是纯数字 format_id。
		id := destStem[len(prefix):]
		if id == "" {
			continue
		}
		allDigits := true
		for _, r := range id {
			if r < '0' || r > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			return true
		}
	}
	return false
}

// stemOf 去掉文件名扩展名（.tar.gz 这类双扩展名只去最后一个）。
func stemOf(name string) string {
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// newFilesSince 兜底：下载启动后新出现且非临时后缀的文件。
func newFilesSince(dir string, before map[string]bool) []DownloadedFile {
	var out []DownloadedFile
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range ents {
		if e.IsDir() || before[e.Name()] {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".part") || strings.HasSuffix(name, ".ytdl") {
			continue
		}
		if st, err := e.Info(); err == nil && st.Size() > 0 {
			out = append(out, DownloadedFile{Path: filepath.Join(dir, name), Size: st.Size()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// EngineError 引擎域错误（含分类错误码；可重试判定在 task 包）。
type EngineError struct {
	Code    string
	Message string
	// Detail 引擎原始报错摘要（stderr 里的 ERROR 行）。
	// 前端不直接展示（避免泄内部信息），但 worker 落库到任务 detail，
	// 排障时能看到「到底是哪个链接、引擎报了什么」，不再只有一句退出码。
	Detail string
}

func (e *EngineError) Error() string { return e.Code + ": " + e.Message }

// classify 按退出码 + 输出尾部归类错误（docs/05 §7 重试策略依据）。
func classify(err error, output string) error {
	// 超时和用户取消是两回事：超时说明这次解析太慢/链接太重，
	// 报「已取消」会让人以为是自己点的。
	if errors.Is(err, context.DeadlineExceeded) {
		return &EngineError{Code: "TIMEOUT", Message: "解析超时（链接内容过多或平台响应过慢），可稍后重试"}
	}
	if errors.Is(err, context.Canceled) {
		return &EngineError{Code: "CANCELLED", Message: "已取消"}
	}
	ue := &EngineError{Code: "ENGINE_ERROR", Message: "下载引擎执行失败"}
	if ee, ok := err.(*ExitError); ok {
		ue.Message = fmt.Sprintf("引擎退出码 %d", ee.Code)
	}
	ue.Detail = engineDetail(output)
	low := strings.ToLower(output)
	switch {
	// 缺 TLS 指纹伪装能力：站点对非浏览器指纹直接拒绝。
	// yt-dlp 的原文是 "The extractor is attempting impersonation, but no
	// impersonate target is available" —— 这句话只在没装 curl_cffi 时出现。
	case strings.Contains(low, "no impersonate target is available"),
		strings.Contains(low, "requires impersonation"):
		ue.Code = "IMPERSONATION_UNAVAILABLE"
		ue.Message = "该站点要求浏览器指纹伪装，运行环境缺少 curl_cffi 支持"
	// 平台人机验证（YouTube 为典型）：数据中心/家宽出口 IP 会被间歇性要求
	// "Sign in to confirm you're not a bot"。这不是账号问题（匿名同样可下），
	// 是 IP 信誉问题，且多数时候重试即过——必须与真正的账号鉴权失败分开：
	// 早先裸匹配 "log in" 把它归入 AUTH_REQUIRED（不可自动重试 + 建议
	// 「去连接页更新凭据」），用户读作「必须登录才能下载」。
	case strings.Contains(low, "confirm you're not a bot"), strings.Contains(low, "confirm you are not a bot"),
		strings.Contains(low, "not a bot"):
		ue.Code = "BOT_CHECK"
		ue.Message = "平台对本服务器 IP 触发了人机验证（非账号问题），通常重试即可；频繁出现可在「连接」页配置该平台 Cookie 或在「设置」页配置代理出口"
	// 引擎版本落后于平台改版：YouTube 改内部实现后老版 yt-dlp 会稳定报
	// "The page needs to be reloaded."。这是本机可修的确定性问题（升级引擎），
	// 不该和普通网络抖动混在一起自动重试。
	case strings.Contains(low, "the page needs to be reloaded"),
		strings.Contains(low, "player_client"),
		strings.Contains(low, "unable to extract"),
		strings.Contains(low, "nsig extraction failed"):
		ue.Code = "ENGINE_OUTDATED"
		ue.Message = "下载引擎版本过旧，平台已改版；升级 yt-dlp 后重试"
	case strings.Contains(low, "requested format is not available"),
		strings.Contains(low, "requested format not available"):
		ue.Code = "FORMAT_UNAVAILABLE"
		ue.Message = "所选清晰度不可用，可换其他清晰度重试"
	// 真正的账号鉴权失败（members-only / 私享视频 / 年龄验证要求登录 /
	// Cookie 过期）。注意 "sign in to confirm your age" 这类年龄闸也归这里，
	// 但 "sign in to confirm you're not a bot" 已被上方 BOT_CHECK 截走。
	case strings.Contains(low, "sign in to confirm"), strings.Contains(low, "log in"), strings.Contains(low, "members-only"),
		strings.Contains(low, "cookies"), strings.Contains(low, "authentication"), strings.Contains(low, "private video"):
		ue.Code = "AUTH_REQUIRED"
		ue.Message = "需要登录或 Cookie 已失效，请到「连接」页更新平台凭据"
	// "video unavailable" 是 yt-dlp 的英文原文；平台与本地化版本会写成
	// "video is unavailable" / "this video is unavailable"，只匹配前者会漏判，
	// 导致「视频不存在」被误报成「引擎退出码 1」。
	case strings.Contains(low, "video unavailable"), strings.Contains(low, "video is unavailable"),
		strings.Contains(low, "this video has been removed"), strings.Contains(low, "404"),
		strings.Contains(low, "not found"), strings.Contains(low, "does not exist"),
		strings.Contains(low, "removed by the uploader"):
		ue.Code = "NOT_FOUND"
		ue.Message = "视频不存在、已删除或链接有误"
	// 410 Gone：部分站点（PornHub 等）对非浏览器 TLS 指纹返回 410。
	// 这不是「内容已删除」——内容没了会返回 404。它意味着请求被站点按
	// 指纹特征拒了，属于环境能力问题（缺 impersonation）。
	case strings.Contains(low, "410: gone"), strings.Contains(low, "http error 410"):
		ue.Code = "BLOCKED_BY_FINGERPRINT"
		ue.Message = "站点拒绝了当前请求（HTTP 410），通常需要浏览器指纹伪装才能访问"
	case strings.Contains(low, "429"), strings.Contains(low, "too many requests"), strings.Contains(low, "rate limit"):
		ue.Code = "RATE_LIMITED"
		ue.Message = "平台限流，稍后自动重试"
	// 403 Forbidden：媒体源**拒绝**了请求（不是网络不通）。
	// 必须排在下面 "unable to download" 兜底之前——yt-dlp 的原文
	// "unable to download video data: HTTP Error 403: Forbidden" 里含
	// "unable to download"，早先被兜底分支吞成「网络连接失败」，
	// 于是用户被告知「网络超时、去换代理」，而真实原因是媒体 CDN 拒绝
	// （Referer/指纹/区域限制/签名过期）。
	case strings.Contains(low, "http error 403"), strings.Contains(low, "403: forbidden"),
		strings.Contains(low, "403 forbidden"):
		ue.Code = "HTTP_FORBIDDEN"
		ue.Message = "媒体源拒绝了请求（HTTP 403）：不是网络不通——通常是媒体 CDN 不接受当前请求（来源页 Referer、指纹或区域限制）"
	case strings.Contains(low, "http error 5"):
		ue.Code = "HTTP_5XX"
		ue.Message = "平台服务端错误，稍后自动重试"
	case strings.Contains(low, "http error 4"):
		ue.Code = "HTTP_4XX"
		ue.Message = "平台返回客户端错误，请检查链接与权限"
	// 网络类兜底（放最后：上面已把带 HTTP 状态码的分支截走）
	case strings.Contains(low, "timed out"), strings.Contains(low, "timeout"),
		strings.Contains(low, "connection reset"), strings.Contains(low, "connection error"),
		strings.Contains(low, "temporary failure in name resolution"), strings.Contains(low, "unable to download"):
		ue.Code = "NET_TIMEOUT"
		ue.Message = "网络连接失败或被重置，稍后自动重试"
	}
	return ue
}

// engineDetail 从引擎输出里抽出最有信息量的一行报错摘要。
//
// 引擎退出时 stdout/stderr 混在一起，可能有几十行 WARNING 和进度噪声。
// 取最后一条 ERROR: 行（yt-dlp / gallery-dl 的错误都走这个前缀），
// 找不到就退而取最后一条非空行。超长截断到 200 字符。
func engineDetail(output string) string {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		if u := strings.ToUpper(l); strings.HasPrefix(u, "ERROR:") {
			return truncate(strings.TrimSpace(l[len("ERROR:"):]), 200)
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		return truncate(l, 200)
	}
	return ""
}

func truncate(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// platformOf 平台标识：extractor_key 优先，URL 域名兜底。
func platformOf(extractorKey, url string) string {
	k := strings.ToLower(extractorKey)
	switch {
	case strings.Contains(k, "youtube"):
		return "youtube"
	case strings.Contains(k, "bili"):
		return "bilibili"
	case strings.Contains(k, "tiktok"):
		return "tiktok"
	case strings.Contains(k, "douyin"):
		return "douyin"
	// 「x」必须整词匹配：Contains(k,"x") 会把 Generic 等任意含 x 的 extractor
	// 键误判成 X 平台，凭据中心就会错拿 twitter Cookie 去请求别的站。
	case strings.Contains(k, "twitter"), k == "x":
		return "twitter"
	case strings.Contains(k, "instagram"):
		return "instagram"
	case strings.Contains(k, "netease"):
		return "netease"
	case strings.Contains(k, "pornhub"):
		return "pornhub"
	case strings.Contains(k, "vimeo"):
		return "vimeo"
	case strings.Contains(k, "soundcloud"):
		return "soundcloud"
	case strings.Contains(k, "bandcamp"):
		return "bandcamp"
	case strings.Contains(k, "weibo"):
		return "weibo"
	case strings.Contains(k, "xiaohongshu"):
		return "xiaohongshu"
	}
	host := strings.ToLower(url)
	switch {
	case strings.Contains(host, "youtube.com"), strings.Contains(host, "youtu.be"):
		return "youtube"
	case strings.Contains(host, "bilibili.com"), strings.Contains(host, "b23.tv"):
		return "bilibili"
	case strings.Contains(host, "douyin.com"):
		return "douyin"
	case strings.Contains(host, "tiktok.com"):
		return "tiktok"
	case strings.Contains(host, "music.163.com"):
		return "netease"
	case strings.Contains(host, "twitter.com"), strings.Contains(host, "x.com"):
		return "twitter"
	case strings.Contains(host, "instagram.com"):
		return "instagram"
	case strings.Contains(host, "pornhub.com"):
		return "pornhub"
	case strings.Contains(host, "vimeo.com"):
		return "vimeo"
	case strings.Contains(host, "soundcloud.com"):
		return "soundcloud"
	case strings.Contains(host, "bandcamp.com"):
		return "bandcamp"
	case strings.Contains(host, "weibo.com"):
		return "weibo"
	case strings.Contains(host, "xiaohongshu.com"):
		return "xiaohongshu"
	case strings.Contains(host, "youku.com"):
		return "youku"
	case strings.Contains(host, "v.qq.com"), strings.Contains(host, "qq.com/nv"):
		return "tencent"
	case strings.Contains(host, "iqiyi.com"):
		return "iqiyi"
	case strings.Contains(host, "weixin.qq.com"), strings.Contains(host, "channels.weixin.qq.com"), strings.Contains(host, "finder.video.qq.com"):
		return "wechat"
	}
	return "web"
}
