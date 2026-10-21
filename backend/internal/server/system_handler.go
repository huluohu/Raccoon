package server

// system_handler.go：系统端点（docs/04 §2.9 system/health/info/restart/diag）。
//
// /system/restart = 进程内让出重启：停领新任务 → 运行中任务让出（≤30s）→ 恢复队列。
// 不重启宿主容器，不挂 Docker socket（docs/02 §7）。

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/huluohu/raccoon/internal/httpx"
)

// setupSystem 注册系统路由。
func (s *Server) setupSystem() {
	s.router.POST("/api/v1/system/restart", s.requireAuth(s.csrfForSession(s.handleSystemRestart)))
	s.router.GET("/api/v1/system/diag/export", s.requireAuth(s.handleDiagExport))
	s.router.GET("/api/v1/system/queue", s.requireAuth(s.handleQueueStats))
}

// handleSystemRestart 让出重启：停领新任务 → 等运行中任务让出 → 触发进程重启回调。
func (s *Server) handleSystemRestart(w http.ResponseWriter, r *http.Request) error {
	if s.restartFn == nil {
		return &httpx.Error{HTTP: http.StatusNotImplemented, Code: "NOT_IMPLEMENTED",
			Message: "当前部署方式不支持应用内重启，请用容器编排重启"}
	}
	// 停领新任务（调度循环退出）。
	if s.shutdownCancel != nil {
		s.shutdownCancel()
	}
	// 等运行中任务让出（上限 30s，docs/05 §6）。
	waited := 0
	for waited < 30 {
		if s.tasks == nil || s.tasks.RunningCount() == 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
		waited++
	}
	running := 0
	if s.tasks != nil {
		running = s.tasks.RunningCount()
	}
	s.log.Info("让出重启：停领新任务，等待运行中任务让出", map[string]any{"waited_sec": waited / 2, "running": running})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"waited_sec":    waited / 2,
		"still_running": running,
		"message":       "已停领新任务，正在退出进程由编排器拉起",
	})
	// 异步触发退出（先让响应落地）。
	go func() {
		time.Sleep(300 * time.Millisecond)
		s.restartFn()
	}()
	return nil
}

// handleQueueStats 队列/状态统计（总览页数据源）。
func (s *Server) handleQueueStats(w http.ResponseWriter, r *http.Request) error {
	counts, err := s.store.CountTasksByStatus()
	if err != nil {
		return httpx.Internal("队列统计失败")
	}
	total, _ := s.store.CountTasks()
	mediaCount, _ := s.store.CountMediaFiles()
	mediaSize, _ := s.store.MediaTotalSize()
	running := 0
	if s.tasks != nil {
		running = s.tasks.RunningCount()
	}
	unread, _ := s.store.CountUnreadNotifications()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"counts":       counts,
		"total_tasks":  total,
		"running":      running,
		"media_count":  mediaCount,
		"media_bytes":  mediaSize,
		"unread_notifs": unread,
	})
	return nil
}

// handleDiagExport 诊断包导出（脱敏日志 + 配置快照 + 任务统计）。
//
// ?preview=1 返回预览（不生成文件）；正式导出返回 zip 字节流。
func (s *Server) handleDiagExport(w http.ResponseWriter, r *http.Request) error {
	preview := r.URL.Query().Get("preview") == "1"

	// 配置快照（敏感项打码）。
	configSnap := map[string]any{}
	items, _ := s.store.ListSettings()
	for _, row := range items {
		if row.Sensitive {
			configSnap[row.Key] = "***"
			continue
		}
		configSnap[row.Key] = row.ValueJSON
	}

	// 任务统计。
	counts, _ := s.store.CountTasksByStatus()

	// 日志（脱敏）。
	logTail := s.readLogTail(200)

	manifest := map[string]any{
		"generated_at": nowMillis(),
		"version":      version,
		"config":       configSnap,
		"task_counts":  counts,
		"log_lines":    len(logTail),
	}
	if preview {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"preview":       true,
			"manifest":      manifest,
			"log_sample":    tailN(logTail, 20),
			"redaction":     "cookie/token/password/key 等字段名强制替换为 ***",
		})
		return nil
	}

	// 打包 zip。
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, content string) {
		f, err := zw.Create(name)
		if err != nil {
			return
		}
		_, _ = f.Write([]byte(content))
	}
	add("manifest.json", toJSONPretty(manifest))
	add("raccoon.log", strings.Join(logTail, "\n"))
	if err := zw.Close(); err != nil {
		return httpx.Internal("诊断包生成失败")
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\"raccoon-diag.zip\"")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
	return nil
}

// readLogTail 读日志尾部若干行（脱敏）。
func (s *Server) readLogTail(n int) []string {
	path := filepath.Join(s.cfg.GetString("storage.data_dir"), "logs", "raccoon.log")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	out := make([]string, 0, n)
	for _, l := range tailN(lines, n) {
		out = append(out, redactLine(l))
	}
	return out
}

// redactLine 日志行脱敏（cookie/token/password/key → ***）。
func redactLine(line string) string {
	lower := strings.ToLower(line)
	for _, key := range []string{"cookie", "token", "password", "passwd", "secret", "authorization", "auth_key", "api_key"} {
		if !strings.Contains(lower, key) {
			continue
		}
		line = redactKeyValue(line, key)
	}
	return line
}

// redactKeyValue 把 "key":"value" / key=value 形式的值替换为 ***。
func redactKeyValue(line, key string) string {
	lower := strings.ToLower(line)
	for i := 0; ; {
		idx := strings.Index(lower[i:], key)
		if idx < 0 {
			break
		}
		abs := i + idx
		rest := line[abs+len(key):]
		trimmed := strings.TrimLeft(rest, " \t\"':")
		if trimmed == "" {
			break
		}
		sep := trimmed[0]
		var end int
		switch sep {
		case '"':
			end = strings.Index(trimmed[1:], "\"")
			if end < 0 {
				end = len(trimmed) - 1
			} else {
				end++
			}
		case '=':
			end = strings.IndexAny(trimmed, ", &}")
			if end < 0 {
				end = len(trimmed)
			}
		default:
			i = abs + len(key)
			continue
		}
		line = line[:abs+len(key)] + "***" + trimmed[end:]
		lower = strings.ToLower(line)
		i = abs + len(key) + 3
		if i > len(line) {
			break
		}
	}
	return line
}

// tailN 取尾部 n 项。
func tailN(items []string, n int) []string {
	if len(items) <= n {
		return items
	}
	return items[len(items)-n:]
}

// toJSONPretty 缩进 JSON。
func toJSONPretty(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}
