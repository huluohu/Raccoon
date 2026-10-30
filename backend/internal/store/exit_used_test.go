package store

// exit_used_test.go：任务「本轮实际使用的出口」字段的存取。
//
// 与已有的 proxy 列分工（2026-10-30）：
//   - proxy     = 用户**配置**的出口
//   - exit_used = 本轮**实际使用**的出口（空串=直连）
//
// 两者不能混：出口决策是「显式 > 任务级 > 全局 net.proxy > 直连」，
// 任务没配 proxy 时全局代理本来就在用——只凭 proxy 判断「是否在走代理」
// 会得出错误结论（重试日志因此误报「已自动切换代理」）。

import (
	"path/filepath"
	"testing"
)

// TestExitUsed_存取往返 写进去再读出来必须是同一个值。
func TestExitUsed_存取往返(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	defer s.Close()

	// 建任务时还没有「本轮实际出口」（还没跑）→ exit_used 为空串，
	// 由运行期（解析/下载阶段）写入。这正是「配置」与「实况」分开的含义。
	task := &Task{
		ID: NewTaskID(), Kind: "download", Source: "web", Platform: "bilibili",
		URL: "https://example.com/v.mp4", Status: "downloading",
		Proxy: "http://127.0.0.1:7890",
	}
	if err := s.CreateTask(task); err != nil {
		t.Fatalf("建任务失败: %v", err)
	}
	// CreateTask 固定落 status='waiting'，CAS 的期望状态要与之一致。
	if err := s.UpdateTaskFields(task.ID, map[string]any{"exit_used": "http://127.0.0.1:7890"}, "waiting"); err != nil {
		t.Fatalf("写 exit_used 失败: %v", err)
	}

	got, err := s.GetTask(task.ID)
	if err != nil {
		t.Fatalf("取任务失败: %v", err)
	}
	if got.ExitUsed != "http://127.0.0.1:7890" {
		t.Errorf("exit_used 应原样往返，实际 %q", got.ExitUsed)
	}
	if got.Proxy != "http://127.0.0.1:7890" {
		t.Errorf("proxy 应原样往返，实际 %q", got.Proxy)
	}

	// 列表接口也要带上（任务列表页与详情页都看得到）。
	res, err := s.ListTasks(TaskFilter{Limit: 10})
	if err != nil {
		t.Fatalf("列任务失败: %v", err)
	}
	found := false
	for _, it := range res.Items {
		if it.ID == task.ID {
			found = true
			if it.ExitUsed != "http://127.0.0.1:7890" {
				t.Errorf("列表里的 exit_used 应为代理地址，实际 %q", it.ExitUsed)
			}
		}
	}
	if !found {
		t.Error("列表里应能查到该任务")
	}
}

// TestExitUsed_直连时为空串 直连是「没有代理」，记空串。
func TestExitUsed_直连时为空串(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	defer s.Close()

	task := &Task{
		ID: NewTaskID(), Kind: "download", Source: "web", Platform: "bilibili",
		URL: "https://example.com/v2.mp4", Status: "waiting",
	}
	if err := s.CreateTask(task); err != nil {
		t.Fatalf("建任务失败: %v", err)
	}
	got, err := s.GetTask(task.ID)
	if err != nil {
		t.Fatalf("取任务失败: %v", err)
	}
	if got.ExitUsed != "" {
		t.Errorf("直连时 exit_used 应为空串，实际 %q", got.ExitUsed)
	}
}

// TestExitUsed_可更新 重试后要能把本轮的实况覆盖掉。
func TestExitUsed_可更新(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	defer s.Close()

	task := &Task{
		ID: NewTaskID(), Kind: "download", Source: "web", Platform: "bilibili",
		URL: "https://example.com/v3.mp4", Status: "waiting",
	}
	if err := s.CreateTask(task); err != nil {
		t.Fatalf("建任务失败: %v", err)
	}
	if err := s.UpdateTaskFields(task.ID, map[string]any{"exit_used": "http://proxy:1080"}, "waiting"); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	got, err := s.GetTask(task.ID)
	if err != nil {
		t.Fatalf("取任务失败: %v", err)
	}
	if got.ExitUsed != "http://proxy:1080" {
		t.Errorf("更新后 exit_used 应为 http://proxy:1080，实际 %q", got.ExitUsed)
	}
}
