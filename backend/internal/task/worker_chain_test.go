package task

// worker_chain_test.go：链路层（DB 行 → worker → 引擎 argv 参数）的回归用例。
//
// 为什么单独一层：引擎层只测了「引擎怎么拼 --proxy」，决策层只测了
// 「怎么选出口」，中间那截——**任务上写的代理到底有没有一路传到引擎**——
// 此前完全没有覆盖。三层各自绿，拼起来仍可能断。

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/huluohu/raccoon/internal/engine"
	"github.com/huluohu/raccoon/internal/store"
)

// chainEngine 记录解析/下载收到的代理与可配的解析结果。
type chainEngine struct {
	fakeWorkerEngine
	resolveCalls  []string
	downloadCalls []string
	formats       []engine.FormatInfo
	resolveErrs   []error
}

func (e *chainEngine) Resolve(ctx context.Context, url string, opts engine.ResolveOpts) (*engine.ResolveResult, error) {
	idx := len(e.resolveCalls)
	e.resolveCalls = append(e.resolveCalls, opts.Proxy)
	if idx < len(e.resolveErrs) && e.resolveErrs[idx] != nil {
		return nil, e.resolveErrs[idx]
	}
	res := &engine.ResolveResult{Title: "链路测试", Platform: "test"}
	if len(e.formats) > 0 {
		res.Formats = e.formats
	}
	return res, nil
}

func (e *chainEngine) Download(ctx context.Context, url string, opts engine.DownloadOpts, onProgress func(engine.Progress)) ([]engine.DownloadedFile, error) {
	e.downloadCalls = append(e.downloadCalls, opts.HTTPProxy)
	if len(e.downloadErrs) > 0 && len(e.downloadCalls) <= len(e.downloadErrs) {
		return nil, e.downloadErrs[len(e.downloadCalls)-1]
	}
	return []engine.DownloadedFile{{Path: "/tmp/fake.mp4", Size: 1024}}, nil
}

// newChainTask 建一条带指定代理的任务并领取到 parsing。
func newChainTask(t *testing.T, m *Manager, proxy string) *store.Task {
	t.Helper()
	id := store.NewTaskID()
	if err := m.st.CreateTask(&store.Task{
		ID: id, Kind: KindDownload, Source: "web", Platform: "test",
		URL: "https://example.com/video/1", Title: "链路测试任务",
		Proxy: proxy, Quality: "best",
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

// Test链路_任务级代理一路传到引擎 任务上写了代理，解析与下载两侧都必须用它。
// （此前只有引擎层的 argv 断言，链路层没测——「写了代理却没生效」查不出来。）
func Test链路_任务级代理一路传到引擎(t *testing.T) {
	m := newExtTestManager(t)
	eng := &chainEngine{}
	w := &Worker{mgr: m, eng: eng}
	task := newChainTask(t, m, "http://task-proxy:1080")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Run(ctx, task)

	if len(eng.resolveCalls) != 1 || eng.resolveCalls[0] != "http://task-proxy:1080" {
		t.Fatalf("解析应使用任务级代理，实际 %v", eng.resolveCalls)
	}
	if len(eng.downloadCalls) != 1 || eng.downloadCalls[0] != "http://task-proxy:1080" {
		t.Fatalf("下载应使用同一出口（任务级代理），实际 %v", eng.downloadCalls)
	}
}

// Test链路_全局代理兜底 任务没写代理时用全局 net.proxy（任务级为空回落全局）。
func Test链路_全局代理兜底(t *testing.T) {
	m := newExtTestManager(t)
	_ = m.cfg.Set("net.proxy", "http://global-proxy:7890")
	eng := &chainEngine{}
	w := &Worker{mgr: m, eng: eng}
	task := newChainTask(t, m, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Run(ctx, task)

	if len(eng.resolveCalls) != 1 || eng.resolveCalls[0] != "http://global-proxy:7890" {
		t.Fatalf("解析应回落到全局代理，实际 %v", eng.resolveCalls)
	}
	if len(eng.downloadCalls) != 1 || eng.downloadCalls[0] != "http://global-proxy:7890" {
		t.Fatalf("下载应回落到全局代理，实际 %v", eng.downloadCalls)
	}
}

// Test链路_回退直连不清空用户钉住的代理 解析在代理上失败、回退直连成功时，
// 不能把用户指定的代理抹成空串（抹了之后所有重试都只能直连，用户钉的代理形同虚设）。
func Test链路_回退直连不清空用户钉住的代理(t *testing.T) {
	m := newExtTestManager(t)
	eng := &chainEngine{
		resolveErrs: []error{netTimeoutErr("代理不可达")},
	}
	w := &Worker{mgr: m, eng: eng}
	task := newChainTask(t, m, "http://task-proxy:1080")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Run(ctx, task)

	cur := mustTask(t, m, task.ID)
	if cur.Proxy != "http://task-proxy:1080" {
		t.Fatalf("回退直连不应清空用户指定的代理，实际 %q", cur.Proxy)
	}
}

// Test链路_媒体直链落库并点名域名 解析拿到的真实媒体直链（媒体 CDN）要落库，
// 下载失败时的提示要点名那个域名——「解析域名 ≠ 媒体域名」是下载失败的头号原因。
func Test链路_媒体直链落库并点名域名(t *testing.T) {
	m := newExtTestManager(t)
	eng := &chainEngine{
		formats: []engine.FormatInfo{
			{Quality: "1080p", URL: "https://r2---sn-abc.googlevideo.com/videoplayback?expire=1&sig=x", Host: "r2---sn-abc.googlevideo.com"},
			{Quality: "720p", URL: "https://r2---sn-def.googlevideo.com/videoplayback?expire=1", Host: "r2---sn-def.googlevideo.com"},
			{Quality: "audio", URL: "https://rr3---sn-xyz.googlevideo.com/videoplayback?expire=1", Host: "rr3---sn-xyz.googlevideo.com"},
		},
	}
	w := &Worker{mgr: m, eng: eng}
	task := newChainTask(t, m, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Run(ctx, task)

	cur := mustTask(t, m, task.ID)
	if cur.MediaURL == "" {
		t.Fatal("解析成功后应记录媒体直链")
	}
	if !strings.Contains(cur.MediaHost, "googlevideo.com") {
		t.Fatalf("媒体域名应记录 googlevideo.com，实际 %q", cur.MediaHost)
	}
	// best 档：视频流 + 音频流两个域名都要记下来
	if !strings.Contains(cur.MediaHost, "googlevideo.com") || !strings.Contains(cur.MediaHost, ",") {
		t.Fatalf("DASH 站点应记录两个媒体域名，实际 %q", cur.MediaHost)
	}
}

// Test链路_网络失败提示点名媒体域名 下载阶段的网络错误，修复建议里要出现媒体域名。
func Test链路_网络失败提示点名媒体域名(t *testing.T) {
	m := newExtTestManager(t)
	eng := &chainEngine{
		formats: []engine.FormatInfo{
			{Quality: "1080p", URL: "https://r2---sn-abc.googlevideo.com/videoplayback?expire=1", Host: "r2---sn-abc.googlevideo.com"},
		},
	}
	eng.downloadErrs = []error{netTimeoutErr("网络连接失败或被重置")}
	w := &Worker{mgr: m, eng: eng}
	task := newChainTask(t, m, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Run(ctx, task)

	cur := mustTask(t, m, task.ID)
	if !strings.Contains(cur.FixHint, "googlevideo.com") {
		t.Fatalf("网络失败的修复建议应点名媒体域名，实际 %q", cur.FixHint)
	}
}
