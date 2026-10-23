package task

// external.go：外部存储/下载通道的任务执行器（docs/08 W4.6/W4.7）。
//
// 为什么与下载任务分通道：
//
//	qB 与 115 的「下载」不由本进程执行 —— 种子由 qB 客户端拉取，离线任务由 115 云端
//	拉取。本进程只做三件事：提交、查询、转述进度。
//
//	因此它们复用同一套七态状态机（保证「任意入口同队列同状态」不变式），
//	但走不同的执行体：
//	  - qb   : parsing=提交到 qB → downloading=轮询 qB 状态 → post=登记文件
//	  - n115 : parsing=提交离线任务 → downloading=轮询云端状态 → post=登记文件
//
//	不可用语义（docs/01 §5.8）：qB/115 不可用时**仅相关任务失败**，应用其他功能可用。

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/huluohu/raccoon/internal/store"
)

// QBClient 是 qB 客户端的能力接口（由 internal/qb 实现，接口化避免 task 包依赖具体包）。
type QBClient interface {
	// AddMagnet 提交磁力链接，返回 info_hash。
	AddMagnet(ctx context.Context, magnet string, opts QBAddOptions) (string, error)
	// Torrents 查询种子状态；hashes 为空表示查全部。
	Torrents(ctx context.Context, hashes []string) ([]QBTorrent, error)
	// Delete 删除种子（用户取消磁力任务时调用，避免留下僵尸下载）。
	Delete(ctx context.Context, hashes []string, deleteFiles bool) error
}

// N115Client 是 115 客户端的能力接口。
type N115Client interface {
	// SubmitOffline 提交离线任务。
	SubmitOffline(ctx context.Context, req N115Request) (*N115Task, error)
	// GetOfflineTask 查询离线任务状态。
	GetOfflineTask(ctx context.Context, ref string) (*N115Task, error)
}

// QBAddOptions qB 提交参数。
type QBAddOptions struct {
	SavePath  string
	Category  string
	Tags      []string
	Paused    bool
	SkipCheck bool
}

// QBTorrent qB 种子状态快照。
type QBTorrent struct {
	Hash       string
	State      string
	Percent    float64
	Size       int64
	SavePath   string
	Name       string
	Downloaded int64
}

// N115Request 115 离线提交参数。
type N115Request struct {
	URL      string
	WPPath   string
	WPPathID string
	CID      string
}

// N115Task 115 离线任务状态快照。
type N115Task struct {
	OfflineID string
	TaskID    string
	Status    string
	Progress  int
	Size      int64
	Name      string
	Files     []N115File
}

// N115File 115 离线结果文件。
type N115File struct {
	FileID string
	Name   string
	Size   int64
	IsDir  bool
}

// ExternalExecutor 执行 qB / 115 类任务。
type ExternalExecutor struct {
	mgr   *Manager
	qb    QBClient
	n115  N115Client
	pollI time.Duration // 轮询间隔
}

// NewExternalExecutor 构造。
func NewExternalExecutor(mgr *Manager, qb QBClient, n115 N115Client) *ExternalExecutor {
	return &ExternalExecutor{mgr: mgr, qb: qb, n115: n115, pollI: 5 * time.Second}
}

// Supports 判断该任务类型是否由本执行器处理。
func (e *ExternalExecutor) Supports(kind string) bool {
	return kind == KindQB || kind == KindN115
}

// Run 执行外部任务全生命周期。入口状态为 parsing。
func (e *ExternalExecutor) Run(ctx context.Context, t *store.Task) {
	switch t.Kind {
	case KindQB:
		e.runQB(ctx, t)
	case KindN115:
		e.runN115(ctx, t)
	}
}

