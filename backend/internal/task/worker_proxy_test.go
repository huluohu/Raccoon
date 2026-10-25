package task

import (
	"context"
	"strings"
	"testing"

	"github.com/huluohu/raccoon/internal/engine"
)

// proxyRecordEngine 记录每次调用的代理参数，可按脚本返回错误。
type proxyRecordEngine struct {
	fakeWorkerEngine
	resolveCalls  []string // 每次 Resolve 传入的 Proxy 参数
	resolveErrs   []error  // 每次 Resolve 返回的错误
	downloadCalls []string // 每次 Download 传入的 HTTPProxy 参数
}

func (e *proxyRecordEngine) Resolve(ctx context.Context, url string, opts engine.ResolveOpts) (*engine.ResolveResult, error) {
	idx := len(e.resolveCalls)
	e.resolveCalls = append(e.resolveCalls, opts.Proxy)
	if idx < len(e.resolveErrs) && e.resolveErrs[idx] != nil {
		return nil, e.resolveErrs[idx]
	}
	return &engine.ResolveResult{Title: "代理测试标题", Platform: "test"}, nil
}

func (e *proxyRecordEngine) Download(ctx context.Context, url string, opts engine.DownloadOpts, onProgress func(engine.Progress)) ([]engine.DownloadedFile, error) {
	e.downloadCalls = append(e.downloadCalls, opts.HTTPProxy)
	if len(e.downloadErrs) > 0 && len(e.downloadCalls) <= len(e.downloadErrs) {
		return nil, e.downloadErrs[len(e.downloadCalls)-1]
	}
	return []engine.DownloadedFile{{Path: "/tmp/fake.mp4", Size: 1024}}, nil
}

// TestPreview_配了代理就用代理 预览与下载必须同一口径：
// 配了全局代理就**直接走代理**，不从直连开始试——
// 从直连开始会让「明明能下载」的任务卡在预览超时上（直连把 deadline 耗尽，
// 后面那次代理重试必然跟着一起死）。
func TestPreview_配了代理就用代理(t *testing.T) {
	m := newExtTestManager(t)
	_ = m.cfg.Set("net.proxy", "http://127.0.0.1:7890")

	eng := &proxyRecordEngine{}
	m.SetEngineFor(func(url string) engine.Engine { return eng })

	pv, err := m.Preview(context.Background(), "https://example.com/video")
	if err != nil {
		t.Fatalf("Preview 不应报错：%v", err)
	}
	if len(eng.resolveCalls) != 1 {
		t.Fatalf("配了代理应只尝试一次（直接走代理），实际 %d 次：%v",
			len(eng.resolveCalls), eng.resolveCalls)
	}
	if eng.resolveCalls[0] != "http://127.0.0.1:7890" {
		t.Fatalf("第 1 次就该用全局代理，得到 %q", eng.resolveCalls[0])
	}
	if pv.ProxyUsed != "http://127.0.0.1:7890" {
		t.Fatalf("Preview 结果应记录使用的代理，实际 %q", pv.ProxyUsed)
	}
	if pv.RouteNote != "" {
		t.Fatalf("未切换出口时不应有切换说明，实际 %q", pv.RouteNote)
	}
}

