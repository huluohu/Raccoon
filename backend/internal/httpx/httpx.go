// Package httpx 统一 JSON 响应、错误信封与请求体解析（docs/04 §1.2）。
package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// ErrorCode 对齐 docs/04 §1.2 错误码表。
type ErrorCode string

const (
	CodeValidation   ErrorCode = "VALIDATION"
	CodeUnauthorized ErrorCode = "UNAUTHORIZED"
	CodeForbidden    ErrorCode = "FORBIDDEN"
	CodeNotFound     ErrorCode = "NOT_FOUND"
	CodeConflict     ErrorCode = "CONFLICT"
	CodeSetupReq     ErrorCode = "SETUP_REQUIRED"
	CodeRateLimited  ErrorCode = "RATE_LIMITED"
	CodeInternal     ErrorCode = "INTERNAL"
	CodeNoEngine     ErrorCode = "ENGINE_UNAVAILABLE"
)

// Error 携带 HTTP 状态与业务码；handler 返回它由 WriteErr 转换。
type Error struct {
	HTTP    int
	Code    ErrorCode
	Message string
	// Detail 补充说明（可选）。用于把引擎/上游的真实原因带出来，
	// 比如「yt-dlp: The page needs to be reloaded.」——
	// 否则用户只能看到一句「引擎退出码 1」，无从判断该升级引擎还是换链接。
	Detail string
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

// WithDetail 附加补充说明（链式）。
func (e *Error) WithDetail(detail string) *Error {
	e.Detail = detail
	return e
}

func Validation(msg string) *Error {
	return &Error{HTTP: http.StatusBadRequest, Code: CodeValidation, Message: msg}
}
func NotFound(msg string) *Error {
	return &Error{HTTP: http.StatusNotFound, Code: CodeNotFound, Message: msg}
}
func Conflict(msg string) *Error {
	return &Error{HTTP: http.StatusConflict, Code: CodeConflict, Message: msg}
}
func Unauthorized(msg string) *Error {
	return &Error{HTTP: http.StatusUnauthorized, Code: CodeUnauthorized, Message: msg}
}
func Forbidden(msg string) *Error {
	return &Error{HTTP: http.StatusForbidden, Code: CodeForbidden, Message: msg}
}
func Internal(msg string) *Error {
	return &Error{HTTP: http.StatusInternalServerError, Code: CodeInternal, Message: msg}
}

// BadGateway 上游平台接口不可达/异常（502；与本地 INTERNAL 区分开，
// 「B 站接口挂了」和「浣熊自己出错了」在排障时是完全不同的两件事）。
func BadGateway(msg string) *Error {
	return &Error{HTTP: http.StatusBadGateway, Code: ErrorCode("BAD_GATEWAY"), Message: msg}
}

type CtxKey struct{}

// RequestID 从 context 取 request_id（中间件注入）；取不到时回退读响应头
// （错误出口在中间件之外，拿不到注入后的 context）。
func RequestID(r *http.Request) string {
	if v, ok := r.Context().Value(CtxKey{}).(string); ok && v != "" {
		return v
	}
	return ""
}

// RequestIDFrom 从 context 或响应头取 request_id。
func RequestIDFrom(r *http.Request, w http.ResponseWriter) string {
	if v := RequestID(r); v != "" {
		return v
	}
	if w != nil {
		return w.Header().Get("X-Request-ID")
	}
	return ""
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// WriteErr 统一错误信封；INTERNAL 不泄露内部细节（docs/02 §5）。
func WriteErr(w http.ResponseWriter, r *http.Request, err error) {
	var he *Error
	if !errors.As(err, &he) {
		he = Internal("服务内部错误")
	}
	body := map[string]any{
		"code":       he.Code,
		"message":    he.Message,
		"request_id": RequestIDFrom(r, w),
	}
	// detail 只在有内容时出现（INTERNAL 永不附带，避免泄露内部细节）。
	if he.Detail != "" && he.Code != CodeInternal {
		body["detail"] = he.Detail
	}
	WriteJSON(w, he.HTTP, map[string]any{"error": body})
}

// DecodeJSON 解析请求体；大小上限 1MB 防滥用。
func DecodeJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return Validation("请求体不能为空")
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return Validation("读取请求体失败")
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return Validation("请求体不能为空")
	}
	if err := json.Unmarshal(body, v); err != nil {
		return Validation("JSON 格式错误: " + err.Error())
	}
	return nil
}
