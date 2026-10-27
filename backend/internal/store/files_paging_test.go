package store

import (
	"path/filepath"
	"testing"
)

// TestListMediaFiles_分页 首页 + 偏移页不重不漏，且 total 是筛选后的总数。
//
// 媒体库前端靠 offset 翻页（2026-10-25 接的分页），这里锁住后端的分页语义：
// offset=0 取最新的一批，offset=N 接在后面，中间没有重复也没有漏。
func TestListMediaFiles_分页(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for i := 0; i < 5; i++ {
		id := "mf" + string(rune('a'+i))
		if err := s.UpsertMediaFile(&MediaFile{
			ID: id, Path: "/d/" + id + ".mp4", Name: id + ".mp4", Size: 10,
			Kind: "video", Platform: "bilibili",
		}); err != nil {
			t.Fatal(err)
		}
	}

	// 首页
	page1, total, err := s.ListMediaFiles(FileFilter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 {
		t.Fatalf("total 应为 5，实际 %d", total)
	}
	if len(page1) != 2 {
		t.Fatalf("首页应有 2 条，实际 %d", len(page1))
	}

	// 偏移页：接在首页后面，两页合起来 4 条互不重复
	page2, _, err := s.ListMediaFiles(FileFilter{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 2 {
		t.Fatalf("第二页应有 2 条，实际 %d", len(page2))
	}
	seen := map[string]bool{}
	for _, it := range append(append([]*MediaFile{}, page1...), page2...) {
		if seen[it.ID] {
			t.Fatalf("分页出现重复条目: %s", it.ID)
		}
		seen[it.ID] = true
	}

	// 越界的偏移返回空页而不是报错
	page3, _, err := s.ListMediaFiles(FileFilter{Limit: 2, Offset: 99})
	if err != nil {
		t.Fatal(err)
	}
	if len(page3) != 0 {
		t.Fatalf("越界偏移应返回空页，实际 %d 条", len(page3))
	}

	// 带筛选时分页同样成立，total 只算筛选后的
	filtered, ftotal, err := s.ListMediaFiles(FileFilter{Limit: 10, Platform: "youtube"})
	if err != nil {
		t.Fatal(err)
	}
	if ftotal != 0 || len(filtered) != 0 {
		t.Fatalf("不匹配的平台应为空，实际 total=%d 条数=%d", ftotal, len(filtered))
	}
}