// runQB 磁力/种子任务：提交 → 轮询 → 完成。
func (e *ExternalExecutor) runQB(ctx context.Context, t *store.Task) {
	if e.qb == nil {
		e.failWithRoute(t, "QB_UNAVAILABLE", "未配置 qBittorrent 连接，该任务无法执行", fixHintGotoSettings(), "qB 不可用")
		return
	}
	hash, err := e.qb.AddMagnet(ctx, t.URL, QBAddOptions{
		SavePath: t.SavePath,
	})
	if err != nil {
		e.failWithRoute(t, "QB_SUBMIT_FAILED", err.Error(), fixHintGotoSettings(), "qB 提交失败")
		return
	}
	_ = e.mgr.st.AppendEvent(t.ID, "info", "qb_submitted", "已提交到 qBittorrent", fmt.Sprintf(`{"hash":%q}`, hash))
	_ = e.mgr.st.UpdateTaskFields(t.ID, map[string]any{
		"title":  t.Title,
		"engine": "qbittorrent",
	}, "parsing")
	if err := e.transition(t.ID, "parsing", "downloading"); err != nil {
		return
	}
	e.pollQB(ctx, t, hash)
}

// pollQB 轮询 qB 直到完成/失败/取消。
func (e *ExternalExecutor) pollQB(ctx context.Context, t *store.Task, hash string) {
	ticker := time.NewTicker(e.pollI)
	defer ticker.Stop()
	// 连续查询失败计数：qB 持续不可用时必须有出口，否则任务永远卡在 downloading
	// 白占 worker 额度（旧实现用 Attempt+1<MaxAttempts 判退出，但 Attempt 在轮询期
	// 从不递增——死循环。现在按连续失败次数收敛，与 max_attempts 语义对齐）。
	queryErrs := 0
	for {
		select {
		case <-ctx.Done():
			e.markCancelled(t.ID, "downloading")
			return
		case <-ticker.C:
		}
		// 取消位检查（DB 轮询，与下载任务同语义）。
		if cur, err := e.mgr.st.GetTask(t.ID); err == nil {
			if cur.CancelRequested {
				_ = e.qb.Delete(ctx, []string{hash}, false)
				e.markCancelled(t.ID, "downloading")
				return
			}
		}
		torrents, err := e.qb.Torrents(ctx, []string{hash})
		if err != nil {
			// qB 短暂不可用不立即失败：连续失败到 max_attempts 次再判失败。
			queryErrs++
			if queryErrs < t.MaxAttempts {
				continue
			}
			e.fail(t.ID, "QB_QUERY_FAILED", err.Error(), fixHintRetry())
			return
		}
		queryErrs = 0
		if len(torrents) == 0 {
			// qB 还没登记（异步），继续等。
			continue
		}
		tr := torrents[0]
		pct := tr.Percent
		_ = e.mgr.st.UpdateTaskFields(t.ID, map[string]any{
			"progress":        pct,
			"downloaded_size": tr.Downloaded,
			"expected_size":   tr.Size,
		}, "")
		e.mgr.hub.Broadcast("task.progress", map[string]any{
			"task_id": t.ID, "progress": pct, "speed_bps": 0,
			"downloaded_size": tr.Downloaded, "expected_size": tr.Size,
			"progress_known": true,
		})
		switch tr.State {
		case "error", "failed", "missing":
			e.fail(t.ID, "QB_FAILED", "qBittorrent 报告种子状态异常: "+tr.State, fixHintGotoSettings())
			return
		case "completed", "seeding", "uploading", "stalled":
			// 种子任务在 qB 里是「完成即做种」，本系统视为完成（文件已落 qB 保存目录）。
			e.finish(t, tr.SavePath, tr.Name, tr.Size)
			return
		}
	}
}

