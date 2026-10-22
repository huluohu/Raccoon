package task

// external_test.go：qB/115 外部通道执行器（docs/08 W4.6/W4.7）。
//
// 锁死两条不变式：
//  1. 外部任务与下载任务走同一套七态状态机（任意入口同队列同状态）；
//  2. qB/115 不可用时仅相关任务失败，其他功能不受影响（docs/01 §5.8）。

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/huluohu/raccoon/internal/config"
	"github.com/huluohu/raccoon/internal/engine"
	"github.com/huluohu/raccoon/internal/logx"
	"github.com/huluohu/raccoon/internal/sse"
	"github.com/huluohu/raccoon/internal/store"
)

// newExtTestManager 起一套「真库 + 真 Hub」的最小 Manager（外部执行器只依赖这两样）。
func newExtTestManager(t *testing.T) *Manager {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试库失败：%v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg := config.New()
	hub := sse.New(logx.New(nil, "test", logx.LevelError))
	return NewManager(st, cfg, &engine.Set{}, hub, testLogger())
}

// newExtTask 建一条指定 kind 的任务并推进到 parsing（外部执行器的入口状态）。
func newExtTask(t *testing.T, m *Manager, kind, url string) *store.Task {
	t.Helper()
	id := store.NewTaskID()
	if err := m.st.CreateTask(&store.Task{
		ID: id, Kind: kind, Source: "web", Platform: "qb",
		URL: url, Title: "测试任务",
		DedupeKey:   store.DedupeKey(kind, "qb", id),
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

// fakeQB 可控假 qB 客户端（记录调用序列，返回脚本化状态）。
type fakeQB struct {
	hash     string
	addErr   error
	torrents []QBTorrent
	torErr   error
	deleted  []string // 记录 Delete 入参
	addCalls int
}

func (f *fakeQB) AddMagnet(ctx context.Context, magnet string, opts QBAddOptions) (string, error) {
	f.addCalls++
	if f.addErr != nil {
		return "", f.addErr
	}
	if f.hash != "" {
		return f.hash, nil
	}
	return "aabbccdd", nil
}

func (f *fakeQB) Torrents(ctx context.Context, hashes []string) ([]QBTorrent, error) {
	if f.torErr != nil {
		return nil, f.torErr
	}
	return f.torrents, nil
}

func (f *fakeQB) Delete(ctx context.Context, hashes []string, deleteFiles bool) error {
	f.deleted = append(f.deleted, hashes...)
	return nil
}

// fakeN115 可控假 115 客户端。
type fakeN115 struct {
	task    *N115Task
	subErr  error
	getErr  error
	subCall int
}

func (f *fakeN115) SubmitOffline(ctx context.Context, req N115Request) (*N115Task, error) {
	f.subCall++
	if f.subErr != nil {
		return nil, f.subErr
	}
	return &N115Task{OfflineID: "off-1", TaskID: "task-1", Name: "离线任务"}, nil
}

func (f *fakeN115) GetOfflineTask(ctx context.Context, ref string) (*N115Task, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.task, nil
}

// fastExec 构造轮询间隔 5ms 的执行器（测试不能等 5 秒 tick）。
func fastExec(m *Manager, qb QBClient, n115 N115Client) *ExternalExecutor {
	e := NewExternalExecutor(m, qb, n115)
	e.pollI = 5 * time.Millisecond
	return e
}

// TestExternalExecutor_Supports 类型路由矩阵。
func TestExternalExecutor_Supports(t *testing.T) {
	e := NewExternalExecutor(nil, nil, nil)
	if !e.Supports(KindQB) || !e.Supports(KindN115) {
		t.Error("qb/n115 应由外部执行器处理")
	}
	for _, k := range []string{KindDownload, KindRecord, KindPost, "bogus", ""} {
		if e.Supports(k) {
			t.Errorf("%q 不应由外部执行器处理", k)
		}
	}
}

// TestRunQB_完整生命周期 waiting→parsing→downloading→post→done。
func TestRunQB_完整生命周期(t *testing.T) {
	m := newExtTestManager(t)
	task := newExtTask(t, m, KindQB, "magnet:?xt=urn:btih:aabbccddeeff00112233445566778899aabbccdd")
	qb := &fakeQB{torrents: []QBTorrent{{
		Hash: "aabbccdd", State: "seeding", Percent: 100, Size: 1024,
		SavePath: t.TempDir(), Name: "movie.mkv", Downloaded: 1024,
	}}}
	e := fastExec(m, qb, nil)

	e.Run(context.Background(), task)

	if qb.addCalls != 1 {
		t.Errorf("应提交 1 次磁力，实际 %d", qb.addCalls)
	}
	got, err := m.st.GetTask(task.ID)
	if err != nil {
		t.Fatalf("查任务失败：%v", err)
	}
	if got.Status != StatusDone {
		t.Fatalf("任务应到达 done，实际 %s（error_code=%s msg=%s）", got.Status, got.ErrorCode, got.ErrorMessage)
	}
	if got.Progress != 100 {
		t.Errorf("进度应回填 100，实际 %v", got.Progress)
	}
	if got.Engine != "qbittorrent" {
		t.Errorf("engine 应标记 qbittorrent，实际 %q", got.Engine)
	}
}

// TestRunQB_种子错误转失败 qB 报 error 状态 → failed 且错误码可读。
func TestRunQB_种子错误转失败(t *testing.T) {
	m := newExtTestManager(t)
	task := newExtTask(t, m, KindQB, "magnet:?xt=urn:btih:aabbccddeeff00112233445566778899aabbccdd")
	qb := &fakeQB{torrents: []QBTorrent{{Hash: "aabbccdd", State: "error", Percent: 0}}}
	e := fastExec(m, qb, nil)

	e.Run(context.Background(), task)

	got, _ := m.st.GetTask(task.ID)
	if got.Status != StatusFailed {
		t.Fatalf("应失败，实际 %s", got.Status)
	}
	if got.ErrorCode != "QB_FAILED" {
		t.Errorf("错误码应为 QB_FAILED，实际 %q", got.ErrorCode)
	}
	if got.FixHint == "" {
		t.Error("失败必须带 fix_hint（用户知道去哪修）")
	}
}

// TestRunQB_未配置客户端 仅该任务失败，错误码明确（docs/01 §5.8）。
func TestRunQB_未配置客户端(t *testing.T) {
	m := newExtTestManager(t)
	task := newExtTask(t, m, KindQB, "magnet:?xt=urn:btih:aabbccddeeff00112233445566778899aabbccdd")
	e := fastExec(m, nil, nil)

	e.Run(context.Background(), task)

	got, _ := m.st.GetTask(task.ID)
	if got.Status != StatusFailed || got.ErrorCode != "QB_UNAVAILABLE" {
		t.Fatalf("应 QB_UNAVAILABLE 失败，实际 status=%s code=%s", got.Status, got.ErrorCode)
	}
}

// TestRunQB_取消语义 ctx 取消 → cancelled，且不下发 Delete（ ctx 取消时进程也要停了）。
func TestRunQB_取消语义(t *testing.T) {
	m := newExtTestManager(t)
	task := newExtTask(t, m, KindQB, "magnet:?xt=urn:btih:aabbccddeeff00112233445566778899aabbccdd")
	// Torrents 恒返回 downloading：不取消就永远完不成，只能被取消收尾。
	qb := &fakeQB{torrents: []QBTorrent{{Hash: "aabbccdd", State: "downloading", Percent: 50}}}
	e := fastExec(m, qb, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	e.Run(ctx, task)

	got, _ := m.st.GetTask(task.ID)
	if got.Status != StatusCancelled {
		t.Fatalf("应 cancelled，实际 %s", got.Status)
	}
}

// TestRunQB_用户取消删除种子 cancel_requested 置位 → 调 qB Delete 清理僵尸下载。
func TestRunQB_用户取消删除种子(t *testing.T) {
	m := newExtTestManager(t)
	task := newExtTask(t, m, KindQB, "magnet:?xt=urn:btih:aabbccddeeff00112233445566778899aabbccdd")
	qb := &fakeQB{torrents: []QBTorrent{{Hash: "aabbccdd", State: "downloading", Percent: 50}}}
	e := fastExec(m, qb, nil)

	// 后台置取消位（模拟用户点「取消」）。
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = m.st.UpdateTaskFields(task.ID, map[string]any{"cancel_requested": 1}, "")
	}()
	e.Run(context.Background(), task)

	got, _ := m.st.GetTask(task.ID)
	if got.Status != StatusCancelled {
		t.Fatalf("应 cancelled，实际 %s", got.Status)
	}
	if len(qb.deleted) == 0 {
		t.Error("用户取消应调用 qB Delete，避免 qB 里留下僵尸下载")
	}
}

// TestRunN115_完整生命周期 115 离线任务同状态机走完。
func TestRunN115_完整生命周期(t *testing.T) {
	m := newExtTestManager(t)
	task := newExtTask(t, m, KindN115, "magnet:?xt=urn:btih:aabbccddeeff00112233445566778899aabbccdd")
	n := &fakeN115{task: &N115Task{
		OfflineID: "off-1", Status: "done", Progress: 100, Size: 2048, Name: "剧集S01E01.mkv",
		Files: []N115File{{FileID: "f1", Name: "剧集S01E01.mkv", Size: 2048}},
	}}
	e := fastExec(m, nil, n)

	e.Run(context.Background(), task)

	if n.subCall != 1 {
		t.Errorf("应提交 1 次离线任务，实际 %d", n.subCall)
	}
	got, _ := m.st.GetTask(task.ID)
	if got.Status != StatusDone {
		t.Fatalf("应 done，实际 %s（code=%s）", got.Status, got.ErrorCode)
	}
}

// TestRunN115_离线任务失败 云端报 failed → failed。
func TestRunN115_离线任务失败(t *testing.T) {
	m := newExtTestManager(t)
	task := newExtTask(t, m, KindN115, "magnet:?xt=urn:btih:aabbccddeeff00112233445566778899aabbccdd")
	n := &fakeN115{task: &N115Task{OfflineID: "off-1", Status: "failed", Progress: 0}}
	e := fastExec(m, nil, n)

	e.Run(context.Background(), task)

	got, _ := m.st.GetTask(task.ID)
	if got.Status != StatusFailed || got.ErrorCode != "N115_FAILED" {
		t.Fatalf("应 N115_FAILED，实际 status=%s code=%s", got.Status, got.ErrorCode)
	}
}

// TestRunN115_未配置客户端 仅该任务失败。
func TestRunN115_未配置客户端(t *testing.T) {
	m := newExtTestManager(t)
	task := newExtTask(t, m, KindN115, "https://example.com/file.zip")
	e := fastExec(m, nil, nil)

	e.Run(context.Background(), task)

	got, _ := m.st.GetTask(task.ID)
	if got.Status != StatusFailed || got.ErrorCode != "N115_UNAVAILABLE" {
		t.Fatalf("应 N115_UNAVAILABLE，实际 status=%s code=%s", got.Status, got.ErrorCode)
	}
}

// TestRunQB_提交即失败 客户端提交阶段报错 → QB_SUBMIT_FAILED，不进轮询。
func TestRunQB_提交即失败(t *testing.T) {
	m := newExtTestManager(t)
	task := newExtTask(t, m, KindQB, "magnet:?xt=urn:btih:aabbccddeeff00112233445566778899aabbccdd")
	qb := &fakeQB{addErr: errors.New("qB 拒绝了登录")}
	e := fastExec(m, qb, nil)

	e.Run(context.Background(), task)

	got, _ := m.st.GetTask(task.ID)
	if got.Status != StatusFailed || got.ErrorCode != "QB_SUBMIT_FAILED" {
		t.Fatalf("应 QB_SUBMIT_FAILED，实际 status=%s code=%s", got.Status, got.ErrorCode)
	}
	if qb.addCalls != 1 {
		t.Error("提交失败后不应继续轮询")
	}
}

// TestRunQB_不影响其他任务 qB 挂掉不影响同库其他任务（docs/01 §5.8）。
func TestRunQB_不影响其他任务(t *testing.T) {
	m := newExtTestManager(t)
	// 一条会失败的 qB 任务。
	qbTask := newExtTask(t, m, KindQB, "magnet:?xt=urn:btih:aabbccddeeff00112233445566778899aabbccdd")
	// 一条普通下载任务（保持 waiting，模拟队列里其他用户任务）。
	dlID := store.NewTaskID()
	if err := m.st.CreateTask(&store.Task{
		ID: dlID, Kind: KindDownload, Source: "web", Platform: "bilibili",
		URL:         "https://www.bilibili.com/video/BV1xx411c7mD",
		DedupeKey:   store.DedupeKey(KindDownload, "bilibili", dlID),
		MaxAttempts: 3, CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("建下载任务失败：%v", err)
	}

	// qB 未配置 → qbTask 失败。
	e := fastExec(m, nil, nil)
	e.Run(context.Background(), qbTask)

	dl, _ := m.st.GetTask(dlID)
	if dl.Status != StatusWaiting {
		t.Errorf("qB 失败不应波及下载任务，实际 %s", dl.Status)
	}
	// 闸门查询也不应把 qB 失败算成平台并发占用。
	_, byPlatform, err := m.st.ActiveConcurrency()
	if err != nil {
		t.Fatalf("查并发失败：%v", err)
	}
	if byPlatform["qb"] != 0 {
		t.Errorf("失败的 qB 任务不应占并发，实际 %d", byPlatform["qb"])
	}
}
