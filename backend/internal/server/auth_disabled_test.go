package server

// auth_disabled_test.go：免登录模式（security.auth_disabled）端到端。
//
// 场景：homelab 内网可信环境不想登录——开启后所有 API 匿名放行（伪用户 local），
// 初始化向导也跳过（auth/status 恒报 setup_required=false）。关着的时候行为
// 必须与过去完全一致（默认关闭，401/428 照旧）。

import (
	"fmt"
	"net/http"
	"testing"
)

func newAuthDisabledEnv(t *testing.T) (baseURL string) {
	t.Helper()
	withFakeYTDLPPATH(t)
	srv, baseURL, _, _ := newSecEnv(t) // 已初始化的管理员存在，但测试从不用它登录
	_ = srv.cfg.Set("security.auth_disabled", true)
	return baseURL
}

func TestAuthDisabled_匿名全权放行(t *testing.T) {
	baseURL := newAuthDisabledEnv(t)

	// 无 cookie 无 token：读列表、写任务、读 me 全部放行。
	if code, _ := secDo(t, http.MethodGet, baseURL+"/api/v1/tasks", "", secOpts{}); code != http.StatusOK {
		t.Fatalf("免登录模式 GET /tasks 应 200，实际 %d", code)
	}
	body := fmt.Sprintf(`{"items":[{"url":"%s"}]}`, "https://www.bilibili.com/video/BV1xx411c7mD")
	if code, _ := secDo(t, http.MethodPost, baseURL+"/api/v1/tasks", body, secOpts{}); code != http.StatusCreated {
		t.Fatalf("免登录模式匿名提交任务应 201，实际 %d", code)
	}
	code, raw, _ := secDoBody(t, http.MethodGet, baseURL+"/api/v1/auth/me", "", secOpts{})
	if code != http.StatusOK {
		t.Fatalf("免登录模式 /auth/me 应 200，实际 %d（%s）", code, raw)
	}
	if !containsAll(raw, `"username":"local"`) {
		t.Fatalf("匿名身份应为 local 用户（%s）", raw)
	}
	// 初始化向导跳过：status 恒 false。
	code, raw, _ = secDoBody(t, http.MethodPost, baseURL+"/api/v1/auth/status", "", secOpts{})
	if code != http.StatusOK || !containsAll(raw, `"setup_required":false`) {
		t.Fatalf("免登录模式 status 应报 setup_required=false（%d %s）", code, raw)
	}
	// 写操作无 CSRF 头也放行（无会话通道，csrfForSession 天然不校验）。
	if code, _ := secDo(t, http.MethodPost, baseURL+"/api/v1/parse", `{"url":"https://www.bilibili.com/video/BV1xx411c7mD"}`, secOpts{}); code == http.StatusForbidden {
		t.Fatal("免登录模式写操作不应被 CSRF 拦截")
	}
}

func TestAuthDisabled_默认关闭时行为不变(t *testing.T) {
	_, baseURL, user, pass := newSecEnv(t)
	// 未带凭据：401（初始化已完成，不再是 428）。
	if code, _ := secDo(t, http.MethodGet, baseURL+"/api/v1/tasks", "", secOpts{}); code != http.StatusUnauthorized {
		t.Fatalf("默认关闭时匿名访问应 401，实际 %d", code)
	}
	// 登录后一切照旧。
	cookie, csrf := secLoginSession(t, baseURL, user, pass)
	if code, _ := secDo(t, http.MethodGet, baseURL+"/api/v1/tasks", "", secOpts{cookie: cookie, csrf: csrf}); code != http.StatusOK {
		t.Fatalf("正常会话应 200，实际 %d", code)
	}
}
