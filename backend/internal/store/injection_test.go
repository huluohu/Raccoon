// injection_test.go：搜索/列表关键词 SQL 注入用例集（docs/08 W5.4）。
//
// 为什么测这个：q=/query= 是唯一把用户原文送进 SQL 的入口
// （ListTasks.title LIKE ? / ListMediaFiles.name LIKE ?）。
// 铁律：关键词必须走绑定参数（?）——注入串不得改变 SQL 结构、
// 不得返回可见范围之外的行（软删行永不出现）、不得破坏表。
//
// 说明：当前数据层没有按用户过滤（单用户自托管设计，docs/01），
// 「不越权」在本语境下指：不返回软删行、不返回非本表数据、
// payload 不得改变结果集语义（如 ' OR 1=1 不得把过滤条件变成永真）。
package store

import (
	"strings"
	"testing"
)

// injectionPayloads 经典注入句式 + 模糊匹配通配符 + 超长/Unicode 边界。
var injectionPayloads = []string{
	`' OR 1=1 --`,
	`' OR '1'='1`,
	`" OR ""="`,
	`'; DROP TABLE tasks; --`,
	`'; DELETE FROM tasks WHERE 1=1; --`,
	`'; UPDATE tasks SET status='done'; --`,
	`' UNION SELECT id, username, password_hash FROM users --`,
	`%' AND 1=1 --`,
	`\'; DROP TABLE media_files; --`,
	`1' ORDER BY 1 --`,
	strings.Repeat("A", 65536),    // 超长串（驱动/解析层缓冲攻击）
	"café' OR 1=1 --",             // Unicode 混排
	"100%' UNION SELECT 1,2,3 --", // 通配符 + 注入
}

// seedInjectionTasks 灌一组含特殊字符标题、不同状态的任务，外加一条软删任务。
// 返回可见任务 ID 集合（软删的不在内）。
func seedInjectionTasks(t *testing.T, s *Store) map[string]bool {
	t.Helper()
	rows := []struct {
		title  string
		status string
	}{
		{"普通视频", "waiting"},
		{"100% 纯享版", "done"},
		{"a_b 下划线标题", "failed"},
		{`back\slash 反斜杠`, "done"},
		{`引号"和'混合`, "waiting"},
		{"café 拉丁标题", "failed"},
	}
	visible := map[string]bool{}
	for i, r := range rows {
		id := NewTaskID()
		if err := s.CreateTask(&Task{
			ID: id, Kind: "download", Source: "web", Platform: "inject",
			URL: "https://example.com/" + id, Title: r.title,
			DedupeKey:   DedupeKey("download", "inject", id),
			MaxAttempts: 3, CreatedAt: int64(i + 1), UpdatedAt: int64(i + 1),
		}); err != nil {
			t.Fatalf("灌任务数据失败: %v", err)
		}
		if r.status != "waiting" {
			if _, err := s.ClaimWaiting(id, r.status); err != nil {
				t.Fatalf("推进任务状态失败: %v", err)
			}
		}
		visible[id] = true
	}
	// 软删行：任何查询都不得返回它（越权红线）。
	delID := NewTaskID()
	if err := s.CreateTask(&Task{
		ID: delID, Kind: "download", Source: "web", Platform: "inject",
		URL: "https://example.com/" + delID, Title: "软删秘密视频",
		DedupeKey:   DedupeKey("download", "inject", delID),
		MaxAttempts: 3, CreatedAt: 999, UpdatedAt: 999,
	}); err != nil {
		t.Fatalf("灌软删任务失败: %v", err)
	}
	if err := s.SoftDelete(delID); err != nil {
		t.Fatalf("软删失败: %v", err)
	}
	return visible
}

// truncate 长 payload 截断用于错误信息（避免 64KB 刷屏）。
func truncate(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}

