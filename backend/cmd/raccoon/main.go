// Command raccoon 是自托管多媒体下载中心「浣熊」的服务端入口。
// 运行：raccoon [serve] [-addr :8530] ；数据目录由 RACCOON_DATA_DIR（默认 /data）决定。
package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/huluohu/raccoon/internal/config"
	"github.com/huluohu/raccoon/internal/engine"
	"github.com/huluohu/raccoon/internal/logx"
	"github.com/huluohu/raccoon/internal/server"
	"github.com/huluohu/raccoon/internal/store"
)

// execCommand 执行外部命令取输出（仅用于 macOS 上 ps 回退）。
func execCommand(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return string(out), err
}

// pidFile 记录本进程 PID；启动时据此回收上次残留的引擎子进程。
const pidFile = "raccoon.pid"

func main() {
	// 子命令剥离：`raccoon serve --addr X` 里 flag 包会在遇到非 flag 参数 "serve" 时停止解析，
	// 后面的 --addr 会被静默丢弃（踩过一次：传了 18551 仍起在默认 8530）。
	// 这里把可选的 serve 子命令摘掉，再交给 flag 包解析剩余参数。
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "serve" {
		args = args[1:]
	}
	fs := flag.NewFlagSet("raccoon", flag.ExitOnError)
	addr := fs.String("addr", "", "HTTP 监听地址（默认取配置 server.addr，可由 RACCOON_SERVER_ADDR 覆盖）")
	_ = fs.Parse(args)

	logger := logx.Default("main")
	cfg := config.New()

	dataDir := cfg.GetString("storage.data_dir")
	if dataDir == "" {
		dataDir = "/data"
	}

	// 日志级别来自配置（immediate 类）。
	switch cfg.GetString("log.level") {
	case "debug":
		logger = logx.New(os.Stdout, "main", logx.LevelDebug)
	}

	st, err := store.Open(dataDir + "/raccoon.db")
	if err != nil {
		logger.Error("数据库初始化失败", map[string]any{"err": err.Error()})
		os.Exit(1)
	}
	defer st.Close()
	logger.Info("数据库就绪", map[string]any{"path": dataDir + "/raccoon.db"})

	// 配置持久化加载（Wave 1.2：settings 表覆盖默认值；env 锁定优先）。
	if err := cfg.LoadFromStore(st.RW); err != nil {
		logger.Error("加载持久化配置失败", map[string]any{"err": err.Error()})
		os.Exit(1)
	}

	// --addr 必须在持久化配置加载之后再应用：显式 flag 优先级高于 settings 表，
	// 否则命令行传址会被上次存下的 server.addr 静默覆盖（踩过一次：传了端口仍起在 8530）。
	if *addr != "" {
		_ = cfg.Set("server.addr", *addr)
	}

	// 孤儿引擎进程回收：上次进程被 kill -9 后 yt-dlp/ffmpeg 会残留并继续写盘。
	// 启动时按 PID 文件杀掉上次遗留的引擎子进程（仅限本服务拉起的）。
	reapOrphanEngines(dataDir, logger)
	writePidFile(dataDir, logger)
	// 引擎子进程 PID 登记（供下次启动回收）。
	engine.SetPIDFile(dataDir + "/engine.pids")

	srv, err := server.New(cfg, st, logger.With("server"))
	if err != nil {
		logger.Error("服务初始化失败", map[string]any{"err": err.Error()})
		os.Exit(1)
	}
	// 让出重启：停领新任务 → 运行中任务让出 → 退出进程（由容器编排器拉起）。
	srv.SetRestartFunc(func() {
		logger.Info("让出重启：退出进程，等待编排器拉起", nil)
		_ = os.Remove(dataDir + "/" + pidFile)
		os.Exit(0)
	})
	httpServer := &http.Server{
		Addr:              cfg.GetString("server.addr"),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("HTTP 服务启动", map[string]any{"addr": cfg.GetString("server.addr")})
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP 服务异常退出", map[string]any{"err": err.Error()})
			os.Exit(1)
		}
	}()

	// 优雅退出：SIGINT/SIGTERM → 停机 manager（让出运行中任务）→ 停止接收新连接 → 等待在途请求（最多 15s）。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	logger.Info("收到退出信号，优雅关闭中", nil)
	// manager 让出窗口：停领新任务，运行中任务最多等 20s（下载可被取消语义）。
	srv.ShutdownTasks(20 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("优雅关闭超时", map[string]any{"err": err.Error()})
	}
	_ = os.Remove(dataDir + "/" + pidFile)
	logger.Info("已退出", nil)
}

// writePidFile 写 PID 文件（供下次启动回收孤儿引擎进程）。
func writePidFile(dataDir string, logger *logx.Logger) {
	if err := os.WriteFile(dataDir+"/"+pidFile, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		logger.Warn("PID 文件写入失败", map[string]any{"err": err.Error()})
	}
}

// reapOrphanEngines 回收上次残留的引擎子进程。
//
// 策略：读取上次记录的引擎子进程 PID 列表，杀掉仍存活的。
// 只杀本服务拉起的（PID 列表由本服务写），不扫全机进程（安全纪律）。
func reapOrphanEngines(dataDir string, logger *logx.Logger) {
	path := dataDir + "/engine.pids"
	raw, err := os.ReadFile(path)
	if err != nil {
		return // 无记录：首次启动或已清理
	}
	killed := 0
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err != nil || pid <= 1 {
			continue
		}
		// 先确认进程存在且是我们拉起的引擎（命令名匹配 yt-dlp/ffmpeg）。
		name, ok := processName(pid)
		if !ok {
			continue // 进程已退出
		}
		if !isEngineProcess(name) {
			continue // PID 已被系统复用成别的进程，不动
		}
		if err := syscall.Kill(pid, syscall.SIGKILL); err == nil {
			killed++
		}
	}
	_ = os.Remove(path)
	if killed > 0 {
		logger.Info("已回收上次残留的引擎子进程", map[string]any{"count": killed})
	}
}

// processName 取进程名（Linux /proc，darwin 用 ps 回退）。
func processName(pid int) (string, bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
	if err == nil {
		return strings.TrimSpace(string(data)), true
	}
	// macOS：ps -p <pid> -o comm=
	out, err := execCommand("ps", "-p", strconv.Itoa(pid), "-o", "comm=")
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(out), true
}

// isEngineProcess 判断是否引擎进程（只回收这些，避免误杀）。
func isEngineProcess(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "yt-dlp") || strings.Contains(n, "yt_dlp") ||
		strings.Contains(n, "ffmpeg") || strings.Contains(n, "gallery-dl")
}
