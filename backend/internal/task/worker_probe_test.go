package task

// worker_probe_test.go：产物校验（ffprobe）与孤儿文件清理的接入验证。
//
// 锁死的不变式：
//   1) 下载失败后，本次下载新产生的 .part / .ytdl 临时文件必须被清理，
//      且不能误删下载开始前就存在的文件；
//   2) ffprobe 校验只记 warn 事件，不把任务打成 failed（任务仍是 done）。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huluohu/raccoon/internal/engine"
	"github.com/huluohu/raccoon/internal/store"
)

// orphanEngine 下载失败时在保存目录里留下 .part 孤儿文件。
type orphanEngine struct {
	fakeWorkerEngine
	saveDir string
	// preexisting ���载前就存在的文件（不该被清理）
	preexisting string
}

func (f *orphanEngine) Download(ctx context.Context, url string, opts engine.DownloadOpts, onProgress func(engine.Progress)) ([]engine.DownloadedFile, error) {
	// 模拟 yt-dlp 中途失败：留下 .part 临时文件
	_ = os.WriteFile(filepath.Join(f.saveDir, "video.mp4.part"), []byte("partial"), 0o600)
	_ = os.WriteFile(filepath.Join(f.saveDir, "video.mp4.ytdl"), []byte("resume"), 0o600)
	return nil, netTimeoutErr("网络连接失败或被重置")
}

// newProbeWorker 建一个 worker，并把 saveDir 指向临时目录。
func newProbeWorker(t *testing.T) (*Worker, *Manager, string) {
	t.Helper()
	m := newExtTestManager(t)
	dir := t.TempDir()
	// 让 saveDirFor 落到临时目录
	_ = m.cfg.Set("storage.data_dir", dir)
	_ = m.cfg.Set("storage.download_dir", "downloads")
	return &Worker{mgr: m}, m, dir
}

