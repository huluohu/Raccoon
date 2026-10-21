// Command raccoon 是自托管多媒体下载中心「浣熊」的服务端入口。
// 运行：raccoon [-addr :8530] ；数据目录由 RACCOON_DATA_DIR（默认 /data）决定。
package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/huluohu/raccoon/internal/config"
	"github.com/huluohu/raccoon/internal/logx"
	"github.com/huluohu/raccoon/internal/server"
	"github.com/huluohu/raccoon/internal/store"
)

func main() {
	addr := flag.String("addr", "", "HTTP 监听地址（默认取配置 server.addr，可由 RACCOON_SERVER_ADDR 覆盖）")
	flag.Parse()

	logger := logx.Default("main")
	cfg := config.New()

	// 配置优先级：默认值 < Flag/环境变量锁定。flag 显式指定时覆盖运行期（等同 env 锁定语义的快捷方式）。
	if *addr != "" {
		_ = cfg.Set("server.addr", *addr)
	}

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

	srv, err := server.New(cfg, st, logger.With("server"))
	if err != nil {
		logger.Error("服务初始化失败", map[string]any{"err": err.Error()})
		os.Exit(1)
	}
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
	logger.Info("已退出", nil)
}
