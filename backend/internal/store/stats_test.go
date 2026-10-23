package store

// stats_test.go：主页统计口径（docs/15 §5 T04 验收：跨日/跨周边界、空窗、
// success_rate 分母 0 → nil、收藏快照口径）。

import (
	"testing"
	"time"
)

// mkWaiting 建一个 waiting 任务（CreateTask 强制 waiting，终态用 finish/fail 推）。
func mkWaiting(t *testing.T, s *Store, id, platform, source, sourceRef string) {
	t.Helper()
	if err := s.CreateTask(&Task{
		ID: id, Kind: "download", Source: source, SourceRef: sourceRef, Platform: platform,
		URL:         "https://example.com/" + id,
		DedupeKey:   DedupeKey("download", platform, id),
		MaxAttempts: 3, CreatedAt: 1, UpdatedAt: 1,
	}); err != nil {
		t.Fatalf("建任务失败: %v", err)
	}
}

// finish 把任务推到 done 终态（finishedAtMs 指定完成时刻，测边界用）。
func finish(t *testing.T, s *Store, id string, finishedAtMs int64, downloaded int64) {
	t.Helper()
	if err := s.UpdateTaskFields(id, map[string]any{
		"status": "done", "finished_at": finishedAtMs, "downloaded_size": downloaded,
	}, "waiting"); err != nil {
		t.Fatalf("推 done 失败: %v", err)
	}
}

// fail 把任务推到 failed 终态（同时落平台，E5 拆分要用）。
func fail(t *testing.T, s *Store, id, platform, errCode string, finishedAtMs int64) {
	t.Helper()
	if err := s.UpdateTaskFields(id, map[string]any{
		"status": "failed", "platform": platform, "error_code": errCode, "finished_at": finishedAtMs,
	}, "waiting"); err != nil {
		t.Fatalf("推 failed 失败: %v", err)
	}
}

// localNoon n 天前/后的本地正午（n 可为负）。
func localNoon(days int) time.Time {
	now := time.Now()
	d := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, time.Local)
	return d.AddDate(0, 0, days)
}

func TestSummaryStats_空库全零且成功率为nil(t *testing.T) {
	s := newTestStore(t)
	got, err := s.SummaryStats(time.Now())
	if err != nil {
		t.Fatalf("聚合失败: %v", err)
	}
	if got.TotalDownloads != 0 || got.TodayDownloads != 0 || got.WeekDownloads != 0 {
		t.Fatalf("空库计数应全 0，实际 %+v", got)
	}
	if got.SuccessRate != nil {
		t.Fatalf("分母 0 时 success_rate 必须为 nil（显示「—」不造假），实际 %v", *got.SuccessRate)
	}
	if got.MediaByKind == nil {
		t.Fatal("MediaByKind 应初始化为空 map（前端好遍历）")
	}
}

func TestSummaryStats_今日与周边界(t *testing.T) {
	s := newTestStore(t)

	mkWaiting(t, s, "t-today", "bilibili", "web", "")
	finish(t, s, "t-today", localNoon(0).UnixMilli(), 1024)

	mkWaiting(t, s, "t-old", "bilibili", "web", "")
	finish(t, s, "t-old", localNoon(-30).UnixMilli(), 2048) // 30 天前：必非今日且必非本周

	mkWaiting(t, s, "t-fail", "youtube", "web", "")
	fail(t, s, "t-fail", "youtube", "FETCH_FAILED", localNoon(0).UnixMilli())

	got, err := s.SummaryStats(time.Now())
	if err != nil {
		t.Fatalf("聚合失败: %v", err)
	}
	if got.TotalDownloads != 3 {
		t.Fatalf("总数应 3，实际 %d", got.TotalDownloads)
	}
	if got.TodayDownloads != 1 {
		t.Fatalf("今日应 1（只算今天 12:00 完成的），实际 %d", got.TodayDownloads)
	}
	if got.WeekDownloads != 1 {
		t.Fatalf("本周应 1（30 天前必溢出一周），实际 %d", got.WeekDownloads)
	}
	if got.SuccessRate == nil || *got.SuccessRate < 66.6 || *got.SuccessRate > 66.7 {
		// 2 done（今天 + 30 天前）/ 3 终态（含 1 failed）= 66.67%
		t.Fatalf("成功率应约 66.67，实际 %v", got.SuccessRate)
	}
}

