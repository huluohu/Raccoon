package n115

// client_test.go：n115 包的单测。
//
// 用 httptest 起一个假 115 服务（不 mock http.Client，真实走 HTTP），覆盖：
// 设备码申请、令牌轮询的成功/失败/过期、离线任务提交与查询、令牌缺失、
// 错误响应转 APIError 且响应体被截断。
//
// 假服务返回的 JSON 形状按 115 开放平台文档编写（未实测），所以这里同时
// 起到「把协议契约钉死」的作用：改 api.go 的字段名会直接让这些用例失败。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fake115 假 115 服务。
//
// 按路径分发到不同的处理器，测试用例按需替换 handler。
type fake115 struct {
	t      *testing.T
	server *httptest.Server

	handlers map[string]http.HandlerFunc
	// requests 记录收到的请求路径与表单参数，供断言用。
	requests []recordedRequest
}

// recordedRequest 一次请求的记录。
type recordedRequest struct {
	Path  string
	Query url.Values
	Form  url.Values
	Auth  string
}

// newFake115 起一个假 115 服务。
func newFake115(t *testing.T) *fake115 {
	t.Helper()
	f := &fake115{t: t, handlers: map[string]http.HandlerFunc{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

// serve 分发请求。
func (f *fake115) serve(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	f.requests = append(f.requests, recordedRequest{
		Path:  r.URL.Path,
		Query: r.URL.Query(),
		Form:  r.PostForm,
		Auth:  r.Header.Get("Authorization"),
	})
	if h, ok := f.handlers[r.URL.Path]; ok {
		h(w, r)
		return
	}
	http.NotFound(w, r)
}

// handle 注册某个路径的处理器。
func (f *fake115) handle(path string, h http.HandlerFunc) {
	f.handlers[path] = h
}

// url 返回假服务地址。
func (f *fake115) url() string { return f.server.URL }

// lastRequest 返回最后一次请求记录。
func (f *fake115) lastRequest() recordedRequest {
	f.t.Helper()
	if len(f.requests) == 0 {
		f.t.Fatal("没有任何请求发出")
	}
	return f.requests[len(f.requests)-1]
}

// writeJSON 写 JSON 响应。
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// staticToken 一个返回固定令牌的 TokenProvider。
type staticToken struct {
	token string
	err   error
}

// Token 实现 TokenProvider。
func (s staticToken) Token(context.Context) (string, error) { return s.token, s.err }

// newTestClient 用假服务造一个客户端。
//
// 三个基址都指向同一个假服务：115 真实环境是三个域名，这里合成一个方便
// 断言，同时不影响被测逻辑（客户端只是拼接基址 + 路径）。
func newTestClient(f *fake115, tp TokenProvider) *Client {
	return New(Config{
		BaseURL:       f.url(),
		AuthBaseURL:   f.url(),
		QRCodeBaseURL: f.url(),
		HTTPClient:    f.server.Client(),
		TokenProvider: tp,
		ClientID:      "test-client-id",
		UserAgent:     "raccoon-test",
		PollInterval:  time.Millisecond,
	})
}

// ---- 设备码申请 ----

// TestRequestDeviceCode 覆盖设备码申请：字段按文档取，PKCE 参数齐全。
func TestRequestDeviceCode(t *testing.T) {
	f := newFake115(t)
	f.handle(PathAuthDeviceCode, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"state": 1,
			"code":  0,
			"data": map[string]any{
				"uid":    "device-uid-001",
				"time":   1700000000,
				"qrcode": "qrcode-content-xxx",
				"sign":   "sign-xxx",
			},
		})
	})
	c := newTestClient(f, staticToken{token: "tok"})

	state, err := c.RequestDeviceCode(context.Background())
	if err != nil {
		t.Fatalf("申请设备码失败: %v", err)
	}
	if state.Status != AuthStatusPending {
		t.Errorf("状态 = %q，期望 %q", state.Status, AuthStatusPending)
	}
	if state.DeviceCode != "device-uid-001" {
		t.Errorf("设备码 = %q，期望 %q", state.DeviceCode, "device-uid-001")
	}
	if state.QRCodeContent != "qrcode-content-xxx" {
		t.Errorf("二维码内容 = %q，期望 %q", state.QRCodeContent, "qrcode-content-xxx")
	}
	if state.CodeVerifier == "" {
		t.Error("PKCE 校验原值为空")
	}
	if state.ExpiresAt.IsZero() {
		t.Error("设备码到期时间未设置")
	}
	// 请求参数：client_id + PKCE 三件套。
	req := f.lastRequest()
	if got := req.Form.Get("client_id"); got != "test-client-id" {
		t.Errorf("client_id = %q，期望 %q", got, "test-client-id")
	}
	if got := req.Form.Get("code_challenge_method"); got != "sha256" {
		t.Errorf("code_challenge_method = %q，期望 sha256", got)
	}
	if req.Form.Get("code_challenge") == "" {
		t.Error("code_challenge 未提交")
	}
	if req.Auth != "" {
		t.Errorf("申请设备码不该带 Authorization，实际 %q", req.Auth)
	}
}

// TestRequestDeviceCodeMissingClientID 未配 ClientID 时直接报错，不发请求。
func TestRequestDeviceCodeMissingClientID(t *testing.T) {
	f := newFake115(t)
	c := New(Config{BaseURL: f.url(), AuthBaseURL: f.url(), QRCodeBaseURL: f.url()})
	if _, err := c.RequestDeviceCode(context.Background()); err == nil {
		t.Fatal("未配置 ClientID 时应当报错")
	}
	if len(f.requests) != 0 {
		t.Errorf("不应发出请求，实际发了 %d 次", len(f.requests))
	}
}

// ---- 令牌轮询 ----

