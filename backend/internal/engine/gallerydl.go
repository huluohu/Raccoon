package engine

// gallerydl.go：gallery-dl 适配器（docs/08 W4.2 图片/图集/小红书/X 等站点）。
//
// 与 yt-dlp 适配器的差异（决定了这里的实现取舍）：
//   - gallery-dl 的元信息走 `--dump-json`（一行一个 JSON 对象，非 yt-dlp 的单对象）；
//   - 进度输出格式是 `%|total|%|now|%|speed|%|eta|`，与 yt-dlp 的 progress-template 不同；
//   - gallery-dl 按「一次调用多文件」工作（一个帖子 = 多个图），因此下载产物是集合。
//
// 能力边界：gallery-dl 依赖站点 extractor 覆盖度，登录态由 --cookies-from-browser 或
// 配置文件提供；本适配器只做「URL → 元信息 / 文件」的受管子进程封装，不猜站点行为。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// GalleryDL 是 gallery-dl 引擎实现。
type GalleryDL struct {
	Bin string

	mu       sync.Mutex
	version  string
	checkErr error
}

// NewGalleryDL 构造；二进制缺失不在此报错，由 Available() 统一自检（503 语义）。
func NewGalleryDL(bin string) *GalleryDL {
	if bin == "" {
		bin = Lookup("gallery-dl")
	}
	return &GalleryDL{Bin: bin}
}

func (g *GalleryDL) Name() string { return "gallery-dl" }

// Version 返回 gallery-dl 版本串（未自检时触发一次探测）。
func (g *GalleryDL) Version() string {
	_ = g.Available()
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.version
}

// Available 启动自检：二进制存在且可执行。缺失时上层转 503 ENGINE_UNAVAILABLE。
func (g *GalleryDL) Available() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.checkErr != nil {
		return g.checkErr
	}
	if g.version != "" {
		return nil
	}
	var lines []string
	if err := RunCmd(context.Background(), g.Bin, []string{"--version"}, func(l string) {
		if len(lines) < 2 {
			lines = append(lines, l)
		}
	}, ""); err != nil {
		g.checkErr = &UnavailableError{Engine: "gallery-dl", Reason: "无法执行: " + err.Error()}
		return g.checkErr
	}
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		g.checkErr = &UnavailableError{Engine: "gallery-dl", Reason: "版本输出为空"}
		return g.checkErr
	}
	g.version = strings.TrimSpace(lines[0])
	return nil
}

// ---- Resolve ----

// galleryJSON 只取适配器关心的字段（gallery-dl 单条记录字段很多，不建全量模型）。
type galleryJSON struct {
	Category     string `json:"category"`
	Title        string `json:"title"`
	Count        int    `json:"count"`
	NumImages    int    `json:"num_images"`
	NumVideos    int    `json:"num_videos"`
	NumPages     int    `json:"num_pages"`
	Date         string `json:"date"`
	Uploader     string `json:"uploader"`
	Username     string `json:"username"`
	Tags         []string
	Files        []galleryFile `json:"files"`
	Post         *galleryPost  `json:"post"`
	ExtractorKey string        `json:"extractor"`
	Image        string        `json:"image"`
	Thumbnail    string        `json:"thumbnail"`
}

type galleryPost struct {
	Title    string `json:"title"`
	Author   string `json:"author"`
	Category string `json:"category"`
	Date     string `json:"date"`
	ID       string `json:"id"`
}

