package server

// auth_middleware.go：会话认证中间件 + CSRF 防护（docs/04 §1.1）。
//
// 双通道：Web 会话（raccoon_session Cookie）与 API Token（Bearer，Wave 4.9 接入 CLI 前先占位）。
// 管理 API 未初始化（users 空）一律 428 SETUP_REQUIRED——前端据此跳 /setup。

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

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
					if got := r.Header.Get("X-CSRF-Token"); got == "" || got != csrf {
						return httpx.Forbidden("CSRF 校验失败")
					}
				}
			}
		}
		return h(w, r)
	}
}

// authenticate 解析请求身份：优先 Cookie 会话；否则 Bearer Token（docs/04 §1.1）。
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
		return s.authenticateToken(r, strings.TrimSpace(strings.TrimPrefix(ah, "Bearer ")))
	}
	// 未带任何凭据：未初始化时引导 setup。
	if n, err := s.store.CountUsers(); err == nil && n == 0 {
		return nil, &httpx.Error{HTTP: http.StatusPreconditionRequired, Code: httpx.CodeSetupReq, Message: "尚未初始化管理员账号"}
	}
	return nil, httpx.Unauthorized("未登录")
}

// authenticateToken API Token 通道：校验哈希 → 撤销/过期检查 → scope 权限 → 载入用户。
func (s *Server) authenticateToken(r *http.Request, raw string) (*store.User, error) {
	if raw == "" {
		return nil, httpx.Unauthorized("API Token 不能为空")
	}
	tok, err := s.store.GetAPITokenByHash(store.HashToken(raw))
	if err != nil {
		if errors.Is(err, store.ErrNoSuchToken) {
			return nil, httpx.Unauthorized("API Token 无效")
		}
		return nil, httpx.Internal("API Token 校验失败")
	}
	// 撤销即时失效。
	if tok.Revoked {
		return nil, httpx.Unauthorized("API Token 已被撤销")
	}
	// 过期检查。
	if tok.ExpiresAt != nil && *tok.ExpiresAt < time.Now().UnixMilli() {
		return nil, httpx.Unauthorized("API Token 已过期")
	}
	// scope 检查：读需 read；写需 write；管理域（配置/凭据/订阅/直播/令牌）需 admin。
	if need := requiredScope(r); tok.Scope&need == 0 {
		return nil, httpx.Forbidden("API Token 权限不足（需要 scope=" + scopeName(need) + "）")
	}
	s.store.TouchToken(tok.ID)
	u, err := s.store.GetUserByID(tok.UserID)
	if err != nil {
		return nil, httpx.Unauthorized("Token 所属用户不存在")
	}
	return u, nil
}

// requiredScope 请求所需 scope。
//
// 顺序即安全：管理域前缀检查必须排在方法短路之前。早年 GET 短路在前，
// 导致一枚只读令牌就能 GET /api/v1/tokens 枚举全部令牌元数据、
// GET /api/v1/credentials 读凭据列表 —— 越权面是真实存在过的（渗透用例锁定）。
func requiredScope(r *http.Request) int {
	for _, prefix := range []string{
		"/api/v1/config", "/api/v1/credentials", "/api/v1/subscriptions",
		"/api/v1/live-sources", "/api/v1/tokens",
	} {
		if strings.HasPrefix(r.URL.Path, prefix) {
			return store.ScopeAdmin
		}
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		return store.ScopeRead
	}
	return store.ScopeWrite
}

// scopeName scope 位 → 名称（错误提示用）。
func scopeName(scope int) string {
	switch scope {
	case store.ScopeRead:
		return "read"
	case store.ScopeWrite:
		return "write"
	case store.ScopeAdmin:
		return "admin"
	}
	return "unknown"
}

// ensureSetUp WRAPPER：requireAuth 已内嵌 428 逻辑，此函数供事件流等特殊路由复用。
