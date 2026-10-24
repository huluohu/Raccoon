package task

// worker_cred_test.go：凭据失效的被动检测。
//
// 锁死的不变式：
//   1) 任务因 AUTH_REQUIRED 失败且**带了凭据** → 该平台凭据降级为 expiring；
//   2) 任务因 AUTH_REQUIRED 失败但**没带凭据** → 凭据状态不动
//      （内容本身要登录，与用户存的凭据无关，误标会让用户白跑一趟）；
//   3) 降级只到 expiring，不判死 —— 真正的 expired 由平台 API 探针确认。

import (
	"context"
	"testing"
	"time"

	"github.com/huluohu/raccoon/internal/engine"
	"github.com/huluohu/raccoon/internal/store"
)

// authRequiredEngine 恒返回 AUTH_REQUIRED 的假引擎。
type authRequiredEngine struct {
	fakeWorkerEngine
}

func (f *authRequiredEngine) Download(ctx context.Context, url string, opts engine.DownloadOpts, onProgress func(engine.Progress)) ([]engine.DownloadedFile, error) {
	return nil, &engine.EngineError{Code: "AUTH_REQUIRED", Message: "需要登录或 Cookie 已失效"}
}

// credTestResolver 返回固定凭据明文的假解析器。
type credTestResolver struct {
	plain string
}

func (f *credTestResolver) PlatformCredential(ctx context.Context, platform string) (string, error) {
	return f.plain, nil
}

// injectCredential 注入假凭据解析器，让 worker 认为「本次任务带了凭据」。
//
// 平台必须用已收录的（cookieDomainFor 认得的）——未收录平台拿不到 Cookie 域，
// 凭据文件不产，usedCredential 自然是 false。
func injectCredential(t *testing.T, m *Manager, platform, plain string) {
	t.Helper()
	m.SetCredentialResolver(&credTestResolver{plain: plain})
	_ = m.cfg.Set("storage.data_dir", t.TempDir())
}

