package task

// stability_test.go：worker 的三类「会卡死/空转」缺陷回归。
//
// 2026-10-30 审计发现并修复：
//
//  1. panic 兜底回队列没有重试上限、也不退避 → **无限热循环**
//     （panic → 立即回 waiting → 立即到期 → 被重新领取 → 再 panic）。
//     每次白占一个 worker 并发额度 + 一次 DB 写 + 一次 SSE 广播。
//  2. `downloading → post` 的 CAS 失败是裸 return → 任务**永久停在 downloading**，
//     而 downloading 计入并发额度，于是白占额度到进程重启。
//  3. `post → done` 的 CAS 失败是裸 return → 任务**永久停在 post**，
//     且不广播 task.done、不聚合父任务，用户看到「永远处理中」。
//  4. 取消监听（startCancelWatcher）**一个 cancel func 都没有**，命中取消位
//     只是退出 goroutine，什么都没取消。多实例部署时（请求打到另一实例，
//     进程内 map 里没有该 taskID）**接口返回 200「ok」但下载照跑**。

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/huluohu/raccoon/internal/engine"
)

// TestRequeue_有重试上限 连续 panic 达到上限后必须判失败，不能无限回队列。
func TestRequeue_有重试上限(t *testing.T) {
	m := newExtTestManager(t)
	task := newDownloadTask(t, m)
	// 把 attempt 顶到上限 - 1，让下一次 requeue 正好触顶。
	if err := m.st.UpdateTaskFields(task.ID, map[string]any{
		"attempt": task.MaxAttempts - 1,
	}, "parsing"); err != nil {
		t.Fatalf("预置 attempt 失败: %v", err)
	}
	w := &Worker{mgr: m}

	w.requeue(task.ID, "worker_panic")

	cur := mustTask(t, m, task.ID)
	if cur.Status != StatusFailed {
		t.Fatalf("重试用尽后应判失败，实际状态 %s（attempt=%d）", cur.Status, cur.Attempt)
	}
	if cur.ErrorCode != "WORKER_PANIC" {
		t.Errorf("错误码应为 WORKER_PANIC，实际 %q", cur.ErrorCode)
	}
}

// TestRequeue_未到上限时排期退避 未到上限要回队列，但不能「立即到期」——
// 否则调度器 500ms 后就重新领取，形成热循环。
func TestRequeue_未到上限时排期退避(t *testing.T) {
	m := newExtTestManager(t)
	task := newDownloadTask(t, m)
	w := &Worker{mgr: m}

	before := time.Now()
	w.requeue(task.ID, "worker_panic")

	cur := mustTask(t, m, task.ID)
	if cur.Status != StatusWaiting || cur.WaitReason != "worker_panic" {
		t.Fatalf("未到上限应回队列等待重试，实际 %s / %s", cur.Status, cur.WaitReason)
	}
	if cur.Attempt != 1 {
		t.Errorf("attempt 应递增为 1，实际 %d", cur.Attempt)
	}
	// 关键：必须排了退避时间，不能立即到期。
	if cur.ScheduledAt == nil {
		t.Fatal("回队列必须设置 scheduled_at（退避排期），否则立即到期形成热循环")
	}
	if *cur.ScheduledAt <= before.UnixMilli() {
		t.Errorf("scheduled_at 应在未来（退避），实际 %d（现在 %d）", *cur.ScheduledAt, before.UnixMilli())
	}
}

// TestRequeue_到期前不会被调度器领走 排期未到的任务不进 DueWaiting，
// 调度器不会立刻把它捞回来。
func TestRequeue_到期前不会被调度器领走(t *testing.T) {
	m := newExtTestManager(t)
	task := newDownloadTask(t, m)
	w := &Worker{mgr: m}
	w.requeue(task.ID, "worker_panic")

	now := time.Now().UnixMilli()
	due, err := m.st.DueWaiting(now, 10)
	if err != nil {
		t.Fatalf("查到期任务失败: %v", err)
	}
	for _, dueTask := range due {
		if dueTask.ID == task.ID {
			t.Fatal("退避期内的任务不应被调度器领走（否则退避形同虚设）")
		}
	}
}