// TestPollDeviceStatusAuthorized 覆盖轮询成功：用户确认后换到令牌。
func TestPollDeviceStatusAuthorized(t *testing.T) {
	f := newFake115(t)
	f.handle(PathQRCodeStatus, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"state":   1,
			"code":    0,
			"message": "",
			"data":    map[string]any{"status": 2, "msg": "授权成功", "version": "1"},
		})
	})
	f.handle(PathDeviceCodeToToken, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"state": 1,
			"code":  0,
			"data": map[string]any{
				"access_token":  "access-token-abc",
				"refresh_token": "refresh-token-abc",
				"expires_in":    7200,
				"uid":           "user-uid-1",
			},
		})
	})
	c := newTestClient(f, staticToken{token: "tok"})

	state := &AuthState{DeviceCode: "device-uid-001", CodeVerifier: "verifier-abc"}
	got, err := c.PollDeviceStatus(context.Background(), state)
	if err != nil {
		t.Fatalf("轮询扫码状态失败: %v", err)
	}
	if got.Status != AuthStatusAuthorized {
		t.Fatalf("状态 = %q，期望 %q", got.Status, AuthStatusAuthorized)
	}
	if got.AccessToken != "access-token-abc" {
		t.Errorf("access_token = %q，期望 %q", got.AccessToken, "access-token-abc")
	}
	if got.RefreshToken != "refresh-token-abc" {
		t.Errorf("refresh_token = %q，期望 %q", got.RefreshToken, "refresh-token-abc")
	}
	if got.UID != "user-uid-1" {
		t.Errorf("uid = %q，期望 %q", got.UID, "user-uid-1")
	}
	if got.ExpiresIn != 7200 {
		t.Errorf("expires_in = %d，期望 7200", got.ExpiresIn)
	}
	// 换令牌时要带上原始的 code_verifier，PKCE 校验就靠它。
	if form := f.requests[len(f.requests)-1].Form; form.Get("code_verifier") != "verifier-abc" {
		t.Errorf("code_verifier = %q，期望 %q", form.Get("code_verifier"), "verifier-abc")
	}
}

// TestPollDeviceStatusPending 覆盖「已扫码但未确认」：状态保持 pending。
func TestPollDeviceStatusPending(t *testing.T) {
	f := newFake115(t)
	f.handle(PathQRCodeStatus, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"state": 1,
			"code":  0,
			"data":  map[string]any{"status": 1, "msg": "扫码成功，等待确认"},
		})
	})
	c := newTestClient(f, staticToken{token: "tok"})

	got, err := c.PollDeviceStatus(context.Background(), &AuthState{DeviceCode: "uid", CodeVerifier: "v"})
	if err != nil {
		t.Fatalf("轮询扫码状态失败: %v", err)
	}
	if got.Status != AuthStatusPending {
		t.Errorf("状态 = %q，期望 %q", got.Status, AuthStatusPending)
	}
	if got.AccessToken != "" {
		t.Error("未确认授权时不应有 access_token")
	}
}

// TestPollDeviceStatusExpired 覆盖二维码失效：设备码过期，需要重新申请。
func TestPollDeviceStatusExpired(t *testing.T) {
	f := newFake115(t)
	f.handle(PathQRCodeStatus, func(w http.ResponseWriter, r *http.Request) {
		// 该接口 state=0 表示二维码无效、结束轮询。
		writeJSON(w, map[string]any{"state": 0, "code": 0, "message": "二维码无效"})
	})
	c := newTestClient(f, staticToken{token: "tok"})

	got, err := c.PollDeviceStatus(context.Background(), &AuthState{DeviceCode: "uid", CodeVerifier: "v"})
	if err != nil {
		t.Fatalf("二维码失效不应返回错误，而应作为状态返回: %v", err)
	}
	if got.Status != AuthStatusExpired {
		t.Errorf("状态 = %q，期望 %q", got.Status, AuthStatusExpired)
	}
}

// TestPollDeviceStatusFailed 覆盖轮询失败：接口返回业务错误码。
func TestPollDeviceStatusFailed(t *testing.T) {
	f := newFake115(t)
	f.handle(PathQRCodeStatus, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"state":   0,
			"code":    40140104,
			"message": "sign 签名失败",
		})
	})
	c := newTestClient(f, staticToken{token: "tok"})

	got, err := c.PollDeviceStatus(context.Background(), &AuthState{DeviceCode: "uid", CodeVerifier: "v"})
	if err == nil {
		t.Fatal("业务错误码应返回错误")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("错误类型 = %T，期望 *APIError", err)
	}
	if apiErr.Code != "40140104" {
		t.Errorf("业务码 = %q，期望 %q", apiErr.Code, "40140104")
	}
	if got.Status != AuthStatusFailed {
		t.Errorf("状态 = %q，期望 %q", got.Status, AuthStatusFailed)
	}
}

// TestExchangeDeviceCodeError 覆盖换令牌失败。
func TestExchangeDeviceCodeError(t *testing.T) {
	f := newFake115(t)
	f.handle(PathDeviceCodeToToken, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"state":   0,
			"code":    40140113,
			"message": "code_verifier 验证失败",
		})
	})
	c := newTestClient(f, staticToken{token: "tok"})

	got, err := c.ExchangeDeviceCode(context.Background(), &AuthState{DeviceCode: "uid", CodeVerifier: "bad"})
	if err == nil {
		t.Fatal("换令牌失败应返回错误")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("错误类型 = %T，期望 *APIError", err)
	}
	if apiErr.Code != "40140113" {
		t.Errorf("业务码 = %q，期望 %q", apiErr.Code, "40140113")
	}
	if got.Status != AuthStatusFailed {
		t.Errorf("状态 = %q，期望 %q", got.Status, AuthStatusFailed)
	}
}

// ---- 令牌刷新 ----

// TestRefreshToken 覆盖刷新令牌：换到新令牌并算出到期时间。
func TestRefreshToken(t *testing.T) {
	f := newFake115(t)
	f.handle(PathRefreshToken, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"state": 1,
			"code":  0,
			"data": map[string]any{
				"access_token":  "new-access-token",
				"refresh_token": "new-refresh-token",
				"expires_in":    2592000,
			},
		})
	})
	c := newTestClient(f, staticToken{token: "tok"})

	before := time.Now()
	pair, err := c.RefreshToken(context.Background(), "old-refresh-token")
	if err != nil {
		t.Fatalf("刷新令牌失败: %v", err)
	}
	if pair.AccessToken != "new-access-token" {
		t.Errorf("access_token = %q，期望 %q", pair.AccessToken, "new-access-token")
	}
	if pair.RefreshToken != "new-refresh-token" {
		t.Errorf("refresh_token = %q，期望 %q", pair.RefreshToken, "new-refresh-token")
	}
	if pair.ExpiresIn != 2592000 {
		t.Errorf("expires_in = %d，期望 2592000", pair.ExpiresIn)
	}
	if pair.ExpiresAt.Before(before.Add(time.Hour)) {
		t.Errorf("到期时间 %v 不合理，应在 1 小时之后", pair.ExpiresAt)
	}
	// 刷新请求必须带上原 refresh_token。
	if got := f.lastRequest().Form.Get("refresh_token"); got != "old-refresh-token" {
		t.Errorf("refresh_token = %q，期望 %q", got, "old-refresh-token")
	}
}

