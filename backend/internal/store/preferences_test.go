package store

// preferences_test.go：用户偏好单行读写（docs/15 §3.3 迁移 0007）。

import (
	"strings"
	"testing"
)

func TestPreferencesLayout_空库返回空串(t *testing.T) {
	s := newTestStore(t)
	got, err := s.GetPreferencesLayout()
	if err != nil {
		t.Fatalf("读偏好失败: %v", err)
	}
	if got != "" {
		t.Fatalf("无记录应返回空串（前端回落默认布局），实际 %q", got)
	}
}

func TestPreferencesLayout_写入后覆盖写(t *testing.T) {
	s := newTestStore(t)
	first := `{"blocks":[{"id":"metric_cards","visible":true}]}`
	if err := s.SetPreferencesLayout(first); err != nil {
		t.Fatalf("写偏好失败: %v", err)
	}
	got, err := s.GetPreferencesLayout()
	if err != nil {
		t.Fatalf("读偏好失败: %v", err)
	}
	if got != first {
		t.Fatalf("写入读回不一致: %q != %q", got, first)
	}

	// 覆盖写（UPSERT，单行不堆积）。
	second := `{"blocks":[]}`
	if err := s.SetPreferencesLayout(second); err != nil {
		t.Fatalf("覆盖写失败: %v", err)
	}
	got2, err := s.GetPreferencesLayout()
	if err != nil {
		t.Fatalf("读偏好失败: %v", err)
	}
	if got2 != second {
		t.Fatalf("覆盖写应生效，实际 %q", got2)
	}

	// 单行保证：表里只有一行。
	var n int
	if err := s.RO.QueryRow(`SELECT COUNT(*) FROM user_preferences`).Scan(&n); err != nil {
		t.Fatalf("计数失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("user_preferences 应只有 1 行，实际 %d", n)
	}
}

func TestPreferencesLayout_含中文与引号的JSON原样往返(t *testing.T) {
	s := newTestStore(t)
	layout := `{"title":"主页布局","order":["趋势图","环形图"]}`
	if err := s.SetPreferencesLayout(layout); err != nil {
		t.Fatalf("写偏好失败: %v", err)
	}
	got, err := s.GetPreferencesLayout()
	if err != nil {
		t.Fatalf("读偏好失败: %v", err)
	}
	if !strings.Contains(got, "主页布局") || !strings.Contains(got, "趋势图") {
		t.Fatalf("中文内容应原样往返，实际 %q", got)
	}
}