// TestCancelWatcher_跨实例取消能真正取消 watcher 必须拿到真正的 cancel func：
// 只写 DB 的 cancel_requested 而不取消进程，接口返回 200 但下载照跑。
func TestCancelWatcher_跨实例取消能真正取消(t *testing.T) {
	m := newExtTestManager(t)
	task := newDownloadTask(t, m)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &Worker{mgr: m}

	// 起 watcher（模拟「取消请求打在另一个实例」：本实例 map 里没有 cancel）。
	w.startCancelWatcher(ctx, cancel, task.ID)

	// 模拟另一个实例写入的取消位。
	if err := m.st.UpdateTaskFields(task.ID, map[string]any{"cancel_requested": true}, "parsing"); err != nil {
		t.Fatalf("写取消位失败: %v", err)
	}

	select {
	case <-ctx.Done():
		// 期望：watcher 观察到取消位后取消 ctx。
	case <-time.After(3 * time.Second):
		t.Fatal("watcher 未在 3s 内响应取消位（跨实例取消会失效：接口返回 ok 但下载照跑）")
	}
}

// TestCancelWatcher_无取消位时不误取消 没有取消位时不能取消任务。
func TestCancelWatcher_无取消位时不误取消(t *testing.T) {
	m := newExtTestManager(t)
	task := newDownloadTask(t, m)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &Worker{mgr: m}
	w.startCancelWatcher(ctx, cancel, task.ID)

	select {
	case <-ctx.Done():
		t.Fatal("没有取消位却取消了任务")
	case <-time.After(1500 * time.Millisecond):
		// 期望：不取消。
	}
}

// TestCancelWatcher_终态任务不再等待 已终态的任务 watcher 应退出，不空转。
func TestCancelWatcher_终态任务不再等待(t *testing.T) {
	m := newExtTestManager(t)
	task := newDownloadTask(t, m)
	if err := m.st.UpdateTaskFields(task.ID, map[string]any{"status": StatusDone}, "parsing"); err != nil {
		t.Fatalf("置终态失败: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &Worker{mgr: m}
	w.startCancelWatcher(ctx, cancel, task.ID)

	// 终态 + 无取消位 → 不应取消；watcher 退出与否从外部不可见，
	// 这里只断言「不会误取消」。
	select {
	case <-ctx.Done():
		t.Fatal("终态任务不应触发取消")
	case <-time.After(1200 * time.Millisecond):
	}
}

// TestWorker_Run_引擎失败能离开下载态 引擎失败必须把任务带出 downloading，
// 不能停在 downloading 白占并发额度。
func TestWorker_Run_引擎失败能离开下载态(t *testing.T) {
	m := newExtTestManager(t)
	eng := &proxyRecordEngine{
		fakeWorkerEngine: fakeWorkerEngine{
			downloadErrs: []error{&engine.EngineError{Code: "NET_TIMEOUT", Message: "连接超时"}},
		},
	}
	m.SetEngineFor(func(url string) engine.Engine { return eng })
	w := &Worker{mgr: m, eng: eng}
	task := newDownloadTask(t, m)

	w.Run(context.Background(), task)

	cur := mustTask(t, m, task.ID)
	if cur.Status == "downloading" {
		t.Fatalf("引擎失败后任务仍停在 downloading（白占并发额度），状态 %s", cur.Status)
	}
	// 网络类错误 → 应进入自动重试等待（而不是直接卡住）。
	if cur.Status != StatusWaiting && cur.Status != StatusFailed {
		t.Errorf("引擎失败后状态应为 waiting/failed，实际 %s", cur.Status)
	}
}

// TestTransition_CAS失败可辨识 transition 的 CAS 失败要能被上层区分
// 「已终态」与「真失败」，否则兜底逻辑会误伤已取消的任务。
func TestTransition_CAS失败可辨识(t *testing.T) {
	m := newExtTestManager(t)
	task := newDownloadTask(t, m)
	w := &Worker{mgr: m}

	// 把任务推到别的状态，再从 parsing 推进 → CAS 必然失败。
	if err := m.st.UpdateTaskFields(task.ID, map[string]any{"status": StatusDownloading}, "parsing"); err != nil {
		t.Fatalf("预置状态失败: %v", err)
	}
	err := w.transition(task.ID, "parsing", "post", nil)
	if err == nil {
		t.Fatal("CAS 失败应返回错误")
	}
	// 非终态冲突不应被误判成 errAlreadyTerminal。
	if strings.Contains(err.Error(), errAlreadyTerminal.Error()) {
		t.Errorf("状态冲突（非终态）不应被误判为终态: %v", err)
	}
}
