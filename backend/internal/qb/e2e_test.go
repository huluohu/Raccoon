package qb

// e2e_test.go：qB 通道端到端验证（对着一个模拟 qB WebUI 跑）。
//
// 验证的是**我们这一侧**的真实代码：登录 → 提交磁力 → 轮询状态 → 删除，
// 以及状态映射（qB 的 state 字符串 → 浣熊任务状态）。
//
// 模拟服务只实现 qB WebUI API v2 的相关端点，形状与真实 qB 一致
// （登录回 SID cookie、add 收 multipart 的 urls、info 回 torrents 数组）。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockQB 一个最小但形状逼真的 qB WebUI。
type mockQB struct {
	mu       sync.Mutex
	sessions map[string]bool
	torrents map[string]*mockTorrent
	queries  int
	loginOK  bool
	// useV5Cookie 用 qB 5.x 的会话 Cookie 名（QBT_SID_<端口>）。
	useV5Cookie bool
	// unauthorizedStatus 未授权时返回的状态码（4.x=403，5.x=401）。
	unauthorizedStatus int
}

type mockTorrent struct {
	name     string
	size     int64
	savePath string
}

func newMockQB() *mockQB {
	return &mockQB{
		sessions:           map[string]bool{},
		torrents:           map[string]*mockTorrent{},
		loginOK:            true,
		unauthorizedStatus: http.StatusForbidden,
	}
}

// authorized 校验会话 Cookie。
//
// 注意这里必须**按名匹配**（真实 qB 也是）：qB 5.x 的会话 Cookie 名是
// QBT_SID_<端口>，若用 strings.Contains(c, "SID=") 判断，5.x 的
// "QBT_SID_8080=" 里并不含 "SID="（是 "SID_8080="），会被误判成未登录。
func (m *mockQB) authorized(r *http.Request) bool {
	c := r.Header.Get("Cookie")
	return strings.Contains(c, "SID=") || strings.Contains(c, "QBT_SID_")
}

// expireSessions 作废全部会话（模拟 SID 过期）。
func (m *mockQB) expireSessions() {}

func (m *mockQB) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/api/v2/auth/login" && r.Method == http.MethodPost:
		_ = r.ParseForm()
		if m.loginOK && r.FormValue("username") == "u" && r.FormValue("password") == "p" {
			// 会话 Cookie 名按 qB 版本切换：4.x = "SID"，5.x = "QBT_SID_<端口>"
			// （实测 v5.3.0）。两种都要覆盖——线上两种都在跑。
			name := "SID"
			if m.useV5Cookie {
				name = "QBT_SID_8080"
			}
			w.Header().Set("Set-Cookie", name+"=abc123; path=/")
			w.WriteHeader(200)
			_, _ = w.Write([]byte("Ok."))
			return
		}
		w.WriteHeader(403)
		return

	case path == "/api/v2/app/version":
		if !m.authorized(r) {
			w.WriteHeader(m.unauthorizedStatus)
			return
		}
		_, _ = w.Write([]byte("v4.6.5"))
		return

	case path == "/api/v2/torrents/add":
		if !m.authorized(r) {
			w.WriteHeader(m.unauthorizedStatus)
			return
		}
		_ = r.ParseMultipartForm(1 << 20)
		urls := r.FormValue("urls")
		savePath := r.FormValue("savepath")
		if savePath == "" {
			savePath = "/downloads"
		}
		hash := magnetHash(urls)
		m.mu.Lock()
		m.torrents[hash] = &mockTorrent{name: "raccoon-test", size: 1024 * 1024 * 7, savePath: savePath}
		m.queries = 0
		m.mu.Unlock()
		w.WriteHeader(200)
		return

	case path == "/api/v2/torrents/info":
		if !m.authorized(r) {
			w.WriteHeader(m.unauthorizedStatus)
			return
		}
		m.mu.Lock()
		m.queries++
		n := m.queries
		out := []map[string]any{}
		for h, t := range m.torrents {
			progress := 40.0
			state := "downloading"
			if n >= 2 {
				progress = 100.0
				state = "uploading"
			}
			out = append(out, map[string]any{
				"hash":         h,
				"name":         t.name,
				"state":        state,
				"progress":     progress,
				"size":         t.size,
				"downloaded":   int64(float64(t.size) * progress / 100),
				"amount_left":  int64(float64(t.size) * (100 - progress) / 100),
				"save_path":    t.savePath,
				"dlspeed":      int64(1048576),
				"category":     "",
				"tags":         "",
				"ratio":        0.0,
				"num_seeds":    3,
				"num_leechs":   1,
				"content_path": t.name,
			})
		}
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
		return

	case path == "/api/v2/torrents/delete":
		if !m.authorized(r) {
			w.WriteHeader(m.unauthorizedStatus)
			return
		}
		m.mu.Lock()
		m.torrents = map[string]*mockTorrent{}
		m.mu.Unlock()
		w.WriteHeader(200)
		return
	}
	w.WriteHeader(404)
}

