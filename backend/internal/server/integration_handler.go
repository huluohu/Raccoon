package server

// integration_handler.go：外部集成通道的装配与入口（docs/08 W4.6/W4.7/W4.8）。
//
// 三条外部通道的共性：
//   - 都需要「连接信息」（qB 地址+账密 / 115 token / TG bot token），
//     这些信息放在 settings 配置项里（凭据中心存的是平台账号 Cookie，不适合放服务连接参数）；
//   - 都不影响其他功能（docs/01 §5.8：qB/115 不可用时仅相关任务失败）；
//   - 都必须与 Web 共用同一任务系统（docs/01 §5.1：任意入口同队列同状态）。
//
// 因此本文件只做：读配置 → 造客户端 → 注入 task.ExternalExecutor / 启动 TG 轮询 → 注册路由。

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/huluohu/raccoon/internal/httpx"
	"github.com/huluohu/raccoon/internal/n115"
	"github.com/huluohu/raccoon/internal/qb"
	"github.com/huluohu/raccoon/internal/store"
	"github.com/huluohu/raccoon/internal/task"
	"github.com/huluohu/raccoon/internal/tgbot"
)

// setupIntegrations 装配外部通道（qB / 115 / Telegram）。
func (s *Server) setupIntegrations() {
	// ---- qBittorrent ----
	qbClient := s.buildQBClient()
	// ---- 115 ----
	n115Client := s.buildN115Client()

	// 外部通道执行器：磁力/115 任务走这里，不进下载引擎。
	s.tasks.SetExternalExecutor(task.NewExternalExecutor(s.tasks, NewQBAdapter(qbClient), NewN115Adapter(n115Client)))

	// ---- Telegram Bot ----
	if s.tgEnabled() {
		s.startTelegram()
	}

	// 路由
	s.router.GET("/api/v1/integrations/status", s.requireAuth(s.handleIntegrationStatus))
	s.router.POST("/api/v1/integrations/qb/test", s.requireAuth(s.csrfForSession(s.handleQBTest)))
	s.router.POST("/api/v1/integrations/n115/test", s.requireAuth(s.csrfForSession(s.handleN115Test)))
	s.router.GET("/api/v1/integrations/telegram/status", s.requireAuth(s.handleTGStatus))
}

// buildQBClient 按配置构造 qB 客户端；未配置返回 nil（相关任务失败，其他功能不受影响）。
func (s *Server) buildQBClient() *qb.Client {
	base := strings.TrimSpace(s.cfg.GetString("qb.url"))
	user := strings.TrimSpace(s.cfg.GetString("qb.username"))
	pass := s.cfg.GetString("qb.password")
	if base == "" || user == "" {
		return nil
	}
	return qb.New(qb.Config{
		BaseURL:   base,
		Username:  user,
		Password:  pass,
		Timeout:   15 * time.Second,
		UserAgent: "Raccoon/1.0",
	})
}

// buildN115Client 按配置构造 115 客户端；未配置 token 返回 nil。
func (s *Server) buildN115Client() *n115.Client {
	token := strings.TrimSpace(s.cfg.GetString("n115.access_token"))
	if token == "" {
		return nil
	}
	return n115.New(n115.Config{
		BaseURL:       s.cfg.GetString("n115.base_url"),
		AuthBaseURL:   s.cfg.GetString("n115.auth_base_url"),
		QRCodeBaseURL: s.cfg.GetString("n115.qrcode_base_url"),
		HTTPClient:    &http.Client{Timeout: 30 * time.Second},
		ClientID:      s.cfg.GetString("n115.client_id"),
		TokenProvider: staticTokenProvider(token),
	})
}

// staticTokenProvider 固定 token 提供器（配置项存的就是 access token）。
type staticTokenProvider string

func (p staticTokenProvider) Token(ctx context.Context) (string, error) {
	if string(p) == "" {
		return "", n115.ErrNoToken
	}
	return string(p), nil
}

// tgEnabled Telegram 是否启用（配置了 token 且有白名单用户）。
func (s *Server) tgEnabled() bool {
	return strings.TrimSpace(s.cfg.GetString("telegram.bot_token")) != "" &&
		len(s.cfg.GetStringList("telegram.allowed_users")) > 0
}

