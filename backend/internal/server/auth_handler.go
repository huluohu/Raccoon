package server

// auth_handler.go：认证路由 handler（docs/04 §2.1）。
// 状态机：users 空 → setup_required=428；setup 仅空库可用；登录限流在中间件。

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/huluohu/raccoon/internal/auth"
	"github.com/huluohu/raccoon/internal/httpx"
	"github.com/huluohu/raccoon/internal/store"
)

const sessionCookieName = "raccoon_session"

const sessionTTL = 30 * 24 * time.Hour

// authDeps 认证模块依赖（Server 持有）。
type authDeps struct {
	auth    *auth.SessionStore
	dataDir string
}

// setupAuth 挂载 auth 路由与认证中间件。
func (s *Server) setupAuth() {
	s.sessions = auth.NewSessionStore(sessionTTL)

	// 无认证入口。
	s.router.POST("/api/v1/auth/status", s.handleAuthStatus)
	s.router.POST("/api/v1/auth/setup", s.handleAuthSetup)
	s.router.POST("/api/v1/auth/login", s.handleAuthLogin)
	s.router.POST("/api/v1/auth/logout", s.handleAuthLogout)

	// 需认证入口（requireAuth 在注册包装层统一截取）。
	s.router.GET("/api/v1/auth/me", s.requireAuth(s.handleAuthMe))
	s.router.PUT("/api/v1/auth/password", s.requireAuth(s.csrfForSession(s.handleAuthPassword)))
	s.router.PUT("/api/v1/auth/username", s.requireAuth(s.csrfForSession(s.handleAuthUsername)))

	// 会话 GC。
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				s.sessions.GC()
			case <-s.done:
				return
			}
		}
	}()
}

// ---- handlers ----

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) error {
	n, err := s.store.CountUsers()
	if err != nil {
		return httpx.Internal("读取用户状态失败")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"setup_required": n == 0})
	return nil
}

type setupReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleAuthSetup(w http.ResponseWriter, r *http.Request) error {
	if n, err := s.store.CountUsers(); err != nil {
		return httpx.Internal("读取用户状态失败")
	} else if n > 0 {
		return httpx.Conflict("已初始化，不可重复 setup")
	}
	var req setupReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := validateUserFields(req.Username, req.Password); err != nil {
		return httpx.Validation(err.Error())
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		return httpx.Validation(err.Error())
	}
	u, err := s.store.CreateUser(strings.TrimSpace(req.Username), hash)
	if err != nil {
		if errors.Is(err, store.ErrUserExists) {
			return httpx.Conflict("用户名已存在")
		}
		return httpx.Internal("创建用户失败")
	}
	_ = s.store.TouchLogin(u.ID)
	sid, csrf, err := s.sessions.Create(u.ID)
	if err != nil {
		return httpx.Internal("创建会话失败")
	}
	s.setSessionCookie(w, sid)
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"user":       u,
		"csrf_token": csrf,
	})
	return nil
}

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) error {
	if !s.loginLimiter.Allow(clientIP(r)) {
		w.Header().Set("Retry-After", "60")
		return &httpx.Error{HTTP: http.StatusTooManyRequests, Code: httpx.CodeRateLimited, Message: "登录尝试过于频繁，请 1 分钟后再试"}
	}
	var req loginReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Username) == "" || req.Password == "" {
		return httpx.Validation("用户名与口令必填")
	}
	u, err := s.store.GetUserByUsername(strings.TrimSpace(req.Username))
	if err != nil {
		// 统一文案，不暴露用户是否存在。
		return httpx.Unauthorized("用户名或口令错误")
	}
	if !auth.VerifyPassword(req.Password, u.PasswordHash) {
		return httpx.Unauthorized("用户名或口令错误")
	}
	// 散列参数过期则透明升级。
	if auth.NeedsRehash(u.PasswordHash) {
		if h, err := auth.HashPassword(req.Password); err == nil {
			_ = s.store.UpdatePassword(u.ID, h)
		}
	}
	sid, csrf, err := s.sessions.Create(u.ID)
	if err != nil {
		return httpx.Internal("创建会话失败")
	}
	_ = s.store.TouchLogin(u.ID)
	s.setSessionCookie(w, sid)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"user":       u,
		"csrf_token": csrf,
	})
	return nil
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(sessionCookieName); err == nil {
		s.sessions.Delete(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/",
		MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"user": u})
	return nil
}

type changePwReq struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

func (s *Server) handleAuthPassword(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	var req changePwReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if !auth.VerifyPassword(req.OldPassword, u.PasswordHash) {
		return httpx.Unauthorized("旧口令不正确")
	}
	if err := auth.CheckPasswordPolicy(req.NewPassword); err != nil {
		return httpx.Validation(err.Error())
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		return httpx.Validation(err.Error())
	}
	if err := s.store.UpdatePassword(u.ID, hash); err != nil {
		return httpx.Internal("密码更新失败")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

// changeNameReq 改用户名：必须带当前口令。
// 单管理员自托管场景下会话有效期 30 天，改账户名这类敏感操作要求当场验证口令，
// 避免会话被借用时账户名被静默改掉（与 qBittorrent WebUI 同一策略）。
type changeNameReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleAuthUsername(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	var req changeNameReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.Password == "" {
		return httpx.Validation("当前口令必填")
	}
	if !auth.VerifyPassword(req.Password, u.PasswordHash) {
		return httpx.Unauthorized("当前口令不正确")
	}
	if err := validateUsername(req.Username); err != nil {
		return httpx.Validation(err.Error())
	}
	// 改成同名视为成功（幂等），不进唯一索引报错分支。
	if strings.TrimSpace(req.Username) == u.Username {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"user": u})
		return nil
	}
	if err := s.store.UpdateUsername(u.ID, strings.TrimSpace(req.Username)); err != nil {
		if errors.Is(err, store.ErrUserExists) {
			return httpx.Conflict("用户名已存在")
		}
		return httpx.Internal("用户名更新失败")
	}
	updated, err := s.store.GetUserByID(u.ID)
	if err != nil {
		return httpx.Internal("读取用户失败")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"user": updated})
	return nil
}

// ---- 辅助 ----

// isCJK 汉字区段（统一表意 + 扩展 A + 兼容表意）。
// 不能图省事写 `c >= 0x4e00`：那样 emoji（U+1F600 起）也会被当成"中文"放过去。
func isCJK(c rune) bool {
	return c >= 0x3400 && c <= 0x4DBF || c >= 0x4E00 && c <= 0x9FFF || c >= 0xF900 && c <= 0xFAFF
}

// validateUsername 用户名校验（setup 与改用户名共用）。
func validateUsername(username string) error {
	username = strings.TrimSpace(username)
	if len([]rune(username)) < 2 || len([]rune(username)) > 32 {
		return errors.New("用户名长度需在 2–32 之间")
	}
	for _, c := range username {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || isCJK(c)) {
			return errors.New("用户名仅限字母/数字/_/-/中文")
		}
	}
	return nil
}

func validateUserFields(username, password string) error {
	if err := validateUsername(username); err != nil {
		return err
	}
	return auth.CheckPasswordPolicy(password)
}

func (s *Server) setSessionCookie(w http.ResponseWriter, sid string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sid,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode, // 同站部署；防跨站 POST 带 cookie
		// Secure 由部署层（HTTPS 反代）决定；本地 http 不置 Secure 否则不生效。
	})
}

var _ = time.Second // sessionTTL 定义处已引用 time
