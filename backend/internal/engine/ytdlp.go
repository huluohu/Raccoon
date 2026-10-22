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
	args := []string{"--dump-single-json", "--no-warnings", "--no-playlist", "--socket-timeout", "20"}
	if opts.Cookies != "" {
		args = append(args, "--cookies", opts.Cookies)
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
	FormatID       string   `json:"format_id"`
	FormatNote     string   `json:"format_note"`
	Ext            string   `json:"ext"`
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

// collectFormats 每分辨率取最小体积候选（界面只展示一版），附加音频档。
func collectFormats(fs []ytdlpFormat) []FormatInfo {
	type cand struct {
		id   string
		size int64
		note string
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
		if f.VCodec != "none" && f.Height > 0 {
			c := cand{id: f.FormatID, size: sz, note: f.FormatNote}
			if old, ok := byHeight[f.Height]; !ok || (sz > 0 && sz < old.size) || old.size == 0 {
				byHeight[f.Height] = c
			}
			continue
		}
		if f.VCodec == "none" && f.ACodec != "none" {
			c := cand{id: f.FormatID, size: sz, note: "audio"}
			if audioBest == nil || (sz > 0 && (*audioBest).size == 0) || (sz > 0 && sz < (*audioBest).size) {
				audioBest = &c
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
		out = append(out, FormatInfo{Quality: fmt.Sprintf("%dp", h), Size: size, Note: c.note})
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
		out = append(out, FormatInfo{Quality: "audio", Size: size, Note: "仅音频"})
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
	if opts.HTTPProxy != "" {
		args = append(args, "--proxy", opts.HTTPProxy)
	}
	args = append(args, url)

	var tail []string // 错误分类用的输出尾部
	var destinations []string
	merging := false
	err := RunCmd(ctx, y.Bin, args, func(line string) {
		if strings.HasPrefix(line, progressPrefix) {
			if p, ok := parseProgress(line); ok && onProgress != nil {
				onProgress(p)
			}
			return
		}
		tail = append(tail, line)
		if len(tail) > 40 {
			tail = tail[1:]
		}
		if strings.HasPrefix(line, "[download] Destination:") {
			destinations = append(destinations, strings.TrimSpace(strings.TrimPrefix(line, "[download] Destination:")))
		}
		if strings.Contains(line, "[Merger]") || strings.Contains(line, "[FixupM3u8]") {
			merging = true
		}
	}, opts.SaveDir)
	_ = merging
	if err != nil {
		return nil, classify(err, strings.Join(tail, "\n"))
	}
	// 产物：Destination > 全量收集（.part 过滤）> 目录新文件兜底。
	// Destination 行输出的是相对路径（-o 为相对模板、cwd=SaveDir），须拼回 SaveDir。
	files := collectFiles(destinations, before, opts.SaveDir)
	if len(files) == 0 {
		files = newFilesSince(opts.SaveDir, before)
	}
	if len(files) == 0 {
		return nil, &EngineError{Code: "NO_OUTPUT", Message: "引擎退出成功但未发现产物文件"}
	}
	return files, nil
}

// formatSelector 内部质量值 → yt-dlp format 表达式（docs/06 默认 best）。
// 注意：内部默认值 "best" 不能透传给 yt-dlp —— 纯 DASH 站点（如 bilibili）
// 没有 combined 流，`-f best` 必然 FORMAT_UNAVAILABLE，须展开为 视频+音频合并。
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

// collectFiles 收集产物：存在的 Destination + 目录内非 .part 新文件。
// dir 为 SaveDir：Destination 为相对路径时拼回（子进程 cwd 即 SaveDir）。
func collectFiles(destinations []string, before map[string]bool, dir string) []DownloadedFile {
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
	for _, d := range destinations {
		add(d)
	}
	return out
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
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &EngineError{Code: "CANCELLED", Message: "已取消"}
	}
	ue := &EngineError{Code: "ENGINE_ERROR", Message: "下载引擎执行失败"}
	if ee, ok := err.(*ExitError); ok {
		ue.Message = fmt.Sprintf("引擎退出码 %d", ee.Code)
	}
	ue.Detail = engineDetail(output)
	low := strings.ToLower(output)
	switch {
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
	case strings.Contains(low, "429"), strings.Contains(low, "too many requests"), strings.Contains(low, "rate limit"):
		ue.Code = "RATE_LIMITED"
		ue.Message = "平台限流，稍后自动重试"
	case strings.Contains(low, "timed out"), strings.Contains(low, "timeout"),
		strings.Contains(low, "connection reset"), strings.Contains(low, "connection error"),
		strings.Contains(low, "temporary failure in name resolution"), strings.Contains(low, "unable to download"):
		ue.Code = "NET_TIMEOUT"
		ue.Message = "网络连接失败或被重置，稍后自动重试"
	case strings.Contains(low, "http error 5"):
		ue.Code = "HTTP_5XX"
		ue.Message = "平台服务端错误，稍后自动重试"
	case strings.Contains(low, "http error 4"):
		ue.Code = "HTTP_4XX"
		ue.Message = "平台返回客户端错误，请检查链接与权限"
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
	case strings.Contains(k, "twitter"), strings.Contains(k, "x"):
		return "twitter"
	case strings.Contains(k, "instagram"):
		return "instagram"
	case strings.Contains(k, "netease"):
		return "netease"
	case strings.Contains(k, "instagram"):
		return "instagram"
	}
	host := strings.ToLower(url)
	switch {
	case strings.Contains(host, "youtube.com"), strings.Contains(host, "youtu.be"):
		return "youtube"
	case strings.Contains(host, "bilibili.com"), strings.Contains(host, "b23.tv"):
		return "bilibili"
	case strings.Contains(host, "music.163.com"):
		return "netease"
	case strings.Contains(host, "twitter.com"), strings.Contains(host, "x.com"):
		return "twitter"
	case strings.Contains(host, "instagram.com"):
		return "instagram"
	}
	return "web"
}
