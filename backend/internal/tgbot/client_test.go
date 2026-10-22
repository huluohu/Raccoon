package tgbot

// client_test.go：tgbot 包的单测。
//
// 用 httptest 起一个假 Telegram 服务（不 mock http.Client，真实走 HTTP），
// 覆盖：长轮询解析 update、命令解析各命令、user_id 白名单拒绝、
// update_id 去重、sendDocument multipart 结构、错误响应（ok=false）转错误
// 类型、APIError 带 error_code/error_description、offset 推进。
//
// 假服务返回的 JSON 形状按 Telegram Bot API 文档编写，所以这些用例同时
// 起到「把协议契约钉死」的作用：改 api.go 的字段名会直接让用例失败。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- 假 Telegram 服务 ----

// fakeTelegram 假 Telegram 服务。
//
// 按「方法名」（路径最后一段）分发：路径形如 /bot<token>/<method>。
type fakeTelegram struct {
	t      *testing.T
	server *httptest.Server

	mu        sync.Mutex
	handlers  map[string]http.HandlerFunc
	requests  []fakeRequest
	sentText  []string
	documents []fakeDocument
	answered  []string
}

// fakeRequest 一次请求的记录。
type fakeRequest struct {
	// Method Telegram 方法名（getUpdates / sendMessage / ...）。
	Method string
	// Query URL 查询参数。
	Query map[string][]string
	// Form 表单参数（POST + form 编码时非空）。
	Form map[string][]string
}

// fakeDocument 一次文件上传的记录。
type fakeDocument struct {
	// FileName 上传的文件名。
	FileName string
	// Content 文件内容。
	Content string
	// ChatID 目标会话。
	ChatID string
	// Caption 说明文字。
	Caption string
}

// newFakeTelegram 起一个假 Telegram 服务。
func newFakeTelegram(t *testing.T) *fakeTelegram {
	t.Helper()
	f := &fakeTelegram{t: t, handlers: map[string]http.HandlerFunc{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

// serve 分发请求。
func (f *fakeTelegram) serve(w http.ResponseWriter, r *http.Request) {
	method := methodOf(r.URL.Path)
	rec := fakeRequest{Method: method, Query: r.URL.Query()}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		_ = r.ParseForm()
		rec.Form = r.PostForm
	}
	f.mu.Lock()
	f.requests = append(f.requests, rec)
	h, ok := f.handlers[method]
	f.mu.Unlock()
	if !ok {
		// 未知方法：返回 ok=false，让调用方拿到明确的 APIError。
		writeTelegramError(w, http.StatusNotFound, 404, "Not Found: method not found")
		return
	}
	h(w, r)
}

// handle 注册某个方法名的处理器。
func (f *fakeTelegram) handle(method string, h http.HandlerFunc) {
	f.mu.Lock()
	f.handlers[method] = h
	f.mu.Unlock()
}

// okJSON 回一个成功响应。
func okJSON(w http.ResponseWriter, result any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
}

// writeTelegramError 回一个 Telegram 风格的错误响应。
func writeTelegramError(w http.ResponseWriter, status, code int, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":          false,
		"error_code":  code,
		"description": desc,
	})
}

// url 返回假服务地址（客户端 BaseURL 用它）。
func (f *fakeTelegram) url() string { return f.server.URL }

// requestsOf 返回某方法的全部请求记录。
func (f *fakeTelegram) requestsOf(method string) []fakeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fakeRequest
	for _, r := range f.requests {
		if r.Method == method {
			out = append(out, r)
		}
	}
	return out
}

// methodOf 从路径里取出方法名：/bot<token>/<method> → <method>。
func methodOf(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return path
	}
	return path[idx+1:]
}

// ---- 测试替身 ----

// stubHandler 记录调用参数的 Handler 替身。
type stubHandler struct {
	mu sync.Mutex
	// startCalls /start 调用次数。
	startCalls int
	// addURLs /add 收到的链接。
	addURLs []string
	// statusIDs /status 收到的任务 ID。
	statusIDs []string
	// listCalls /list 调用次数。
	listCalls int
	// cancelIDs /cancel 收到的任务 ID。
	cancelIDs []string
	// mediaQueries /media 收到的检索词。
	mediaQueries []string
	// err 非 nil 时所有方法都返回它。
	err error
	// reply 各方法的回执文本。
	reply string
}

// OnStart 实现 Handler。
func (h *stubHandler) OnStart(ctx context.Context, chatID int64, user *User) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.startCalls++
	return h.err
}

// OnAdd 实现 Handler。
func (h *stubHandler) OnAdd(ctx context.Context, chatID int64, user *User, url string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.addURLs = append(h.addURLs, url)
	if h.err != nil {
		return "", h.err
	}
	return "已提交: " + url, nil
}

// OnStatus 实现 Handler。
func (h *stubHandler) OnStatus(ctx context.Context, chatID int64, user *User, taskID string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.statusIDs = append(h.statusIDs, taskID)
	if h.err != nil {
		return "", h.err
	}
	return "状态: " + taskID, nil
}

