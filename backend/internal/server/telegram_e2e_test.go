package server

// telegram_e2e_test.go：Telegram 通道的服务端端到端验证。
//
// 这一层才是「机器人发消息 → 浣熊真的建了任务 → 任务完成 → 机器人回消息」
// 的完整闭环：协议层（internal/tgbot）已单独验证过。
//
// 覆盖：
//   - /add 真的在库里建了任务（不是只回了句「已入队」）；
//   - task→chat 映射记上了（否则完成时不知道回给谁）；
//   - 任务终态时确实向该 chat 发出通知；
//   - 裸链接与 /add 走同一条提交路径；
//   - 内网链接被 SSRF 校验拦下（不建任务）。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/huluohu/raccoon/internal/store"
	"github.com/huluohu/raccoon/internal/tgbot"
)

// tgMock 记录 Bot API 收到的消息。
type tgMock struct {
	mu   sync.Mutex
	sent []map[string]any
}

func (m *tgMock) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		method := ""
		if len(parts) == 2 {
			method = parts[1]
		}
		switch method {
		case "sendMessage":
			// 浣熊的客户端用 application/x-www-form-urlencoded 发消息
			// （chat_id 是字符串形态），按表单解析而不是 JSON。
			_ = r.ParseForm()
			body := map[string]any{
				"chat_id": r.PostForm.Get("chat_id"),
				"text":    r.PostForm.Get("text"),
			}
			m.mu.Lock()
			m.sent = append(m.sent, body)
			m.mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": 1}})
	})
}

func (m *tgMock) messages() []map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]map[string]any(nil), m.sent...)
}

// textsOf 取某会话收到的消息文本。
func (m *tgMock) textsOf(chatID int64) []string {
	var out []string
	for _, msg := range m.messages() {
		if v, ok := msg["chat_id"].(string); ok && v == strconv.FormatInt(chatID, 10) {
			if txt, ok := msg["text"].(string); ok {
				out = append(out, txt)
			}
		}
	}
	return out
}

// setupTGEnv 起一个带假 Telegram 的服务端环境。
func setupTGEnv(t *testing.T) (*Server, *tgMock) {
	t.Helper()
	srv, _, _, _ := newSecEnv(t)
	mock := &tgMock{}
	ts := httptest.NewServer(mock.handler())
	t.Cleanup(ts.Close)

	client, err := tgbot.New(tgbot.Config{
		Token:       "test-token",
		BaseURL:     ts.URL,
		PollTimeout: 50 * 1000 * 1000, // 50ms
	})
	if err != nil {
		t.Fatalf("构造 Telegram 客户端失败: %v", err)
	}
	srv.tgClient = client
	return srv, mock
}

// TestTelegram_加命令真的建了任务 /add 不只是回句话——库里必须多出一条任务。
func TestTelegram_加命令真的建了任务(t *testing.T) {
	srv, _ := setupTGEnv(t)
	h := &tgHandler{srv: srv}
	user := &tgbot.User{ID: 42, FirstName: "U"}

	before, err := srv.store.ListTasks(store.TaskFilter{Limit: 100})
	if err != nil {
		t.Fatalf("查询任务失败: %v", err)
	}
	text, err := h.OnAdd(context.Background(), 555, user, "https://www.bilibili.com/video/BV1xx411c7mD")
	if err != nil {
		t.Fatalf("/add 应成功: %v", err)
	}
	if !strings.Contains(text, "已入队") {
		t.Errorf("回执应说明已入队，实际 %q", text)
	}
	after, err := srv.store.ListTasks(store.TaskFilter{Limit: 100})
	if err != nil {
		t.Fatalf("查询任务失败: %v", err)
	}
	if len(after.Items) != len(before.Items)+1 {
		t.Fatalf("库里应多出 1 条任务，之前 %d 现在 %d", len(before.Items), len(after.Items))
	}
	// 新任务的来源应是 telegram（与 Web 同队列同状态机）。
	found := false
	for _, task := range after.Items {
		if task.Source == "telegram" {
			found = true
			if task.Platform == "" {
				t.Errorf("任务应带平台标识，实际 %q", task.Platform)
			}
		}
	}
	if !found {
		t.Errorf("应有一条 source=telegram 的任务，实际 %d 条", len(after.Items))
	}

	// task→chat 映射必须记上，否则任务完成时不知道回给谁。
	var newID string
	for _, task := range after.Items {
		if task.Source == "telegram" {
			newID = task.ID
		}
	}
	srv.tgMu.Lock()
	chat, ok := srv.tgChats[newID]
	srv.tgMu.Unlock()
	if !ok || chat != 555 {
		t.Fatalf("应记录 task→chat 映射（555），实际 %d ok=%v（task=%s）", chat, ok, newID)
	}
	t.Logf("任务已建: %s，映射 chat=%d", newID, chat)
}

