// Package subscription 实现订阅系统（docs/01 §3.2 / docs/05 §5）。
//
// 职责：周期检查来源 → 发现新内容 → 防重复 → 走统一任务系统入队。
// 关键纪律：订阅产生的任务与手动任务同一队列同一状态机（docs/01 §5.1）。
package subscription

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/huluohu/raccoon/internal/config"
	"github.com/huluohu/raccoon/internal/engine"
	"github.com/huluohu/raccoon/internal/logx"
	"github.com/huluohu/raccoon/internal/store"
	"github.com/huluohu/raccoon/internal/task"
)

// Service 订阅服务。
type Service struct {
	st  *store.Store
	cfg *config.Config
	eng *engine.Set
	mgr *task.Manager
	log *logx.Logger

	mu       sync.Mutex
	checking map[string]bool // 订阅 ID → 检查中（单飞，docs/04 §4）
}

// New 构造订阅服务。
func New(st *store.Store, cfg *config.Config, eng *engine.Set, mgr *task.Manager, logger *logx.Logger) *Service {
	return &Service{st: st, cfg: cfg, eng: eng, mgr: mgr, log: logger, checking: map[string]bool{}}
}

// Start 启动周期检查循环（每分钟扫一次到期的订阅）。
func (s *Service) Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.checkDue(ctx)
			}
		}
	}()
}

// checkDue 检查所有到期订阅。
func (s *Service) checkDue(ctx context.Context) {
	now := time.Now().UnixMilli()
	subs, err := s.st.DueSubscriptions(now, 20)
	if err != nil {
		s.log.Error("到期订阅查询失败", map[string]any{"err": err.Error()})
		return
	}
	for _, sub := range subs {
		go s.CheckOne(ctx, sub)
	}
}

// CheckOne 执行一次订阅检查（单飞；已检查中直接返回 ErrChecking）。
func (s *Service) CheckOne(ctx context.Context, sub *store.Subscription) (int, error) {
	s.mu.Lock()
	if s.checking[sub.ID] {
		s.mu.Unlock()
		return 0, ErrChecking
	}
	s.checking[sub.ID] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.checking, sub.ID)
		s.mu.Unlock()
	}()

	items, err := s.discover(ctx, sub)
	if err != nil {
		next := time.Now().Add(time.Duration(s.intervalOf(sub)) * time.Minute).UnixMilli()
		_ = s.st.MarkSubscriptionChecked(sub.ID, sub.LastItemID, err.Error(), next)
		return 0, err
	}

	queued := 0
	var lastItemID string
	for _, it := range items {
		lastItemID = it.ItemID
		// 防重复：已记录过就跳过（跨重启有效，docs/01 §3.2）。
		has, _ := s.st.HasSubscriptionItem(sub.ID, it.ItemID)
		if has {
			continue
		}
		if sub.Mode == "new_only" && sub.LastItemID != "" && it.ItemID == sub.LastItemID {
			continue
		}
		// 入队（走统一任务系统）。
		quality := sub.Quality
		if quality == "" {
			quality = s.cfg.GetString("download.default_quality")
		}
		results, err := s.mgr.Submit([]task.SubmitItem{{URL: it.URL, Quality: quality}}, task.SourceSubscription, sub.ID)
		if err != nil {
			_, _ = s.st.UpsertSubscriptionItem(sub.ID, it.ItemID, it.Title, "", "failed")
			continue
		}
		taskID := ""
		if len(results) > 0 && results[0].Task != nil {
			taskID = results[0].Task.ID
			queued++
		} else if len(results) > 0 && results[0].ExistingID != "" {
			taskID = results[0].ExistingID
		}
		status := "queued"
		if taskID == "" {
			status = "skipped"
		}
		_, _ = s.st.UpsertSubscriptionItem(sub.ID, it.ItemID, it.Title, taskID, status)
	}
	next := time.Now().Add(time.Duration(s.intervalOf(sub)) * time.Minute).UnixMilli()
	_ = s.st.MarkSubscriptionChecked(sub.ID, lastItemID, "", next)
	s.log.Info("订阅检查完成", map[string]any{"sub_id": sub.ID, "discovered": len(items), "queued": queued})
	return queued, nil
}

