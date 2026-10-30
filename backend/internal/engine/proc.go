// Package engine：下载引擎抽象（Engine 接口）、子进程管理器与 yt-dlp 适配器。
// docs/05 §1：Worker 只依赖 Engine 接口；引擎可替换、可自检、可取消。
package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Progress 引擎进度回调（节流后由 worker 落库/广播）。
type Progress struct {
	Percent    float64 // 0–100；-1 = 未知（禁估算）
	Downloaded int64   // 字节
	Total      int64   // 字节；0 = 未知
	SpeedBps   int64
	ETASec     int64
	// 播放列表/合集任务的条目维度（单视频任务为 0）：
	// ItemIndex 当前第几个条目（1 起），ItemCount 总条目数。
	// worker 据此计算全局进度（(X-1 + 当前百分比)/Y），避免进度条
	// 在每个视频开始时清零回跳、被误读为「进度不对」。
	ItemIndex int
	ItemCount int
}

// DownloadedFile 引擎产物（绝对路径）。
type DownloadedFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// Engine 下载引擎契约。实现须满足：ctx 取消立即生效（杀进程组）；
// 错误分类见错误码前缀（网络类可重试，平台类不可重试）。
type Engine interface {
	Name() string
	Available() error // 启动自检：二进制存在且可执行
	Version() string  // 版本串（自检时获取）
	Resolve(ctx context.Context, url string, opts ResolveOpts) (*ResolveResult, error)
	Download(ctx context.Context, url string, opts DownloadOpts, onProgress func(Progress)) ([]DownloadedFile, error)
}

// ResolveOpts / DownloadOpts 引擎入参（由任务字段 + 配置快照构造）。
type (
	ResolveOpts struct {
		Cookies string // cookie 文件路径（凭据中心派生，可为空）
		Proxy   string // 网络代理（net.proxy，空=直连）。解析与下载必须同出口，否则会「解析成功、下载超时」
	}
	DownloadOpts struct {
		Cookies   string
		Format    string // 质量选择串（如 1080p 映射的 yt-dlp format 表达式）
		SaveDir   string
		NameTmpl  string // -o 模板
		RateLimit int64  // 0=不限
		// MaxFilesize 单文件体积上限（字节）；0=不限。
		//
		// 为什么需要（2026-10-30 加固）：此前没有任何体积上限，一个
		// 指向超大文件（或无限流）的 URL 能把磁盘写满——自托管场景下
		// 这会连带把数据库和凭据一起写坏。映射到 yt-dlp 的 --max-filesize，
		// 超限时引擎直接失败（可重试/可提示），而不是悄悄写满盘。
		MaxFilesize int64
		WriteDesc   bool
		EmbedSubs   bool
		HTTPProxy   string
		// WriteThumbnail 封面图落盘（--write-thumbnail；有 ffmpeg 时转 jpg）。
		WriteThumbnail bool
		// EmbedMetadata 标题/作者写入媒体文件（--embed-metadata）。
		EmbedMetadata bool
	}
)

// ResolveResult 元信息（POST /parse 与任务 parsing 阶段共用）。
type ResolveResult struct {
	Title      string
	Thumbnail  string
	Uploader   string
	Duration   int64 // 秒
	Platform   string
	IsPlaylist bool
	Formats    []FormatInfo
}

// FormatInfo 一个可选质量。
type FormatInfo struct {
	Quality string // 展示名：1080p / 720p / audio / best ...
	Size    *int64 // nil=未知（禁估算）
	Note    string
	// URL 该格式的**真实直链**（媒体 CDN，带签名有时效）。
	// Host 是它的主机名——「解析域名 ≠ 媒体域名」是「解析成功却下载失败」的主因，
	// 把域名摊开才能归因（2026-10-28）。
	URL  string
	Host string
}

// ProgressFn 进度回调类型别名。
type ProgressFn func(Progress)

// ---- 子进程管理器（Wave 1.6：exec + 进程组 + 取消 + 超时） ----

// pidFilePath 引擎子进程 PID 登记文件（孤儿回收用；nil=不登记）。
var (
	pidFilePath string
	pidFileMu   sync.Mutex
)

// SetPIDFile 设置 PID 登记文件路径（服务启动时调用）。
//
// 用途：进程被 kill -9 后，yt-dlp/ffmpeg 子进程会变成孤儿继续写盘。
// 启动时读该文件杀掉残留的引擎进程（只杀本服务拉起的）。
func SetPIDFile(path string) {
	pidFileMu.Lock()
	pidFilePath = path
	pidFileMu.Unlock()
}