// newCredTask 建一条指定平台的任务并领取到 parsing。
func newCredTask(t *testing.T, m *Manager, platform string) *store.Task {
	t.Helper()
	id := store.NewTaskID()
	if err := m.st.CreateTask(&store.Task{
		ID: id, Kind: KindDownload, Source: "web", Platform: platform,
		URL: "https://example.com/video/1", Title: "凭据测试任务",
		DedupeKey:   store.DedupeKey(KindDownload, platform, id),
		MaxAttempts: 3, CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("建任务失败：%v", err)
	}
	claimed, err := m.st.ClaimWaiting(id, "parsing")
	if err != nil {
		t.Fatalf("领取任务失败：%v", err)
	}
	return claimed
}

// addPlatformCred 建一条平台凭据，返回其真实 ID（CreateCredential 自行生成 ID）。
func addPlatformCred(t *testing.T, m *Manager, platform string) string {
	t.Helper()
	if err := m.st.CreateCredential(&store.Credential{
		Platform: platform, Kind: "cookie_file", Label: platform,
		Status: "active", CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli(),
	}, []byte("fake-ciphertext")); err != nil {
		t.Fatalf("建凭据失败：%v", err)
	}
	creds, err := m.st.ListCredentials()
	if err != nil {
		t.Fatalf("列凭据失败：%v", err)
	}
	for _, c := range creds {
		if c.Platform == platform {
			return c.ID
		}
	}
	t.Fatalf("建凭据后未在列表中找到 platform=%s 的凭据", platform)
	return ""
}

// TestWorker_AUTH_REQUIRED降级凭据为expiring 带了凭据 + AUTH_REQUIRED → 降级。
func TestWorker_AUTH_REQUIRED降级凭据为expiring(t *testing.T) {
	m := newExtTestManager(t)
	credID := addPlatformCred(t, m, "bilibili")
	injectCredential(t, m, "bilibili", "SESSDATA=abc")
	w := &Worker{mgr: m, eng: &authRequiredEngine{}}
	task := newCredTask(t, m, "bilibili")

	w.Run(context.Background(), task)

	cur := mustTask(t, m, task.ID)
	if cur.Status != StatusFailed {
		t.Fatalf("前置：AUTH_REQUIRED 应直接 failed，得到 %s", cur.Status)
	}

	c, _, err := m.st.GetCredential(credID)
	if err != nil {
		t.Fatalf("读凭据失败：%v", err)
	}
	if c.Status != "expiring" {
		t.Fatalf("凭据应降级为 expiring，得到 %s", c.Status)
	}
	if c.Status == "expired" {
		t.Fatal("被动检测不应判死为 expired（那要靠平台 API 探针确认）")
	}
}

// TestWorker_无凭据时不动凭据状态 没带凭据 + AUTH_REQUIRED → 状态不动。
func TestWorker_无凭据时不动凭据状态(t *testing.T) {
	m := newExtTestManager(t)
	credID := addPlatformCred(t, m, "bilibili")
	// 不注入 resolver → 本次任务无凭据
	w := &Worker{mgr: m, eng: &authRequiredEngine{}}
	task := newCredTask(t, m, "bilibili")

	w.Run(context.Background(), task)

	c, _, err := m.st.GetCredential(credID)
	if err != nil {
		t.Fatalf("读凭据失败：%v", err)
	}
	if c.Status != "active" {
		t.Fatalf("无凭据任务不应改动凭据状态，得到 %s", c.Status)
	}
}

// TestWorker_降级写事件与通知 降级要留痕，让用户知道该去检查。
func TestWorker_降级写事件与通知(t *testing.T) {
	m := newExtTestManager(t)
	addPlatformCred(t, m, "bilibili")
	injectCredential(t, m, "bilibili", "SESSDATA=abc")
	w := &Worker{mgr: m, eng: &authRequiredEngine{}}
	task := newCredTask(t, m, "bilibili")

	w.Run(context.Background(), task)

	events, _, err := m.st.ListEvents(task.ID, 0, 100)
	if err != nil {
		t.Fatalf("读事件失败：%v", err)
	}
	var found bool
	for _, e := range events {
		if e.Event == "credential_expiring" {
			found = true
			if e.Level != "warn" {
				t.Fatalf("credential_expiring 级别应为 warn，得到 %s", e.Level)
			}
		}
	}
	if !found {
		t.Fatalf("应记录 credential_expiring 事件，实际：%v", events)
	}
}

// TestWorker_其他错误码不降级凭据 只有 AUTH_REQUIRED 才降级，
// 网络超时/平台限流与凭据无关。
func TestWorker_其他错误码不降级凭据(t *testing.T) {
	m := newExtTestManager(t)
	credID := addPlatformCred(t, m, "bilibili")
	injectCredential(t, m, "bilibili", "SESSDATA=abc")
	w := &Worker{mgr: m, eng: &fakeWorkerEngine{downloadErrs: []error{
		netTimeoutErr("网络连接失败或被重置"),
	}}}
	task := newCredTask(t, m, "bilibili")

	w.Run(context.Background(), task)

	c, _, err := m.st.GetCredential(credID)
	if err != nil {
		t.Fatalf("读凭据失败：%v", err)
	}
	if c.Status != "active" {
		t.Fatalf("NET_TIMEOUT 不应降级凭据，得到 %s", c.Status)
	}
}

// TestMarkPlatformCredentialStatus_禁用不被改写 disabled 是用户主动停用，
// 系统探针不该改写它。
func TestMarkPlatformCredentialStatus_禁用不被改写(t *testing.T) {
	m := newExtTestManager(t)
	credID := addPlatformCred(t, m, "test")
	if err := m.st.UpdateCredentialStatus(credID, "disabled"); err != nil {
		t.Fatal(err)
	}

	n, err := m.st.MarkPlatformCredentialStatus("test", "expiring")
	if err != nil {
		t.Fatalf("标记失败：%v", err)
	}
	if n != 0 {
		t.Fatalf("disabled 凭据不应被标记，实际标记 %d 条", n)
	}
	c, _, err := m.st.GetCredential(credID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != "disabled" {
		t.Fatalf("disabled 应保持不变，得到 %s", c.Status)
	}
}

// TestMarkPlatformCredentialStatus_同状态不重复计数 已是 expiring 的再标 expiring
// 不应算作「变更条数」。
func TestMarkPlatformCredentialStatus_同状态不重复计数(t *testing.T) {
	m := newExtTestManager(t)
	credID := addPlatformCred(t, m, "test")
	if err := m.st.UpdateCredentialStatus(credID, "expiring"); err != nil {
		t.Fatal(err)
	}
	n, err := m.st.MarkPlatformCredentialStatus("test", "expiring")
	if err != nil {
		t.Fatalf("标记失败：%v", err)
	}
	if n != 0 {
		t.Fatalf("同状态不应计入变更条数，得到 %d", n)
	}
	_ = credID
}