// ErrChecking 检查进行中（单飞）。
var ErrChecking = fmt.Errorf("检查进行中")

// DiscoveredItem 发现的一条内容。
type DiscoveredItem struct {
	ItemID string
	Title  string
	URL    string
}

// discover 从来源发现内容（真实调用引擎解析播放列表/收藏夹/UP 主投稿）。
func (s *Service) discover(ctx context.Context, sub *store.Subscription) ([]DiscoveredItem, error) {
	switch sub.SourceType {
	case "favorite", "uploader", "list":
	default:
		return nil, fmt.Errorf("未知的订阅来源类型: %s", sub.SourceType)
	}
	// 收藏夹/UP 主需要平台 API 凭据；这里用引擎的 playlist 解析能力。
	// 未配置凭据时如实返回错误，不静默跳过。
	//
	// 出口与其它链路一致：走全局 net.proxy（订阅自己没有独立的代理配置项）。
	// 之前这一步恒直连 —— 只靠代理可达的站点，订阅永远发现不到内容，
	// 也就永远不会建任务（2026-10-28 定位）。
	res, err := s.eng.YTDLP.ResolvePlaylist(ctx, sub.SourceURL, s.cfg.GetString("net.proxy"))
	if err != nil {
		return nil, err
	}
	out := make([]DiscoveredItem, 0, len(res.Entries))
	for _, e := range res.Entries {
		if e.URL == "" {
			continue
		}
		out = append(out, DiscoveredItem{ItemID: e.ID, Title: e.Title, URL: e.URL})
	}
	if sub.Mode == "range" && sub.RangeFrom != nil && sub.RangeTo != nil {
		from, to := int(*sub.RangeFrom), int(*sub.RangeTo)
		if from < 0 {
			from = 0
		}
		if to > len(out) || to <= 0 {
			to = len(out)
		}
		if from < len(out) {
			out = out[from:to]
		} else {
			out = nil
		}
	}
	return out, nil
}

func (s *Service) intervalOf(sub *store.Subscription) int {
	if sub.IntervalMin > 0 {
		return sub.IntervalMin
	}
	if v := s.cfg.GetInt("subscription.interval_min"); v > 0 {
		return v
	}
	return 30
}

// ParseSource 解析订阅来源（创建前预览，docs/04 §2.6）。
func (s *Service) ParseSource(ctx context.Context, url string) (*store.ParsePreview, error) {
	return s.mgr.Preview(ctx, url)
}

// SourceIDFromURL 从来源 URL 提取稳定标识（收藏夹 ID / UP 主 mid / 播放列表 ID）。
func SourceIDFromURL(url string) string {
	u := strings.TrimSpace(url)
	// B 站收藏夹：https://space.bilibili.com/{mid}/favlist?fid=xxx
	if i := strings.Index(u, "favlist?fid="); i >= 0 {
		rest := u[i+len("favlist?fid="):]
		end := strings.IndexAny(rest, "&/")
		if end > 0 {
			rest = rest[:end]
		}
		return "fav:" + rest
	}
	// B 站空间：https://space.bilibili.com/{mid}
	if strings.Contains(u, "space.bilibili.com/") {
		rest := u[strings.Index(u, "space.bilibili.com/")+len("space.bilibili.com/"):]
		end := strings.IndexAny(rest, "/?#")
		if end > 0 {
			rest = rest[:end]
		}
		if rest != "" {
			return "space:" + rest
		}
	}
	// 通用：取 URL 末段。
	parts := strings.Split(strings.TrimRight(u, "/"), "/")
	last := parts[len(parts)-1]
	if last == "" {
		return u
	}
	return last
}

// SourceTypeFromURL 猜来源类型。
func SourceTypeFromURL(url string) string {
	u := strings.ToLower(url)
	switch {
	case strings.Contains(u, "favlist"):
		return "favorite"
	case strings.Contains(u, "space.bilibili.com/"), strings.Contains(u, "/user/"):
		return "uploader"
	default:
		return "list"
	}
}

// PlatformFromURL 猜平台。
func PlatformFromURL(url string) string {
	return engine.PlatformGuess(url)
}

// FormatCount 供界面展示的条目数格式化。
func FormatCount(n int) string { return strconv.Itoa(n) }