// TestPreview_代理异常回退直连 代理挂了（本地代理没启动是常见情况）要能退回直连，
// 且必须用**全新 ctx**——复用已耗尽的 ctx 会让备用尝试瞬间失败。
func TestPreview_代理异常回退直连(t *testing.T) {
	m := newExtTestManager(t)
	_ = m.cfg.Set("net.proxy", "http://127.0.0.1:7890")

	// 第 1 次（代理）返回 NET_TIMEOUT，第 2 次（直连）成功
	eng := &proxyRecordEngine{
		resolveErrs: []error{
			&engine.EngineError{Code: "NET_TIMEOUT", Message: "连接超时"},
		},
	}
	m.SetEngineFor(func(url string) engine.Engine { return eng })

	pv, err := m.Preview(context.Background(), "https://example.com/video")
	if err != nil {
		t.Fatalf("代理异常时应回退直连成功，实际报错：%v", err)
	}
	if len(eng.resolveCalls) != 2 {
		t.Fatalf("期望 2 次 Resolve 调用（代理 → 直连），实际 %d 次：%v",
			len(eng.resolveCalls), eng.resolveCalls)
	}
	if eng.resolveCalls[0] != "http://127.0.0.1:7890" {
		t.Fatalf("第 1 次应走全局代理，得到 %q", eng.resolveCalls[0])
	}
	if eng.resolveCalls[1] != "" {
		t.Fatalf("第 2 次应回退直连，得到 %q", eng.resolveCalls[1])
	}
	if pv.ProxyUsed != "" {
		t.Fatalf("回退直连后应记录直连出口，实际 %q", pv.ProxyUsed)
	}
	if !strings.Contains(pv.RouteNote, "切换") {
		t.Fatalf("Preview 结果应附带切换出口提示，实际 %q", pv.RouteNote)
	}
}

// TestPreview_手动传参指定代理 验证 Preview 手工传入代理时优先使用该代理
func TestPreview_手动传参指定代理(t *testing.T) {
	m := newExtTestManager(t)
	_ = m.cfg.Set("net.proxy", "http://default-proxy:7890")

	eng := &proxyRecordEngine{}
	m.SetEngineFor(func(url string) engine.Engine { return eng })

	pv, err := m.Preview(context.Background(), "https://example.com/video", "socks5://127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	if len(eng.resolveCalls) != 1 {
		t.Fatalf("期望恰好 1 次 Resolve 调用，实际 %d", len(eng.resolveCalls))
	}
	if eng.resolveCalls[0] != "socks5://127.0.0.1:1080" {
		t.Fatalf("应使用显式传入的代理，实际使用 %q", eng.resolveCalls[0])
	}
	if pv.ProxyUsed != "socks5://127.0.0.1:1080" {
		t.Fatalf("Preview 结果应记录显式代理，实际 %q", pv.ProxyUsed)
	}
}

