package engine

// proxy_args_test.go：net.proxy 配置 → 引擎 --proxy 参数接线（docs/06 net.*）。
//
// 锁死的不变式：配置了代理时，解析与下载两侧都必须带上 --proxy——
// 只给下载配会造成「解析成功（直连元数据页）、下载超时（媒体流不可达）」的错位，
// 这正是 bad.news 一类站点 NET_TIMEOUT 的典型形态。
// 用假二进制（sh 脚本把 "$@" 落盘）捕获真实命令行，不 mock 引擎内部。

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeArgRecorderBin 生成一个把 "$@" 写进 argsFile 再执行给定动作的假引擎脚本。
func writeArgRecorderBin(t *testing.T, dir, name, action string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("假二进制用 sh 脚本，Windows 跳过")
	}
	bin := filepath.Join(dir, name)
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$FAKE_ARGS\"\n" + action + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("写假引擎失败：%v", err)
	}
	return bin
}

func readArgs(t *testing.T, argsFile string) []string {
	t.Helper()
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("读参数文件失败：%v", err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func assertProxyPassed(t *testing.T, args []string, want string) {
	t.Helper()
	for i, a := range args {
		if a == "--proxy" {
			if i+1 < len(args) && args[i+1] == want {
				return
			}
			t.Fatalf("--proxy 后面跟的不是配置的代理：%v", args)
		}
	}
	t.Fatalf("参数里没有 --proxy：%v", args)
}

func assertNoProxy(t *testing.T, args []string) {
	t.Helper()
	for _, a := range args {
		if a == "--proxy" {
			t.Fatalf("未配置代理时不应出现 --proxy：%v", args)
		}
	}
}

func TestYTDLP_解析与下载带代理(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FAKE_ARGS", filepath.Join(dir, "args"))
	argsFile := filepath.Join(dir, "args")

	// Resolve：假引擎输出一行合法 JSON 即可。
	y := NewYTDLP(writeArgRecorderBin(t, dir, "yt-dlp",
		`echo '{"title":"t","id":"1","extractor":"generic","_type":"video"}'`), "")
	if _, err := y.Resolve(context.Background(), "https://example.com/v", ResolveOpts{Proxy: "http://127.0.0.1:7890"}); err != nil {
		t.Fatalf("Resolve 失败：%v", err)
	}
	assertProxyPassed(t, readArgs(t, argsFile), "http://127.0.0.1:7890")

	// Download：假引擎退出 0 但无产物会报 NO_OUTPUT——没关系，我们只验证参数。
	os.Remove(argsFile)
	d := NewYTDLP(writeArgRecorderBin(t, dir, "yt-dlp-dl", "exit 0"), "")
	saveDir := filepath.Join(dir, "dl")
	_, _ = d.Download(context.Background(), "https://example.com/v", DownloadOpts{
		SaveDir: saveDir, NameTmpl: "%(title)s.%(ext)s", HTTPProxy: "http://127.0.0.1:7890",
	}, nil)
	assertProxyPassed(t, readArgs(t, argsFile), "http://127.0.0.1:7890")
}

func TestGalleryDL_解析与下载带代理(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FAKE_ARGS", filepath.Join(dir, "args"))
	argsFile := filepath.Join(dir, "args")

	g := NewGalleryDL(writeArgRecorderBin(t, dir, "gallery-dl",
		`echo '{"title":"t","category":"generic"}'`))
	if _, err := g.Resolve(context.Background(), "https://example.com/g", ResolveOpts{Proxy: "socks5://127.0.0.1:1080"}); err != nil {
		t.Fatalf("Resolve 失败：%v", err)
	}
	assertProxyPassed(t, readArgs(t, argsFile), "socks5://127.0.0.1:1080")

	os.Remove(argsFile)
	d := NewGalleryDL(writeArgRecorderBin(t, dir, "gallery-dl-dl", "exit 0"))
	_, _ = d.Download(context.Background(), "https://example.com/g", DownloadOpts{
		SaveDir: filepath.Join(dir, "dl"), HTTPProxy: "socks5://127.0.0.1:1080",
	}, nil)
	assertProxyPassed(t, readArgs(t, argsFile), "socks5://127.0.0.1:1080")
}

func TestYTDLP_未配置代理不加参数(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FAKE_ARGS", filepath.Join(dir, "args"))
	argsFile := filepath.Join(dir, "args")

	y := NewYTDLP(writeArgRecorderBin(t, dir, "yt-dlp",
		`echo '{"title":"t","id":"1","extractor":"generic","_type":"video"}'`), "")
	if _, err := y.Resolve(context.Background(), "https://example.com/v", ResolveOpts{}); err != nil {
		t.Fatalf("Resolve 失败：%v", err)
	}
	assertNoProxy(t, readArgs(t, argsFile))
}
