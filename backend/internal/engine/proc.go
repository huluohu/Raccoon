// Package engine：下载引擎抽象（Engine 接口）、子进程管理器与 yt-dlp 适配器。
// docs/05 §1：Worker 只依赖 Engine 接口；引擎可替换、可自检、可取消。
package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
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
	}
	DownloadOpts struct {
		Cookies   string
		Format    string // 质量选择串（如 1080p 映射的 yt-dlp format 表达式）
		SaveDir   string
		NameTmpl  string // -o 模板
		RateLimit int64  // 0=不限
		WriteDesc bool
		EmbedSubs bool
		HTTPProxy string
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

// RunCmd 以新进程组运行命令；ctx 取消时杀整个进程组（含 yt-dlp 派生的 ffmpeg）。
// stdout 逐行回调 onLine；stderr 合并进同一扫描器（yt-dlp 进度走 stderr）。
// dir 非空时作为子进程工作目录（yt-dlp 的 -o 相对模板据此落盘，必须显式传
// SaveDir，否则文件会落到服务进程 cwd）。
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
	cmd.WaitDelay = 10 * time.Second // Cancel 后兜底强杀

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 %s 失败: %w", bin, err)
	}
	registerPID(cmd.Process.Pid)
	defer unregisterPID(cmd.Process.Pid)
	lines := make(chan string, 64)
	scan := func(r io.Reader) {
		defer func() { _ = recover() }()
		br := bufio.NewReaderSize(r, 1<<16)
		for {
			line, err := br.ReadString('\n')
			if line != "" {
				select {
				case lines <- strings.TrimRight(line, "\r\n"):
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}
	go scan(stdout)
	go scan(stderr)
	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		for {
			select {
			case ln, ok := <-lines:
				if !ok {
					return
				}
				if onLine != nil {
					onLine(ln)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	waitErr := cmd.Wait()
	// 等扫描器排空（上限 2s，防丢失尾部行）。
	select {
	case <-scanDone:
	case <-time.After(2 * time.Second):
	}
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