// OnList 实现 Handler。
func (h *stubHandler) OnList(ctx context.Context, chatID int64, user *User) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.listCalls++
	if h.err != nil {
		return "", h.err
	}
	return "任务列表", nil
}

// OnCancel 实现 Handler。
func (h *stubHandler) OnCancel(ctx context.Context, chatID int64, user *User, taskID string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cancelIDs = append(h.cancelIDs, taskID)
	if h.err != nil {
		return "", h.err
	}
	return "已取消: " + taskID, nil
}

// OnMedia 实现 Handler。
func (h *stubHandler) OnMedia(ctx context.Context, chatID int64, user *User, query string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.mediaQueries = append(h.mediaQueries, query)
	if h.err != nil {
		return "", h.err
	}
	return "检索结果: " + query, nil
}

// newTestClient 起一个指向假服务的客户端。
func newTestClient(t *testing.T, f *fakeTelegram, mutate func(cfg *Config)) *Client {
	t.Helper()
	cfg := Config{
		Token:        "123456:TEST-TOKEN",
		BaseURL:      f.url(),
		AllowedUsers: []int64{1001},
		Logger:       nil,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("构造客户端失败: %v", err)
	}
	return c
}

// ---- 长轮询 ----

// TestGetUpdatesParsesUpdates 长轮询拉取并解析 update。
func TestGetUpdatesParsesUpdates(t *testing.T) {
	f := newFakeTelegram(t)
	f.handle(MethodGetUpdates, func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, []map[string]any{
			{
				"update_id": 100,
				"message": map[string]any{
					"message_id": 7,
					"from": map[string]any{
						"id":         1001,
						"is_bot":     false,
						"first_name": "张三",
						"username":   "zhangsan",
					},
					"chat": map[string]any{
						"id":   2001,
						"type": "private",
					},
					"date": 1700000000,
					"text": "/start",
				},
			},
		})
	})
	c := newTestClient(t, f, nil)

	updates, err := c.GetUpdates(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("拉取更新失败: %v", err)
	}
	if len(updates) != 1 {
		t.Fatalf("期望 1 条更新，实际 %d 条", len(updates))
	}
	u := updates[0]
	if u.UpdateID != 100 {
		t.Errorf("update_id 期望 100，实际 %d", u.UpdateID)
	}
	msg := u.EffectiveMessage()
	if msg == nil {
		t.Fatal("有效消息为空")
	}
	if msg.Text != "/start" {
		t.Errorf("文本期望 /start，实际 %q", msg.Text)
	}
	if msg.From == nil || msg.From.ID != 1001 {
		t.Errorf("发送者期望 1001，实际 %+v", msg.From)
	}
	if msg.Chat.ID != 2001 {
		t.Errorf("chat_id 期望 2001，实际 %d", msg.Chat.ID)
	}
}

// TestGetUpdatesSendsOffsetAndTimeout 长轮询把 offset 与 timeout 传给服务端。
func TestGetUpdatesSendsOffsetAndTimeout(t *testing.T) {
	f := newFakeTelegram(t)
	f.handle(MethodGetUpdates, func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, []any{})
	})
	c := newTestClient(t, f, nil)

	if _, err := c.GetUpdates(context.Background(), 555, 5*time.Second); err != nil {
		t.Fatalf("拉取更新失败: %v", err)
	}
	reqs := f.requestsOf(MethodGetUpdates)
	if len(reqs) != 1 {
		t.Fatalf("期望 1 次 getUpdates，实际 %d 次", len(reqs))
	}
	if got := reqs[0].Query["offset"]; len(got) != 1 || got[0] != "555" {
		t.Errorf("offset 期望 555，实际 %v", got)
	}
	if got := reqs[0].Query["timeout"]; len(got) != 1 || got[0] != "5" {
		t.Errorf("timeout 期望 5，实际 %v", got)
	}
}

// TestOffsetAdvances 轮询循环把 offset 推进到已处理 update_id+1。
func TestOffsetAdvances(t *testing.T) {
	f := newFakeTelegram(t)
	var calls int
	var mu sync.Mutex
	f.handle(MethodGetUpdates, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			okJSON(w, []map[string]any{{"update_id": 42, "message": map[string]any{
				"message_id": 1,
				"from":       map[string]any{"id": 1001},
				"chat":       map[string]any{"id": 2001},
				"text":       "/help",
			}}})
			return
		}
		okJSON(w, []any{})
	})
	c := newTestClient(t, f, nil)
	b := NewBot(c, &stubHandler{})

	// 手动跑两轮：第一轮拿到 update 42，第二轮验证 offset 已推进。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates, err := c.GetUpdates(ctx, b.Offset(), 0)
	if err != nil {
		t.Fatalf("拉取更新失败: %v", err)
	}
	for _, u := range updates {
		b.advanceOffset(u.UpdateID)
	}
	if got := b.Offset(); got != 43 {
		t.Errorf("offset 期望 43，实际 %d", got)
	}
	// 第二次请求必须带上 43，服务端才不会再重发 update 42。
	if _, err := c.GetUpdates(ctx, b.Offset(), 0); err != nil {
		t.Fatalf("第二次拉取失败: %v", err)
	}
	reqs := f.requestsOf(MethodGetUpdates)
	if len(reqs) != 2 {
		t.Fatalf("期望 2 次 getUpdates，实际 %d 次", len(reqs))
	}
	if got := reqs[1].Query["offset"]; len(got) != 1 || got[0] != "43" {
		t.Errorf("第二次 offset 期望 43，实际 %v", got)
	}
}

