package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/huluohu/raccoon/internal/httpx"
)

// accessLog 访问日志（独立于 logx 业务日志，行内 JSON 供采集）。
var accessLog = log.New(os.Stdout, "", 0)

type paramsCtxKey struct{}

func withParams(r *http.Request, params map[string]string) context.Context {
	return context.WithValue(r.Context(), paramsCtxKey{}, params)
}

func paramsFrom(r *http.Request) (map[string]string, bool) {
	p, ok := r.Context().Value(paramsCtxKey{}).(map[string]string)
	return p, ok
}

// RequestID 中间件：生成/透传 request_id 并写访问日志。键与 httpx.RequestID 同源。
func RequestID(next HandlerFunc) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		rid := r.Header.Get("X-Request-ID")
		if rid == "" {
			buf := make([]byte, 8)
			_, _ = rand.Read(buf)
			rid = "req_" + hex.EncodeToString(buf)
		}
		r = r.WithContext(context.WithValue(r.Context(), httpx.CtxKey{}, rid))
		w.Header().Set("X-Request-ID", rid)
		start := time.Now()
		err := next(w, r)
		dur := time.Since(start)
		// 访问日志（脱敏后）；错误由各 handler 记录详情。
		accessLog.Printf(`{"ts":%q,"request_id":%q,"method":%q,"path":%q,"dur_ms":%d,"err":%v}`,
			time.Now().UTC().Format(time.RFC3339Nano), rid, r.Method, r.URL.Path, dur.Milliseconds(), err != nil)
		return err
	}
}

type reqIDKey struct{}

// Recover 中间件：handler panic 不拖垮进程（对应 docs/05 §4 worker panic 兜底）。
func Recover(next HandlerFunc) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) (err error) {
		defer func() {
			if rec := recover(); rec != nil {
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
		}()
		return next(w, r)
	}
}
