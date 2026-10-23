package server

// stats_handler.go：主页仪表盘统计端点（docs/04 §2.10-a/b / docs/15 §3.1）。
//
// 口径纪律见 store/stats.go：今日/本周用进程本地时区算边界；success_rate
// 分母 0 时 store 层返回 nil，此处原样透传（前端显示「—」，不造假）。

import (
	"net/http"
	"strconv"
	"time"

	"github.com/huluohu/raccoon/internal/httpx"
)

// setupStats 注册统计路由。
func (s *Server) setupStats() {
	s.router.GET("/api/v1/system/stats/summary", s.requireAuth(s.handleStatsSummary))
	s.router.GET("/api/v1/system/stats/trend", s.requireAuth(s.handleStatsTrend))
}

// handleStatsSummary 主页指标卡一次聚合（uptime 由进程启动时间现算）。
func (s *Server) handleStatsSummary(w http.ResponseWriter, r *http.Request) error {
	stats, err := s.store.SummaryStats(time.Now())
	if err != nil {
		return httpx.Internal("统计聚合失败")
	}
	stats.UptimeSec = int64(time.Since(s.start).Seconds())
	httpx.WriteJSON(w, http.StatusOK, stats)
	return nil
}

// handleStatsTrend 下载趋势 + E5 失败拆分（?days=7|30，默认 7，其它 400）。
//
// points 长度恒等于 days（无数据日补 0，不断轴），末元素为本地今日；
// failures 是同一时间窗内 failed 任务按平台/错误码的聚合（降序，各上限 10）。
func (s *Server) handleStatsTrend(w http.ResponseWriter, r *http.Request) error {
	days := 7
	if raw := r.URL.Query().Get("days"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || (v != 7 && v != 30) {
			return httpx.Validation("days 仅支持 7 或 30")
		}
		days = v
	}

	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	fromStart := todayStart.AddDate(0, 0, -(days - 1))
	toEnd := todayStart.AddDate(0, 0, 1)

	byDate, err := s.store.DailyDoneStats(fromStart, toEnd)
	if err != nil {
		return httpx.Internal("趋势聚合失败")
	}
	// 补 0：连续日期序列（缺数据日 count=0，不断轴）。
	points := make([]map[string]any, 0, days)
	for d := fromStart; d.Before(toEnd); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		st := byDate[key]
		points = append(points, map[string]any{
			"date":  key,
			"count": st.Count,
			"bytes": st.Bytes,
		})
	}

	failures, err := s.store.FailureBreakdown(fromStart, toEnd)
	if err != nil {
		return httpx.Internal("失败拆分聚合失败")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"days":   days,
		"points": points,
		"failures": map[string]any{
			"by_platform": failures.ByPlatform,
			"by_reason":   failures.ByReason,
		},
	})
	return nil
}

// parseBoundedInt 解析查询参数整数，越界/非法回落默认值。
func parseBoundedInt(raw string, def, lo, hi int) int {
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < lo || v > hi {
		return def
	}
	return v
}