// TestListTasks_注入payload不越权 全部 payload 过一遍真实搜索：
// 不报错、结果只含播种的可见行、软删行永不出现。
func TestListTasks_注入payload不越权(t *testing.T) {
	s := newTestStore(t)
	visible := seedInjectionTasks(t, s)

	for _, p := range injectionPayloads {
		res, err := s.ListTasks(TaskFilter{Q: p, Limit: 100})
		if err != nil {
			// 实测发现：关键词超过约 49998 字符触发 SQLite LIKE 模式上限
			// （SQLITE_MAX_LIKE_PATTERN_LENGTH=50000，含两侧 %），store 直接
			// 返错 → 上游 500。期望（用户规格）：超长串不报错（截断或校验后查）。
			t.Errorf("payload %q 不应导致查询错误: %v", truncate(p), err)
			continue
		}
		for _, it := range res.Items {
			if !visible[it.ID] {
				t.Errorf("payload %q 返回了播种范围外的行 %q（SQL 结构可能被篡改）", truncate(p), it.ID)
			}
			if it.Title == "软删秘密视频" {
				t.Errorf("payload %q 返回了软删行（越权）", truncate(p))
			}
		}
	}

	// 表与数据完好：全量查询仍是播种的可见行数，状态列没被 UPDATE payload 篡改。
	res, err := s.ListTasks(TaskFilter{Limit: 100})
	if err != nil {
		t.Fatalf("payload 之后全量查询失败: %v", err)
	}
	if len(res.Items) != len(visible) {
		t.Errorf("payload 之后可见任务应仍是 %d 条，实际 %d（表数据被破坏？）", len(visible), len(res.Items))
	}
	for _, it := range res.Items {
		if it.Status != "waiting" && it.Status != "done" && it.Status != "failed" {
			t.Errorf("任务 %q 状态被篡改为 %q", it.ID, it.Status)
		}
	}
}

// TestListTasks_永假注入串零命中 ' OR 1=1 -- 若真的突破了字符串字面量，
// LIKE 条件会变成永真，把全部 done 行吐出来。断言 0 命中即证明没有突破。
func TestListTasks_永假注入串零命中(t *testing.T) {
	s := newTestStore(t)
	seedInjectionTasks(t, s)

	res, err := s.ListTasks(TaskFilter{Q: `' OR 1=1 --`, Limit: 100})
	if err != nil {
		t.Fatalf("查询不应报错: %v", err)
	}
	if len(res.Items) != 0 {
		t.Errorf("永假注入串应 0 命中（参数化生效），实际 %d 行", len(res.Items))
	}
}

// TestListTasks_组合条件下payload仍不越权 payload 与 status/platform 过滤叠加：
// 若注入突破字面量，status 条件会被 OR 短路，done 之外的行也会漏出来。
// 播种集里有 done 行，所以这条是真断言（能区分「穿透」与「未穿透」）。
func TestListTasks_组合条件下payload仍不越权(t *testing.T) {
	s := newTestStore(t)
	seedInjectionTasks(t, s)

	res, err := s.ListTasks(TaskFilter{
		Q: `' OR 1=1 --`, Statuses: []string{"done"}, Platform: "inject", Limit: 100,
	})
	if err != nil {
		t.Fatalf("查询不应报错: %v", err)
	}
	if len(res.Items) != 0 {
		t.Errorf("永假注入串 + status=done 应 0 命中，实际 %d 行（注入可能穿透了 AND 结构）", len(res.Items))
	}
}

