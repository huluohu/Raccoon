package store

import (
	"path/filepath"
	"testing"
)

// newTestStore 起一个临时库（内存不行：partial index 与 WAL 行为需要真实文件）。
func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试库失败：%v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestActiveConcurrency_软删任务不占并发 锁死一个已上线的严重缺陷：
//
// ActiveConcurrency 曾漏掉 deleted_at 过滤。用户软删（SoftDelete）一个卡在
// parsing/downloading 的任务后，该行仍被计入 byPlatform，
// 持续占满 download.platform_cap —— 该平台所有新任务被永久饿死，
// 队列看起来「有任务在等」却一个都不动，且没有任何错误日志。
func TestActiveConcurrency_软删任务不占并发(t *testing.T) {
	s := newTestStore(t)

	// 两个 bilibili 任务处于 downloading（占并发）
	for i := 0; i < 2; i++ {
		id := NewTaskID()
		if err := s.CreateTask(&Task{
			ID: id, Kind: "download", Source: "web", Platform: "bilibili",
			URL: "https://www.bilibili.com/video/BV" + string(rune('A'+i)),
			DedupeKey: DedupeKey("download", "bilibili", id),
			MaxAttempts: 3, CreatedAt: 1, UpdatedAt: 1,
		}); err != nil {
			t.Fatalf("建任务失败：%v", err)
		}
		if _, err := s.ClaimWaiting(id, "downloading"); err != nil {
			t.Fatalf("领取任务失败：%v", err)
		}
	}

	_, byPlatform, err := s.ActiveConcurrency()
	if err != nil {
		t.Fatalf("统计并发失败：%v", err)
	}
	if byPlatform["bilibili"] != 2 {
		t.Fatalf("软删前应统计到 2 个活跃任务，实际 %d", byPlatform["bilibili"])
	}

	// 软删其中一个（模拟用户在 UI 上删除一个卡住的任务）
	var ids []string
	rows, err := s.RO.Query(`SELECT id FROM tasks WHERE deleted_at IS NULL AND status='downloading' LIMIT 1`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) != 1 {
		t.Fatalf("应找到 1 个待软删任务，实际 %d", len(ids))
	}
	if err := s.SoftDelete(ids[0]); err != nil {
		t.Fatalf("软删失败：%v", err)
	}

	_, byPlatform, err = s.ActiveConcurrency()
	if err != nil {
		t.Fatal(err)
	}
	if got := byPlatform["bilibili"]; got != 1 {
		t.Errorf("软删的任务不应继续占用并发，应为 1，实际 %d（不修则永久占满 cap 导致队列饿死）", got)
	}
}

// TestActiveConcurrency_批量父任务不占并发 父任务是聚合壳，没有真实下载动作，
// 计入并发会虚增占用。
func TestActiveConcurrency_批量父任务不占并发(t *testing.T) {
	s := newTestStore(t)
	id := NewTaskID()
	if err := s.CreateTask(&Task{
		ID: id, Kind: "download", Source: "web", Platform: "batch",
		URL: "https://example.com/playlist", DedupeKey: DedupeKey("download", "batch", id),
		MaxAttempts: 3, CreatedAt: 1, UpdatedAt: 1,
	}); err != nil {
		t.Fatalf("建父任务失败：%v", err)
	}
	if _, err := s.ClaimWaiting(id, "downloading"); err != nil {
		t.Fatalf("领取父任务失败：%v", err)
	}
	_, byPlatform, err := s.ActiveConcurrency()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := byPlatform["batch"]; ok {
		t.Error("批量父任务不应计入平台并发")
	}
}

// TestDueWaiting_不返回批量父任务 父任务只能由 aggregateParent 推进，
// 被调度器领走会重复下载首个子任务的链接。
func TestDueWaiting_不返回批量父任务(t *testing.T) {
	s := newTestStore(t)
	parentID := NewTaskID()
	if err := s.CreateTask(&Task{
		ID: parentID, Kind: "download", Source: "web", Platform: "batch",
		URL: "https://example.com/playlist", DedupeKey: DedupeKey("download", "batch", parentID),
		MaxAttempts: 3, CreatedAt: 1, UpdatedAt: 1,
	}); err != nil {
		t.Fatalf("建父任务失败：%v", err)
	}
	childID := NewTaskID()
	if err := s.CreateTask(&Task{
		ID: childID, ParentID: &parentID, Kind: "download", Source: "web", Platform: "bilibili",
		URL: "https://www.bilibili.com/video/BV1", DedupeKey: DedupeKey("download", "bilibili", childID),
		MaxAttempts: 3, CreatedAt: 2, UpdatedAt: 2,
	}); err != nil {
		t.Fatalf("建子任务失败：%v", err)
	}

	due, err := s.DueWaiting(9999999999999, 20)
	if err != nil {
		t.Fatalf("查待领任务失败：%v", err)
	}
	for _, d := range due {
		if d.ID == parentID {
			t.Error("批量父任务不应出现在待领队列（会被当普通下载领走并覆写 platform）")
		}
	}
	found := false
	for _, d := range due {
		if d.ID == childID {
			found = true
		}
	}
	if !found {
		t.Error("正常子任务应出现在待领队列")
	}
}