// TestTelegram_任务终态回推通知 任务完成后应向该 chat 发出通知。
func TestTelegram_任务终态回推通知(t *testing.T) {
	srv, mock := setupTGEnv(t)
	h := &tgHandler{srv: srv}
	user := &tgbot.User{ID: 42, FirstName: "U"}

	if _, err := h.OnAdd(context.Background(), 555, user, "https://www.bilibili.com/video/BV1xx411c7mD"); err != nil {
		t.Fatalf("/add 应成功: %v", err)
	}
	// 找出刚建的任务
	tasks, err := srv.store.ListTasks(store.TaskFilter{Limit: 100})
	if err != nil {
		t.Fatalf("查询任务失败: %v", err)
	}
	var taskID string
	for _, task := range tasks.Items {
		if task.Source == "telegram" {
			taskID = task.ID
		}
	}
	if taskID == "" {
		t.Fatal("没找到 telegram 任务")
	}

	// 触发完成通知（服务端真实路径）。
	srv.notifyTelegram(taskID, "任务完成：测试视频", "", "success")

	texts := mock.textsOf(555)
	if len(texts) == 0 {
		t.Fatal("任务完成后应向该 chat 发出通知")
	}
	if !strings.Contains(texts[0], taskID) {
		t.Errorf("通知应带任务 ID，实际 %q", texts[0])
	}
	t.Logf("完成通知: %q", texts[0])

	// 一次性通知：映射用完即清，重复触发不应再发。
	srv.notifyTelegram(taskID, "任务完成：测试视频", "", "success")
	if len(mock.textsOf(555)) != len(texts) {
		t.Errorf("通知是一次性的，重复触发不应再发，实际 %d 条", len(mock.textsOf(555)))
	}
}

// TestTelegram_裸链接与命令同路径 裸链接消息也要建任务。
func TestTelegram_裸链接与命令同路径(t *testing.T) {
	srv, _ := setupTGEnv(t)
	h := &tgHandler{srv: srv}
	user := &tgbot.User{ID: 42, FirstName: "U"}

	text, err := h.OnAddBatch(context.Background(), 555, user, []string{
		"https://www.bilibili.com/video/BV1xx411c7mD",
		"https://www.bilibili.com/video/BV1yy411c7mD",
	})
	if err != nil {
		t.Fatalf("批量提交应成功: %v", err)
	}
	if !strings.Contains(text, "2 入队") {
		t.Errorf("回执应说明 2 条入队，实际 %q", text)
	}
	tasks, err := srv.store.ListTasks(store.TaskFilter{Limit: 100})
	if err != nil {
		t.Fatalf("查询任务失败: %v", err)
	}
	n := 0
	for _, task := range tasks.Items {
		if task.Source == "telegram" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("应建 2 条任务，实际 %d", n)
	}
}

// TestTelegram_内网链接被拦 不该为内网地址建任务。
func TestTelegram_内网链接被拦(t *testing.T) {
	srv, _ := setupTGEnv(t)
	h := &tgHandler{srv: srv}
	user := &tgbot.User{ID: 42, FirstName: "U"}

	before, _ := srv.store.ListTasks(store.TaskFilter{Limit: 100})
	if _, err := h.OnAdd(context.Background(), 555, user, "http://169.254.169.254/latest/meta-data"); err == nil {
		t.Fatal("内网地址应被 SSRF 校验拦下")
	}
	after, _ := srv.store.ListTasks(store.TaskFilter{Limit: 100})
	if len(after.Items) != len(before.Items) {
		t.Fatalf("不该为内网地址建任务（%d → %d）", len(before.Items), len(after.Items))
	}
}

// TestTelegram_重复链接不重复建 队列里已有相同链接时应回「已存在」而不是再建一条。
func TestTelegram_重复链接不重复建(t *testing.T) {
	srv, _ := setupTGEnv(t)
	h := &tgHandler{srv: srv}
	user := &tgbot.User{ID: 42, FirstName: "U"}
	url := "https://www.bilibili.com/video/BV1xx411c7mD"

	if _, err := h.OnAdd(context.Background(), 555, user, url); err != nil {
		t.Fatalf("首次 /add 应成功: %v", err)
	}
	text, err := h.OnAdd(context.Background(), 555, user, url)
	if err != nil {
		t.Fatalf("重复 /add 不应报错: %v", err)
	}
	if !strings.Contains(text, "已存在") {
		t.Errorf("重复提交应回「队列中已存在」，实际 %q", text)
	}
}
