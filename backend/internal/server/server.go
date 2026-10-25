package server

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/huluohu/raccoon/internal/ai"
	"github.com/huluohu/raccoon/internal/auth"
	"github.com/huluohu/raccoon/internal/config"
	"github.com/huluohu/raccoon/internal/credential"
	"github.com/huluohu/raccoon/internal/httpx"
	"github.com/huluohu/raccoon/internal/live"
	"github.com/huluohu/raccoon/internal/logx"
	"github.com/huluohu/raccoon/internal/monitor"
	"github.com/huluohu/raccoon/internal/sse"
	"github.com/huluohu/raccoon/internal/store"
	"github.com/huluohu/raccoon/internal/subscription"
	"github.com/huluohu/raccoon/internal/task"
	"github.com/huluohu/raccoon/internal/tgbot"
	"github.com/huluohu/raccoon/internal/web"
)

// Server 聚合依赖并注册路由。
type Server struct {
	cfg            *config.Config
	log            *logx.Logger
	store          *store.Store
	router         *Router
	start          time.Time
	webFS          fs.FS // 前端产物（内嵌，ADR-5）
	sessions       *auth.SessionStore
	done           chan struct{}         // 优雅停机信号（manager/auth GC 监听）
	hub            *sse.Hub              // SSE 广播（Wave 2）
	tasks          *task.Manager         // 任务系统 Manager（Wave 2）
	subs           *subscription.Service // 订阅服务（Wave 3）
	liveSvc        *live.Service         // 直播守护（开播自动录制）
	vault          *credential.Vault     // 凭据加密（Wave 3）
	shutdownCancel context.CancelFunc    // 停机时 cancel manager loop
	loginLimiter   *Limiter              // 登录限流（docs/04 §5：5 次/分钟/IP）
	parseLimiter   *Limiter              // 解析限流（20 次/分钟/IP）
	restartFn      func()                // 让出重启回调（main 注入）
	monitor        *monitor.Sampler      // 资源监控采样器（docs/15 §3.2）
	monitorCancel  context.CancelFunc    // 停机时停采样器
	credProbe      *credentialProbe      // 凭据健康探针（定期验证 Cookie 有效性）
	ai             *ai.Summarizer        // AI 视频总结与转录服务

	// Telegram 通道状态（docs/08 W4.8）。
	tgClient *tgbot.Client
	tgCancel context.CancelFunc
	tgMu     sync.Mutex
	tgChats  map[string]int64 // taskID → chatID（任务终态时定向通知）
}

// New 构建 Server：router + 中间件链 + 路由注册。
func New(cfg *config.Config, st *store.Store, logger *logx.Logger) (*Server, error) {
	sub, err := web.FS()
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, log: logger, store: st, router: NewRouter(RequestID, Recover), start: time.Now(), webFS: sub, done: make(chan struct{})}
	s.loginLimiter = NewLimiter(time.Minute, 5)
	s.parseLimiter = NewLimiter(time.Minute, 20)
	s.router.SetErrorHandler(httpx.WriteErr)
	// 凭据加密保管（主密钥在 data/keys/master.key）。
	vault, err := credential.OpenVault(cfg.GetString("storage.data_dir"))
	if err != nil {
		return nil, fmt.Errorf("初始化凭据加密: %w", err)
	}
	s.vault = vault
	s.setupAuth()
	s.setupTasks()
	s.setupConfig()
	s.setupCredentials()
	s.setupQRLogin()
	s.setupFiles()
	s.setupSubscriptions()
	s.setupLive()
	s.setupNotifications()
	s.setupTokens()
	s.setupIntegrations()
	s.setupSystem()
	s.setupStats()
	s.setupMonitor()
	s.setupLogs()
	s.setupPreferences()
	// 资源监控采样器：2s tick × 60 点内存环 + 每 60s 聚合落库（保留 7 天）。
	// 非 Linux 平台采集桩 ok=false，链路诚实降级（docs/15 §3.2）。
	monitorCtx, monitorCancel := context.WithCancel(context.Background())
	s.monitor = monitor.New(st, 2*time.Second, 60)
	s.monitorCancel = monitorCancel
	s.monitor.Start(monitorCtx)
	// 凭据健康探针：定期用平台 API 验证 Cookie 有效性，
	// 把「过期才发现」变成「过期前就提示」。间隔可配（0 = 关闭）。
	s.credProbe = newCredentialProbe(st, vault, defaultPlatformProbe,
		time.Duration(s.cfg.GetInt("credential.probe_interval_minutes"))*time.Minute, logger)
	s.credProbe.Start(context.Background())
	// AI 总结与转写服务
	s.ai = ai.NewSummarizer(cfg, st)
	s.registerRoutes()
	return s, nil
}

