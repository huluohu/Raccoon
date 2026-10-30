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
	"io"
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
	"github.com/huluohu/raccoon/internal/wechat"
)

// setupIntegrations 装配外部通道（qB / 115 / Telegram / WeChat）。
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

	// ---- 微信公众平台 / 远程下载 Webhook ----
	s.router.GET("/api/v1/integrations/wechat/webhook", s.handleWeChatVerify)
	s.router.POST("/api/v1/integrations/wechat/webhook", s.handleWeChatMessage)
	s.router.GET("/api/v1/integrations/wechat/status", s.requireAuth(s.handleWeChatStatus))
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

// OnAddBatch 批量提交（裸链接消息 / /add 多链接）：逐条走与 Web 相同的提交路径，
// 单条失败不影响其余；汇总回执一次发出。
func (h *tgHandler) OnAddBatch(ctx context.Context, chatID int64, user *tgbot.User, urls []string) (string, error) {
	var sb strings.Builder
	ok, dup, fail := 0, 0, 0
	for _, u := range urls {
		if err := validateOutboundURL(u); err != nil {
			fail++
			sb.WriteString("\n✕ " + u + "：" + err.Error())
			continue
		}
		results, err := h.srv.tasks.Submit([]task.SubmitItem{{URL: u}}, task.SourceTelegram, strconv.FormatInt(chatID, 10))
		if err != nil || len(results) == 0 {
			fail++
			sb.WriteString("\n✕ " + u + "：提交失败")
			continue
		}
		if results[0].ExistingID != "" {
			dup++
			sb.WriteString("\n= " + u + "：队列中已存在，不重复下载")
			continue
		}
		id := ""
		if results[0].Task != nil {
			id = results[0].Task.ID
			h.srv.rememberTGChat(id, chatID)
		}
		ok++
		sb.WriteString("\n✓ " + u + " → 任务 " + id)
	}
	head := fmt.Sprintf("批量提交：%d 入队 / %d 重复 / %d 失败", ok, dup, fail)
	return head + sb.String(), nil
}

func (h *tgHandler) OnStatus(ctx context.Context, chatID int64, user *tgbot.User, taskID string) (string, error) {
	if taskID == "" {
		return h.listRecentTasks()
	}
	t, err := h.srv.store.GetTask(taskID)
	if err != nil {
		return "任务不存在", nil
	}
	var sb strings.Builder
	sb.WriteString(t.Title + " | 状态 " + t.Status)
	if t.Status == "downloading" {
		sb.WriteString(fmt.Sprintf(" | 进度 %.1f%%", t.Progress))
		if t.SpeedBps > 0 {
			sb.WriteString(" | " + formatSpeedTG(t.SpeedBps))
		}
		// 剩余时间：有速度且有预期大小时才可估；估不出就不写（不编数）。
		if t.SpeedBps > 0 && t.ExpectedSize != nil && *t.ExpectedSize > t.DownloadedSize {
			remain := float64(*t.ExpectedSize-t.DownloadedSize) / float64(t.SpeedBps)
			sb.WriteString(" | 剩余 " + formatDurationTG(int64(remain)))
		}
	} else {
		sb.WriteString(fmt.Sprintf(" | 进度 %.1f%%", t.Progress))
	}
	if t.Status == "waiting" && t.WaitReason == "auto_retry" && t.ScheduledAt != nil {
		sb.WriteString(" | 将自动重试")
	}
	return sb.String(), nil
}

// formatSpeedTG 字节/秒 → 人类可读速度。
func formatSpeedTG(bps int64) string {
	switch {
	case bps >= 1<<20:
		return fmt.Sprintf("%.1f MB/s", float64(bps)/(1<<20))
	case bps >= 1<<10:
		return fmt.Sprintf("%.1f KB/s", float64(bps)/(1<<10))
	default:
		return fmt.Sprintf("%d B/s", bps)
	}
}

