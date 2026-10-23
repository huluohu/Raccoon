package store

// stats.go：主页仪表盘聚合查询（docs/14 §2.1 / docs/15 §3.3）。
//
// 口径纪律（docs/15 §6.4）：
//   - 「今日/本周」一律用**进程本地时区**（time.Local）的自然日/自然周（周一起），
//     边界在 Go 侧算好，SQL 只做 BETWEEN——不依赖 SQLite 时区函数，规避容器 TZ 坑；
//   - success_rate 分母为 0 返回 nil（前端显示「—」，不显示 0%/100% 造假）；
//   - favorite_downloads 是**快照口径**：订阅删除后历史任务仍计入（删订阅不影响历史统计）。

import "time"

// SummaryStats 主页指标聚合（/system/stats/summary）。
type SummaryStats struct {
	UptimeSec         int64          `json:"uptime_sec"`      // 由 handler 填（进程启动时长）
	TotalDownloads    int            `json:"total_downloads"` // 全部非软删任务数
	TodayDownloads    int            `json:"today_downloads"` // done 且 finished_at ∈ 本地今日
	WeekDownloads     int            `json:"week_downloads"`  // done 且 finished_at ∈ 本地本周（周一起）
	SuccessRate       *float64       `json:"success_rate"`    // done/(done+failed)；分母 0 = nil
	MediaBytes        int64          `json:"media_bytes"`
	MediaByKind       map[string]int `json:"media_by_kind"`
	SubDownloads      int            `json:"sub_downloads"`      // source='subscription' 的 done
	FavoriteDownloads int            `json:"favorite_downloads"` // 关联订阅 source_type='favorite' 的 done
	LiveRecording     int            `json:"live_recording"`     // live_sources.status='recording'
}

// DailyDoneStat 趋势单点（date = 本地时区 YYYY-MM-DD）。
type DailyDoneStat struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
	Bytes int64  `json:"bytes"`
}

// NamedCount 命名计数（E5 失败拆分）。
type NamedCount struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// FailureBreakdown E5：时间窗内 failed 按平台/原因聚合。
type FailureBreakdown struct {
	ByPlatform []NamedCount `json:"by_platform"`
	ByReason   []NamedCount `json:"by_reason"`
}

// dayBoundsMs 本地时区当日 [00:00, 次日 00:00) 的 Unix 毫秒边界。
func dayBoundsMs(now time.Time) (start, end int64) {
	y, m, d := now.Date()
	startOfDay := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	return startOfDay.UnixMilli(), startOfDay.AddDate(0, 0, 1).UnixMilli()
}

// weekBoundsMs 本地时区本周（周一起）[00:00, 下周一 00:00) 的 Unix 毫秒边界。
func weekBoundsMs(now time.Time) (start, end int64) {
	y, m, d := now.Date()
	startOfDay := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	// time.Weekday: Sunday=0 ... Saturday=6；周一为界 → 回溯 (wd+6)%7 天。
	back := (int(startOfDay.Weekday()) + 6) % 7
	startOfWeek := startOfDay.AddDate(0, 0, -back)
	return startOfWeek.UnixMilli(), startOfWeek.AddDate(0, 0, 7).UnixMilli()
}

