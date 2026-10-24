// file_delete_test.go：媒体库删文件的 HTTP 契约（docs/04 §2.8）。
//
// 为什么要单独测：这是本轮唯一新增的「真删磁盘文件」入口，前端一旦传错参数
// 就会变成「点删除没反应」或者更糟——删掉不该删的东西。三条硬要求：
// 1) 正常路径：文件从磁盘消失、索引消失、返回 200；
// 2) 路径越界：库里的 path 指向数据根之外 → 403，且磁盘文件必须还在；
// 3) 缺 CSRF → 403，文件不能动。
package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/huluohu/raccoon/internal/store"
)

// newTestMediaFile 在数据目录里落一个真文件并登记索引。
func newTestMediaFile(t *testing.T, srv *Server, name string) (*store.MediaFile, string) {
	t.Helper()
	dataDir := srv.cfg.GetString("storage.data_dir")
	abs := filepath.Join(dataDir, "downloads", name)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("建下载目录失败：%v", err)
	}
	if err := os.WriteFile(abs, []byte("test-bytes"), 0o644); err != nil {
		t.Fatalf("写测试文件失败：%v", err)
	}
	m := &store.MediaFile{
		ID:   "mf_" + name,
		Path: abs,
		Name: name,
		Size: 10,
		Kind: "video",
	}
	if err := srv.store.UpsertMediaFile(m); err != nil {
		t.Fatalf("登记媒体文件失败：%v", err)
	}
	return m, abs
}

// Test删除媒体文件_磁盘与索引一起消失 正常路径：200 + 文件没了 + 索引没了。
func Test删除媒体文件_磁盘与索引一起消失(t *testing.T) {
	srv, base, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, base, username, password)
	m, abs := newTestMediaFile(t, srv, "clip.mp4")

	code, body, env := secDoBody(t, http.MethodDelete, base+"/api/v1/files/"+m.ID, "",
		secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusOK {
		t.Fatalf("删文件应返回 200，实际 %d（%s / %s）", code, env.Error.Code, body)
	}
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Fatalf("磁盘文件应已删除，实际 stat err=%v", err)
	}
	if _, err := srv.store.GetMediaFile(m.ID); err == nil {
		t.Fatal("媒体索引应已删除")
	}
}

// Test删除媒体文件_路径越界403 库里的 path 被改成根外路径时，必须拒绝且不删。
func Test删除媒体文件_路径越界403(t *testing.T) {
	srv, base, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, base, username, password)

	outside := filepath.Join(t.TempDir(), "outside.mp4")
	if err := os.WriteFile(outside, []byte("keep-me"), 0o644); err != nil {
		t.Fatalf("写根外文件失败：%v", err)
	}
	m := &store.MediaFile{ID: "mf_outside", Path: outside, Name: "outside.mp4", Size: 7, Kind: "video"}
	if err := srv.store.UpsertMediaFile(m); err != nil {
		t.Fatalf("登记媒体文件失败：%v", err)
	}

	code, _, env := secDoBody(t, http.MethodDelete, base+"/api/v1/files/"+m.ID, "",
		secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusForbidden {
		t.Fatalf("根外路径应返回 403，实际 %d（%s）", code, env.Error.Code)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("越界请求不得删文件，实际 stat err=%v", err)
	}
}

// Test删除媒体文件_缺CSRF403 会话写接口必须带 CSRF，否则文件不能被删。
func Test删除媒体文件_缺CSRF403(t *testing.T) {
	srv, base, username, password := newSecEnv(t)
	cookie, _ := secLoginSession(t, base, username, password)
	m, abs := newTestMediaFile(t, srv, "csrf.mp4")

	if code, _ := secDo(t, http.MethodDelete, base+"/api/v1/files/"+m.ID, "", secOpts{cookie: cookie}); code != http.StatusForbidden {
		t.Fatalf("缺 CSRF 的 DELETE 应返回 403，实际 %d", code)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("被拒绝的请求不得删文件，实际 stat err=%v", err)
	}
}

// Test独立下载根内文件可删除 下载目录脱离数据根（如容器内 /downloads）后，
// 媒体库删除的越界校验必须认「数据根 ∪ 下载根」——只认数据根会把独立
// 下载根里的文件误判 403（2026-10-24 引入独立下载根时回归锁定）。
func Test独立下载根内文件可删除(t *testing.T) {
	srv, base, username, password := newSecEnv(t)
	cookie, csrf := secLoginSession(t, base, username, password)

	// 造一个独立的下载根，登记一个其中的媒体文件。
	dlRoot := t.TempDir()
	if err := srv.cfg.Set("storage.download_dir", dlRoot); err != nil {
		t.Fatalf("设置独立下载根失败：%v", err)
	}
	abs := filepath.Join(dlRoot, "youtube", "clip.mp4")
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &store.MediaFile{ID: "mf_dlroot", Path: abs, Name: "clip.mp4", Size: 1, Kind: "video"}
	if err := srv.store.UpsertMediaFile(m); err != nil {
		t.Fatal(err)
	}

	code, _, env := secDoBody(t, http.MethodDelete, base+"/api/v1/files/"+m.ID, "",
		secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusOK {
		t.Fatalf("独立下载根内文件删除应放行，实际 %d（%v）", code, env.Error.Message)
	}
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Fatal("磁盘文件应已删除")
	}
	// 数据根之外的越界路径仍要拦：把下载根指回数据根后，第三方目录必须 403。
	evl := filepath.Join(t.TempDir(), "evil.mp4")
	_ = os.WriteFile(evl, []byte("x"), 0o644)
	m2 := &store.MediaFile{ID: "mf_evil", Path: evl, Name: "evil.mp4", Size: 1, Kind: "video"}
	_ = srv.store.UpsertMediaFile(m2)
	code, _, _ = secDoBody(t, http.MethodDelete, base+"/api/v1/files/"+m2.ID, "",
		secOpts{cookie: cookie, csrf: csrf})
	if code != http.StatusForbidden {
		t.Fatalf("允许根之外的路径应 403，实际 %d", code)
	}
	if _, err := os.Stat(evl); err != nil {
		t.Fatal("越界文件不应被删除")
	}
}
