package server

// subscription_handler.go：订阅路由（docs/04 §2.6）。

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/huluohu/raccoon/internal/httpx"
	"github.com/huluohu/raccoon/internal/store"
	"github.com/huluohu/raccoon/internal/subscription"
)

// setupSubscriptions 注册订阅路由。
func (s *Server) setupSubscriptions() {
	s.router.GET("/api/v1/subscriptions", s.requireAuth(s.handleSubList))
	s.router.POST("/api/v1/subscriptions", s.requireAuth(s.csrfForSession(s.handleSubCreate)))
	s.router.GET("/api/v1/subscriptions/:id", s.requireAuth(s.handleSubDetail))
	s.router.PUT("/api/v1/subscriptions/:id", s.requireAuth(s.csrfForSession(s.handleSubUpdate)))
	s.router.DELETE("/api/v1/subscriptions/:id", s.requireAuth(s.csrfForSession(s.handleSubDelete)))
	s.router.POST("/api/v1/subscriptions/:id/check-now", s.requireAuth(s.csrfForSession(s.handleSubCheckNow)))
	s.router.GET("/api/v1/subscriptions/:id/items", s.requireAuth(s.handleSubItems))
}

// handleSubList 订阅列表。
func (s *Server) handleSubList(w http.ResponseWriter, r *http.Request) error {
	items, err := s.store.ListSubscriptions()
	if err != nil {
		return httpx.Internal("订阅列表查询失败")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

// handleSubDetail 订阅详情 + 统计。
func (s *Server) handleSubDetail(w http.ResponseWriter, r *http.Request) error {
	sub, err := s.store.GetSubscription(param(r, "id"))
	if err != nil {
		return subErrToHTTP(err)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"data": sub})
	return nil
}

type subReq struct {
	SourceURL    string `json:"source_url"`
	SourceType   string `json:"source_type"`
	Mode         string `json:"mode"`
	Quality      string `json:"quality"`
	SaveRule     string `json:"save_rule"`
	IntervalMin  int    `json:"interval_min"`
	Enabled      *bool  `json:"enabled"`
	RangeFrom    *int64 `json:"range_from"`
	RangeTo      *int64 `json:"range_to"`
	CredentialID string `json:"credential_id"`
	Preview      bool   `json:"preview"`
}

// handleSubCreate 创建订阅（先预览来源）。
func (s *Server) handleSubCreate(w http.ResponseWriter, r *http.Request) error {
	var req subReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	url := strings.TrimSpace(req.SourceURL)
	if url == "" {
		return httpx.Validation("source_url 不能为空")
	}
	if err := validateOutboundURL(url); err != nil {
		return err
	}
	sourceType := req.SourceType
	if sourceType == "" {
		sourceType = subscription.SourceTypeFromURL(url)
	}
	switch sourceType {
	case "favorite", "uploader", "list":
	default:
		return httpx.Validation("source_type 需为 favorite / uploader / list")
	}
	mode := req.Mode
	if mode == "" {
		mode = "new_only"
	}
	switch mode {
	case "new_only", "backfill", "range":
	default:
		return httpx.Validation("mode 需为 new_only / backfill / range")
	}
	// 预览：真实解析来源，解析失败如实报错（不静默创建空订阅）。
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	pv, err := s.subs.ParseSource(ctx, url)
	if err != nil {
		return taskErrToHTTP(err)
	}
	if req.Preview {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"data": pv, "preview": true})
		return nil
	}
	interval := req.IntervalMin
	if interval <= 0 {
		interval = 30
	}
	sub := &store.Subscription{
		Platform:     subscription.PlatformFromURL(url),
		SourceType:   sourceType,
		SourceID:     subscription.SourceIDFromURL(url),
		SourceURL:    url,
		Title:        pv.Title,
		Mode:         mode,
		RangeFrom:    req.RangeFrom,
		RangeTo:      req.RangeTo,
		Quality:      req.Quality,
		SaveRule:     req.SaveRule,
		IntervalMin:  interval,
		Enabled:      true,
		CredentialID: req.CredentialID,
	}
	if err := s.store.CreateSubscription(sub); err != nil {
		s.log.Error("订阅创建失败", map[string]any{"err": err.Error()})
		return httpx.Internal("订阅创建失败")
	}
	s.notify("sub_new", "info", "订阅已创建", sub.Title, "")
	s.hub.Broadcast("notif.new", map[string]any{"kind": "sub_new", "title": "订阅已创建", "body": sub.Title})
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"data": sub})
	return nil
}

// handleSubUpdate 修改订阅规则。
func (s *Server) handleSubUpdate(w http.ResponseWriter, r *http.Request) error {
	sub, err := s.store.GetSubscription(param(r, "id"))
	if err != nil {
		return subErrToHTTP(err)
	}
	var req subReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.Mode != "" {
		sub.Mode = req.Mode
	}
	if req.Quality != "" {
		sub.Quality = req.Quality
	}
	if req.SaveRule != "" {
		sub.SaveRule = req.SaveRule
	}
	if req.IntervalMin > 0 {
		sub.IntervalMin = req.IntervalMin
	}
	if req.Enabled != nil {
		sub.Enabled = *req.Enabled
	}
	if req.RangeFrom != nil {
		sub.RangeFrom = req.RangeFrom
	}
	if req.RangeTo != nil {
		sub.RangeTo = req.RangeTo
	}
	if err := s.store.UpdateSubscription(sub); err != nil {
		// 带上底层错误：这句 500 曾经掩盖了外键约束失败，排查全靠猜。
		s.log.Error("订阅更新失败", map[string]any{"id": sub.ID, "err": err.Error()})
		return subErrToHTTP(err)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"data": sub})
	return nil
}

// handleSubDelete 删除订阅（不删已下载文件）。
func (s *Server) handleSubDelete(w http.ResponseWriter, r *http.Request) error {
	if err := s.store.DeleteSubscription(param(r, "id")); err != nil {
		s.log.Error("订阅删除失败", map[string]any{"id": param(r, "id"), "err": err.Error()})
		return subErrToHTTP(err)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

// handleSubCheckNow 立即检查一次（同步返回本次发现数）。
func (s *Server) handleSubCheckNow(w http.ResponseWriter, r *http.Request) error {
	sub, err := s.store.GetSubscription(param(r, "id"))
	if err != nil {
		return subErrToHTTP(err)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	n, err := s.subs.CheckOne(ctx, sub)
	if err != nil {
		if errors.Is(err, subscription.ErrChecking) {
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "checking": true, "message": "检查进行中"})
			return nil
		}
		return httpx.Validation("订阅检查失败: " + err.Error())
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "queued": n})
	return nil
}

// handleSubItems 已发现内容列表。
func (s *Server) handleSubItems(w http.ResponseWriter, r *http.Request) error {
	id := param(r, "id")
	if _, err := s.store.GetSubscription(id); err != nil {
		return subErrToHTTP(err)
	}
	items, err := s.store.ListSubscriptionItems(id, atoiDefault(r.URL.Query().Get("limit"), 200))
	if err != nil {
		return httpx.Internal("订阅内容查询失败")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

// subErrToHTTP store 错误 → HTTP 信封。
func subErrToHTTP(err error) error {
	if errors.Is(err, store.ErrNoSuchSubscription) {
		return httpx.NotFound("订阅不存在")
	}
	return httpx.Internal("订阅操作失败")
}
