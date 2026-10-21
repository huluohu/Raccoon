package server

// notification_handler.go：通知中心路由（docs/04 §2.9）+ 通知写入助手。

import (
	"net/http"

	"github.com/huluohu/raccoon/internal/httpx"
	"github.com/huluohu/raccoon/internal/store"
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
}
