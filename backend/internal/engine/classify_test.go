package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// classify 的错误归类：真实线上故障（yt-dlp 2025.01.15 撞上 YouTube 改版）
// 全部落在 ENGINE_ERROR 里，前端只看到「引擎退出码 1」，无从判断原因。
// 这里按实际出现过的 stderr 文本逐条锁死归类结果。

func TestClassify_引擎版本过旧归类为ENGINE_OUTDATED(t *testing.T) {
	// 2026-10 实测：yt-dlp 2025.01.15 解析任何 YouTube 链接都报这句。
	out := "ERROR: [youtube] dQw4w9WgXcQ: The page needs to be reloaded.\n"
	err := classify(&ExitError{Code: 1}, out)
	var ee *EngineError
	if !errors.As(err, &ee) {
		t.Fatalf("期望 EngineError，实际 %T", err)
	}
	if ee.Code != "ENGINE_OUTDATED" {
		t.Fatalf("错误码应为 ENGINE_OUTDATED，实际 %s（message=%s）", ee.Code, ee.Message)
	}
	if !strings.Contains(ee.Message, "版本过旧") {
		t.Fatalf("提示文案应说明引擎版本过旧，实际 %s", ee.Message)
	}
}

func TestClassify_视频不可用归类为NOT_FOUND(t *testing.T) {
	// "This video is unavailable" 里有 "video is unavailable"，
	// 原实现只匹配 "video unavailable"（无 is），导致漏判成 ENGINE_ERROR。
	for _, out := range []string{
		"ERROR: [youtube] 5Qm0GPaOWM8: This video is unavailable\n",
		"ERROR: [youtube] abc: Video unavailable\n",
		"ERROR: This video has been removed by the uploader\n",
	} {
		err := classify(&ExitError{Code: 1}, out)
		var ee *EngineError
		if !errors.As(err, &ee) {
			t.Fatalf("期望 EngineError，实际 %T", err)
		}
		if ee.Code != "NOT_FOUND" {
			t.Fatalf("输出 %q 的错误码应为 NOT_FOUND，实际 %s", out, ee.Code)
		}
	}
}

func TestClassify_保留引擎原始报错到Detail(t *testing.T) {
	out := "WARNING: [youtube] falling back\nERROR: [youtube] abc: This video is unavailable\n"
	err := classify(&ExitError{Code: 1}, out)
	var ee *EngineError
	if !errors.As(err, &ee) {
		t.Fatalf("期望 EngineError，实际 %T", err)
	}
	// Detail 必须是 ERROR 行本身（去掉前缀），而不是那句无用的「引擎退出码 1」。
	if !strings.Contains(ee.Detail, "This video is unavailable") {
		t.Fatalf("Detail 应含引擎原始报错，实际 %q", ee.Detail)
	}
	if strings.Contains(ee.Detail, "WARNING") {
		t.Fatalf("Detail 不应取 WARNING 行，实际 %q", ee.Detail)
	}
}

func TestClassify_无ERROR行时取最后非空行(t *testing.T) {
	out := "some log line\nanother line\n"
	err := classify(&ExitError{Code: 2}, out)
	var ee *EngineError
	if !errors.As(err, &ee) {
		t.Fatalf("期望 EngineError，实际 %T", err)
	}
	if ee.Detail != "another line" {
		t.Fatalf("Detail 应为最后一条非空行，实际 %q", ee.Detail)
	}
}

func TestClassify_取消不归类为引擎错误(t *testing.T) {
	err := classify(context.Canceled, "")
	var ee *EngineError
	if !errors.As(err, &ee) {
		t.Fatalf("期望 EngineError，实际 %T", err)
	}
	if ee.Code != "CANCELLED" {
		t.Fatalf("错误码应为 CANCELLED，实际 %s", ee.Code)
	}
}

func TestClassify_超时与取消分开报(t *testing.T) {
	// 超时说明这次解析太慢/链接太重，报「已取消」会让人误以为是自己点的。
	err := classify(context.DeadlineExceeded, "")
	var ee *EngineError
	if !errors.As(err, &ee) {
		t.Fatalf("期望 EngineError，实际 %T", err)
	}
	if ee.Code != "TIMEOUT" {
		t.Fatalf("错误码应为 TIMEOUT，实际 %s", ee.Code)
	}
	if !strings.Contains(ee.Message, "超时") {
		t.Fatalf("提示应说明超时，实际 %s", ee.Message)
	}
}

