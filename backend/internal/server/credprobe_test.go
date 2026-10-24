package server

// credprobe_test.go：凭据健康探针（定期验证 Cookie 有效性）。
//
// 锁死的不变式（最容易出错的三态语义）：
//   1) 平台明确返回未登录 → expired；
//   2) 平台不可达（网络故障）→ **保持原状态**，绝不误报 expired；
//   3) 探针确认有效 → active（能从 expired 恢复）。
//
// 第 2 条是重点：把网络抖动误报成「凭据失效」会让用户白跑一趟重登。

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/huluohu/raccoon/internal/credential"
	"github.com/huluohu/raccoon/internal/store"
)

// probeTestVault 假 vault：直接返回明文（不真加密）。
type probeTestVault struct{}

func (probeTestVault) Decrypt(cipherText []byte) ([]byte, error) { return cipherText, nil }

// probeTestLogger 假 logger：记录调用，不输出。
type probeTestLogger struct{}

func (probeTestLogger) Info(msg string, fields map[string]any) {}
func (probeTestLogger) Warn(msg string, fields map[string]any) {}

// newProbeEnv 建一个带凭据的测试环境。
func newProbeEnv(t *testing.T) (*store.Store, *credential.Vault) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	vault, err := credential.OpenVault(dir)
	if err != nil {
		t.Fatal(err)
	}
	return st, vault
}

// addCred 建一条凭据（真加密落库）。
func addCred(t *testing.T, st *store.Store, vault *credential.Vault, platform, label, plain string) string {
	t.Helper()
	cipher, err := vault.Encrypt([]byte(plain))
	if err != nil {
		t.Fatal(err)
	}
	c := &store.Credential{
		Platform: platform, Kind: "cookie_file", Label: label,
		CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli(),
	}
	if err := st.CreateCredential(c, cipher); err != nil {
		t.Fatal(err)
	}
	return c.ID
}

// credStatus 读凭据状态。
func credStatus(t *testing.T, st *store.Store, id string) string {
	t.Helper()
	c, _, err := st.GetCredential(id)
	if err != nil {
		t.Fatal(err)
	}
	return c.Status
}

// TestProbe_未登录标记expired 平台明确返回未登录 → 凭据标 expired。
func TestProbe_未登录标记expired(t *testing.T) {
	st, vault := newProbeEnv(t)
	id := addCred(t, st, vault, "bilibili", "B站", "SESSDATA=abc")

	cp := newCredentialProbe(st, vault, func(ctx context.Context, platform, plain string) ProbeOutcome {
		return ProbeUnauthorized
	}, 0, probeTestLogger{})
	cp.runOnce(context.Background())

	if got := credStatus(t, st, id); got != "expired" {
		t.Fatalf("未登录应标 expired，得到 %s", got)
	}
}

// TestProbe_平台不可达保持原状态 网络故障不是凭据问题——状态必须不变。
func TestProbe_平台不可达保持原状态(t *testing.T) {
	st, vault := newProbeEnv(t)
	id := addCred(t, st, vault, "bilibili", "B站", "SESSDATA=abc")

	cp := newCredentialProbe(st, vault, func(ctx context.Context, platform, plain string) ProbeOutcome {
		return ProbeUnreachable
	}, 0, probeTestLogger{})
	cp.runOnce(context.Background())

	if got := credStatus(t, st, id); got != "active" {
		t.Fatalf("平台不可达应保持 active，得到 %s（不能把网络故障误报成凭据失效）", got)
	}
}

// TestProbe_有效保持active 探针确认有效 → active。
func TestProbe_有效保持active(t *testing.T) {
	st, vault := newProbeEnv(t)
	id := addCred(t, st, vault, "bilibili", "B站", "SESSDATA=abc")

	cp := newCredentialProbe(st, vault, func(ctx context.Context, platform, plain string) ProbeOutcome {
		return ProbeOK
	}, 0, probeTestLogger{})
	cp.runOnce(context.Background())

	if got := credStatus(t, st, id); got != "active" {
		t.Fatalf("有效凭据应标 active，得到 %s", got)
	}
}

// TestProbe_已失效可恢复 expired → 探针确认有效后恢复 active。
func TestProbe_已失效可恢复(t *testing.T) {
	st, vault := newProbeEnv(t)
	id := addCred(t, st, vault, "bilibili", "B站", "SESSDATA=abc")
	if err := st.UpdateCredentialStatus(id, "expired"); err != nil {
		t.Fatal(err)
	}

	cp := newCredentialProbe(st, vault, func(ctx context.Context, platform, plain string) ProbeOutcome {
		return ProbeOK
	}, 0, probeTestLogger{})
	cp.runOnce(context.Background())

	if got := credStatus(t, st, id); got != "active" {
		t.Fatalf("探针确认有效后应恢复 active，得到 %s", got)
	}
}

