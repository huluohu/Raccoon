package task

// worker_ssrf_test.go：媒体直取旁路的 SSRF 防护（2026-10-30 加固）。
//
// 攻击链（加固前成立）：
//
//	1. 用户提交一个公网 URL（入口校验通过——它确实是公网地址）；
//	2. 该站点在响应里给出媒体清单地址，例如
//	   http://169.254.169.254/latest/meta-data/iam/x.m3u8
//	   ——**这条地址完全由远端控制**，从未经过任何校验；
//	3. 常规下载拿不到媒体（站点可以对媒体地址返回 403，触发直取旁路）；
//	4. 直取旁路把这条地址交给 ffmpeg 抓取 → 服务端替攻击者读了云元数据
//	   或本机管理端口，**读到的内容还会落盘并进媒体库**。
//
// 修复：直取前按「内容类出站」的严格标准复核媒体直链（协议白名单 +
// 域名解析后逐条 IP 判定），命中即拒绝直取并记事件。

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/huluohu/raccoon/internal/engine"
	"github.com/huluohu/raccoon/internal/netguard"
)

// withResolver 临时替换 netguard 的 DNS 解析器（跨包测试用）。
func withResolver(t *testing.T, ips ...string) {
	t.Helper()
	orig := netguard.SetResolverForTest(func(ctx context.Context, host string) ([]net.IP, error) {
		var out []net.IP
		for _, s := range ips {
			out = append(out, net.ParseIP(s))
		}
		return out, nil
	})
	t.Cleanup(orig)
}

// TestManifestSources_只认清单 回归：直取旁路只处理清单型媒体（HLS/DASH），
// 普通直链不该绕这一圈。
func TestManifestSources_只认清单(t *testing.T) {
	formats := []engine.FormatInfo{
		{Quality: "1080p", URL: "https://cdn.example.com/v.mp4"},
		{Quality: "1080p", URL: "https://cdn.example.com/v.m3u8"},
	}
	video, audio := manifestSources(formats)
	if video != "https://cdn.example.com/v.m3u8" {
		t.Errorf("应只取清单型直链，实际 %q", video)
	}
	if audio != "" {
		t.Errorf("无音轨时应为空，实际 %q", audio)
	}
}

// TestMediaHostOnly_日志不带签名 日志里可能带签名参数，只应记主机名。
func TestMediaHostOnly_日志不带签名(t *testing.T) {
	got := mediaHostOnly("https://cdn.example.com/path/x.m3u8?sign=secret&t=123")
	if got != "cdn.example.com" {
		t.Errorf("应只返回主机名，实际 %q", got)
	}
}

// TestNetGuard_Media直链指向内网被拒 核心断言：远端给的直链指向内网时，
// 校验必须拦下（这条直链会被交给 ffmpeg 抓取）。
func TestNetGuard_Media直链指向内网被拒(t *testing.T) {
	withResolver(t, "169.254.169.254")
	err := netguard.ValidateMedia("http://metadata.example.com/latest/meta-data/x.m3u8")
	if err == nil {
		t.Fatal("指向云元数据的媒体直链应被拒绝")
	}
}

// TestNetGuard_正常媒体直链放行 不能把正常下载搞挂。
func TestNetGuard_正常媒体直链放行(t *testing.T) {
	withResolver(t, "104.18.32.47")
	if err := netguard.ValidateMedia("https://cdn.example.com/seg.m3u8"); err != nil {
		t.Fatalf("正常媒体直链应放行: %v", err)
	}
}

// TestNetGuard_直链协议受限 清单地址只允许 http/https。
func TestNetGuard_直链协议受限(t *testing.T) {
	if err := netguard.ValidateMedia("file:///etc/passwd"); err == nil {
		t.Fatal("file:// 直链应被拒绝")
	}
	if err := netguard.ValidateMedia("gopher://127.0.0.1:6379/_INFO"); err == nil {
		t.Fatal("gopher:// 直链应被拒绝")
	}
}

// TestNetGuard_解析失败不误伤 代理用户的域名可能只���代理侧解析。
func TestNetGuard_解析失败不误伤(t *testing.T) {
	orig := netguard.SetResolverForTest(func(ctx context.Context, host string) ([]net.IP, error) {
		return nil, errors.New("no such host")
	})
	t.Cleanup(orig)
	if err := netguard.ValidateMedia("https://via-proxy.example.com/x.m3u8"); err != nil {
		t.Fatalf("解析失败不应拦截: %v", err)
	}
}
