package server

// qrlogin_handler.go：B 站扫码登录（免手动 Cookie 的凭据获取通道）。
//
// 会员内容必须带登录态（平台侧硬约束），本通道把「弄到登录态」的成本
// 从「浏览器 F12 抄 Cookie」压到「手机扫码」：二维码内容由后端向 B 站
// passport 申请，前端只渲染；扫码确认后登录 Cookie 直接加密进凭据中心，
// 全程不经过用户剪贴板。

import (
	"net/http"
	"time"

	"github.com/huluohu/raccoon/internal/bilibili"
	"github.com/huluohu/raccoon/internal/httpx"
	"github.com/huluohu/raccoon/internal/store"
)

// setupQRLogin 注册扫码登录路由（挂在凭据域下）。
func (s *Server) setupQRLogin() {
	s.router.POST("/api/v1/credentials/bilibili/qrcode/start", s.requireAuth(s.csrfForSession(s.handleBiliQRStart)))
	s.router.POST("/api/v1/credentials/bilibili/qrcode/poll", s.requireAuth(s.csrfForSession(s.handleBiliQRPoll)))
}

// biliQRClient 惰性构造扫码客户端（测试可替换 PassportBase 指到假服务）。
func (s *Server) biliQRClient() *bilibili.Client {
	return bilibili.NewClient(nil)
}

// handleBiliQRStart 申请二维码。
func (s *Server) handleBiliQRStart(w http.ResponseWriter, r *http.Request) error {
	start, err := s.biliQRClient().Generate(r.Context())
	if err != nil {
		s.log.Warn("B 站二维码申请失败", map[string]any{"err": err.Error()})
		return httpx.BadGateway("B 站二维码申请失败: " + err.Error())
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"url":        start.URL,
			"qrcode_key": start.QrcodeKey,
			"expires_in": 180, // B 站二维码有效期约 3 分钟（前端轮询节奏参考）
		},
	})
	return nil
}

// handleBiliQRPoll 轮询扫码状态；确认后落凭据。
func (s *Server) handleBiliQRPoll(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		QrcodeKey string `json:"qrcode_key"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.QrcodeKey == "" {
		return httpx.Validation("qrcode_key 不能为空")
	}
	res, err := s.biliQRClient().Poll(r.Context(), req.QrcodeKey)
	if err != nil {
		s.log.Warn("B 站扫码轮询失败", map[string]any{"err": err.Error()})
		return httpx.BadGateway("扫码状态查询失败: " + err.Error())
	}
	switch res.Code {
	case bilibili.CodeSuccess:
		if s.vault == nil {
			return httpx.Internal("凭据加密模块未初始化")
		}
		cipher, err := s.vault.Encrypt([]byte(res.Cookie))
		if err != nil {
			return httpx.Internal("凭据加密失败")
		}
		expiresAt := bilibili.CredentialExpiry()
		c := &store.Credential{
			Platform:  "bilibili",
			Kind:      "session",
			Label:     "B站扫码登录 " + time.Now().Format("2006-01-02 15:04"),
			ExpiresAt: &expiresAt,
		}
		if err := s.store.CreateCredential(c, cipher); err != nil {
			return httpx.Internal("凭据保存失败")
		}
		c.HasValue = true
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{"status": "success", "credential": c},
		})
		return nil
	case bilibili.CodeWaitingScan:
		return s.writeQRStatus(w, "waiting_scan", "等待扫码")
	case bilibili.CodeScanned:
		return s.writeQRStatus(w, "scanned", "已扫码，请在手机上确认登录")
	case bilibili.CodeQRExpired:
		return s.writeQRStatus(w, "expired", "二维码已过期，请重新获取")
	}
	return s.writeQRStatus(w, "unknown", res.Message)
}

func (s *Server) writeQRStatus(w http.ResponseWriter, status, message string) error {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{"status": status, "message": message},
	})
	return nil
}
