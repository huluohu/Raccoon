package task

// manager.go：任务系统总控（docs/05 §1 组件装配 + §3 调度循环 + §6 恢复）。
// 数据流：HTTP → Manager.Submit（落库 waiting）→ tick 领取（闸门）→ Worker 执行 → SSE 广播。

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/huluohu/raccoon/internal/config"
	"github.com/huluohu/raccoon/internal/engine"
	"github.com/huluohu/raccoon/internal/logx"
	"github.com/huluohu/raccoon/internal/sse"
	"github.com/huluohu/raccoon/internal/store"
)

// Manager 任务系统总控。
type Manager struct {
	st      *store.Store
	cfg     *config.Config
	engines *engine.Set
	hub     *sse.Hub
	log     *logx.Logger

	mu      sync.Mutex
	cancels map[string]context.CancelFunc // taskID → 运行中 worker 的 cancel
	running map[string]bool

	done      chan struct{}
	stopOnce  sync.Once
	startedAt time.Time
}

// NewManager 构造（不启动循环；Start 由 main 调用）。
func NewManager(st *store.Store, cfg *config.Config, eng *engine.Set, hub *sse.Hub, logger *logx.Logger) *Manager {
	return &Manager{
		st: st, cfg: cfg, engines: eng, hub: hub, log: logger,
		cancels: map[string]context.CancelFunc{}, running: map[string]bool{},
		done: make(chan struct{}), startedAt: time.Now(),
	}
}

// Start 启动调度循环（tick 500ms）+ 启动恢复扫描（docs/05 §3.1 仅首轮）。
func (m *Manager) Start(ctx context.Context) {
	go m.loop(ctx)
}

// Stop 优雅停机（docs/05 §6「意图重启」）：停领新任务 → 等待运行中 worker 让出 → 返回。
func (m *Manager) Stop(ctx context.Context) {
	m.stopOnce.Do(func() { close(m.done) })
	// 等待运行中 worker 退出（每 200ms 查一次，上限由 ctx 控制）。
	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()
	for {
		m.mu.Lock()
		n := len(m.running)
		m.mu.Unlock()
		if n == 0 {
			return
		}
		select {
		case <-ctx.Done():
			m.log.Warn("停机超时，仍有任务在运行", map[string]any{"running": n})
			return
		case <-t.C:
		}
	}
}

// loop 调度主循环。
func (m *Manager) loop(ctx context.Context) {
	m.recoverInterrupted()
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.done:
			return
		case <-t.C:
			m.tick(ctx)
		}
	}
}

// recoverInterrupted 启动恢复扫描（docs/05 §3.1）：parsing/downloading/post → 回 waiting
// + wait_reason='interrupted'——如实说明「无法保证从原字节位置续传」。
func (m *Manager) recoverInterrupted() {
	for _, from := range []string{"parsing", "downloading", "post"} {
		var ids []string
		rows, err := m.st.RO.Query(`SELECT id FROM tasks WHERE status=? AND deleted_at IS NULL`, from)
		if err != nil {
			m.log.Error("恢复扫描查询失败", map[string]any{"err": err.Error(), "status": from})
			continue
		}
		for rows.Next() {
			var id string
			_ = rows.Scan(&id)
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			// 不经 CAS 也可：进程刚启动无并发。
			_ = m.st.UpdateTaskFields(id, map[string]any{
				"status":      "waiting",
				"wait_reason": "interrupted",
				"speed_bps":   0,
				"progress":    0,
			}, from)
			_ = m.st.AppendEvent(id, "warn", "interrupted", "进程重启，任务被中断；无法保证从原字节位置续传，将重新排队", "")
			m.hub.Broadcast("task.status", map[string]any{"task_id": id, "status": "waiting", "wait_reason": "interrupted"})
		}
		if len(ids) > 0 {
			m.log.Info("恢复扫描完成", map[string]any{"status": from, "count": len(ids)})
		}
	}
}