// ---- 命令解析 ----

// TestParseCommand 逐个命令验证解析结果。
func TestParseCommand(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		wantCmd Command
		wantArg string
		wantNil bool
		wantErr error
	}{
		{name: "start", text: "/start", wantCmd: CmdStart, wantArg: ""},
		{name: "help", text: "/help", wantCmd: CmdHelp},
		{name: "list", text: "/list", wantCmd: CmdList},
		{name: "status 无参", text: "/status", wantCmd: CmdStatus, wantArg: ""},
		{name: "status 带任务ID", text: "/status tsk_123", wantCmd: CmdStatus, wantArg: "tsk_123"},
		{name: "add 带链接", text: "/add https://example.com/a.mp4", wantCmd: CmdAdd, wantArg: "https://example.com/a.mp4"},
		{name: "cancel 带ID", text: "/cancel tsk_9", wantCmd: CmdCancel, wantArg: "tsk_9"},
		{name: "media 多词", text: "/media 老片 修复", wantCmd: CmdMedia, wantArg: "老片 修复"},
		{name: "带 bot 后缀", text: "/add@RaccoonBot https://x.test/f", wantCmd: CmdAdd, wantArg: "https://x.test/f"},
		{name: "大小写不敏感", text: "/ADD https://y.test/g", wantCmd: CmdAdd, wantArg: "https://y.test/g"},
		{name: "非命令", text: "你好", wantNil: true},
		{name: "空文本", text: "   ", wantNil: true},
		{name: "未知命令", text: "/nope", wantErr: ErrInvalidCommand},
		{name: "add 缺参数", text: "/add", wantErr: ErrMissingArgument},
		{name: "cancel 缺参数", text: "/cancel", wantErr: ErrMissingArgument},
		{name: "media 缺参数", text: "/media", wantErr: ErrMissingArgument},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := ParseCommand(tc.text)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("期望错误 %v，实际 %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("解析出错: %v", err)
			}
			if tc.wantNil {
				if req != nil {
					t.Fatalf("期望 nil 请求，实际 %+v", req)
				}
				return
			}
			if req == nil {
				t.Fatal("期望解析出命令，实际 nil")
			}
			if req.Command != tc.wantCmd {
				t.Errorf("命令期望 %s，实际 %s", tc.wantCmd, req.Command)
			}
			if req.Arg != tc.wantArg {
				t.Errorf("参数期望 %q，实际 %q", tc.wantArg, req.Arg)
			}
		})
	}
}

// ---- 白名单 ----

// TestUnauthorizedUserRejected 白名单外的用户被拒绝。
func TestUnauthorizedUserRejected(t *testing.T) {
	f := newFakeTelegram(t)
	f.handle(MethodSendMessage, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		okJSON(w, map[string]any{"message_id": 1})
	})
	c := newTestClient(t, f, nil)
	h := &stubHandler{}
	b := NewBot(c, h)

	update := Update{
		UpdateID: 900,
		Message: &Message{
			MessageID: 1,
			From:      &User{ID: 6666, FirstName: "陌生人"},
			Chat:      Chat{ID: 2001, Type: "private"},
			Text:      "/add https://evil.test/x",
		},
	}
	err := b.HandleUpdate(context.Background(), update)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("期望 ErrUnauthorized，实际 %v", err)
	}
	// 未授权时绝不能触发任何业务动作。
	h.mu.Lock()
	addCount := len(h.addURLs)
	h.mu.Unlock()
	if addCount != 0 {
		t.Errorf("未授权用户不应触发 OnAdd，实际调用 %d 次", addCount)
	}
	// 但要回一句拒绝。
	reqs := f.requestsOf(MethodSendMessage)
	if len(reqs) != 1 {
		t.Fatalf("期望 1 次回话，实际 %d 次", len(reqs))
	}
	text := reqs[0].Form["text"]
	if len(text) != 1 || !strings.Contains(text[0], "未授权") {
		t.Errorf("期望回复含「未授权」，实际 %v", text)
	}
}

// TestAllowedUserAccepted 白名单内的用户可以提交任务。
func TestAllowedUserAccepted(t *testing.T) {
	f := newFakeTelegram(t)
	f.handle(MethodSendMessage, func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, map[string]any{"message_id": 1})
	})
	c := newTestClient(t, f, nil)
	h := &stubHandler{}
	b := NewBot(c, h)

	update := Update{
		UpdateID: 901,
		Message: &Message{
			MessageID: 2,
			From:      &User{ID: 1001, FirstName: "张三"},
			Chat:      Chat{ID: 2001, Type: "private"},
			Text:      "/add https://example.com/a.mp4",
		},
	}
	if err := b.HandleUpdate(context.Background(), update); err != nil {
		t.Fatalf("处理更新失败: %v", err)
	}
	h.mu.Lock()
	got := h.addURLs
	h.mu.Unlock()
	if len(got) != 1 || got[0] != "https://example.com/a.mp4" {
		t.Errorf("期望提交 https://example.com/a.mp4，实际 %v", got)
	}
}