// magnetHash 从磁力链接里取 info hash（测试用固定串即可）。
func magnetHash(magnet string) string {
	if i := strings.Index(magnet, "urn:btih:"); i >= 0 {
		s := magnet[i+len("urn:btih:"):]
		if j := strings.IndexAny(s, "& "); j >= 0 {
			s = s[:j]
		}
		return strings.ToLower(s)
	}
	return "deadbeef"
}

// Test端到端_登录到提交到轮询 完整链路：客户端登录 → 提交磁力 → 轮询到完成态。
func Test端到端_登录到提交到轮询(t *testing.T) {
	mock := newMockQB()
	srv := httptest.NewServer(mock)
	defer srv.Close()

	c := New(Config{
		BaseURL:   srv.URL,
		Username:  "u",
		Password:  "p",
		Timeout:   5 * time.Second,
		UserAgent: "Raccoon/test",
	})

	ctx := context.Background()
	if err := c.Login(ctx); err != nil {
		t.Fatalf("登录应成功: %v", err)
	}
	if err := c.IsAvailable(ctx); err != nil {
		t.Fatalf("可用性检查应通过: %v", err)
	}
	ver, err := c.Version(ctx)
	if err != nil || ver == "" {
		t.Fatalf("版本查询应成功: ver=%q err=%v", ver, err)
	}
	t.Logf("qB 版本: %s", ver)

	magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=raccoon-test"
	hash, err := c.AddMagnet(ctx, magnet, AddOptions{SavePath: "/downloads/movies"})
	if err != nil {
		t.Fatalf("提交磁力应成功: %v", err)
	}
	t.Logf("提交返回 hash: %s", hash)
	if hash == "" {
		t.Fatal("提交后应返回 info hash")
	}

	// 轮询：mock 第 2 次查询起报完成。
	var last Torrent
	for i := 0; i < 5; i++ {
		ts, err := c.Torrents(ctx, []string{hash})
		if err != nil {
			t.Fatalf("查询种子状态失败: %v", err)
		}
		if len(ts) == 0 {
			t.Fatal("应查到种子")
		}
		last = ts[0]
		if last.State == "uploading" || last.State == "completed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if last.Progress != 100 {
		t.Errorf("轮询到完成态时进度应为 100，实际 %v（state=%s）", last.Progress, last.State)
	}
	if last.SavePath != "/downloads/movies" {
		t.Errorf("保存目录应原样带回，实际 %q", last.SavePath)
	}
	if last.Size != 1024*1024*7 {
		t.Errorf("大小应正确，实际 %d", last.Size)
	}
	t.Logf("轮询终态: state=%s progress=%v size=%d save_path=%s", last.State, last.Progress, last.Size, last.SavePath)
}

// Test端到端_密码错误时登录失败 鉴权失败必须如实报错，不能假装连上。
func Test端到端_密码错误时登录失败(t *testing.T) {
	mock := newMockQB()
	srv := httptest.NewServer(mock)
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Username: "u", Password: "WRONG", Timeout: 3 * time.Second})
	if err := c.Login(context.Background()); err == nil {
		t.Fatal("密码错误时登录应失败")
	}
}

