package task

// worker.go：单任务生命周期（docs/05 §4）。状态推进只走 CAS；
// 取消语义：cancel_requested（DB）+ ctx cancel（内存）双通道，任一触发杀进程组。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/huluohu/raccoon/internal/engine"
	"github.com/huluohu/raccoon/internal/store"
)

// Worker 执行单个任务（无状态，可并发多实例）。
type Worker struct {
	mgr *Manager
	eng engine.Engine
}

// Run 执行任务全生命周期。约定：入口状态必为 parsing（manager CAS 后启动）。
func (w *Worker) Run(ctx context.Context, t *store.Task) {
	// panic 兜底（docs/05 §4：任务回 waiting + worker_panic 事件，attempt+1）。
	defer func() {
		if rec := recover(); rec != nil {
			w.mgr.log.Error("worker panic 已恢复", map[string]any{"task_id": t.ID, "panic": fmt.Sprint(rec)})
			_ = w.mgr.st.AppendEvent(t.ID, "error", "worker_panic", fmt.Sprintf("内部错误已恢复: %v", rec), "")
			w.requeue(t.ID, "worker_panic")
		}
	}()

	// 取消监听（DB 轮询兜底；manager.Cancel 已可直接 cancel ctx）。
	w.startCancelWatcher(ctx, t.ID)

	// 引擎自检（缺失 → 503 类失败，不可重试短路）。
	if err := w.eng.Available(); err != nil {
		w.fail(t.ID, "ENGINE_UNAVAILABLE", err.Error(), fixHintGotoSettings())
		return
	}

	// ---- parsing ----
	res, err := w.eng.Resolve(ctx, t.URL, engine.ResolveOpts{})
	if err != nil {
		w.handleEngineErr(ctx, t, "parsing", err)
		return
	}
	plat := res.Platform
	if plat == "" {
		plat = t.Platform
	}
	ver := w.eng.Version()
	if err := w.mgr.st.UpdateTaskFields(t.ID, map[string]any{
		"title":          res.Title,
		"thumbnail":      res.Thumbnail,
		"platform":       plat,
		"engine":         w.eng.Name(),
		"engine_version": ver,
	}, "parsing"); err != nil {
		w.mgr.log.Error("解析结果写库失败", map[string]any{"task_id": t.ID, "err": err.Error()})
	}
	if res.Duration > 0 {
		_ = w.mgr.st.AppendEvent(t.ID, "info", "parsed", fmt.Sprintf("解析成功：%s", res.Title),
			fmt.Sprintf(`{"duration":%d,"platform":%q}`, res.Duration, plat))
	} else {
		_ = w.mgr.st.AppendEvent(t.ID, "info", "parsed", fmt.Sprintf("解析成功：%s", res.Title), "")
	}
	if err := w.transition(t.ID, "parsing", "downloading", nil); err != nil {
		// 取消/异常已在别处处理。
		w.mgr.log.Warn("parsing→downloading 失败", map[string]any{"task_id": t.ID, "err": err.Error()})
	}

	// ---- downloading ----
	saveDir := w.saveDirFor(t, plat)
	opts := engine.DownloadOpts{
		Format:   w.formatSelector(t.Quality),
		SaveDir:  saveDir,
		NameTmpl: "%(title).120s [%(id)s].%(ext)s",
	}
	var lastWrite time.Time
	onProgress := func(p engine.Progress) {
		if time.Since(lastWrite) < time.Second {
			return // 节流 1s（docs/05 §4）
		}
		lastWrite = time.Now()
		pct := p.Percent
		if pct < 0 {
			pct = 0 // DB 列 NOT NULL DEFAULT 0；未知进度前端按 -1 口径展示 expected_size=NULL
		}
		_ = w.mgr.st.UpdateTaskFields(t.ID, map[string]any{
			"downloaded_size": p.Downloaded,
			"speed_bps":       p.SpeedBps,
			"progress":        pct,
		}, "")
		w.mgr.hub.Broadcast("task.progress", map[string]any{
			"task_id": t.ID, "progress": pct, "speed_bps": p.SpeedBps,
			"eta_sec": p.ETASec, "downloaded_size": p.Downloaded, "expected_size": p.Total,
			"progress_known": p.Percent >= 0,
		})
	}
	files, err := w.eng.Download(ctx, t.URL, opts, onProgress)
	if err != nil {
		w.handleEngineErr(ctx, t, "downloading", err)
		return
	}
	if len(files) == 0 {
		w.fail(t.ID, "NO_OUTPUT", "引擎未产出文件", fixHintRetry())
		return
	}

	// ---- post / done ----
	w.finish(t, files, res.Duration, plat, res.Uploader)
}

