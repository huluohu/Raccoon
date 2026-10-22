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