// TestProbe_禁用凭据不探测 用户主动停用的凭据不该被系统改状态。
func TestProbe_禁用凭据不探测(t *testing.T) {
	st, vault := newProbeEnv(t)
	id := addCred(t, st, vault, "bilibili", "B站", "SESSDATA=abc")
	if err := st.UpdateCredentialStatus(id, "disabled"); err != nil {
		t.Fatal(err)
	}

	probed := false
	cp := newCredentialProbe(st, vault, func(ctx context.Context, platform, plain string) ProbeOutcome {
		probed = true
		return ProbeUnauthorized
	}, 0, probeTestLogger{})
	cp.runOnce(context.Background())

	if probed {
		t.Fatal("disabled 凭据不应被探测")
	}
	if got := credStatus(t, st, id); got != "disabled" {
		t.Fatalf("disabled 状态不应被改写，得到 %s", got)
	}
}

// TestProbe_一条失败不影响其他 一条凭据解密失败不应阻断其他凭据的探测。
func TestProbe_一条失败不影响其他(t *testing.T) {
	st, vault := newProbeEnv(t)
	good := addCred(t, st, vault, "bilibili", "好凭据", "SESSDATA=good")

	// 造一条密文损坏的凭据（写库时用假密文，解密会失败）
	bad := &store.Credential{
		Platform: "bilibili", Kind: "cookie_file", Label: "坏凭据",
		CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli(),
	}
	if err := st.CreateCredential(bad, []byte("not-valid-ciphertext")); err != nil {
		t.Fatal(err)
	}

	cp := newCredentialProbe(st, vault, func(ctx context.Context, platform, plain string) ProbeOutcome {
		return ProbeOK
	}, 0, probeTestLogger{})
	cp.runOnce(context.Background())

	if got := credStatus(t, st, good); got != "active" {
		t.Fatalf("好凭据应正常探测为 active，得到 %s", got)
	}
	// 坏凭据解密失败 → 标 expired
	if got := credStatus(t, st, bad.ID); got != "expired" {
		t.Fatalf("解密失败的凭据应标 expired，得到 %s", got)
	}
}

// TestProbe_无平台实现不改状态 未实现探针的平台返回 Unreachable → 状态不变。
func TestProbe_无平台实现不改状态(t *testing.T) {
	st, vault := newProbeEnv(t)
	id := addCred(t, st, vault, "youtube", "YouTube", "SID=xyz")

	cp := newCredentialProbe(st, vault, defaultPlatformProbe, 0, probeTestLogger{})
	cp.runOnce(context.Background())

	if got := credStatus(t, st, id); got != "active" {
		t.Fatalf("无探针实现的平台应保持 active，得到 %s", got)
	}
}

// TestProbe_探针拿到明文 探针必须拿到凭据明文（而不是密文）才能发请求。
func TestProbe_探针拿到明文(t *testing.T) {
	st, vault := newProbeEnv(t)
	addCred(t, st, vault, "bilibili", "B站", "SESSDATA=plaintext_value")

	var gotPlain string
	cp := newCredentialProbe(st, vault, func(ctx context.Context, platform, plain string) ProbeOutcome {
		gotPlain = plain
		return ProbeOK
	}, 0, probeTestLogger{})
	cp.runOnce(context.Background())

	if gotPlain != "SESSDATA=plaintext_value" {
		t.Fatalf("探针应拿到凭据明文，实际 %q", gotPlain)
	}
}

// TestProbe_间隔为0不启动 间隔 <= 0 时不启动后台循环（不产生任何请求）。
func TestProbe_间隔为0不启动(t *testing.T) {
	st, vault := newProbeEnv(t)
	id := addCred(t, st, vault, "bilibili", "B站", "SESSDATA=abc")

	probed := make(chan struct{}, 1)
	cp := newCredentialProbe(st, vault, func(ctx context.Context, platform, plain string) ProbeOutcome {
		select {
		case probed <- struct{}{}:
		default:
		}
		return ProbeOK
	}, 0, probeTestLogger{})
	cp.Start(context.Background())
	defer cp.Stop()

	select {
	case <-probed:
		t.Fatal("间隔为 0 时不应发起探测")
	case <-time.After(300 * time.Millisecond):
		// 期望：不探测
	}
	if got := credStatus(t, st, id); got != "active" {
		t.Fatalf("未探测时状态应保持 active，得到 %s", got)
	}
}
