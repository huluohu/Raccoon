package tgbot

// errors.go：错误类型定义。
//
// 分工：网络/协议层的错误用「哨兵错误 + 包装」表达（errors.Is 可判）；
// Telegram 返回的业务错误用结构化类型 APIError 表达（errors.As 可取字段）。

import (
	"errors"
	"fmt"
)

// 哨兵错误：调用方用 errors.Is 判定「属于哪一类问题」。
var (
	// ErrUnauthorized 用户不在白名单内。
	ErrUnauthorized = errors.New("tgbot: 用户不在白名单内")
	// ErrInvalidCommand 命令无法解析（既不是已知命令，参数也不合法）。
	ErrInvalidCommand = errors.New("tgbot: 无效命令")
	// ErrMissingArgument 命令缺少必需参数。
	ErrMissingArgument = errors.New("tgbot: 命令缺少必需参数")
	// ErrDuplicateUpdate update_id 重复，已被去重丢弃。
	ErrDuplicateUpdate = errors.New("tgbot: update 重复，已忽略")
	// ErrNoHandler 未注入 Handler，无法处理命令。
	ErrNoHandler = errors.New("tgbot: 未注入 Handler")
	// ErrEmptyToken Bot Token 为空。
	ErrEmptyToken = errors.New("tgbot: Bot Token 为空")
	// ErrNotFound 目标对象不存在。
	ErrNotFound = errors.New("tgbot: 目标不存在")
)

// ProtocolError 响应不是合法的 Telegram 报文（JSON 解析失败、信封缺字段）。
//
// 与 APIError 分开：APIError 是「Telegram 明确返回了错误」（有 error_code），
// ProtocolError 是「根本没拿到能理解的响应」（代理返回了 HTML 之类）——
// 上层对这两者的处理策略不同（前者可按码分支，后者只能重试或告警）。
type ProtocolError struct {
	// Method 触发的接口方法名。
	Method string
	// Err 底层原因（JSON 解析错误等）。
	Err error
	// Body 响应体摘要（已截断），便于排查。
	Body string
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("tgbot: %s 响应格式错误: %v", e.Method, e.Err)
}

func (e *ProtocolError) Unwrap() error { return e.Err }

// APIError Telegram Bot API 返回的业务错误（响应里 ok=false）。
//
// 结构化而非字符串：调用方需要按 error_code 分支处理（例如 429 限流要退避、
// 403 被封禁要停机），光靠字符串匹配不可靠。
type APIError struct {
	// ErrorCode Telegram 错误码，与 HTTP 状态码同值（如 400/401/429）。
	ErrorCode int
	// ErrorDescription Telegram 的错误描述。
	ErrorDescription string
	// Method 触发的接口方法名（getUpdates/sendMessage/...），便于定位。
	Method string
	// RetryAfterSeconds 限流建议等待秒数；非 429 错误为 0。
	//
	// 单列一个字段而不是每次都从描述里抠字符串：退避策略需要它，
	// 而 Telegram 把它放在响应的 parameters.retry_after 里。
	RetryAfterSeconds int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("tgbot: %s 失败 (error_code=%d): %s",
		e.Method, e.ErrorCode, e.ErrorDescription)
}

// Is 支持 errors.Is(err, ErrNotFound)：Telegram 用 404 表达「对象不存在」，
// 上层通常想按语义判而不是按数字码判。
func (e *APIError) Is(target error) bool {
	if target == ErrNotFound {
		return e.ErrorCode == 404
	}
	return false
}