// tick 一次调度：领 due → 闸门 → CAS 领取 → 起 worker。
func (m *Manager) tick(ctx context.Context) {
	now := time.Now().UnixMilli()
	due, err := m.st.DueWaiting(now, 20)
	if err != nil || len(due) == 0 {
		return
	}
	total, byPlatform, err := m.st.ActiveConcurrency()
	if err != nil {
		return
	}
	maxGlobal := m.cfg.GetInt("download.max_concurrent")
	if maxGlobal <= 0 {
		maxGlobal = 3
	}
	platformCap := m.cfg.GetInt("download.platform_cap")
	if platformCap <= 0 {
		platformCap = 2
	}
	diskOK, diskMsg := m.diskGate()
	for _, t := range due {
		if !diskOK {
			m.hub.Broadcast("system.warn", map[string]any{"code": "DISK_LOW", "message": diskMsg})
			break // 磁盘水位不健康：本轮不领
		}
		if total >= maxGlobal {
			break // 全局并发满
		}
		p := t.Platform
		if p == "" {
			p = "web"
		}
		if byPlatform[p] >= platformCap {
			continue
		}
		claimed, err := m.st.ClaimWaiting(t.ID, "parsing")
		if err != nil {
			continue // 被抢/已转态
		}
		byPlatform[p]++
		total++
		m.startWorker(ctx, claimed)
	}
}

// diskGate 磁盘水位闸门（docs/05 §3：剩余 <5% 或 <1GB 停领新任务）。
func (m *Manager) diskGate() (bool, string) {
	dir := m.cfg.GetString("storage.data_dir")
	free, total, err := statFree(dir)
	if err != nil {
		return true, "" // 探测失败不阻塞（waiting 侧如实失败）
	}
	warnPct := m.cfg.GetInt("storage.disk_warn_pct")
	if warnPct <= 0 {
		warnPct = 5
	}
	var usedPct int
	if total > 0 {
		usedPct = int((total - free) * 100 / total)
	}
	if 100-usedPct < warnPct || free < 1<<30 {
		return false, fmt.Sprintf("磁盘余量不足（剩余 %d%% / %d MB），已暂停领取新任务", 100-usedPct, free>>20)
	}
	return true, ""
}

func (m *Manager) startWorker(ctx context.Context, t *store.Task) {
	wctx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	if m.running[t.ID] {
		m.mu.Unlock()
		cancel()
		return
	}
	m.cancels[t.ID] = cancel
	m.running[t.ID] = true
	m.mu.Unlock()
	w := &Worker{mgr: m, eng: m.engines.EngineFor(t.URL)}
	go func() {
		defer func() {
			m.mu.Lock()
			delete(m.cancels, t.ID)
			delete(m.running, t.ID)
			m.mu.Unlock()
		}()
		w.Run(wctx, t)
	}()
}

// Cancel 请求取消（幂等；终态任务直接 200——由 handler 判）。
func (m *Manager) Cancel(taskID string) error {
	t, err := m.st.GetTask(taskID)
	if err != nil {
		return err
	}
	if storeTaskTerminal(t.Status) {
		return nil // 幂等
	}
	if !t.CancelRequested {
		_ = m.st.UpdateTaskFields(taskID, map[string]any{"cancel_requested": true}, "")
	}
	m.mu.Lock()
	cancel, ok := m.cancels[taskID]
	m.mu.Unlock()
	if ok {
		cancel() // 杀进程组（engine 支持中断）
	}
	_ = m.st.AppendEvent(taskID, "info", "cancel_requested", "用户请求取消", "")
	return nil
}

// Retry 手动重试（仅 failed/cancelled → waiting，attempt+1，保留原 config_version）。
func (m *Manager) Retry(taskID string) error {
	t, err := m.st.GetTask(taskID)
	if err != nil {
		return err
	}
	if t.Status != "failed" && t.Status != "cancelled" {
		return &ErrRetryNotAllowed{Status: t.Status}
	}
	if err := m.st.UpdateTaskFields(taskID, map[string]any{
		"status":           "waiting",
		"attempt":          t.Attempt + 1,
		"error_code":       nil,
		"error_message":    nil,
		"fix_hint":         nil,
		"cancel_requested": false,
		"scheduled_at":     nil,
	}, t.Status); err != nil {
		return err
	}
	_ = m.st.AppendEvent(taskID, "info", "retry", fmt.Sprintf("手动重试（第 %d 次尝试）", t.Attempt+1), "")
	m.hub.Broadcast("task.status", map[string]any{"task_id": taskID, "status": "waiting"})
	return nil
}

// SubmitItem 创建入参（docs/04 §2.2 POST /tasks items[]）。
type SubmitItem struct {
	URL      string         `json:"url"`
	Quality  string         `json:"quality,omitempty"`
	SavePath string         `json:"save_path,omitempty"`
	Options  map[string]any `json:"options,omitempty"`
}

