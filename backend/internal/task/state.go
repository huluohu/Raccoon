// Package task 定义任务模型与七态状态机（docs/05 §2）。state.go 为纯函数，禁止依赖 DB。
package task

import (
	"context"
	"errors"

	"github.com/huluohu/raccoon/internal/engine"
)

// 任务七态（与 tasks.status CHECK 约束同源）。
const (
	StatusWaiting     = "waiting"
	StatusParsing     = "parsing"
	StatusDownloading = "downloading"
	StatusPost        = "post"
	StatusDone        = "done"
	StatusFailed      = "failed"
	StatusCancelled   = "cancelled"
)

// Kind 任务类型。
const (
	KindDownload = "download"
	KindRecord   = "record"
	KindQB       = "qb"
	KindN115     = "n115"
	KindPost     = "postprocess"
)

// Source 提交入口（单一任务系统：所有入口同队列同状态）。
const (
	SourceWeb          = "web"
	SourceTelegram     = "telegram"
	SourceWeChat       = "wechat"
	SourceAPI          = "api"
	SourceCLI          = "cli"
	SourceExtension    = "extension"
	SourceSubscription = "subscription"
	SourceLive         = "live"
	SourceSystem       = "system"
)

// transitions 合法转换表（docs/05 §2）。不在此表中的转换一律拒绝。
var transitions = map[string]map[string]bool{
	StatusWaiting: {
		StatusParsing:   true,
		StatusFailed:    true,
		StatusCancelled: true,
	},
	StatusParsing: {
		StatusDownloading: true,
		StatusPost:        true, // 无下载内容的元信息型任务（如 115 提交后处理）
		StatusFailed:      true,
		StatusCancelled:   true,
	},
	StatusDownloading: {
		StatusPost:      true,
		StatusFailed:    true,
		StatusCancelled: true,
	},
	StatusPost: {
		StatusDone:      true,
		StatusFailed:    true,
		StatusCancelled: true,
	},
	StatusFailed: {
		StatusWaiting:   true, // 重试：attempt+1 回队列
		StatusCancelled: true,
	},
	StatusDone:      {},
	StatusCancelled: {},
}

// CanTransition 校验状态迁移合法性。
func CanTransition(from, to string) bool {
	next, ok := transitions[from]
	if !ok {
		return false // 未知源状态一律拒绝（含终态）
	}
	return next[to]
}

// IsTerminal 终态不可再迁移。
func IsTerminal(s string) bool {
	return s == StatusDone || s == StatusCancelled
}

// IsActive 活跃态（占用并发额度；dedupe 唯一索引同步此集合）。
func IsActive(s string) bool {
	switch s {
	case StatusWaiting, StatusParsing, StatusDownloading, StatusPost:
		return true
	}
	return false
}

// RetryableErrorCodes 可自动重试的错误码（docs/05 §7）；其余直接 failed + fix_hint。
// 全仓库唯一来源：worker.handleEngineErr 与 panic 兜底都以这里为准，勿再复制私有清单。
// ENGINE_ERROR 是分类兜底码、TIMEOUT 是 ctx 超时，两者都可能瞬时性，纳入可重试。
// NET_RESET 已删除：classify 把 connection reset 归入 NET_TIMEOUT，该码从未被产生过。
var RetryableErrorCodes = map[string]bool{
	"NET_TIMEOUT":    true,
	"HTTP_5XX":       true,
	"RATE_LIMITED":   true,
	"ENGINE_CRASH":   true,
	"WORKER_PANIC":   true,
	"ENGINE_ERROR":   true,
	"TIMEOUT":        true,
	"BOT_CHECK":      true, // 平台 IP 人机验证：间歇性，重试大概率过（docs/05 §7）
	"HTTP_FORBIDDEN": true, // 媒体源 403：CDN 拦截常是间歇的，重试可能过；但它不是网络不通，不自动换代理
}

func IsRetryable(code string) bool { return RetryableErrorCodes[code] }

// NetworkErrorCodes 网络类异常错误码集合（超时/重置/平台限流/人机拦截）。
// 遇到此类错误时，系统支持自动切换代理或提示手工切换代理重试。
var NetworkErrorCodes = map[string]bool{
	"NET_TIMEOUT":            true,
	"TIMEOUT":                true,
	"RATE_LIMITED":           true,
	"BOT_CHECK":              true,
	"HTTP_5XX":               true,
	"BLOCKED_BY_FINGERPRINT": true,
	"ENGINE_ERROR":           true,
}

// IsNetworkError 判断是否为网络类异常（超时/重连/限流/人机拦截）。
func IsNetworkError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ee *engine.EngineError
	if errors.As(err, &ee) {
		return NetworkErrorCodes[ee.Code]
	}
	return false
}