// finish 落盘登记 + media 索引 + 终态（docs/05 §2 post→done 副作用）。
func (w *Worker) finish(t *store.Task, files []engine.DownloadedFile, duration int64, platform, uploader string) {
	now := time.Now().UnixMilli()
	var total int64
	var firstPath, firstName, firstMime string
	for _, f := range files {
		kind := guessKind(filepath.Ext(f.Path))
		mime := guessMime(filepath.Ext(f.Path))
		_ = w.mgr.st.AddTaskFile(&store.TaskFile{
			ID: store.NewTaskID(), TaskID: t.ID, Path: f.Path, Size: f.Size,
			Mime: mime, Kind: kind, CreatedAt: now,
		})
		// 主文件：第一个非字幕/元数据产物。
		if firstPath == "" && kind != "subtitle" && kind != "metadata" {
			firstPath, firstName, firstMime = f.Path, filepath.Base(f.Path), mime
		}
		total += f.Size
	}
	var dur *int64
	if duration > 0 {
		dur = &duration
	}
	if firstPath != "" {
		_ = w.mgr.st.AddMediaFile(&store.MediaFile{
			ID: store.NewTaskID(), TaskID: &t.ID, Path: firstPath, Name: firstName,
			Size: total, Mime: firstMime, Duration: dur, Platform: platform, Uploader: uploader, CreatedAt: now,
		})
	}
	if err := w.transition(t.ID, "downloading", "post", nil); err != nil {
		return
	}
	_ = w.mgr.st.UpdateTaskFields(t.ID, map[string]any{
		"downloaded_size": total,
		"progress":        100,
	}, "post")
	if err := w.transition(t.ID, "post", "done", map[string]any{
		"finished_at": now,
	}); err != nil {
		return
	}
	_ = w.mgr.st.AppendEvent(t.ID, "info", "done", fmt.Sprintf("下载完成：%d 个文件，共 %d 字节", len(files), total), "")
	w.mgr.hub.Broadcast("task.done", map[string]any{"task_id": t.ID, "status": "done", "files": len(files), "size": total})
}

// transition CAS 状态推进（fromStatus 匹配才更新；终态/取消快速失败）。
func (w *Worker) transition(id, from, to string, extra map[string]any) error {
	fields := map[string]any{"status": to}
	for k, v := range extra {
		fields[k] = v
	}
	err := w.mgr.st.UpdateTaskFields(id, fields, from)
	if err != nil {
		// CAS 失败常见原因：已被取消或状态已变。终态核对。
		if cur, e := w.mgr.st.GetTask(id); e == nil && (cur.Status == "cancelled" || cur.Status == "failed") {
			return errAlreadyTerminal
		}
		return err
	}
	w.mgr.hub.Broadcast("task.status", map[string]any{"task_id": id, "status": to})
	return nil
}

var errAlreadyTerminal = errors.New("task already terminal")

