// authz_test.go：API Token scope 与撤销用例集（docs/04 §5 / §1.1）。
//
// 为什么测这个：Bearer Token 是 CLI/快捷指令/第三方扩展的入口，
// 一旦 scope 校验被绕过或撤销不即时，一枚泄露的只读令牌就变成永久后门。
// 所有用例都起真实 HTTP 服务打真实请求，令牌一律走真实签发流程
// （setup/登录 → 带 Cookie+CSRF 调 /api/v1/tokens），不手搓假令牌。
//
// 与 csrf_test.go 共用 newSecEnv/secLoginSession/secCreateToken/secDo 辅助（同包）。
package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/huluohu/raccoon/internal/config"
	"github.com/huluohu/raccoon/internal/httpx"
	"github.com/huluohu/raccoon/internal/logx"
	"github.com/huluohu/raccoon/internal/store"
)

// newSecEnv 起一套最小可用 Server：真库（临时目录）+ 真路由 + 真中间件链，
// 并走真实 /auth/setup 流程建好管理员。返回 Server（便于直连 store 造边角料数据）、
// baseURL 与管理员账号口令。
func newSecEnv(t *testing.T) (srv *Server, baseURL, username, password string) {
	t.Helper()
	dataDir := t.TempDir()
	st, err := store.Open(filepath.Join(dataDir, "raccoon.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg := config.New()
	if err := cfg.Set("storage.data_dir", dataDir); err != nil {
		t.Fatalf("配置数据目录失败: %v", err)
	}
	s, err := New(cfg, st, logx.New(io.Discard, "test", logx.LevelError))
	if err != nil {
		t.Fatalf("构造 Server 失败: %v", err)
	}
	t.Cleanup(func() { s.ShutdownTasks(2 * time.Second) })
	ts := httptestServer(t, s)

	username, password = "sectest", "Passw0rd-123"
	code, _, _ := secDoBody(t, http.MethodPost, ts.URL+"/api/v1/auth/setup",
		fmt.Sprintf(`{"username":%q,"password":%q}`, username, password), secOpts{})
	if code != http.StatusCreated {
		t.Fatalf("setup 初始化管理员应返回 201，实际 %d", code)
	}
	return s, ts.URL, username, password
}

// httptestServer 包一层，方便统一管理 httptest 生命周期。
func httptestServer(t *testing.T, s *Server) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// secOpts 一次安全用例请求的可选凭证。
type secOpts struct {
	cookie *http.Cookie // 会话通道
	csrf   string       // 会话通道的 X-CSRF-Token
	token  string       // Bearer Token 通道
}

// errEnvelope 统一错误信封（docs/04 §1.2）。
type errEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// secDoRaw 发一次真实请求，返回原始响应（调用方负责 Close Body）。
func secDoRaw(t *testing.T, method, url, body string, o secOpts) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if o.cookie != nil {
		req.AddCookie(o.cookie)
	}
	if o.csrf != "" {
		req.Header.Set("X-CSRF-Token", o.csrf)
	}
	if o.token != "" {
		req.Header.Set("Authorization", "Bearer "+o.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	return resp
}

// secDo 发请求，只取状态码 + 错误信封。
func secDo(t *testing.T, method, url, body string, o secOpts) (int, errEnvelope) {
	t.Helper()
	code, _, env := secDoBody(t, method, url, body, o)
	return code, env
}

// secDoBody 发请求，取状态码 + 响应体 + 错误信封。
func secDoBody(t *testing.T, method, url, body string, o secOpts) (int, string, errEnvelope) {
	t.Helper()
	resp := secDoRaw(t, method, url, body, o)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var env errEnvelope
	_ = json.Unmarshal(raw, &env)
	return resp.StatusCode, string(raw), env
}

// secLoginSession 真实登录并取回会话 Cookie 与 CSRF token（读 Set-Cookie）。
func secLoginSession(t *testing.T, baseURL, username, password string) (*http.Cookie, string) {
	t.Helper()
	resp := secDoRaw(t, http.MethodPost, baseURL+"/api/v1/auth/login",
		fmt.Sprintf(`{"username":%q,"password":%q}`, username, password), secOpts{})
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("登录应返回 200，实际 %d（body: %s）", resp.StatusCode, raw)
	}
	var out struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("解析登录响应失败: %v", err)
	}
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("登录响应未种下会话 Cookie")
	}
	if out.CSRFToken == "" {
		t.Fatal("登录响应未下发 CSRF token")
	}
	return cookie, out.CSRFToken
}