// TestWorker_下载失败清理孤儿文件 下载失败后 .part / .ytdl 必须被清掉。
func TestWorker_下载失败清理孤儿文件(t *testing.T) {
	w, m, dataDir := newProbeWorker(t)
	task := newDownloadTask(t, m)
	// 预置一个下载前就存在的 .part 文件（模拟上一次下载的残留）
	pre := filepath.Join(dataDir, "downloads", "test", "old.part")
	if err := os.MkdirAll(filepath.Dir(pre), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pre, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	saveDir := filepath.Join(dataDir, "downloads", "test")
	w.eng = &orphanEngine{saveDir: saveDir}

	w.Run(context.Background(), task)

	// 任务应走自动重试（NET_TIMEOUT 可重试）
	cur := mustTask(t, m, task.ID)
	if cur.Status != StatusWaiting {
		t.Fatalf("前置：NET_TIMEOUT 应进入重试等待，得到 %s", cur.Status)
	}

	// 本次新增的孤儿文件必须被清理
	for _, name := range []string{"video.mp4.part", "video.mp4.ytdl"} {
		if _, err := os.Stat(filepath.Join(saveDir, name)); err == nil {
			t.Fatalf("孤儿文件 %s 应已被清理", name)
		}
	}
	// 下载前就存在的文件不该被删
	if _, err := os.Stat(pre); err != nil {
		t.Fatalf("下载前存在的 %s 不应被清理", pre)
	}
}

// TestWorker_孤儿清理写事件 清理动作要留痕，不能静默删除。
func TestWorker_孤儿清理写事件(t *testing.T) {
	w, m, dataDir := newProbeWorker(t)
	task := newDownloadTask(t, m)
	saveDir := filepath.Join(dataDir, "downloads", "test")
	w.eng = &orphanEngine{saveDir: saveDir}

	w.Run(context.Background(), task)

	events, _, err := m.st.ListEvents(task.ID, 0, 100)
	if err != nil {
		t.Fatalf("读事件失败：%v", err)
	}
	var found bool
	for _, e := range events {
		if e.Event == "orphan_cleaned" {
			found = true
			if !strings.Contains(e.Message, "临时文件") {
				t.Fatalf("orphan_cleaned 事件文案应说明清理了什么，得到 %q", e.Message)
			}
		}
	}
	if !found {
		t.Fatalf("应记录 orphan_cleaned 事件，实际事件：%v", events)
	}
}

// TestWorker_无ffprobe时校验跳过 ffprobe 不可用时任务照常 done，不报错。
func TestWorker_无ffprobe时校验跳过(t *testing.T) {
	w, m, _ := newProbeWorker(t)
	dir := t.TempDir()
	video := filepath.Join(dir, "v.mp4")
	if err := os.WriteFile(video, []byte("fake video"), 0o600); err != nil {
		t.Fatal(err)
	}
	w.eng = &fakeWorkerEngine{files: []engine.DownloadedFile{{Path: video, Size: 10}}}
	task := newDownloadTask(t, m)

	// 引擎集无 ffprobe（newExtTestManager 构造的 Manager 用的空 &engine.Set{}）
	w.Run(context.Background(), task)

	cur := mustTask(t, m, task.ID)
	if cur.Status != StatusDone {
		t.Fatalf("无 ffprobe 时任务应照常 done，得到 %s（%s）", cur.Status, cur.ErrorMessage)
	}
}

// TestWorker_probeFiles_无ffprobe不写事件 ffprobe 缺失时不产生噪音事件。
func TestWorker_probeFiles_无ffprobe不写事件(t *testing.T) {
	w, m, _ := newProbeWorker(t)
	task := newDownloadTask(t, m)

	w.probeFiles(task.ID, []engine.DownloadedFile{{Path: "/tmp/x.mp4", Size: 1}})

	events, _, err := m.st.ListEvents(task.ID, 0, 100)
	if err != nil {
		t.Fatalf("读事件失败：%v", err)
	}
	for _, e := range events {
		if e.Event == "probe_warn" {
			t.Fatalf("无 ffprobe 时不应写 probe_warn 事件：%v", e)
		}
	}
}

// TestWorker_probeFiles_有ffprobe时校验失败记warn 用一个必然失败的 ffprobe 路径，
// 触发 Warn；事件应记 warn 且不影响任务终态。
func TestWorker_probeFiles_有ffprobe时校验失败记warn(t *testing.T) {
	w, m, _ := newProbeWorker(t)
	task := newDownloadTask(t, m)

	// 注入一个坏掉的 ffprobe
	m.engines.FFprobeBin = "/tmp/nonexistent_ffprobe_raccoon_test"

	dir := t.TempDir()
	video := filepath.Join(dir, "v.mp4")
	if err := os.WriteFile(video, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	w.probeFiles(task.ID, []engine.DownloadedFile{{Path: video, Size: 1}})

	events, _, err := m.st.ListEvents(task.ID, 0, 100)
	if err != nil {
		t.Fatalf("读事件失败：%v", err)
	}
	var found bool
	for _, e := range events {
		if e.Event == "probe_warn" {
			found = true
			if e.Level != "warn" {
				t.Fatalf("probe_warn 事件级别应为 warn，得到 %s", e.Level)
			}
		}
	}
	if !found {
		t.Fatalf("ffprobe 失败应记 probe_warn 事件，实际事件：%v", events)
	}
}

// TestProbeFiles_非媒体文件不校验 图片/字幕不触发 ffprobe（无 warn 事件）。
func TestProbeFiles_非媒体文件不校验(t *testing.T) {
	w, m, _ := newProbeWorker(t)
	task := newDownloadTask(t, m)
	m.engines.FFprobeBin = "/tmp/nonexistent_ffprobe_raccoon_test"

	w.probeFiles(task.ID, []engine.DownloadedFile{
		{Path: "/tmp/cover.jpg", Size: 1},
		{Path: "/tmp/sub.srt", Size: 1},
	})

	events, _, err := m.st.ListEvents(task.ID, 0, 100)
	if err != nil {
		t.Fatalf("读事件失败：%v", err)
	}
	for _, e := range events {
		if e.Event == "probe_warn" {
			t.Fatalf("非媒体文件不应触发 probe_warn：%v", e)
		}
	}
}

// 确认 store.TaskEvent 的 Level 字段存在（编译期校验）。
var _ = func(e store.TaskEvent) string { return e.Level }
