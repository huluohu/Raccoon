package server

import (
	"context"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/huluohu/raccoon/internal/auth"
	"github.com/huluohu/raccoon/internal/config"
	"github.com/huluohu/raccoon/internal/httpx"
	"github.com/huluohu/raccoon/internal/logx"
	"github.com/huluohu/raccoon/internal/sse"
	"github.com/huluohu/raccoon/internal/store"
	"github.com/huluohu/raccoon/internal/task"
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
	done           chan struct{}      // 优雅停机信号（manager/auth GC 监听）
	hub            *sse.Hub           // SSE 广播（Wave 2）
	tasks          *task.Manager      // 任务系统 Manager（Wave 2）
	shutdownCancel context.CancelFunc // 停机时 cancel manager loop
	loginLimiter   *Limiter           // 登录限流（docs/04 §5：5 次/分钟/IP）
	parseLimiter   *Limiter           // 解析限流（20 次/分钟/IP）
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
	s.setupAuth()
	s.setupTasks()
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
