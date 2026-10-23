package store

// tasks_route_path_test.go：tasks.route_path（存储策略留痕，docs/01 §3.4）的落库往返。
//
// 锁定两条不变式：
//  1. CreateTask 写入的 route_path 能被 GetTask / ListTasks / DueWaiting 原样读回
//     （列名、占位符顺序、Scan 顺序任一处漏掉都会读出空串——静默丢字段，
//     任务详情就说不出「走了哪条路」）；
//  2. 未显式赋值的任务读回空串（列 DEFAULT ''，老数据与直接构造路径不受影响）。

import (
	"testing"
	"time"
)

func TestCreateTask_routePath往返(t *testing.T) {
	s := newTestStore(t)
	const want = "115 优先 → 115 提交中 → 115 失败 → 转 qB"

	now := time.Now().UnixMilli()
	task := &Task{
		ID: NewTaskID(), Kind: "qb", Source: "web", Platform: "qb",
		URL:       "magnet:?xt=urn:btih:aabbccddeeff00112233445566778899aabbccdd",
		DedupeKey: DedupeKey("qb", "qb", "magnet:test"),
		RoutePath: want, MaxAttempts: 3, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateTask(task); err != nil {
		t.Fatalf("建任务失败：%v", err)
	}

	got, err := s.GetTask(task.ID)
	if err != nil {
		t.Fatalf("查任务失败：%v", err)
	}
	if got.RoutePath != want {
		t.Errorf("GetTask 读回 route_path=%q，期望 %q", got.RoutePath, want)
	}

	// List 走 scanTaskRows（与 GetTask 的 scanTask 是两份 Scan，必须分别覆盖）。
	list, err := s.ListTasks(TaskFilter{Limit: 10})
	if err != nil {
		t.Fatalf("列表查询失败：%v", err)
	}
	if len(list.Items) != 1 || list.Items[0].RoutePath != want {
		t.Errorf("ListTasks 读回 route_path=%q，期望 %q", list.Items[0].RoutePath, want)
	}

	// 调度器领取同样依赖 scanTaskRows：链路不能在重新入队时丢掉。
	due, err := s.DueWaiting(time.Now().UnixMilli(), 10)
	if err != nil {
		t.Fatalf("取到期任务失败：%v", err)
	}
	if len(due) != 1 || due[0].RoutePath != want {
		t.Errorf("DueWaiting 读回 route_path=%q，期望 %q", due[0].RoutePath, want)
	}

	// 运行期追补链路（failover / 提交成功）走 UpdateTaskFields 白名单。
	const appended = want + " → qB 不可用"
	if err := s.UpdateTaskFields(task.ID, map[string]any{"route_path": appended}, ""); err != nil {
		t.Fatalf("追补 route_path 失败：%v", err)
	}
	got, err = s.GetTask(task.ID)
	if err != nil {
		t.Fatalf("查任务失败：%v", err)
	}
	if got.RoutePath != appended {
		t.Errorf("追补后 route_path=%q，期望 %q", got.RoutePath, appended)
	}
}

func TestCreateTask_routePath默认空串(t *testing.T) {
	s := newTestStore(t)

	task := &Task{
		ID: NewTaskID(), Kind: "download", Source: "web", Platform: "bilibili",
		URL: "https://www.bilibili.com/video/BV1xx411c7mD", DedupeKey: DedupeKey("download", "bilibili", "BV1xx411c7mD"),
		MaxAttempts: 3, CreatedAt: 1, UpdatedAt: 1,
	}
	if err := s.CreateTask(task); err != nil {
		t.Fatalf("建任务失败：%v", err)
	}
	got, err := s.GetTask(task.ID)
	if err != nil {
		t.Fatalf("查任务失败：%v", err)
	}
	if got.RoutePath != "" {
		t.Errorf("未赋值的 route_path 应读回空串，实际 %q", got.RoutePath)
	}
}
