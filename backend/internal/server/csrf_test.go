// csrf_test.go：CSRF 防护用例集（docs/04 §1.1：会话通道写操作需 X-CSRF-Token，
// Bearer Token 通道豁免）。
//
// 为什么测这个：会话 Cookie 是 SameSite=Lax——跨站 POST 不带 Cookie，
// 但同站子域/浏览器怪癖仍可能拼出「带着 Cookie 的伪造写请求」。
// X-CSRF-Token 双提交是第二道闸：攻击者能种 Cookie 也读不到 token
// （token 只在登录响应体里下发，且不与 Cookie 同源暴露）。
// 豁免逻辑同样要锁死：Token 通道（CLI/快捷指令）没有 Cookie 上下文，
// 若误加 CSRF 校验会打断全部 API 客户端。
package server

import (
	"net/http"
	"testing"

	"github.com/huluohu/raccoon/internal/store"
)

// TestCSRF_会话通道缺token403 Cookie 会话发 POST 但不带 X-CSRF-Token → 403。
func TestCSRF_会话通道缺token403(t *testing.T) {
	_, baseURL, username, password := newSecEnv(t)
	cookie, _ := secLoginSession(t, baseURL, username, password)

	code, env := secDo(t, http.MethodPost, baseURL+"/api/v1/tokens",
		`{"name":"no-csrf","scope":1}`, secOpts{cookie: cookie})
	if code != http.StatusForbidden {
		t.Fatalf("会话通道 POST 缺 CSRF token 应 403，实际 %d（%+v）", code, env)
	}
	if env.Error.Code != "FORBIDDEN" {
		t.Errorf("错误码应为 FORBIDDEN，实际 %q", env.Error.Code)
	}
}

// TestCSRF_会话通道错误token403 带错 token 与不带等价，都必须 403。
func TestCSRF_会话通道错误token403(t *testing.T) {
	_, baseURL, username, password := newSecEnv(t)
	cookie, _ := secLoginSession(t, baseURL, username, password)

	code, env := secDo(t, http.MethodPost, baseURL+"/api/v1/tokens",
		`{"name":"bad-csrf","scope":1}`, secOpts{cookie: cookie, csrf: "wrong-token-value"})
	if code != http.StatusForbidden {
		t.Fatalf("会话通道 POST 带错误 CSRF token 应 403，实际 %d（%+v）", code, env)
	}
}

// TestCSRF_会话通道正确token通过 登录响应下发的 CSRF token 必须放行（201）。
// 这是前两条用例的对照：证明 403 确实来自 CSRF 校验而非路由/认证问题。
func TestCSRF_会话通道正确token通过(t *testing.T) {
	_, baseURL, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, baseURL, username, password)

	code, env := secDo(t, http.MethodPost, baseURL+"/api/v1/tokens",
		`{"name":"with-csrf","scope":1}`, secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusCreated {
		t.Fatalf("会话通道 POST 带正确 CSRF token 应 201，实际 %d（%+v）", code, env)
	}
}

// TestCSRF_Token通道豁免 Bearer 通道 POST 不带 X-CSRF-Token、不带 Cookie → 放行。
// docs/04 §1.1 明确「Token 通道豁免 CSRF（无 Cookie 上下文）」；
// 若这里 403，全部 CLI/快捷指令客户端都会被打破。
func TestCSRF_Token通道豁免(t *testing.T) {
	_, baseURL, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, baseURL, username, password)
	_, adminTok := secCreateToken(t, baseURL, cookie, csrf, "admin-api", store.ScopeAll)

	code, env := secDo(t, http.MethodPost, baseURL+"/api/v1/tokens",
		`{"name":"bearer-created","scope":1}`, secOpts{token: adminTok})
	if code != http.StatusCreated {
		t.Fatalf("Bearer 通道 POST 应豁免 CSRF（201），实际 %d（%+v）", code, env)
	}
}

// TestCSRF_GET免检 CSRF 只拦状态变更方法；GET 带会话 Cookie 不带 token 必须放行。
func TestCSRF_GET免检(t *testing.T) {
	_, baseURL, username, password := newSecEnv(t)
	cookie, _ := secLoginSession(t, baseURL, username, password)

	code, env := secDo(t, http.MethodGet, baseURL+"/api/v1/tasks", "", secOpts{cookie: cookie})
	if code != http.StatusOK {
		t.Fatalf("GET 带会话不带 CSRF token 应 200，实际 %d（%+v）", code, env)
	}
}

// TestCSRF_会话Cookie优先于Bearer 同时带会话 Cookie 与 Bearer 的写请求：
// authenticate 先查 Cookie 通道 → csrfForSession 照常校验 → 403。
// 锁死「偷到 Bearer 也不能借会话 Cookie 绕过 CSRF」的通道优先级。
func TestCSRF_会话Cookie优先于Bearer(t *testing.T) {
	_, baseURL, username, password := newSecEnv(t)
	// 同一会话的 cookie + csrf 配对使用（换会话会导致 csrf 与 cookie 不匹配）。
	cookie, csrf := secLoginSession(t, baseURL, username, password)
	_, tok := secCreateToken(t, baseURL, cookie, csrf, "both-channels", store.ScopeAll)

	code, env := secDo(t, http.MethodPost, baseURL+"/api/v1/tokens",
		`{"name":"cookie-priority","scope":1}`, secOpts{cookie: cookie, token: tok})
	if code != http.StatusForbidden {
		t.Fatalf("带会话 Cookie 的写请求即使有 Bearer 也应过 CSRF（403），实际 %d（%+v）", code, env)
	}
}
