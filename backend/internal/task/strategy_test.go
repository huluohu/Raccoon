package task

// strategy_test.go：存储策略路由矩阵（docs/01 §3.4 / docs/15 §3.7）。
//
// 锁定四条不变式：
//  1. 三值策略的 Submit 路由：local/qb_fallback → magnet 走 qB；n115_first → magnet 走 115；
//  2. failover 只对「确定失败」生效，且 attempt 不递增（路由转移不是重试）；
//  3. local 策略下 115 失败按普通失败落库（升级零行为变化）；
//  4. route_path 全程留痕，断点也能讲清「走了哪条路」。

import (
	"context"
	"errors"
	"testing"
)

// ---- 纯函数矩阵 ----

func TestSubmitKindFor_三值策略(t *testing.T) {
	cases := map[string]string{
		StrategyLocal:      KindQB,
		StrategyQBFallback: KindQB,
		StrategyN115First:  KindN115,
		"":                 KindQB, // 空值回落 local
		"bogus":            KindQB, // 非法值回落 local
	}
	for strategy, want := range cases {
		if got := SubmitKindFor(strategy); got != want {
			t.Errorf("SubmitKindFor(%q) = %q，期望 %q", strategy, got, want)
		}
	}
}

func TestInitialRoutePath_按kind与策略(t *testing.T) {
	cases := []struct {
		kind, strategy, want string
	}{
		{KindDownload, StrategyLocal, "本地下载"},
		{KindQB, StrategyLocal, "qB"},
		{KindQB, StrategyQBFallback, "qB"},
		{KindN115, StrategyLocal, "115 离线提交"},
		{KindN115, StrategyN115First, "115 优先 → 115 提交中"},
	}
	for _, c := range cases {
		if got := InitialRoutePath(c.kind, c.strategy); got != c.want {
			t.Errorf("InitialRoutePath(%q,%q) = %q，期望 %q", c.kind, c.strategy, got, c.want)
		}
	}
}

func TestIsDefiniteN115Failure_只有确定失败才转移(t *testing.T) {
	definite := []string{"N115_UNAVAILABLE", "N115_SUBMIT_FAILED", "N115_FAILED"}
	for _, code := range definite {
		if !IsDefiniteN115Failure(code) {
			t.Errorf("%q 应判确定失败（115 侧无远程副作用或已失败，可转 qB）", code)
		}
	}
	// 轮询期查询错误 = 不确定（可能只是网络抖动，先查询不转移，避免两边重复下载）。
	for _, code := range []string{"N115_QUERY_FAILED", "QB_UNAVAILABLE", ""} {
		if IsDefiniteN115Failure(code) {
			t.Errorf("%q 不应判确定失败", code)
		}
	}
}

func TestShouldFailoverToQB_策略门禁(t *testing.T) {
	if !ShouldFailoverToQB(StrategyN115First, "N115_SUBMIT_FAILED") {
		t.Error("n115_first + 确定失败应转 qB")
	}
	if ShouldFailoverToQB(StrategyLocal, "N115_SUBMIT_FAILED") {
		t.Error("local 策略不应转 qB（存量行为零变化）")
	}
	if ShouldFailoverToQB(StrategyQBFallback, "N115_SUBMIT_FAILED") {
		t.Error("qb_fallback 策略的 magnet 本就走 qB，无 115 失败可言")
	}
	if ShouldFailoverToQB(StrategyN115First, "N115_QUERY_FAILED") {
		t.Error("不确定失败不转移（提交-查询闭环）")
	}
}

func TestAppendRoutePath(t *testing.T) {
	if got := AppendRoutePath("", "qB"); got != "qB" {
		t.Errorf("空链路追补应得段本身，实际 %q", got)
	}
	if got := AppendRoutePath("115 优先 → 115 提交中", "115 失败 → 转 qB"); got != "115 优先 → 115 提交中 → 115 失败 → 转 qB" {
		t.Errorf("链路追补不符，实际 %q", got)
	}
}

// ---- Submit 路由集成 ----

func TestSubmit_磁力按策略路由(t *testing.T) {
	magnet := "magnet:?xt=urn:btih:aabbccddeeff00112233445566778899aabbccdd"
	cases := []struct {
		strategy  string
		wantKind  string
		wantRoute string
	}{
		{StrategyLocal, KindQB, "qB"},
		{StrategyQBFallback, KindQB, "qB"},
		{StrategyN115First, KindN115, "115 优先 → 115 提交中"},
	}
	for _, c := range cases {
		m := newExtTestManager(t)
		_ = m.cfg.Set("storage.strategy", c.strategy)
		results, err := m.Submit([]SubmitItem{{URL: magnet}}, SourceWeb, "")
		if err != nil {
			t.Fatalf("提交失败：%v", err)
		}
		if len(results) != 1 || results[0].Task == nil {
			t.Fatalf("应创建 1 条任务，实际 %+v", results)
		}
		got := results[0].Task
		if got.Kind != c.wantKind {
			t.Errorf("策略 %s：kind=%q，期望 %q", c.strategy, got.Kind, c.wantKind)
		}
		if got.RoutePath != c.wantRoute {
			t.Errorf("策略 %s：route_path=%q，期望 %q", c.strategy, got.RoutePath, c.wantRoute)
		}
	}
}

