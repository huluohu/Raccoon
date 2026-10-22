package qb

// client_test.go：qb 包的单测。
//
// 用 httptest 起一个假 qB 服务（不 mock http.Client，真实走 HTTP），覆盖：
// 登录成功/失败(403)、SID 复用（第二次请求带 Cookie）、添加 magnet、
// multipart 上传种子文件、查询状态、删除、分类、版本、magnet info_hash 解析
// （base32 与 hex 两种）、进度换算、状态归一、会话失效自动重登。
//
// 假服务返回的 JSON 形状按 qB 官方 WebUI API v2 文档编写（未在真实实例
// 实测），所以这些用例同时起到"把协议契约钉死"的作用：改 api.go 的字段名
// 会直接让用例失败。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeQB 假 qB 服务。
//
// 按路径分发到不同处理器，测试用例按需替换 handler；所有收到的请求都会
// 被记录下来，供断言使用。
type fakeQB struct {
	t      *testing.T
	server *httptest.Server

	handlers map[string]http.HandlerFunc
	// requests 记录收到的请求，供断言用。
	requests []recordedRequest
	// loginCount 统计登录次数（验证会话失效后是否重登）。
	loginCount int
}

// recordedRequest 一次请求的记录。
type recordedRequest struct {
	Method string
	Path   string
	Query  url.Values
	// Form 是解析后的表单字段（multipart 与 urlencoded 统一到这里）。
	Form url.Values
	// Cookie 请求头里的 Cookie 原文。
	Cookie string
	// Files 是 multipart 里的文件段（字段名 → 文件名/内容）。
	Files map[string]uploadedFile
}

// uploadedFile 一个上传的文件段。
type uploadedFile struct {
	Field    string
	Filename string
	Data     []byte
}

// newFakeQB 起一个假 qB 服务。
func newFakeQB(t *testing.T) *fakeQB {
	t.Helper()
	f := &fakeQB{t: t, handlers: map[string]http.HandlerFunc{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	f.installDefaults()
	return f
}

// installDefaults 装上默认行为：登录成功发 SID，其余接口校验 Cookie。
func (f *fakeQB) installDefaults() {
	f.handle(PathAuthLogin, func(w http.ResponseWriter, r *http.Request) {
		f.loginCount++
		user := r.FormValue("username")
		pass := r.FormValue("password")
		if user != "admin" || pass != "s3cret" {
			// qB 对凭据错误统一回 403，响应体固定 "Fails."。
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("Fails."))
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: testSID, Path: "/"})
		_, _ = w.Write([]byte("Ok."))
	})
	f.handle(PathAppVersion, func(w http.ResponseWriter, r *http.Request) {
		if !f.checkAuth(w, r) {
			return
		}
		_, _ = w.Write([]byte("v4.6.7\n"))
	})
}

// testSID 假服务下发的会话 ID。
const testSID = "test-sid-abcdef"

// checkAuth 校验请求带上了会话 Cookie；没带就回 403。
func (f *fakeQB) checkAuth(w http.ResponseWriter, r *http.Request) bool {
	if !strings.Contains(r.Header.Get("Cookie"), "SID="+testSID) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("Forbidden."))
		return false
	}
	return true
}

