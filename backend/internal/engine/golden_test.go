package engine

// golden_test.go：引擎输出的 Golden Fixture 回放测试。
//
// 为什么要有这层：classify 的输入是「真实引擎的 stderr」——手写的假字符串
// 覆盖不到引擎真实的措辞变化（比如 "This video is unavailable" 与
// "Video unavailable" 之差）。fixture 是从真实 yt-dlp 2026.08.19 录制的，
// 引擎改版导致输出变化时，这里会先红——提醒同步更新分类规则。
//
// 录制方法（复现 fixture）：
//
//	yt-dlp --no-warnings --socket-timeout 20 --dump-single-json "<url>" 2> testdata/xxx.stderr
//
// 断言的是**分类结果**（Code + Detail），不是 stderr 原文——
// 原文会随引擎版本变，分类语义才是我们要守住的不变式。

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// readFixture 读 testdata 下的 fixture 文件。
func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("读取 fixture %s 失败：%v", name, err)
	}
	return string(b)
}

// TestGolden_真实引擎输出分类 回放真实 yt-dlp 录制的错误输出，
// 断言分类结果与引擎原始报错都在位。
func TestGolden_真实引擎输出分类(t *testing.T) {
	cases := []struct {
		fixture   string
		wantCode  string
		wantInMsg string // Detail 里必须出现的关键片段
	}{
		{
			fixture:   "ytdlp_not_found.stderr",
			wantCode:  "NOT_FOUND",
			wantInMsg: "unavailable",
		},
		{
			fixture:   "ytdlp_private.stderr",
			wantCode:  "NOT_FOUND",
			wantInMsg: "unavailable",
		},
		{
			fixture:   "ytdlp_format_unavailable.stderr",
			wantCode:  "FORMAT_UNAVAILABLE",
			wantInMsg: "Requested format is not available",
		},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			out := readFixture(t, tc.fixture)
			err := classify(&ExitError{Code: 1}, out)
			var ee *EngineError
			if !errors.As(err, &ee) {
				t.Fatalf("期望 EngineError，实际 %T", err)
			}
			if ee.Code != tc.wantCode {
				t.Fatalf("错误码应为 %s，实际 %s（message=%s detail=%s）",
					tc.wantCode, ee.Code, ee.Message, ee.Detail)
			}
			if !contains(ee.Detail, tc.wantInMsg) {
				t.Fatalf("Detail 应保留引擎原文关键片段 %q，实际 %q", tc.wantInMsg, ee.Detail)
			}
			if ee.Message == "" {
				t.Fatal("Message 不应为空（前端要显示人话解释）")
			}
		})
	}
}

// TestGolden_真实输出不含退出码噪音 Detail 必须是引擎的原始报错，
// 而不是「引擎退出码 1」这种无信息量的句子。
func TestGolden_真实输出不含退出码噪音(t *testing.T) {
	out := readFixture(t, "ytdlp_not_found.stderr")
	err := classify(&ExitError{Code: 1}, out)
	var ee *EngineError
	if !errors.As(err, &ee) {
		t.Fatalf("期望 EngineError，实际 %T", err)
	}
	if contains(ee.Detail, "退出码") {
		t.Fatalf("Detail 不应是退出码描述：%q", ee.Detail)
	}
	if !contains(ee.Detail, "ERROR") && !contains(ee.Detail, "unavailable") {
		t.Fatalf("Detail 应是引擎原始报错：%q", ee.Detail)
	}
}

// TestGolden_真实输出不触发重试 不可重试的错误码不该进重试循环。
//
// classify 在 engine 包、IsRetryable 在 task 包（避免包依赖倒置），
// 这里用同样的错误码清单断言一次——防止将来把 NOT_FOUND 加进可重试列表。
func TestGolden_真实输出不触发重试(t *testing.T) {
	// 与 task.RetryableErrorCodes 保持一致的快照。
	retriable := map[string]bool{
		"NET_TIMEOUT": true, "HTTP_5XX": true, "RATE_LIMITED": true,
		"ENGINE_CRASH": true, "WORKER_PANIC": true, "ENGINE_ERROR": true,
		"TIMEOUT": true, "BOT_CHECK": true,
	}
	for _, fixture := range []string{
		"ytdlp_not_found.stderr",
		"ytdlp_private.stderr",
		"ytdlp_format_unavailable.stderr",
	} {
		out := readFixture(t, fixture)
		err := classify(&ExitError{Code: 1}, out)
		var ee *EngineError
		if !errors.As(err, &ee) {
			t.Fatalf("期望 EngineError，实际 %T", err)
		}
		if retriable[ee.Code] {
			t.Fatalf("%s 分类为 %s，不应可重试（重试无意义，只会空转）", fixture, ee.Code)
		}
	}
}

// TestGolden_取消与超时分类不受fixture影响 取消/超时是上下文语义，
// 与引擎输出无关——回归保护。
func TestGolden_取消与超时分类不受fixture影响(t *testing.T) {
	err := classify(context.Canceled, readFixture(t, "ytdlp_not_found.stderr"))
	var ee *EngineError
	if !errors.As(err, &ee) {
		t.Fatalf("期望 EngineError，实际 %T", err)
	}
	if ee.Code != "CANCELLED" {
		t.Fatalf("取消应归类为 CANCELLED，实际 %s", ee.Code)
	}
}

// contains 字符串包含（避免为一个 helper 引入 strings 之外的东西）。
func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