func TestEngineDetail_超长截断(t *testing.T) {
	long := strings.Repeat("x", 500)
	got := engineDetail("ERROR: " + long)
	if len([]rune(got)) > 201 {
		t.Fatalf("Detail 应截断到 200 字 + 省略号，实际 %d 字", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("截断后应以省略号结尾，实际 %q", got)
	}
}

func TestClassify_平台人机验证归类为BOT_CHECK(t *testing.T) {
	// 2026-10-24 用户实测：容器出口 IP 被 YouTube 间歇性要求
	// "Sign in to confirm you're not a bot"。裸匹配 "log in" 曾把它归为
	// AUTH_REQUIRED（不可自动重试 + 建议「去连接页更新凭据」），
	// 用户读作「必须登录才能下载」——实际是 IP 人机验证，重试大概率过。
	for _, out := range []string{
		"ERROR: [youtube] dQw4w9WgXcQ: Sign in to confirm you're not a bot. This helps protect our community. Learn more\n",
		"ERROR: [youtube] abc: Sign in to confirm you are not a bot\n",
	} {
		err := classify(&ExitError{Code: 1}, out)
		var ee *EngineError
		if !errors.As(err, &ee) {
			t.Fatalf("期望 EngineError，实际 %T", err)
		}
		if ee.Code != "BOT_CHECK" {
			t.Fatalf("输出 %q 的错误码应为 BOT_CHECK，实际 %s", out, ee.Code)
		}
		if strings.Contains(ee.Message, "登录") || strings.Contains(ee.Message, "凭据") {
			t.Fatalf("BOT_CHECK 文案不应引导用户去登录/配凭据: %s", ee.Message)
		}
	}
}

func TestClassify_真账号问题仍归AUTH_REQUIRED(t *testing.T) {
	// bot check 截走后，真正的账号类错误（年龄验证/members-only/Cookie 失效）
	// 仍应归 AUTH_REQUIRED。
	for _, out := range []string{
		"ERROR: [youtube] abc: Sign in to confirm your age. This video may be inappropriate for some users.\n",
		"ERROR: [youtube] abc: Private video. Sign in if you've been granted access\n",
		"ERROR: [bilibili] abc: account is required, use --cookies\n",
	} {
		err := classify(&ExitError{Code: 1}, out)
		var ee *EngineError
		if !errors.As(err, &ee) {
			t.Fatalf("期望 EngineError，实际 %T", err)
		}
		if ee.Code != "AUTH_REQUIRED" {
			t.Fatalf("输出 %q 的错误码应为 AUTH_REQUIRED，实际 %s", out, ee.Code)
		}
	}
}

func TestParseItemOf(t *testing.T) {
	cases := []struct {
		line         string
		index, total int
		ok           bool
	}{
		{"[download] Downloading item 3 of 25", 3, 25, true},
		{"[download] Downloading video 3 of 25", 3, 25, true},
		{"[download] Downloading item 1 of 1", 1, 1, true},
		{"[download] Destination: x.mp4", 0, 0, false},
		{"[download] Downloading item 9 of 3", 0, 0, false}, // total < index 视为坏行
		{"[download] Downloading item abc of 25", 0, 0, false},
		{"PROGRESS|12|1|2|3|4", 0, 0, false},
	}
	for _, tc := range cases {
		i, n, ok := parseItemOf(tc.line)
		if ok != tc.ok || (ok && (i != tc.index || n != tc.total)) {
			t.Fatalf("parseItemOf(%q) = (%d,%d,%v)，期望 (%d,%d,%v)", tc.line, i, n, ok, tc.index, tc.total, tc.ok)
		}
	}
}

// TestClassify_媒体源403不当成网络超时 403 是「媒体源拒绝了请求」，
// 不是「网络不通」——早先它被 "unable to download" 兜底分支吞成了 NET_TIMEOUT，
// 于是用户被告知「网络失败、去换代理」，而真实原因是媒体 CDN 拒绝
// （Referer/指纹/区域限制）。2026-10-28 定位。
func TestClassify_媒体源403不当成网络超时(t *testing.T) {
	out := "ERROR: unable to download video data: HTTP Error 403: Forbidden"
	err := classify(&ExitError{Code: 1}, out)
	ue, ok := err.(*EngineError)
	if !ok {
		t.Fatalf("应返回 EngineError，实际 %T", err)
	}
	if ue.Code != "HTTP_FORBIDDEN" {
		t.Fatalf("403 应归类为 HTTP_FORBIDDEN，实际 %q（%s）", ue.Code, ue.Message)
	}
	if ue.Message == "网络连接失败或被重置" {
		t.Fatalf("403 的提示不应说成网络失败：%q", ue.Message)
	}
}

// TestClassify_5xx优先于unabletodownload兜底 "unable to download video data:
// HTTP Error 502" 是服务端错误，不是网络不通。
func TestClassify_5xx优先于unabletodownload兜底(t *testing.T) {
	out := "ERROR: unable to download video data: HTTP Error 502: Bad Gateway"
	err := classify(&ExitError{Code: 1}, out)
	ue, ok := err.(*EngineError)
	if !ok {
		t.Fatalf("应返回 EngineError，实际 %T", err)
	}
	if ue.Code != "HTTP_5XX" {
		t.Fatalf("502 应归类为 HTTP_5XX，实际 %q", ue.Code)
	}
}
