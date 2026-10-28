package subscription

// service_test.go：订阅链路的代理接线。
//
// 关键不变式：订阅的「周期发现」必须走与其它链路同一份出口决策（全局 net.proxy）。
// 此前这一步恒直连 —— 只靠代理可达的站点，订阅根本发现不到内容，
// 也就永远不会建任务（而同一订阅在创建时的预览却是走代理的，前后口径不一致）。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huluohu/raccoon/internal/config"
	"github.com/huluohu/raccoon/internal/engine"
	"github.com/huluohu/raccoon/internal/logx"
	"github.com/huluohu/raccoon/internal/sse"
	"github.com/huluohu/raccoon/internal/store"
	"github.com/huluohu/raccoon/internal/task"
)

// writeArgRecorder 生成一个把 "$@" 落盘的假 yt-dlp（捕获真实命令行，不 mock 引擎内部）。
func writeArgRecorder(t *testing.T, dir, name string) string {
	t.Helper()
	bin := filepath.Join(dir, name)
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$FAKE_ARGS\"\n" +
		`echo '{"title":"假列表","uploader":"u","entries":[{"id":"e1","title":"t1","url":"https://example.com/1"}]}'` + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("写假引擎失败：%v", err)
	}
	return bin
}

func newTestService(t *testing.T) (*Service, *store.Store, *config.Config) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "sub.db"))
	if err != nil {
		t.Fatalf("打开测试库失败：%v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg := config.New()
	hub := sse.New(logx.New(nil, "test", logx.LevelError))
	mgr := task.NewManager(st, cfg, &engine.Set{}, hub, logx.New(nil, "test", logx.LevelError))
	return New(st, cfg, &engine.Set{}, mgr, logx.New(nil, "test", logx.LevelError)), st, cfg
}

// Test订阅发现_走全局代理 发现阶段必须带 --proxy（全局 net.proxy）。
func Test订阅发现_走全局代理(t *testing.T) {
	svc, st, cfg := newTestService(t)
	_ = cfg.Set("net.proxy", "http://127.0.0.1:7890")

	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	t.Setenv("FAKE_ARGS", argsFile)
	bin := writeArgRecorder(t, dir, "yt-dlp")
	svc.eng.YTDLP = engine.NewYTDLP(bin, "")

	sub := &store.Subscription{
		ID: "sub1", Platform: "youtube", SourceType: "favorite", Title: "测试订阅", Enabled: true,
		SourceURL: "https://example.com/fav", IntervalMin: 60,
	}
	if err := st.CreateSubscription(sub); err != nil {
		t.Fatalf("建订阅失败：%v", err)
	}
	if _, err := svc.CheckOne(context.Background(), sub); err != nil {
		t.Fatalf("订阅检查失败：%v", err)
	}

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("读假引擎参数失败：%v", err)
	}
	args := string(raw)
	if !strings.Contains(args, "--proxy") {
		t.Fatalf("订阅发现阶段应带 --proxy，实际参数：%q", args)
	}
	if !strings.Contains(args, "http://127.0.0.1:7890") {
		t.Fatalf("订阅发现应使用全局代理，实际参数：%q", args)
	}
}

// Test订阅发现_未配代理不加参数 没配代理就不该出现 --proxy（与引擎层口径一致）。
func Test订阅发现_未配代理不加参数(t *testing.T) {
	svc, st, _ := newTestService(t)

	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	t.Setenv("FAKE_ARGS", argsFile)
	bin := writeArgRecorder(t, dir, "yt-dlp")
	svc.eng.YTDLP = engine.NewYTDLP(bin, "")

	sub := &store.Subscription{
		ID: "sub2", Platform: "youtube", SourceType: "favorite", Title: "无代理订阅", Enabled: true,
		SourceURL: "https://example.com/fav2", IntervalMin: 60,
	}
	if err := st.CreateSubscription(sub); err != nil {
		t.Fatalf("建订阅失败：%v", err)
	}
	if _, err := svc.CheckOne(context.Background(), sub); err != nil {
		t.Fatalf("订阅检查失败：%v", err)
	}
	raw, _ := os.ReadFile(argsFile)
	if strings.Contains(string(raw), "--proxy") {
		t.Fatalf("未配代理时不应出现 --proxy，实际参数：%q", string(raw))
	}
}
