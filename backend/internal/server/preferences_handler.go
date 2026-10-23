package server

// preferences_handler.go：用户偏好（主页布局）读写（docs/14 E 系列 / docs/15 §3.3）。
//
// 契约（docs/15 §5 T04 验收）：GET 返回 {layout: "<json string>"}（无记录时
// 空串，前端回落默认布局）；PUT 接收 {layout: "<json string>"}——layout 是
// 一段 JSON **字符串**（不是嵌套对象），store 层原样存取，此处只做「合法
// JSON + 体积上限」两道校验，不解析业务语义（字段白名单在前端，见 docs/14）。

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/huluohu/raccoon/internal/httpx"
)

// maxLayoutBytes 布局 JSON 上限（64KB：17 个信息块的开关/排序远小于此，
// 防止把偏好接口当任意存储用）。
const maxLayoutBytes = 64 * 1024

// setupPreferences 注册偏好路由。
func (s *Server) setupPreferences() {
	s.router.GET("/api/v1/user/preferences", s.requireAuth(s.handleGetPreferences))
	s.router.PUT("/api/v1/user/preferences", s.requireAuth(s.csrfForSession(s.handlePutPreferences)))
}

// handleGetPreferences 读偏好（无记录 → layout 空串）。
func (s *Server) handleGetPreferences(w http.ResponseWriter, r *http.Request) error {
	layout, err := s.store.GetPreferencesLayout()
	if err != nil {
		return httpx.Internal("偏好读取失败")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"layout": layout}})
	return nil
}

// handlePutPreferences 覆盖写偏好（单行 UPSERT）。
func (s *Server) handlePutPreferences(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Layout string `json:"layout"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		return httpx.Validation("请求体不是合法 JSON")
	}
	layout := strings.TrimSpace(body.Layout)
	if layout == "" {
		return httpx.Validation("layout 不能为空（传默认布局 JSON）")
	}
	if len(layout) > maxLayoutBytes {
		return httpx.Validation("layout 超过大小上限")
	}
	// layout 本身必须是一段合法 JSON（对象或数组皆可，业务字段不在此校验）。
	var probe any
	if err := json.Unmarshal([]byte(layout), &probe); err != nil {
		return httpx.Validation("layout 不是合法 JSON 字符串")
	}
	if err := s.store.SetPreferencesLayout(layout); err != nil {
		return httpx.Internal("偏好保存失败")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"layout": layout}})
	return nil
}
