package server

// task_kind_test.go：单任务下载通道（kind）的选择与路由。
//
// 2026-10-30 加：此前通道选择**只能靠全局存储策略**（storage.strategy）——
// 想给单个任务走 115，就得把全局策略翻成 n115_first，于是所有任务都跟着变。
// 用户问「下载时如何直接选择用 115 网盘或 qBittorrent」，答案是「做不到」。
// 现在单任务可显式指定（API 的 kind 字段 / CLI --kind / MCP kind / 界面「通道」）。

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/huluohu/raccoon/internal/store"
	"github.com/huluohu/raccoon/internal/task"
)

// magnetURL 一个合法磁力链接（info_hash 40 位 hex）。
const magnetURL = "magnet:?xt=urn:btih:aabbccddeeff00112233445566778899aabbccdd&dn=raccoon-test"

// submitWithKind 提交一条指定通道的任务，返回建出的任务。
func submitWithKind(t *testing.T, srv *Server, baseURL, url, kind string, opts secOpts) *store.Task {
	t.Helper()
	body := fmt.Sprintf(`{"items":[{"url":%q,"kind":%q}]}`, url, kind)
	code, raw, _ := secDoBody(t, http.MethodPost, baseURL+"/api/v1/tasks", body, opts)
	if code != http.StatusCreated {
		t.Fatalf("提交应 201，实际 %d（%s）", code, raw)
	}
	return firstTask(t, srv)
}

// authedSecOpts 登录后拿到会话 Cookie 与 CSRF（写操作需要）。
func authedSecOpts(t *testing.T, baseURL, user, pass string) secOpts {
	t.Helper()
	cookie, csrf := secLoginSession(t, baseURL, user, pass)
	return secOpts{cookie: cookie, csrf: csrf}
}

// firstTask 取库里最新一条任务（直接查 store，不走 HTTP 列表接口）。
func firstTask(t *testing.T, srv *Server) *store.Task {
	t.Helper()
	res, err := srv.store.ListTasks(store.TaskFilter{Limit: 100})
	if err != nil {
		t.Fatalf("查询任务失败: %v", err)
	}
	if len(res.Items) == 0 {
		t.Fatal("库里没有任务")
	}
	it := res.Items[0]
	return &store.Task{ID: it.ID, Kind: it.Kind, Platform: it.Platform, Source: it.Source, URL: it.URL}
}

// Test单任务通道_磁力可显式指定qB 磁力链接显式指定 qb → 任务 kind=qb。
func Test单任务通道_磁力可显式指定qB(t *testing.T) {
	withFakeYTDLPPATH(t)
	srv, baseURL, user, pass := newSecEnv(t)
	_ = srv
	opts := authedSecOpts(t, baseURL, user, pass)
	tk := submitWithKind(t, srv, baseURL, magnetURL, "qb", opts)
	if tk.Kind != task.KindQB {
		t.Errorf("应建 qb 任务，实际 kind=%q", tk.Kind)
	}
	if tk.Platform != "qb" {
		t.Errorf("平台标识应为 qb，实际 %q", tk.Platform)
	}
}

// Test单任务通道_磁力可显式指定115 磁力链接显式指定 n115 → 任务 kind=n115。
func Test单任务通道_磁力可显式指定115(t *testing.T) {
	withFakeYTDLPPATH(t)
	srv, baseURL, user, pass := newSecEnv(t)
	_ = srv
	opts := authedSecOpts(t, baseURL, user, pass)
	tk := submitWithKind(t, srv, baseURL, magnetURL, "n115", opts)
	if tk.Kind != task.KindN115 {
		t.Errorf("应建 n115 任务，实际 kind=%q", tk.Kind)
	}
	if tk.Platform != "115" {
		t.Errorf("平台标识应为 115，实际 %q", tk.Platform)
	}
}

// Test单任务通道_留空按策略 自动模式下磁力走存储策略（默认 local → qB）。
func Test单任务通道_留空按策略(t *testing.T) {
	withFakeYTDLPPATH(t)
	srv, baseURL, user, pass := newSecEnv(t)
	_ = srv
	opts := authedSecOpts(t, baseURL, user, pass)
	tk := submitWithKind(t, srv, baseURL, magnetURL, "", opts)
	if tk.Kind != task.KindQB {
		t.Errorf("默认策略 local 下磁力应走 qB，实际 kind=%q", tk.Kind)
	}
}