// finish qB/115 任务完成：登记文件与产物路径。
func (e *ExternalExecutor) finish(t *store.Task, savePath, name string, size int64) {
	now := time.Now().UnixMilli()
	if name == "" {
		name = t.Title
	}
	if savePath != "" {
		path := savePath
		if !e.pathExists(path) {
			path = savePath + "/" + name
		}
		_ = e.mgr.st.AddTaskFile(&store.TaskFile{
			ID: store.NewTaskID(), TaskID: t.ID, Path: path, Size: size,
			Mime: "", Kind: "torrent", CreatedAt: now,
		})
		_ = e.mgr.st.UpsertMediaFile(&store.MediaFile{
			ID: store.NewTaskID(), TaskID: t.ID, Path: path, Name: name,
			Size: size, Mime: "", Platform: t.Platform, Kind: store.MediaKindOf("", name),
			CreatedAt: now,
		})
	}
	if err := e.transition(t.ID, "downloading", "post"); err != nil {
		return
	}
	_ = e.mgr.st.UpdateTaskFields(t.ID, map[string]any{"progress": 100, "downloaded_size": size}, "post")
	if err := e.transition(t.ID, "post", "done"); err != nil {
		return
	}
	_ = e.mgr.st.UpdateTaskFields(t.ID, map[string]any{"finished_at": now}, "post")
	_ = e.mgr.st.AppendEvent(t.ID, "info", "done", "任务完成", "")
	e.mgr.hub.Broadcast("task.done", map[string]any{"task_id": t.ID, "status": "done"})
	e.mgr.notify("task_done", "success", "任务完成："+name, "", t.ID)
}

// runN115 115 离线任务：提交 → 轮询 → 完成。
// n115_first 策略下，115 确定失败时自动转 qB（docs/15 §3.7 failover）。
func (e *ExternalExecutor) runN115(ctx context.Context, t *store.Task) {
	if e.n115 == nil {
		e.failN115(t, "N115_UNAVAILABLE", "未配置 115 连接，该任务无法执行", fixHintGotoSettings())
		return
	}
	sub, err := e.n115.SubmitOffline(ctx, N115Request{
		URL:    t.URL,
		WPPath: t.SavePath,
	})
	if err != nil {
		e.failN115(t, "N115_SUBMIT_FAILED", err.Error(), fixHintGotoSettings())
		return
	}
	ref := sub.OfflineID
	if ref == "" {
		ref = sub.TaskID
	}
	_ = e.mgr.st.AppendEvent(t.ID, "info", "n115_submitted", "已提交 115 离线任务", fmt.Sprintf(`{"ref":%q}`, ref))
	_ = e.mgr.st.UpdateTaskFields(t.ID, map[string]any{
		"title":      firstNonEmptyStr(sub.Name, t.Title),
		"engine":     "115",
		"route_path": AppendRoutePath(t.RoutePath, "115 提交成功"),
	}, "parsing")
	if err := e.transition(t.ID, "parsing", "downloading"); err != nil {
		return
	}
	e.pollN115(ctx, t, ref)
}

// pollN115 轮询 115 离线任务状态。
func (e *ExternalExecutor) pollN115(ctx context.Context, t *store.Task, ref string) {
	ticker := time.NewTicker(e.pollI)
	defer ticker.Stop()
	// 连续查询失败计数（同 pollQB：轮询期 Attempt 不递增，必须独立收敛，
	// 否则 115 持续不可用时任务永远卡 downloading 白占 worker）。
	queryErrs := 0
	for {
		select {
		case <-ctx.Done():
			e.markCancelled(t.ID, "downloading")
			return
		case <-ticker.C:
		}
		if cur, err := e.mgr.st.GetTask(t.ID); err == nil && cur.CancelRequested {
			e.markCancelled(t.ID, "downloading")
			return
		}
		cur, err := e.n115.GetOfflineTask(ctx, ref)
		if err != nil {
			queryErrs++
			if queryErrs < t.MaxAttempts {
				continue
			}
			e.fail(t.ID, "N115_QUERY_FAILED", err.Error(), fixHintRetry())
			return
		}
		queryErrs = 0
		pct := float64(cur.Progress)
		_ = e.mgr.st.UpdateTaskFields(t.ID, map[string]any{
			"progress":      pct,
			"expected_size": cur.Size,
		}, "")
		e.mgr.hub.Broadcast("task.progress", map[string]any{
			"task_id": t.ID, "progress": pct, "speed_bps": 0,
			"expected_size": cur.Size, "progress_known": true,
		})
		switch cur.Status {
		case "failed", "error":
			// 云端报告失败 = 确定失败：n115_first 策略下允许转 qB（115 侧任务已失败，
			// 不存在两边重复下载）；其余策略按普通失败落库。
			e.failN115(t, "N115_FAILED", "115 离线任务失败", fixHintGotoSettings())
			return
		case "done", "completed", "success":
			var total int64
			for _, f := range cur.Files {
				total += f.Size
			}
			e.finish(t, t.SavePath, firstNonEmptyStr(cur.Name, t.Title), total)
			return
		}
	}
}