// TestRefreshTokenEmpty 缺少 refresh_token 时直接报错。
func TestRefreshTokenEmpty(t *testing.T) {
	f := newFake115(t)
	c := newTestClient(f, staticToken{token: "tok"})
	if _, err := c.RefreshToken(context.Background(), "  "); err == nil {
		t.Fatal("缺少 refresh_token 时应当报错")
	}
}

// ---- 离线任务 ----

// TestSubmitOffline 覆盖提交离线任务：参数透传与返回的任务标识。
func TestSubmitOffline(t *testing.T) {
	f := newFake115(t)
	f.handle(PathOfflineAddTaskURLs, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"state":   true,
			"code":    0,
			"message": "",
			"data": []map[string]any{
				{
					"state":     true,
					"code":      0,
					"message":   "",
					"info_hash": "hash-abc",
					"url":       "magnet:?xt=urn:btih:abc",
				},
			},
		})
	})
	c := newTestClient(f, staticToken{token: "access-token-xyz"})

	task, err := c.SubmitOffline(context.Background(), OfflineRequest{
		URL:      "magnet:?xt=urn:btih:abc",
		WPPath:   "downloads/movies",
		WPPathID: "249163533602609229",
		CID:      "249163533602609229",
	})
	if err != nil {
		t.Fatalf("提交离线任务失败: %v", err)
	}
	if task.OfflineID != "hash-abc" {
		t.Errorf("offline_id = %q，期望 %q", task.OfflineID, "hash-abc")
	}
	if task.Status != OfflineStatusPending {
		t.Errorf("状态 = %q，期望 %q", task.Status, OfflineStatusPending)
	}
	// 请求参数：链接、保存目录、目标目录都要送到。
	req := f.lastRequest()
	if got := req.Form.Get("urls"); got != "magnet:?xt=urn:btih:abc" {
		t.Errorf("urls = %q，期望磁力链", got)
	}
	if got := req.Form.Get("wp_path"); got != "downloads/movies" {
		t.Errorf("wp_path = %q，期望 %q", got, "downloads/movies")
	}
	if got := req.Form.Get("wp_path_id"); got != "249163533602609229" {
		t.Errorf("wp_path_id = %q，期望 %q", got, "249163533602609229")
	}
	if got := req.Form.Get("cid"); got != "249163533602609229" {
		t.Errorf("cid = %q，期望 %q", got, "249163533602609229")
	}
	// 鉴权头必须带上令牌。
	if req.Auth != "Bearer access-token-xyz" {
		t.Errorf("Authorization = %q，期望 %q", req.Auth, "Bearer access-token-xyz")
	}
}

// TestSubmitOfflineItemFailed 单条链接添加失败时整体报错。
func TestSubmitOfflineItemFailed(t *testing.T) {
	f := newFake115(t)
	f.handle(PathOfflineAddTaskURLs, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"state": true,
			"code":  0,
			"data": []map[string]any{
				{"state": false, "code": 20018, "message": "文件不存在或已删除"},
			},
		})
	})
	c := newTestClient(f, staticToken{token: "tok"})

	if _, err := c.SubmitOffline(context.Background(), OfflineRequest{URL: "magnet:?xt=urn:btih:bad"}); err == nil {
		t.Fatal("单条链接添加失败时应返回错误")
	}
}

// TestSubmitOfflineMissingURL 缺少链接时直接报错，不发请求。
func TestSubmitOfflineMissingURL(t *testing.T) {
	f := newFake115(t)
	c := newTestClient(f, staticToken{token: "tok"})
	if _, err := c.SubmitOffline(context.Background(), OfflineRequest{}); err == nil {
		t.Fatal("缺少链接时应当报错")
	}
	if len(f.requests) != 0 {
		t.Errorf("不应发出请求，实际发了 %d 次", len(f.requests))
	}
}

// TestGetOfflineTaskByOfflineID 按 offline_id（info_hash）查到任务。
func TestGetOfflineTaskByOfflineID(t *testing.T) {
	f := newFake115(t)
	f.handle(PathOfflineGetTaskList, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"state": true,
			"code":  0,
			"data": map[string]any{
				"page":       1,
				"page_count": 1,
				"count":      1,
				"tasks": []map[string]any{
					{
						"info_hash":   "hash-abc",
						"file_id":     "file-xyz",
						"name":        "BigBuckBunny.mp4",
						"status":      2,
						"percentDone": 100,
						"size":        1048576,
						"url":         "magnet:?xt=urn:btih:abc",
						"wp_path_id":  "249163533602609229",
					},
				},
			},
		})
	})
	c := newTestClient(f, staticToken{token: "tok"})

	task, err := c.GetOfflineTask(context.Background(), "hash-abc")
	if err != nil {
		t.Fatalf("查询离线任务失败: %v", err)
	}
	if task.OfflineID != "hash-abc" {
		t.Errorf("offline_id = %q，期望 %q", task.OfflineID, "hash-abc")
	}
	if task.TaskID != "file-xyz" {
		t.Errorf("task_id = %q，期望 %q", task.TaskID, "file-xyz")
	}
	if task.Status != OfflineStatusDone {
		t.Errorf("状态 = %q，期望 %q", task.Status, OfflineStatusDone)
	}
	if task.Progress != 100 {
		t.Errorf("进度 = %d，期望 100", task.Progress)
	}
	if task.Size != 1048576 {
		t.Errorf("大小 = %d，期望 1048576", task.Size)
	}
}

// TestGetOfflineTaskByTaskID 按 task_id（file_id）也能查到同一个任务。
func TestGetOfflineTaskByTaskID(t *testing.T) {
	f := newFake115(t)
	f.handle(PathOfflineGetTaskList, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"state": true,
			"code":  0,
			"data": map[string]any{
				"page":       1,
				"page_count": 1,
				"count":      1,
				"tasks": []map[string]any{
					{"info_hash": "hash-abc", "file_id": "file-xyz", "status": 1, "percentDone": 42},
				},
			},
		})
	})
	c := newTestClient(f, staticToken{token: "tok"})

	task, err := c.GetOfflineTask(context.Background(), "file-xyz")
	if err != nil {
		t.Fatalf("按 task_id 查询失败: %v", err)
	}
	if task.OfflineID != "hash-abc" {
		t.Errorf("offline_id = %q，期望 %q", task.OfflineID, "hash-abc")
	}
	if task.Status != OfflineStatusRunning {
		t.Errorf("状态 = %q，期望 %q", task.Status, OfflineStatusRunning)
	}
	if task.Progress != 42 {
		t.Errorf("进度 = %d，期望 42", task.Progress)
	}
}