// handleEngineErr 引擎错误分类处理：取消 → cancelled；可重试 → 退避回 waiting；否则 failed。
func (w *Worker) handleEngineErr(ctx context.Context, t *store.Task, from string, err error) {
	// 取消优先判定（进程被杀/用户取消）；以 DB 最新状态为准。
	cur, curErr := w.mgr.st.GetTask(t.ID)
	if errors.Is(err, context.Canceled) || (curErr == nil && cur.CancelRequested) {
		w.markCancelled(t.ID, from)
		return
	}
	var ee *engine.EngineError
	code, msg := "ENGINE_ERROR", err.Error()
	if errors.As(err, &ee) {
		code, msg = ee.Code, ee.Message
	}
	if code == "CANCELLED" {
		w.markCancelled(t.ID, from)
		return
	}
	if curErr == nil && (cur.Status == "cancelled" || cur.Status == "failed" || cur.Status == "done") {
		return // 已被其他路径终态化
	}
	// 重试判定（docs/05 §7）：可重试错误码且 attempt<max → 指数退避回 waiting。
	if isRetryableCode(code) && t.Attempt+1 < t.MaxAttempts {
		backoff := backoffFor(t.Attempt)
		_ = w.mgr.st.UpdateTaskFields(t.ID, map[string]any{
			"status":        "waiting",
			"wait_reason":   "auto_retry",
			"scheduled_at":  time.Now().Add(backoff).UnixMilli(),
			"error_code":    code,
			"error_message": msg,
			"speed_bps":     0,
		}, from)
		_ = w.mgr.st.AppendEvent(t.ID, "warn", "retry_scheduled",
			fmt.Sprintf("%s：%s；%s 后自动重试（第 %d/%d 次）", code, msg, backoff, t.Attempt+2, t.MaxAttempts), "")
		w.mgr.hub.Broadcast("task.status", map[string]any{"task_id": t.ID, "status": "waiting", "wait_reason": "auto_retry"})
		return
	}
	w.fail(t.ID, code, msg, fixHintFor(code))
}

// fail 置 failed + fix_hint（docs/04 §3 task.failed 事件携带修复建议）。
func (w *Worker) fail(taskID, code, msg, hint string) {
	now := time.Now().UnixMilli()
	_ = w.mgr.st.UpdateTaskFields(taskID, map[string]any{
		"status": "failed", "error_code": code, "error_message": msg,
		"fix_hint": hint, "finished_at": now, "speed_bps": 0,
	}, "")
	_ = w.mgr.st.AppendEvent(taskID, "error", "failed", fmt.Sprintf("%s：%s", code, msg), "")
	w.mgr.hub.Broadcast("task.failed", mustJSON(map[string]any{
		"task_id": taskID, "status": "failed", "error_code": code,
		"fix_hint": parseHint(hint),
	}))
}

// markCancelled 置 cancelled（幂等）。
func (w *Worker) markCancelled(taskID, from string) {
	cur, err := w.mgr.st.GetTask(taskID)
	if err == nil && cur.Status == "cancelled" {
		return
	}
	now := time.Now().UnixMilli()
	_ = w.mgr.st.UpdateTaskFields(taskID, map[string]any{
		"status": "cancelled", "finished_at": now, "speed_bps": 0, "cancel_requested": false,
	}, from)
	_ = w.mgr.st.AppendEvent(taskID, "info", "cancelled", "任务已取消", "")
	w.mgr.hub.Broadcast("task.status", map[string]any{"task_id": taskID, "status": "cancelled"})
}

// requeue panic 兜底回队列。
func (w *Worker) requeue(taskID, reason string) {
	cur, err := w.mgr.st.GetTask(taskID)
	if err != nil {
		return
	}
	_ = w.mgr.st.UpdateTaskFields(taskID, map[string]any{
		"status": "waiting", "wait_reason": reason,
		"attempt": cur.Attempt + 1, "speed_bps": 0,
	}, "")
	w.mgr.hub.Broadcast("task.status", map[string]any{"task_id": taskID, "status": "waiting", "wait_reason": reason})
}

// startCancelWatcher DB 轮询取消位（1s）；命中即 cancel ctx（杀进程组）。
func (w *Worker) startCancelWatcher(ctx context.Context, taskID string) {
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				cur, err := w.mgr.st.GetTask(taskID)
				if err != nil {
					continue
				}
				if cur.CancelRequested || cur.Status == "cancelled" || cur.Status == "failed" || cur.Status == "done" {
					return // manager.Cancel 已处理或已终态
				}
			}
		}
	}()
}