func TestSummaryStats_软删任务不计入(t *testing.T) {
	s := newTestStore(t)
	mkWaiting(t, s, "t-del", "bilibili", "web", "")
	finish(t, s, "t-del", localNoon(0).UnixMilli(), 512)
	if err := s.SoftDelete("t-del"); err != nil {
		t.Fatalf("软删失败: %v", err)
	}
	got, err := s.SummaryStats(time.Now())
	if err != nil {
		t.Fatalf("聚合失败: %v", err)
	}
	if got.TotalDownloads != 0 || got.TodayDownloads != 0 {
		t.Fatalf("软删任务不应计入统计，实际 %+v", got)
	}
}

// TestSummaryStats_收藏下载快照口径 删订阅不影响历史统计：
// favorite_downloads 数的是「任务行当时关联的收藏订阅」，不是「现存订阅的下载」。
func TestSummaryStats_收藏下载快照口径(t *testing.T) {
	s := newTestStore(t)
	sub := &Subscription{
		ID: "sub-fav", Platform: "bilibili", SourceType: "favorite", SourceID: "mid123",
		Mode: "new_only", IntervalMin: 60, CreatedAt: 1, UpdatedAt: 1,
	}
	if err := s.CreateSubscription(sub); err != nil {
		t.Fatalf("建订阅失败: %v", err)
	}
	// CreateSubscription 会生成真实 ID（传入的 ID 会被覆盖），任务要引用它。
	mkWaiting(t, s, "t-fav", "bilibili", "subscription", sub.ID)
	finish(t, s, "t-fav", localNoon(0).UnixMilli(), 4096)

	got, err := s.SummaryStats(time.Now())
	if err != nil {
		t.Fatalf("聚合失败: %v", err)
	}
	if got.FavoriteDownloads != 1 || got.SubDownloads != 1 {
		t.Fatalf("收藏/订阅下载应各 1，实际 fav=%d sub=%d", got.FavoriteDownloads, got.SubDownloads)
	}

	// 删除订阅后：历史任务仍在，快照口径应保持 1（订阅来源计数也不变）。
	if err := s.DeleteSubscription(sub.ID); err != nil {
		t.Fatalf("删订阅失败: %v", err)
	}
	got2, err := s.SummaryStats(time.Now())
	if err != nil {
		t.Fatalf("聚合失败: %v", err)
	}
	if got2.FavoriteDownloads != 1 || got2.SubDownloads != 1 {
		t.Fatalf("删订阅后快照口径应保持 1，实际 fav=%d sub=%d", got2.FavoriteDownloads, got2.SubDownloads)
	}
}

func TestSummaryStats_媒体按kind聚合(t *testing.T) {
	s := newTestStore(t)
	for _, m := range []*MediaFile{
		{ID: "m1", Path: "/data/a.mp4", Name: "a.mp4", Size: 1000, Kind: "video", CreatedAt: 1},
		{ID: "m2", Path: "/data/b.mp4", Name: "b.mp4", Size: 2000, Kind: "video", CreatedAt: 1},
		{ID: "m3", Path: "/data/c.m4a", Name: "c.m4a", Size: 500, Kind: "audio", CreatedAt: 1},
	} {
		if err := s.UpsertMediaFile(m); err != nil {
			t.Fatalf("登记媒体失败: %v", err)
		}
	}
	got, err := s.SummaryStats(time.Now())
	if err != nil {
		t.Fatalf("聚合失败: %v", err)
	}
	if got.MediaBytes != 3500 {
		t.Fatalf("媒体总字节应 3500，实际 %d", got.MediaBytes)
	}
	if got.MediaByKind["video"] != 2 || got.MediaByKind["audio"] != 1 {
		t.Fatalf("按 kind 计数错误: %+v", got.MediaByKind)
	}
}

