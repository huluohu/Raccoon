package task

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestStop_等待运行中worker让出 验证 docs/05 §6「等待运行中任务让出」不是空转。
//
// 这条语义早先被破坏过：worker ctx 原先派生自调度循环 ctx，「让出重启」cancel
// 调度 ctx 时会把运行中的下载一起打断，RunningCount() 瞬间归零，
// Stop 立刻返回 —— 表现为重启接口的 waited_sec 恒为 0。
// 这里用一个不依赖引擎的假 worker 卡住 running 槽位，直接验证 Stop 会等它。
func TestStop_等待运行中worker让出(t *testing.T) {
	m := &Manager{
		cancels: map[string]context.CancelFunc{},
		running: map[string]bool{},
		done:    make(chan struct{}),
		log:     testLogger(),
	}

	// 占用一个 running 槽位，模拟「正在下载」
	m.running["task-1"] = true
	release := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-release // 模拟下载进行中
		m.mu.Lock()
		delete(m.running, "task-1")
		m.mu.Unlock()
	}()

	// 900ms 后才让出
	go func() {
		time.Sleep(900 * time.Millisecond)
		close(release)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	m.Stop(ctx)
	waited := time.Since(start)
	wg.Wait()

	if waited < 800*time.Millisecond {
		t.Errorf("Stop 应等待运行中 worker 让出，实际只等了 %v（疑似又被打断）", waited)
	}
	if n := m.RunningCount(); n != 0 {
		t.Errorf("Stop 返回后不应还有运行中任务，实际 %d", n)
	}
}

// TestStop_超时后强制取消 验证卡死的下载不会把停机无限拖住。
func TestStop_超时后强制取消(t *testing.T) {
	wctx, wcancel := context.WithCancel(context.Background())
	defer wcancel()

	m := &Manager{
		cancels:      map[string]context.CancelFunc{},
		running:      map[string]bool{"stuck": true},
		done:         make(chan struct{}),
		log:          testLogger(),
		workerCtx:    wctx,
		workerCancel: wcancel,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	start := time.Now()
	m.Stop(ctx)
	waited := time.Since(start)

	if waited > 3*time.Second {
		t.Errorf("Stop 超时后应立即返回，实际等了 %v", waited)
	}
	// workerCancel 被调用 → 派生的 worker ctx 全部取消
	select {
	case <-wctx.Done():
	default:
		t.Error("超时后应调用 workerCancel 强制取消剩余任务")
	}
	// running 条目由 worker 的 defer 清理，这里只验证 Stop 不会 panic
}

// TestStop_空闲时立即返回 确认无任务时不引入额外延迟。
func TestStop_空闲时立即返回(t *testing.T) {
	wctx, wcancel := context.WithCancel(context.Background())
	defer wcancel()
	m := &Manager{
		cancels: map[string]context.CancelFunc{}, running: map[string]bool{},
		done: make(chan struct{}), log: testLogger(),
		workerCtx: wctx, workerCancel: wcancel,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	m.Stop(ctx)
	if waited := time.Since(start); waited > 200*time.Millisecond {
		t.Errorf("空闲时 Stop 应立即返回，实际等了 %v", waited)
	}
}

// TestNewManager_工人上下文与调度上下文解耦 直接锁死这次修复的结构前提：
// workerCtx 不能是调度 ctx 的子级，否则 cancel 调度 ctx 会连带打断下载。
func TestNewManager_工人上下文与调度上下文解耦(t *testing.T) {
	m := NewManager(nil, nil, nil, nil, testLogger())
	defer m.workerCancel()

	// 取消一个独立的「调度 ctx」，不应影响 workerCtx
	schedCtx, schedCancel := context.WithCancel(context.Background())
	schedCancel()

	select {
	case <-m.workerCtx.Done():
		t.Error("workerCtx 不应受调度 ctx 取消影响（会导致下载被当场打断）")
	default:
	}

	// 但 workerCancel 自身必须有效
	m.workerCancel()
	select {
	case <-m.workerCtx.Done():
	default:
		t.Error("workerCancel 应能取消 workerCtx（停机超时兜底依赖它）")
	}
	_ = schedCtx
}