// SubmitResult 创建结果（含幂等冲突的 existing_task_id）。
type SubmitResult struct {
	Task       *store.Task `json:"task"`
	ExistingID string      `json:"existing_task_id,omitempty"`
}

// Submit 批量创建（单条 dedupe 冲突不阻塞其余）。
func (m *Manager) Submit(items []SubmitItem, source, sourceRef string) ([]SubmitResult, error) {
	results := make([]SubmitResult, 0, len(items))
	cfgVersion := m.cfg.Version()
	maxAttempts := m.cfg.GetInt("download.max_attempts")
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	for _, it := range items {
		norm := store.NormalizeURL(it.URL)
		platform := engine.PlatformGuess(norm)
		quality := it.Quality
		if quality == "" {
			quality = m.cfg.GetString("download.default_quality")
		}
		t := &store.Task{
			ID:            store.NewTaskID(),
			Kind:          KindDownload,
			Source:        source,
			SourceRef:     sourceRef,
			Platform:      platform,
			URL:           norm,
			DedupeKey:     store.DedupeKey(KindDownload, platform, norm),
			Status:        StatusWaiting, // 回填：CreateTask 固定落 'waiting'
			Quality:       quality,
			SavePath:      it.SavePath,
			ConfigVersion: cfgVersion,
			MaxAttempts:   maxAttempts,
			CreatedAt:     time.Now().UnixMilli(),
			UpdatedAt:     time.Now().UnixMilli(),
		}
		err := m.st.CreateTask(t)
		var de *store.ErrDedupeActive
		if errors.As(err, &de) {
			results = append(results, SubmitResult{Task: nil, ExistingID: de.ExistingID})
			continue
		}
		if err != nil {
			return results, err
		}
		_ = m.st.AppendEvent(t.ID, "info", "created", fmt.Sprintf("任务创建（来源 %s）", source), "")
		m.hub.Broadcast("task.status", map[string]any{"task_id": t.ID, "status": "waiting"})
		results = append(results, SubmitResult{Task: t})
	}
	return results, nil
}

// ErrRetryNotAllowed 非 failed/cancelled 禁止重试。
type ErrRetryNotAllowed struct{ Status string }

func (e *ErrRetryNotAllowed) Error() string { return "当前状态不可重试: " + e.Status }

// Hub 暴露广播（handler 的 SSE 路由用）。
func (m *Manager) Hub() *sse.Hub { return m.hub }

// Engines 暴露引擎集（handler 自检/503 语义用）。
func (m *Manager) Engines() *engine.Set { return m.engines }

// Preview 解析预览（POST /parse 行动作，不落库）。
func (m *Manager) Preview(ctx context.Context, url string) (*store.ParsePreview, error) {
	res, err := m.engines.YTDLP.Resolve(ctx, url, engine.ResolveOpts{})
	if err != nil {
		return nil, err
	}
	pv := &store.ParsePreview{
		URL:        url,
		Platform:   res.Platform,
		Title:      res.Title,
		Thumbnail:  res.Thumbnail,
		Uploader:   res.Uploader,
		Duration:   nilIfZero(res.Duration),
		IsPlaylist: res.IsPlaylist,
		Formats:    make([]store.FormatOption, 0, len(res.Formats)),
	}
	for _, f := range res.Formats {
		pv.Formats = append(pv.Formats, store.FormatOption{Quality: f.Quality, Size: f.Size, Note: f.Note})
	}
	if res.IsPlaylist {
		pv.Kind = "playlist"
	} else if len(res.Formats) > 0 {
		pv.Kind = "video"
	} else {
		pv.Kind = "unknown"
	}
	return pv, nil
}

func storeTaskTerminal(s string) bool { return s == "done" || s == "cancelled" }

func nilIfZero(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

// statFree 磁盘余量（bytes）；与 server.handleHealthz 同源逻辑（task 侧独立，避免包循环）。
func statFree(dir string) (free, total uint64, err error) {
	var st syscall.Statfs_t
	if err = syscall.Statfs(dir, &st); err != nil {
		return 0, 0, err
	}
	return st.Bavail * uint64(st.Bsize), st.Blocks * uint64(st.Bsize), nil
}

// SafeJoin 校验产物路径在数据根内（防命名穿越；文件删除端点同用）。
func SafeJoin(root, rel string) (string, bool) {
	if strings.Contains(rel, "..") {
		return "", false
	}
	abs := filepath.Clean(filepath.Join(root, rel))
	if !strings.HasPrefix(abs, filepath.Clean(root)) {
		return "", false
	}
	return abs, true
}
