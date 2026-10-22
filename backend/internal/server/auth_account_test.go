// auth_account_test.go：账号信息修改用例集（PUT /auth/username + PUT /auth/password）。
//
// 为什么测这个：这是单管理员实例上唯一的「账户自救」入口。口令改错不给过、
// 弱口令不给过、用户名撞车要给 409，任何一条松了都会把用户锁在门外或改出脏数据。
// 改用户名要求当场验证当前口令——会话有效期 30 天，借用会话不应能静默改账户名。
package server

import (
	"fmt"
	"net/http"
	"testing"
)

// Test改用户名_成功并即时生效 改完用新用户名 + 旧口令能登录（说明库里的确改了）。
func Test改用户名_成功并即时生效(t *testing.T) {
	_, base, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, base, username, password)

	code, _, _ := secDoBody(t, http.MethodPut, base+"/api/v1/auth/username",
		fmt.Sprintf(`{"username":"renamed","password":%q}`, password), secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusOK {
		t.Fatalf("改用户名应返回 200，实际 %d", code)
	}

	// 新用户名 + 原口令登录应成功；旧用户名应失败（统一 401 文案）。
	if code, _, _ := secDoBody(t, http.MethodPost, base+"/api/v1/auth/login",
		fmt.Sprintf(`{"username":"renamed","password":%q}`, password), secOpts{}); code != http.StatusOK {
		t.Fatalf("新用户名登录应返回 200，实际 %d", code)
	}
	if code, _, _ := secDoBody(t, http.MethodPost, base+"/api/v1/auth/login",
		fmt.Sprintf(`{"username":%q,"password":%q}`, username, password), secOpts{}); code != http.StatusUnauthorized {
		t.Fatalf("旧用户名登录应返回 401，实际 %d", code)
	}
}

// Test改用户名_口令错误401 当前口令不对，一律 401（不泄露账户是否存在由登录处统一处理）。
func Test改用户名_口令错误401(t *testing.T) {
	_, base, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, base, username, password)

	code, _ := secDo(t, http.MethodPut, base+"/api/v1/auth/username",
		`{"username":"whatever","password":"wrong-password"}`, secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusUnauthorized {
		t.Fatalf("口令错误应返回 401，实际 %d", code)
	}
}

// Test改用户名_缺口令400 没带当前口令直接拒绝（Validation）。
func Test改用户名_缺口令400(t *testing.T) {
	_, base, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, base, username, password)

	code, _ := secDo(t, http.MethodPut, base+"/api/v1/auth/username",
		`{"username":"whatever"}`, secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusBadRequest {
		t.Fatalf("缺口令应返回 400，实际 %d", code)
	}
}

// Test改用户名_非法用户名400 长度/字符校验与 setup 同一套规则。
func Test改用户名_非法用户名400(t *testing.T) {
	_, base, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, base, username, password)

	for _, bad := range []string{`"a"`, `"名字 带空格"`, `"emoji😀"`} {
		code, _ := secDo(t, http.MethodPut, base+"/api/v1/auth/username",
			fmt.Sprintf(`{"username":%s,"password":%q}`, bad, password), secOpts{cookie: cookie, csrf: csrf})
		if code != http.StatusBadRequest {
			t.Fatalf("非法用户名 %s 应返回 400，实际 %d", bad, code)
		}
	}
}

// Test改用户名_撞名409 与现存用户名冲突返回 409，且原用户名不受影响。
func Test改用户名_撞名409(t *testing.T) {
	srv, base, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, base, username, password)

	// 直连 store 造第二个用户占用目标名（单管理员实例正常不会出现，但接口要能正确处理）。
	taken := "taken"
	if _, err := srv.store.CreateUser(taken, "x"); err != nil {
		t.Fatalf("造冲突用户失败: %v", err)
	}

	code, _ := secDo(t, http.MethodPut, base+"/api/v1/auth/username",
		fmt.Sprintf(`{"username":%q,"password":%q}`, taken, password), secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusConflict {
		t.Fatalf("用户名冲突应返回 409，实际 %d", code)
	}
	// 冲突后原用户名仍可登录（没有被半途改掉）。
	if code, _, _ := secDoBody(t, http.MethodPost, base+"/api/v1/auth/login",
		fmt.Sprintf(`{"username":%q,"password":%q}`, username, password), secOpts{}); code != http.StatusOK {
		t.Fatalf("冲突后原用户名登录应仍返回 200，实际 %d", code)
	}
}

// Test改用户名_未登录401 无会话直接 401。
func Test改用户名_未登录401(t *testing.T) {
	_, base, _, password := newSecEnv(t)
	code, _ := secDo(t, http.MethodPut, base+"/api/v1/auth/username",
		fmt.Sprintf(`{"username":"someone","password":%q}`, password), secOpts{})
	if code != http.StatusUnauthorized {
		t.Fatalf("未登录应返回 401，实际 %d", code)
	}
}

// Test改密码_成功且旧口令失效 改完新口令能登录、旧口令不能。
func Test改密码_成功且旧口令失效(t *testing.T) {
	_, base, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, base, username, password)

	newPassword := "Brand-New-Passw0rd"
	code, _, _ := secDoBody(t, http.MethodPut, base+"/api/v1/auth/password",
		fmt.Sprintf(`{"old_password":%q,"new_password":%q}`, password, newPassword), secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusOK {
		t.Fatalf("改密码应返回 200，实际 %d", code)
	}
	if code, _, _ := secDoBody(t, http.MethodPost, base+"/api/v1/auth/login",
		fmt.Sprintf(`{"username":%q,"password":%q}`, username, newPassword), secOpts{}); code != http.StatusOK {
		t.Fatalf("新口令登录应返回 200，实际 %d", code)
	}
	if code, _, _ := secDoBody(t, http.MethodPost, base+"/api/v1/auth/login",
		fmt.Sprintf(`{"username":%q,"password":%q}`, username, password), secOpts{}); code != http.StatusUnauthorized {
		t.Fatalf("旧口令登录应返回 401，实际 %d", code)
	}
}

// Test改密码_旧口令错误401与弱口令400 两个拒绝分支都要在。
func Test改密码_旧口令错误401与弱口令400(t *testing.T) {
	_, base, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, base, username, password)

	code, _ := secDo(t, http.MethodPut, base+"/api/v1/auth/password",
		`{"old_password":"not-the-password","new_password":"another-good-pass"}`, secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusUnauthorized {
		t.Fatalf("旧口令错误应返回 401，实际 %d", code)
	}

	code, _ = secDo(t, http.MethodPut, base+"/api/v1/auth/password",
		fmt.Sprintf(`{"old_password":%q,"new_password":"short"}`, password), secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusBadRequest {
		t.Fatalf("弱口令应返回 400，实际 %d", code)
	}
}
