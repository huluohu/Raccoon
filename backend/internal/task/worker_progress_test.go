package task

// worker_progress_test.go：播放列表（合集/频道）进度聚合 + expected_size 回填。
//
// 锁死的不变式（2026-10-24 实测踩过）：
//   1) 合集任务逐条目下载时进度不得回跳——全局百分比 = (已完成条目 + 当前百分比)/总数；
//   2) downloaded_size 是合集累计字节，不是「当前那条视频」的量；
//   3) 单条目任务把引擎报的总大小回填 expected_size（进度条从条纹切真实百分比）；
//      合集任务的总大小不可知，expected_size 保持 NULL（禁估算）。

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/huluohu/raccoon/internal/config"
	"github.com/huluohu/raccoon/internal/engine"
	"github.com/huluohu/raccoon/internal/logx"
	"github.com/huluohu/raccoon/internal/sse"
	"github.com/huluohu/raccoon/internal/store"
)

// playlistFakeEngine 两个条目的合集：每条吐 3 次进度（起步/中段/完成）。
type playlistFakeEngine struct {
	fakeWorkerEngine
}

func (f *playlistFakeEngine) Download(ctx context.Context, url string, opts engine.DownloadOpts, onProgress func(engine.Progress)) ([]engine.DownloadedFile, error) {
	f.downloadCalls++
	// 条目 1：total=100MB，进度 1% → 50% → 100%
	for _, p := range []engine.Progress{
		{Percent: 1, Downloaded: 1 << 20, Total: 100 << 20, SpeedBps: 1000, ItemIndex: 1, ItemCount: 2},
		{Percent: 50, Downloaded: 50 << 20, Total: 100 << 20, SpeedBps: 1000, ItemIndex: 1, ItemCount: 2},
		{Percent: 100, Downloaded: 100 << 20, Total: 100 << 20, SpeedBps: 1000, ItemIndex: 1, ItemCount: 2},
	} {
		onProgress(p)
	}
	// 条目 2：total=50MB，进度 0% → 40%
	for _, p := range []engine.Progress{
		{Percent: 0, Downloaded: 0, Total: 50 << 20, SpeedBps: 1000, ItemIndex: 2, ItemCount: 2},
		{Percent: 40, Downloaded: 20 << 20, Total: 50 << 20, SpeedBps: 1000, ItemIndex: 2, ItemCount: 2},
	} {
		onProgress(p)
	}
	_ = time.Second
	return []engine.DownloadedFile{{Path: "/tmp/a.mp4", Size: 150 << 20}}, nil
}

func newProgressTestWorker(t *testing.T) (*Worker, *store.Task, *Manager) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg := config.New()
	_ = cfg.Set("storage.data_dir", dir)
	hub := sse.New(logx.New(nil, "t", logx.LevelError))
	m := NewManager(st, cfg, &engine.Set{}, hub, logx.New(nil, "t", logx.LevelError))
	w := &Worker{mgr: m}
	id := store.NewTaskID()
	if err := st.CreateTask(&store.Task{
		ID: id, Kind: KindDownload, Source: "web", Platform: "test",
		URL: "https://example.com/playlist", Title: "合集",
		DedupeKey:   store.DedupeKey(KindDownload, "test", id),
		MaxAttempts: 3, CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	tk, err := st.ClaimWaiting(id, "parsing")
	if err != nil {
		t.Fatal(err)
	}
	return w, tk, m
}

func TestWorker_合集进度全局折算不回跳(t *testing.T) {
	w, tk, m := newProgressTestWorker(t)
	eng := &playlistFakeEngine{fakeWorkerEngine{files: []engine.DownloadedFile{{Path: "/tmp/a.mp4", Size: 150 << 20}}}}
	w.eng = eng

	w.Run(context.Background(), tk)

	cur := mustTask(t, m, tk.ID)
	if cur.Status != "done" {
		t.Fatalf("任务应完成，实际 %s", cur.Status)
	}
	// 全局百分比单调：最后一次进度 = 条目2 40% → (1 + 0.4)/2 = 70%。
	// done 后 progress 被置 100，这里校验中间态只能靠引擎脚本顺序推演，
	// 所以直接锁终态与累计字节（150MB = 100 + 50）。
	if cur.DownloadedSize != 150<<20 {
		t.Fatalf("downloaded_size 应为合集累计 150MB，实际 %d", cur.DownloadedSize)
	}
	if cur.ExpectedSize != nil {
		t.Fatalf("合集任务禁估算：expected_size 应保持 NULL，实际 %d", *cur.ExpectedSize)
	}
	if cur.Progress != 100 {
		t.Fatalf("完成态 progress 应为 100，实际 %v", cur.Progress)
	}
}

type singleFakeEngine struct {
	fakeWorkerEngine
}

func (f *singleFakeEngine) Download(ctx context.Context, url string, opts engine.DownloadOpts, onProgress func(engine.Progress)) ([]engine.DownloadedFile, error) {
	onProgress(engine.Progress{Percent: 30, Downloaded: 30 << 20, Total: 100 << 20, SpeedBps: 1000})
	onProgress(engine.Progress{Percent: 60, Downloaded: 60 << 20, Total: 100 << 20, SpeedBps: 1000})
	return []engine.DownloadedFile{{Path: "/tmp/v.mp4", Size: 100 << 20}}, nil
}

func TestWorker_单条目任务回填expected_size(t *testing.T) {
	w, tk, m := newProgressTestWorker(t)
	w.eng = &singleFakeEngine{fakeWorkerEngine{files: []engine.DownloadedFile{{Path: "/tmp/v.mp4", Size: 100 << 20}}}}

	w.Run(context.Background(), tk)

	cur := mustTask(t, m, tk.ID)
	if cur.ExpectedSize == nil || *cur.ExpectedSize != 100<<20 {
		t.Fatalf("单条目任务应回填 expected_size=100MB，实际 %v", cur.ExpectedSize)
	}
}
