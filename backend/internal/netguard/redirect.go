package netguard

// redirect.go：重定向链逐跳校验。
//
// 为什么需要（2026-10-30 加固）：入口 URL 校验只管**第一次**请求，
// 而 HTTP 重定向会把客户端带到别处去。若不逐跳复核，攻击者用一个公网地址
// 作跳板：
//
//	http://evil.example/r  →  302 Location: http://169.254.169.254/latest/…
//
// 入口校验通过（evil.example 确实是公网地址），跟随重定向后服务端却去读了
// 云元数据端点。docs/04 §5 早就写着「重定向逐跳复查 ≤5 跳」，但代码里
// 并没有实现——文档与实现对不上，对安全评审来说比没有防护更糟。
//
// 做法：自己走一遍重定向链（**不自动跟随**），每一跳都按出站标准校验；
// 任一跳落到内网/环回/链路本地即拒绝。
//
// 与真正发请求的组件之间存在时间差（TOCTOU）：攻击者可以在预检通过后
// 改变重定向目标。这一点与 DNS rebinding 同类，靠纵深防御覆盖，不隐瞒。

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultMaxRedirects 默认最大跟随跳数。
const DefaultMaxRedirects = 5

// redirectCheckTimeout 整条链的校验超时。
const redirectCheckTimeout = 8 * time.Second

// CheckRedirectChain 走一遍重定向链并逐跳校验。
//
// 合法返回 nil。命中内网目标返回错误。
//
// 刻意「失败不拦截」的情形（避免误伤正常下载）：
//   - 首跳就请求失败（网络问题、站点要求特定请求头/鉴权）→ 放行；
//   - 响应不是重定向（200/403 等）→ 放行；
//   - 链太长超过 maxHops → 放行（只报一条提示，不阻断）。
//
// 只有「明确看到某一跳指向内网」才拒绝——那是确定性的攻击特征。
func CheckRedirectChain(ctx context.Context, raw string, maxHops int) error {
	if maxHops <= 0 {
		maxHops = DefaultMaxRedirects
	}
	u, err := parseHTTPURL(raw)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, redirectCheckTimeout)
	defer cancel()

	client := &http.Client{
		// 不自动跟随：自己逐跳走，才能在每一跳之间做校验。
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	if rt := redirectTransport(); rt != nil {
		client.Transport = rt
	}
	current := u
	for hop := 0; hop < maxHops; hop++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, current.String(), nil)
		if err != nil {
			return nil // 构造不出来就算了，不阻断（引擎自己会报错）
		}
		// 站点多半要求浏览器 UA；带上常见的，不带也不该阻断。
		req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Raccoon/1.0)")
		resp, err := client.Do(req)
		if err != nil {
			return nil // 网络/协议问题 → 不阻断
		}
		loc := resp.Header.Get("Location")
		status := resp.StatusCode
		// 立刻丢弃响应体，避免把大文件拉下来。
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		_ = resp.Body.Close()
		if status < 300 || status >= 400 || loc == "" {
			return nil // 不是重定向 → 链结束
		}
		next, err := current.Parse(loc) // 相对 Location 也能解析
		if err != nil {
			return nil
		}
		// 逐跳校验：这一跳的目标必须是可安全请求的地址。
		if err := Validate(next.String(), Options{ResolveDNS: true}); err != nil {
			return fmt.Errorf("重定向第 %d 跳指向不安全地址: %w", hop+1, err)
		}
		current = next
	}
	return nil
}

// redirectTransport 供测试替换的 HTTP 传输层（nil = 用默认）。
var redirectTransport = func() http.RoundTripper { return nil }

// SetTransportForTest 替换重定向校验用的传输层，返回还原函数。
func SetTransportForTest(rt http.RoundTripper) func() {
	orig := redirectTransport
	redirectTransport = func() http.RoundTripper { return rt }
	return func() { redirectTransport = orig }
}
