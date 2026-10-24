package task

// worker_retry_test.go：自动重试机制（docs/05 §7）。
//
// 锁死的不变式：attempt 必须随每次自动重试递增——否则 NET_TIMEOUT 任务会以
// 30s 间隔无限重试、文案永远停在「第 2/3 次」、到不了 failed 终态（实测回归：
// bad.news 媒体流超时任务反复重试 30 分钟不自愈）。

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/huluohu/raccoon/internal/engine"
	"github.com/huluohu/raccoon/internal/store"
)

// fakeWorkerEngine 可控假引擎：Resolve 恒成功，Download 按脚本逐次返回错误，脚本耗尽后成功。
type fakeWorkerEngine struct {
	downloadErrs  []error
	files         []engine.DownloadedFile
	downloadCalls int
}

func (f *fakeWorkerEngine) Name() string     { return "fake" }
func (f *fakeWorkerEngine) Available() error { return nil }
func (f *fakeWorkerEngine) Version() string  { return "test" }

func (f *fakeWorkerEngine) Resolve(ctx context.Context, url string, opts engine.ResolveOpts) (*engine.ResolveResult, error) {
	return &engine.ResolveResult{Title: "测试标题", Platform: "test"}, nil
}

func (f *fakeWorkerEngine) Download(ctx context.Context, url string, opts engine.DownloadOpts, onProgress func(engine.Progress)) ([]engine.DownloadedFile, error) {
	f.downloadCalls++
	if f.downloadCalls <= len(f.downloadErrs) {
		return nil, f.downloadErrs[f.downloadCalls-1]
	}
	return f.files, nil
}

