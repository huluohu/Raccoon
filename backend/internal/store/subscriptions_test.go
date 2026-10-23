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

// TestUpdateSubscription_未选凭据也能暂停 锁死「点暂停完全没反应」的真因：
//
// UPDATE 语句漏了 nullIfEmpty，把空串写进 credential_id 外键列 →
// "FOREIGN KEY constraint failed" → 接口 500 → 前端只能弹「操作失败」，
// 订阅状态一个都没变。列表页每个未绑凭据的订阅都中招（绝大多数订阅都不绑凭据）。
func TestUpdateSubscription_未选凭据也能暂停(t *testing.T) {
	s := newTestStore(t)

	sub := &Subscription{
		Platform: "bilibili", SourceType: "favorite", SourceID: "mid1",
		SourceURL: "https://space.bilibili.com/1/favlist?fid=2", Title: "收藏夹",
		Mode: "new_only", IntervalMin: 30, Enabled: true,
		// CredentialID 留空：前端「不选凭据」就是这个形状
	}
	if err := s.CreateSubscription(sub); err != nil {
		t.Fatalf("建订阅失败：%v", err)
	}

	// 暂停（enabled=false）。
	sub.Enabled = false
	if err := s.UpdateSubscription(sub); err != nil {
		t.Fatalf("未绑凭据的订阅暂停应成功，实际失败：%v", err)
	}
	got, err := s.GetSubscription(sub.ID)
	if err != nil {
		t.Fatalf("读回订阅失败：%v", err)
	}
	if got.Enabled {
		t.Fatal("暂停后 enabled 应为 false")
	}

	// 再启用（enabled=true），确认双向都能改。
	sub.Enabled = true
	if err := s.UpdateSubscription(sub); err != nil {
		t.Fatalf("重新启用应成功，实际失败：%v", err)
	}
	if got2, _ := s.GetSubscription(sub.ID); !got2.Enabled {
		t.Fatal("启用后 enabled 应为 true")
	}
}

// TestUpdateSubscription_解绑凭据置NULL 改规则时把 credential_id 清空，
// 必须写 NULL 而不是空串，否则同样撞外键。
func TestUpdateSubscription_解绑凭据置NULL(t *testing.T) {
	s := newTestStore(t)

	cred := &Credential{Platform: "bilibili", Kind: "cookie_file", Label: "cookie", CreatedAt: 1, UpdatedAt: 1}
	if err := s.CreateCredential(cred, []byte("SESSDATA=abc")); err != nil {
		t.Fatalf("建凭据失败：%v", err)
	}
	sub := &Subscription{
		Platform: "bilibili", SourceType: "uploader", SourceID: "space:1",
		SourceURL: "https://space.bilibili.com/1", Mode: "new_only",
		IntervalMin: 30, Enabled: true, CredentialID: cred.ID,
	}
	if err := s.CreateSubscription(sub); err != nil {
		t.Fatalf("建订阅失败：%v", err)
	}

	// 解绑凭据（前端把 credential_id 传空串）。
	sub.CredentialID = ""
	sub.Quality = "1080p"
	if err := s.UpdateSubscription(sub); err != nil {
		t.Fatalf("解绑凭据应成功，实际失败：%v", err)
	}
	got, err := s.GetSubscription(sub.ID)
	if err != nil {
		t.Fatalf("读回订阅失败：%v", err)
	}
	if got.CredentialID != "" {
		t.Fatalf("解绑后 credential_id 应为空，实际 %q", got.CredentialID)
	}
	if got.Quality != "1080p" {
		t.Fatalf("画质应更新为 1080p，实际 %q", got.Quality)
	}
}
