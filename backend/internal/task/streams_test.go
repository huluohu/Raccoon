package task

import (
	"context"
	"testing"

	"github.com/huluohu/raccoon/internal/engine"
)

// TestSumStreamTotals_单流精确 单流下载只有一个 total，累加结果就是它本身。
func TestSumStreamTotals_单流精确(t *testing.T) {
	got := sumStreamTotals(map[int64]bool{1000: true})
	if got != 1000 {
		t.Fatalf("单流应等于该流大小，实际 %d", got)
	}
}

// TestSumStreamTotals_多流累加 DASH 双流：视频流 + 音频流应相加。
//
// 这是「任务详情显示 210.6 MB / 13.3 MB」的根因回归：
// 原实现取第一帧有 total 的进度行（恰是音频流），把 expected_size 写成
// 13.27 MiB，而真实文件是视频+音频合并后的 210.58 MiB。
func TestSumStreamTotals_多流累加(t *testing.T) {
	got := sumStreamTotals(map[int64]bool{
		207040993: true, // 视频流
		13916470:  true, // 音频流
	})
	want := int64(207040993 + 13916470)
	if got != want {
		t.Fatalf("多流应累加为 %d，实际 %d", want, got)
	}
}

// TestSumStreamTotals_重复流只算一次 同一流的多帧 total 相同，去重后只算一次。
func TestSumStreamTotals_重复流只算一次(t *testing.T) {
	got := sumStreamTotals(map[int64]bool{5000: true})
	if got != 5000 {
		t.Fatalf("同一流不应重复累加，实际 %d", got)
	}
}

// TestExpectedSizeFor_播放列表返回未知 播放列表总大小不可知（禁估算）→ 0。
func TestExpectedSizeFor_播放列表返回未知(t *testing.T) {
	got := expectedSizeFor(map[int64]bool{1000: true, 2000: true}, 3)
	if got != 0 {
		t.Fatalf("播放列表应返回 0（未知），实际 %d", got)
	}
}

// TestExpectedSizeFor_单条目返回累加值。
func TestExpectedSizeFor_单条目返回累加值(t *testing.T) {
	got := expectedSizeFor(map[int64]bool{1000: true, 2000: true}, 1)
	if got != 3000 {
		t.Fatalf("单条目应返回累加值 3000，实际 %d", got)
	}
}

// dashProgressEngine 模拟 DASH 双流下载的进度序列：
//
//	视频流（分片，total 期间 NA，末尾揭晓）→ 音频流（total 从头准确）
type dashProgressEngine struct {
	fakeWorkerEngine
}

func (e *dashProgressEngine) Download(ctx context.Context, url string, opts engine.DownloadOpts, onProgress func(engine.Progress)) ([]engine.DownloadedFile, error) {
	// 视频流：total NA
	for _, d := range []int64{1000, 2000, 3000} {
		onProgress(engine.Progress{Percent: -1, Downloaded: d, Total: 0, SpeedBps: 100})
	}
	// 视频流末尾揭晓总大小（yt-dlp 行为）
	onProgress(engine.Progress{Percent: -1, Downloaded: 207040993, Total: 207040993, SpeedBps: 100})
	// 切到音频流：total 变了 → 上一流 downloaded 结转
	onProgress(engine.Progress{Percent: -1, Downloaded: 1024, Total: 13916470, SpeedBps: 50})
	onProgress(engine.Progress{Percent: -1, Downloaded: 13916470, Total: 13916470, SpeedBps: 50})
	return []engine.DownloadedFile{{Path: "/tmp/merged.mp4", Size: 220812087}}, nil
}

// TestWorker_DASH双流_预期大小为累加值 下载过程中 expected_size 应是各流之和，
// 而非某一流单独的大小。
func TestWorker_DASH双流_预期大小为累加值(t *testing.T) {
	m := newExtTestManager(t)
	eng := &dashProgressEngine{}
	w := &Worker{mgr: m, eng: eng}
	task := newDownloadTask(t, m)

	w.Run(context.Background(), task)

	cur := mustTask(t, m, task.ID)
	if cur.Status != StatusDone {
		t.Fatalf("任务应完成，实际 %s（%s）", cur.Status, cur.ErrorMessage)
	}
	// 完成态用真实文件大小校正：220812087
	if cur.ExpectedSize == nil || *cur.ExpectedSize != 220812087 {
		t.Fatalf("完成态 expected_size 应为真实文件大小 220812087，实际 %v", cur.ExpectedSize)
	}
	if cur.DownloadedSize != 220812087 {
		t.Fatalf("downloaded_size 应为真实文件大小，实际 %d", cur.DownloadedSize)
	}
}