func (s *Server) registerRoutes() {
	// 根路径 /healthz 供容器 HEALTHCHECK；API 契约路径见 docs/04 §2.9。
	s.router.GET("/healthz", s.handleHealthz)
	s.router.GET("/api/v1/system/health", s.handleHealthz)
	s.router.GET("/api/v1/system/info", s.handleSystemInfo)
	// Wave 1–2 起按 docs/04 §2 注册：auth / tasks / parse / config ...
	s.router.Fallback(s.handleStatic)
}

// Handler 返回 http.Handler。
func (s *Server) Handler() http.Handler { return s.router }

// SetRestartFunc 注入让出重启回调（main 注入：退出进程由编排器拉起）。
func (s *Server) SetRestartFunc(fn func()) { s.restartFn = fn }

// ShutdownTasks 优雅停任务系统：cancel 调度循环 → 等待运行中 worker 让出（上限 timeout）。
func (s *Server) ShutdownTasks(timeout time.Duration) {
	if s.shutdownCancel != nil {
		s.shutdownCancel()
	}
	if s.tasks != nil {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		s.tasks.Stop(ctx)
	}
	// 停监控采样器并 flush 未满 1min 的部分聚合（重启/停机都走这里）。
	if s.monitor != nil {
		if s.monitorCancel != nil {
			s.monitorCancel()
		}
		s.monitor.Stop()
	}
	// 停凭据探针（避免停机后还在发探测请求）。
	if s.credProbe != nil {
		s.credProbe.Stop()
	}
	s.log.Info("任务系统已停止", nil)
}

// clientIP 取客户端 IP（限流键；反代 X-Forwarded-For 取第一段）。
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := indexByte(xff, ','); i > 0 {
			return trimSpace(xff[:i])
		}
		return trimSpace(xff)
	}
	return r.RemoteAddr
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) error {
	checks := map[string]any{}
	code := http.StatusOK

	if err := s.store.RW.PingContext(r.Context()); err != nil {
		checks["db"] = "unhealthy: " + err.Error()
		code = http.StatusServiceUnavailable
	} else {
		checks["db"] = "ok"
	}
	if free, total, err := diskUsage(s.cfg.GetString("storage.data_dir")); err == nil {
		pct := 0
		if total > 0 {
			pct = int((total - free) * 100 / total)
		}
		checks["disk_used_pct"] = pct
	}
	httpx.WriteJSON(w, code, map[string]any{"status": checks["db"] == "ok" && code == http.StatusOK, "checks": checks})
	return nil
}

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) error {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"version":  version,
		"go":       runtime.Version(),
		"arch":     runtime.GOOS + "/" + runtime.GOARCH,
		"uptime":   int64(time.Since(s.start).Seconds()),
		"data_dir": s.cfg.GetString("storage.data_dir"),
	})
	return nil
}

// handleStatic SPA 托管：静态文件直出，其余路径回 index.html；/api/* 不 fallback。
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) error {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		return httpx.NotFound("接口不存在")
	}
	fileServer := http.FileServer(http.FS(s.webFS))
	// 文件存在则直出，否则 SPI history 回 index.html。
	if f, err := s.webFS.Open(strings.TrimPrefix(r.URL.Path, "/")); err == nil {
		f.Close()
		fileServer.ServeHTTP(w, r)
		return nil
	}
	idx, err := web.IndexHTML()
	if err != nil {
		return httpx.NotFound("页面不存在")
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(idx)
	return nil
}

// version 由构建注入：-ldflags "-X raccoon/internal/server.version=x.y.z"
var version = "dev"

// sysStat 抽象 statfs 结果（linux/darwin 字段名相同，宽度不同）。
type sysStat struct {
	Bsize  uint64
	Blocks uint64
	Bavail uint64
}

func statFS(dir string, st *sysStat) error {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(dir, &fs); err != nil {
		return err
	}
	st.Bsize = uint64(fs.Bsize)
	st.Blocks = uint64(fs.Blocks)
	st.Bavail = uint64(fs.Bavail)
	return nil
}

// dataDir 返回数据目录绝对路径（不存在则创建）。
func diskUsage(dir string) (free, total uint64, err error) {
	var st sysStat
	if err = statFS(dir, &st); err != nil {
		return 0, 0, err
	}
	return st.Bavail * st.Bsize, st.Blocks * st.Bsize, nil
}

// dataDir 返回数据目录绝对路径（不存在则创建）。
func dataDir() string {
	dir := os.Getenv("RACCOON_DATA_DIR")
	if dir == "" {
		dir = "/data"
	}
	_ = os.MkdirAll(dir, 0o750)
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	return abs
}
