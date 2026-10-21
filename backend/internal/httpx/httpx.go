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
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

func Validation(msg string) *Error   { return &Error{http.StatusBadRequest, CodeValidation, msg} }
func NotFound(msg string) *Error     { return &Error{http.StatusNotFound, CodeNotFound, msg} }
func Conflict(msg string) *Error     { return &Error{http.StatusConflict, CodeConflict, msg} }
func Unauthorized(msg string) *Error { return &Error{http.StatusUnauthorized, CodeUnauthorized, msg} }
func Forbidden(msg string) *Error    { return &Error{http.StatusForbidden, CodeForbidden, msg} }
func Internal(msg string) *Error     { return &Error{http.StatusInternalServerError, CodeInternal, msg} }

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
	WriteJSON(w, he.HTTP, map[string]any{
		"error": map[string]any{
			"code":       he.Code,
			"message":    he.Message,
			"request_id": RequestIDFrom(r, w),
		},
	})
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