// serve 分发请求。
func (f *fakeQB) serve(w http.ResponseWriter, r *http.Request) {
	rec := recordedRequest{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.Query(),
		Cookie: r.Header.Get("Cookie"),
		Form:   url.Values{},
		Files:  map[string]uploadedFile{},
	}
	// multipart 要单独解析：普通 ParseForm 不会把文件段取出来。
	if ct := r.Header.Get("Content-Type"); ct != "" {
		if mt, _, err := mime.ParseMediaType(ct); err == nil && strings.HasPrefix(mt, "multipart/") {
			if err := r.ParseMultipartForm(1 << 20); err == nil && r.MultipartForm != nil {
				for k, v := range r.MultipartForm.Value {
					rec.Form[k] = append(rec.Form[k], v...)
				}
				for k, files := range r.MultipartForm.File {
					if len(files) == 0 {
						continue
					}
					fh := files[0]
					fp, err := fh.Open()
					if err != nil {
						continue
					}
					data, _ := io.ReadAll(fp)
					_ = fp.Close()
					rec.Files[k] = uploadedFile{Field: k, Filename: fh.Filename, Data: data}
				}
			}
		}
	}
	if len(rec.Form) == 0 {
		_ = r.ParseForm()
		for k, v := range r.PostForm {
			rec.Form[k] = append(rec.Form[k], v...)
		}
	}
	f.requests = append(f.requests, rec)

	if h, ok := f.handlers[r.URL.Path]; ok {
		h(w, r)
		return
	}
	http.NotFound(w, r)
}

// handle 注册某个路径的处理器。
func (f *fakeQB) handle(path string, h http.HandlerFunc) {
	f.handlers[path] = h
}

// url 返回假服务地址。
func (f *fakeQB) url() string { return f.server.URL }

// lastRequest 返回最后一次请求记录。
func (f *fakeQB) lastRequest() recordedRequest {
	f.t.Helper()
	if len(f.requests) == 0 {
		f.t.Fatal("没有任何请求发出")
	}
	return f.requests[len(f.requests)-1]
}

// requestCount 返回指定路径的请求次数。
func (f *fakeQB) requestCount(path string) int {
	n := 0
	for _, r := range f.requests {
		if r.Path == path {
			n++
		}
	}
	return n
}

// writeJSON 写 JSON 响应。
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// newTestClient 用假服务造一个客户端。
func newTestClient(f *fakeQB) *Client {
	return New(Config{
		BaseURL:    f.url(),
		Username:   "admin",
		Password:   "s3cret",
		HTTPClient: f.server.Client(),
		Timeout:    5 * time.Second,
		UserAgent:  "raccoon-qb-test",
	})
}

// ---- 登录 ----

// TestLoginSuccess 登录成功要拿到并缓存 SID。
func TestLoginSuccess(t *testing.T) {
	f := newFakeQB(t)
	c := newTestClient(f)
	if err := c.Login(context.Background()); err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	if got := c.sessionID(); got != testSID {
		t.Fatalf("会话 ID = %q, 期望 %q", got, testSID)
	}
	if f.loginCount != 1 {
		t.Fatalf("登录请求次数 = %d, 期望 1", f.loginCount)
	}
}

// TestLoginForbidden 凭据错误时 qB 回 403，要翻译成 ErrAuthFailed。
func TestLoginForbidden(t *testing.T) {
	f := newFakeQB(t)
	c := New(Config{
		BaseURL:    f.url(),
		Username:   "admin",
		Password:   "wrong",
		HTTPClient: f.server.Client(),
		Timeout:    5 * time.Second,
	})
	err := c.Login(context.Background())
	if err == nil {
		t.Fatal("期望登录失败, 实际成功")
	}
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("错误 = %v, 期望 ErrAuthFailed", err)
	}
}

// TestSIDReusedOnSecondRequest 登录后的后续请求必须带上 SID Cookie。
func TestSIDReusedOnSecondRequest(t *testing.T) {
	f := newFakeQB(t)
	c := newTestClient(f)
	ctx := context.Background()
	if err := c.Login(ctx); err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	f.handle(PathAppVersion, func(w http.ResponseWriter, r *http.Request) {
		if !f.checkAuth(w, r) {
			return
		}
		_, _ = w.Write([]byte("v4.6.7"))
	})
	if _, err := c.Version(ctx); err != nil {
		t.Fatalf("取版本失败: %v", err)
	}
	// 第二次请求（版本）必须复用登录拿到的 SID，而不是重新登录。
	if f.loginCount != 1 {
		t.Fatalf("登录请求次数 = %d, 期望 1（SID 应被复用）", f.loginCount)
	}
	rec := f.lastRequest()
	if !strings.Contains(rec.Cookie, "SID="+testSID) {
		t.Fatalf("请求 Cookie = %q, 期望包含 SID=%s", rec.Cookie, testSID)
	}
}