// Test单任务通道_非法值被拒 通道值会决定哪个执行器接管，不能被静默当成本地下载。
func Test单任务通道_非法值被拒(t *testing.T) {
	withFakeYTDLPPATH(t)
	_, baseURL, user, pass := newSecEnv(t)
	opts := authedSecOpts(t, baseURL, user, pass)
	body := fmt.Sprintf(`{"items":[{"url":%q,"kind":"evil"}]}`, magnetURL)
	code, raw, _ := secDoBody(t, http.MethodPost, baseURL+"/api/v1/tasks", body, opts)
	if code != http.StatusBadRequest {
		t.Fatalf("非法通道应 400，实际 %d（%s）", code, raw)
	}
	if !containsAll(raw, "download") {
		t.Errorf("错误信息应说明合法取值，实际 %s", raw)
	}
}

// Test单任务通道_普通直链显式指定n115 非磁力链接也可以显式指定 115 离线
// （离线下载的目标 URL 与直链在形态上无法区分，必须由调用方明确表达意图）。
func Test单任务通道_普通直链显式指定n115(t *testing.T) {
	withFakeYTDLPPATH(t)
	srv, baseURL, user, pass := newSecEnv(t)
	_ = srv
	opts := authedSecOpts(t, baseURL, user, pass)
	tk := submitWithKind(t, srv, baseURL, "https://www.bilibili.com/video/BV1xx411c7mD", "n115", opts)
	if tk.Kind != task.KindN115 {
		t.Errorf("显式指定 n115 时应建 n115 任务，实际 kind=%q", tk.Kind)
	}
}

// Test磁力链接可提交 2026-10-30 修的关键回归：磁力链接此前在**所有**入口
// （Web 提交、批量、解析、Telegram、微信）都被协议校验挡掉（"仅支持
// http/https 链接"），于是「磁力 → qB/115」这条链路从用户侧永远走不到——
// qB 配好了也收不到任何种子。
func Test磁力链接可提交(t *testing.T) {
	withFakeYTDLPPATH(t)
	srv, baseURL, user, pass := newSecEnv(t)
	opts := authedSecOpts(t, baseURL, user, pass)

	tk := submitWithKind(t, srv, baseURL, magnetURL, "qb", opts)
	if tk.Kind != task.KindQB {
		t.Errorf("磁力 + qb 应建 qb 任务，实际 kind=%q", tk.Kind)
	}
	if tk.URL != magnetURL {
		t.Errorf("磁力链接应原样落库，实际 %q", tk.URL)
	}
}

// Test磁力链接非法info_hash被拒 磁力必须有合法 info_hash，否则 qB 收不了。
func Test磁力链接非法info_hash被拒(t *testing.T) {
	withFakeYTDLPPATH(t)
	_, baseURL, user, pass := newSecEnv(t)
	opts := authedSecOpts(t, baseURL, user, pass)
	body := `{"items":[{"url":"magnet:?xt=urn:btih:tooshort&dn=x"}]}`
	code, raw, _ := secDoBody(t, http.MethodPost, baseURL+"/api/v1/tasks", body, opts)
	if code != http.StatusBadRequest {
		t.Fatalf("非法 info_hash 应 400，实际 %d（%s）", code, raw)
	}
}

// Test普通链接仍受协议白名单约束 加了 magnet 之后不能把 file:// 之类也放进来。
func Test普通链接仍受协议白名单约束(t *testing.T) {
	withFakeYTDLPPATH(t)
	_, baseURL, user, pass := newSecEnv(t)
	opts := authedSecOpts(t, baseURL, user, pass)
	body := `{"items":[{"url":"file:///etc/passwd"}]}`
	code, _, _ := secDoBody(t, http.MethodPost, baseURL+"/api/v1/tasks", body, opts)
	if code != http.StatusBadRequest {
		t.Fatalf("file:// 应被拒，实际 %d", code)
	}
}
