package server

// credprobe.go：凭据健康探针（定期用平台 API 验证 Cookie 有效性）。
//
// 为什么需要：Cookie 过期是自托管下载器最常见的失败原因，但它是「静默」的——
// 用户不点下载就不知道凭据已死，等发现时往往已经攒了一堆失败任务。
// 定期探测把「发现」从事后变成事前：平台账号页直接显示「已失效」。
//
// 三态语义（不能混淆）：
//   - active   ：平台 API 确认登录态有效。
//   - expiring ：探针连不上平台（网络故障/平台维护）——**不是凭据的问题**，
//     保持原状态，只更新 last_check_at。不能把网络抖动误报成「凭据失效」。
//   - expired  ：平台 API 明确返回「未登录」——凭据确实失效。
//
// 探针可注入（probeFunc），测试用假实现，不打真实网络。

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/huluohu/raccoon/internal/store"
)

// ProbeOutcome 一次探测的结论。
type ProbeOutcome int

const (
	// ProbeOK 登录态有效。
	ProbeOK ProbeOutcome = iota
	// ProbeUnreachable 平台不可达（网络/平台故障）——不据此判凭据失效。
	ProbeUnreachable
	// ProbeUnauthorized 平台明确返回未登录——凭据失效。
	ProbeUnauthorized
)

// PlatformProbe 对某平台的凭据做一次探测，返回结论。
// plain 是凭据明文（Cookie 头字符串）。
type PlatformProbe func(ctx context.Context, platform, plain string) ProbeOutcome

// credentialProbe 定期探测凭据健康度的后台任务。
type credentialProbe struct {
	store  *store.Store
	vault  VaultLike
	probe  PlatformProbe
	every  time.Duration
	logger LoggerLike
	cancel context.CancelFunc
}

// VaultLike 解密能力的最小接口（避免依赖 credential 包的实现细节）。
type VaultLike interface {
	Decrypt(cipherText []byte) ([]byte, error)
}

// LoggerLike 日志最小接口。
type LoggerLike interface {
	Info(msg string, fields map[string]any)
	Warn(msg string, fields map[string]any)
}

// newCredentialProbe 构造探针；every<=0 时不启动（探针可通过配置关闭）。
func newCredentialProbe(st *store.Store, vault VaultLike, probe PlatformProbe, every time.Duration, logger LoggerLike) *credentialProbe {
	return &credentialProbe{store: st, vault: vault, probe: probe, every: every, logger: logger}
}

// Start 启动后台探测（每 every 一轮）。
func (cp *credentialProbe) Start(ctx context.Context) {
	if cp == nil || cp.every <= 0 || cp.probe == nil {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	cp.cancel = cancel
	go func() {
		ticker := time.NewTicker(cp.every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cp.runOnce(ctx)
			}
		}
	}()
}

// Stop 停止后台探测。
func (cp *credentialProbe) Stop() {
	if cp != nil && cp.cancel != nil {
		cp.cancel()
	}
}

// runOnce 跑一轮探测：逐条凭据独立处理，一条失败不影响其他。
func (cp *credentialProbe) runOnce(ctx context.Context) {
	creds, err := cp.store.ListCredentials()
	if err != nil {
		cp.logger.Warn("凭据探测：列表读取失败", map[string]any{"err": err.Error()})
		return
	}
	for _, c := range creds {
		if c.Status == "disabled" {
			continue // 用户主动停用，不探测
		}
		cp.probeOne(ctx, c)
	}
}

// probeOne 探测单条凭据并落状态。
func (cp *credentialProbe) probeOne(ctx context.Context, c *store.Credential) {
	_, cipher, err := cp.store.GetCredential(c.ID)
	if err != nil {
		cp.logger.Warn("凭据探测：读取密文失败", map[string]any{
			"credential_id": c.ID, "platform": c.Platform, "err": err.Error(),
		})
		return
	}
	if len(cipher) == 0 {
		return // 没有值的凭据无从探测
	}
	plain, err := cp.vault.Decrypt(cipher)
	if err != nil {
		// 解密失败（主密钥不匹配/数据损坏）——这确实意味着这条凭据用不了了。
		_ = cp.store.UpdateCredentialStatus(c.ID, "expired")
		cp.logger.Warn("凭据探测：解密失败，标记为已失效", map[string]any{
			"credential_id": c.ID, "platform": c.Platform,
		})
		return
	}

	outcome := cp.probe(ctx, c.Platform, string(plain))
	switch outcome {
	case ProbeOK:
		if c.Status != "active" {
			_ = cp.store.UpdateCredentialStatus(c.ID, "active")
			cp.logger.Info("凭据探测：已恢复有效", map[string]any{
				"credential_id": c.ID, "platform": c.Platform,
			})
		} else {
			// 状态没变也要更新 last_check_at（平台账号页显示「上次检查」）。
			_ = cp.store.UpdateCredentialStatus(c.ID, "active")
		}
	case ProbeUnauthorized:
		if c.Status != "expired" {
			_ = cp.store.UpdateCredentialStatus(c.ID, "expired")
			cp.logger.Warn("凭据探测：平台返回未登录，标记为已失效", map[string]any{
				"credential_id": c.ID, "platform": c.Platform,
			})
		} else {
			_ = cp.store.UpdateCredentialStatus(c.ID, "expired")
		}
	case ProbeUnreachable:
		// 平台不可达不是凭据的问题——保持原状态，只刷新检查时间。
		_ = cp.store.UpdateCredentialStatus(c.ID, c.Status)
		cp.logger.Warn("凭据探测：平台不可达，保留原状态", map[string]any{
			"credential_id": c.ID, "platform": c.Platform,
		})
	}
}

// ---- 真实平台探针 ----

// bilibiliNavProbe B 站登录态探针。
//
// B 站 nav 接口用 cookie 鉴权：未登录返回 code=-101（账号未登录），
// 登录后返回 code=0 且 data.isLogin=true。
//
// 注意：外层 code 恒为 0，业务码在**内层 data.code**——早先按外层解析导致
// 「测试全绿、真机必挂」。这里两层都看。
func bilibiliNavProbe(ctx context.Context, platform, plain string) ProbeOutcome {
	if platform != "bilibili" {
		return ProbeUnreachable // 该平台无探针实现：不做判断
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.bilibili.com/x/web-interface/nav", nil)
	if err != nil {
		return ProbeUnreachable
	}
	req.Header.Set("Cookie", plain)
	req.Header.Set("User-Agent", "Mozilla/5.0 Raccoon/1.0")
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ProbeUnreachable // 网络问题不是凭据问题
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return ProbeUnauthorized
		}
		return ProbeUnreachable
	}
	// 读 body 判断业务码
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	if strings.Contains(body, `"isLogin":true`) {
		return ProbeOK
	}
	if strings.Contains(body, `"code":-101`) || strings.Contains(body, `"code":-400`) {
		return ProbeUnauthorized
	}
	// 认不出来：不猜，保持原状态（宁可漏报也不误报）
	return ProbeUnreachable
}

// defaultPlatformProbe 平台探针路由：已实现探针的平台走探针，
// 其余平台返回 ProbeUnreachable（不判断、不改状态）。
func defaultPlatformProbe(ctx context.Context, platform, plain string) ProbeOutcome {
	switch platform {
	case "bilibili":
		return bilibiliNavProbe(ctx, platform, plain)
	}
	return ProbeUnreachable
}