func TestSubmit_直链不受策略影响(t *testing.T) {
	m := newExtTestManager(t)
	_ = m.cfg.Set("storage.strategy", StrategyN115First)
	results, err := m.Submit([]SubmitItem{{URL: "https://www.bilibili.com/video/BV1xx411c7mD"}}, SourceWeb, "")
	if err != nil {
		t.Fatalf("提交失败：%v", err)
	}
	got := results[0].Task
	if got.Kind != KindDownload {
		t.Errorf("普通直链应永远本地下载，实际 kind=%q", got.Kind)
	}
	if got.RoutePath != "本地下载" {
		t.Errorf("直链 route_path 应为「本地下载」，实际 %q", got.RoutePath)
	}
}

// ---- failover 集成 ----

// TestFailover_n115优先确定失败自动转qB 核心不变式：kind 改写 + 回 waiting +
// route_path 留痕 + attempt 不递增。
func TestFailover_n115优先确定失败自动转qB(t *testing.T) {
	m := newExtTestManager(t)
	_ = m.cfg.Set("storage.strategy", StrategyN115First)
	task := newExtTask(t, m, KindN115, "magnet:?xt=urn:btih:aabbccddeeff00112233445566778899aabbccdd")
	task.RoutePath = InitialRoutePath(KindN115, StrategyN115First)
	n115 := &fakeN115{subErr: errors.New("115 拒绝提交：token 无效")}
	e := fastExec(m, &fakeQB{}, n115)

	e.Run(context.Background(), task)

	got, err := m.st.GetTask(task.ID)
	if err != nil {
		t.Fatalf("查任务失败：%v", err)
	}
	if got.Kind != KindQB {
		t.Errorf("应转 qB，实际 kind=%q", got.Kind)
	}
	if got.Status != StatusWaiting {
		t.Errorf("应回 waiting 重新入队，实际 %s", got.Status)
	}
	if got.Attempt != 0 {
		t.Errorf("路由转移不是重试，attempt 应保持 0，实际 %d", got.Attempt)
	}
	wantRoute := "115 优先 → 115 提交中 → 115 失败 → 转 qB"
	if got.RoutePath != wantRoute {
		t.Errorf("route_path=%q，期望 %q", got.RoutePath, wantRoute)
	}
}

// TestFailover_云端失败也转移 115 云端报告失败 = 确定失败（115 侧任务已失败，
// 不存在两边重复下载），n115_first 下同样转 qB。
func TestFailover_云端失败也转移(t *testing.T) {
	m := newExtTestManager(t)
	_ = m.cfg.Set("storage.strategy", StrategyN115First)
	task := newExtTask(t, m, KindN115, "magnet:?xt=urn:btih:aabbccddeeff00112233445566778899aabbccdd")
	task.RoutePath = InitialRoutePath(KindN115, StrategyN115First)
	n115 := &fakeN115{task: &N115Task{OfflineID: "off-1", Status: "failed"}}
	e := fastExec(m, &fakeQB{}, n115)

	e.Run(context.Background(), task)

	got, _ := m.st.GetTask(task.ID)
	if got.Kind != KindQB || got.Status != StatusWaiting {
		t.Errorf("云端失败应转 qB 重新入队，实际 kind=%q status=%s", got.Kind, got.Status)
	}
}

// TestFailover_local策略不转移 local（默认）下 115 失败按普通失败落库，
// 保证存量用户升级后行为零变化。
func TestFailover_local策略不转移(t *testing.T) {
	m := newExtTestManager(t)
	// 默认策略即 local，无需 Set。
	task := newExtTask(t, m, KindN115, "https://115.com/xxx")
	n115 := &fakeN115{subErr: errors.New("115 拒绝提交")}
	e := fastExec(m, &fakeQB{}, n115)

	e.Run(context.Background(), task)

	got, _ := m.st.GetTask(task.ID)
	if got.Status != StatusFailed {
		t.Errorf("local 策略下应普通失败，实际 %s", got.Status)
	}
	if got.ErrorCode != "N115_SUBMIT_FAILED" {
		t.Errorf("错误码应透传，实际 %q", got.ErrorCode)
	}
	if got.Kind != KindN115 {
		t.Errorf("kind 不应被改写，实际 %q", got.Kind)
	}
}