// startTelegram 启动 TG 长轮询。
//
// Handler 走统一任务系统（SourceTelegram），因此 TG 提交的任务与 Web 提交的在
// 同一列表、同一状态机 —— 满足 docs/01 §5.1 的不变式。
func (s *Server) startTelegram() {
	users := s.cfg.GetStringList("telegram.allowed_users")
	allowed := make([]int64, 0, len(users))
	for _, u := range users {
		if id, err := strconv.ParseInt(strings.TrimSpace(u), 10, 64); err == nil {
			allowed = append(allowed, id)
		}
	}
	client, err := tgbot.New(tgbot.Config{
		Token:        strings.TrimSpace(s.cfg.GetString("telegram.bot_token")),
		AllowedUsers: allowed,
		PollTimeout:  30 * time.Second,
		Logger:       s.log.With("telegram"),
	})
	if err != nil {
		s.log.Warn("Telegram Bot 初始化失败，通道未启用", map[string]any{"err": err.Error()})
		return
	}
	s.tgClient = client
	bot := tgbot.NewBot(client, &tgHandler{srv: s})
	ctx, cancel := context.WithCancel(context.Background())
	s.tgCancel = cancel
	go func() {
		if err := bot.Run(ctx); err != nil && ctx.Err() == nil {
			s.log.Error("Telegram Bot 轮询退出", map[string]any{"err": err.Error()})
		}
	}()
	s.log.Info("Telegram Bot 已启动", map[string]any{"allowed_users": len(allowed)})
}

// tgHandler 把 TG 命令接到统一任务系统。
type tgHandler struct {
	srv *Server
}

func (h *tgHandler) OnStart(ctx context.Context, chatID int64, user *tgbot.User) error {
	return h.srv.tgSend(chatID, "浣熊已就绪。发送链接即可加入下载队列，/help 查看全部命令。")
}

func (h *tgHandler) OnAdd(ctx context.Context, chatID int64, user *tgbot.User, url string) (string, error) {
	// 复用 Web 的同一提交路径（同一套校验：SSRF 白名单、去重、引擎可用性）。
	if err := validateOutboundURL(url); err != nil {
		return "", err
	}
	results, err := h.srv.tasks.Submit([]task.SubmitItem{{URL: url}}, task.SourceTelegram, strconv.FormatInt(chatID, 10))
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "提交失败", nil
	}
	if results[0].ExistingID != "" {
		return "队列中已存在相同任务（" + results[0].ExistingID + "），不重复下载", nil
	}
	id := ""
	if results[0].Task != nil {
		id = results[0].Task.ID
	}
	// 记录 task → chat 映射，完成/失败时主动通知。
	h.srv.rememberTGChat(id, chatID)
	return "已入队（任务 " + id + "），用 /status " + id + " 查进度", nil
}

func (h *tgHandler) OnStatus(ctx context.Context, chatID int64, user *tgbot.User, taskID string) (string, error) {
	if taskID == "" {
		return h.listRecentTasks()
	}
	t, err := h.srv.store.GetTask(taskID)
	if err != nil {
		return "任务不存在", nil
	}
	return fmt.Sprintf("%s | 状态 %s | 进度 %.1f%%", t.Title, t.Status, t.Progress), nil
}

func (h *tgHandler) OnList(ctx context.Context, chatID int64, user *tgbot.User) (string, error) {
	return h.listRecentTasks()
}

func (h *tgHandler) OnCancel(ctx context.Context, chatID int64, user *tgbot.User, taskID string) (string, error) {
	if err := h.srv.tasks.Cancel(taskID); err != nil {
		return "取消失败：" + err.Error(), nil
	}
	return "已取消 " + taskID, nil
}