// secCreateToken 经 HTTP 真实签发一枚令牌（会话通道 + CSRF），返回 (id, 明文)。
func secCreateToken(t *testing.T, baseURL string, cookie *http.Cookie, csrf, name string, scope int) (string, string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"name": name, "scope": scope})
	if err != nil {
		t.Fatal(err)
	}
	code, raw, _ := secDoBody(t, http.MethodPost, baseURL+"/api/v1/tokens", string(body),
		secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusCreated {
		t.Fatalf("签发令牌应返回 201，实际 %d（body: %s）", code, raw)
	}
	var out struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("解析签发响应失败: %v", err)
	}
	if out.Token == "" || out.Data.ID == "" {
		t.Fatalf("签发响应缺少 token/id: %s", raw)
	}
	return out.Data.ID, out.Token
}

// TestTokenScope_只读令牌写接口403 docs/04 §5：scope 位掩码（read=1/write=2/admin=4）。
// 只读令牌调写接口必须在认证阶段就被 403 拦掉——放进 handler 才拦等于半执行。
func TestTokenScope_只读令牌写接口403(t *testing.T) {
	_, baseURL, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, baseURL, username, password)
	_, readTok := secCreateToken(t, baseURL, cookie, csrf, "read-only", store.ScopeRead)

	// 对照：同一枚令牌读接口必须放行（证明不是把令牌一棍子打死）。
	if code, _ := secDo(t, http.MethodGet, baseURL+"/api/v1/tasks", "", secOpts{token: readTok}); code != http.StatusOK {
		t.Errorf("只读令牌访问 GET /api/v1/tasks 应 200，实际 %d", code)
	}
	// 写接口 403。
	code, env := secDo(t, http.MethodPost, baseURL+"/api/v1/tasks", `{"items":[]}`, secOpts{token: readTok})
	if code != http.StatusForbidden {
		t.Fatalf("只读令牌 POST /api/v1/tasks 应 403，实际 %d（%+v）", code, env)
	}
	if env.Error.Code != string(httpx.CodeForbidden) {
		t.Errorf("错误码应为 FORBIDDEN，实际 %q", env.Error.Code)
	}
}

// TestTokenScope_管理域接口需admin /tokens 属管理域（auth_middleware.go:102
// 注释：「管理域（配置/凭据/订阅/直播/令牌）需 admin」），读+写令牌
// （scope=3）访问按注释应 403——防止「能写任务就能管令牌」的隐式扩权。
//
// 实测发现（本用例锁死期望行为）：requiredScope 对 GET/HEAD 短路返回
// ScopeRead，管理域前缀检查只对写方法生效——read 令牌即可枚举全部
// 令牌/配置/凭据元数据。期望 403，实际 200（代码注释与实现不一致）。
func TestTokenScope_管理域接口需admin(t *testing.T) {
	_, baseURL, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, baseURL, username, password)
	_, rwTok := secCreateToken(t, baseURL, cookie, csrf, "read-write", store.ScopeRead|store.ScopeWrite)

	code, env := secDo(t, http.MethodGet, baseURL+"/api/v1/tokens", "", secOpts{token: rwTok})
	if code != http.StatusForbidden {
		t.Fatalf("读+写令牌访问 GET /api/v1/tokens 应 403，实际 %d（%+v）", code, env)
	}
	if env.Error.Code != string(httpx.CodeForbidden) {
		t.Errorf("错误码应为 FORBIDDEN，实际 %q", env.Error.Code)
	}
}

// TestToken_撤销后立即失效401 撤销必须即时：撤销前 200、撤销后 401，
// 且撤销走真实 HTTP 端点（会话通道）完成，不停在「只改库不验证」。
func TestToken_撤销后立即失效401(t *testing.T) {
	_, baseURL, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, baseURL, username, password)
	id, tok := secCreateToken(t, baseURL, cookie, csrf, "revocable", store.ScopeRead|store.ScopeWrite)

	if code, _ := secDo(t, http.MethodGet, baseURL+"/api/v1/tasks", "", secOpts{token: tok}); code != http.StatusOK {
		t.Fatal("撤销前令牌应可用（200）")
	}
	code, env := secDo(t, http.MethodDelete, baseURL+"/api/v1/tokens/"+id, "", secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusOK {
		t.Fatalf("撤销令牌应返回 200，实际 %d（%+v）", code, env)
	}
	code, env = secDo(t, http.MethodGet, baseURL+"/api/v1/tasks", "", secOpts{token: tok})
	if code != http.StatusUnauthorized {
		t.Fatalf("撤销后令牌应 401，实际 %d（%+v）", code, env)
	}
	if env.Error.Code != string(httpx.CodeUnauthorized) {
		t.Errorf("错误码应为 UNAUTHORIZED，实际 %q", env.Error.Code)
	}
}

