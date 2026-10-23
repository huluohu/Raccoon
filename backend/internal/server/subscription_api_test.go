// subscription_api_test.go：订阅暂停/删除的 HTTP 契约用例（docs/04 §2.6）。
//
// 为什么测这个：这两个按钮在列表页最常用，用户报「点了完全没反应」。
// 根因是 PUT 的 UPDATE 语句把空串写进 credential_id 外键列 → 500，
// 前端只能弹「操作失败」，状态一动不动。store 层已锁死该 SQL，
// 这里再锁一层 HTTP：真路由 + 真中间件 + 真库，断言 200 与落库结果。
package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/huluohu/raccoon/internal/store"
)

// newTestSubscription 直连 store 造一条订阅（跳过 yt-dlp 解析，测试不需要网络）。
func newTestSubscription(t *testing.T, srv *Server, id string) *store.Subscription {
	t.Helper()
	sub := &store.Subscription{
		ID:          id,
		Platform:    "bilibili",
		SourceType:  "favorite",
		SourceID:    "mid-1",
		SourceURL:   "https://space.bilibili.com/1/favlist?fid=2",
		Title:       "测试收藏夹",
		Mode:        "new_only",
		IntervalMin: 30,
		Enabled:     true,
	}
	if err := srv.store.CreateSubscription(sub); err != nil {
		t.Fatalf("造订阅失败：%v", err)
	}
	return sub
}

// Test暂停订阅_HTTP200且状态落库 未绑凭据的订阅（绝大多数）必须能改。
func Test暂停订阅_HTTP200且状态落库(t *testing.T) {
	srv, base, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, base, username, password)
	sub := newTestSubscription(t, srv, "sub_pause_1")

	code, body, env := secDoBody(t, http.MethodPut, base+"/api/v1/subscriptions/"+sub.ID,
		`{"enabled":false}`, secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusOK {
		t.Fatalf("暂停应返回 200，实际 %d（%s / %s）", code, env.Error.Code, body)
	}
	got, err := srv.store.GetSubscription(sub.ID)
	if err != nil {
		t.Fatalf("读回订阅失败：%v", err)
	}
	if got.Enabled {
		t.Fatal("暂停后 enabled 应为 false（状态没落库）")
	}

	// 再启用，确认双向都能改。
	code, _, env = secDoBody(t, http.MethodPut, base+"/api/v1/subscriptions/"+sub.ID,
		`{"enabled":true}`, secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusOK {
		t.Fatalf("启用应返回 200，实际 %d（%s）", code, env.Error.Code)
	}
	if got2, _ := srv.store.GetSubscription(sub.ID); !got2.Enabled {
		t.Fatal("启用后 enabled 应为 true")
	}
}

// Test删除订阅_HTTP200且从列表消失 删除是真删（不软删），列表随即为空。
func Test删除订阅_HTTP200且从列表消失(t *testing.T) {
	srv, base, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, base, username, password)
	sub := newTestSubscription(t, srv, "sub_del_1")

	code, body, env := secDoBody(t, http.MethodDelete, base+"/api/v1/subscriptions/"+sub.ID,
		"", secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusOK {
		t.Fatalf("删除应返回 200，实际 %d（%s / %s）", code, env.Error.Code, body)
	}
	if _, err := srv.store.GetSubscription(sub.ID); err == nil {
		t.Fatal("删除后订阅应查不到")
	}

	code, body, _ = secDoBody(t, http.MethodGet, base+"/api/v1/subscriptions", "",
		secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusOK {
		t.Fatalf("列表应返回 200，实际 %d", code)
	}
	if body == "" || strings.Contains(body, sub.ID) {
		t.Fatalf("列表不应再包含已删除订阅：%s", body)
	}
}

// Test订阅写操作缺CSRF_403 写接口必须校验 CSRF（会话 Cookie 是 HttpOnly，
// 但跨站表单仍能带 Cookie 发起请求）。
func Test订阅写操作缺CSRF_403(t *testing.T) {
	srv, base, username, password := newSecEnv(t)
	cookie, _ := secLoginSession(t, base, username, password)
	sub := newTestSubscription(t, srv, "sub_csrf_1")

	if code, _ := secDo(t, http.MethodPut, base+"/api/v1/subscriptions/"+sub.ID,
		`{"enabled":false}`, secOpts{cookie: cookie}); code != http.StatusForbidden {
		t.Fatalf("缺 CSRF 的 PUT 应返回 403，实际 %d", code)
	}
	if code, _ := secDo(t, http.MethodDelete, base+"/api/v1/subscriptions/"+sub.ID,
		"", secOpts{cookie: cookie}); code != http.StatusForbidden {
		t.Fatalf("缺 CSRF 的 DELETE 应返回 403，实际 %d", code)
	}
	// 两次都该被拒：订阅仍在且仍是启用态。
	got, err := srv.store.GetSubscription(sub.ID)
	if err != nil {
		t.Fatalf("读回订阅失败：%v", err)
	}
	if !got.Enabled {
		t.Fatal("被 CSRF 拒绝的请求不应改动数据")
	}
}