// TestLoginDoesNotSendStaleCookie 重新登录时不能把旧 SID 带过去：
// 拿一个可能已失效的会话去登录只会让人误以为会话还有效。
func TestLoginDoesNotSendStaleCookie(t *testing.T) {
	f := newFakeQB(t)
	c := newTestClient(f)
	ctx := context.Background()
	if err := c.Login(ctx); err != nil {
		t.Fatalf("首次登录失败: %v", err)
	}
	if err := c.Login(ctx); err != nil {
		t.Fatalf("二次登录失败: %v", err)
	}
	// 两次登录请求都不该带 Cookie。
	for i, rec := range f.requests {
		if rec.Path != PathAuthLogin {
			continue
		}
		if rec.Cookie != "" {
			t.Fatalf("第 %d 次登录请求带了 Cookie = %q, 期望为空", i+1, rec.Cookie)
		}
	}
}

// ---- 添加种子 ----

// TestAddMagnet 提交 magnet 要带对字段，并返回 info_hash。
func TestAddMagnet(t *testing.T) {
	f := newFakeQB(t)
	var gotForm url.Values
	f.handle(PathTorrentsAdd, func(w http.ResponseWriter, r *http.Request) {
		if !f.checkAuth(w, r) {
			return
		}
		_ = r.ParseMultipartForm(1 << 20)
		gotForm = url.Values{}
		if r.MultipartForm != nil {
			for k, v := range r.MultipartForm.Value {
				gotForm[k] = append(gotForm[k], v...)
			}
		}
		_, _ = w.Write([]byte("Ok."))
	})
	c := newTestClient(f)
	// base32 形式的 magnet：期望解析出 40 位小写 hex。
	magnet := "magnet:?xt=urn:btih:VL2MMHO4YXUKFWV63YHTWSBM3GXKSQ2N&dn=hello"
	wantHash := "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d"
	hash, err := c.AddMagnet(context.Background(), magnet, AddOptions{
		SavePath:     "/downloads",
		Category:     "movie",
		Tags:         []string{"tag1", "tag2"},
		Paused:       true,
		SkipChecking: true,
	})
	if err != nil {
		t.Fatalf("添加 magnet 失败: %v", err)
	}
	if hash != wantHash {
		t.Fatalf("info_hash = %q, 期望 %q", hash, wantHash)
	}
	if got := gotForm.Get("urls"); got != magnet {
		t.Fatalf("urls 字段 = %q, 期望 %q", got, magnet)
	}
	if got := gotForm.Get("savepath"); got != "/downloads" {
		t.Fatalf("savepath 字段 = %q, 期望 /downloads", got)
	}
	if got := gotForm.Get("category"); got != "movie" {
		t.Fatalf("category 字段 = %q, 期望 movie", got)
	}
	if got := gotForm.Get("tags"); got != "tag1,tag2" {
		t.Fatalf("tags 字段 = %q, 期望 tag1,tag2", got)
	}
	if got := gotForm.Get("paused"); got != "true" {
		t.Fatalf("paused 字段 = %q, 期望 true", got)
	}
	if got := gotForm.Get("skip_checking"); got != "true" {
		t.Fatalf("skip_checking 字段 = %q, 期望 true", got)
	}
}

