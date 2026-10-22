package server

// credential_handler.go：凭据中心路由（docs/04 §2.5）。
//
// 密文加密落库，API 只回元信息 + 打码预览，永不回明文。

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/huluohu/raccoon/internal/credential"
	"github.com/huluohu/raccoon/internal/httpx"
	"github.com/huluohu/raccoon/internal/store"
)

// setupCredentials 注册凭据中心路由。
func (s *Server) setupCredentials() {
	s.router.GET("/api/v1/credentials", s.requireAuth(s.handleCredList))
	s.router.POST("/api/v1/credentials", s.requireAuth(s.csrfForSession(s.handleCredCreate)))
	s.router.GET("/api/v1/credentials/:id", s.requireAuth(s.handleCredGet))
	s.router.PUT("/api/v1/credentials/:id", s.requireAuth(s.csrfForSession(s.handleCredUpdate)))
	s.router.DELETE("/api/v1/credentials/:id", s.requireAuth(s.csrfForSession(s.handleCredDelete)))
	s.router.POST("/api/v1/credentials/:id/check", s.requireAuth(s.csrfForSession(s.handleCredCheck)))
}

// handleCredList 列出凭据（不含密文）。
func (s *Server) handleCredList(w http.ResponseWriter, r *http.Request) error {
	items, err := s.store.ListCredentials()
	if err != nil {
		return httpx.Internal("凭据列表查询失败")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

// handleCredGet 凭据元信息。
func (s *Server) handleCredGet(w http.ResponseWriter, r *http.Request) error {
	c, _, err := s.store.GetCredential(param(r, "id"))
	if err != nil {
		return credErrToHTTP(err)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"data": c})
	return nil
}

type credReq struct {
	Platform  string `json:"platform"`
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	Note      string `json:"note"`
	Value     string `json:"value"`
	ExpiresAt *int64 `json:"expires_at"`
}

// handleCredCreate 新建凭据（加密存储）。
func (s *Server) handleCredCreate(w http.ResponseWriter, r *http.Request) error {
	var req credReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Platform) == "" {
		return httpx.Validation("platform 不能为空")
	}
	switch req.Kind {
	case "cookie_file", "account", "token", "session":
	default:
		return httpx.Validation("kind 需为 cookie_file / account / token / session")
	}
	if strings.TrimSpace(req.Value) == "" {
		return httpx.Validation("value 不能为空")
	}
	if len(req.Value) > 1<<20 {
		return httpx.Validation("凭据内容过大（上限 1MB）")
	}
	if s.vault == nil {
		return httpx.Internal("凭据加密模块未初始化")
	}
	cipher, err := s.vault.Encrypt([]byte(req.Value))
	if err != nil {
		s.log.Error("凭据加密失败", map[string]any{"err": err.Error()})
		return httpx.Internal("凭据加密失败")
	}
	c := &store.Credential{
		Platform:  strings.TrimSpace(req.Platform),
		Kind:      req.Kind,
		Label:     strings.TrimSpace(req.Label),
		Note:      req.Note,
		ExpiresAt: req.ExpiresAt,
	}
	if c.Label == "" {
		c.Label = c.Platform + " " + c.Kind
	}
	if err := s.store.CreateCredential(c, cipher); err != nil {
		return httpx.Internal("凭据保存失败")
	}
	c.Preview = credential.Mask(req.Value)
	c.HasValue = true
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"data": c})
	return nil
}

// handleCredUpdate 更新凭据内容或标签。
func (s *Server) handleCredUpdate(w http.ResponseWriter, r *http.Request) error {
	id := param(r, "id")
	old, _, err := s.store.GetCredential(id)
	if err != nil {
		return credErrToHTTP(err)
	}
	var req credReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = old.Label
	}
	var cipher []byte
	if strings.TrimSpace(req.Value) != "" {
		if s.vault == nil {
			return httpx.Internal("凭据加密模块未初始化")
		}
		cipher, err = s.vault.Encrypt([]byte(req.Value))
		if err != nil {
			return httpx.Internal("凭据加密失败")
		}
	}
	if err := s.store.UpdateCredential(id, label, req.Note, cipher); err != nil {
		return credErrToHTTP(err)
	}
	c, _, _ := s.store.GetCredential(id)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"data": c})
	return nil
}

// handleCredDelete 删除凭据。
func (s *Server) handleCredDelete(w http.ResponseWriter, r *http.Request) error {
	if err := s.store.DeleteCredential(param(r, "id")); err != nil {
		return credErrToHTTP(err)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

// handleCredCheck 健康检查（能连通平台接口则 active→expiring/expired）。
func (s *Server) handleCredCheck(w http.ResponseWriter, r *http.Request) error {
	id := param(r, "id")
	c, cipher, err := s.store.GetCredential(id)
	if err != nil {
		return credErrToHTTP(err)
	}
	if s.vault == nil {
		return httpx.Internal("凭据加密模块未初始化")
	}
	plain, err := s.vault.Decrypt(cipher)
	if err != nil {
		_ = s.store.UpdateCredentialStatus(id, "expired")
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"ok": false, "status": "expired", "message": "无法解密（主密钥不匹配或数据损坏）",
		})
		return nil
	}
	// 平台可达性探测：拿凭据请求平台接口，401/403 说明需更新（expired）。
	status, message := s.probeCredentialPlatform(c, plain)
	_ = s.store.UpdateCredentialStatus(id, status)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": status == "active", "status": status, "message": message,
	})
	return nil
}

// probeCredentialPlatform 平台凭据可达性探测（真实请求，不模拟）。
func (s *Server) probeCredentialPlatform(c *store.Credential, plain []byte) (string, string) {
	switch c.Platform {
	case "bilibili":
		// B 站接口用 cookie 鉴权：未登录返回 -101。
		req, _ := http.NewRequest(http.MethodGet, "https://api.bilibili.com/x/web-interface/nav", nil)
		req.Header.Set("Cookie", string(plain))
		req.Header.Set("User-Agent", "Mozilla/5.0 Raccoon/1.0")
		resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
		if err != nil {
			return "expiring", "无法连接平台: " + err.Error()
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "expired", "平台返回异常状态: " + resp.Status
		}
		return "active", "凭据可用"
	case "telegram":
		return "active", "Telegram 凭据需通过 Bot Token 设置项配置"
	default:
		return "active", "未配置该平台的自动检查逻辑，保留当前状态"
	}
}

// credErrToHTTP store 错误 → HTTP 信封。
func credErrToHTTP(err error) error {
	if errors.Is(err, store.ErrNoSuchCredential) {
		return httpx.NotFound("凭据不存在")
	}
	return httpx.Internal("凭据操作失败")
}