// TestToken_无效与空令牌401 乱码/空令牌都必须 401。
func TestToken_无效与空令牌401(t *testing.T) {
	_, baseURL, _, _ := newSecEnv(t)
	if code, env := secDo(t, http.MethodGet, baseURL+"/api/v1/tasks", "", secOpts{token: "rc_not_a_real_token"}); code != http.StatusUnauthorized {
		t.Errorf("无效令牌应 401，实际 %d（%+v）", code, env)
	}
	// 空令牌：Bearer 头存在但值为空（authenticateToken 显式拒绝）。
	resp := secDoRaw(t, http.MethodGet, baseURL+"/api/v1/tasks", "", secOpts{})
	resp.Body.Close()
	req, _ := http.NewRequest(http.MethodGet, baseURL+"/api/v1/tasks", nil)
	req.Header.Set("Authorization", "Bearer ")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("空令牌应 401，实际 %d", resp2.StatusCode)
	}
}

// TestToken_过期令牌401 过期检查在 authenticateToken 内按 expires_at 判。
// HTTP 创建接口最小有效期是 1 小时（expires_in_hours>0），造不出「已过期」行，
// 因此这里经 store.CreateAPIToken 以真实哈希建一枚过去时间过期的令牌——
// 认证链路（哈希比对 → 撤销/过期检查 → scope）与在线签发完全一致，不是绕过校验的假令牌。
func TestToken_过期令牌401(t *testing.T) {
	srv, baseURL, username, _ := newSecEnv(t)
	u, err := srv.store.GetUserByUsername(username)
	if err != nil {
		t.Fatalf("取测试用户失败: %v", err)
	}
	raw := "rc_expired_probe_token"
	past := time.Now().Add(-time.Hour).UnixMilli()
	if _, err := srv.store.CreateAPIToken(u.ID, "expired", store.HashToken(raw), store.ScopeAll, &past); err != nil {
		t.Fatalf("预置过期令牌失败: %v", err)
	}
	code, env := secDo(t, http.MethodGet, baseURL+"/api/v1/tasks", "", secOpts{token: raw})
	if code != http.StatusUnauthorized {
		t.Fatalf("过期令牌应 401，实际 %d（%+v）", code, env)
	}
	if env.Error.Code != string(httpx.CodeUnauthorized) {
		t.Errorf("错误码应为 UNAUTHORIZED，实际 %q", env.Error.Code)
	}
}

// Test无凭据访问受保护接口401 无 Cookie 无 Token 时必须 401（用户已初始化，
// 不再是 428 SETUP_REQUIRED——GET/POST 两条路径都测，锁死状态机）。
func Test无凭据访问受保护接口401(t *testing.T) {
	_, baseURL, _, _ := newSecEnv(t)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		code, env := secDo(t, method, baseURL+"/api/v1/tasks", `{}`, secOpts{})
		if code != http.StatusUnauthorized {
			t.Errorf("%s /api/v1/tasks 无凭据应 401，实际 %d（%+v）", method, code, env)
		}
		if env.Error.Code != string(httpx.CodeUnauthorized) {
			t.Errorf("%s 错误码应为 UNAUTHORIZED，实际 %q", method, env.Error.Code)
		}
	}
}

// Test伪造会话Cookie401 Cookie 值不是合法会话 ID 时必须 401——
// 不得因「Cookie 存在」就放行（authenticate 先查内存会话表）。
func Test伪造会话Cookie401(t *testing.T) {
	_, baseURL, _, _ := newSecEnv(t)
	code, env := secDo(t, http.MethodGet, baseURL+"/api/v1/tasks", "", secOpts{
		cookie: &http.Cookie{Name: sessionCookieName, Value: "forged-session-id"},
	})
	if code != http.StatusUnauthorized {
		t.Fatalf("伪造会话 Cookie 应 401，实际 %d（%+v）", code, env)
	}
}