// TestAddTorrentFileMultipart 上传种子文件要走 multipart，字段名必须是 torrents。
func TestAddTorrentFileMultipart(t *testing.T) {
	f := newFakeQB(t)
	f.handle(PathTorrentsAdd, func(w http.ResponseWriter, r *http.Request) {
		if !f.checkAuth(w, r) {
			return
		}
		_, _ = w.Write([]byte("Ok."))
	})
	c := newTestClient(f)
	content := []byte("d8:announce3:abce")
	if err := c.AddTorrentFile(context.Background(), "x.torrent", content, AddOptions{
		SavePath: "/downloads",
	}); err != nil {
		t.Fatalf("上传种子失败: %v", err)
	}
	rec := f.lastRequest()
	up, ok := rec.Files[addFileField]
	if !ok {
		t.Fatalf("未收到名为 %s 的文件段, 实际收到 %v", addFileField, rec.Files)
	}
	if up.Filename != "x.torrent" {
		t.Fatalf("文件名 = %q, 期望 x.torrent", up.Filename)
	}
	if string(up.Data) != string(content) {
		t.Fatalf("文件内容 = %q, 期望 %q", up.Data, content)
	}
	if got := rec.Form.Get("savepath"); got != "/downloads" {
		t.Fatalf("savepath 字段 = %q, 期望 /downloads", got)
	}
}

// TestAddTorrentBase64 base64 内容要能解码后走同一条 multipart 通道。
func TestAddTorrentBase64(t *testing.T) {
	f := newFakeQB(t)
	f.handle(PathTorrentsAdd, func(w http.ResponseWriter, r *http.Request) {
		if !f.checkAuth(w, r) {
			return
		}
		_, _ = w.Write([]byte("Ok."))
	})
	c := newTestClient(f)
	raw := []byte("d8:announce3:abc")
	encoded := base64.StdEncoding.EncodeToString(raw)
	if err := c.AddTorrentBase64(context.Background(), "b.torrent", encoded, AddOptions{}); err != nil {
		t.Fatalf("上传 base64 种子失败: %v", err)
	}
	rec := f.lastRequest()
	up, ok := rec.Files[addFileField]
	if !ok {
		t.Fatalf("未收到名为 %s 的文件段", addFileField)
	}
	if string(up.Data) != string(raw) {
		t.Fatalf("文件内容 = %q, 期望 %q", up.Data, raw)
	}
}

// ---- 查询与删除 ----