// SummaryStats 聚合主页全部指标（一次查询拿齐，避免主页 N 连发）。
func (s *Store) SummaryStats(now time.Time) (*SummaryStats, error) {
	// 五个 kind 键预置为 0：前端图例按固定键渲染，缺键会显示空白而不是「0」。
	out := &SummaryStats{MediaByKind: map[string]int{
		"video": 0, "audio": 0, "image": 0, "subtitle": 0, "other": 0,
	}}

	// 任务侧：总数 / done / failed / 今日 / 本周 / 订阅来源。
	todayStart, todayEnd := dayBoundsMs(now)
	weekStart, weekEnd := weekBoundsMs(now)
	row := s.RO.QueryRow(`SELECT
		COUNT(*)                                                     AS total,
		COALESCE(SUM(CASE WHEN status='done' THEN 1 ELSE 0 END), 0)   AS done_n,
		COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END), 0) AS failed_n,
		COALESCE(SUM(CASE WHEN status='done' AND finished_at >= ? AND finished_at < ? THEN 1 ELSE 0 END), 0) AS today_n,
		COALESCE(SUM(CASE WHEN status='done' AND finished_at >= ? AND finished_at < ? THEN 1 ELSE 0 END), 0) AS week_n,
		COALESCE(SUM(CASE WHEN status='done' AND source='subscription' THEN 1 ELSE 0 END), 0) AS sub_n
		FROM tasks WHERE deleted_at IS NULL`,
		todayStart, todayEnd, weekStart, weekEnd)
	var doneN, failedN int
	if err := row.Scan(&out.TotalDownloads, &doneN, &failedN,
		&out.TodayDownloads, &out.WeekDownloads, &out.SubDownloads); err != nil {
		return nil, err
	}
	// success_rate：分母 0 → nil（诚实原则，不造假 100%）。
	if doneN+failedN > 0 {
		rate := float64(doneN) * 100 / float64(doneN+failedN)
		out.SuccessRate = &rate
	}

	// 收藏下载：在册收藏订阅走 JOIN，已删订阅走 sub_favorite 快照标记（快照口径）。
	if err := s.RO.QueryRow(`SELECT COUNT(*) FROM tasks t
		WHERE t.status='done' AND t.deleted_at IS NULL AND t.source='subscription'
		  AND (t.sub_favorite = 1
		       OR EXISTS (SELECT 1 FROM subscriptions sb
		                  WHERE sb.id = t.source_ref AND sb.source_type='favorite'))`,
	).Scan(&out.FavoriteDownloads); err != nil {
		return nil, err
	}

	// 媒体侧：总字节 + 按 kind 计数。
	mediaRows, err := s.RO.Query(`SELECT kind, COUNT(*), COALESCE(SUM(size),0)
		FROM media_files GROUP BY kind`)
	if err != nil {
		return nil, err
	}
	defer mediaRows.Close()
	for mediaRows.Next() {
		var kind string
		var n int
		var bytes int64
		if err := mediaRows.Scan(&kind, &n, &bytes); err != nil {
			return nil, err
		}
		out.MediaByKind[kind] = n
		out.MediaBytes += bytes
	}
	if err := mediaRows.Err(); err != nil {
		return nil, err
	}

	// 直播录制中源数。
	if err := s.RO.QueryRow(`SELECT COUNT(*) FROM live_sources WHERE status='recording'`).Scan(&out.LiveRecording); err != nil {
		return nil, err
	}
	return out, nil
}

// DailyDoneStats 下载趋势（按日聚合 done；无数据日由调用方补 0）。
// 返回 map[日期]点，长度不保证等于 days——补 0 在 handler 做（保持 store 层单纯）。
func (s *Store) DailyDoneStats(from, to time.Time) (map[string]DailyDoneStat, error) {
	rows, err := s.RO.Query(`SELECT date(finished_at/1000, 'unixepoch', 'localtime') AS d,
		COUNT(*), COALESCE(SUM(downloaded_size),0)
		FROM tasks
		WHERE status='done' AND deleted_at IS NULL
		  AND finished_at >= ? AND finished_at < ?
		GROUP BY d`,
		from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]DailyDoneStat{}
	for rows.Next() {
		var d DailyDoneStat
		if err := rows.Scan(&d.Date, &d.Count, &d.Bytes); err != nil {
			return nil, err
		}
		out[d.Date] = d
	}
	return out, rows.Err()
}

// FailureBreakdown 时间窗内 failed 任务按平台/错误码聚合（E5，各上限 10）。
func (s *Store) FailureBreakdown(from, to time.Time) (*FailureBreakdown, error) {
	out := &FailureBreakdown{ByPlatform: []NamedCount{}, ByReason: []NamedCount{}}
	for _, spec := range []struct {
		col  string
		dest *[]NamedCount
	}{
		{"platform", &out.ByPlatform},
		{"error_code", &out.ByReason},
	} {
		rows, err := s.RO.Query(`SELECT `+spec.col+` AS k, COUNT(*) AS n
			FROM tasks WHERE status='failed' AND deleted_at IS NULL
			  AND finished_at >= ? AND finished_at < ?
			  AND `+spec.col+` <> ''
			GROUP BY k ORDER BY n DESC LIMIT 10`,
			from.UnixMilli(), to.UnixMilli())
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var nc NamedCount
			if err := rows.Scan(&nc.Key, &nc.Count); err != nil {
				rows.Close()
				return nil, err
			}
			*spec.dest = append(*spec.dest, nc)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}
