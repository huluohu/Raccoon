package server

// monitor_handler.go：资源监控端点（docs/14 §2.3 / docs/15 §3.2）。
//
// 诚实原则（docs/01）：非 Linux 平台 sampler 采集 ok=false，/system/monitor
// 直接返回降级读模型（ok=false + message），前端显示「当前平台不支持」，
// 不渲染假图表。

import (
	"net/http"
	"strings"
	"time"

	"github.com/huluohu/raccoon/internal/httpx"
	"github.com/huluohu/raccoon/internal/store"
)

// setupMonitor 注册监控路由。
func (s *Server) setupMonitor() {
	s.router.GET("/api/v1/system/monitor", s.requireAuth(s.handleMonitor))
	s.router.GET("/api/v1/system/monitor/history", s.requireAuth(s.handleMonitorHistory))
}

// handleMonitor 实时读模型（瞬时值 + 60 点序列；断点为 null）。
func (s *Server) handleMonitor(w http.ResponseWriter, r *http.Request) error {
	if s.monitor == nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"ok":      false,
			"message": "监控采样器未启用",
		})
		return nil
	}
	httpx.WriteJSON(w, http.StatusOK, s.monitor.Snapshot())
	return nil
}

// handleMonitorHistory 历史视图（?range=1h|24h，默认 1h；分钟聚合点）。
func (s *Server) handleMonitorHistory(w http.ResponseWriter, r *http.Request) error {
	rangeKey := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("range")))
	var dur time.Duration
	switch rangeKey {
	case "", "1h":
		dur = time.Hour
	case "24h":
		dur = 24 * time.Hour
	default:
		return httpx.Validation("range 仅支持 1h / 24h")
	}
	to := time.Now().Unix()
	from := to - int64(dur.Seconds())
	rows, err := s.store.MonitorHistory(from, to)
	if err != nil {
		return httpx.Internal("监控历史查询失败")
	}
	// 空窗期返回空数组（前端画空态，不补 0 造假）。
	if rows == nil {
		rows = []store.MonitorSampleRow{}
	}
	// granularity_sec：1h 视图 = 60s 原值点，24h 视图 = 1440s（24min）均值点。
	granularity := int64(60)
	if dur == 24*time.Hour {
		granularity = 1440
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"range":           rangeKey,
		"granularity_sec": granularity,
		"points":          rows,
	})
	return nil
}