// TestTorrents 查询状态要归一状态并换算进度。
func TestTorrents(t *testing.T) {
	f := newFakeQB(t)
	f.handle(PathTorrentsInfo, func(w http.ResponseWriter, r *http.Request) {
		if !f.checkAuth(w, r) {
			return
		}
		writeJSON(w, []torrentInfoWire{
			{
				Hash:      "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d",
				State:     "stalledDL",
				Progress:  0.42,
				Size:      1024,
				TotalSize: 2048,
				SavePath:  "/downloads/movie",
				Name:      "hello",
				AddedOn:   1700000000,
				Category:  "movie",
				Tags:      "tag1,tag2",
				DLSpeed:   1024,
				ETA:       60,
				NumSeeds:  3,
			},
		})
	})
	c := newTestClient(f)
	list, err := c.Torrents(context.Background(), nil)
	if err != nil {
		t.Fatalf("查询种子失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("种子数 = %d, 期望 1", len(list))
	}
	got := list[0]
	if got.Hash != "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d" {
		t.Fatalf("hash = %q", got.Hash)
	}
	if got.State != StateDownloading {
		t.Fatalf("state = %q, 期望 %q", got.State, StateDownloading)
	}
	if got.RawState != "stalledDL" {
		t.Fatalf("rawState = %q, 期望 stalledDL", got.RawState)
	}
	if got.Percent != 42 {
		t.Fatalf("percent = %v, 期望 42", got.Percent)
	}
	if got.Progress != 0.42 {
		t.Fatalf("progress = %v, 期望 0.42", got.Progress)
	}
	if got.Size != 1024 || got.TotalSize != 2048 {
		t.Fatalf("size/totalSize = %d/%d, 期望 1024/2048", got.Size, got.TotalSize)
	}
	if got.SavePath != "/downloads/movie" {
		t.Fatalf("savePath = %q", got.SavePath)
	}
	if got.Name != "hello" {
		t.Fatalf("name = %q", got.Name)
	}
	if got.AddedOn != 1700000000 {
		t.Fatalf("addedOn = %d, 期望 1700000000", got.AddedOn)
	}
	if got.Category != "movie" || got.Tags != "tag1,tag2" {
		t.Fatalf("category/tags = %q/%q", got.Category, got.Tags)
	}
	if got.DownloadSpeed != 1024 || got.ETA != 60 || got.NumSeeds != 3 {
		t.Fatalf("速度/ETA/做种数 = %d/%d/%d", got.DownloadSpeed, got.ETA, got.NumSeeds)
	}
}

// TestTorrentsFilterByHashes 按哈希过滤要落到 query 的 hashes 参数上。
func TestTorrentsFilterByHashes(t *testing.T) {
	f := newFakeQB(t)
	f.handle(PathTorrentsInfo, func(w http.ResponseWriter, r *http.Request) {
		if !f.checkAuth(w, r) {
			return
		}
		writeJSON(w, []torrentInfoWire{})
	})
	c := newTestClient(f)
	// 上层常把哈希存成一整串逗号分隔的文本，这里两种形态都要能传。
	if _, err := c.Torrents(context.Background(), []string{"aaa,bbb"}); err != nil {
		t.Fatalf("查询种子失败: %v", err)
	}
	rec := f.lastRequest()
	if got := rec.Query.Get("hashes"); got != "aaa"+HashSeparator+"bbb" {
		t.Fatalf("hashes 参数 = %q, 期望 aaa%qbbb", got, HashSeparator)
	}
}

// TestDelete 删除要带 hashes 与 deleteFiles。
func TestDelete(t *testing.T) {
	f := newFakeQB(t)
	f.handle(PathTorrentsDelete, func(w http.ResponseWriter, r *http.Request) {
		if !f.checkAuth(w, r) {
			return
		}
		_, _ = w.Write([]byte("Ok."))
	})
	c := newTestClient(f)
	if err := c.Delete(context.Background(), []string{"aaa", "bbb"}, true); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	rec := f.lastRequest()
	if got := rec.Form.Get("hashes"); got != "aaa"+HashSeparator+"bbb" {
		t.Fatalf("hashes 字段 = %q", got)
	}
	if got := rec.Form.Get("deleteFiles"); got != "true" {
		t.Fatalf("deleteFiles 字段 = %q, 期望 true", got)
	}
	// 没给哈希时不该发请求（避免误删"全部"）。
	before := f.requestCount(PathTorrentsDelete)
	if err := c.Delete(context.Background(), nil, true); err == nil {
		t.Fatal("空哈希删除期望报错")
	}
	if after := f.requestCount(PathTorrentsDelete); after != before {
		t.Fatalf("空哈希删除发出了请求: %d -> %d", before, after)
	}
}

// ---- 分类与版本 ----

// TestCategories 分类列表要按名称排序返回。
func TestCategories(t *testing.T) {
	f := newFakeQB(t)
	f.handle(PathTorrentsCategories, func(w http.ResponseWriter, r *http.Request) {
		if !f.checkAuth(w, r) {
			return
		}
		writeJSON(w, map[string]categoryWire{
			"movie": {Name: "movie", SavePath: "/dl/movie"},
			"book":  {Name: "book", SavePath: "/dl/book"},
		})
	})
	c := newTestClient(f)
	list, err := c.Categories(context.Background())
	if err != nil {
		t.Fatalf("取分类失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("分类数 = %d, 期望 2", len(list))
	}
	if list[0].Name != "book" || list[1].Name != "movie" {
		t.Fatalf("分类顺序 = %s,%s, 期望 book,movie", list[0].Name, list[1].Name)
	}
	if list[1].SavePath != "/dl/movie" {
		t.Fatalf("movie 的 savePath = %q", list[1].SavePath)
	}
}

// TestVersion 版本接口返回纯文本。
func TestVersion(t *testing.T) {
	f := newFakeQB(t)
	c := newTestClient(f)
	v, err := c.Version(context.Background())
	if err != nil {
		t.Fatalf("取版本失败: %v", err)
	}
	if v != "v4.6.7" {
		t.Fatalf("版本 = %q, 期望 v4.6.7", v)
	}
}

// ---- 可用性探测 ----

// TestIsAvailable 可用 = 能登录且业务接口通。
func TestIsAvailable(t *testing.T) {
	f := newFakeQB(t)
	c := newTestClient(f)
	if err := c.IsAvailable(context.Background()); err != nil {
		t.Fatalf("期望可用, 实际不可用: %v", err)
	}
	if f.loginCount != 1 {
		t.Fatalf("登录次数 = %d, 期望 1", f.loginCount)
	}
}

// TestIsAvailableBadCredentials 凭据错时不可用。
func TestIsAvailableBadCredentials(t *testing.T) {
	f := newFakeQB(t)
	c := New(Config{
		BaseURL:    f.url(),
		Username:   "admin",
		Password:   "nope",
		HTTPClient: f.server.Client(),
		Timeout:    5 * time.Second,
	})
	err := c.IsAvailable(context.Background())
	if err == nil {
		t.Fatal("期望不可用, 实际可用")
	}
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("错误 = %v, 期望 ErrAuthFailed", err)
	}
}

// TestSessionExpiryReLogin 会话失效（403）时要自动重登并重放请求。
func TestSessionExpiryReLogin(t *testing.T) {
	f := newFakeQB(t)
	infoCalls := 0
	f.handle(PathTorrentsInfo, func(w http.ResponseWriter, r *http.Request) {
		infoCalls++
		// 第一次调用假装会话已失效，重登后第二次才放行。
		if infoCalls == 1 {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("Forbidden."))
			return
		}
		writeJSON(w, []torrentInfoWire{{Hash: "abc", State: "downloading"}})
	})
	c := newTestClient(f)
	list, err := c.Torrents(context.Background(), nil)
	if err != nil {
		t.Fatalf("查询种子失败: %v", err)
	}
	if len(list) != 1 || list[0].Hash != "abc" {
		t.Fatalf("重登后结果 = %+v", list)
	}
	if f.loginCount != 2 {
		t.Fatalf("登录次数 = %d, 期望 2（失效后应重登）", f.loginCount)
	}
}