// formatDurationTG 秒 → mm:ss / hh:mm:ss。
func formatDurationTG(sec int64) string {
	if sec < 0 {
		sec = 0
	}
	h, m, s := sec/3600, sec%3600/60, sec%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
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
		"wechat":   map[string]any{"configured": strings.TrimSpace(s.cfg.GetString("wechat.token")) != ""},
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

// handleWeChatVerify 微信服务器接入校验（GET /api/v1/integrations/wechat/webhook）。
func (s *Server) handleWeChatVerify(w http.ResponseWriter, r *http.Request) error {
	token := strings.TrimSpace(s.cfg.GetString("wechat.token"))
	q := r.URL.Query()
	signature := q.Get("signature")
	timestamp := q.Get("timestamp")
	nonce := q.Get("nonce")
	echostr := q.Get("echostr")

	if token == "" || !wechat.VerifySignature(token, timestamp, nonce, signature) {
		http.Error(w, "Forbidden: signature verification failed", http.StatusForbidden)
		return nil
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(echostr))
	return nil
}

// handleWeChatMessage 接收并处理微信推送的文本消息（POST /api/v1/integrations/wechat/webhook）。
func (s *Server) handleWeChatMessage(w http.ResponseWriter, r *http.Request) error {
	token := strings.TrimSpace(s.cfg.GetString("wechat.token"))
	q := r.URL.Query()
	signature := q.Get("signature")
	timestamp := q.Get("timestamp")
	nonce := q.Get("nonce")

	if token != "" && !wechat.VerifySignature(token, timestamp, nonce, signature) {
		http.Error(w, "Forbidden: signature verification failed", http.StatusForbidden)
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 256<<10))
	if err != nil {
		return httpx.Validation("读取请求包失败")
	}

	msg, err := wechat.ParseMessage(body)
	if err != nil {
		s.log.Warn("微信消息解析异常", map[string]any{"err": err.Error()})
		// 微信规定必须返回 success 或空串，避免重试轰炸
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
		return nil
	}

	if msg.MsgType != "text" {
		reply, _ := wechat.FormatReplyXML(msg.FromUserName, msg.ToUserName,
			"🦝 浣熊自托管下载助手：\n请向我直接发送视频分享链接，我会自动为您排队下载保存。")
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(reply)
		return nil
	}

	urls := wechat.ExtractURLs(msg.Content)
	if len(urls) == 0 {
		reply, _ := wechat.FormatReplyXML(msg.FromUserName, msg.ToUserName,
			"🦝 浣熊自托管下载助手：\n未检测到有效的视频链接。请直接粘贴包含 http(s):// 的链接。")
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(reply)
		return nil
	}

	// SSRF 校验：微信转发的是**聊天内容**（外部输入），不能因为入口是
	// webhook 就跳过校验——Telegram 路径是有的（validateOutboundURL）。
	// 少这一步等于给微信通道开了个内网探测的口子（云元数据端点等）。
	var items []task.SubmitItem
	var rejected []string
	for _, u := range urls {
		if err := validateOutboundURL(u); err != nil {
			rejected = append(rejected, u+"："+err.Error())
			continue
		}
		items = append(items, task.SubmitItem{URL: u})
	}
	if len(items) == 0 {
		reply, _ := wechat.FormatReplyXML(msg.FromUserName, msg.ToUserName,
			"抱歉，链接不被允许（防 SSRF）：\n"+strings.Join(rejected, "\n"))
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(reply)
		return nil
	}
	results, err := s.tasks.Submit(items, task.SourceAPI, "wechat:"+msg.FromUserName)
	if err != nil {
		s.log.Error("微信链接入队失败", map[string]any{"user": msg.FromUserName, "err": err.Error()})
		reply, _ := wechat.FormatReplyXML(msg.FromUserName, msg.ToUserName,
			"抱歉，下载任务创建失败："+err.Error())
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(reply)
		return nil
	}

	var titles []string
	for _, res := range results {
		if res.Task != nil {
			titles = append(titles, res.Task.URL)
		} else if res.ExistingID != "" {
			titles = append(titles, "(任务已在队列中，未重复添加)")
		}
	}

	replyContent := fmt.Sprintf("🦝 浣熊已收到下载任务（共 %d 个）！\n%s\n任务已入队，下载完成后可至媒体库查看。",
		len(items), strings.Join(titles, "\n"))
	reply, _ := wechat.FormatReplyXML(msg.FromUserName, msg.ToUserName, replyContent)
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(reply)
	return nil
}

// handleWeChatStatus 查询微信集成状态（GET /api/v1/integrations/wechat/status）。
func (s *Server) handleWeChatStatus(w http.ResponseWriter, r *http.Request) error {
	token := strings.TrimSpace(s.cfg.GetString("wechat.token"))
	appid := strings.TrimSpace(s.cfg.GetString("wechat.appid"))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"enabled":     token != "",
		"appid":       appid,
		"webhook_url": "/api/v1/integrations/wechat/webhook",
	})
	return nil
}