// TestChatWhitelist 白名单外的会话被拒绝（即使用户在白名单内）。
func TestChatWhitelist(t *testing.T) {
	f := newFakeTelegram(t)
	f.handle(MethodSendMessage, func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, map[string]any{"message_id": 1})
	})
	c := newTestClient(t, f, func(cfg *Config) {
		cfg.AllowedChats = []int64{2001}
	})
	b := NewBot(c, &stubHandler{})

	update := Update{
		UpdateID: 902,
		Message: &Message{
			MessageID: 3,
			From:      &User{ID: 1001},
			Chat:      Chat{ID: 3003, Type: "group"},
			Text:      "/list",
		},
	}
	if err := b.HandleUpdate(context.Background(), update); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("期望 ErrUnauthorized，实际 %v", err)
	}
}

// ---- 去重 ----

// TestRunLoopProcessesUpdates 轮询循环端到端：拉取 → 分发 → 推进 offset。
//
// 用一个可取消的 context 控制循环退出，并让假服务在收到第二条 update 后
// 立刻返回空数组，把轮询变成「转两圈就停」。
func TestRunLoopProcessesUpdates(t *testing.T) {
	f := newFakeTelegram(t)
	var calls int
	var mu sync.Mutex
	f.handle(MethodGetUpdates, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		switch n {
		case 1:
			okJSON(w, []map[string]any{{"update_id": 500, "message": map[string]any{
				"message_id": 1,
				"from":       map[string]any{"id": 1001},
				"chat":       map[string]any{"id": 2001},
				"text":       "/add https://example.com/run.mp4",
			}}})
		default:
			okJSON(w, []any{})
		}
	})
	f.handle(MethodSendMessage, func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, map[string]any{"message_id": 1})
	})
	c := newTestClient(t, f, nil)
	h := &stubHandler{}
	b := NewBot(c, h)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// 跑几轮后取消：轮询会阻塞在 getUpdates 上，cancel 让它立刻返回。
	go func() {
		deadline := time.Now().Add(300 * time.Millisecond)
		for time.Now().Before(deadline) {
			if len(f.requestsOf(MethodGetUpdates)) >= 2 {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		cancel()
	}()
	_ = b.Run(ctx)

	h.mu.Lock()
	addURLs := append([]string(nil), h.addURLs...)
	h.mu.Unlock()
	if len(addURLs) != 1 || addURLs[0] != "https://example.com/run.mp4" {
		t.Errorf("期望提交一次 https://example.com/run.mp4，实际 %v", addURLs)
	}
	// offset 必须推进到 501，否则服务端会重发 update 500。
	if got := b.Offset(); got != 501 {
		t.Errorf("offset 期望 501，实际 %d", got)
	}
}

// TestDuplicateUpdateIgnored 相同 update_id 只处理一次。
func TestDuplicateUpdateIgnored(t *testing.T) {
	f := newFakeTelegram(t)
	f.handle(MethodSendMessage, func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, map[string]any{"message_id": 1})
	})
	c := newTestClient(t, f, nil)
	h := &stubHandler{}
	b := NewBot(c, h)

	update := Update{
		UpdateID: 1234,
		Message: &Message{
			MessageID: 4,
			From:      &User{ID: 1001},
			Chat:      Chat{ID: 2001},
			Text:      "/add https://example.com/dup.mp4",
		},
	}
	if err := b.HandleUpdate(context.Background(), update); err != nil {
		t.Fatalf("首次处理失败: %v", err)
	}
	err := b.HandleUpdate(context.Background(), update)
	if !errors.Is(err, ErrDuplicateUpdate) {
		t.Fatalf("期望 ErrDuplicateUpdate，实际 %v", err)
	}
	h.mu.Lock()
	addCount := len(h.addURLs)
	h.mu.Unlock()
	if addCount != 1 {
		t.Errorf("重复 update 应只处理 1 次，实际 %d 次", addCount)
	}
}

// TestSeenSetEvictsOldest 去重窗口满了之后淘汰最旧的 update_id。
func TestSeenSetEvictsOldest(t *testing.T) {
	s := newSeenSet(3)
	if s.Seen(1) || s.Seen(2) || s.Seen(3) {
		t.Fatal("首次出现的 update 不应被判为重复")
	}
	if !s.Seen(1) {
		t.Error("update 1 应仍在窗口内被判为重复")
	}
	// 写入 4 会淘汰最旧的 1。
	s.Seen(4)
	if s.Len() != 3 {
		t.Errorf("窗口大小期望 3，实际 %d", s.Len())
	}
	if s.Seen(1) {
		t.Error("update 1 应已被淘汰，不再判为重复")
	}
}

