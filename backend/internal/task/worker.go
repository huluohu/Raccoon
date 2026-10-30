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
	// usedCredential 本次任务是否真的带上了凭据。
	// 决定 AUTH_REQUIRED 时能否把平台凭据降级为「待确认」：
	// 没带凭据就报 AUTH_REQUIRED 说明内容本身要登录，与用户存的凭据无关。
	usedCredential bool
	// lastItemCount 引擎报告的条目总数（0=单条目 / >1=合集）。
	// 完成态据此决定要不要用真实大小校正 expected_size（合集总大小不可知，禁估算）。
	lastItemCount int
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
	w.usedCredential = cookieFile != ""
	defer w.cleanupCookieFile(cookieFile)

	// ---- parsing ----
	// 出口决策与预览共用同一套（task/resolve.go）：任务级 proxy 优先，其次全局
	// net.proxy。解析与下载必须走同一个出口——解析走代理成功、下载走直连超时，
	// 是比「解析失败」更隐蔽的错位。
	primary, fallback := resolveAttempts("", t.Proxy, w.mgr.cfg.GetString("net.proxy"))
	res, usedProxy, switchNote, err := w.mgr.resolveWithProxy(ctx, w.eng, t.URL, cookieFile, primary, fallback, 0)
	if switchNote != "" {
		fields := map[string]any{
			"route_path": AppendRoutePath(t.RoutePath, switchNote),
		}
		// 只有「确实用了某个出口」才回写 proxy。回退直连时 usedProxy 是空串，
		// 写回去等于把用户钉住的代理抹掉——之后所有重试都只能直连，
		// 用户专门指定的出口形同虚设（2026-10-28 定位）。
		if usedProxy != "" {
			fields["proxy"] = usedProxy
		}
		_ = w.mgr.st.UpdateTaskFields(t.ID, fields, "parsing")
		_ = w.mgr.st.AppendEvent(t.ID, "info", "proxy_switched", switchNote, "")
	}
	if err != nil {
		w.handleEngineErr(ctx, t, "parsing", err)
		return
	}
	// 下载用解析实际成功的那条出口。
	proxy := usedProxy
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
	// 记录本次将要下载的**媒体直链与域名**（2026-10-28）。
	//
	// 为什么存：解析打的是站点 API，下载打的是媒体 CDN——两者往往不是同一个域名
	//（YouTube: www.youtube.com vs googlevideo.com）。代理规则常常只覆盖了解析域名，
	// 于是「解析成功、下载超时」。把媒体域名存下来，失败时才能点名说清是哪一段路断了。
	mediaURL, mediaHost := mediaTargetsFor(t.Quality, res.Formats)
	if mediaURL != "" {
		_ = w.mgr.st.UpdateTaskFields(t.ID, map[string]any{
			"media_url":  mediaURL,
			"media_host": mediaHost,
		}, "parsing")
		// 同步内存副本：下载阶段失败时的提示要用它点名媒体域名。
		t.MediaURL, t.MediaHost = mediaURL, mediaHost
		_ = w.mgr.st.AppendEvent(t.ID, "info", "media_host", "媒体直链域名："+mediaHost, "")
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
	// 播放列表（合集/频道）进度聚合：yt-dlp 逐条目下载，单个条目的百分比/字节
	// 会在每个视频开始时清零——直接透传会让进度条反复回跳、已下载量永远是
	// 「当前那条视频」的量。worker 在这里折算成全局口径：
	//   downloaded_size = 已完成条目累计字节 + 当前条目已下载
	//   progress        = ((itemIndex-1) + 当前条目百分比/100) / itemTotal * 100
	accBytes, lastItem := int64(0), 0
	var lastFileTotal int64
	// 多流（DASH）下载的体积口径（docs/05 §4）：
	//
	// yt-dlp 对 **视频流**（分片下载）报 progress.total_bytes = NA，
	// 对 **音频流**（单文件 range 请求）报的是**该音频流自己**的大小。
	// 原实现「取第一帧有 total 的进度行」当 expected_size，于是 DASH 任务的
	// 预期大小变成了音频流大小（实测 13.27 MiB），而真实文件是
	// 视频+音频合并后的 210.58 MiB——进度条显示「210.6 MB / 13.3 MB」。
	//
	// 现在按「流」去重累加：每个不同的 total 值视为一个流，累加得总体积。
	// 下载完成后还会用真实文件大小校正（见 finish），保证完成态精确。
	streamTotals := map[int64]bool{}
	// 跨流累计的已完成字节（切流时把上一流的 downloaded 结转）。
	var streamDone int64
	var lastStreamTotal int64
	var lastStreamDownloaded int64
	onProgress := func(p engine.Progress) {
		w.lastItemCount = p.ItemCount
		if p.ItemIndex > lastItem {
			// 进入下一条目：上一条的字节总量计入累计（含跳过/秒完的小视频）。
			if lastItem > 0 {
				accBytes += lastFileTotal
			}
			lastItem = p.ItemIndex
		}
		// 切流检测：total 变了说明换了另一个流（同一流内 total 恒定）。
		// 切流时把上一流的已下载量结转到 streamDone，downloaded 才是全量。
		if p.Total > 0 && lastStreamTotal > 0 && p.Total != lastStreamTotal {
			streamDone += lastStreamDownloaded
			lastStreamDownloaded = 0
		}
		if p.Total > 0 {
			lastStreamTotal = p.Total
			streamTotals[p.Total] = true
		}
		lastStreamDownloaded = p.Downloaded
		lastFileTotal = p.Total
		downloaded := accBytes + streamDone + p.Downloaded
		pct := p.Percent
		if p.ItemCount > 1 {
			// 当前条目百分比未知时按 0 计入（条目计数是引擎真实输出，非估算；
			// 直接透传 -1 会算出负的全局百分比）。
			cur := p.Percent
			if cur < 0 {
				cur = 0
			}
			pct = (float64(p.ItemIndex-1) + cur/100) / float64(p.ItemCount) * 100
		}
		if time.Since(lastWrite) < time.Second {
			return // 节流 1s（docs/05 §4）
		}
		lastWrite = time.Now()
		if pct < 0 {
			pct = 0 // DB 列 NOT NULL DEFAULT 0；未知进度前端按 -1 口径展示 expected_size=NULL
		}
		fields := map[string]any{
			"downloaded_size": downloaded,
			"speed_bps":       p.SpeedBps,
			"progress":        pct,
		}
		// 单条目任务：按流累加出总体积 → 回填 expected_size，
		// 前端进度条从「不确定条纹」切换为真实百分比（修「进度像满了」）。
		// 播放列表的总大小不可知（禁估算），expected_size 保持 NULL。
		if p.ItemCount <= 1 {
			if sum := sumStreamTotals(streamTotals); sum > 0 {
				fields["expected_size"] = sum
			}
		}
		_ = w.mgr.st.UpdateTaskFields(t.ID, fields, "")
		w.mgr.hub.Broadcast("task.progress", map[string]any{
			"task_id": t.ID, "progress": pct, "speed_bps": p.SpeedBps,
			"eta_sec": p.ETASec, "downloaded_size": downloaded,
			"expected_size":  expectedSizeFor(streamTotals, p.ItemCount),
			"progress_known": pct >= 0,
			"item_index":     p.ItemIndex, "item_count": p.ItemCount,
		})
	}
	// before 快照：下载开始前目录状态（失败时清孤儿文件用）。
	// 先确保目录存在再快照，否则快照读不到（返回空表）会把目录里原有的
	// .part 文件误判为「本次新增」而删掉。
	_ = os.MkdirAll(saveDir, 0o755)
	before := engine.DirSnapshot(saveDir)
	files, err := w.eng.Download(ctx, t.URL, opts, onProgress)
	if err != nil {
		// 下载被媒体源拒绝（403）→ 走**直取旁路**：用解析阶段拿到的媒体直链、
		// 用 ffmpeg 拉流，Referer 指向媒体源自己而不是来源页。
		//
		// 为什么需要：通用提取器会把页面自己的请求头（含 Referer）抄到媒体
		// 请求上，媒体 CDN 的防盗链策略据此拒绝 → 403。浏览器侧的下载插件是在
		// 页面上下文里拦截请求、天然带对上下文；服务端只能自己定
		// （详见 engine/media_fetch.go）。
		if alt, ok := w.fetchMediaDirect(ctx, t, res, saveDir, err); ok {
			files = alt
			err = nil
			_ = w.mgr.st.AppendEvent(t.ID, "info", "media_direct",
				"媒体源拒绝了常规下载请求，已改用直链方式重新获取", "")
		}
	}
	if err != nil {
		// 下载失败：清理 yt-dlp 留下的 .part / .ytdl 孤儿文件。
		// 只清本次下载开始后新增的临时文件，不动之前已存在的文件。
		if n := engine.CleanOrphanFiles(saveDir, before); n > 0 {
			_ = w.mgr.st.AppendEvent(t.ID, "info", "orphan_cleaned",
				fmt.Sprintf("已清理 %d 个下载中断产生的临时文件", n), "")
		}
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
	// ffprobe 产物校验（warn 级别，不影响 done 终态）。
	// ffprobe 不可用时自动跳过（降级而非失败）。
	w.probeFiles(t.ID, files)
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
		// 主文件：第一个非字幕/元数据/图片产物。
		// 图片要排除：yt-dlp --write-thumbnail 落的封面 jpg 也在产物里，
		// 拿它当主文件会让媒体库把一张封面当成下载的视频（2026-10-28 定位）。
		if firstPath == "" && kind != "subtitle" && kind != "metadata" && kind != "image" {
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
			// 封面：平台缩略图优先，没有就用 ffmpeg 抽首帧（2026-10-28）。
			// 此前封面只能靠用户手动「制作封面」，媒体库大面积无封面。
			CoverPath: w.pickCover(files, firstPath),
		})
	}
	_ = w.mgr.st.UpdateTaskFields(t.ID, map[string]any{
		"downloaded_size": total,
		"progress":        100,
	}, "post")
	// 完成态用**真实文件大小**校正 expected_size：下载中按流累加的总体积
	// 与实际文件总有容器封装开销的微小差异（如 DASH 合并的 mp4/webm 头部），
	// 完成时以实际大小为准，任务详情显示「X / X」而不是差一点的数。
	//
	// 只对单条目任务做：合集（多条目 URL）的总大小不可知，禁估算（docs/05 §4），
	// expected_size 保持 NULL。
	if w.lastItemCount <= 1 {
		_ = w.mgr.st.UpdateTaskFields(t.ID, map[string]any{"expected_size": total}, "post")
	}
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
	// 凭据失效被动检测：AUTH_REQUIRED 说明当前凭据很可能已不可用，
	// 把该平台的凭据降级为 expiring（待确认）——平台账号页立刻显示
	// 「已失效或待确认」，用户不必逐个手动点「检查」才发现 Cookie 过期。
	//
	// 只降级不判死：AUTH_REQUIRED 也可能是内容本身要大会员/私有视频，
	// 而凭据本身好着。真正的 expired 只由平台 API 探针确认
	// （server 层 probeCredentialPlatform）。
	//
	// 条件：本次任务确实带了该平台的凭据。无凭据时报 AUTH_REQUIRED
	// 与用户存的凭据无关，误标会让用户白跑一趟。
	if code == "AUTH_REQUIRED" && w.usedCredential {
		w.markCredentialExpiring(t)
	}
	// 重试判定（docs/05 §7）：可重试错误码且 attempt<max → 指数退避回 waiting。
	// attempt 必须在本分支递增，否则 t.Attempt 恒为 0、条件恒真 → 30s 间隔无限重试，
	// 文案恒为「第 2/3 次」（实测踩过：bad.news 媒体流超时任务永远重试不到终态）。
	if IsRetryable(code) && t.Attempt+1 < t.MaxAttempts {
		attempt := t.Attempt + 1
		backoff := backoffFor(t.Attempt)
		fields := map[string]any{
			"status":        "waiting",
			"wait_reason":   "auto_retry",
			"attempt":       attempt,
			"scheduled_at":  time.Now().Add(backoff).UnixMilli(),
			"error_code":    code,
			"error_message": msg,
			// 重试期就把修复建议写上：网络类失败要点名媒体域名，
			// 等到重试耗尽才显示，用户已经白等了几轮退避（30s→2m→10m）。
			"fix_hint":  fixHintForTask(code, t.MediaHost),
			"speed_bps": 0,
		}
		msgEvent := fmt.Sprintf("%s：%s；%s 后自动重试（第 %d/%d 次）", code, msg, backoff, attempt+1, t.MaxAttempts)
		// 自动切换代理：若本次下载走的出口失败（网络类异常），而系统配置了
		// net.proxy 且本次**没在用**代理，下次重试自动切到代理通道。
		// 已经在用代理时不再切（切了还是同一个出口，等于白重试一次）。
		globalProxy := strings.TrimSpace(w.mgr.cfg.GetString("net.proxy"))
		usingProxy := strings.TrimSpace(t.Proxy) != ""
		if !usingProxy && globalProxy != "" && NetworkErrorCodes[code] {
			fields["proxy"] = globalProxy
			fields["route_path"] = AppendRoutePath(t.RoutePath, "直连异常 → 自动切换代理重试")
			msgEvent = fmt.Sprintf("%s：%s；已自动切换代理，%s 后重试（第 %d/%d 次）", code, msg, backoff, attempt+1, t.MaxAttempts)
		}
		_ = w.mgr.st.UpdateTaskFields(t.ID, fields, from)
		_ = w.mgr.st.AppendEvent(t.ID, "warn", "retry_scheduled", msgEvent, "")
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
	w.fail(t.ID, code, msg, fixHintForTask(code, t.MediaHost))
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
// download_dir 支持绝对路径（如容器内 /downloads，独立于数据根——数据库/凭据
// 备份不必背着视频文件）；相对路径保持「相对数据根」的存量语义。
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
	if filepath.IsAbs(base) {
		return filepath.Join(base, p)
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

// fixHintForTask 错误码 → 修复建议；网络类错误点名媒体域名。
//
// 为什么点名域名（2026-10-28）：解析打的是站点 API，下载打的是媒体 CDN，
// 两者的可达性可以完全不同——代理规则常常只覆盖了解析域名（youtube.com），
// 媒体域名（googlevideo.com）却直连被墙。只说「网络连接失败」用户无从下手；
// 说出「媒体直链来自 googlevideo.com」才知道该查哪一段路。
func fixHintForTask(code, mediaHost string) string {
	// 403：媒体源拒绝了请求——不是网络不通，换代理有时有用（区域限制），
	// 但更常见的是媒体 CDN 不接受当前请求（来源页 Referer、指纹不符）。
	if code == "HTTP_FORBIDDEN" {
		if mediaHost != "" {
			return fmt.Sprintf(
				`{"action":"retry_proxy","label":"媒体源拒绝了请求（HTTP 403，来自 %s）——不是网络不通：常见原因是媒体 CDN 不接受当前请求（来源页 Referer、指纹或区域限制），可换出口重试"}`,
				mediaHost)
		}
		return `{"action":"retry_proxy","label":"媒体源拒绝了请求（HTTP 403）——不是网络不通：常见原因是媒体 CDN 不接受当前请求（来源页 Referer、指纹或区域限制），可换出口重试"}`
	}
	if mediaHost != "" {
		switch code {
		case "NET_TIMEOUT":
			return fmt.Sprintf(
				`{"action":"retry_proxy","label":"网络连接失败：本次媒体直链来自 %s（解析域名 ≠ 媒体域名）——请确认代理规则覆盖该域名，或点击「切换代理重试」"}`,
				mediaHost)
		case "HTTP_5XX":
			return fmt.Sprintf(
				`{"action":"retry_proxy","label":"平台或媒体源返回服务端错误（5xx）：本次媒体直链来自 %s，可稍后重试或点击「切换代理重试」"}`,
				mediaHost)
		case "RATE_LIMITED":
			return fmt.Sprintf(
				`{"action":"retry_proxy","label":"被限流：本次媒体直链来自 %s，可稍后重试或换出口"}`,
				mediaHost)
		}
	}
	return fixHintFor(code)
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
		return `{"action":"retry_proxy","label":"网络连接失败，可点击「切换代理重试」或在「设置」页配置代理"}`
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

// probeFiles ffprobe 校验产物完整性（warn 级别，不影响 done 终态）。
// ffprobe 不可用或文件非媒体类型时自动跳过（降级而非失败）。
func (w *Worker) probeFiles(taskID string, files []engine.DownloadedFile) {
	ffprobe := w.mgr.Engines().FFprobeBin
	if ffprobe == "" {
		return // 无 ffprobe 工具，跳过
	}
	for _, f := range files {
		res := engine.ProbeFile(context.Background(), ffprobe, f.Path)
		if res.Warn != "" {
			_ = w.mgr.st.AppendEvent(taskID, "warn", "probe_warn",
				fmt.Sprintf("产物校验警告 %s：%s", filepath.Base(f.Path), res.Warn), "")
		}
	}
}

// markCredentialExpiring 把该平台的凭据降级为「待确认」（expiring）。
//
// 触发点：任务因 AUTH_REQUIRED 失败且本次确实带了该平台的凭据。
// 语义边界：降级而非判死——AUTH_REQUIRED 也可能是内容要大会员/私有视频，
// 凭据本身好着。真正的 expired 由平台 API 探针确认（server 层）。
//
// 平台标识用任务的 platform（解析后已用 extractor 覆盖，比 URL 推断准）。
func (w *Worker) markCredentialExpiring(t *store.Task) {
	platform := t.Platform
	if platform == "" {
		return
	}
	n, err := w.mgr.st.MarkPlatformCredentialStatus(platform, "expiring")
	if err != nil {
		w.mgr.log.Warn("凭据状态降级失败", map[string]any{
			"task_id": t.ID, "platform": platform, "err": err.Error(),
		})
		return
	}
	if n == 0 {
		return // 该平台没有凭据（或已是 expiring）——不制造噪音事件
	}
	_ = w.mgr.st.AppendEvent(t.ID, "warn", "credential_expiring",
		fmt.Sprintf("平台 %s 的凭据已降级为「待确认」：下载返回鉴权失败，请到「平台账号」页检查", platform), "")
	w.mgr.notify("credential_expiring", "warn",
		"平台凭据可能已失效："+platform, "下载返回鉴权失败，请到「平台账号」页检查或更新 Cookie", t.ID)
}

// mediaTargetsFor 按任务清晰度挑出「将被下载」的那条媒体直链，并汇总它的域名。
//
// 口径与引擎的 formatSelector 对齐（engine/ytdlp.go formatSelector）：
//   - audio → 只取音频档；
//   - best/空 → 最高分辨率的视频档（DASH 站点会是「视频流 + 音频流」两条直链，
//     域名可能不同，两个都记下来）；
//   - 720p/1080p… → 该清晰度档，没有就退一档（宁可降档也别报「无此格式」）。
//
// 返回的直链**只用于排查与展示**：它带签名、会过期，不做断点续传复用。
func mediaTargetsFor(quality string, formats []engine.FormatInfo) (url string, hosts string) {
	if len(formats) == 0 {
		return "", ""
	}
	pick := func(pred func(engine.FormatInfo) bool) (engine.FormatInfo, bool) {
		for _, f := range formats {
			if pred(f) {
				return f, true
			}
		}
		return engine.FormatInfo{}, false
	}
	isAudio := func(f engine.FormatInfo) bool { return f.Quality == "audio" }
	heightOf := func(f engine.FormatInfo) int {
		n := 0
		for _, c := range f.Quality {
			if c >= '0' && c <= '9' {
				n = n*10 + int(c-'0')
			}
		}
		return n
	}
	isVideo := func(f engine.FormatInfo) bool { return !isAudio(f) && heightOf(f) > 0 }

	var chosen []engine.FormatInfo
	switch quality {
	case "audio":
		if f, ok := pick(isAudio); ok {
			chosen = append(chosen, f)
		}
	default:
		// 档位上限：与引擎的 formatSelector 同一套解析（engine.ParseHeightQuality），
		// 不能在这里再抄一份硬编码表——抄一份就会漏（漏了 4K/2K/8K 时，
		// limit 退化为 0 = 不设限，任务选了 1440p 却把媒体域名记成 2160p 那档，
		// 失败提示会点名错的域名）。
		limit := 0
		if h, ok := engine.ParseHeightQuality(quality); ok {
			limit = h
		}
		// 视频档：取不超过上限的最高档；best/空 → 最高档
		best := engine.FormatInfo{}
		have := false
		for _, f := range formats {
			if !isVideo(f) {
				continue
			}
			h := heightOf(f)
			if limit > 0 && h > limit {
				continue
			}
			if !have || h > heightOf(best) {
				best = f
				have = true
			}
		}
		if have {
			chosen = append(chosen, best)
		}
		// DASH 站点：视频流之外还要一条音频流
		if a, ok := pick(isAudio); ok {
			chosen = append(chosen, a)
		}
	}
	if len(chosen) == 0 {
		return "", ""
	}
	seen := map[string]bool{}
	hostList := make([]string, 0, 2)
	for _, f := range chosen {
		if f.URL == "" {
			continue
		}
		if url == "" {
			url = f.URL
		}
		if f.Host != "" && !seen[f.Host] {
			seen[f.Host] = true
			hostList = append(hostList, f.Host)
		}
	}
	return url, strings.Join(hostList, ",")
}

// pickCover 给媒体文件挑封面：平台缩略图优先，没有就用 ffmpeg 抽视频首帧。
//
// 两条来源：
//  1. yt-dlp --write-thumbnail 落的图片（平台给的封面，最好看）；
//  2. 没有平台封面时，用 ffmpeg 抽视频首帧（任何视频都有封面）。
//
// 返回封面文件的绝对路径（空串 = 没有封面）。抽帧会顺带去黑边
// （平台封面与视频帧都可能带黑边，见 engine.TrimLetterbox）。
func (w *Worker) pickCover(files []engine.DownloadedFile, mainPath string) string {
	// 1) 产物里的图片（平台封面）
	for _, f := range files {
		switch strings.ToLower(filepath.Ext(f.Path)) {
		case ".jpg", ".jpeg", ".png", ".webp":
			return f.Path
		}
	}
	// 2) 没有平台封面 → ffmpeg 抽首帧
	ffmpeg := w.mgr.Engines().FFmpegBin
	if ffmpeg == "" || mainPath == "" {
		return ""
	}
	base := strings.TrimSuffix(mainPath, filepath.Ext(mainPath)) + ".cover.jpg"
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := engine.ExtractFirstFrame(ctx, ffmpeg, w.mgr.Engines().FFprobeBin, mainPath, base); err != nil {
		w.mgr.log.Warn("抽取封面首帧失败", map[string]any{"path": mainPath, "err": err.Error()})
		return ""
	}
	return base
}

// fetchMediaDirect 直取旁路：媒体源拒绝常规下载（403）时，用解析阶段拿到的
// 媒体直链 + ffmpeg 拉流再试一次（Referer 指向媒体源自己）。
//
// 返回 (产物, true) 表示直取成功；ok=false 表示这条路径不适用或失败，
// 交回调用方按原错误处理（不掩盖原始失败原因）。
func (w *Worker) fetchMediaDirect(ctx context.Context, t *store.Task, res *engine.ResolveResult, saveDir string, dlErr error) ([]engine.DownloadedFile, bool) {
	ffmpeg := w.mgr.Engines().FFmpegBin
	if ffmpeg == "" || res == nil {
		return nil, false
	}
	// 只在「媒体源拒绝」时才值得换路：其它失败（网络不通、格式没了）直取也救不了。
	if engineErrCode(dlErr) != "HTTP_FORBIDDEN" {
		return nil, false
	}
	video, audio := manifestSources(res.Formats)
	if video == "" {
		return nil, false
	}
	base := strings.TrimSuffix(filepath.Base(t.URL), filepath.Ext(t.URL))
	name := fmt.Sprintf("%s [%s].mp4", sanitizeFileName(res.Title, 120), base)
	dst := filepath.Join(saveDir, name)
	_ = w.mgr.st.AppendEvent(t.ID, "info", "media_direct_start",
		"改用直链方式获取媒体（Referer 指向媒体源）", "")

	err := engine.FetchMediaDirect(ctx, ffmpeg, []engine.MediaSource{
		{URL: video},
		{URL: audio, Audio: true},
	}, dst, float64(res.Duration), func(p engine.Progress) {
		if p.Percent < 0 {
			return
		}
		fields := map[string]any{}
		if p.Percent >= 0 {
			fields["progress"] = p.Percent
		}
		if p.Downloaded > 0 {
			// 直取时时长常常未知（百分比算不出），但已写入的字节数是实的——
			// 报出来用户才看得到「在动」。
			fields["downloaded_size"] = p.Downloaded
		}
		if len(fields) > 0 {
			_ = w.mgr.st.UpdateTaskFields(t.ID, fields, "")
		}
	})
	if err != nil {
		_ = w.mgr.st.AppendEvent(t.ID, "warn", "media_direct_failed",
			"直链获取也失败："+err.Error(), "")
		return nil, false
	}
	st, statErr := os.Stat(dst)
	if statErr != nil || st.Size() == 0 {
		return nil, false
	}
	return []engine.DownloadedFile{{Path: dst, Size: st.Size()}}, true
}

// manifestSources 从解析出的格式里挑出「清单型」媒体直链（视频流 + 音轨）。
//
// 只认清单（HLS/DASH）：普通直链文件用 yt-dlp 自己的下载器没问题，
// 不该绕这一圈（绕了反而丢进度与断点续传）。
func manifestSources(formats []engine.FormatInfo) (video, audio string) {
	for _, f := range formats {
		if !engine.IsManifestURL(f.URL) {
			continue
		}
		if f.Quality == "audio" {
			if audio == "" {
				audio = f.URL
			}
			continue
		}
		if video == "" {
			video = f.URL
		}
	}
	return video, audio
}

// sanitizeFileName 文件名清洗（去掉路径分隔符与控制字符，截断到 n 个字符）。
func sanitizeFileName(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', 0:
			return -1
		}
		if r < 32 {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if s == "" {
		s = "media"
	}
	runes := []rune(s)
	if len(runes) > n {
		s = string(runes[:n])
	}
	return s
}

// engineErrCode 取引擎错误的错误码（非引擎错误返回空串）。
func engineErrCode(err error) string {
	var ue *engine.EngineError
	if errors.As(err, &ue) {
		return ue.Code
	}
	return ""
}