// TestGetOfflineTaskNotFound 查不到时返回 ErrNotFound。
func TestGetOfflineTaskNotFound(t *testing.T) {
	f := newFake115(t)
	f.handle(PathOfflineGetTaskList, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"state": true,
			"code":  0,
			"data":  map[string]any{"page": 1, "page_count": 1, "count": 0, "tasks": []map[string]any{}},
		})
	})
	c := newTestClient(f, staticToken{token: "tok"})

	if _, err := c.GetOfflineTask(context.Background(), "no-such-hash"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("错误 = %v，期望 ErrNotFound", err)
	}
}

// TestListOfflineTasksPaging 任务列表按页取，分页信息要正确归一化。
func TestListOfflineTasksPaging(t *testing.T) {
	f := newFake115(t)
	f.handle(PathOfflineGetTaskList, func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page == "1" {
			writeJSON(w, map[string]any{
				"state": true,
				"code":  0,
				"data": map[string]any{
					"page": 1, "page_count": 2, "count": 2,
					"tasks": []map[string]any{{"info_hash": "h1", "status": 1, "percentDone": 10}},
				},
			})
			return
		}
		writeJSON(w, map[string]any{
			"state": true,
			"code":  0,
			"data": map[string]any{
				"page": 2, "page_count": 2, "count": 2,
				"tasks": []map[string]any{{"info_hash": "h2", "status": -1}},
			},
		})
	})
	c := newTestClient(f, staticToken{token: "tok"})

	page1, err := c.ListOfflineTasks(context.Background(), 1)
	if err != nil {
		t.Fatalf("拉取第 1 页失败: %v", err)
	}
	if page1.PageCount != 2 || len(page1.Tasks) != 1 {
		t.Errorf("第 1 页 = %+v，期望 1 个任务 / 共 2 页", page1)
	}
	// GET 的分页参数走 query string，不走请求体。
	if got := f.lastRequest().Query.Get("page"); got != "1" {
		t.Errorf("page = %q，期望 1", got)
	}

	page2, err := c.ListOfflineTasks(context.Background(), 2)
	if err != nil {
		t.Fatalf("拉取第 2 页失败: %v", err)
	}
	if len(page2.Tasks) != 1 {
		t.Fatalf("第 2 页任务数 = %d，期望 1", len(page2.Tasks))
	}
	if page2.Tasks[0].Status != OfflineStatusFailed {
		t.Errorf("失败任务状态 = %q，期望 %q", page2.Tasks[0].Status, OfflineStatusFailed)
	}
}

// TestCancelOfflineTask 覆盖取消离线任务：参数与鉴权都要对。
func TestCancelOfflineTask(t *testing.T) {
	f := newFake115(t)
	f.handle(PathOfflineDelTask, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"state": true, "code": 0, "message": "", "data": []string{}})
	})
	c := newTestClient(f, staticToken{token: "access-token-xyz"})

	if err := c.CancelOfflineTask(context.Background(), "hash-abc", true); err != nil {
		t.Fatalf("取消离线任务失败: %v", err)
	}
	req := f.lastRequest()
	if got := req.Form.Get("info_hash"); got != "hash-abc" {
		t.Errorf("info_hash = %q，期望 %q", got, "hash-abc")
	}
	if got := req.Form.Get("del_source_file"); got != "1" {
		t.Errorf("del_source_file = %q，期望 1", got)
	}
	if req.Auth != "Bearer access-token-xyz" {
		t.Errorf("Authorization = %q，期望 %q", req.Auth, "Bearer access-token-xyz")
	}
}

// TestCancelOfflineTaskMissingID 缺少任务标识时直接报错。
func TestCancelOfflineTaskMissingID(t *testing.T) {
	f := newFake115(t)
	c := newTestClient(f, staticToken{token: "tok"})
	if err := c.CancelOfflineTask(context.Background(), "", false); err == nil {
		t.Fatal("缺少任务标识时应当报错")
	}
}

// ---- 文件列表 ----

// TestListFiles 覆盖文件列表查询：字段归一化与目录判定。
func TestListFiles(t *testing.T) {
	f := newFake115(t)
	f.handle(PathUFileFiles, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"state": true,
			"code":  0,
			"data": []map[string]any{
				{
					"fid": "file-1", "pid": "0", "fc": "1", "fn": "movie.mkv",
					"fs": 1048576, "sha1": "abc123", "ico": "mkv", "pc": "pick1",
					"uet": 1700000000, "uppt": 1699999999,
				},
				{
					"fid": "dir-1", "pid": "0", "fc": "0", "fn": "subdir",
					"fs": 0, "ico": "",
				},
			},
			"count": 2,
			"limit": 20,
		})
	})
	c := newTestClient(f, staticToken{token: "tok"})

	files, err := c.ListFiles(context.Background(), "12345", FileListOptions{Limit: 20, IncludeDirs: true})
	if err != nil {
		t.Fatalf("查询文件列表失败: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("文件数 = %d，期望 2", len(files))
	}
	if files[0].FileID != "file-1" || files[0].Name != "movie.mkv" {
		t.Errorf("文件 = %+v，期望 file-1 / movie.mkv", files[0])
	}
	if files[0].IsDir {
		t.Error("fc=1 应当是文件而不是目录")
	}
	if files[0].Size != 1048576 {
		t.Errorf("大小 = %d，期望 1048576", files[0].Size)
	}
	if files[0].SHA1 != "abc123" {
		t.Errorf("sha1 = %q，期望 abc123", files[0].SHA1)
	}
	if !files[1].IsDir {
		t.Error("fc=0 应当是目录")
	}
	// 查询参数走 query string。
	req := f.lastRequest()
	if got := req.Query.Get("cid"); got != "12345" {
		t.Errorf("cid = %q，期望 12345", got)
	}
	if got := req.Query.Get("show_dir"); got != "1" {
		t.Errorf("show_dir = %q，期望 1", got)
	}
}