// ---- 文件上传 ----

// TestSendDocumentMultipart sendDocument 走 multipart 且结构正确。
func TestSendDocumentMultipart(t *testing.T) {
	f := newFakeTelegram(t)
	f.handle(MethodSendDocument, func(w http.ResponseWriter, r *http.Request) {
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Errorf("解析 Content-Type 失败: %v", err)
			writeTelegramError(w, http.StatusBadRequest, 400, "bad content type")
			return
		}
		if mediaType != "multipart/form-data" {
			t.Errorf("期望 multipart/form-data，实际 %s", mediaType)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		doc := fakeDocument{}
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("读取 multipart 分段失败: %v", err)
				break
			}
			data, _ := io.ReadAll(part)
			switch part.FormName() {
			case "chat_id":
				doc.ChatID = string(data)
			case "caption":
				doc.Caption = string(data)
			case "document":
				doc.FileName = part.FileName()
				doc.Content = string(data)
			}
		}
		f.mu.Lock()
		f.documents = append(f.documents, doc)
		f.mu.Unlock()
		okJSON(w, map[string]any{"message_id": 5, "document": map[string]any{"file_id": "f1"}})
	})
	c := newTestClient(t, f, nil)

	// 造一个真实文件，走真实的文件上传路径。
	dir := t.TempDir()
	path := filepath.Join(dir, "报告.txt")
	content := "hello telegram"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写临时文件失败: %v", err)
	}

	msg, err := c.SendDocument(context.Background(), 2001, path, "月度报告")
	if err != nil {
		t.Fatalf("发送文件失败: %v", err)
	}
	if msg == nil || msg.MessageID != 5 {
		t.Errorf("期望返回 message_id=5，实际 %+v", msg)
	}
	f.mu.Lock()
	docs := f.documents
	f.mu.Unlock()
	if len(docs) != 1 {
		t.Fatalf("期望 1 次文件上传，实际 %d 次", len(docs))
	}
	d := docs[0]
	if d.FileName != "报告.txt" {
		t.Errorf("文件名期望 报告.txt，实际 %q", d.FileName)
	}
	if d.Content != content {
		t.Errorf("文件内容期望 %q，实际 %q", content, d.Content)
	}
	if d.ChatID != "2001" {
		t.Errorf("chat_id 期望 2001，实际 %q", d.ChatID)
	}
	if d.Caption != "月度报告" {
		t.Errorf("caption 期望 月度报告，实际 %q", d.Caption)
	}
}

// TestSendDocumentMissingFile 文件不存在时返回错误（不发起请求）。
func TestSendDocumentMissingFile(t *testing.T) {
	f := newFakeTelegram(t)
	c := newTestClient(t, f, nil)
	if _, err := c.SendDocument(context.Background(), 2001, filepath.Join(t.TempDir(), "nope.txt"), ""); err == nil {
		t.Fatal("期望文件不存在时报错，实际 nil")
	}
	if len(f.requestsOf(MethodSendDocument)) != 0 {
		t.Error("文件不存在时不应发起 HTTP 请求")
	}
}

// ---- 错误处理 ----

// TestAPIErrorFromOkFalse ok=false 的响应转成 *APIError。
func TestAPIErrorFromOkFalse(t *testing.T) {
	f := newFakeTelegram(t)
	f.handle(MethodSendMessage, func(w http.ResponseWriter, r *http.Request) {
		writeTelegramError(w, http.StatusBadRequest, 400, "Bad Request: chat not found")
	})
	c := newTestClient(t, f, nil)

	_, err := c.SendMessage(context.Background(), 2001, "hi", ParseModeNone)
	if err == nil {
		t.Fatal("期望出错，实际 nil")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("期望 *APIError，实际 %T (%v)", err, err)
	}
	if apiErr.ErrorCode != 400 {
		t.Errorf("error_code 期望 400，实际 %d", apiErr.ErrorCode)
	}
	if apiErr.ErrorDescription != "Bad Request: chat not found" {
		t.Errorf("error_description 期望 Bad Request: chat not found，实际 %q", apiErr.ErrorDescription)
	}
	if apiErr.Method != MethodSendMessage {
		t.Errorf("method 期望 %s，实际 %s", MethodSendMessage, apiErr.Method)
	}
}

// TestAPIErrorCarriesFields APIError 的字段齐全且 Error() 含关键信息。
func TestAPIErrorCarriesFields(t *testing.T) {
	f := newFakeTelegram(t)
	f.handle(MethodSendMessage, func(w http.ResponseWriter, r *http.Request) {
		writeTelegramError(w, http.StatusUnauthorized, 401, "Unauthorized")
	})
	c := newTestClient(t, f, nil)

	_, err := c.SendMessage(context.Background(), 2001, "hi", ParseModeNone)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("期望 *APIError，实际 %T", err)
	}
	if apiErr.ErrorCode != 401 {
		t.Errorf("error_code 期望 401，实际 %d", apiErr.ErrorCode)
	}
	if !strings.Contains(apiErr.Error(), "401") {
		t.Errorf("错误消息应含错误码，实际 %q", apiErr.Error())
	}
	if !strings.Contains(apiErr.Error(), "Unauthorized") {
		t.Errorf("错误消息应含错误描述，实际 %q", apiErr.Error())
	}
}