type galleryFile struct {
	URL       string `json:"url"`
	Path      string `json:"path"`
	Filename  string `json:"filename"`
	Extension string `json:"extension"`
	Size      int64  `json:"size"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

// Resolve 拉元信息（--dump-json，多行 JSON 逐条解析）。
func (g *GalleryDL) Resolve(ctx context.Context, url string, opts ResolveOpts) (*ResolveResult, error) {
	args := []string{"--dump-json", "--no-input", "--quiet"}
	if opts.Cookies != "" {
		args = append(args, "--cookies", opts.Cookies)
	}
	args = append(args, url)

	var raw strings.Builder
	err := RunCmd(ctx, g.Bin, args, func(l string) {
		raw.WriteString(l)
		raw.WriteByte('\n')
	}, "")
	if err != nil {
		return nil, classify(err, raw.String())
	}
	recs, err := parseGalleryRecords(raw.String())
	if err != nil {
		return nil, &EngineError{Code: "PARSE_FAILED", Message: "gallery-dl 解析输出失败: " + err.Error()}
	}
	if len(recs) == 0 {
		return nil, &EngineError{Code: "NOT_FOUND", Message: "gallery-dl 未解析到任何内容（链接无效或站点不支持）"}
	}

	// 一条记录 = 一个条目（多图帖会产出多条）。取首条做主信息，其余计入数量。
	first := recs[0]
	res := &ResolveResult{
		Title:    first.postTitle(),
		Uploader: first.postUploader(),
		Platform: platformOf(first.extractorName(), url),
	}
	if len(recs) > 1 {
		res.IsPlaylist = true
	}
	// 质量项：gallery-dl 不暴露多质量档，只提供「原图」一档（大小未知时显未知）。
	var total int64
	sizeKnown := true
	for _, r := range recs {
		for _, f := range r.Files {
			if f.Size > 0 {
				total += f.Size
			} else {
				sizeKnown = false
			}
		}
	}
	if total > 0 && sizeKnown {
		sz := total
		res.Formats = []FormatInfo{{Quality: "original", Size: &sz, Note: fmt.Sprintf("%d 个文件", len(recs))}}
	} else {
		res.Formats = []FormatInfo{{Quality: "original", Size: nil, Note: fmt.Sprintf("%d 个文件", len(recs))}}
	}
	return res, nil
}

// parseGalleryRecords 解析 --dump-json 输出：每行一个 JSON 对象，容忍混杂的日志行。
func parseGalleryRecords(out string) ([]galleryJSON, error) {
	var recs []galleryJSON
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var d galleryJSON
		if err := json.Unmarshal([]byte(line), &d); err != nil {
			continue // 单行坏数据不阻塞整批
		}
		recs = append(recs, d)
	}
	if len(recs) == 0 {
		return nil, errors.New("no JSON record found")
	}
	return recs, nil
}

func (r galleryJSON) postTitle() string {
	if r.Post != nil && r.Post.Title != "" {
		return r.Post.Title
	}
	if r.Title != "" {
		return r.Title
	}
	return firstNonEmpty(r.Username, r.Uploader, r.Category)
}

func (r galleryJSON) postUploader() string {
	if r.Post != nil && r.Post.Author != "" {
		return r.Post.Author
	}
	return firstNonEmpty(r.Uploader, r.Username)
}

func (r galleryJSON) extractorName() string {
	if r.ExtractorKey != "" {
		return r.ExtractorKey
	}
	return r.Category
}

// ---- Download ----

const galleryProgressPrefix = "PROGRESS|"

// Download 执行下载。gallery-dl 一次调用产出多个文件，产物为集合。
func (g *GalleryDL) Download(ctx context.Context, url string, opts DownloadOpts, onProgress func(Progress)) ([]DownloadedFile, error) {
	if err := os.MkdirAll(opts.SaveDir, 0o755); err != nil {
		return nil, &EngineError{Code: "IO", Message: "创建保存目录失败: " + err.Error()}
	}
	before := dirSnapshot(opts.SaveDir)

	args := []string{
		"--no-input",
		"--quiet",
		"--dest", opts.SaveDir,
		// 进度行格式：%|total|%|now|%|speed|%|eta| —— 与 yt-dlp 不同，故单独解析。
		"--option", "progress.hook=PROGRESS|%(total)d|%(now)d|%(speed)d|%(eta)d",
	}
	if opts.Cookies != "" {
		args = append(args, "--cookies", opts.Cookies)
	}
	if opts.RateLimit > 0 {
		args = append(args, "--option", "rate="+strconv.FormatInt(opts.RateLimit/1024, 10)+"K")
	}
	args = append(args, url)

	var tail []string
	err := RunCmd(ctx, g.Bin, args, func(line string) {
		if strings.HasPrefix(line, galleryProgressPrefix) {
			if p, ok := parseGalleryProgress(line); ok && onProgress != nil {
				onProgress(p)
			}
			return
		}
		tail = append(tail, line)
		if len(tail) > 40 {
			tail = tail[1:]
		}
	}, opts.SaveDir)
	if err != nil {
		return nil, classify(err, strings.Join(tail, "\n"))
	}

	files := newFilesSince(opts.SaveDir, before)
	if len(files) == 0 {
		return nil, &EngineError{Code: "NO_OUTPUT", Message: "gallery-dl 退出成功但未发现产物文件"}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// parseGalleryProgress 解析 gallery-dl 进度行；total 未知时 percent 记 -1（禁估算）。
func parseGalleryProgress(line string) (Progress, bool) {
	parts := strings.Split(strings.TrimSpace(line), "|")
	if len(parts) != 5 {
		return Progress{}, false
	}
	p := Progress{Percent: -1}
	p.Total = atoiOr0(parts[1])
	p.Downloaded = atoiOr0(parts[2])
	p.SpeedBps = atoiOr0(parts[3])
	p.ETASec = atoiOr0(parts[4])
	if p.Total > 0 {
		p.Percent = float64(p.Downloaded) / float64(p.Total) * 100
	}
	return p, true
}

// GalleryFilename 推导 gallery-dl 落盘文件名（供 UI 展示预期产物，best-effort）。
func GalleryFilename(dir, name string) string {
	if name == "" {
		return ""
	}
	return filepath.Join(dir, name)
}
