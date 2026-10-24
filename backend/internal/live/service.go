package live

// service.go：直播源守护（补齐「手动之外的计划录制」）。
//
// 之前只有手动录制链路：live_sources 表里的 record_plan（always/scheduled）、
// check_interval_sec、schedule_json 建好了却没有任何调度器读它——「开播即录」
// 形同虚设。本服务对齐订阅服务的模式：
//
//	每 30s 扫描到期源（last_checked_at 早于 check_interval_sec）
//	  → 探测开播状态（bilibili 房间状态 API，走 net.proxy）
//	  → 回写源状态（live / offline / error，如实三态）
//	  → 开播且计划允许（always 或 scheduled 命中时段）→ 提交录制任务
//
// 「下播即成品」的收尾在 task.Worker：live 来源任务终态时回写录制记录与源状态。
// 单飞防并发（同源探测不叠加）；录制中的源跳过探测（收尾交给任务终态）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/huluohu/raccoon/internal/config"
	"github.com/huluohu/raccoon/internal/logx"
	"github.com/huluohu/raccoon/internal/store"
	"github.com/huluohu/raccoon/internal/task"
)

// ErrAlreadyRecording 该源已有进行中的录制（不视为错误，调用方静默跳过）。
var ErrAlreadyRecording = errors.New("live source already recording")

// Service 直播源守护。
type Service struct {
	st     *store.Store
	cfg    *config.Config
	mgr    *task.Manager
	log    *logx.Logger
	hc     *http.Client
	probe  func(ctx context.Context, platform, roomID string) (bool, error) // 可注入（测试）
	mu     sync.Mutex
	probes map[string]bool // 源 ID → 探测中（单飞）
}

// New 构造守护服务。
func New(st *store.Store, cfg *config.Config, mgr *task.Manager, logger *logx.Logger) *Service {
	s := &Service{
		st:     st,
		cfg:    cfg,
		mgr:    mgr,
		log:    logger,
		probes: map[string]bool{},
	}
	s.probe = s.probeLiveStatus
	return s
}

// setProbe 注入探测函数（仅测试用）。
func (s *Service) setProbe(p func(ctx context.Context, platform, roomID string) (bool, error)) {
	s.probe = p
}

// Start 启动扫描循环（30s 一轮；源自身的 check_interval_sec 控制实际探测频率）。
func (s *Service) Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.scanDue(ctx)
			}
		}
	}()
}

// scanDue 扫描到期的非手动源并逐个探测（单源单 goroutine，互不阻塞）。
func (s *Service) scanDue(ctx context.Context) {
	now := time.Now().UnixMilli()
	sources, err := s.st.ListLiveSources()
	if err != nil {
		s.log.Error("直播源扫描失败", map[string]any{"err": err.Error()})
		return
	}
	for _, ls := range sources {
		if !ls.Enabled || ls.RecordPlan == "manual" || ls.Status == "recording" {
			continue
		}
		interval := ls.CheckIntervalSec
		if interval <= 0 {
			interval = 60
		}
		if ls.LastCheckedAt != nil && now-*ls.LastCheckedAt < int64(interval)*1000 {
			continue
		}
		go s.ProbeOne(ctx, ls)
	}
}

// ProbeOne 探测一个源：回写三态（live/offline/error），开播且计划允许就自动录制。
func (s *Service) ProbeOne(ctx context.Context, ls *store.LiveSource) error {
	s.mu.Lock()
	if s.probes[ls.ID] {
		s.mu.Unlock()
		return ErrAlreadyRecording
	}
	s.probes[ls.ID] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.probes, ls.ID)
		s.mu.Unlock()
	}()

	liveOn, err := s.probe(ctx, ls.Platform, ls.RoomID)
	if err != nil {
		_ = s.st.MarkLiveSourceStatus(ls.ID, "error", err.Error(), false)
		return err
	}
	if !liveOn {
		_ = s.st.MarkLiveSourceStatus(ls.ID, "offline", "", false)
		return nil
	}
	_ = s.st.MarkLiveSourceStatus(ls.ID, "live", "", true)

	// 定时计划：只在配置的时段内自动开录；时段缺失/不可解析时不开录（不猜）。
	if ls.RecordPlan == "scheduled" && !inWindow(ls.ScheduleJSON, time.Now()) {
		return nil
	}
	if ls.RecordPlan != "always" && ls.RecordPlan != "scheduled" {
		return nil
	}
	return s.startRecording(ctx, ls)
}