// saveDir 计算保存目录：save_path（相对数据根，SafeJoin 校验）或 download_dir/platform/。
func (w *Worker) saveDirFor(t *store.Task, platform string) string {
	root := w.mgr.cfg.GetString("storage.data_dir")
	base := w.mgr.cfg.GetString("storage.download_dir")
	if base == "" {
		base = "downloads"
	}
	if t.SavePath != "" {
		if abs, ok := SafeJoin(root, t.SavePath); ok {
			return abs
		}
	}
	p := platform
	if p == "" {
		p = "web"
	}
	return filepath.Join(root, base, p)
}

// formatSelector 任务质量 → yt-dlp 表达式（引擎内实现一致口径，此处仅透传）。
func (w *Worker) formatSelector(q string) string {
	return q // 空串由 engine 默认 best 处理
}

func isRetryableCode(code string) bool {
	switch code {
	case "NET_TIMEOUT", "HTTP_5XX", "RATE_LIMITED", "ENGINE_CRASH", "WORKER_PANIC", "ENGINE_ERROR":
		return true
	}
	return false
}

// backoffFor 指数退避 30s/2m/10m（docs/05 §7）。
func backoffFor(attempt int) time.Duration {
	switch attempt {
	case 0:
		return 30 * time.Second
	case 1:
		return 2 * time.Minute
	default:
		return 10 * time.Minute
	}
}

// fixHintFor 错误码 → 修复建议（JSON：{action,label}；docs/04 §2.2）。
func fixHintFor(code string) string {
	switch code {
	case "AUTH_REQUIRED":
		return fixHintGotoCredentials()
	case "FORMAT_UNAVAILABLE":
		return fixHintChangeQuality()
	case "NOT_FOUND":
		return `{"action":"none","label":"链接不存在或已删除，请检查后重试"}`
	case "NET_TIMEOUT", "HTTP_5XX", "RATE_LIMITED":
		return fixHintRetry()
	case "ENGINE_UNAVAILABLE":
		return fixHintGotoSettings()
	}
	return fixHintRetry()
}

func fixHintGotoCredentials() string {
	return `{"action":"goto_credentials","label":"去「连接」页更新平台凭据"}`
}

func fixHintGotoSettings() string {
	return `{"action":"goto_settings","label":"检查「设置」页引擎与网络配置"}`
}

func fixHintChangeQuality() string {
	return `{"action":"change_quality","label":"更换清晰度后重试"}`
}

func fixHintRetry() string {
	return `{"action":"retry","label":"稍后重试或手动重试"}`
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{}`)
	}
	return b
}

func parseHint(hint string) map[string]string {
	var m map[string]string
	if err := json.Unmarshal([]byte(hint), &m); err != nil {
		return map[string]string{"action": "retry", "label": hint}
	}
	return m
}

// guessKind / guessMime 产物类型识别（task_files.kind CHECK 同源）。
func guessKind(ext string) string {
	switch strings.ToLower(ext) {
	case ".mp4", ".mkv", ".webm", ".mov", ".flv", ".ts", ".m4s", ".avi":
		return "video"
	case ".mp3", ".m4a", ".aac", ".flac", ".opus", ".wav", ".ogg":
		return "audio"
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
		return "image"
	case ".srt", ".vtt", ".ass":
		return "subtitle"
	case ".json", ".info.json", ".description", ".txt":
		return "metadata"
	}
	return "other"
}

func guessMime(ext string) string {
	switch strings.ToLower(ext) {
	case ".mp4":
		return "video/mp4"
	case ".mkv":
		return "video/x-matroska"
	case ".webm":
		return "video/webm"
	case ".mov":
		return "video/quicktime"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a":
		return "audio/mp4"
	case ".flac":
		return "audio/flac"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	}
	return ""
}