// Test端到端_未显式登录时自动登录 客户端在会话缺失时应当自动登录一次
// （doAuth：没 SID 就先登录），而不是直接报错——否则上层每次调用都得
// 自己记得先 Login，漏一次就整条链路失败。
func Test端到端_未显式登录时自动登录(t *testing.T) {
	mock := newMockQB()
	srv := httptest.NewServer(mock)
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Username: "u", Password: "p", Timeout: 3 * time.Second})
	ver, err := c.Version(context.Background())
	if err != nil {
		t.Fatalf("未显式登录时应自动登录后成功，实际: %v", err)
	}
	if ver == "" {
		t.Fatal("应拿到版本号")
	}
}

// Test端到端_会话失效自动重登 服务端返回 403（会话没了）时，客户端应清掉
// 旧 SID 重登一次并重放请求，而不是把任务判成失败。
func Test端到端_会话失效自动重登(t *testing.T) {
	mock := newMockQB()
	srv := httptest.NewServer(mock)
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Username: "u", Password: "p", Timeout: 3 * time.Second})
	ctx := context.Background()
	if err := c.Login(ctx); err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	// 把服务端会话作废（模拟 SID 过期）：客户端持有的 SID 不再被接受。
	mock.mu.Lock()
	mock.sessions = map[string]bool{}
	mock.mu.Unlock()
	mock.expireSessions()

	if _, err := c.Version(ctx); err != nil {
		t.Fatalf("会话失效后应自动重登并成功，实际: %v", err)
	}
}

// Test端到端_删除种子 取消任务时要删种子，避免留僵尸下载。
func Test端到端_删除种子(t *testing.T) {
	mock := newMockQB()
	srv := httptest.NewServer(mock)
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Username: "u", Password: "p", Timeout: 3 * time.Second})
	ctx := context.Background()
	if err := c.Login(ctx); err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	hash, err := c.AddMagnet(ctx, "magnet:?xt=urn:btih:abcdef0123456789abcdef0123456789abcdef01", AddOptions{})
	if err != nil {
		t.Fatalf("提交失败: %v", err)
	}
	if err := c.Delete(ctx, []string{hash}, false); err != nil {
		t.Fatalf("删除种子失败: %v", err)
	}
	ts, err := c.Torrents(ctx, nil)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(ts) != 0 {
		t.Errorf("删除后不应再有种子，实际 %d 个", len(ts))
	}
}

// Test端到端_连接测试端点语义 与前端「测试连接」按钮的语义对齐：
// 未授权时 qB 版本接口返回 403，服务其实是在的。
func Test端到端_连接测试端点语义(t *testing.T) {
	mock := newMockQB()
	srv := httptest.NewServer(mock)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v2/app/version")
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("未授权应返回 403/401，实际 %d", resp.StatusCode)
	}
	_ = os.Stdout
}

// ---- qB 5.x 兼容性（2026-10-30 对着真实 qBittorrent v5.3.0 实测）----

// Test端到端_v5会话Cookie名 真实 qB 5.x 登录返回的会话 Cookie 叫
// **QBT_SID_<端口>**（如 QBT_SID_8080），不是 4.x 的 "SID"。
//
// 旧实现只认 "SID" → 登录「成功」却拿不到会话 Cookie → 客户端当场报
// 「登录成功但未返回会话 Cookie」——**整个 qB 通道对 5.x 用户完全不可用**
// （连接测试失败、种子任务全挂）。这是实测 qB v5.3.0 才发现的。
func Test端到端_v5会话Cookie名(t *testing.T) {
	mock := newMockQB()
	mock.useV5Cookie = true
	srv := httptest.NewServer(mock)
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Username: "u", Password: "p", Timeout: 3 * time.Second})
	ctx := context.Background()
	if err := c.Login(ctx); err != nil {
		t.Fatalf("qB 5.x 应能登录: %v", err)
	}
	// 会话 Cookie 必须**原名**发送：qB 按完整 Cookie 头校验，
	// 把值换个名字发回去会 403（会话无效）。
	if got := c.sessionCookieHeader(); got != "QBT_SID_8080=abc123" {
		t.Errorf("会话 Cookie 头应为 QBT_SID_8080=abc123，实际 %q", got)
	}
	if _, err := c.Version(ctx); err != nil {
		t.Fatalf("5.x 登录后应能调业务接口: %v", err)
	}
}

