package server

// log_order_test.go：运行日志的时间倒序契约（docs/04 §2.10-e）。
//
// 为什么单独测：日志页是「看刚发生的事」的界面，正序等于逼用户翻到底部。
// 且文件行序 ≠ 时间序——容器重建时新旧实例可能同时写同一个文件，
// 交错的行时间戳会乱（实测见过 23:46 夹在 23:51 之间）。
// 所以倒序必须由服务端按 ts 排序保证，不能依赖文件顺序。

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/huluohu/raccoon/internal/logx"
)

// writeLogLines 往日志文件写若干行（ts 由传入的偏移决定）。
func writeLogLines(t *testing.T, dataDir string, lines []logItem) {
	t.Helper()
	p := logx.FilePath(dataDir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf []byte
	for _, l := range lines {
		rec := map[string]any{
			"ts":     time.Unix(l.Ts, 0).UTC().Format(time.RFC3339Nano),
			"level":  l.Level,
			"module": l.Module,
			"msg":    l.Msg,
		}
		b, err := json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		buf = append(buf, b...)
		buf = append(buf, '\n')
	}
	if err := os.WriteFile(p, buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestLogs_时间倒序 首屏返回的最新一条必须是最新的，且整体按 ts 降序。
func TestLogs_时间倒序(t *testing.T) {
	srv, base, user, pass := newSecEnv(t)
	cookie, _ := secLoginSession(t, base, user, pass)
	dataDir := srv.cfg.GetString("storage.data_dir")
	base2 := time.Now().Unix()
	writeLogLines(t, dataDir, []logItem{
		{Ts: base2 - 300, Level: "info", Module: "main", Msg: "最旧的一条"},
		{Ts: base2 - 200, Level: "info", Module: "main", Msg: "中间的一条"},
		{Ts: base2 - 100, Level: "info", Module: "main", Msg: "最新的一条"},
	})

	code, body, _ := secDoBody(t, http.MethodGet, base+"/api/v1/logs", "", secOpts{cookie: cookie})
	if code != 200 {
		t.Fatalf("应返回 200，实际 %d", code)
	}
	var res struct {
		Items []logItem `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if len(res.Items) != 3 {
		t.Fatalf("应返回 3 条，实际 %d", len(res.Items))
	}
	if res.Items[0].Msg != "最新的一条" {
		t.Fatalf("首条应是最新的，实际 %q", res.Items[0].Msg)
	}
	// 整体必须严格降序
	for i := 1; i < len(res.Items); i++ {
		if res.Items[i-1].Ts < res.Items[i].Ts {
			t.Fatalf("第 %d 与 %d 条时间逆序: %d < %d", i-1, i, res.Items[i-1].Ts, res.Items[i].Ts)
		}
	}
}

// TestLogs_乱序文件按时间排序 文件行序乱时（多实例交错写）也要按 ts 排。
func TestLogs_乱序文件按时间排序(t *testing.T) {
	srv, base, user, pass := newSecEnv(t)
	cookie, _ := secLoginSession(t, base, user, pass)
	dataDir := srv.cfg.GetString("storage.data_dir")
	base2 := time.Now().Unix()
	writeLogLines(t, dataDir, []logItem{
		{Ts: base2 - 100, Level: "info", Module: "main", Msg: "较新的一条"},
		{Ts: base2 - 300, Level: "info", Module: "main", Msg: "最旧的一条"},
		{Ts: base2 - 200, Level: "info", Module: "main", Msg: "中间的一条"},
	})

	code, body, _ := secDoBody(t, http.MethodGet, base+"/api/v1/logs", "", secOpts{cookie: cookie})
	if code != 200 {
		t.Fatalf("应返回 200，实际 %d", code)
	}
	var res struct {
		Items []logItem `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 3 {
		t.Fatalf("应返回 3 条，实际 %d", len(res.Items))
	}
	if res.Items[0].Msg != "较新的一条" || res.Items[2].Msg != "最旧的一条" {
		t.Fatalf("未按时间排序，实际顺序: %q %q %q",
			res.Items[0].Msg, res.Items[1].Msg, res.Items[2].Msg)
	}
}