// newDownloadTask 建一条本地下载任务并领取到 parsing（Worker.Run 的入口状态）。
func newDownloadTask(t *testing.T, m *Manager) *store.Task {
	t.Helper()
	id := store.NewTaskID()
	if err := m.st.CreateTask(&store.Task{
		ID: id, Kind: KindDownload, Source: "web", Platform: "test",
		URL: "https://example.com/video/1", Title: "测试任务",
		DedupeKey:   store.DedupeKey(KindDownload, "test", id),
		MaxAttempts: 3, CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("建任务失败：%v", err)
	}
	claimed, err := m.st.ClaimWaiting(id, "parsing")
	if err != nil {
		t.Fatalf("领取任务失败：%v", err)
	}
	return claimed
}

func mustTask(t *testing.T, m *Manager, id string) *store.Task {
	t.Helper()
	cur, err := m.st.GetTask(id)
	if err != nil {
		t.Fatalf("读任务失败：%v", err)
	}
	return cur
}

func mustReclaim(t *testing.T, m *Manager, id string) *store.Task {
	t.Helper()
	claimed, err := m.st.ClaimWaiting(id, "parsing")
	if err != nil {
		t.Fatalf("重试领取失败：%v", err)
	}
	return claimed
}

func netTimeoutErr(msg string) error {
	return &engine.EngineError{Code: "NET_TIMEOUT", Message: msg, Detail: "ERROR: unable to download webpage"}
}

// TestWorker_自动重试attempt递增且耗尽后落failed 三次失败必须终止，
// attempt 逐步递增、退避 30s→2m 递增、事件文案计数正确。
func TestWorker_自动重试attempt递增且耗尽后落failed(t *testing.T) {
	m := newExtTestManager(t)
	eng := &fakeWorkerEngine{downloadErrs: []error{
		netTimeoutErr("网络连接失败或被重置"),
		netTimeoutErr("网络连接失败或被重置"),
		netTimeoutErr("网络连接失败或被重置"),
	}}
	w := &Worker{mgr: m, eng: eng}
	task := newDownloadTask(t, m)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 第 1 次失败 → waiting(auto_retry)，attempt=1，退避 30s。
	w.Run(ctx, task)
	cur := mustTask(t, m, task.ID)
	if cur.Status != StatusWaiting || cur.WaitReason != "auto_retry" {
		t.Fatalf("第 1 次失败后期望 waiting/auto_retry，得到 %s/%s", cur.Status, cur.WaitReason)
	}
	if cur.Attempt != 1 {
		t.Fatalf("第 1 次失败后期望 attempt=1，得到 %d", cur.Attempt)
	}
	if cur.ErrorCode != "NET_TIMEOUT" {
		t.Fatalf("重试等待期 error_code 应已落库，得到 %q", cur.ErrorCode)
	}
	if cur.ScheduledAt == nil {
		t.Fatal("重试等待期 scheduled_at 应已落库")
	}
	if d := time.Duration(*cur.ScheduledAt-time.Now().UnixMilli()) * time.Millisecond; d < 20*time.Second || d > 40*time.Second {
		t.Fatalf("第 1 次退避期望约 30s，得到 %v", d)
	}

	// 第 2 次失败 → attempt=2，退避 2m。
	w.Run(ctx, mustReclaim(t, m, task.ID))
	cur = mustTask(t, m, task.ID)
	if cur.Status != StatusWaiting || cur.Attempt != 2 {
		t.Fatalf("第 2 次失败后期望 waiting/attempt=2，得到 %s/%d", cur.Status, cur.Attempt)
	}
	if d := time.Duration(*cur.ScheduledAt-time.Now().UnixMilli()) * time.Millisecond; d < 100*time.Second || d > 140*time.Second {
		t.Fatalf("第 2 次退避期望约 2m，得到 %v", d)
	}

	// 第 3 次失败：重试耗尽 → failed 终态。
	w.Run(ctx, mustReclaim(t, m, task.ID))
	cur = mustTask(t, m, task.ID)
	if cur.Status != StatusFailed {
		t.Fatalf("3 次失败后期望 failed 终态，得到 %s（无限重试回归）", cur.Status)
	}
	if cur.ErrorCode != "NET_TIMEOUT" || cur.FixHint == "" {
		t.Fatalf("终态应带 error_code 与 fix_hint，得到 %q/%q", cur.ErrorCode, cur.FixHint)
	}
	if eng.downloadCalls != 3 {
		t.Fatalf("期望恰好下载 3 次，实际 %d", eng.downloadCalls)
	}
}

// TestWorker_重试计数事件文案 事件里的「第 X/Y 次」必须随 attempt 递进，
// 不允许永远停在「第 2/3 次」。
func TestWorker_重试计数事件文案(t *testing.T) {
	m := newExtTestManager(t)
	eng := &fakeWorkerEngine{downloadErrs: []error{
		netTimeoutErr("网络连接失败或被重置"),
		netTimeoutErr("网络连接失败或被重置"),
	}}
	w := &Worker{mgr: m, eng: eng}
	task := newDownloadTask(t, m)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w.Run(ctx, task)
	w.Run(ctx, mustReclaim(t, m, task.ID))

	events, _, err := m.st.ListEvents(task.ID, 0, 100)
	if err != nil {
		t.Fatalf("读事件失败：%v", err)
	}
	var seen []string
	for _, e := range events {
		if e.Event == "retry_scheduled" {
			seen = append(seen, e.Message)
		}
	}
	if len(seen) != 2 {
		t.Fatalf("期望 2 条 retry_scheduled 事件，得到 %d：%v", len(seen), seen)
	}
	if !strings.Contains(seen[0], "第 2/3 次") {
		t.Fatalf("第 1 条事件应为「第 2/3 次」：%s", seen[0])
	}
	if !strings.Contains(seen[1], "第 3/3 次") {
		t.Fatalf("第 2 条事件应为「第 3/3 次」（回归：恒为第 2/3 次）：%s", seen[1])
	}
}

// TestWorker_失败后成功清错误痕迹 失败重试后成功的任务不得残留 error 三件套。
func TestWorker_失败后成功清错误痕迹(t *testing.T) {
	m := newExtTestManager(t)
	dir := t.TempDir()
	eng := &fakeWorkerEngine{
		downloadErrs: []error{netTimeoutErr("网络连接失败或被重置")},
		files:        []engine.DownloadedFile{{Path: filepath.Join(dir, "video.mp4"), Size: 1024}},
	}
	w := &Worker{mgr: m, eng: eng}
	task := newDownloadTask(t, m)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w.Run(ctx, task)
	if cur := mustTask(t, m, task.ID); cur.Status != StatusWaiting {
		t.Fatalf("前置：第 1 次失败应 waiting，得到 %s", cur.Status)
	}
	w.Run(ctx, mustReclaim(t, m, task.ID))

	cur := mustTask(t, m, task.ID)
	if cur.Status != StatusDone {
		t.Fatalf("第 2 次应成功 done，得到 %s（错误：%s）", cur.Status, cur.ErrorMessage)
	}
	if cur.ErrorCode != "" || cur.ErrorMessage != "" || cur.FixHint != "" {
		t.Fatalf("成功后应清空错误痕迹，残留 %q/%q/%q", cur.ErrorCode, cur.ErrorMessage, cur.FixHint)
	}
}

// TestWorker_不可重试错误直接failed 平台类错误（如 AUTH_REQUIRED）不进重试循环。
func TestWorker_不可重试错误直接failed(t *testing.T) {
	m := newExtTestManager(t)
	eng := &fakeWorkerEngine{downloadErrs: []error{
		&engine.EngineError{Code: "AUTH_REQUIRED", Message: "需要登录"},
	}}
	w := &Worker{mgr: m, eng: eng}
	task := newDownloadTask(t, m)

	w.Run(context.Background(), task)
	cur := mustTask(t, m, task.ID)
	if cur.Status != StatusFailed {
		t.Fatalf("AUTH_REQUIRED 应直接 failed，得到 %s", cur.Status)
	}
	if cur.Attempt != 0 {
		t.Fatalf("不可重试错误不应递增 attempt，得到 %d", cur.Attempt)
	}
	if cur.ErrorCode != "AUTH_REQUIRED" {
		t.Fatalf("error_code 应为 AUTH_REQUIRED，得到 %q", cur.ErrorCode)
	}
}

// TestRetry_退避等待期立即重试 waiting+auto_retry 可跳过剩余退避：
// scheduled_at 提前到立即到期，attempt 不变（次数在下次失败时由 worker 递增）。
func TestRetry_退避等待期立即重试(t *testing.T) {
	m := newExtTestManager(t)
	eng := &fakeWorkerEngine{downloadErrs: []error{
		netTimeoutErr("网络连接失败或被重置"),
	}}
	w := &Worker{mgr: m, eng: eng}
	task := newDownloadTask(t, m)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w.Run(ctx, task)
	cur := mustTask(t, m, task.ID)
	if cur.Status != StatusWaiting || cur.ScheduledAt == nil {
		t.Fatalf("前置：应处于 auto_retry 等待期，得到 %s", cur.Status)
	}
	original := *cur.ScheduledAt

	if err := m.Retry(task.ID); err != nil {
		t.Fatalf("退避等待期应可立即重试：%v", err)
	}
	cur = mustTask(t, m, task.ID)
	if cur.Status != StatusWaiting || cur.Attempt != 1 {
		t.Fatalf("立即重试不应改状态或消耗 attempt，得到 %s/%d", cur.Status, cur.Attempt)
	}
	if cur.ScheduledAt == nil || *cur.ScheduledAt > original {
		t.Fatalf("scheduled_at 应提前到立即到期，得到 %v（原 %v）", cur.ScheduledAt, original)
	}
	if cur.ErrorCode != "NET_TIMEOUT" {
		t.Fatalf("跳过退避不清错误：错误在下次失败时由 worker 覆写，得到 %q", cur.ErrorCode)
	}
}

// TestRetry_运行中拒绝 运行态任务不允许手动重试（docs/05 §7）。
func TestRetry_运行中拒绝(t *testing.T) {
	m := newExtTestManager(t)
	task := newDownloadTask(t, m) // parsing 态
	if err := m.Retry(task.ID); err == nil {
		t.Fatal("运行中任务不应可重试")
	}
}

// ---- 手动换通道（RetryVia，docs/05 §7）：本地下载失败 → 转 115 离线 ----

// TestRetryVia_失败任务转115 kind 改写、route_path 追补、错误清空、回 waiting。
func TestRetryVia_失败任务转115(t *testing.T) {
	m := newExtTestManager(t)
	m.SetExternalExecutor(NewExternalExecutor(m, nil, &fakeN115{}))
	eng := &fakeWorkerEngine{downloadErrs: []error{
		&engine.EngineError{Code: "AUTH_REQUIRED", Message: "需要登录"}, // 不可重试，一次即失败
	}}
	w := &Worker{mgr: m, eng: eng}
	task := newDownloadTask(t, m)
	w.Run(context.Background(), task)
	if cur := mustTask(t, m, task.ID); cur.Status != StatusFailed {
		t.Fatalf("前置：任务应 failed，得到 %s", cur.Status)
	}

	if err := m.RetryVia(task.ID, KindN115); err != nil {
		t.Fatalf("换道重试失败：%v", err)
	}
	cur := mustTask(t, m, task.ID)
	if cur.Kind != KindN115 {
		t.Fatalf("kind 应已改为 n115，得到 %s", cur.Kind)
	}
	if cur.Status != StatusWaiting || cur.WaitReason != "" {
		t.Fatalf("换道后应回 waiting 排队，得到 %s/%q", cur.Status, cur.WaitReason)
	}
	if cur.ErrorCode != "" || cur.FixHint != "" {
		t.Fatalf("换道应清错误三件套，残留 %q/%q", cur.ErrorCode, cur.FixHint)
	}
	if !containsSub(cur.RoutePath, "本地下载失败 → 转 115") {
		t.Fatalf("route_path 应追补换道痕迹，得到 %q", cur.RoutePath)
	}
}

// TestRetryVia_115未配置如实拒绝 未配置 115 时拒绝换道，不假装受理。
func TestRetryVia_115未配置如实拒绝(t *testing.T) {
	m := newExtTestManager(t)
	m.SetExternalExecutor(NewExternalExecutor(m, nil, nil)) // n115 客户端 nil = 未配置
	eng := &fakeWorkerEngine{downloadErrs: []error{
		&engine.EngineError{Code: "AUTH_REQUIRED", Message: "需要登录"},
	}}
	w := &Worker{mgr: m, eng: eng}
	task := newDownloadTask(t, m)
	w.Run(context.Background(), task)

	err := m.RetryVia(task.ID, KindN115)
	var notConfigured *ErrChannelNotConfigured
	if !errors.As(err, &notConfigured) {
		t.Fatalf("应返回 ErrChannelNotConfigured，得到 %v", err)
	}
	if cur := mustTask(t, m, task.ID); cur.Kind != KindDownload {
		t.Fatalf("拒绝后 kind 不得被改写，得到 %s", cur.Kind)
	}
}

// TestRetryVia_门槛与非法目标 运行中不可换道；不支持的目标通道拒绝；已是该通道拒绝。
func TestRetryVia_门槛与非法目标(t *testing.T) {
	m := newExtTestManager(t)
	m.SetExternalExecutor(NewExternalExecutor(m, nil, &fakeN115{}))
	running := newDownloadTask(t, m) // parsing
	if err := m.RetryVia(running.ID, KindN115); err == nil {
		t.Fatal("运行中任务不应可换道")
	}

	// 已是 n115 的任务不能再换到 n115。
	id := store.NewTaskID()
	if err := m.st.CreateTask(&store.Task{
		ID: id, Kind: KindN115, Source: "web", Platform: "qb",
		URL: "magnet:?xt=1", Title: "磁力任务",
		DedupeKey: store.DedupeKey(KindN115, "qb", id), MaxAttempts: 3,
		CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("建任务失败：%v", err)
	}
	if err := m.RetryVia(id, KindN115); err == nil {
		t.Fatal("已在该通道上的任务不应可换道")
	}
	if err := m.RetryVia(id, KindQB); err == nil {
		t.Fatal("不在白名单里的目标通道应被拒绝")
	}
}

func containsSub(s, sub string) bool {
	return strings.Contains(s, sub)
}

// ---- 直播录制收尾（下播即成品）：live 来源任务终态回写录制记录 ----

// newLiveTask 建一条 live 来源的任务并领取到 parsing，同时建好录制记录。
func newLiveTask(t *testing.T, m *Manager) *store.Task {
	t.Helper()
	ls := &store.LiveSource{
		Platform: "bilibili", RoomID: "23058", RoomURL: "https://live.bilibili.com/23058",
		RecordPlan: "manual", CheckIntervalSec: 60, Enabled: true,
	}
	if err := m.st.CreateLiveSource(ls); err != nil {
		t.Fatalf("建直播源失败：%v", err)
	}
	id := store.NewTaskID()
	if err := m.st.CreateTask(&store.Task{
		ID: id, Kind: KindDownload, Source: SourceLive, SourceRef: ls.ID, Platform: "bilibili",
		URL: ls.RoomURL, Title: "直播录制",
		DedupeKey:   store.DedupeKey(KindDownload, "bilibili", id),
		MaxAttempts: 3, CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("建任务失败：%v", err)
	}
	if _, err := m.st.CreateLiveRecording(ls.ID, id); err != nil {
		t.Fatalf("建录制记录失败：%v", err)
	}
	_ = m.st.MarkLiveSourceStatus(ls.ID, "recording", "", true)
	claimed, err := m.st.ClaimWaiting(id, "parsing")
	if err != nil {
		t.Fatalf("领取任务失败：%v", err)
	}
	return claimed
}

func TestWorker_直播任务完成回写录制记录(t *testing.T) {
	m := newExtTestManager(t)
	dir := t.TempDir()
	eng := &fakeWorkerEngine{
		files: []engine.DownloadedFile{
			{Path: filepath.Join(dir, "live.flv"), Size: 4096},
			{Path: filepath.Join(dir, "live.meta"), Size: 16},
		},
	}
	w := &Worker{mgr: m, eng: eng}
	task := newLiveTask(t, m)

	w.Run(context.Background(), task)

	cur := mustTask(t, m, task.ID)
	if cur.Status != StatusDone {
		t.Fatalf("前置：任务应 done，得到 %s（%s）", cur.Status, cur.ErrorMessage)
	}
	rec, err := m.st.GetLiveRecordingByTask(task.ID)
	if err != nil || rec == nil {
		t.Fatalf("应能按任务找到录制记录（err=%v）", err)
	}
	if rec.Status != "completed" {
		t.Fatalf("录制记录应 completed，得到 %s", rec.Status)
	}
	if rec.Size != 4096+16 || rec.Segments != 2 {
		t.Fatalf("产物大小/文件数应回写，得到 size=%d segments=%d", rec.Size, rec.Segments)
	}
	if rec.EndedAt == nil {
		t.Fatal("ended_at 应回写")
	}
	ls, err := m.st.GetLiveSource(task.SourceRef)
	if err != nil {
		t.Fatal(err)
	}
	if ls.Status != "offline" {
		t.Fatalf("下播后源应复位 offline，得到 %s", ls.Status)
	}
}

func TestWorker_直播任务失败也收尾录制(t *testing.T) {
	m := newExtTestManager(t)
	eng := &fakeWorkerEngine{downloadErrs: []error{
		&engine.EngineError{Code: "AUTH_REQUIRED", Message: "需要登录"},
	}}
	w := &Worker{mgr: m, eng: eng}
	task := newLiveTask(t, m)

	w.Run(context.Background(), task)

	rec, err := m.st.GetLiveRecordingByTask(task.ID)
	if err != nil || rec == nil {
		t.Fatalf("应能找到录制记录（err=%v）", err)
	}
	if rec.Status != "failed" || rec.ErrorNote == "" {
		t.Fatalf("录制记录应 failed 且带原因，得到 %s/%q", rec.Status, rec.ErrorNote)
	}
}

func TestWorker_普通下载任务不触发录制回写(t *testing.T) {
	m := newExtTestManager(t)
	dir := t.TempDir()
	eng := &fakeWorkerEngine{files: []engine.DownloadedFile{{Path: filepath.Join(dir, "v.mp4"), Size: 1}}}
	w := &Worker{mgr: m, eng: eng}
	task := newDownloadTask(t, m) // Source=web

	w.Run(context.Background(), task)
	// 无录制记录可写——只要不 panic、不影响 done 即视为通过
	if cur := mustTask(t, m, task.ID); cur.Status != StatusDone {
		t.Fatalf("普通任务应正常 done，得到 %s", cur.Status)
	}
}
