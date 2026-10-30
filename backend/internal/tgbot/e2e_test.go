package tgbot

// e2e_test.go：Telegram 通道端到端验证（复用 client_test.go 的 fakeTelegram）。
//
// 验证我们这一侧的真实代码：长轮询拉 update → 去重 → 鉴权 → 解析命令 →
// 调 Handler → 回话，以及内联按钮回调的应答路径。
//
// 用的是**真实**的 Bot.Run 轮询循环（不是直接调 HandleUpdate），
// 所以 offset 推进、去重窗口、白名单鉴权都在链路里。

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// e2eRecorder 记录 Handler 的调用。
type e2eRecorder struct {
	mu       sync.Mutex
	addURLs  []string
	starts   int
	statuses []string
	cancels  []string
}

func (h *e2eRecorder) OnStart(ctx context.Context, chatID int64, user *User) error {
	h.mu.Lock()
	h.starts++
	h.mu.Unlock()
	return nil
}

func (h *e2eRecorder) OnAdd(ctx context.Context, chatID int64, user *User, url string) (string, error) {
	h.mu.Lock()
	h.addURLs = append(h.addURLs, url)
	h.mu.Unlock()
	return "已入队（任务 tsk_test）", nil
}

func (h *e2eRecorder) OnAddBatch(ctx context.Context, chatID int64, user *User, urls []string) (string, error) {
	h.mu.Lock()
	h.addURLs = append(h.addURLs, urls...)
	h.mu.Unlock()
	return fmt.Sprintf("批量提交：%d 入队", len(urls)), nil
}

func (h *e2eRecorder) OnStatus(ctx context.Context, chatID int64, user *User, id string) (string, error) {
	h.mu.Lock()
	h.statuses = append(h.statuses, id)
	h.mu.Unlock()
	return "进度 50%", nil
}

func (h *e2eRecorder) OnList(ctx context.Context, chatID int64, user *User) (string, error) {
	return "列表", nil
}

func (h *e2eRecorder) OnCancel(ctx context.Context, chatID int64, user *User, id string) (string, error) {
	h.mu.Lock()
	h.cancels = append(h.cancels, id)
	h.mu.Unlock()
	return "已取消", nil
}

func (h *e2eRecorder) OnMedia(ctx context.Context, chatID int64, user *User, q string) (string, error) {
	return "媒体", nil
}

// e2eUpdate 造一条文本消息 update（真实 Update/Message 结构）。
func e2eUpdate(id, chatID, userID int64, text string) Update {
	return Update{
		UpdateID: id,
		Message: &Message{
			MessageID: id,
			From:      &User{ID: userID, FirstName: "U"},
			Chat:      Chat{ID: chatID, Type: "private"},
			Date:      1700000000,
			Text:      text,
		},
	}
}

// setupE2E 起假 Telegram + 客户端 + 分发器。
func setupE2E(t *testing.T, allowedUsers []int64, updates []Update) (*fakeTelegram, *Bot, *e2eRecorder) {
	t.Helper()
	f := newFakeTelegram(t)

	var mu sync.Mutex
	pending := updates
	f.handle("getUpdates", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		out := pending
		pending = nil
		mu.Unlock()
		if out == nil {
			out = []Update{}
		}
		okJSON(w, out)
	})
	f.handle("sendMessage", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.sentText = append(f.sentText, "")
		f.mu.Unlock()
		okJSON(w, map[string]any{"message_id": 1})
	})
	f.handle("answerCallbackQuery", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.answered = append(f.answered, "")
		f.mu.Unlock()
		okJSON(w, true)
	})
	f.handle("getMe", func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, map[string]any{"id": 1, "is_bot": true, "username": "raccoon_bot"})
	})

	c := newTestClient(t, f, func(cfg *Config) {
		cfg.AllowedUsers = allowedUsers
		cfg.PollTimeout = 50 * time.Millisecond
		cfg.RetryBackoff = 20 * time.Millisecond
	})
	h := &e2eRecorder{}
	return f, NewBot(c, h), h
}

// sentCount 返回机器人发出的消息条数。
func sentCount(f *fakeTelegram) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sentText)
}

