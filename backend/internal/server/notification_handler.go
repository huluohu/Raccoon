package server

// notification_handler.go：通知中心路由（docs/04 §2.9）+ 通知写入助手。

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/huluohu/raccoon/internal/httpx"
	"github.com/huluohu/raccoon/internal/store"
	"github.com/huluohu/raccoon/internal/tgbot"
)

// setupNotifications 注册通知路由。
func (s *Server) setupNotifications() {
	s.router.GET("/api/v1/notifications", s.requireAuth(s.handleNotifList))
	s.router.POST("/api/v1/notifications/read", s.requireAuth(s.csrfForSession(s.handleNotifRead)))
}

// handleNotifList 通知列表（未读徽章计数）。
func (s *Server) handleNotifList(w http.ResponseWriter, r *http.Request) error {
	items, err := s.store.ListNotifications(atoiDefault(r.URL.Query().Get("limit"), 50), r.URL.Query().Get("unread") == "1")
	if err != nil {
		return httpx.Internal("通知查询失败")
	}
	unread, _ := s.store.CountUnreadNotifications()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "unread": unread})
	return nil
}

// handleNotifRead 标记已读。
func (s *Server) handleNotifRead(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		IDs []string `json:"ids"`
		All bool     `json:"all"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	n, err := s.store.MarkNotificationsRead(req.IDs, req.All)
	if err != nil {
		return httpx.Internal("通知更新失败")
	}
	unread, _ := s.store.CountUnreadNotifications()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"updated": n, "unread": unread})
	return nil
}

// notifierFunc 适配 task.Notifier 接口（task 包不依赖 server）。
type notifierFunc func(kind, severity, title, body, taskID string)

// Notify 实现 task.Notifier。
func (f notifierFunc) Notify(kind, severity, title, body, taskID string) {
	f(kind, severity, title, body, taskID)
}

// notify 写一条通知并广播（notify.task_done / task_failed 开关生效）。
func (s *Server) notify(kind, severity, title, body, taskID string) {
	// 开关：任务完成/失败通知可关（配置项 immediate 生效）。
	switch kind {
	case "task_done":
		if !s.cfg.GetBool("notify.task_done") {
			return
		}
	case "task_failed":
		if !s.cfg.GetBool("notify.task_failed") {
			return
		}
	case "disk_warn":
		if !s.cfg.GetBool("notify.disk_warn") {
			return
		}
	}
	n := &store.Notification{
		Kind:     kind,
		Severity: severity,
		Title:    title,
		Body:     body,
		TaskID:   taskID,
	}
	if err := s.store.CreateNotification(n); err != nil {
		s.log.Error("通知写入失败", map[string]any{"err": err.Error()})
		return
	}
	s.hub.Broadcast("notif.new", n)
	// Telegram 定向通知：只有从 TG 提交的任务才推回原会话（web 提交的不打扰）。
	s.notifyTelegram(taskID, title, body, severity)
	// Webhook 出口：自建聚合的通用管道，与 TG/通知中心并行。
	s.notifyWebhook(kind, severity, title, body, taskID)
	// Server酱出口：个人微信推送（任务完成/失败/磁盘预警）。
	s.notifyServerChan(kind, severity, title, body, taskID)
}

// notifyWebhook 任务事件 POST 到 notify.webhook_url（JSON）。
// 异步执行 + 5s 超时 + 失败仅记日志：通知出口绝不阻塞任务流，也不重试风暴。
func (s *Server) notifyWebhook(kind, severity, title, body, taskID string) {
	hook := strings.TrimSpace(s.cfg.GetString("notify.webhook_url"))
	if hook == "" {
		return
	}
	go func() {
		payload, err := json.Marshal(map[string]any{
			"kind": kind, "severity": severity, "title": title,
			"body": body, "task_id": taskID, "ts": time.Now().Unix(),
		})
		if err != nil {
			return
		}
		req, err := http.NewRequest(http.MethodPost, hook, bytes.NewReader(payload))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req = req.WithContext(ctx)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			s.log.Warn("Webhook 通知发送失败", map[string]any{"err": err.Error(), "url": hook})
			return
		}
		resp.Body.Close()
	}()
}

// serverchanBase Server酱 API 地址（测试注入假服务）。
var serverchanBase = "https://sctapi.ftqq.com"

// notifyServerChan 任务完成/失败/磁盘预警推送到微信（Server酱通道）。
//
// 与 Webhook 同款纪律：异步 + 5s 超时 + 失败只记日志——通知出口绝不阻塞
// 任务流。免费额度 5 条/天由 Server酱侧计数，超限它回错误码，我们记日志即止。
func (s *Server) notifyServerChan(kind, severity, title, body, taskID string) {
	switch kind {
	case "task_done", "task_failed", "disk_warn":
	default:
		return // 微信是强打扰通道：只推用户点名关心的三类事件，不推全量通知流
	}
	key := strings.TrimSpace(s.cfg.GetString("notify.serverchan_sendkey"))
	if key == "" {
		return
	}
	go func() {
		form := url.Values{}
		form.Set("title", title)
		desp := body
		if taskID != "" {
			desp += "\n\n任务 ID: " + taskID
		}
		form.Set("desp", desp)
		req, err := http.NewRequest(http.MethodPost, serverchanBase+"/"+key+".send", strings.NewReader(form.Encode()))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req = req.WithContext(ctx)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			s.log.Warn("Server酱推送失败", map[string]any{"err": err.Error()})
			return
		}
		defer resp.Body.Close()
		// Server酱错误体：{"code":<非0>,"message":...}；HTTP 200 不代表送达。
		var out struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		if json.Unmarshal(raw, &out) == nil && out.Code != 0 {
			s.log.Warn("Server酱拒绝推送（额度用尽或 SendKey 失效）", map[string]any{"code": out.Code, "message": out.Message})
		}
	}()
}

// notifyTelegram 向任务来源的 Telegram 会话推送终态通知。
func (s *Server) notifyTelegram(taskID, title, body, severity string) {
	if taskID == "" || s.tgClient == nil {
		return
	}
	s.tgMu.Lock()
	chatID, ok := s.tgChats[taskID]
	if ok {
		delete(s.tgChats, taskID) // 一次性通知，映射用完即清
	}
	s.tgMu.Unlock()
	if !ok {
		return
	}
	text := title
	if body != "" {
		text += "\n" + body
	}
	text += "\n任务 ID: " + taskID
	if _, err := s.tgClient.SendMessage(context.Background(), chatID, text, tgbot.ParseModeNone); err != nil {
		s.log.Warn("Telegram 通知发送失败", map[string]any{"task_id": taskID, "err": err.Error()})
	}
	// 文件回传（telegram.send_files，默认关）：完成类通知才回传；
	// 只发第一个 ≤50MB 的产物（TG bot 文档上限），大文件/多文件场景回传
	// 既慢又可能被限流——路径已在文本里，需要时用 /media 检索。
	if severity != "success" || !s.cfg.GetBool("telegram.send_files") {
		return
	}
	files, err := s.store.TaskFiles(taskID)
	if err != nil || len(files) == 0 {
		return
	}
	for _, f := range files {
		if f.Size > 50*1024*1024 {
			continue
		}
		name := filepath.Base(f.Path)
		if _, err := s.tgClient.SendDocument(context.Background(), chatID, f.Path, name); err != nil {
			// 回传失败不影响通知本身：记日志即可（文件还在服务器上）。
			s.log.Warn("Telegram 文件回传失败", map[string]any{"task_id": taskID, "file": f.Path, "err": err.Error()})
		}
		return // 只发第一个符合尺寸的产物
	}
}
