package server

// auth_middleware.go：会话认证中间件 + CSRF 防护（docs/04 §1.1）。
//
// 双通道：Web 会话（raccoon_session Cookie）与 API Token（Bearer，Wave 4.9 接入 CLI 前先占位）。
// 管理 API 未初始化（users 空）一律 428 SETUP_REQUIRED——前端据此跳 /setup。

import (
	"context"
	"net/http"
	"strings"

	"github.com/huluohu/raccoon/internal/httpx"
	"github.com/huluohu/raccoon/internal/store"
)

type authCtxKey struct{}

// withUser 注入已认证用户（handler 用 userFrom 读取）。
func withUser(r *http.Request, u *store.User) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), authCtxKey{}, u))
}

func userFrom(r *http.Request) *store.User {
	u, _ := r.Context().Value(authCtxKey{}).(*store.User)
	return u
}

// requireAuth 包装需要登录的 handler。
func (s *Server) requireAuth(h HandlerFunc) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		u, err := s.authenticate(r)
		if err != nil {
			return err
		}
		return h(w, withUser(r, u))
	}
}

// csrfForSession 会话通道的写操作校验 X-CSRF-Token（Token 通道豁免，docs/04 §1.1）。
func (s *Server) csrfForSession(h HandlerFunc) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			c, err := r.Cookie(sessionCookieName)
			if err == nil {
				if _, csrf, ok := s.sessions.Get(c.Value); ok {
					if subtle := r.Header.Get("X-CSRF-Token"); subtle == "" || subtle != csrf {
						return httpx.Forbidden("CSRF 校验失败")
					}
				}
			}
		}
		return h(w, r)
	}
}

// authenticate 解析请求身份：优先 Cookie 会话；Bearer Token 走 api_tokens（4.9）。
func (s *Server) authenticate(r *http.Request) (*store.User, error) {
	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		uid, _, ok := s.sessions.Get(c.Value)
		if !ok {
			return nil, httpx.Unauthorized("会话已过期，请重新登录")
		}
		u, err := s.store.GetUserByID(uid)
		if err != nil {
			return nil, httpx.Unauthorized("用户不存在")
		}
		return u, nil
	}
	if ah := r.Header.Get("Authorization"); strings.HasPrefix(ah, "Bearer ") {
		return nil, httpx.Forbidden("API Token 通道尚未开放（Wave 4.9）")
	}
	// 未带任何凭据：未初始化时引导 setup。
	if n, err := s.store.CountUsers(); err == nil && n == 0 {
		return nil, &httpx.Error{HTTP: http.StatusPreconditionRequired, Code: httpx.CodeSetupReq, Message: "尚未初始化管理员账号"}
	}
	return nil, httpx.Unauthorized("未登录")
}

// ensureSetUp WRAPPER：requireAuth 已内嵌 428 逻辑，此函数供事件流等特殊路由复用。