// ---- 令牌缺失与可用性 ----

// TestNoTokenProvider 未配置令牌提供器时，需要鉴权的接口报 ErrNoToken。
func TestNoTokenProvider(t *testing.T) {
	f := newFake115(t)
	c := New(Config{BaseURL: f.url(), AuthBaseURL: f.url(), QRCodeBaseURL: f.url()})

	if _, err := c.ListFiles(context.Background(), "0", FileListOptions{}); !errors.Is(err, ErrNoToken) {
		t.Fatalf("错误 = %v，期望 ErrNoToken", err)
	}
	if len(f.requests) != 0 {
		t.Errorf("没有令牌时不应发出请求，实际发了 %d 次", len(f.requests))
	}
}

// TestEmptyToken 提供器返回空令牌时同样报 ErrNoToken。
func TestEmptyToken(t *testing.T) {
	f := newFake115(t)
	c := newTestClient(f, staticToken{token: "   "})

	if _, err := c.ListFiles(context.Background(), "0", FileListOptions{}); !errors.Is(err, ErrNoToken) {
		t.Fatalf("错误 = %v，期望 ErrNoToken", err)
	}
}

// TestTokenProviderError 提供器报错时错误要带上下文。
func TestTokenProviderError(t *testing.T) {
	f := newFake115(t)
	c := newTestClient(f, staticToken{err: errors.New("凭据中心不可用")})

	_, err := c.ListFiles(context.Background(), "0", FileListOptions{})
	if err == nil {
		t.Fatal("令牌提供器报错时应当返回错误")
	}
	if !errors.Is(err, errors.New("凭据中心不可用")) && !strings.Contains(err.Error(), "凭据中心不可用") {
		t.Errorf("错误 = %v，期望包含底层原因", err)
	}
}

// TestIsAvailable 令牌可用时返回 nil。
func TestIsAvailable(t *testing.T) {
	f := newFake115(t)
	f.handle(PathUserInfo, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"state": true,
			"code":  0,
			"data": map[string]any{
				"user_id":   1001,
				"user_name": "tester",
				"rt_space_info": map[string]any{
					"all_total":  1000,
					"all_remain": 600,
					"all_use":    400,
				},
				"vip_info": map[string]any{"level_name": "SVIP"},
			},
		})
	})
	c := newTestClient(f, staticToken{token: "access-token-xyz"})

	if err := c.IsAvailable(context.Background()); err != nil {
		t.Fatalf("令牌应可用，却报错: %v", err)
	}
	// 用户信息要带上鉴权头。
	if got := f.lastRequest().Auth; got != "Bearer access-token-xyz" {
		t.Errorf("Authorization = %q，期望 %q", got, "Bearer access-token-xyz")
	}
}

// TestIsAvailableNoToken 没有令牌时不可用。
func TestIsAvailableNoToken(t *testing.T) {
	f := newFake115(t)
	c := newTestClient(f, staticToken{token: ""})

	if err := c.IsAvailable(context.Background()); !errors.Is(err, ErrNoToken) {
		t.Fatalf("错误 = %v，期望 ErrNoToken", err)
	}
}

// ---- 错误处理 ----