// transition 状态推进（与 Worker.transition 同语义，外部任务独立持有一份）。
func (e *ExternalExecutor) transition(id, from, to string) error {
	err := e.mgr.st.UpdateTaskFields(id, map[string]any{"status": to}, from)
	if err != nil {
		return err
	}
	e.mgr.hub.Broadcast("task.status", map[string]any{"task_id": id, "status": to})
	return nil
}

// fail 置失败并广播。
func (e *ExternalExecutor) fail(taskID, code, msg, hint string) {
	now := time.Now().UnixMilli()
	_ = e.mgr.st.UpdateTaskFields(taskID, map[string]any{
		"status": "failed", "error_code": code, "error_message": msg,
		"fix_hint": hint, "finished_at": now,
	}, "")
	_ = e.mgr.st.AppendEvent(taskID, "error", "failed", code+": "+msg, "")
	e.mgr.hub.Broadcast("task.failed", map[string]any{"task_id": taskID, "status": "failed", "error_code": code})
	e.mgr.notify("task_failed", "error", "任务失败："+code, msg, taskID)
}

// failWithRoute 置失败，并把最后一段链路追补进 route_path（「走了哪条路」不留断点）。
func (e *ExternalExecutor) failWithRoute(t *store.Task, code, msg, hint, routeSeg string) {
	if routeSeg != "" {
		_ = e.mgr.st.UpdateTaskFields(t.ID, map[string]any{
			"route_path": AppendRoutePath(t.RoutePath, routeSeg),
		}, "")
	}
	e.fail(t.ID, code, msg, hint)
}

// failN115 115 失败出口：策略=n115_first 且确定失败时自动转 qB（failover），
// 否则按普通失败落库。attempt 不递增——路由转移不是重试（docs/15 §3.7）。
func (e *ExternalExecutor) failN115(t *store.Task, code, msg, hint string) {
	strategy := e.mgr.cfg.GetString("storage.strategy")
	if !ShouldFailoverToQB(strategy, code) {
		e.failWithRoute(t, code, msg, hint, "115 失败")
		return
	}
	// 转 qB：kind 改写 + 回 waiting 队列由调度循环重新领取。
	// 前提（IsDefiniteN115Failure 保证）：115 侧未产生远程副作用，不会两边重复下载。
	newRoute := AppendRoutePath(t.RoutePath, "115 失败 → 转 qB")
	err := e.mgr.st.UpdateTaskFields(t.ID, map[string]any{
		"kind":       KindQB,
		"platform":   "qb",
		"status":     StatusWaiting,
		"route_path": newRoute,
		"error_code": "", "error_message": "", "fix_hint": "",
	}, "")
	if err != nil {
		// CAS 失败（状态已被并发推进）：回落普通失败，不重试转移。
		e.failWithRoute(t, code, msg, hint, "115 失败")
		return
	}
	_ = e.mgr.st.AppendEvent(t.ID, "warn", "route_failover",
		"115 确定失败，按策略 n115_first 自动转 qB 重新入队", "")
	e.mgr.hub.Broadcast("task.status", map[string]any{
		"task_id": t.ID, "status": "waiting", "route_path": newRoute,
	})
}

// markCancelled 置取消。
func (e *ExternalExecutor) markCancelled(taskID, from string) {
	now := time.Now().UnixMilli()
	_ = e.mgr.st.UpdateTaskFields(taskID, map[string]any{
		"status": "cancelled", "finished_at": now,
	}, from)
	_ = e.mgr.st.AppendEvent(taskID, "info", "cancelled", "任务已取消", "")
	e.mgr.hub.Broadcast("task.status", map[string]any{"task_id": taskID, "status": "cancelled"})
}

// pathExists 判断路径是否存在（用于判断 qB 保存目录 vs 具体文件）。
func (e *ExternalExecutor) pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// firstNonEmptyStr 返回首个非空字符串。
func firstNonEmptyStr(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