func TestDailyDoneStats_跨日分组与窗口(t *testing.T) {
	s := newTestStore(t)
	mkWaiting(t, s, "d1", "bilibili", "web", "")
	finish(t, s, "d1", localNoon(0).UnixMilli(), 100)
	mkWaiting(t, s, "d2", "bilibili", "web", "")
	finish(t, s, "d2", localNoon(-2).UnixMilli(), 300)
	mkWaiting(t, s, "d3", "bilibili", "web", "") // waiting 不分组
	mkWaiting(t, s, "d4", "bilibili", "web", "")
	finish(t, s, "d4", localNoon(-10).UnixMilli(), 999) // 窗口外

	// 窗口 [-3 天 00:00, 明日 00:00)：覆盖今天与 2 天前，不含 10 天前。
	from := localNoon(-3).Add(-12 * time.Hour) // 当天 00:00
	to := localNoon(1).Add(-12 * time.Hour)    // 明日 00:00
	byDate, err := s.DailyDoneStats(from, to)
	if err != nil {
		t.Fatalf("趋势聚合失败: %v", err)
	}
	if len(byDate) != 2 {
		t.Fatalf("窗口内应有 2 个有数据的日，实际 %d: %+v", len(byDate), byDate)
	}
	if st := byDate[localNoon(0).Format("2006-01-02")]; st.Count != 1 || st.Bytes != 100 {
		t.Fatalf("今日点错误: %+v", st)
	}
	if st := byDate[localNoon(-2).Format("2006-01-02")]; st.Count != 1 || st.Bytes != 300 {
		t.Fatalf("2 天前点错误: %+v", st)
	}
	if _, ok := byDate[localNoon(-10).Format("2006-01-02")]; ok {
		t.Fatal("窗口外的 10 天前不应出现")
	}
}

func TestDailyDoneStats_空窗返回空map(t *testing.T) {
	s := newTestStore(t)
	byDate, err := s.DailyDoneStats(localNoon(-30), localNoon(0))
	if err != nil {
		t.Fatalf("趋势聚合失败: %v", err)
	}
	if len(byDate) != 0 {
		t.Fatalf("空窗应返回空 map，实际 %+v", byDate)
	}
}

func TestFailureBreakdown_按平台与错误码(t *testing.T) {
	s := newTestStore(t)
	for _, id := range []string{"f1", "f2", "f3"} {
		mkWaiting(t, s, id, "bilibili", "web", "")
	}
	fail(t, s, "f1", "bilibili", "FETCH_FAILED", localNoon(0).UnixMilli())
	fail(t, s, "f2", "bilibili", "FETCH_FAILED", localNoon(-1).UnixMilli())
	fail(t, s, "f3", "youtube", "GEO_BLOCKED", localNoon(0).UnixMilli())

	from := localNoon(-7).Add(-12 * time.Hour) // 7 天前 00:00
	to := localNoon(1).Add(-12 * time.Hour)    // 明日 00:00
	got, err := s.FailureBreakdown(from, to)
	if err != nil {
		t.Fatalf("失败拆分失败: %v", err)
	}
	if len(got.ByPlatform) != 2 || got.ByPlatform[0].Key != "bilibili" || got.ByPlatform[0].Count != 2 {
		t.Fatalf("平台拆分错误: %+v", got.ByPlatform)
	}
	if len(got.ByReason) != 2 || got.ByReason[0].Key != "FETCH_FAILED" || got.ByReason[0].Count != 2 {
		t.Fatalf("错误码拆分错误: %+v", got.ByReason)
	}
}

func TestFailureBreakdown_空窗返回空列表(t *testing.T) {
	s := newTestStore(t)
	got, err := s.FailureBreakdown(localNoon(-7), localNoon(1))
	if err != nil {
		t.Fatalf("失败拆分失败: %v", err)
	}
	if len(got.ByPlatform) != 0 || len(got.ByReason) != 0 {
		t.Fatalf("空窗应返回空列表，实际 %+v / %+v", got.ByPlatform, got.ByReason)
	}
}