// TestWorker_直连下载超时自动切换代理重试
func TestWorker_直连下载超时自动切换代理重试(t *testing.T) {
	m := newExtTestManager(t)
	_ = m.cfg.Set("net.proxy", "http://127.0.0.1:7890")

	eng := &proxyRecordEngine{
		fakeWorkerEngine: fakeWorkerEngine{
			downloadErrs: []error{
				&engine.EngineError{Code: "NET_TIMEOUT", Message: "连接超时"},
			},
		},
	}
	w := &Worker{mgr: m, eng: eng}
	task := newDownloadTask(t, m) // proxy 为空（直连）

	w.Run(context.Background(), task)

	cur := mustTask(t, m, task.ID)
	if cur.Status != StatusWaiting || cur.WaitReason != "auto_retry" {
		t.Fatalf("直连失败应进入自动重试，实际状态 %s / %s", cur.Status, cur.WaitReason)
	}
	if cur.Proxy != "http://127.0.0.1:7890" {
		t.Fatalf("任务 proxy 字段应自动回填全局代理，实际 %q", cur.Proxy)
	}
	if !strings.Contains(cur.RoutePath, "自动切换代理重试") {
		t.Fatalf("route_path 应记录切换代理痕迹，实际 %q", cur.RoutePath)
	}

	events, _, err := m.st.ListEvents(task.ID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	foundRetry := false
	for _, e := range events {
		if e.Event == "retry_scheduled" && strings.Contains(e.Message, "自动切换代理") {
			foundRetry = true
			break
		}
	}
	if !foundRetry {
		t.Fatalf("未找到包含「自动切换代理」的 retry_scheduled 事件: %+v", events)
	}
}

// TestManager_RetryWithProxy 手动切换代理重试
func TestManager_RetryWithProxy(t *testing.T) {
	m := newExtTestManager(t)
	_ = m.cfg.Set("net.proxy", "http://global-proxy:7890")

	task := newDownloadTask(t, m)
	// 置为 failed 态
	if err := m.st.UpdateTaskFields(task.ID, map[string]any{"status": StatusFailed}, "parsing"); err != nil {
		t.Fatal(err)
	}

	// 1. 指定自定义代理
	if err := m.RetryWithProxy(task.ID, "socks5://127.0.0.1:1080"); err != nil {
		t.Fatalf("指定代理重试失败: %v", err)
	}
	cur := mustTask(t, m, task.ID)
	if cur.Status != StatusWaiting {
		t.Fatalf("重试后状态应为 waiting，实际 %s", cur.Status)
	}
	if cur.Proxy != "socks5://127.0.0.1:1080" {
		t.Fatalf("Proxy 字段应更新为指定代理，实际 %q", cur.Proxy)
	}
	if !strings.Contains(cur.RoutePath, "手动切换代理重试") {
		t.Fatalf("route_path 应记录手动切换代理，实际 %q", cur.RoutePath)
	}

	// 2. 传空使用全局默认代理
	if err := m.st.UpdateTaskFields(task.ID, map[string]any{"status": StatusFailed}, "waiting"); err != nil {
		t.Fatal(err)
	}
	if err := m.RetryWithProxy(task.ID, ""); err != nil {
		t.Fatalf("使用默认代理重试失败: %v", err)
	}
	cur = mustTask(t, m, task.ID)
	if cur.Proxy != "http://global-proxy:7890" {
		t.Fatalf("Proxy 字段应回退为全局代理，实际 %q", cur.Proxy)
	}
}

// TestPreview与Worker_出口一致 核心不变式：预览走的出口必须与下载走的出口相同。
//
// 这条不变式破了就是用户报的「明明可以下载，却解析失败」：
// 预览走 A、下载走 B，用户看到预览失败就以为下不了，实际任务能成；
// 反过来则更糟——预览成功、下载超时，用户以为能下结果下不来。
func TestPreview与Worker_出口一致(t *testing.T) {
	m := newExtTestManager(t)
	_ = m.cfg.Set("net.proxy", "http://shared-proxy:7890")

	eng := &proxyRecordEngine{
		fakeWorkerEngine: fakeWorkerEngine{
			files: []engine.DownloadedFile{{Path: "/tmp/fake.mp4", Size: 1024}},
		},
	}
	m.SetEngineFor(func(url string) engine.Engine { return eng })

	// 预览
	pv, err := m.Preview(context.Background(), "https://example.com/video")
	if err != nil {
		t.Fatalf("Preview 失败: %v", err)
	}
	previewProxy := eng.resolveCalls[len(eng.resolveCalls)-1]

	// 任务
	w := &Worker{mgr: m, eng: eng}
	task := newDownloadTask(t, m)
	w.Run(context.Background(), task)

	if len(eng.downloadCalls) == 0 {
		t.Fatal("任务应发起过下载")
	}
	downloadProxy := eng.downloadCalls[len(eng.downloadCalls)-1]

	if previewProxy != downloadProxy {
		t.Fatalf("预览出口 %q 与下载出口 %q 不一致——这会让用户看到「预览失败但能下载」或反过来的错位",
			previewProxy, downloadProxy)
	}
	if pv.ProxyUsed != downloadProxy {
		t.Fatalf("Preview 记录的出口 %q 与实际下载出口 %q 不一致", pv.ProxyUsed, downloadProxy)
	}
}

// TestManager_RetryWithProxy_无代理时报错 当无全局代理且未传入代理时拒绝重试并说明原因
func TestManager_RetryWithProxy_无代理时报错(t *testing.T) {
	m := newExtTestManager(t)
	_ = m.cfg.Set("net.proxy", "")

	task := newDownloadTask(t, m)
	if err := m.st.UpdateTaskFields(task.ID, map[string]any{"status": StatusFailed}, "parsing"); err != nil {
		t.Fatal(err)
	}

	err := m.RetryWithProxy(task.ID, "")
	if err == nil {
		t.Fatal("未配置代理时应报错")
	}
	if !strings.Contains(err.Error(), "未配置网络代理") {
		t.Fatalf("错误应提示未配置网络代理，实际: %v", err)
	}
}