func (h *tgHandler) OnMedia(ctx context.Context, chatID int64, user *tgbot.User, query string) (string, error) {
	items, _, err := h.srv.store.ListMediaFiles(store.FileFilter{Query: query, Limit: 10})
	if err != nil {
		return "媒体库查询失败", nil
	}
	if len(items) == 0 {
		return "没有匹配的媒体", nil
	}
	var sb strings.Builder
	sb.WriteString("媒体库匹配 " + strconv.Itoa(len(items)) + " 项：\n")
	for _, m := range items {
		sb.WriteString("- " + m.Name + " (" + strconv.FormatInt(m.Size, 10) + " 字节)\n")
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// listRecentTasks 列出最近任务（TG 端无表格，输出纯文本）。
func (h *tgHandler) listRecentTasks() (string, error) {
	res, err := h.srv.store.ListTasks(store.TaskFilter{Limit: 10})
	if err != nil {
		return "任务列表读取失败", nil
	}
	if len(res.Items) == 0 {
		return "队列为空", nil
	}
	var sb strings.Builder
	sb.WriteString("最近任务：\n")
	for _, t := range res.Items {
		sb.WriteString("- " + t.ID + " | " + t.Title + " | " + t.Status + "\n")
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// tgSend 发送 TG 消息（供 tgHandler 使用）。
func (s *Server) tgSend(chatID int64, text string) error {
	if s.tgClient == nil {
		return nil
	}
	_, err := s.tgClient.SendMessage(context.Background(), chatID, text, tgbot.ParseModeNone)
	return err
}

// rememberTGChat 记录 task → chat 映射（任务终态时通知）。
func (s *Server) rememberTGChat(taskID string, chatID int64) {
	if taskID == "" {
		return
	}
	s.tgMu.Lock()
	defer s.tgMu.Unlock()
	if s.tgChats == nil {
		s.tgChats = map[string]int64{}
	}
	s.tgChats[taskID] = chatID
}

// ---- HTTP 端点 ----

// handleIntegrationStatus 各外部通道的可用性（供前端「连接」页展示）。
func (s *Server) handleIntegrationStatus(w http.ResponseWriter, r *http.Request) error {
	out := map[string]any{
		"qb":       map[string]any{"configured": s.cfg.GetString("qb.url") != ""},
		"n115":     map[string]any{"configured": s.cfg.GetString("n115.access_token") != ""},
		"telegram": map[string]any{"configured": s.tgEnabled()},
	}
	// qB 实时探测（超时 5s，失败不影响其他通道）。
	if c := s.buildQBClient(); c != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := c.IsAvailable(ctx); err != nil {
			out["qb"].(map[string]any)["ok"] = false
			out["qb"].(map[string]any)["message"] = err.Error()
		} else {
			out["qb"].(map[string]any)["ok"] = true
		}
	}
	if c := s.buildN115Client(); c != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := c.IsAvailable(ctx); err != nil {
			out["n115"].(map[string]any)["ok"] = false
			out["n115"].(map[string]any)["message"] = err.Error()
		} else {
			out["n115"].(map[string]any)["ok"] = true
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
	return nil
}

// handleQBTest 测试 qB 连接。
func (s *Server) handleQBTest(w http.ResponseWriter, r *http.Request) error {
	c := s.buildQBClient()
	if c == nil {
		return httpx.Validation("未配置 qBittorrent 连接（需在设置页填写 qB 地址与账号）")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := c.IsAvailable(ctx); err != nil {
		return httpx.Conflict("qBittorrent 连接失败: " + err.Error())
	}
	ver, _ := c.Version(ctx)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "version": ver})
	return nil
}

// handleN115Test 测试 115 连接。
func (s *Server) handleN115Test(w http.ResponseWriter, r *http.Request) error {
	c := s.buildN115Client()
	if c == nil {
		return httpx.Validation("未配置 115 凭据（需在设置页填写 access token）")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := c.IsAvailable(ctx); err != nil {
		return httpx.Conflict("115 连接失败: " + err.Error())
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

// handleTGStatus Telegram 通道状态。
func (s *Server) handleTGStatus(w http.ResponseWriter, r *http.Request) error {
	out := map[string]any{
		"enabled":   s.tgEnabled(),
		"allowlist": len(s.cfg.GetStringList("telegram.allowed_users")),
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
	return nil
}
