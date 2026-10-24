package task

// worker.go：单任务生命周期（docs/05 §4）。状态推进只走 CAS；
// 取消语义：cancel_requested（DB）+ ctx cancel（内存）双通道，任一触发杀进程组。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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

	// 凭据 → 临时 cookie 文件（引擎按 --cookies 读取）。
	// 解密失败或凭据不存在都不阻断任务：无凭据时引擎照常跑，命中需要登录的内容时
	// 会返回 AUTH_REQUIRED，届时 fix_hint 指向「连接」页 —— 比在此处直接失败更准确。
	cookieFile := w.prepareCookieFile(t)
	defer w.cleanupCookieFile(cookieFile)

	// ---- parsing ----
	// 代理：net.proxy 是「网络出口」配置，解析与下载必须走同一出口——
	// 只给下载配代理会造成「解析成功（直连元数据页）、下载超时（媒体流不可达）」的错位。
	proxy := w.mgr.cfg.GetString("net.proxy")
	res, err := w.eng.Resolve(ctx, t.URL, engine.ResolveOpts{Cookies: cookieFile, Proxy: proxy})
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
		Format:         w.formatSelector(t.Quality),
		SaveDir:        saveDir,
		NameTmpl:       "%(title).120s [%(id)s].%(ext)s",
		Cookies:        cookieFile,
		HTTPProxy:      proxy,
		RateLimit:      int64(w.mgr.cfg.GetInt("download.rate_limit_bps")),
		WriteThumbnail: w.mgr.cfg.GetBool("download.save_thumbnail"),
		EmbedMetadata:  w.mgr.cfg.GetBool("download.embed_metadata"),
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
	// 先转 post 态再做格式转换：转换（尤其转码）可能耗时数分钟，
	// 期间 UI 显示「处理中」而不是挂在「下载中」的假进度上（docs/05 §4）。
	if err := w.transition(t.ID, "downloading", "post", nil); err != nil {
		return
	}
	files = w.convertFiles(ctx, t, files)
	if ctx.Err() != nil {
		w.markCancelled(t.ID, "post")
		return
	}
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
		_ = w.mgr.st.UpsertMediaFile(&store.MediaFile{
			ID: store.NewTaskID(), TaskID: t.ID, Path: firstPath, Name: firstName,
			Size: total, Mime: firstMime, Duration: dur, Platform: platform, Uploader: uploader,
			Kind: store.MediaKindOf(firstMime, firstName), CreatedAt: now,
		})
	}
	_ = w.mgr.st.UpdateTaskFields(t.ID, map[string]any{
		"downloaded_size": total,
		"progress":        100,
	}, "post")
	// 成功即清错误痕迹：失败过后来成功的任务不该在详情页还挂着旧错误
	//（ClaimWaiting 领取不清、错误字段只能在这里收尾清掉）。
	if err := w.transition(t.ID, "post", "done", map[string]any{
		"finished_at":   now,
		"error_code":    "",
		"error_message": "",
		"fix_hint":      "",
	}); err != nil {
		return
	}
	_ = w.mgr.st.AppendEvent(t.ID, "info", "done", fmt.Sprintf("下载完成：%d 个文件，共 %d 字节", len(files), total), "")
	w.mgr.hub.Broadcast("task.done", map[string]any{"task_id": t.ID, "status": "done", "files": len(files), "size": total})
	w.mgr.notify("task_done", "success", "下载完成："+t.Title, fmt.Sprintf("%d 个文件，共 %d 字节", len(files), total), t.ID)
	// 直播录制：任务终态即「下播」，产物回写录制记录（下播即成品）。
	w.closeLiveRecording(t, "completed", "", len(files), total)
	// 父任务聚合（docs/05 §5）：子任务终态后刷新父进度。
	w.mgr.aggregateParent(t.ParentID)
}

