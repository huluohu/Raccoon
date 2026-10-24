package server

// serverchan_test.go：Server酱（微信推送）通道行为测试。
//
// 通知出口的纪律比「发得出去」更重要：未配置不发、非关心事件不发、
// 发送异步不阻塞、Server酱回非 0 code 只记日志不炸任务流。

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/huluohu/raccoon/internal/config"
	"github.com/huluohu/raccoon/internal/logx"
	"github.com/huluohu/raccoon/internal/store"
	"io"
	"path/filepath"
	"strings"
)

// newNotifyEnv 最小 Server（真库 + 真配置），不动任务系统。
func newNotifyEnv(t *testing.T) (*Server, *config.Config) {
	t.Helper()
	dataDir := t.TempDir()
	st, err := store.Open(filepath.Join(dataDir, "raccoon.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg := config.New()
	_ = cfg.Set("storage.data_dir", dataDir)
	s, err := New(cfg, st, logx.New(io.Discard, "test", logx.LevelError))
	if err != nil {
		t.Fatalf("构造 Server 失败: %v", err)
	}
	t.Cleanup(func() { s.ShutdownTasks(2 * time.Second) })
	return s, cfg
}

func TestServerChan_SendsOnTaskDone(t *testing.T) {
	var hits int32
	var gotPath, gotForm string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		gotForm = string(raw)
		_, _ = w.Write([]byte(`{"code":0,"message":"ok"}`))
	}))
	t.Cleanup(ts.Close)
	old := serverchanBase
	serverchanBase = ts.URL
	t.Cleanup(func() { serverchanBase = old })

	s, cfg := newNotifyEnv(t)
	_ = cfg.Set("notify.serverchan_sendkey", "SCTtestkey")
	// notifications.task_id 有外键约束：先造一条真任务（真实链路里 taskID 必存在）。
	task := &store.Task{
		ID: store.NewTaskID(), Kind: "download", Source: "web", URL: "https://x/v1",
		DedupeKey:     store.DedupeKey("download", "web", "https://x/v1"),
		ConfigVersion: 1, MaxAttempts: 1, CreatedAt: 1, UpdatedAt: 1,
	}
	if err := s.store.CreateTask(task); err != nil {
		t.Fatalf("造任务失败: %v", err)
	}
	s.notify("task_done", "success", "下载完成：标题", "2 个文件", task.ID)

	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&hits) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if atomic.LoadInt32(&hits) == 0 {
		t.Fatal("task_done 应触发 Server酱推送")
	}
	if gotPath != "/SCTtestkey.send" {
		t.Fatalf("推送路径应携带 SendKey: %s", gotPath)
	}
	if gotForm == "" || !containsAll(gotForm, "title=", "desp=", task.ID) {
		t.Fatalf("推送表单缺字段: %s", gotForm)
	}
}

func TestServerChan_SilentWhenUnconfiguredOrIrrelevant(t *testing.T) {
	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	t.Cleanup(ts.Close)
	old := serverchanBase
	serverchanBase = ts.URL
	t.Cleanup(func() { serverchanBase = old })

	s, cfg := newNotifyEnv(t)
	// 未配置 SendKey：任何事件都不发。
	s.notify("task_done", "success", "t", "b", "tsk1")
	// 配置后：非关心事件（如 live_start）也不发。
	_ = cfg.Set("notify.serverchan_sendkey", "SCTk")
	s.notify("live_start", "info", "开播", "", "tsk2")
	time.Sleep(300 * time.Millisecond)
	if atomic.LoadInt32(&hits) != 0 {
		t.Fatalf("未配置/非关心事件不应推送，实际推送 %d 次", hits)
	}
}

func TestServerChan_ServerSideRejectOnlyLogs(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":40001,"message":"额度用尽"}`))
	}))
	t.Cleanup(ts.Close)
	old := serverchanBase
	serverchanBase = ts.URL
	t.Cleanup(func() { serverchanBase = old })

	s, cfg := newNotifyEnv(t)
	_ = cfg.Set("notify.serverchan_sendkey", "SCTk")
	// 被拒绝不该 panic/阻塞——调用方无需感知。
	s.notify("task_failed", "error", "任务失败：X", "msg", "tsk1")
	time.Sleep(300 * time.Millisecond)
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
