package server

// live_handler.go：直播源路由（docs/04 §2.7）。
//
// 支持平台清单以实测为准（docs/01 §3.3：平台不在实测清单 → 不允许添加并说明）。
// 当前仅 B 站（经 yt-dlp 探测开播状态）进入实测清单。

import (
	"errors"
	"net/http"
	"strings"

	"github.com/huluohu/raccoon/internal/httpx"
	"github.com/huluohu/raccoon/internal/store"
	"github.com/huluohu/raccoon/internal/task"
)

// supportedLivePlatforms 实测通过的直播平台清单（新增平台须先实测再加入）。
var supportedLivePlatforms = []string{"bilibili"}

// setupLive 注册直播路由。
func (s *Server) setupLive() {
	s.router.GET("/api/v1/live-sources", s.requireAuth(s.handleLiveList))
	s.router.POST("/api/v1/live-sources", s.requireAuth(s.csrfForSession(s.handleLiveCreate)))
	s.router.PUT("/api/v1/live-sources/:id", s.requireAuth(s.csrfForSession(s.handleLiveUpdate)))
	s.router.DELETE("/api/v1/live-sources/:id", s.requireAuth(s.csrfForSession(s.handleLiveDelete)))
	s.router.GET("/api/v1/live-sources/:id/recordings", s.requireAuth(s.handleLiveRecordings))
	s.router.POST("/api/v1/live-sources/:id/record", s.requireAuth(s.csrfForSession(s.handleLiveRecord)))
}

// handleLiveList 直播源列表。
func (s *Server) handleLiveList(w http.ResponseWriter, r *http.Request) error {
	items, err := s.store.ListLiveSources()
	if err != nil {
		return httpx.Internal("直播源查询失败")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"items":          items,
		"supported":      supportedLivePlatforms,
		"supported_note": "支持清单以实测为准；新增平台需先验证开播检测可用",
	})
	return nil
}

type liveReq struct {
	Platform         string `json:"platform"`
	RoomURL          string `json:"room_url"`
	Title            string `json:"title"`
	Quality          string `json:"quality"`
	RecordPlan       string `json:"record_plan"`
	SegmentMin       int    `json:"segment_min"`
	CheckIntervalSec int    `json:"check_interval_sec"`
	Enabled          *bool  `json:"enabled"`
	CredentialID     string `json:"credential_id"`
}

// handleLiveCreate 新增直播源。
func (s *Server) handleLiveCreate(w http.ResponseWriter, r *http.Request) error {
	var req liveReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	platform := strings.ToLower(strings.TrimSpace(req.Platform))
	if platform == "" {
		platform = "bilibili"
	}
	if !containsStr(supportedLivePlatforms, platform) {
		return &httpx.Error{HTTP: http.StatusUnprocessableEntity, Code: "UNSUPPORTED_PLATFORM",
			Message: "平台 " + platform + " 不在实测支持清单内（当前支持：" + strings.Join(supportedLivePlatforms, "、") + "）"}
	}
	url := strings.TrimSpace(req.RoomURL)
	if url == "" {
		return httpx.Validation("room_url 不能为空")
	}
	if err := validateOutboundURL(url); err != nil {
		return err
	}
	ls := &store.LiveSource{
		Platform:         platform,
		RoomID:           roomIDFromURL(url),
		RoomURL:          url,
		Title:            req.Title,
		Quality:          req.Quality,
		RecordPlan:       req.RecordPlan,
		SegmentMin:       req.SegmentMin,
		CheckIntervalSec: req.CheckIntervalSec,
		CredentialID:     req.CredentialID,
		Enabled:          true,
	}
	if req.Enabled != nil {
		ls.Enabled = *req.Enabled
	}
	if err := s.store.CreateLiveSource(ls); err != nil {
		s.log.Error("直播源创建失败", map[string]any{"err": err.Error()})
		return httpx.Internal("直播源创建失败")
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"data": ls})
	return nil
}

// handleLiveUpdate 修改直播源。
func (s *Server) handleLiveUpdate(w http.ResponseWriter, r *http.Request) error {
	ls, err := s.store.GetLiveSource(param(r, "id"))
	if err != nil {
		return liveErrToHTTP(err)
	}
	var req liveReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.Title != "" {
		ls.Title = req.Title
	}
	if req.Quality != "" {
		ls.Quality = req.Quality
	}
	if req.RecordPlan != "" {
		switch req.RecordPlan {
		case "manual", "always", "scheduled":
			ls.RecordPlan = req.RecordPlan
		default:
			return httpx.Validation("record_plan 需为 manual / always / scheduled")
		}
	}
	if req.SegmentMin > 0 {
		ls.SegmentMin = req.SegmentMin
	}
	if req.CheckIntervalSec > 0 {
		ls.CheckIntervalSec = req.CheckIntervalSec
	}
	if req.Enabled != nil {
		ls.Enabled = *req.Enabled
	}
	if req.CredentialID != "" {
		ls.CredentialID = req.CredentialID
	}
	if err := s.store.UpdateLiveSource(ls); err != nil {
		return liveErrToHTTP(err)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"data": ls})
	return nil
}

// handleLiveDelete 删除直播源。
func (s *Server) handleLiveDelete(w http.ResponseWriter, r *http.Request) error {
	if err := s.store.DeleteLiveSource(param(r, "id")); err != nil {
		return liveErrToHTTP(err)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

// handleLiveRecordings 录制历史。
func (s *Server) handleLiveRecordings(w http.ResponseWriter, r *http.Request) error {
	id := param(r, "id")
	if _, err := s.store.GetLiveSource(id); err != nil {
		return liveErrToHTTP(err)
	}
	items, err := s.store.ListLiveRecordings(id, atoiDefault(r.URL.Query().Get("limit"), 100))
	if err != nil {
		return httpx.Internal("录制历史查询失败")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

// handleLiveRecord 手动开始录制（创建 record 任务，走统一队列）。
func (s *Server) handleLiveRecord(w http.ResponseWriter, r *http.Request) error {
	ls, err := s.store.GetLiveSource(param(r, "id"))
	if err != nil {
		return liveErrToHTTP(err)
	}
	if err := s.tasks.Engines().YTDLP.Available(); err != nil {
		return &httpx.Error{HTTP: http.StatusServiceUnavailable, Code: httpx.CodeNoEngine, Message: err.Error()}
	}
	results, err := s.tasks.Submit([]task.SubmitItem{{URL: ls.RoomURL, Quality: ls.Quality}}, task.SourceLive, ls.ID)
	if err != nil {
		return httpx.Internal("录制任务创建失败")
	}
	if len(results) == 0 || results[0].Task == nil {
		return httpx.Conflict("该直播源已有进行中的任务")
	}
	rec, err := s.store.CreateLiveRecording(ls.ID, results[0].Task.ID)
	if err != nil {
		return httpx.Internal("录制记录创建失败")
	}
	_ = s.store.MarkLiveSourceStatus(ls.ID, "recording", "", true)
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"data": rec, "task": results[0].Task})
	return nil
}

// roomIDFromURL 从房间 URL 提取房间号。
func roomIDFromURL(url string) string {
	u := strings.TrimRight(url, "/")
	if i := strings.LastIndex(u, "/"); i >= 0 && i < len(u)-1 {
		return u[i+1:]
	}
	return u
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// liveErrToHTTP store 错误 → HTTP 信封。
func liveErrToHTTP(err error) error {
	if errors.Is(err, store.ErrNoSuchLiveSource) {
		return httpx.NotFound("直播源不存在")
	}
	return httpx.Internal("直播源操作失败")
}