// TestAPIErrorRetryAfter 429 响应带上 retry_after，并且按它退避后重试。
//
// retry_after 故意设成 1 秒：退避逻辑确实按 Telegram 说的等，但测试不能
// 为了验证一个字段而等上几十秒。
func TestAPIErrorRetryAfter(t *testing.T) {
	f := newFakeTelegram(t)
	f.handle(MethodSendMessage, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":          false,
			"error_code":  429,
			"description": "Too Many Requests: retry after 1",
			"parameters":  map[string]any{"retry_after": 1},
		})
	})
	c := newTestClient(t, f, func(cfg *Config) {
		cfg.MaxRetries = 1
		cfg.RetryBackoff = 10 * time.Millisecond
	})

	_, err := c.SendMessage(context.Background(), 2001, "hi", ParseModeNone)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("期望 *APIError，实际 %T", err)
	}
	if apiErr.RetryAfterSeconds != 1 {
		t.Errorf("retry_after 期望 1，实际 %d", apiErr.RetryAfterSeconds)
	}
	if apiErr.ErrorCode != 429 {
		t.Errorf("error_code 期望 429，实际 %d", apiErr.ErrorCode)
	}
	// 限流可安全重试：应该真的重试了一次。
	if got := len(f.requestsOf(MethodSendMessage)); got != 2 {
		t.Errorf("期望 2 次请求（1 次重试），实际 %d 次", got)
	}
}

// TestGetUpdatesRetriesOnServerError getUpdates 遇到 5xx 会退避重试。
//
// getUpdates 是只读接口，重试没有副作用，所以网络/服务端故障都值得重试；
// 而 sendMessage 这类发送接口在 5xx 时不重试（可能已发出去了，重试会重复发）。
func TestGetUpdatesRetriesOnServerError(t *testing.T) {
	f := newFakeTelegram(t)
	var calls int
	var mu sync.Mutex
	f.handle(MethodGetUpdates, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n < 3 {
			writeTelegramError(w, http.StatusInternalServerError, 500, "Internal Server Error")
			return
		}
		okJSON(w, []any{})
	})
	c := newTestClient(t, f, func(cfg *Config) {
		cfg.MaxRetries = 3
		cfg.RetryBackoff = 10 * time.Millisecond
	})

	if _, err := c.GetUpdates(context.Background(), 0, 0); err != nil {
		t.Fatalf("重试后应成功，实际出错: %v", err)
	}
	if got := len(f.requestsOf(MethodGetUpdates)); got != 3 {
		t.Errorf("期望 3 次请求（2 次重试），实际 %d 次", got)
	}
}

// TestSendMessageDoesNotRetryOnServerError 发送类接口遇到 5xx 不重试。
//
// 服务端可能已经处理了请求只是响应丢了，重试会发重复消息——宁可让上层
// 感知到失败，也不制造重复。
func TestSendMessageDoesNotRetryOnServerError(t *testing.T) {
	f := newFakeTelegram(t)
	f.handle(MethodSendMessage, func(w http.ResponseWriter, r *http.Request) {
		writeTelegramError(w, http.StatusInternalServerError, 500, "Internal Server Error")
	})
	c := newTestClient(t, f, func(cfg *Config) {
		cfg.MaxRetries = 3
		cfg.RetryBackoff = 10 * time.Millisecond
	})

	if _, err := c.SendMessage(context.Background(), 2001, "hi", ParseModeNone); err == nil {
		t.Fatal("期望出错，实际 nil")
	}
	if got := len(f.requestsOf(MethodSendMessage)); got != 1 {
		t.Errorf("发送类接口不应重试，实际请求 %d 次", got)
	}
}

// TestTokenNotLeakedInError 错误消息里不能出现 Bot Token。
func TestTokenNotLeakedInError(t *testing.T) {
	f := newFakeTelegram(t)
	// 让服务直接断连，触发传输层错误（*url.Error 里带完整 URL）。
	f.handle(MethodSendMessage, func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	})
	c := newTestClient(t, f, nil)

	_, err := c.SendMessage(context.Background(), 2001, "hi", ParseModeNone)
	if err == nil {
		t.Fatal("期望出错，实际 nil")
	}
	if strings.Contains(err.Error(), "TEST-TOKEN") {
		t.Errorf("错误消息泄露了 Token: %q", err.Error())
	}
}