// TestAPIErrorFromHTTPStatus 非 2xx 响应转成 APIError，带状态码与响应体摘要。
func TestAPIErrorFromHTTPStatus(t *testing.T) {
	f := newFake115(t)
	f.handle(PathUFileFiles, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"内部错误","code":500}`))
	})
	c := newTestClient(f, staticToken{token: "tok"})

	_, err := c.ListFiles(context.Background(), "0", FileListOptions{})
	if err == nil {
		t.Fatal("500 响应应返回错误")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("错误类型 = %T，期望 *APIError", err)
	}
	if apiErr.Status != http.StatusInternalServerError {
		t.Errorf("状态码 = %d，期望 %d", apiErr.Status, http.StatusInternalServerError)
	}
	if apiErr.Code != "500" {
		t.Errorf("业务码 = %q，期望 500", apiErr.Code)
	}
	if apiErr.Message != "内部错误" {
		t.Errorf("消息 = %q，期望 %q", apiErr.Message, "内部错误")
	}
}

// TestAPIErrorBodyTruncated 响应体超长时摘要被截断到 512 字节以内。
func TestAPIErrorBodyTruncated(t *testing.T) {
	f := newFake115(t)
	// 造一个远大于 512 字节的响应体。
	huge := strings.Repeat("x", 4096)
	f.handle(PathUFileFiles, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(huge))
	})
	c := newTestClient(f, staticToken{token: "tok"})

	_, err := c.ListFiles(context.Background(), "0", FileListOptions{})
	if err == nil {
		t.Fatal("502 响应应返回错误")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("错误类型 = %T，期望 *APIError", err)
	}
	if len(apiErr.Message) > maxErrorBodyBytes {
		t.Errorf("摘要长度 = %d，超过上限 %d", len(apiErr.Message), maxErrorBodyBytes)
	}
	if len(apiErr.Message) == 0 {
		t.Error("截断后摘要不应为空")
	}
	if !strings.HasPrefix(apiErr.Message, "xxx") {
		t.Errorf("摘要 = %q，期望保留响应体开头", apiErr.Message)
	}
}

// TestAPIErrorDoesNotLeakToken 错误消息里不能出现令牌。
func TestAPIErrorDoesNotLeakToken(t *testing.T) {
	f := newFake115(t)
	const secret = "super-secret-access-token-value"
	f.handle(PathUFileFiles, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		// 远端把令牌回显在错误信息里——这种情况绝不能让它流进日志。
		_, _ = fmt.Fprintf(w, `{"message":"invalid token: %s","code":40140125}`, secret)
	})
	c := newTestClient(f, staticToken{token: secret})

	_, err := c.ListFiles(context.Background(), "0", FileListOptions{})
	if err == nil {
		t.Fatal("401 响应应返回错误")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("错误消息泄漏了令牌: %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("错误类型 = %T，期望 *APIError", err)
	}
	if strings.Contains(apiErr.Message, secret) {
		t.Errorf("错误摘要泄漏了令牌: %q", apiErr.Message)
	}
}

// TestBusinessErrorWithHTTP200 业务失败（HTTP 200 + state=false）也要转 APIError。
func TestBusinessErrorWithHTTP200(t *testing.T) {
	f := newFake115(t)
	f.handle(PathOfflineGetTaskList, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"state":   false,
			"code":    40140125,
			"message": "access_token 已过期，请刷新",
		})
	})
	c := newTestClient(f, staticToken{token: "tok"})

	_, err := c.ListOfflineTasks(context.Background(), 1)
	if err == nil {
		t.Fatal("业务失败应返回错误")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("错误类型 = %T，期望 *APIError", err)
	}
	if apiErr.Code != "40140125" {
		t.Errorf("业务码 = %q，期望 %q", apiErr.Code, "40140125")
	}
	if !strings.Contains(apiErr.Message, "已过期") {
		t.Errorf("消息 = %q，期望包含远端提示", apiErr.Message)
	}
}

// TestEnvelopeErrorMessageRedacted 远端消息里带令牌字段名时也要被抹掉。
func TestEnvelopeErrorMessageRedacted(t *testing.T) {
	f := newFake115(t)
	f.handle(PathOfflineGetTaskList, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"state":   false,
			"code":    40140116,
			"message": `refresh_token=abcdef123456 invalid`,
		})
	})
	c := newTestClient(f, staticToken{token: "tok"})

	_, err := c.ListOfflineTasks(context.Background(), 1)
	if err == nil {
		t.Fatal("业务失败应返回错误")
	}
	if strings.Contains(err.Error(), "abcdef123456") {
		t.Fatalf("错误消息泄漏了 refresh_token: %v", err)
	}
}

// ---- 协议契约 ----

// TestNetworkErrorDoesNotLeakQueryParams 网络错误的消息里不能带出 query 参数。
func TestNetworkErrorDoesNotLeakQueryParams(t *testing.T) {
	f := newFake115(t)
	// 假服务直接断开连接，客户端拿到的是 *url.Error（内含完整 URL）。
	f.handle(PathQRCodeStatus, func(w http.ResponseWriter, r *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("响应不支持 hijack")
			return
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			t.Errorf("hijack 失败: %v", err)
			return
		}
		_ = conn.Close()
	})
	c := newTestClient(f, staticToken{token: "tok"})

	state := &AuthState{DeviceCode: "uid-1", CodeVerifier: "v", PollSign: "sign-secret"}
	_, err := c.PollDeviceStatus(context.Background(), state)
	if err == nil {
		t.Fatal("连接被中断时应返回错误")
	}
	if strings.Contains(err.Error(), "sign-secret") {
		t.Fatalf("错误消息泄漏了 query 参数: %v", err)
	}
}

// TestBusinessErrorWithCodeOnly 只填 code 不填 state 的失败也要被识别。
func TestBusinessErrorWithCodeOnly(t *testing.T) {
	f := newFake115(t)
	f.handle(PathOfflineGetTaskList, func(w http.ResponseWriter, r *http.Request) {
		// 115 有些接口失败时只给业务码，state 仍是 true——漏判会静默吞错误。
		writeJSON(w, map[string]any{
			"state":   true,
			"code":    990002,
			"message": "参数错误",
		})
	})
	c := newTestClient(f, staticToken{token: "tok"})

	_, err := c.ListOfflineTasks(context.Background(), 1)
	if err == nil {
		t.Fatal("只填 code 的业务失败也应返回错误")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("错误类型 = %T，期望 *APIError", err)
	}
	if apiErr.Code != "990002" {
		t.Errorf("业务码 = %q，期望 %q", apiErr.Code, "990002")
	}
}

// TestAwaitAuthorization 覆盖完整的等待授权：先扫码待确认，再确认授权换到令牌。
func TestAwaitAuthorization(t *testing.T) {
	f := newFake115(t)
	var polls int
	f.handle(PathQRCodeStatus, func(w http.ResponseWriter, r *http.Request) {
		polls++
		if polls == 1 {
			// 第一次：已扫码，等待用户确认。
			writeJSON(w, map[string]any{
				"state": 1, "code": 0,
				"data": map[string]any{"status": 1, "msg": "扫码成功，等待确认"},
			})
			return
		}
		// 第二次：用户已确认。
		writeJSON(w, map[string]any{
			"state": 1, "code": 0,
			"data": map[string]any{"status": 2, "msg": "授权成功"},
		})
	})
	f.handle(PathDeviceCodeToToken, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"state": 1, "code": 0,
			"data": map[string]any{
				"access_token":  "access-token-abc",
				"refresh_token": "refresh-token-abc",
				"expires_in":    7200,
			},
		})
	})
	c := newTestClient(f, staticToken{token: "tok"})

	state := &AuthState{
		Status:       AuthStatusPending,
		DeviceCode:   "uid-1",
		CodeVerifier: "verifier",
		Interval:     time.Millisecond,
		ExpiresAt:    time.Now().Add(time.Minute),
	}
	got, err := c.AwaitAuthorization(context.Background(), state)
	if err != nil {
		t.Fatalf("等待授权失败: %v", err)
	}
	if got.Status != AuthStatusAuthorized {
		t.Fatalf("状态 = %q，期望 %q", got.Status, AuthStatusAuthorized)
	}
	if !got.Authorized() {
		t.Error("Authorized() 应为 true")
	}
	if got.AccessToken != "access-token-abc" {
		t.Errorf("access_token = %q，期望 %q", got.AccessToken, "access-token-abc")
	}
	if polls < 2 {
		t.Errorf("轮询次数 = %d，期望至少 2 次", polls)
	}
}

// TestAwaitAuthorizationExpired 设备码已过期时直接结束，返回 expired 状态。
func TestAwaitAuthorizationExpired(t *testing.T) {
	f := newFake115(t)
	c := newTestClient(f, staticToken{token: "tok"})

	state := &AuthState{
		Status:     AuthStatusPending,
		DeviceCode: "uid-1",
		Interval:   time.Millisecond,
		ExpiresAt:  time.Now().Add(-time.Second), // 已过期
	}
	got, err := c.AwaitAuthorization(context.Background(), state)
	if err != nil {
		t.Fatalf("设备码过期不应返回错误: %v", err)
	}
	if got.Status != AuthStatusExpired {
		t.Errorf("状态 = %q，期望 %q", got.Status, AuthStatusExpired)
	}
	// 过期时不该再发请求。
	if len(f.requests) != 0 {
		t.Errorf("设备码已过期时不应发请求，实际发了 %d 次", len(f.requests))
	}
}

// TestAwaitAuthorizationContextCancel 上下文取消时结束等待。
func TestAwaitAuthorizationContextCancel(t *testing.T) {
	f := newFake115(t)
	f.handle(PathQRCodeStatus, func(w http.ResponseWriter, r *http.Request) {
		// 一直返回「等待确认」，让循环一直转下去直到上下文取消。
		writeJSON(w, map[string]any{
			"state": 1, "code": 0,
			"data": map[string]any{"status": 1},
		})
	})
	c := newTestClient(f, staticToken{token: "tok"})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	state := &AuthState{
		Status:     AuthStatusPending,
		DeviceCode: "uid-1",
		Interval:   time.Millisecond,
		ExpiresAt:  time.Now().Add(time.Minute),
	}
	got, err := c.AwaitAuthorization(ctx, state)
	if err == nil {
		t.Fatal("上下文取消时应返回错误")
	}
	if got.Status != AuthStatusFailed {
		t.Errorf("状态 = %q，期望 %q", got.Status, AuthStatusFailed)
	}
}

// refresherToken 一个实现 TokenRefresher 的假提供器。
type refresherToken struct {
	token        string
	refreshToken string
	saved        []string
	saveErr      error
}

// Token 实现 TokenProvider。
func (r *refresherToken) Token(context.Context) (string, error) { return r.token, nil }

// RefreshToken 实现 TokenRefresher。
func (r *refresherToken) RefreshToken(context.Context) (string, error) { return r.refreshToken, nil }

// SaveTokens 实现 TokenRefresher。
func (r *refresherToken) SaveTokens(_ context.Context, accessToken, refreshToken string, _ time.Time) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	r.saved = append(r.saved, accessToken+"|"+refreshToken)
	return nil
}

// TestRefreshFromProvider 从提供器取 refresh_token 刷新，并回写新令牌。
func TestRefreshFromProvider(t *testing.T) {
	f := newFake115(t)
	f.handle(PathRefreshToken, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"state": 1, "code": 0,
			"data": map[string]any{
				"access_token":  "new-access",
				"refresh_token": "new-refresh",
				"expires_in":    3600,
			},
		})
	})
	provider := &refresherToken{token: "old-access", refreshToken: "old-refresh"}
	c := newTestClient(f, provider)

	pair, err := c.RefreshFromProvider(context.Background())
	if err != nil {
		t.Fatalf("从提供器刷新失败: %v", err)
	}
	if pair.AccessToken != "new-access" {
		t.Errorf("access_token = %q，期望 new-access", pair.AccessToken)
	}
	// 新令牌必须回写给提供器，否则等于没刷新。
	if len(provider.saved) != 1 {
		t.Fatalf("回写次数 = %d，期望 1 次", len(provider.saved))
	}
	if provider.saved[0] != "new-access|new-refresh" {
		t.Errorf("回写内容 = %q，期望 %q", provider.saved[0], "new-access|new-refresh")
	}
}

// TestRefreshFromProviderUnsupported 提供器不支持刷新时返回 ErrNoToken。
func TestRefreshFromProviderUnsupported(t *testing.T) {
	f := newFake115(t)
	c := newTestClient(f, staticToken{token: "tok"})

	if _, err := c.RefreshFromProvider(context.Background()); !errors.Is(err, ErrNoToken) {
		t.Fatalf("错误 = %v，期望 ErrNoToken", err)
	}
}

// TestRefreshFromProviderSaveFailed 回写失败要报错——令牌没落盘等于没刷新成功。
func TestRefreshFromProviderSaveFailed(t *testing.T) {
	f := newFake115(t)
	f.handle(PathRefreshToken, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"state": 1, "code": 0,
			"data": map[string]any{"access_token": "new-access", "expires_in": 3600},
		})
	})
	provider := &refresherToken{token: "old", refreshToken: "old-refresh", saveErr: errors.New("磁盘只读")}
	c := newTestClient(f, provider)

	_, err := c.RefreshFromProvider(context.Background())
	if err == nil {
		t.Fatal("回写失败时应返回错误")
	}
	if !strings.Contains(err.Error(), "磁盘只读") {
		t.Errorf("错误 = %v，期望包含底层原因", err)
	}
}

// TestTokenResponseTopLevelFallback 令牌平铺在顶层（不在 data 里）也要认。
//
// 115 不同接口/版本的信封形状不完全一致，两种都解析，取值时优先用 data。
func TestTokenResponseTopLevelFallback(t *testing.T) {
	f := newFake115(t)
	f.handle(PathDeviceCodeToToken, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"state":         1,
			"code":          0,
			"access_token":  "flat-access-token",
			"refresh_token": "flat-refresh-token",
			"expires_in":    7200,
		})
	})
	c := newTestClient(f, staticToken{token: "tok"})

	got, err := c.ExchangeDeviceCode(context.Background(), &AuthState{DeviceCode: "uid", CodeVerifier: "v"})
	if err != nil {
		t.Fatalf("换令牌失败: %v", err)
	}
	if got.AccessToken != "flat-access-token" {
		t.Errorf("access_token = %q，期望 %q", got.AccessToken, "flat-access-token")
	}
	if got.RefreshToken != "flat-refresh-token" {
		t.Errorf("refresh_token = %q，期望 %q", got.RefreshToken, "flat-refresh-token")
	}
}

// TestTokenListTopLevelFallback 任务列表平铺在顶层时也要认。
func TestTokenListTopLevelFallback(t *testing.T) {
	f := newFake115(t)
	f.handle(PathOfflineGetTaskList, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"state":      true,
			"code":       0,
			"page":       1,
			"page_count": 1,
			"count":      1,
			"tasks":      []map[string]any{{"info_hash": "flat-hash", "status": 2}},
		})
	})
	c := newTestClient(f, staticToken{token: "tok"})

	page, err := c.ListOfflineTasks(context.Background(), 1)
	if err != nil {
		t.Fatalf("拉取任务列表失败: %v", err)
	}
	if len(page.Tasks) != 1 {
		t.Fatalf("任务数 = %d，期望 1", len(page.Tasks))
	}
	if page.Tasks[0].OfflineID != "flat-hash" {
		t.Errorf("offline_id = %q，期望 %q", page.Tasks[0].OfflineID, "flat-hash")
	}
}

// TestFlexArrayObjectFallback data 返回对象而非数组时按单元素数组处理。
func TestFlexArrayObjectFallback(t *testing.T) {
	var arr flexArray[offlineAddTaskItem]
	if err := json.Unmarshal([]byte(`{"info_hash":"h1","url":"magnet:?x"}`), &arr); err != nil {
		t.Fatalf("解析对象形式的 data 失败: %v", err)
	}
	if len(arr) != 1 {
		t.Fatalf("元素数 = %d，期望 1", len(arr))
	}
	if arr[0].InfoHash != "h1" {
		t.Errorf("info_hash = %q，期望 h1", arr[0].InfoHash)
	}
	// 空 data 不该报错。
	if err := json.Unmarshal([]byte(`null`), &arr); err != nil {
		t.Fatalf("解析 null 失败: %v", err)
	}
	if arr != nil {
		t.Errorf("null 应解析成 nil，实际 %v", arr)
	}
}

// TestMaskSecret 短串整体打码，长串只露首尾。
func TestMaskSecret(t *testing.T) {
	if got := maskSecret(""); got != "" {
		t.Errorf("空串 = %q，期望空", got)
	}
	if got := maskSecret("short"); got != "****" {
		t.Errorf("短串 = %q，期望 ****", got)
	}
	got := maskSecret("0123456789abcdef")
	if strings.Contains(got, "3456789") {
		t.Errorf("打码结果 = %q，中间部分应被抹掉", got)
	}
	if !strings.HasPrefix(got, "01") {
		t.Errorf("打码结果 = %q，应保留首位", got)
	}
}

// TestAuthStateStringRedacts 打印 AuthState 不能带出令牌。
func TestAuthStateStringRedacts(t *testing.T) {
	const secret = "super-secret-access-token"
	state := &AuthState{
		Status:      AuthStatusAuthorized,
		DeviceCode:  "uid-1",
		AccessToken: secret,
	}
	got := state.String()
	if strings.Contains(got, secret) {
		t.Fatalf("AuthState 的字符串形式泄漏了令牌: %q", got)
	}
	if !strings.Contains(got, "uid-1") {
		t.Errorf("字符串形式应保留设备码: %q", got)
	}
}

// TestTokenPairStringRedacts 打印 TokenPair 不能带出令牌。
func TestTokenPairStringRedacts(t *testing.T) {
	const secret = "super-secret-refresh-token"
	pair := &TokenPair{AccessToken: secret, RefreshToken: secret}
	got := pair.String()
	if strings.Contains(got, secret) {
		t.Fatalf("TokenPair 的字符串形式泄漏了令牌: %q", got)
	}
}

// TestEncodeForm 表单编码只输出非零字段，且字段名来自 form tag。
func TestEncodeForm(t *testing.T) {
	values := encodeForm(addOfflineTaskRequest{
		URLs:     "magnet:?xt=urn:btih:abc",
		WPPath:   "movies",
		WPPathID: "0",
	})
	if got := values.Get("urls"); got != "magnet:?xt=urn:btih:abc" {
		t.Errorf("urls = %q", got)
	}
	if got := values.Get("wp_path"); got != "movies" {
		t.Errorf("wp_path = %q", got)
	}
	// 字符串 "0" 是根目录的合法取值，必须原样送出。
	if got := values.Get("wp_path_id"); got != "0" {
		t.Errorf("wp_path_id = %q，期望 0", got)
	}
	// 未设置的字符串字段不输出。
	if _, ok := values["cid"]; ok {
		t.Error("空字符串字段不应输出")
	}
	// 值为 0 的整数字段不输出：115 把 del_source_file 当可选参数，缺省即 0。
	del := encodeForm(delOfflineTaskRequest{InfoHash: "hash-abc"})
	if _, ok := del["del_source_file"]; ok {
		t.Error("值为 0 的整数字段不应输出")
	}
	if got := del.Get("info_hash"); got != "hash-abc" {
		t.Errorf("info_hash = %q，期望 hash-abc", got)
	}
	// 需要删除源文件时该字段要送出去。
	del = encodeForm(delOfflineTaskRequest{InfoHash: "hash-abc", DelSourceFile: 1})
	if got := del.Get("del_source_file"); got != "1" {
		t.Errorf("del_source_file = %q，期望 1", got)
	}
}

// TestFlexStateParsing state 字段兼容数字与布尔两种写法。
func TestFlexStateParsing(t *testing.T) {
	cases := []struct {
		raw     string
		val     bool
		present bool
	}{
		{`true`, true, true},
		{`false`, false, true},
		{`1`, true, true},
		{`0`, false, true},
		{`"1"`, true, true},
	}
	for _, tc := range cases {
		var s flexState
		if err := json.Unmarshal([]byte(tc.raw), &s); err != nil {
			t.Fatalf("解析 %s 失败: %v", tc.raw, err)
		}
		if s.Bool() != tc.val || s.Present() != tc.present {
			t.Errorf("解析 %s = (%v, %v)，期望 (%v, %v)", tc.raw, s.Bool(), s.Present(), tc.val, tc.present)
		}
	}
}

// TestCodeChallenge PKCE 校验值必须是 url_safe 的 sha256 摘要。
func TestCodeChallenge(t *testing.T) {
	verifier, err := generateCodeVerifier()
	if err != nil {
		t.Fatalf("生成校验原值失败: %v", err)
	}
	if len(verifier) < 43 || len(verifier) > 128 {
		t.Errorf("校验原值长度 = %d，超出 43~128 的要求", len(verifier))
	}
	challenge := codeChallenge(verifier)
	if strings.ContainsAny(challenge, "+/=") {
		t.Errorf("校验值含非 url_safe 字符: %q", challenge)
	}
	if challenge != codeChallenge(verifier) {
		t.Error("同一原值应算出同一校验值")
	}
}

// TestTruncateUTF8 截断不切开多字节字符。
func TestTruncateUTF8(t *testing.T) {
	got := truncateUTF8("中文内容很长很长", 8)
	if len(got) > 8 {
		t.Errorf("截断后长度 = %d，超过上限", len(got))
	}
	// 截断点在多字节字符中间时要回退，不能出现半个汉字。
	if !strings.HasPrefix(got, "中") {
		t.Errorf("截断结果 = %q，期望保留开头", got)
	}
	if strings.Contains(got, "\uFFFD") {
		t.Errorf("截断结果 = %q，含非法字符", got)
	}
	// 未超长时原样返回。
	if got := truncateUTF8("短", 512); got != "短" {
		t.Errorf("未超长时 = %q，期望 %q", got, "短")
	}
}