// ---- magnet 解析 ----

// TestParseInfoHash magnet 里的 info_hash 有 base32 与 hex 两种写法。
func TestParseInfoHash(t *testing.T) {
	cases := []struct {
		name   string
		magnet string
		want   string
	}{
		{
			name:   "base32",
			magnet: "magnet:?xt=urn:btih:VL2MMHO4YXUKFWV63YHTWSBM3GXKSQ2N&dn=hello",
			want:   "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d",
		},
		{
			name:   "hex",
			magnet: "magnet:?xt=urn:btih:aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d&dn=hello",
			want:   "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d",
		},
		{
			name:   "大写 hex 要归一成小写",
			magnet: "magnet:?xt=urn:btih:AAF4C61DDCC5E8A2DABEDE0F3B482CD9AEA9434D",
			want:   "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d",
		},
		{
			name:   "小写 base32 也要能解",
			magnet: "magnet:?xt=urn:btih:vl2mmho4yxukfwv63yhtwsbm3gxksq2n",
			want:   "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseInfoHash(tc.magnet)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if got != tc.want {
				t.Fatalf("info_hash = %q, 期望 %q", got, tc.want)
			}
		})
	}
}

// TestParseInfoHashInvalid 非法链接要报 ErrInvalidMagnet。
func TestParseInfoHashInvalid(t *testing.T) {
	cases := []string{
		"",
		"http://example.com/file.torrent",
		"magnet:?dn=no-hash",
		"magnet:?xt=urn:btih:tooshort",
		// base32 字母表是 A-Z2-7，"1" 不在其中。
		"magnet:?xt=urn:btih:11111111111111111111111111111111",
		// 40 位 hex 里的 "z" 不是十六进制字符。
		"magnet:?xt=urn:btih:zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz",
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%q", c), func(t *testing.T) {
			if _, err := ParseInfoHash(c); err == nil {
				t.Fatalf("期望报错, 实际成功")
			} else if !errors.Is(err, ErrInvalidMagnet) {
				t.Fatalf("错误 = %v, 期望 ErrInvalidMagnet", err)
			}
		})
	}
}

