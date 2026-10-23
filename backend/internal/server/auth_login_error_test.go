// auth_login_error_test.go：登录失败的错误语义（docs/04 §2.1）。
//
// 为什么测这个：2026-10-23 线上「密码明明是对的却一直报用户名或口令错误」，
// 真实原因是数据库连接坏了（WAL 句柄失效）。登录 handler 把所有
// GetUserByUsername 的错误都当成「查无此人」→ 401，把基础设施故障伪装成
// 用户输错密码，排查方向被彻底带偏。数据库故障必须如实报 500。
package server

import (
	"fmt"
	"net/http"
	"testing"
)

// Test登录_数据库故障返回500 关掉连接池制造真实查询失败，登录必须 500 而不是 401。
func Test登录_数据库故障返回500(t *testing.T) {
	srv, base, username, password := newSecEnv(t)
	// 模拟数据库不可用（连接池关闭后任何查询都会失败）。
	if err := srv.store.Close(); err != nil {
		t.Fatalf("关闭测试库失败：%v", err)
	}

	code, body, env := secDoBody(t, http.MethodPost, base+"/api/v1/auth/login",
		fmt.Sprintf(`{"username":%q,"password":%q}`, username, password), secOpts{})
	if code != http.StatusInternalServerError {
		t.Fatalf("数据库故障时登录应返回 500，实际 %d（body: %s）", code, body)
	}
	if env.Error.Code == "UNAUTHORIZED" {
		t.Fatal("数据库故障不应伪装成 401「用户名或口令错误」")
	}
}

// Test登录_用户不存在仍是401 查无此人必须保持统一 401 文案（不暴露账号是否存在）。
func Test登录_用户不存在仍是401(t *testing.T) {
	_, base, _, _ := newSecEnv(t)
	code, _, env := secDoBody(t, http.MethodPost, base+"/api/v1/auth/login",
		`{"username":"nobody","password":"whatever-123"}`, secOpts{})
	if code != http.StatusUnauthorized {
		t.Fatalf("不存在的用户应返回 401，实际 %d", code)
	}
	if env.Error.Code == "" {
		t.Fatal("401 应带错误信封")
	}
}
