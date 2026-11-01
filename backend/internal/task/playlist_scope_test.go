package task

// playlist_scope_test.go：合集/频道的下载范围。
//
// 2026-11-01 加：预览只解析第一条（快），下载阶段默认会拉**整个列表**——
// 用户预览时看到「1 个视频 3:45」，以为只下一个，点下去却开始下 25 个 1080P。
// 现在显式可选范围（0 = 全部）。

import (
	"testing"
)

// TestClampPlaylistItems_规整范围 负数视为「全部」，超量封顶（--playlist-items
// 是拼进命令行的，必须有界）。
func TestClampPlaylistItems_规整范围(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, 0},
		{-5, 0},
		{1, 1},
		{3, 3},
		{100000, 500},
	}
	for _, c := range cases {
		if got := clampPlaylistItems(c.in); got != c.want {
			t.Errorf("clampPlaylistItems(%d) = %d，期望 %d", c.in, got, c.want)
		}
	}
}

// TestSubmitItem_条目数落库 提交时要真的把范围记到任务上。
func TestSubmitItem_条目数落库(t *testing.T) {
	m := newExtTestManager(t)
	res, err := m.Submit([]SubmitItem{
		{URL: "https://www.bilibili.com/video/BV1xx411c7mD", PlaylistItems: 3},
	}, SourceWeb, "test")
	if err != nil {
		t.Fatalf("提交失败: %v", err)
	}
	if len(res) == 0 || res[0].Task == nil {
		t.Fatal("提交应产出任务")
	}
	got, err := m.st.GetTask(res[0].Task.ID)
	if err != nil {
		t.Fatalf("取任务失败: %v", err)
	}
	if got.PlaylistItems != 3 {
		t.Errorf("playlist_items 应落库为 3，实际 %d", got.PlaylistItems)
	}
}

// TestSubmitItem_超量条目数被封顶 提交 100000 条时按上限封顶，避免变成
// 「无限下载」的入口。
func TestSubmitItem_超量条目数被封顶(t *testing.T) {
	m := newExtTestManager(t)
	res, err := m.Submit([]SubmitItem{
		{URL: "https://www.bilibili.com/video/BV1yy411c7mD", PlaylistItems: 100000},
	}, SourceWeb, "test")
	if err != nil {
		t.Fatalf("提交失败: %v", err)
	}
	got, err := m.st.GetTask(res[0].Task.ID)
	if err != nil {
		t.Fatalf("取任务失败: %v", err)
	}
	if got.PlaylistItems != 500 {
		t.Errorf("超量条目数应封顶为 500，实际 %d", got.PlaylistItems)
	}
}
