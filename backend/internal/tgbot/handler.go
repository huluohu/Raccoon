package tgbot

// handler.go：对外的回调接口（Bot 与业务的接缝）。
//
// 设计意图：Bot 只做「协议 + 命令解析 + 鉴权 + 分发」，所有业务语义
// （提交下载任务、查任务状态、查媒体库）都通过下面的接口交给上层实现。
// 上层接统一的 task.Manager，于是 Bot 与 Web 共享同一队列、同一状态机、
// 同一份任务真相——Bot 不自己下载、不自己建任务。

import "context"

// Handler 上层注入的命令处理器。
//
// 每个方法都返回要回给用户的文本（空串表示不回复）。返回 error 时，
// Bot 会把错误摘要回给用户并记日志。
//
// 线程安全由实现方负责：Bot 的轮询循环是单 goroutine，但上层可能在
// 其它 goroutine 里并发调用。
type Handler interface {
	// OnStart /start：用户开始使用。
	//
	// 返回 error 表示欢迎流程失败（例如创建用户失败）。
	OnStart(ctx context.Context, chatID int64, user *User) error
	// OnAdd /add <url>：提交下载任务。
	//
	// url 是用户给出的下载链接。实现方应把它提交给 task.Manager 并返回
	// 「已提交，任务 ID 是 xxx」这类回执——Bot 不负责下载本身。
	//
	// 这一条是 Bot 的核心价值：Bot 与 Web 走同一条队列，不存在两套状态。
	OnAdd(ctx context.Context, chatID int64, user *User, url string) (string, error)
	// OnAddBatch 批量提交（裸链接消息或 /add 多链接）。
	//
	// urls 已提取、去重。实现方逐条提交并汇总回执
	//（单条失败不影响其余——与 Web 批量提交同一语义）。
	OnAddBatch(ctx context.Context, chatID int64, user *User, urls []string) (string, error)
	// OnStatus /status [taskID]：查询任务状态。
	//
	// taskID 为空表示「概览我的全部任务」，非空表示查单个任务。
	OnStatus(ctx context.Context, chatID int64, user *User, taskID string) (string, error)
	// OnList /list：列出任务。
	OnList(ctx context.Context, chatID int64, user *User) (string, error)
	// OnCancel /cancel <taskID>：取消任务。
	OnCancel(ctx context.Context, chatID int64, user *User, taskID string) (string, error)
	// OnMedia /media <query>：检索媒体库。
	OnMedia(ctx context.Context, chatID int64, user *User, query string) (string, error)
}

// Notifier 任务完成/失败的推送出口。
//
// 抽成接口而不是直接调 task.Manager：Bot 不该知道任务系统长什么样，
// 上层想在推送前多写一条通知中心记录、多推一个渠道，都在自己的实现里做。
//
// 谁来用：上层持有 task.Manager，任务状态变化时调用 Notifier 把结果推回
// 用户所在的聊天窗口。chatID 由上层自己记录（/add 时就知道是哪个会话
// 提交的），本包不维护「任务 → 会话」的映射——那是任务系统的职责。
//
// 内置实现 MessageNotifier 直接把结果发到 Telegram 聊天窗口；
// 上层也可以自己实现（例如同时写通知中心 + 推 Telegram）。
type Notifier interface {
	// NotifyTaskDone 任务完成时推送。
	//
	// title 是任务标题，body 是补充说明（如文件大小、保存路径）。
	NotifyTaskDone(ctx context.Context, chatID int64, taskID, title, body string) error
	// NotifyTaskFailed 任务失败时推送。
	NotifyTaskFailed(ctx context.Context, chatID int64, taskID, title, body string) error
}
