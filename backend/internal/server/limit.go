package server

// limit.go：内存限流器（docs/04 §5：登录 5 次/分钟/IP；解析端点复用 20 次/分钟）。

import (
	"net/http"
	"sync"
	"time"

	"github.com/huluohu/raccoon/internal/httpx"
)

// Limiter 滑动窗口计数器（单机内存版；多实例部署由反代层承担，已知限制）。
type Limiter struct {
	mu      sync.Mutex
	window  time.Duration
	max     int
	clients map[string]*limitBucket
}

type limitBucket struct {
	count   int
	resetAt time.Time
}

// NewLimiter 构造。
func NewLimiter(window time.Duration, max int) *Limiter {
	return &Limiter{window: window, max: max, clients: map[string]*limitBucket{}}
}

// Allow 记录一次请求并返回是否放行。
func (l *Limiter) Allow(client string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.clients[client]
	if !ok || now.After(b.resetAt) {
		l.clients[client] = &limitBucket{count: 1, resetAt: now.Add(l.window)}
		// 顺便清理过期桶（防内存膨胀）。
		for k, v := range l.clients {
			if now.After(v.resetAt) {
				delete(l.clients, k)
			}
		}
		return true
	}
	if b.count >= l.max {
		return false
	}
	b.count++
	return true
}

// LimitMiddleware 超限 429（Retry-After：窗口秒数取整）。
func LimitMiddleware(l *Limiter, key func(*http.Request) string) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			if !l.Allow(key(r)) {
				w.Header().Set("Retry-After", "60")
				return &httpx.Error{HTTP: http.StatusTooManyRequests, Code: httpx.CodeRateLimited, Message: "请求过于频繁，请稍后再试"}
			}
			return next(w, r)
		}
	}
}