// convertFiles post 阶段格式转换（convert_to；docs/05 §4）。
//
// 语义取舍：转换失败不推翻下载成功——保留源格式文件、事件流记 warn，
// 任务照常 done。用户要的是「拿到片子」，一次 remux 失败不该把整个
// 下载判成白干。转换成功则删源文件（双份成品翻倍占盘，没人要）。
func (w *Worker) convertFiles(ctx context.Context, t *store.Task, files []engine.DownloadedFile) []engine.DownloadedFile {
	if t.ConvertTo == "" || !engine.ConvertSupported(t.ConvertTo) {
		return files
	}
	ffmpeg := w.mgr.Engines().FFmpegBin
	if ffmpeg == "" {
		_ = w.mgr.st.AppendEvent(t.ID, "warn", "convert_skipped", "ffmpeg 未配置，产物保留源格式", "")
		return files
	}
	out := make([]engine.DownloadedFile, 0, len(files))
	for _, f := range files {
		if ctx.Err() != nil {
			out = append(out, f)
			continue
		}
		if !convertible(filepath.Ext(f.Path), t.ConvertTo) {
			out = append(out, f) // 字幕/封面等配套产物不做格式转换
			continue
		}
		dst, mode, err := engine.ConvertFile(ctx, ffmpeg, f.Path, t.ConvertTo)
		if err != nil {
			_ = w.mgr.st.AppendEvent(t.ID, "warn", "convert_failed",
				fmt.Sprintf("转换失败，保留源格式 %s：%v", filepath.Base(f.Path), err), "")
			out = append(out, f)
			continue
		}
		if mode == "skip" {
			out = append(out, f)
			continue
		}
		var size int64
		if st, statErr := os.Stat(dst); statErr == nil {
			size = st.Size()
		}
		_ = os.Remove(f.Path)
		_ = w.mgr.st.AppendEvent(t.ID, "info", "converted",
			fmt.Sprintf("格式转换：%s → %s（%s）", filepath.Base(f.Path), filepath.Base(dst), mode), "")
		out = append(out, engine.DownloadedFile{Path: dst, Size: size})
	}
	return out
}

// convertible 源文件类型与目标格式的匹配：mp4 只收视频轨文件；mp3 从视频/音频提取。
func convertible(ext, target string) bool {
	kind := guessKind(ext)
	switch target {
	case "mp4":
		return kind == "video"
	case "mp3":
		return kind == "video" || kind == "audio"
	}
	return false
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
	detail := ""
	if errors.As(err, &ee) {
		code, msg = ee.Code, ee.Message
		detail = ee.Detail
	}
	if code == "CANCELLED" {
		w.markCancelled(t.ID, from)
		return
	}
	if curErr == nil && (cur.Status == "cancelled" || cur.Status == "failed" || cur.Status == "done") {
		return // 已被其他路径终态化
	}
	// 引擎原始报错写进事件 detail：任务详情里能看到「引擎到底报了什么」，
	// 而不只是「引擎退出码 1」。detail 独立字段，不污染 error_message 的展示文案。
	// 自动重试与最终失败两条路都要写：重试期间「引擎原文」不该缺位。
	if detail != "" {
		_ = w.mgr.st.AppendEvent(t.ID, "error", "engine_error", detail, "")
	}
	// 重试判定（docs/05 §7）：可重试错误码且 attempt<max → 指数退避回 waiting。
	// attempt 必须在本分支递增，否则 t.Attempt 恒为 0、条件恒真 → 30s 间隔无限重试，
	// 文案恒为「第 2/3 次」（实测踩过：bad.news 媒体流超时任务永远重试不到终态）。
	if IsRetryable(code) && t.Attempt+1 < t.MaxAttempts {
		attempt := t.Attempt + 1
		backoff := backoffFor(t.Attempt)
		_ = w.mgr.st.UpdateTaskFields(t.ID, map[string]any{
			"status":        "waiting",
			"wait_reason":   "auto_retry",
			"attempt":       attempt,
			"scheduled_at":  time.Now().Add(backoff).UnixMilli(),
			"error_code":    code,
			"error_message": msg,
			"speed_bps":     0,
		}, from)
		_ = w.mgr.st.AppendEvent(t.ID, "warn", "retry_scheduled",
			fmt.Sprintf("%s：%s；%s 后自动重试（第 %d/%d 次）", code, msg, backoff, attempt+1, t.MaxAttempts), "")
		// 广播带错误字段：前端在重试等待期就能显示「错误与建议」，
		// 不必等 failed 终态才全量刷新。
		w.mgr.hub.Broadcast("task.status", map[string]any{
			"task_id": t.ID, "status": "waiting", "wait_reason": "auto_retry",
			"attempt": attempt, "max_attempts": t.MaxAttempts,
			"error_code": code, "error_message": msg,
			"scheduled_at": time.Now().Add(backoff).UnixMilli(),
		})
		return
	}
	w.fail(t.ID, code, msg, fixHintFor(code))
}

