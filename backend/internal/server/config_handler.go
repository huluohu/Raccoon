package server

// config_handler.go：配置中心路由（docs/04 §2.4）。
//
// 五要素 + 生效时机 + env 锁定 + 历史回滚 + 导出导入。
// 保存结果按接口返回的 effective_summary 展示（docs/06 §3），前端不猜。

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/huluohu/raccoon/internal/config"
	"github.com/huluohu/raccoon/internal/httpx"
)

// setupConfig 注册配置中心路由。
func (s *Server) setupConfig() {
	s.router.GET("/api/v1/config", s.requireAuth(s.handleConfigGet))
	s.router.PUT("/api/v1/config", s.requireAuth(s.csrfForSession(s.handleConfigPut)))
	s.router.PUT("/api/v1/config/:key", s.requireAuth(s.csrfForSession(s.handleConfigPutOne)))
	s.router.POST("/api/v1/config/test-connection", s.requireAuth(s.csrfForSession(s.handleConfigTestConn)))
	s.router.GET("/api/v1/config/export", s.requireAuth(s.handleConfigExport))
	s.router.POST("/api/v1/config/import", s.requireAuth(s.csrfForSession(s.handleConfigImport)))
	s.router.GET("/api/v1/config/history", s.requireAuth(s.handleConfigHistory))
	s.router.POST("/api/v1/config/rollback", s.requireAuth(s.csrfForSession(s.handleConfigRollback)))
}

// configItemJSON 界面渲染用的一项（五要素 + 当前值 + 锁定状态）。
type configItemJSON struct {
	Key       string   `json:"key"`
	Desc      string   `json:"desc"`
	Default   any      `json:"default"`
	Value     any      `json:"value"`
	Type      string   `json:"type"`
	Effective string   `json:"effective"`
	Sensitive bool     `json:"sensitive"`
	Locked    bool     `json:"locked"`
	EnvVar    string   `json:"env_var,omitempty"`
	Version   int      `json:"version"`
	Choices   []string `json:"choices,omitempty"` // 枚举项的合法取值（前端渲染成下拉）
}

// handleConfigGet 返回全量注册表 + 当前值（敏感项打码）。
func (s *Server) handleConfigGet(w http.ResponseWriter, r *http.Request) error {
	rows, _ := s.store.ListSettings()
	versions := map[string]int{}
	for _, row := range rows {
		versions[row.Key] = row.Version
	}
	items := make([]configItemJSON, 0, len(config.Registry))
	for _, meta := range config.Registry {
		val := s.cfg.Get(meta.Key)
		item := configItemJSON{
			Key:       meta.Key,
			Desc:      meta.Desc,
			Default:   meta.Default,
			Value:     val,
			Type:      configTypeName(meta.Default),
			Effective: string(meta.Effective),
			Sensitive: meta.Sensitive,
			EnvVar:    meta.EnvVar,
			Version:   versions[meta.Key],
			Choices:   settingChoices[meta.Key],
		}
		if env := s.cfg.LockedBy(meta.Key); env != "" {
			item.Locked = true
			item.EnvVar = env
		}
		if meta.Sensitive {
			if str, ok := val.(string); ok {
				item.Value = maskString(str)
			} else {
				item.Value = "***"
			}
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"items":   items,
		"version": s.cfg.Version(),
	})
	return nil
}

// handleConfigPut 批量保存（docs/06 §3 五步流程）。
func (s *Server) handleConfigPut(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Settings map[string]any `json:"settings"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if len(req.Settings) == 0 {
		return httpx.Validation("settings 不能为空")
	}
	return s.saveConfig(w, r, req.Settings)
}

// handleConfigPutOne 单项保存。
func (s *Server) handleConfigPutOne(w http.ResponseWriter, r *http.Request) error {
	key := param(r, "key")
	var req struct {
		Value any `json:"value"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	return s.saveConfig(w, r, map[string]any{key: req.Value})
}