// ---- 归一化 ----

// TestProgressPercent 进度换算。
func TestProgressPercent(t *testing.T) {
	cases := []struct {
		raw  float64
		want float64
	}{
		{raw: 0, want: 0},
		{raw: 0.42, want: 42},
		{raw: 1, want: 100},
		{raw: 0.001, want: 0.1},
		{raw: -1, want: 0},
		{raw: 2, want: 100},
	}
	for _, tc := range cases {
		if got := ProgressPercent(tc.raw); got != tc.want {
			t.Fatalf("ProgressPercent(%v) = %v, 期望 %v", tc.raw, got, tc.want)
		}
	}
}

// TestNormalizeState 状态归一。
func TestNormalizeState(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{raw: "downloading", want: StateDownloading},
		{raw: "stalledDL", want: StateDownloading},
		{raw: "metaDL", want: StateDownloading},
		{raw: "forcedDL", want: StateDownloading},
		{raw: "allocating", want: StateDownloading},
		{raw: "uploading", want: StateUploading},
		{raw: "forcedUP", want: StateUploading},
		{raw: "stalledUP", want: StateSeeding},
		{raw: "pausedUP", want: StatePaused},
		{raw: "pausedDL", want: StatePaused},
		{raw: "stoppedUP", want: StateStopped},
		{raw: "queuedDL", want: StateQueued},
		{raw: "queuedUP", want: StateQueued},
		{raw: "checkingDL", want: StateChecking},
		{raw: "checkingUP", want: StateChecking},
		{raw: "error", want: StateError},
		{raw: "missingFiles", want: StateError},
		{raw: "不存在的状态", want: StateError},
	}
	for _, tc := range cases {
		if got := NormalizeState(tc.raw); got != tc.want {
			t.Fatalf("NormalizeState(%q) = %q, 期望 %q", tc.raw, got, tc.want)
		}
	}
}

// TestNormalizeTorrentState 下载完的空闲做种要判成已完成。
func TestNormalizeTorrentState(t *testing.T) {
	if got := NormalizeTorrentState("stalledUP", 1); got != StateCompleted {
		t.Fatalf("NormalizeTorrentState(stalledUP, 1) = %q, 期望 %q", got, StateCompleted)
	}
	if got := NormalizeTorrentState("stalledUP", 0.5); got != StateSeeding {
		t.Fatalf("NormalizeTorrentState(stalledUP, 0.5) = %q, 期望 %q", got, StateSeeding)
	}
	// 正在上传时保留"上传中"，比"已完成"更有信息量。
	if got := NormalizeTorrentState("uploading", 1); got != StateUploading {
		t.Fatalf("NormalizeTorrentState(uploading, 1) = %q, 期望 %q", got, StateUploading)
	}
}

// ---- 其它 ----

// TestMultipartBoundary multipart 段名固定为 torrents，Content-Type 带 boundary。
func TestMultipartBoundary(t *testing.T) {
	body, ct, err := buildMultipart(map[string]string{"urls": "magnet:?xt=urn:btih:abc"}, "", nil)
	if err != nil {
		t.Fatalf("构造 multipart 失败: %v", err)
	}
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil {
		t.Fatalf("解析 Content-Type 失败: %v", err)
	}
	if mediaType != "multipart/form-data" {
		t.Fatalf("Content-Type = %q", mediaType)
	}
	if params["boundary"] == "" {
		t.Fatalf("Content-Type 缺少 boundary: %q", ct)
	}
	if !strings.Contains(string(body), "name=\"urls\"") {
		t.Fatalf("multipart 体缺少 urls 字段: %q", body)
	}
}