// startRecording 提交录制任务 + 建录制记录 + 置 recording 态。
// 去重冲突（该房间已有同任务）静默跳过——上一轮可能刚提交过。
func (s *Service) startRecording(ctx context.Context, ls *store.LiveSource) error {
	quality := ls.Quality
	if quality == "" {
		quality = s.cfg.GetString("download.default_quality")
	}
	results, err := s.mgr.Submit([]task.SubmitItem{{URL: ls.RoomURL, Quality: quality}}, task.SourceLive, ls.ID)
	if err != nil {
		s.log.Error("自动录制任务创建失败", map[string]any{"source_id": ls.ID, "err": err.Error()})
		return err
	}
	if len(results) == 0 || results[0].Task == nil || results[0].ExistingID != "" {
		return ErrAlreadyRecording
	}
	if _, err := s.st.CreateLiveRecording(ls.ID, results[0].Task.ID); err != nil {
		s.log.Error("录制记录创建失败", map[string]any{"source_id": ls.ID, "err": err.Error()})
		return err
	}
	_ = s.st.MarkLiveSourceStatus(ls.ID, "recording", "", true)
	s.mgr.Notify("live_start", "info", "开播自动录制："+firstNonEmpty(ls.Title, ls.RoomID),
		"检测到开播（计划："+ls.RecordPlan+"），已加入录制队列", "")
	s.log.Info("开播自动录制", map[string]any{"source_id": ls.ID, "task_id": results[0].Task.ID})
	return nil
}

// probeLiveStatus 开播探测（bilibili：房间状态 API，live_status==1 即开播）。
// 走 net.proxy 配置的代理（探测与录制必须同出口，否则「探测得到、录制连不上」）。
func (s *Service) probeLiveStatus(ctx context.Context, platform, roomID string) (bool, error) {
	if platform != "bilibili" {
		return false, fmt.Errorf("平台 %s 不支持自动探测（实测清单仅 bilibili）", platform)
	}
	room := strings.TrimSpace(roomID)
	if room == "" {
		return false, errors.New("房间号缺失，无法探测开播状态")
	}
	if _, err := strconv.ParseInt(room, 10, 64); err != nil {
		return false, fmt.Errorf("房间号 %q 不是数字，无法探测", room)
	}
	target := "https://api.live.bilibili.com/room/v1/Room/get_info?room_id=" + url.QueryEscape(room)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Raccoon/1.0")
	transport := http.DefaultTransport
	if proxy := strings.TrimSpace(s.cfg.GetString("net.proxy")); proxy != "" {
		if pu, perr := url.Parse(proxy); perr == nil {
			transport = &http.Transport{Proxy: http.ProxyURL(pu)}
		}
	}
	client := &http.Client{Timeout: 8 * time.Second, Transport: transport}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("开播探测请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("开播探测返回 %d", resp.StatusCode)
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			LiveStatus int `json:"live_status"` // 0 未开播 1 开播 2 轮播
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false, fmt.Errorf("开播探测响应解析失败: %w", err)
	}
	if body.Code != 0 {
		return false, fmt.Errorf("开播探测接口返回 code=%d", body.Code)
	}
	return body.Data.LiveStatus == 1, nil
}

// scheduleWindow 时段计划（schedule_json）：{"days":[1,2],"start":"20:00","end":"23:30"}。
// days 为星期（0=周日…6=周六），缺省 = 每天；end <= start 视为跨午夜。
type scheduleWindow struct {
	Days  []int  `json:"days"`
	Start string `json:"start"`
	End   string `json:"end"`
}

// inWindow 判断时刻是否落在计划的时段内。
// schedule_json 缺失或不可解析 → false（定时计划没配时段就不自动录，不猜）。
func inWindow(scheduleJSON string, now time.Time) bool {
	if strings.TrimSpace(scheduleJSON) == "" {
		return false
	}
	var w scheduleWindow
	if err := json.Unmarshal([]byte(scheduleJSON), &w); err != nil {
		return false
	}
	startMin, ok1 := parseHHMM(w.Start)
	endMin, ok2 := parseHHMM(w.End)
	if !ok1 || !ok2 {
		return false
	}
	if len(w.Days) > 0 {
		today := int(now.Weekday())
		allowed := false
		for _, d := range w.Days {
			if d == today {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}
	cur := now.Hour()*60 + now.Minute()
	if startMin <= endMin {
		return cur >= startMin && cur < endMin
	}
	// 跨午夜：22:00–02:00
	return cur >= startMin || cur < endMin
}

// parseHHMM 解析 "HH:MM" → 当日分钟数。
func parseHHMM(s string) (int, bool) {
	parts := strings.SplitN(strings.TrimSpace(s), ":", 2)
	if len(parts) != 2 {
		return 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