// saveConfig 统一保存流程：校验 → 持久化 → 应用 → 返回生效时机汇总。
func (s *Server) saveConfig(w http.ResponseWriter, r *http.Request, values map[string]any) error {
	// 1. 逐项校验（类型 + 路径/URL 规则 + 锁定检查）。
	for key, val := range values {
		meta, ok := config.SettingMeta(key)
		if !ok {
			return httpx.Validation("未注册的配置项: " + key)
		}
		if env := s.cfg.LockedBy(key); env != "" {
			return &httpx.Error{HTTP: http.StatusConflict, Code: "LOCKED_BY_ENV",
				Message: "配置项 " + key + " 由环境变量 " + env + " 锁定，请在部署环境修改"}
		}
		if err := validateSettingValue(key, val, meta); err != nil {
			return err
		}
	}
	// 2. 持久化（逐项写库 + 留痕；失败即整体放弃，保持上一份有效配置）。
	user := userRef(userFrom(r))
	saved := []string{}
	effective := map[string][]string{"immediate": {}, "next_task": {}, "restart": {}}
	for key, val := range values {
		version, err := s.cfg.Persist(s.store.RW, key, val, user)
		if err != nil {
			var te *config.TypeError
			if errors.As(err, &te) {
				return httpx.Validation(te.Error())
			}
			var le *config.LockedError
			if errors.As(err, &le) {
				return &httpx.Error{HTTP: http.StatusConflict, Code: "LOCKED_BY_ENV", Message: le.Error()}
			}
			s.log.Error("配置保存失败", map[string]any{"key": key, "err": err.Error()})
			return httpx.Internal("配置保存失败")
		}
		raw, _ := json.Marshal(val)
		_ = s.store.AppendSettingHistory(key, string(raw), version, user, "")
		meta, _ := config.SettingMeta(key)
		effective[string(meta.Effective)] = append(effective[string(meta.Effective)], key)
		saved = append(saved, key)
	}
	// 3. 广播 config.changed（前端即时生效；docs/04 §3）。
	s.hub.Broadcast("config.changed", map[string]any{"keys": saved, "effective": effective})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"saved":             saved,
		"effective_summary": effective,
		"version":           s.cfg.Version(),
	})
	return nil
}

// settingChoices 枚举型配置项的合法取值（唯一来源）。
//
// 同一份定义供两处使用，避免「后端校验一套、前端下拉另一套」慢慢漂移：
//   - validateSettingValue 做保存时校验（前端绕过也拦得住）
//   - configItemJSON 下发给前端，渲染成下拉（用户不用猜合法值）
//
// 缺省项（未列出的 key）没有枚举，自由文本 + 后端类型校验。
var settingChoices = map[string][]string{
	"download.default_quality": {"best", "audio", "360p", "480p", "720p", "1080p"},
	"log.level":                {"debug", "info", "warn", "error"},
}

// validateSettingValue 值域校验：类型已在 config.Persist 里，这里补业务规则。
func validateSettingValue(key string, val any, meta config.Setting) error {
	switch key {
	case "storage.data_dir", "storage.download_dir":
		str, ok := val.(string)
		if !ok {
			return httpx.Validation(key + " 需要字符串类型的值")
		}
		if strings.TrimSpace(str) == "" {
			return httpx.Validation(key + " 不能为空")
		}
		if key == "storage.data_dir" && !strings.HasPrefix(str, "/") {
			return httpx.Validation("数据目录必须是绝对路径")
		}
	case "download.max_concurrent", "download.platform_cap", "download.max_attempts":
		f, ok := val.(float64)
		if !ok {
			return httpx.Validation(key + " 需要整数")
		}
		if int(f) != int(f) || int(f) < 1 || int(f) > 20 {
			return httpx.Validation(key + " 需在 1–20 之间")
		}
	case "download.rate_limit_bps":
		f, ok := val.(float64)
		if !ok || f < 0 {
			return httpx.Validation(key + " 需要非负整数")
		}
	case "storage.disk_warn_pct":
		f, ok := val.(float64)
		if !ok || f < 1 || f > 50 {
			return httpx.Validation(key + " 需在 1–50 之间")
		}
	case "net.proxy":
		str, ok := val.(string)
		if !ok {
			return httpx.Validation(key + " 需要字符串")
		}
		if strings.TrimSpace(str) != "" {
			if !strings.HasPrefix(str, "http://") && !strings.HasPrefix(str, "https://") && !strings.HasPrefix(str, "socks5://") {
				return httpx.Validation("代理地址需以 http:// / https:// / socks5:// 开头")
			}
		}
	case "download.default_quality":
		str, ok := val.(string)
		if !ok {
			return httpx.Validation(key + " 需要字符串")
		}
		if !inStrList(settingChoices[key], str) {
			return httpx.Validation("清晰度需为 " + strings.Join(settingChoices[key], " / "))
		}
	case "log.level":
		str, _ := val.(string)
		if !inStrList(settingChoices[key], str) {
			return httpx.Validation("日志级别需为 " + strings.Join(settingChoices[key], " / "))
		}
	}
	return nil
}