// TestFailover_不确定失败先查询不转移 轮询期查询错误重试到 max_attempts 才判失败，
// 全程不转 qB（提交-查询闭环，避免两边重复下载）。
func TestFailover_不确定失败先查询不转移(t *testing.T) {
	m := newExtTestManager(t)
	_ = m.cfg.Set("storage.strategy", StrategyN115First)
	task := newExtTask(t, m, KindN115, "magnet:?xt=urn:btih:aabbccddeeff00112233445566778899aabbccdd")
	task.RoutePath = InitialRoutePath(KindN115, StrategyN115First)
	n115 := &fakeN115{getErr: errors.New("查询超时")}
	e := fastExec(m, &fakeQB{}, n115)

	e.Run(context.Background(), task)

	got, _ := m.st.GetTask(task.ID)
	if got.Status != StatusFailed || got.ErrorCode != "N115_QUERY_FAILED" {
		t.Errorf("不确定失败应判 N115_QUERY_FAILED，实际 %s/%s", got.Status, got.ErrorCode)
	}
	if got.Kind != KindN115 {
		t.Errorf("不确定失败不转移，kind 应保持 n115，实际 %q", got.Kind)
	}
}

// TestFailover_转移后qB也不可用则失败并保留全链路 策略链路全失败 →
// 任务 failed 且 route_path 不断点（docs/01 §3.4「保留上下文」）。
func TestFailover_转移后qB也不可用则失败并保留全链路(t *testing.T) {
	m := newExtTestManager(t)
	_ = m.cfg.Set("storage.strategy", StrategyN115First)
	task := newExtTask(t, m, KindN115, "magnet:?xt=urn:btih:aabbccddeeff00112233445566778899aabbccdd")
	task.RoutePath = InitialRoutePath(KindN115, StrategyN115First)
	n115 := &fakeN115{subErr: errors.New("115 拒绝提交")}
	// 第一遍：n115 失败 → 转 qB（qb 客户端存在但 nil 时才不可用，这里先给真 client 让转移发生）。
	e := fastExec(m, &fakeQB{}, n115)
	e.Run(context.Background(), task)

	// 第二遍：模拟调度循环重新领取，qB 客户端缺失（未配置）。
	claimed, err := m.st.ClaimWaiting(task.ID, "parsing")
	if err != nil {
		t.Fatalf("转移后应可被重新领取：%v", err)
	}
	if claimed.Kind != KindQB {
		t.Fatalf("重新领取的任务 kind 应为 qb，实际 %q", claimed.Kind)
	}
	e2 := fastExec(m, nil, n115) // qb = nil → 不可用
	e2.Run(context.Background(), claimed)

	got, _ := m.st.GetTask(task.ID)
	if got.Status != StatusFailed {
		t.Errorf("链路全失败应判 failed，实际 %s", got.Status)
	}
	wantRoute := "115 优先 → 115 提交中 → 115 失败 → 转 qB → qB 不可用"
	if got.RoutePath != wantRoute {
		t.Errorf("route_path 应保留全链路：%q，期望 %q", got.RoutePath, wantRoute)
	}
}

// TestRoutePath_115提交成功留痕 runN115 提交成功后追补「115 提交成功」。
func TestRoutePath_115提交成功留痕(t *testing.T) {
	m := newExtTestManager(t)
	_ = m.cfg.Set("storage.strategy", StrategyN115First)
	task := newExtTask(t, m, KindN115, "magnet:?xt=urn:btih:aabbccddeeff00112233445566778899aabbccdd")
	task.RoutePath = InitialRoutePath(KindN115, StrategyN115First)
	n115 := &fakeN115{task: &N115Task{OfflineID: "off-1", Status: "done", Files: []N115File{{FileID: "f1", Size: 100}}}}
	e := fastExec(m, &fakeQB{}, n115)

	e.Run(context.Background(), task)

	got, _ := m.st.GetTask(task.ID)
	if got.RoutePath != "115 优先 → 115 提交中 → 115 提交成功" {
		t.Errorf("route_path=%q，期望含「115 提交成功」", got.RoutePath)
	}
	if got.Status != StatusDone {
		t.Errorf("115 成功应到 done，实际 %s", got.Status)
	}
}

// TestNewExtTask_默认无RoutePath 旧构造路径不带 route_path 时任务仍可执行
// （列有 DEFAULT ”，老数据与测试构造不受影响）。
func TestNewExtTask_默认无RoutePath(t *testing.T) {
	m := newExtTestManager(t)
	task := newExtTask(t, m, KindQB, "magnet:?xt=urn:btih:aabbccddeeff00112233445566778899aabbccdd")
	if task.RoutePath != "" {
		t.Errorf("直接构造的任务 route_path 应为空，实际 %q", task.RoutePath)
	}
	// Submit 创建的任务必须带初始链路。
	res, err := m.Submit([]SubmitItem{{URL: task.URL}}, SourceWeb, "")
	if err != nil {
		t.Fatalf("提交失败：%v", err)
	}
	if res[0].Task.RoutePath == "" {
		t.Error("Submit 创建的任务必须带初始 route_path")
	}
}