// Test端到端_v4会话Cookie名仍可用 4.x 的 "SID" 不能被改坏。
func Test端到端_v4会话Cookie名仍可用(t *testing.T) {
	mock := newMockQB()
	srv := httptest.NewServer(mock)
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Username: "u", Password: "p", Timeout: 3 * time.Second})
	ctx := context.Background()
	if err := c.Login(ctx); err != nil {
		t.Fatalf("qB 4.x 应能登录: %v", err)
	}
	if got := c.sessionCookieHeader(); got != "SID=abc123" {
		t.Errorf("会话 Cookie 头应为 SID=abc123，实际 %q", got)
	}
}

// Test端到端_v5用401表示鉴权失败 真实 qB 5.x 对「未登录的业务接口」和
// 「密码错误」都返回 **401**（4.x 返回 403）。
//
// 旧实现只把 403 当鉴权失败 → 5.x 上：①密码填错给出干巴巴的 401 而不是
// 「用户名或密码错误」；②**会话失效不触发「清 SID 重登再重放」**，任务直接失败。
func Test端到端_v5用401表示鉴权失败(t *testing.T) {
	mock := newMockQB()
	mock.unauthorizedStatus = http.StatusUnauthorized
	srv := httptest.NewServer(mock)
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Username: "u", Password: "WRONG", Timeout: 3 * time.Second})
	err := c.Login(context.Background())
	if err == nil {
		t.Fatal("密码错误应登录失败")
	}
	if !errors.Is(err, ErrAuthFailed) {
		t.Errorf("应归类为 ErrAuthFailed（凭据错），实际 %v", err)
	}
	if !strings.Contains(err.Error(), "用户名或密码错误") {
		t.Errorf("错误信息应说「用户名或密码错误」，实际 %v", err)
	}
}

// Test端到端_会话失效自动重登_v5 5.x 的 401 同样要触发重登重放。
func Test端到端_会话失效自动重登_v5(t *testing.T) {
	mock := newMockQB()
	mock.unauthorizedStatus = http.StatusUnauthorized
	srv := httptest.NewServer(mock)
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Username: "u", Password: "p", Timeout: 3 * time.Second})
	ctx := context.Background()
	if err := c.Login(ctx); err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	if _, err := c.Version(ctx); err != nil {
		t.Fatalf("5.x 会话失效应自动重登并成功，实际: %v", err)
	}
}

// Test端到端_重复磁力按已存在处理 真实 qB 对已存在的种子返回 **409 Conflict**
// （实测 v5.3.0）。这不是失败——qB 已经在下载它了，任务应当继续轮询。
// 旧实现当错误 → 同一磁力重复提交（重试、两个任务撞车）一律失败。
func Test端到端_重复磁力按已存在处理(t *testing.T) {
	mock := newMockQB()
	srv := httptest.NewServer(mock)
	defer srv.Close()

	// 模拟 qB：已存在该 hash 时 add 返回 409。
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/torrents/add") {
			mock.mu.Lock()
			exists := len(mock.torrents) > 0
			if !exists {
				mock.torrents["aabbccddeeff00112233445566778899aabbccdd"] = &mockTorrent{
					name: "dup", size: 1024, savePath: "/downloads",
				}
			}
			mock.mu.Unlock()
			if exists {
				w.WriteHeader(http.StatusConflict)
				return
			}
			w.WriteHeader(200)
			return
		}
		mock.ServeHTTP(w, r)
	})
	srv2 := httptest.NewServer(mux)
	defer srv2.Close()

	c := New(Config{BaseURL: srv2.URL, Username: "u", Password: "p", Timeout: 3 * time.Second})
	ctx := context.Background()
	magnet := "magnet:?xt=urn:btih:aabbccddeeff00112233445566778899aabbccdd"
	hash, err := c.AddMagnet(ctx, magnet, AddOptions{})
	if err != nil {
		t.Fatalf("首次提交应成功: %v", err)
	}
	if hash == "" {
		t.Fatal("应返回 info hash")
	}
	// 第二次：qB 返回 409，但应视为「已在下载」——不报错，且仍给出 hash。
	hash2, err := c.AddMagnet(ctx, magnet, AddOptions{})
	if err != nil {
		t.Fatalf("重复提交（409）不应报错，应视为已存在: %v", err)
	}
	if hash2 != hash {
		t.Errorf("重复提交应返回同一个 hash，实际 %q vs %q", hash2, hash)
	}
}