// TestNewDefaultTimeout 没注入 http.Client 时要自己造一个带超时的。
func TestNewDefaultTimeout(t *testing.T) {
	c := New(Config{BaseURL: "http://127.0.0.1:1"})
	if c.http == nil {
		t.Fatal("http.Client 为空")
	}
	if c.http.Timeout != defaultTimeout {
		t.Fatalf("http.Client.Timeout = %v, 期望 %v", c.http.Timeout, defaultTimeout)
	}
	// 基址要去掉尾部斜杠，否则拼出来的路径会是双斜杠。
	c2 := New(Config{BaseURL: "http://127.0.0.1:8080/"})
	if c2.baseURL != "http://127.0.0.1:8080" {
		t.Fatalf("baseURL = %q", c2.baseURL)
	}
}

// TestDecodeBase64 缺 padding 的 base64 也要能解。
func TestDecodeBase64(t *testing.T) {
	raw := []byte("hello world!")
	got, err := decodeBase64(base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if string(got) != string(raw) {
		t.Fatalf("解码结果 = %q", got)
	}
	if _, err := decodeBase64("  "); err == nil {
		t.Fatal("空内容期望报错")
	}
	if _, err := decodeBase64("!!!not-base64!!!"); err == nil {
		t.Fatal("非法 base64 期望报错")
	}
}

// TestContextCancel 上层取消上下文时要立刻返回，不能挂住。
func TestContextCancel(t *testing.T) {
	f := newFakeQB(t)
	c := newTestClient(f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Torrents(ctx, nil)
	if err == nil {
		t.Fatal("期望错误, 实际成功")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("错误 = %v, 期望 context.Canceled", err)
	}
}

// TestAddTorrentFileEmpty 空文件要被拦下。
func TestAddTorrentFileEmpty(t *testing.T) {
	f := newFakeQB(t)
	c := newTestClient(f)
	if err := c.AddTorrentFile(context.Background(), "x.torrent", nil, AddOptions{}); err == nil {
		t.Fatal("空文件期望报错")
	}
}

// TestAddTorrentFileFromDisk 从磁盘读种子文件再上传（真实使用路径）。
func TestAddTorrentFileFromDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "real.torrent")
	content := []byte("d4:name4:teste")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("写临时文件失败: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读临时文件失败: %v", err)
	}
	f := newFakeQB(t)
	f.handle(PathTorrentsAdd, func(w http.ResponseWriter, r *http.Request) {
		if !f.checkAuth(w, r) {
			return
		}
		_, _ = w.Write([]byte("Ok."))
	})
	c := newTestClient(f)
	if err := c.AddTorrentFile(context.Background(), filepath.Base(path), data, AddOptions{}); err != nil {
		t.Fatalf("上传种子失败: %v", err)
	}
	rec := f.lastRequest()
	up, ok := rec.Files[addFileField]
	if !ok {
		t.Fatalf("未收到文件段")
	}
	if string(up.Data) != string(content) {
		t.Fatalf("文件内容 = %q, 期望 %q", up.Data, content)
	}
}

// 确认 multipart 的解析路径确实被走到（防止测试自身把断言写空了）。
func TestFakeServerParsesMultipart(t *testing.T) {
	f := newFakeQB(t)
	f.handle(PathTorrentsAdd, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Ok."))
	})
	c := newTestClient(f)
	if err := c.AddTorrentFile(context.Background(), "t.torrent", []byte("abc"), AddOptions{}); err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	rec := f.lastRequest()
	if len(rec.Files) != 1 {
		t.Fatalf("假服务解析到的文件段数 = %d, 期望 1", len(rec.Files))
	}
}