// TestListTasks_LIKE通配符转义失效（差异记录，非安全断言）
//
// 实测发现（探针结论）：escapeLike 转了 %/_/\，但 SQL 没有 ESCAPE 子句，
// SQLite 默认反斜杠不是转义符——转义反而制造了「通配符 + 字面反斜杠」模式：
//   - 搜 "%" 命中「含反斜杠的标题」，而不是「含 % 的标题」；
//   - 搜 "100%" / "a_b" / "back\slash" 全部 0 命中（字面量搜索坏掉）。
//
// 这不是注入（仍走绑定参数、仍限本表、无跨用户面），是搜索功能缺陷。
// 修复前本用例只锁安全不变式（不报错、结果不越界），不锁具体命中集。
func TestListTasks_LIKE通配符转义失效(t *testing.T) {
	s := newTestStore(t)
	visible := seedInjectionTasks(t, s)

	for _, q := range []string{"%", "_", "100%", "a_b", `back\slash`} {
		res, err := s.ListTasks(TaskFilter{Q: q, Limit: 100})
		if err != nil {
			t.Errorf("q=%q 不应报错: %v", q, err)
			continue
		}
		for _, it := range res.Items {
			if !visible[it.ID] {
				t.Errorf("q=%q 返回播种范围外的行 %q", q, it.ID)
			}
		}
		t.Logf("q=%q 命中 %d 行（escapeLike 缺 ESCAPE 子句，见报告）", q, len(res.Items))
	}
}

// ---- 媒体库（files.go：ListMediaFiles.name LIKE ?）----

// seedInjectionMedia 灌一组特殊字符文件名的媒体条目，返回文件名集合。
func seedInjectionMedia(t *testing.T, s *Store) map[string]bool {
	t.Helper()
	names := []string{
		"普通视频.mp4",
		"100% 纯享版.mp4",
		"a_b 电影.mkv",
		`back\slash.mp4`,
		"café 视频.webm",
	}
	for i, name := range names {
		if err := s.UpsertMediaFile(&MediaFile{
			Path:     "/data/downloads/inject/" + name,
			Name:     name,
			Size:     int64(i+1) * 1024,
			Kind:     "video",
			Platform: "inject",
		}); err != nil {
			t.Fatalf("灌媒体数据失败: %v", err)
		}
	}
	set := map[string]bool{}
	for _, n := range names {
		set[n] = true
	}
	return set
}

// TestListMediaFiles_注入payload不越权 与任务搜索同理：不报错、不越界、表不坏。
func TestListMediaFiles_注入payload不越权(t *testing.T) {
	s := newTestStore(t)
	seeded := seedInjectionMedia(t, s)

	for _, p := range injectionPayloads {
		items, _, err := s.ListMediaFiles(FileFilter{Query: p, Limit: 100})
		if err != nil {
			// 同任务搜索：超长关键词触发 SQLite LIKE 模式上限（约 49998 字符）。
			t.Errorf("payload %q 不应导致查询错误: %v", truncate(p), err)
			continue
		}
		for _, it := range items {
			if !seeded[it.Name] {
				t.Errorf("payload %q 返回了播种范围外的媒体 %q", truncate(p), it.Name)
			}
		}
	}

	// DROP/DELETE payload 之后表完好。
	n, err := s.CountMediaFiles()
	if err != nil {
		t.Fatalf("payload 之后媒体计数失败: %v", err)
	}
	if n != len(seeded) {
		t.Errorf("payload 之后媒体应仍是 %d 条，实际 %d（表数据被破坏？）", len(seeded), n)
	}
}

// TestListMediaFiles_查询通配符未转义（差异记录，非安全断言）
// files.go 把 Query 原样拼进 LIKE 模式（不做 escapeLike）："%" 会作为
// 通配符返回全部行——与 ListTasks（试图转义但转义失效）行为不一致。
// 同样不是注入（参数化成立），单用户系统无越权面；修复前只锁安全不变式。
func TestListMediaFiles_查询通配符未转义(t *testing.T) {
	s := newTestStore(t)
	seeded := seedInjectionMedia(t, s)

	for _, q := range []string{"%", "_", "100%"} {
		items, _, err := s.ListMediaFiles(FileFilter{Query: q, Limit: 100})
		if err != nil {
			t.Errorf("q=%q 不应报错: %v", q, err)
			continue
		}
		for _, it := range items {
			if !seeded[it.Name] {
				t.Errorf("q=%q 返回播种范围外的媒体 %q", q, it.Name)
			}
		}
		t.Logf("q=%q 命中 %d 行（媒体库搜索未转义通配符，见报告）", q, len(items))
	}
}
