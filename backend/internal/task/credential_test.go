package task

// credential_test.go：凭据 → 引擎 cookie 文件桥接（docs/01 §3.1）。
//
// 锁死三件事：
//  1. 产物是 yt-dlp / gallery-dl 唯一认的 Netscape 格式（格式错 = 静默不登录）；
//  2. 权限 0600（cookie 是会话级凭据，落 0644 等于同机任何进程可偷）；
//  3. 无凭据时返回空串而非报错（不阻断公开内容任务）。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteCookieFile_Netscape格式 逐字段断言输出格式。
func TestWriteCookieFile_Netscape格式(t *testing.T) {
	w := cookieFileWriter{dir: t.TempDir()}
	path, err := w.writeCookieFile("bilibili", "SESSDATA=abc; bili_jct=def")
	if err != nil {
		t.Fatalf("写 cookie 文件失败: %v", err)
	}
	if path == "" {
		t.Fatal("应返回文件路径")
	}
	defer w.removeCookieFile(path)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	content := string(raw)

	// 首行必须是 Netscape 魔数，否则 yt-dlp 直接拒收。
	if !strings.HasPrefix(content, "# Netscape HTTP Cookie File\n") {
		t.Errorf("首行不是 Netscape 魔数，实际: %q", strings.SplitN(content, "\n", 2)[0])
	}
	// 域名取平台默认（.bilibili.com），includeSubdomains=TRUE。
	want := ".bilibili.com\tTRUE\t/\tFALSE\t"
	if !strings.Contains(content, want) {
		t.Errorf("缺少 Netscape 记录行 %q，实际内容:\n%s", want, content)
	}
	if !strings.Contains(content, "\tSESSDATA\tabc\n") {
		t.Errorf("缺少 SESSDATA 记录，实际内容:\n%s", content)
	}
	if !strings.Contains(content, "\tbili_jct\tdef\n") {
		t.Errorf("缺少 bili_jct 记录，实际内容:\n%s", content)
	}
}

// TestWriteCookieFile_权限0600 cookie 明文只允许属主读。
func TestWriteCookieFile_权限0600(t *testing.T) {
	w := cookieFileWriter{dir: t.TempDir()}
	path, err := w.writeCookieFile("youtube", "SID=xyz")
	if err != nil {
		t.Fatalf("写失败: %v", err)
	}
	defer w.removeCookieFile(path)

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat 失败: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("权限应为 0600，实际 %o", perm)
	}
}

// TestWriteCookieFile_采用cookie自带domain 用户整条 Cookie 头粘进来时以 domain 属性为准。
func TestWriteCookieFile_采用cookie自带domain(t *testing.T) {
	w := cookieFileWriter{dir: t.TempDir()}
	path, err := w.writeCookieFile("bilibili", "domain=.example.com; SESSDATA=abc")
	if err != nil {
		t.Fatalf("写失败: %v", err)
	}
	defer w.removeCookieFile(path)

	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), ".example.com\tTRUE") {
		t.Errorf("应采用 cookie 自带域名 .example.com，实际:\n%s", raw)
	}
	if strings.Contains(string(raw), "domain\t") {
		t.Error("domain 属性本身不应作为 cookie 记录写出")
	}
}

// TestWriteCookieFile_空凭据返回空串 无凭据不阻断任务。
func TestWriteCookieFile_空凭据返回空串(t *testing.T) {
	w := cookieFileWriter{dir: t.TempDir()}
	for _, in := range []string{"", "   ", ";;;"} {
		path, err := w.writeCookieFile("bilibili", in)
		if err != nil {
			t.Fatalf("空凭据不应报错: %v", err)
		}
		if path != "" {
			t.Errorf("空凭据 %q 应返回空路径，实际 %q", in, path)
		}
	}
}

// TestRemoveCookieFile 清理幂等（不存在不报错，空路径安全）。
func TestRemoveCookieFile(t *testing.T) {
	w := cookieFileWriter{dir: t.TempDir()}
	path, err := w.writeCookieFile("bilibili", "SESSDATA=abc")
	if err != nil || path == "" {
		t.Fatalf("前置写入失败: %v", err)
	}
	w.removeCookieFile(path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("removeCookieFile 后文件应不存在")
	}
	w.removeCookieFile(path)       // 重复删除不 panic
	w.removeCookieFile("")         // 空路径不 panic
	w.removeCookieFile("/no/such") // 不存在路径不 panic
}

// TestCookieDomainFor 平台默认域名表。
func TestCookieDomainFor(t *testing.T) {
	cases := map[string]string{
		"bilibili":    ".bilibili.com",
		"xiaohongshu": ".xiaohongshu.com",
		"netease":     ".music.163.com",
		"youtube":     ".youtube.com",
		"unknown":     ".com", // 兜底：宁可他站不匹配，也不写死单一站点
		"":            ".com",
	}
	for platform, want := range cases {
		if got := cookieDomainFor(platform); got != want {
			t.Errorf("cookieDomainFor(%q) = %q，期望 %q", platform, got, want)
		}
	}
}

// TestCookieResolver_空实现不阻断 未注入 store/vault 时取不到凭据但不报错。
func TestCookieResolver_空实现不阻断(t *testing.T) {
	c := NewCookieResolver(nil, nil)
	got, err := c.PlatformCredential(context.Background(), "bilibili")
	if err != nil {
		t.Errorf("未装配时应返回空凭据而非错误: %v", err)
	}
	if got != "" {
		t.Errorf("应返回空串，实际 %q", got)
	}
	// 平台名为空同样安全（内部接口传空不炸）。
	got, err = c.PlatformCredential(context.Background(), "")
	if err != nil || got != "" {
		t.Errorf("空平台名应安全返回空: got=%q err=%v", got, err)
	}
}

// fakeResolver 验证 CredentialResolver 接口可被上层注入假实现（单测隔离）。
type fakeResolver struct {
	val string
	err error
}

func (f fakeResolver) PlatformCredential(ctx context.Context, platform string) (string, error) {
	if platform == "" {
		return "", nil
	}
	return f.val, f.err
}

// TestCredentialResolver_接口可注入 上层可换实现（如未来的系统 keychain）。
func TestCredentialResolver_接口可注入(t *testing.T) {
	var r CredentialResolver = fakeResolver{val: "SESSDATA=abc"}
	got, err := r.PlatformCredential(context.Background(), "bilibili")
	if err != nil || got != "SESSDATA=abc" {
		t.Errorf("假实现应原样返回: got=%q err=%v", got, err)
	}
	// 空 cookie 字符串时上层应走到 writeCookieFile 的空值分支（不产文件）。
	w := cookieFileWriter{dir: t.TempDir()}
	path, err := w.writeCookieFile("bilibili", got)
	if err != nil || path == "" {
		t.Fatalf("有效凭据应产出文件: path=%q err=%v", path, err)
	}
	w.removeCookieFile(path)
	_ = filepath.Join // 保持 import 稳定
}