// closeLiveRecording 直播录制收尾（「下播即成品」闭环）：live 来源的任务终态时，
// 把产物大小回写录制记录并复位直播源状态。普通下载任务没有录制记录，直接跳过。
// 只收尾 status=recording 的记录：失败后被手动重试再成功的任务不会重复改写历史。
func (w *Worker) closeLiveRecording(t *store.Task, status, note string, segments int, size int64) {
	if t.Source != SourceLive || t.SourceRef == "" {
		return
	}
	rec, err := w.mgr.st.GetLiveRecordingByTask(t.ID)
	if err != nil || rec == nil || rec.Status != "recording" {
		return
	}
	_ = w.mgr.st.FinishLiveRecording(rec.ID, status, note, segments, size)
	_ = w.mgr.st.MarkLiveSourceStatus(t.SourceRef, "offline", "", false)
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
	w.mgr.notify("task_failed", "error", "任务失败："+code, msg, taskID)
	if t, err := w.mgr.st.GetTask(taskID); err == nil {
		w.closeLiveRecording(t, "failed", msg, 0, 0)
		w.mgr.aggregateParent(t.ParentID)
	}
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
	if t, err := w.mgr.st.GetTask(taskID); err == nil {
		w.closeLiveRecording(t, "cancelled", "任务已取消", 0, 0)
	}
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

// prepareCookieFile 为任务准备临时 cookie 文件；无凭据/解密失败时返回空串（不阻断）。
func (w *Worker) prepareCookieFile(t *store.Task) string {
	resolver := w.mgr.Credentials()
	if resolver == nil {
		return ""
	}
	platform := t.Platform
	if platform == "" {
		platform = "web"
	}
	plain, err := resolver.PlatformCredential(context.Background(), platform)
	if err != nil || plain == "" {
		return ""
	}
	writer := w.mgr.cookieWriter()
	path, err := writer.writeCookieFile(platform, plain)
	if err != nil {
		w.mgr.log.Warn("临时 cookie 文件写入失败", map[string]any{"task_id": t.ID, "err": err.Error()})
		return ""
	}
	return path
}

// cleanupCookieFile 清理临时 cookie 文件（凭据明文不留在磁盘）。
func (w *Worker) cleanupCookieFile(path string) {
	if path == "" {
		return
	}
	w.mgr.cookieWriter().removeCookieFile(path)
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
	case "BOT_CHECK":
		return `{"action":"retry","label":"平台对本服务器 IP 的人机验证（非账号问题），可直接重试；频繁出现可在「连接」页配该平台 Cookie 或在「设置」页配代理"}`
	case "FORMAT_UNAVAILABLE":
		return fixHintChangeQuality()
	case "NOT_FOUND":
		return `{"action":"none","label":"链接不存在或已删除，请检查后重试"}`
	case "NET_TIMEOUT", "HTTP_5XX", "RATE_LIMITED":
		return fixHintRetry()
	case "TIMEOUT":
		return `{"action":"none","label":"解析超时，可稍后重试；若链接是大型合集/频道，可先缩小范围"}`
	case "ENGINE_UNAVAILABLE":
		return fixHintGotoSettings()
	case "ENGINE_OUTDATED":
		return `{"action":"none","label":"下载引擎版本过旧，需要升级 yt-dlp 后才能解析该平台"}`
	case "IMPERSONATION_UNAVAILABLE":
		return `{"action":"none","label":"该站点要求浏览器指纹伪装，当前运行环境缺少 curl_cffi 支持"}`
	case "BLOCKED_BY_FINGERPRINT":
		return `{"action":"none","label":"站点按请求指纹拒绝了访问（HTTP 410），需要启用浏览器指纹伪装"}`
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
