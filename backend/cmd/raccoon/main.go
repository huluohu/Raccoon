// Command raccoon 是自托管多媒体下载中心「浣熊」的服务端入口。
// 运行：raccoon [serve] [-addr :8530] ；数据目录由 RACCOON_DATA_DIR（默认 /data）决定。
package main

import (
	"context"
	"errors"
	"flag"
	"io"
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

// enginePIDFile 登记本服务拉起的引擎子进程（yt-dlp/ffmpeg/gallery-dl）PID。
// 进程被 kill -9 时来不及注销，这个文件就是下次启动时的清理依据。
const enginePIDFile = "engine.pids"

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
	minLv := logx.LevelInfo
	if cfg.GetString("log.level") == "debug" {
		minLv = logx.LevelDebug
	}

	// 双写：stdout（docker logs 照旧可看）+ 运行日志文件（/api/v1/logs 页面读它）。
	// 文件打不开不致命——降级为仅 stdout，但要在 stdout 留痕，否则日志页空着没人知道为什么。
	var logSink io.WriteCloser
	if sink, err := logx.OpenSink(logx.FilePath(dataDir)); err != nil {
		logger = logx.New(os.Stdout, "main", minLv)
		logger.Warn("运行日志文件不可写，日志页将为空", map[string]any{"err": err.Error()})
	} else {
		logSink = sink
		logger = logx.New(io.MultiWriter(os.Stdout, sink), "main", minLv)
	}
	if logSink != nil {
		defer func() { _ = logSink.Close() }()
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
	engine.SetPIDFile(dataDir + "/" + enginePIDFile)

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
// 策略：读取上次记录的引擎子进程 PID 列表，只杀仍存活且身份可确认为引擎的。
// 只杀本服务拉起的（PID 列表由本服务写），不扫全机进程（安全纪律）。
//
// 三种判定结果必须分开记账，因为失败含义完全不同：
//   - gone       进程已退出 → 正常，无需处理
//   - notEngine  PID 被系统复用成别的进程 → 必须跳过，否则误杀无关进程
//   - unknown    无法探测进程身份 → 也跳过（安全默认），但必须告警，
//     否则会出现「回收静默失效、孤儿进程继续写盘占带宽」而无人察觉
func reapOrphanEngines(dataDir string, logger *logx.Logger) {
	path := dataDir + "/" + enginePIDFile
	raw, err := os.ReadFile(path)
	if err != nil {
		return // 无记录：首次启动或已清理
	}
	var killed, gone, notEngine, unknown []int
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err != nil || pid <= 1 {
			continue
		}
		name, ok := processName(pid)
		switch {
		case !ok:
			gone = append(gone, pid)
		case !isEngineProcess(name):
			notEngine = append(notEngine, pid)
		default:
			if err := syscall.Kill(pid, syscall.SIGKILL); err == nil {
				killed = append(killed, pid)
			} else {
				unknown = append(unknown, pid)
			}
		}
	}
	// 无论回收结果如何都清空登记文件：这些 PID 属于上一个进程生命周期，
	// 留着会让下次启动重复处理（也可能命中已被系统复用的新进程）。
	_ = os.Remove(path)

	if len(killed) > 0 {
		logger.Info("已回收上次残留的引擎子进程", map[string]any{"pids": killed})
	}
	if len(unknown) > 0 {
		// 探测不到身份或 kill 失败：如实告警，不假装回收成功。
		logger.Warn("部分残留引擎进程未能回收（无法确认进程身份或 kill 失败，可能继续占用磁盘与带宽）",
			map[string]any{"pids": unknown})
	}
	if len(notEngine) > 0 {
		// PID 复用防护生效的证据，留痕便于排查。
		logger.Info("跳过疑似 PID 复用进程（非引擎，未动）", map[string]any{"pids": notEngine})
	}
}

// isEngineProcess 判断是否引擎进程（只回收这些，避免误杀）。
func isEngineProcess(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "yt-dlp") || strings.Contains(n, "yt_dlp") ||
		strings.Contains(n, "ffmpeg") || strings.Contains(n, "gallery-dl")
}