// Test端到端_加命令创建任务并回话 完整链路：轮询拿到 /add → 鉴权 → 解析 →
// 调 Handler → 回一条消息。
func Test端到端_加命令创建任务并回话(t *testing.T) {
	f, bot, h := setupE2E(t, []int64{42}, []Update{
		e2eUpdate(100, 555, 42, "/add https://www.bilibili.com/video/BV1xx411c7mD"),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() { _ = bot.Run(ctx) }()

	waitFor(t, 2*time.Second, func() bool { return len(h.addURLs) > 0 })

	h.mu.Lock()
	adds := append([]string(nil), h.addURLs...)
	h.mu.Unlock()
	if len(adds) != 1 || !strings.Contains(adds[0], "BV1xx411c7mD") {
		t.Fatalf("Handler 应被调用 1 次并收到链接，实际 %v", adds)
	}
	if sentCount(f) == 0 {
		t.Fatal("应回一条消息")
	}
	t.Logf("已回话 %d 条", sentCount(f))
}

// Test端到端_裸链接直接入队 直接发链接（不带 /add）也应能下载——核心体验。
func Test端到端_裸链接直接入队(t *testing.T) {
	_, bot, h := setupE2E(t, []int64{42}, []Update{
		e2eUpdate(200, 555, 42, "帮我下这个 https://www.bilibili.com/video/BV1xx411c7mD 谢谢"),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() { _ = bot.Run(ctx) }()

	waitFor(t, 2*time.Second, func() bool { return len(h.addURLs) > 0 })
	h.mu.Lock()
	adds := append([]string(nil), h.addURLs...)
	h.mu.Unlock()
	if len(adds) != 1 || !strings.Contains(adds[0], "BV1xx411c7mD") {
		t.Fatalf("裸链接应被提取并提交，实际 %v", adds)
	}
}

// Test端到端_未授权用户被拒 未在白名单的用户不应能提交任务，但要有回执。
func Test端到端_未授权用户被拒(t *testing.T) {
	f, bot, h := setupE2E(t, []int64{42}, []Update{
		e2eUpdate(300, 555, 999, "/add https://www.bilibili.com/video/BV1xx411c7mD"),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() { _ = bot.Run(ctx) }()

	waitFor(t, 2*time.Second, func() bool { return sentCount(f) > 0 })
	h.mu.Lock()
	adds := len(h.addURLs)
	h.mu.Unlock()
	if adds != 0 {
		t.Fatalf("未授权用户的命令不应被提交，实际提交了 %d 次", adds)
	}
	t.Log("未授权用户被拒且有回执")
}

// Test端到端_状态命令带任务ID /status <id> 的参数应完整传给 Handler。
func Test端到端_状态命令带任务ID(t *testing.T) {
	_, bot, h := setupE2E(t, []int64{42}, []Update{
		e2eUpdate(500, 555, 42, "/status tsk_abc123"),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() { _ = bot.Run(ctx) }()

	waitFor(t, 2*time.Second, func() bool { return len(h.statuses) > 0 })
	h.mu.Lock()
	st := append([]string(nil), h.statuses...)
	h.mu.Unlock()
	if len(st) != 1 || st[0] != "tsk_abc123" {
		t.Fatalf("/status 的任务 ID 应原样传给 Handler，实际 %v", st)
	}
}

// Test端到端_取消命令带任务ID
func Test端到端_取消命令带任务ID(t *testing.T) {
	_, bot, h := setupE2E(t, []int64{42}, []Update{
		e2eUpdate(600, 555, 42, "/cancel tsk_xyz789"),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() { _ = bot.Run(ctx) }()

	waitFor(t, 2*time.Second, func() bool { return len(h.cancels) > 0 })
	h.mu.Lock()
	c := append([]string(nil), h.cancels...)
	h.mu.Unlock()
	if len(c) != 1 || c[0] != "tsk_xyz789" {
		t.Fatalf("/cancel 的任务 ID 应原样传给 Handler，实际 %v", c)
	}
}

// Test端到端_重复update只处理一次 Telegram 会重投同一条 update。
func Test端到端_重复update只处理一次(t *testing.T) {
	_, bot, h := setupE2E(t, []int64{42}, nil)
	msg := &Message{
		MessageID: 400,
		From:      &User{ID: 42, FirstName: "U"},
		Chat:      Chat{ID: 555, Type: "private"},
		Date:      1700000000,
		Text:      "/add https://www.bilibili.com/video/BV1xx411c7mD",
	}
	ctx := context.Background()
	if err := bot.HandleUpdate(ctx, Update{UpdateID: 400, Message: msg}); err != nil {
		t.Fatalf("首次处理应成功: %v", err)
	}
	if err := bot.HandleUpdate(ctx, Update{UpdateID: 400, Message: msg}); err == nil {
		t.Fatal("重复 update 应被去重拦下")
	}
	h.mu.Lock()
	adds := len(h.addURLs)
	h.mu.Unlock()
	if adds != 1 {
		t.Fatalf("重复 update 只应提交 1 次，实际 %d 次", adds)
	}
}

// Test端到端_内联按钮回调被应答 回调没有文本，走独立应答路径。
func Test端到端_内联按钮回调被应答(t *testing.T) {
	f, bot, h := setupE2E(t, []int64{42}, nil)
	ctx := context.Background()
	cq := &CallbackQuery{
		ID:   "cbq-1",
		Data: "/status tsk_from_button",
		From: &User{ID: 42, FirstName: "U"},
		Message: &Message{
			MessageID: 700,
			Chat:      Chat{ID: 555, Type: "private"},
		},
	}
	if err := bot.HandleUpdate(ctx, Update{UpdateID: 700, CallbackQuery: cq}); err != nil {
		t.Fatalf("回调处理应成功: %v", err)
	}
	f.mu.Lock()
	answered := len(f.answered)
	f.mu.Unlock()
	if answered != 1 {
		t.Fatalf("回调应被应答一次，实际 %d", answered)
	}
	h.mu.Lock()
	st := append([]string(nil), h.statuses...)
	h.mu.Unlock()
	if len(st) != 1 || st[0] != "tsk_from_button" {
		t.Fatalf("按钮携带的命令应被分发，实际 %v", st)
	}
}

// waitFor 轮询等待条件成立。
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("等待条件超时")
}
