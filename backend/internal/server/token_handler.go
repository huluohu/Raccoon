package server

// token_handler.go：API Token 路由（docs/04 §2.1 认证双通道 / §4 幂等）。
//
// 明文 token 只在创建时返回一次；库里只存 SHA-256 哈希。
// 撤销后立即失效（每次请求校验 revoked_at）。

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/huluohu/raccoon/internal/httpx"
	"github.com/huluohu/raccoon/internal/store"
)

// setupTokens 注册令牌路由。
func (s *Server) setupTokens() {
	s.router.GET("/api/v1/tokens", s.requireAuth(s.handleTokenList))
	s.router.POST("/api/v1/tokens", s.requireAuth(s.csrfForSession(s.handleTokenCreate)))
	s.router.DELETE("/api/v1/tokens/:id", s.requireAuth(s.csrfForSession(s.handleTokenRevoke)))
}

// handleTokenList 列出令牌。
func (s *Server) handleTokenList(w http.ResponseWriter, r *http.Request) error {
	items, err := s.store.ListAPITokens()
	if err != nil {
		return httpx.Internal("令牌列表查询失败")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

type tokenReq struct {
	Name      string `json:"name"`
	Scope     int    `json:"scope"`
	ExpiresIn int    `json:"expires_in_hours"` // 0=永不过期
}

// handleTokenCreate 签发令牌（明文只返回这一次）。
func (s *Server) handleTokenCreate(w http.ResponseWriter, r *http.Request) error {
	var req tokenReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Name) == "" {
		return httpx.Validation("name 不能为空")
	}
	scope := req.Scope
	if scope == 0 {
		scope = store.ScopeRead | store.ScopeWrite
	}
	if scope < 0 || scope > store.ScopeAll {
		return httpx.Validation("scope 必须是 1–7 的位掩码（read=1, write=2, admin=4）")
	}
	// 负数必须拒：ExpiresIn <= 0 都表示「永不过期」，传 -1 若被静默当成永久，
	// 调用方会拿到一个自己以为会过期的长期有效令牌（实测踩过：负数被当 0 处理，返回 200）。
	if req.ExpiresIn < 0 {
		return httpx.Validation("expires_in_hours 不能为负（0 表示永不过期）")
	}
	// 生成 32 字节随机明文（base64url）。
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return httpx.Internal("令牌生成失败")
	}
	raw := "rc_" + base64.RawURLEncoding.EncodeToString(buf)
	u := userFrom(r)
	if u == nil {
		return httpx.Unauthorized("未登录")
	}
	var expiresAt *int64
	if req.ExpiresIn > 0 {
		t := time.Now().Add(time.Duration(req.ExpiresIn) * time.Hour).UnixMilli()
		expiresAt = &t
	}
	tok, err := s.store.CreateAPIToken(u.ID, strings.TrimSpace(req.Name), store.HashToken(raw), scope, expiresAt)
	if err != nil {
		return httpx.Internal("令牌保存失败")
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"data":   tok,
		"token":  raw,
		"notice": "令牌明文仅此一次可见，请立即保存",
	})
	return nil
}

// handleTokenRevoke 撤销令牌（立即失效）。
func (s *Server) handleTokenRevoke(w http.ResponseWriter, r *http.Request) error {
	if err := s.store.RevokeAPIToken(param(r, "id")); err != nil {
		return httpx.NotFound("令牌不存在或已撤销")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}