// TestProtocolErrorOnGarbageResponse 响应不是 Telegram 报文时返回 *ProtocolError。
//
// 与 APIError 区分：APIError 是「Telegram 明确报错」（有 error_code），
// ProtocolError 是「根本没拿到能理解的响应」（比如中间代理返回了 HTML）。
func TestProtocolErrorOnGarbageResponse(t *testing.T) {
	f := newFakeTelegram(t)
	f.handle(MethodSendMessage, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>proxy error</body></html>"))
	})
	c := newTestClient(t, f, nil)

	_, err := c.SendMessage(context.Background(), 2001, "hi", ParseModeNone)
	if err == nil {
		t.Fatal("期望出错，实际 nil")
	}
	var protoErr *ProtocolError
	if !errors.As(err, &protoErr) {
		t.Fatalf("期望 *ProtocolError，实际 %T (%v)", err, err)
	}
	if protoErr.Method != MethodSendMessage {
		t.Errorf("method 期望 %s，实际 %s", MethodSendMessage, protoErr.Method)
	}
	if !strings.Contains(protoErr.Body, "proxy error") {
		t.Errorf("响应体摘要应含原始内容，实际 %q", protoErr.Body)
	}
}

// TestEmptyTokenRejected 空 Token 直接拒绝构造。
func TestEmptyTokenRejected(t *testing.T) {
	if _, err := New(Config{}); !errors.Is(err, ErrEmptyToken) {
		t.Fatalf("期望 ErrEmptyToken，实际 %v", err)
	}
}

// ---- 命令分发 ----

// TestDispatchAllCommands 逐个命令验证分发到对应 Handler 方法。
func TestDispatchAllCommands(t *testing.T) {
	cases := []struct {
		name string
		text string
		// check 校验 handler 收到的调用。
		check func(h *stubHandler) error
	}{
		{
			name: "start",
			text: "/start",
			check: func(h *stubHandler) error {
				if h.startCalls != 1 {
					return fmt.Errorf("OnStart 调用 %d 次", h.startCalls)
				}
				return nil
			},
		},
		{
			name: "help",
			text: "/help",
			check: func(h *stubHandler) error {
				if h.listCalls != 0 || h.startCalls != 0 {
					return errors.New("help 不应触发其它回调")
				}
				return nil
			},
		},
		{
			name: "list",
			text: "/list",
			check: func(h *stubHandler) error {
				if h.listCalls != 1 {
					return fmt.Errorf("OnList 调用 %d 次", h.listCalls)
				}
				return nil
			},
		},
		{
			name: "status",
			text: "/status tsk_7",
			check: func(h *stubHandler) error {
				if len(h.statusIDs) != 1 || h.statusIDs[0] != "tsk_7" {
					return fmt.Errorf("OnStatus 收到 %v", h.statusIDs)
				}
				return nil
			},
		},
		{
			name: "add",
			text: "/add https://example.com/x.mp4",
			check: func(h *stubHandler) error {
				if len(h.addURLs) != 1 || h.addURLs[0] != "https://example.com/x.mp4" {
					return fmt.Errorf("OnAdd 收到 %v", h.addURLs)
				}
				return nil
			},
		},
		{
			name: "cancel",
			text: "/cancel tsk_3",
			check: func(h *stubHandler) error {
				if len(h.cancelIDs) != 1 || h.cancelIDs[0] != "tsk_3" {
					return fmt.Errorf("OnCancel 收到 %v", h.cancelIDs)
				}
				return nil
			},
		},
		{
			name: "media",
			text: "/media 修复 老片",
			check: func(h *stubHandler) error {
				if len(h.mediaQueries) != 1 || h.mediaQueries[0] != "修复 老片" {
					return fmt.Errorf("OnMedia 收到 %v", h.mediaQueries)
				}
				return nil
			},
		},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeTelegram(t)
			f.handle(MethodSendMessage, func(w http.ResponseWriter, r *http.Request) {
				okJSON(w, map[string]any{"message_id": 1})
			})
			c := newTestClient(t, f, nil)
			h := &stubHandler{}
			b := NewBot(c, h)
			update := Update{
				UpdateID: int64(2000 + i),
				Message: &Message{
					MessageID: int64(i + 1),
					From:      &User{ID: 1001},
					Chat:      Chat{ID: 2001, Type: "private"},
					Text:      tc.text,
				},
			}
			if err := b.HandleUpdate(context.Background(), update); err != nil {
				t.Fatalf("处理更新失败: %v", err)
			}
			h.mu.Lock()
			defer h.mu.Unlock()
			if err := tc.check(h); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestHandlerErrorReplies 命令处理出错时回一句错误。
func TestHandlerErrorReplies(t *testing.T) {
	f := newFakeTelegram(t)
	f.handle(MethodSendMessage, func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, map[string]any{"message_id": 1})
	})
	c := newTestClient(t, f, nil)
	h := &stubHandler{err: errors.New("任务系统不可用")}
	b := NewBot(c, h)

	update := Update{
		UpdateID: 3000,
		Message: &Message{
			MessageID: 9,
			From:      &User{ID: 1001},
			Chat:      Chat{ID: 2001},
			Text:      "/add https://example.com/y.mp4",
		},
	}
	if err := b.HandleUpdate(context.Background(), update); err == nil {
		t.Fatal("期望返回错误，实际 nil")
	}
	reqs := f.requestsOf(MethodSendMessage)
	if len(reqs) != 1 {
		t.Fatalf("期望 1 次回话，实际 %d 次", len(reqs))
	}
	text := reqs[0].Form["text"]
	if len(text) != 1 || !strings.Contains(text[0], "任务系统不可用") {
		t.Errorf("期望回复含错误信息，实际 %v", text)
	}
}

