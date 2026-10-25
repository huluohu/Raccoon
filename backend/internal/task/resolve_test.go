package task

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/huluohu/raccoon/internal/engine"
)

// proxyChainEngine 记录每次 Resolve 的代理参数，按脚本返回错误。
type proxyChainEngine struct {
	fakeWorkerEngine
	calls   []string
	errs    []error
	results []*engine.ResolveResult
}

func (e *proxyChainEngine) Resolve(ctx context.Context, url string, opts engine.ResolveOpts) (*engine.ResolveResult, error) {
	idx := len(e.calls)
	e.calls = append(e.calls, opts.Proxy)
	if idx < len(e.errs) && e.errs[idx] != nil {
		return nil, e.errs[idx]
	}
	if idx < len(e.results) {
		return e.results[idx], nil
	}
	return &engine.ResolveResult{Title: "ok", Platform: "test"}, nil
}

func netErr(code string) error {
	return &engine.EngineError{Code: code, Message: "网络异常"}
}

// TestResolveAttempts_出口优先级 显式 > 任务级 > 全局 > 直连。
func TestResolveAttempts_出口优先级(t *testing.T) {
	cases := []struct {
		name         string
		explicit     string
		taskProxy    string
		globalProxy  string
		wantPrimary  string
		wantFallback string
	}{
		{"全都没配 → 直连无备用", "", "", "", "", ""},
		{"只有全局代理 → 代理 + 直连备用", "", "", "http://g:1", "http://g:1", ""},
		{"任务级覆盖全局", "", "http://t:1", "http://g:1", "http://t:1", ""},
		{"显式覆盖任务级", "http://e:1", "http://t:1", "http://g:1", "http://e:1", ""},
		{"空白视为未配", "  ", "  ", "  ", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, f := resolveAttempts(tc.explicit, tc.taskProxy, tc.globalProxy)
			if p != tc.wantPrimary || f != tc.wantFallback {
				t.Fatalf("resolveAttempts(%q,%q,%q) = (%q,%q)，期望 (%q,%q)",
					tc.explicit, tc.taskProxy, tc.globalProxy, p, f, tc.wantPrimary, tc.wantFallback)
			}
		})
	}
}

// TestResolveWithProxy_主出口成功不切换 主出口可用时不该有多余尝试。
func TestResolveWithProxy_主出口成功不切换(t *testing.T) {
	m := newExtTestManager(t)
	eng := &proxyChainEngine{}
	res, used, note, err := m.resolveWithProxy(context.Background(), eng, "https://x/1", "", "http://p:1", "", 0)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if res == nil || res.Title != "ok" {
		t.Fatalf("应返回解析结果，实际 %+v", res)
	}
	if used != "http://p:1" {
		t.Fatalf("应记录主出口，实际 %q", used)
	}
	if note != "" {
		t.Fatalf("未切换出口时不应有切换说明，实际 %q", note)
	}
	if len(eng.calls) != 1 {
		t.Fatalf("应只尝试一次，实际 %d 次: %v", len(eng.calls), eng.calls)
	}
}

// TestResolveWithProxy_网络异常切备用出口 代理挂了要回退直连。
func TestResolveWithProxy_网络异常切备用出口(t *testing.T) {
	m := newExtTestManager(t)
	eng := &proxyChainEngine{
		errs: []error{netErr("NET_TIMEOUT"), nil},
	}
	_, used, note, err := m.resolveWithProxy(context.Background(), eng, "https://x/1", "", "http://p:1", "", 0)
	if err != nil {
		t.Fatalf("备用出口应成功: %v", err)
	}
	if used != "" {
		t.Fatalf("切换后应记录直连出口，实际 %q", used)
	}
	if note == "" {
		t.Fatal("切换出口必须给出说明（界面要如实告知用户）")
	}
	if len(eng.calls) != 2 {
		t.Fatalf("应尝试两次（主 + 备用），实际 %d 次: %v", len(eng.calls), eng.calls)
	}
	if eng.calls[0] != "http://p:1" || eng.calls[1] != "" {
		t.Fatalf("尝试顺序应为 代理→直连，实际 %v", eng.calls)
	}
}

// TestResolveWithProxy_平台类错误不换出口 404/需登录换出口也没用。
func TestResolveWithProxy_平台类错误不换出口(t *testing.T) {
	m := newExtTestManager(t)
	eng := &proxyChainEngine{
		errs: []error{&engine.EngineError{Code: "NOT_FOUND", Message: "视频不存在"}},
	}
	_, _, _, err := m.resolveWithProxy(context.Background(), eng, "https://x/1", "", "http://p:1", "", 0)
	if err == nil {
		t.Fatal("应返回错误")
	}
	if len(eng.calls) != 1 {
		t.Fatalf("平台类错误不应换出口重试，实际尝试 %d 次", len(eng.calls))
	}
}

// TestResolveWithProxy_重试用全新ctx 这是「能下载却解析失败」的根因：
// 主出口把父 ctx 的 deadline 耗尽后，第二次尝试若复用同一个 ctx 会瞬间失败。
func TestResolveWithProxy_重试用全新ctx(t *testing.T) {
	m := newExtTestManager(t)
	// 第一次尝试阻塞到自己的时限耗尽，第二次必须仍有可用时间。
	eng := &blockingThenOKEngine{}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	var used string
	var err error
	go func() {
		defer close(done)
		_, used, _, err = m.resolveWithProxy(ctx, eng, "https://x/1", "", "http://p:1", "", 200*time.Millisecond)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("resolveWithProxy 未能结束")
	}
	if err != nil {
		t.Fatalf("第二次尝试应成功（全新 ctx 有独立时限），实际报错: %v", err)
	}
	if used != "" {
		t.Fatalf("第二次应走直连出口，实际 %q", used)
	}
}

// blockingThenOKEngine 第一次 Resolve 睡到自己的 ctx 结束，第二次直接成功。
type blockingThenOKEngine struct {
	fakeWorkerEngine
	calls int
}

func (e *blockingThenOKEngine) Resolve(ctx context.Context, url string, opts engine.ResolveOpts) (*engine.ResolveResult, error) {
	e.calls++
	if e.calls == 1 {
		<-ctx.Done()
		return nil, &engine.EngineError{Code: "TIMEOUT", Message: "超时"}
	}
	// 第二次：确认 ctx 仍然有效（父 ctx 还有余量）
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &engine.ResolveResult{Title: "ok"}, nil
}

// TestResolveWithProxy_总错误透传 两次都失败时返回最后一次的真实原因。
func TestResolveWithProxy_总错误透传(t *testing.T) {
	m := newExtTestManager(t)
	eng := &proxyChainEngine{
		errs: []error{netErr("NET_TIMEOUT"), netErr("HTTP_5XX")},
	}
	_, _, _, err := m.resolveWithProxy(context.Background(), eng, "https://x/1", "", "http://p:1", "", 0)
	if err == nil {
		t.Fatal("应返回错误")
	}
	var ee *engine.EngineError
	if !errors.As(err, &ee) || ee.Code != "HTTP_5XX" {
		t.Fatalf("应透传最后一次的真实原因，实际 %v", err)
	}
}