// registerPID 登记子进程 PID。
func registerPID(pid int) {
	pidFileMu.Lock()
	path := pidFilePath
	pidFileMu.Unlock()
	if path == "" || pid <= 0 {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(strconv.Itoa(pid) + "\n")
}

// unregisterPID 注销子进程 PID（进程正常退出时清理文件）。
func unregisterPID(pid int) {
	pidFileMu.Lock()
	path := pidFilePath
	pidFileMu.Unlock()
	if path == "" || pid <= 0 {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(string(raw), "\n")
	keep := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || l == strconv.Itoa(pid) {
			continue
		}
		keep = append(keep, l)
	}
	_ = os.WriteFile(path, []byte(strings.Join(keep, "\n")), 0o600)
}

// lineWriter 把子进程的字节流按行喂给 onLine。
//
// 为什么自己实现而不是用 bufio.Scanner：cmd.Stdout 必须是一个 io.Writer，
// os/exec 会在内部起 goroutine 把管道内容拷进它；两个拷贝 goroutine
// （stdout / stderr）会并发调用 Write，所以这里要加锁——调用方传的 onLine
// 多半是往 strings.Builder 里追加，那不是并发安全的。
type lineWriter struct {
	mu     sync.Mutex
	onLine func(string)
	buf    []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(w.buf[:i]), "\r")
		w.buf = w.buf[i+1:]
		if w.onLine != nil {
			w.onLine(line)
		}
	}
	return len(p), nil
}

// flush 吐出末尾没有换行的那一行（引擎最后一行常不带 \n）。
func (w *lineWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) == 0 {
		return
	}
	line := strings.TrimRight(string(w.buf), "\r\n")
	w.buf = nil
	if w.onLine != nil && line != "" {
		w.onLine(line)
	}
}

// RunCmd 以新进程组运行命令；ctx 取消时杀整个进程组（含 yt-dlp 派生的 ffmpeg）。
// stdout 逐行回调 onLine；stderr 合并进同一回调（yt-dlp 进度走 stderr）。
// dir 非空时作为子进程工作目录（yt-dlp 的 -o 相对模板据此落盘，必须显式传
// SaveDir，否则文件会落到服务进程 cwd）。
//
// 输出捕获用 cmd.Stdout/cmd.Stderr = writer，**不用 StdoutPipe**：
// StdoutPipe 的官方约定是「必须读完再 Wait」，而 Wait 会关闭管道——
// 顺序反了就会吞掉快退进程的输出，解析侧拿到空字符串，报成
// 「解析引擎输出失败: unexpected end of JSON input」（链接其实完全正常）。
// 交给 os/exec 自己拷贝时，Wait 会等拷贝完成，输出不会丢，也没有多余的
// 扫描 goroutine 与空等（旧实现每次调用恒定多花 2 秒，且泄漏一个 goroutine）。
func RunCmd(ctx context.Context, bin string, args []string, onLine func(string), dir string) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Cancel = func() error {
		// 杀负 PGID = 整个进程组；进程已退出返回 ProcessLookup（忽略）。
		if cmd.Process == nil {
			return nil
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
		return nil
	}
	// Cancel 后兜底强杀；另外兜住「子进程退出但管道仍被孙进程持有」
	// （yt-dlp 派生的 ffmpeg）：最多再等 10 秒就放弃剩余输出。
	cmd.WaitDelay = 10 * time.Second

	lw := &lineWriter{onLine: onLine}
	cmd.Stdout = lw
	cmd.Stderr = lw

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 %s 失败: %w", bin, err)
	}
	registerPID(cmd.Process.Pid)
	defer unregisterPID(cmd.Process.Pid)

	waitErr := cmd.Wait()
	lw.flush()
	if waitErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			return &ExitError{Code: ee.ExitCode(), Err: waitErr}
		}
		return waitErr
	}
	return nil
}

// ExitError 子进程非零退出。
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }
func (e *ExitError) Unwrap() error { return e.Err }

// LookPath 查找引擎二进制（PATH 查找，可注入自定义路径）。
func LookPath(bin, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	p, err := exec.LookPath(bin)
	if err != nil {
		return "", fmt.Errorf("未找到 %s 可执行文件: %w", bin, err)
	}
	return p, nil
}

// urlHost 取 URL 的主机名（去掉端口）；解析不出就返回空串。
//
// 用途：把「媒体直链来自哪个 CDN」摊开给人看——解析域名（youtube.com）与
// 媒体域名（googlevideo.com）往往不同，下载失败十有八九出在后者上。
func urlHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