// TestUnknownCommandRepliesHelp 未知命令回帮助。
func TestUnknownCommandRepliesHelp(t *testing.T) {
	f := newFakeTelegram(t)
	f.handle(MethodSendMessage, func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, map[string]any{"message_id": 1})
	})
	c := newTestClient(t, f, nil)
	b := NewBot(c, &stubHandler{})

	update := Update{
		UpdateID: 3001,
		Message: &Message{
			MessageID: 10,
			From:      &User{ID: 1001},
			Chat:      Chat{ID: 2001},
			Text:      "/nosuch",
		},
	}
	if err := b.HandleUpdate(context.Background(), update); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("期望 ErrInvalidCommand，实际 %v", err)
	}
	reqs := f.requestsOf(MethodSendMessage)
	if len(reqs) != 1 {
		t.Fatalf("期望 1 次回话，实际 %d 次", len(reqs))
	}
	text := reqs[0].Form["text"]
	if len(text) != 1 || !strings.Contains(text[0], "未知命令") {
		t.Errorf("期望回复含「未知命令」，实际 %v", text)
	}
}

// TestNonCommandIgnored 普通聊天内容不触发任何处理。
func TestNonCommandIgnored(t *testing.T) {
	f := newFakeTelegram(t)
	c := newTestClient(t, f, nil)
	h := &stubHandler{}
	b := NewBot(c, h)

	update := Update{
		UpdateID: 3002,
		Message: &Message{
			MessageID: 11,
			From:      &User{ID: 1001},
			Chat:      Chat{ID: 2001},
			Text:      "今天天气不错",
		},
	}
	if err := b.HandleUpdate(context.Background(), update); err != nil {
		t.Fatalf("普通消息不应报错: %v", err)
	}
	if len(f.requestsOf(MethodSendMessage)) != 0 {
		t.Error("普通消息不应回话")
	}
}

// TestCallbackQueryAnswered 内联按钮回调会先应答。
func TestCallbackQueryAnswered(t *testing.T) {
	f := newFakeTelegram(t)
	f.handle(MethodAnswerCallbackQuery, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.answered = append(f.answered, r.PostForm.Get("callback_query_id"))
		f.mu.Unlock()
		okJSON(w, true)
	})
	f.handle(MethodSendMessage, func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, map[string]any{"message_id": 1})
	})
	c := newTestClient(t, f, nil)
	b := NewBot(c, &stubHandler{})

	update := Update{
		UpdateID: 3003,
		CallbackQuery: &CallbackQuery{
			ID:   "cbq_1",
			From: &User{ID: 1001},
			Message: &Message{
				MessageID: 12,
				Chat:      Chat{ID: 2001},
			},
			Data: "/list",
		},
	}
	if err := b.HandleUpdate(context.Background(), update); err != nil {
		t.Fatalf("处理回调失败: %v", err)
	}
	f.mu.Lock()
	answered := f.answered
	f.mu.Unlock()
	if len(answered) != 1 || answered[0] != "cbq_1" {
		t.Errorf("期望应答 cbq_1，实际 %v", answered)
	}
}

// ---- 通知出口 ----

// TestMessageNotifier 通知出口把完成/失败推回聊天窗口。
func TestMessageNotifier(t *testing.T) {
	f := newFakeTelegram(t)
	f.handle(MethodSendMessage, func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, map[string]any{"message_id": 1})
	})
	c := newTestClient(t, f, nil)
	n := NewMessageNotifier(c)

	if err := n.NotifyTaskDone(context.Background(), 2001, "tsk_1", "下载完成", "共 12 个文件"); err != nil {
		t.Fatalf("推送完成通知失败: %v", err)
	}
	if err := n.NotifyTaskFailed(context.Background(), 2001, "tsk_2", "下载失败", "链接超时"); err != nil {
		t.Fatalf("推送失败通知失败: %v", err)
	}
	reqs := f.requestsOf(MethodSendMessage)
	if len(reqs) != 2 {
		t.Fatalf("期望 2 条通知，实际 %d 条", len(reqs))
	}
	done := reqs[0].Form["text"][0]
	if !strings.Contains(done, "任务完成") || !strings.Contains(done, "tsk_1") {
		t.Errorf("完成通知内容不对: %q", done)
	}
	failed := reqs[1].Form["text"][0]
	if !strings.Contains(failed, "任务失败") || !strings.Contains(failed, "tsk_2") {
		t.Errorf("失败通知内容不对: %q", failed)
	}
	// chat_id 必须是字符串形式。
	if got := reqs[0].Form["chat_id"]; len(got) != 1 || got[0] != strconv.FormatInt(2001, 10) {
		t.Errorf("chat_id 期望 2001，实际 %v", got)
	}
}
