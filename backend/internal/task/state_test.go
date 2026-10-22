package task

// state_test.go：七态状态机全量转换矩阵（docs/05 §2）。
//
// 锁死不变量：合法转换一个不能少（少了任务会卡死），非法转换一个不能多
// （多了会出现 done→parsing 这类回跳，重试机制被绕过）。

import "testing"

// TestCanTransition_合法转换全放行 逐条走 transitions 表，防漏配。
func TestCanTransition_合法转换全放行(t *testing.T) {
	legal := [][2]string{
		{StatusWaiting, StatusParsing},
		{StatusWaiting, StatusFailed},
		{StatusWaiting, StatusCancelled},
		{StatusParsing, StatusDownloading},
		{StatusParsing, StatusPost},
		{StatusParsing, StatusFailed},
		{StatusParsing, StatusCancelled},
		{StatusDownloading, StatusPost},
		{StatusDownloading, StatusFailed},
		{StatusDownloading, StatusCancelled},
		{StatusPost, StatusDone},
		{StatusPost, StatusFailed},
		{StatusPost, StatusCancelled},
		{StatusFailed, StatusWaiting}, // 重试回队列
		{StatusFailed, StatusCancelled},
	}
	for _, c := range legal {
		if !CanTransition(c[0], c[1]) {
			t.Errorf("合法转换 %s→%s 被拒绝（ transitions 表漏配）", c[0], c[1])
		}
	}
}

// TestCanTransition_非法转换全拒绝 含状态回跳、跨级跳跃、未知状态。
func TestCanTransition_非法转换全拒绝(t *testing.T) {
	illegal := [][2]string{
		// 终态不可再迁移（状态回跳是重试机制的头号漏洞）。
		{StatusDone, StatusParsing},
		{StatusDone, StatusDownloading},
		{StatusDone, StatusWaiting},
		{StatusDone, StatusFailed},
		{StatusCancelled, StatusWaiting},
		{StatusCancelled, StatusParsing},
		// 跨级跳跃：必须逐级过，否则进度事件顺序错乱。
		{StatusWaiting, StatusDownloading},
		{StatusWaiting, StatusPost},
		{StatusWaiting, StatusDone},
		{StatusParsing, StatusDone},
		{StatusDownloading, StatusDone},
		{StatusDownloading, StatusWaiting},
		{StatusPost, StatusWaiting},
		{StatusPost, StatusParsing},
		{StatusPost, StatusDownloading},
		// failed 只能回 waiting 重试或取消，不能原地复活成中间态。
		{StatusFailed, StatusParsing},
		{StatusFailed, StatusDownloading},
		{StatusFailed, StatusPost},
		{StatusFailed, StatusDone},
		// 自转移与未知状态。
		{StatusWaiting, StatusWaiting},
		{StatusParsing, StatusParsing},
		{"", StatusParsing},
		{"unknown", StatusDone},
		{StatusDownloading, ""},
	}
	for _, c := range illegal {
		if CanTransition(c[0], c[1]) {
			t.Errorf("非法转换 %q→%q 被放行", c[0], c[1])
		}
	}
}

// TestIsTerminal 终态判定（dedupe 唯一索引与恢复扫描都依赖它）。
func TestIsTerminal(t *testing.T) {
	if !IsTerminal(StatusDone) || !IsTerminal(StatusCancelled) {
		t.Error("done/cancelled 应为终态")
	}
	for _, s := range []string{StatusWaiting, StatusParsing, StatusDownloading, StatusPost, StatusFailed} {
		if IsTerminal(s) {
			t.Errorf("%s 不应是终态（failed 可重试回 waiting）", s)
		}
	}
}

// TestIsActive 活跃态集合（占并发额度；软删过滤依赖它）。
func TestIsActive(t *testing.T) {
	for _, s := range []string{StatusWaiting, StatusParsing, StatusDownloading, StatusPost} {
		if !IsActive(s) {
			t.Errorf("%s 应为活跃态", s)
		}
	}
	for _, s := range []string{StatusDone, StatusFailed, StatusCancelled, "bogus"} {
		if IsActive(s) {
			t.Errorf("%s 不应是活跃态", s)
		}
	}
}

// TestIsRetryable 可重试错误码白名单（docs/05 §7）。
func TestIsRetryable(t *testing.T) {
	for _, c := range []string{"NET_TIMEOUT", "NET_RESET", "HTTP_5XX", "RATE_LIMITED", "ENGINE_CRASH", "WORKER_PANIC"} {
		if !IsRetryable(c) {
			t.Errorf("%s 应可自动重试", c)
		}
	}
	for _, c := range []string{"AUTH_REQUIRED", "GEO_BLOCKED", "QB_UNAVAILABLE", "N115_SUBMIT_FAILED", ""} {
		if IsRetryable(c) {
			t.Errorf("%s 不应自动重试（重试无意义，只会拖延用户看到 fix_hint）", c)
		}
	}
}
