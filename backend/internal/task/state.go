// Package task 定义任务模型与七态状态机（docs/05 §2）。state.go 为纯函数，禁止依赖 DB。
package task

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
var RetryableErrorCodes = map[string]bool{
	"NET_TIMEOUT":  true,
	"NET_RESET":    true,
	"HTTP_5XX":     true,
	"RATE_LIMITED": true,
	"ENGINE_CRASH": true,
	"WORKER_PANIC": true,
}

func IsRetryable(code string) bool { return RetryableErrorCodes[code] }
