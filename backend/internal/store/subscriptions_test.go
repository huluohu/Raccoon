package store

import (
	"testing"
)

// TestCreateSubscription_未选凭据也能建成 锁死一个真实故障：
//
// subscriptions.credential_id 是外键（REFERENCES credentials(id)），
// 但未选凭据时前端传的是空串。空串不是合法外键值，插入直接报
// "FOREIGN KEY constraint failed" → 接口回 500，订阅一条都建不出来。
// 「没选凭据」的正确表达是 NULL。
func TestCreateSubscription_未选凭据也能建成(t *testing.T) {
	s := newTestStore(t)

	sub := &Subscription{
		Platform:    "youtube",
		SourceType:  "list",
		SourceID:    "list:@laoming",
		SourceURL:   "https://www.youtube.com/@laoming",
		Title:       "老鳴TV",
		Mode:        "new_only",
		IntervalMin: 30,
		Enabled:     true,
		// CredentialID 故意留空（= 前端「不选凭据」）
	}
	if err := s.CreateSubscription(sub); err != nil {
		t.Fatalf("未选凭据时建订阅应成功，实际失败：%v", err)
	}

	got, err := s.GetSubscription(sub.ID)
	if err != nil {
		t.Fatalf("读回订阅失败：%v", err)
	}
	if got.CredentialID != "" {
		t.Fatalf("未选凭据时 credential_id 应为空，实际 %q", got.CredentialID)
	}
	if got.Title != "老鳴TV" {
		t.Fatalf("标题应回写，实际 %q", got.Title)
	}
}

func TestCreateSubscription_指定凭据时外键生效(t *testing.T) {
	s := newTestStore(t)

	// 建一个真实凭据，让外键有合法目标。
	cred := &Credential{Platform: "bilibili", Kind: "cookie_file", Label: "cookie", CreatedAt: 1, UpdatedAt: 1}
	if err := s.CreateCredential(cred, []byte("SESSDATA=abc")); err != nil {
		t.Fatalf("建凭据失败：%v", err)
	}

	sub := &Subscription{
		Platform: "bilibili", SourceType: "uploader", SourceID: "space:123",
		SourceURL: "https://space.bilibili.com/123", Mode: "new_only",
		IntervalMin: 30, Enabled: true, CredentialID: cred.ID,
	}
	if err := s.CreateSubscription(sub); err != nil {
		t.Fatalf("指定凭据建订阅应成功，实际失败：%v", err)
	}
	got, err := s.GetSubscription(sub.ID)
	if err != nil {
		t.Fatalf("读回订阅失败：%v", err)
	}
	if got.CredentialID != cred.ID {
		t.Fatalf("credential_id 应为 %q，实际 %q", cred.ID, got.CredentialID)
	}
}
