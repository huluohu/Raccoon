// Package tgbot 是 Telegram Bot 的长轮询客户端与命令分发器。
//
// 职责边界（重要，勿越界）：
//   - 本包只做「协议适配 + 入口管控」：把 Telegram Bot API 包成 Go 方法，
//     解析 update、解析命令、做用户白名单鉴权与入站去重。
//   - 本包不做业务：下载、入库、任务编排、媒体检索都不在这里。
//     `/add` 只产出「提交下载任务」的意图，通过注入的 Handler 交给上层，
//     上层接统一的 task.Manager，从而保证 Bot 与 Web 同队列、同状态、
//     同一套七态状态机（docs/05 §2）。
//   - 零第三方依赖：只用标准库 net/http + encoding/json。
//
// 安全约定：
//   - Bot Token 只出现在请求 URL 里，绝不进入日志与错误消息（错误消息里的
//     接口路径统一打码成 bot***）。
//   - 默认拒绝所有不在白名单内的用户（fail-closed）：Bot 是对外暴露的入口，
//     空名单等于把「谁能下任务」交给陌生人。
package tgbot

import (
	"fmt"
)

// ---- 接口方法名 ----
//
// Telegram Bot API 的方法名直接拼在 URL 路径末尾（/bot<token>/<method>），
// 集中在此处，改版只动这里。
const (
	// MethodGetUpdates 拉取入站更新（长轮询）。
	MethodGetUpdates = "getUpdates"
	// MethodSendMessage 发送文本消息。
	MethodSendMessage = "sendMessage"
	// MethodSendDocument 上传并发送文件。
	MethodSendDocument = "sendDocument"
	// MethodAnswerCallbackQuery 应答内联按钮回调（停止按钮转圈）。
	MethodAnswerCallbackQuery = "answerCallbackQuery"
)

// DefaultBaseURL Telegram Bot API 官方地址。
const DefaultBaseURL = "https://api.telegram.org"

// ---- 解析模式 ----
//
// Telegram 的 parse_mode 决定文本怎么渲染；空串表示纯文本（不解析）。
// 业务侧拿不准时一律用纯文本，避免 400（can't parse entities）。
const (
	// ParseModeNone 纯文本，不解析标记。
	ParseModeNone = ""
	// ParseModeMarkdown 旧版 Markdown。
	ParseModeMarkdown = "Markdown"
	// ParseModeMarkdownV2 MarkdownV2。
	ParseModeMarkdownV2 = "MarkdownV2"
	// ParseModeHTML HTML。
	ParseModeHTML = "HTML"
)

// ---- 协议结构体 ----

// User Telegram 用户。
type User struct {
	// ID 用户唯一标识（白名单用的就是这个数字 id，不是 @username）。
	ID int64 `json:"id"`
	// IsBot 是否是机器人账号。
	IsBot bool `json:"is_bot"`
	// FirstName 名。
	FirstName string `json:"first_name"`
	// LastName 姓。
	LastName string `json:"last_name"`
	// Username 用户名（不含 @，可能为空）。
	Username string `json:"username"`
	// LanguageCode 语言码。
	LanguageCode string `json:"language_code"`
}

// Display 返回可读的用户标识：优先 @username，其次 first_name，最后裸 id。
func (u *User) Display() string {
	if u == nil {
		return "匿名"
	}
	if u.Username != "" {
		return "@" + u.Username
	}
	if u.FirstName != "" {
		return u.FirstName
	}
	return fmt.Sprintf("user#%d", u.ID)
}

// Chat 聊天（私聊/群/频道）。
type Chat struct {
	// ID 聊天 id（私聊时等于对方 user id）。
	ID int64 `json:"id"`
	// Type 聊天类型：private / group / supergroup / channel。
	Type string `json:"type"`
	// Title 群/频道标题。
	Title string `json:"title"`
	// FirstName 私聊对方名。
	FirstName string `json:"first_name"`
	// Username 群/频道用户名。
	Username string `json:"username"`
}

// Document 附件文件。
type Document struct {
	// FileID 文件 id。
	FileID string `json:"file_id"`
	// FileUniqueID 全局唯一文件 id。
	FileUniqueID string `json:"file_unique_id"`
	// FileName 文件名。
	FileName string `json:"file_name"`
	// MimeType MIME 类型。
	MimeType string `json:"mime_type"`
	// FileSize 字节数。
	FileSize int64 `json:"file_size"`
}

// Message 一条消息。
type Message struct {
	// MessageID 消息 id。
	MessageID int64 `json:"message_id"`
	// From 发送者；频道消息可能为空。
	From *User `json:"from"`
	// Chat 所属聊天。
	Chat Chat `json:"chat"`
	// Date 发送时间（Unix 秒）。
	Date int64 `json:"date"`
	// Text 文本内容（命令从这来）。
	Text string `json:"text"`
	// Caption 图片/文件说明文字。
	Caption string `json:"caption"`
	// Document 附件。
	Document *Document `json:"document"`
	// ReplyToMessage 被回复的那条消息。
	ReplyToMessage *Message `json:"reply_to_message"`
}

// CallbackQuery 内联按钮回调。
type CallbackQuery struct {
	// ID 回调 id（answerCallbackQuery 用它）。
	ID string `json:"id"`
	// From 点击者。
	From *User `json:"from"`
	// Message 承载按钮的消息（可能是空）。
	Message *Message `json:"message"`
	// Data 按钮携带的业务数据。
	Data string `json:"data"`
}

// Update 一条入站更新。
type Update struct {
	// UpdateID 更新序号，单调递增，是本包去重的主键。
	UpdateID int64 `json:"update_id"`
	// Message 普通消息。
	Message *Message `json:"message"`
	// EditedMessage 编辑过的消息。
	EditedMessage *Message `json:"edited_message"`
	// ChannelPost 频道消息。
	ChannelPost *Message `json:"channel_post"`
	// CallbackQuery 内联按钮回调。
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

// EffectiveMessage 取本条更新里真正承载内容的消息。
//
// Telegram 会按事件类型把消息放在不同字段；命令解析只需要「有文本的那条」，
// 所以这里做一次归一，避免上层到处判空。
func (u Update) EffectiveMessage() *Message {
	switch {
	case u.Message != nil:
		return u.Message
	case u.EditedMessage != nil:
		return u.EditedMessage
	case u.ChannelPost != nil:
		return u.ChannelPost
	}
	return nil
}

// ResponseParameters 响应里的附加参数（限流重试时间、群组迁移等）。
type ResponseParameters struct {
	// RetryAfter 命中限流时建议等待的秒数。
	RetryAfter int `json:"retry_after"`
	// MigrateToChatID 群组迁移后的新 chat id。
	MigrateToChatID int64 `json:"migrate_to_chat_id"`
}

// apiEnvelope Telegram Bot API 统一响应信封.
//
// 成功：{"ok":true,"result":...}
// 失败：{"ok":false,"error_code":401,"description":"Unauthorized"}
//
// 用泛型包一层：不同方法的 result 形状完全不同（数组/对象/布尔），
// 由调用方指定 T，收发两侧共用同一套解析逻辑。
type apiEnvelope[T any] struct {
	// OK 是否成功。
	OK bool `json:"ok"`
	// Result 成功时的业务结果。
	Result T `json:"result"`
	// ErrorCode 失败时的错误码（HTTP 状态码同值）。
	ErrorCode int `json:"error_code"`
	// Description 失败时的错误描述。
	Description string `json:"description"`
	// Parameters 附加参数。
	Parameters *ResponseParameters `json:"parameters"`
}