// inStrList s 是否在列表内。
func inStrList(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// handleConfigTestConn 连接类设置测试（qb / 115 / telegram）。
func (s *Server) handleConfigTestConn(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Target string         `json:"target"`
		Params map[string]any `json:"params"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	res := s.testConnection(req.Target, req.Params)
	httpx.WriteJSON(w, http.StatusOK, res)
	return nil
}

// handleConfigExport 导出配置 JSON（默认不含敏感项明文）。
func (s *Server) handleConfigExport(w http.ResponseWriter, r *http.Request) error {
	includeSensitive := r.URL.Query().Get("include_sensitive") == "1"
	items := map[string]any{}
	for _, meta := range config.Registry {
		val := s.cfg.Get(meta.Key)
		if meta.Sensitive {
			if includeSensitive {
				items[meta.Key] = val
			} else {
				items[meta.Key] = "***"
			}
			continue
		}
		items[meta.Key] = val
	}
	out := map[string]any{
		"version":     s.cfg.Version(),
		"exported_at": nowMillis(),
		"settings":    items,
	}
	if includeSensitive {
		out["warning"] = "本次导出包含敏感项明文，请妥善保管"
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// handleConfigImport 导入配置（先校验，冲突项列 diff；凭据类拒绝）。
func (s *Server) handleConfigImport(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Settings map[string]any `json:"settings"`
		DryRun   bool           `json:"dry_run"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if len(req.Settings) == 0 {
		return httpx.Validation("settings 不能为空")
	}
	diff := []map[string]any{}
	for key, val := range req.Settings {
		meta, ok := config.SettingMeta(key)
		if !ok {
			return httpx.Validation("未注册的配置项: " + key)
		}
		if meta.Sensitive {
			return httpx.Validation("敏感配置项 " + key + " 不能通过导入修改，请到凭据中心单独更新")
		}
		if err := validateSettingValue(key, val, meta); err != nil {
			return err
		}
		cur := s.cfg.Get(key)
		if !jsonEqual(cur, val) {
			diff = append(diff, map[string]any{"key": key, "current": cur, "incoming": val})
		}
	}
	if req.DryRun || len(diff) == 0 {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"diff": diff, "applied": false})
		return nil
	}
	if err := s.saveConfig(w, r, req.Settings); err != nil {
		return err
	}
	return nil
}

// handleConfigHistory 配置历史。
func (s *Server) handleConfigHistory(w http.ResponseWriter, r *http.Request) error {
	key := r.URL.Query().Get("key")
	limit := atoiDefault(r.URL.Query().Get("limit"), 50)
	items, err := s.store.ListSettingHistory(key, limit)
	if err != nil {
		return httpx.Internal("配置历史查询失败")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

// handleConfigRollback 回滚到指定版本（写新版本，非物理回退；docs/06 §5）。
func (s *Server) handleConfigRollback(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Key     string `json:"key"`
		Version int    `json:"version"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	meta, ok := config.SettingMeta(req.Key)
	if !ok {
		return httpx.Validation("未注册的配置项: " + req.Key)
	}
	if meta.Sensitive {
		return httpx.Validation("敏感配置项不能回滚，请在凭据中心重新上传")
	}
	raw, err := s.store.GetSettingHistoryValue(req.Key, req.Version)
	if err != nil {
		return httpx.NotFound("该配置的历史版本不存在")
	}
	var val any
	if err := json.Unmarshal([]byte(raw), &val); err != nil {
		return httpx.Validation("历史版本数据损坏，无法回滚")
	}
	if err := validateSettingValue(req.Key, val, meta); err != nil {
		return err
	}
	return s.saveConfig(w, r, map[string]any{req.Key: val})
}

// configTypeName 注册表默认值类型名（前端渲染控件用）。
func configTypeName(def any) string {
	switch def.(type) {
	case int:
		return "int"
	case bool:
		return "bool"
	case string:
		return "string"
	}
	return "string"
}

// maskString 敏感值打码。
func maskString(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= 4 {
		return "****"
	}
	return string(r[:2]) + "****" + string(r[len(r)-2:])
}

// jsonEqual 两值 JSON 等价比较。
func jsonEqual(a, b any) bool {
	ra, err1 := json.Marshal(a)
	rb, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return string(ra) == string(rb)
}
