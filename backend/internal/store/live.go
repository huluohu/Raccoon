package store

// live.go：直播源与录制记录数据层（docs/03 §4.5 live_sources / live_recordings 表）。

import (
	"database/sql"
	"errors"
	"time"
)

// LiveSource 直播源。
type LiveSource struct {
	ID               string `json:"id"`
	Platform         string `json:"platform"`
	RoomID           string `json:"room_id"`
	RoomURL          string `json:"room_url"`
	Title            string `json:"title,omitempty"`
	CredentialID     string `json:"credential_id,omitempty"`
	Enabled          bool   `json:"enabled"`
	Quality          string `json:"quality,omitempty"`
	RecordPlan       string `json:"record_plan"`
	ScheduleJSON     string `json:"schedule_json,omitempty"`
	SegmentMin       int    `json:"segment_min"`
	CheckIntervalSec int    `json:"check_interval_sec"`
	Status           string `json:"status"`
	LastCheckedAt    *int64 `json:"last_checked_at,omitempty"`
	LastLiveAt       *int64 `json:"last_live_at,omitempty"`
	ErrorNote        string `json:"error_note,omitempty"`
	CreatedAt        int64  `json:"created_at"`
	UpdatedAt        int64  `json:"updated_at"`
	RecordingCount   int    `json:"recording_count"`
}

// LiveRecording 一次录制。
type LiveRecording struct {
	ID        string `json:"id"`
	SourceID  string `json:"source_id"`
	TaskID    string `json:"task_id,omitempty"`
	StartedAt int64  `json:"started_at"`
	EndedAt   *int64 `json:"ended_at,omitempty"`
	Segments  int    `json:"segments"`
	Size      int64  `json:"size"`
	Status    string `json:"status"`
	ErrorNote string `json:"error_note,omitempty"`
}

// ErrNoSuchLiveSource 直播源不存在。
var ErrNoSuchLiveSource = errors.New("live source not found")

const liveSelect = `SELECT id, platform, room_id, room_url, title, credential_id, enabled, quality, record_plan,
	schedule_json, segment_min, check_interval_sec, status, last_checked_at, last_live_at, error_note,
	created_at, updated_at FROM live_sources`

// ListLiveSources 列出直播源（附录制次数）。
func (s *Store) ListLiveSources() ([]*LiveSource, error) {
	rows, err := s.RO.Query(liveSelect + ` ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*LiveSource{}
	for rows.Next() {
		ls, err := scanLiveSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ls)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, ls := range out {
		var n int
		if err := s.RO.QueryRow(`SELECT COUNT(*) FROM live_recordings WHERE source_id=?`, ls.ID).Scan(&n); err == nil {
			ls.RecordingCount = n
		}
	}
	return out, nil
}

// GetLiveSource 取直播源。
func (s *Store) GetLiveSource(id string) (*LiveSource, error) {
	ls, err := scanLiveSource(s.RO.QueryRow(liveSelect+` WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoSuchLiveSource
	}
	return ls, err
}

// CreateLiveSource 新建直播源。
func (s *Store) CreateLiveSource(ls *LiveSource) error {
	now := time.Now().UnixMilli()
	ls.ID = newID("live")
	ls.CreatedAt = now
	ls.UpdatedAt = now
	if ls.RecordPlan == "" {
		ls.RecordPlan = "manual"
	}
	if ls.SegmentMin <= 0 {
		ls.SegmentMin = 60
	}
	if ls.CheckIntervalSec <= 0 {
		ls.CheckIntervalSec = 60
	}
	ls.Status = "offline"
	_, err := s.RW.Exec(`INSERT INTO live_sources (id, platform, room_id, room_url, title, credential_id, enabled,
		quality, record_plan, schedule_json, segment_min, check_interval_sec, status, last_checked_at, last_live_at,
		error_note, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		ls.ID, ls.Platform, ls.RoomID, ls.RoomURL, ls.Title, nullIfEmpty(ls.CredentialID), boolInt(ls.Enabled),
		ls.Quality, ls.RecordPlan, ls.ScheduleJSON, ls.SegmentMin, ls.CheckIntervalSec, ls.Status,
		ls.LastCheckedAt, ls.LastLiveAt, ls.ErrorNote, ls.CreatedAt, ls.UpdatedAt)
	return err
}

// UpdateLiveSource 更新直播源规则。
func (s *Store) UpdateLiveSource(ls *LiveSource) error {
	ls.UpdatedAt = time.Now().UnixMilli()
	res, err := s.RW.Exec(`UPDATE live_sources SET title=?, credential_id=?, enabled=?, quality=?, record_plan=?,
		schedule_json=?, segment_min=?, check_interval_sec=?, status=?, last_checked_at=?, last_live_at=?,
		error_note=?, updated_at=? WHERE id=?`,
		ls.Title, nullIfEmpty(ls.CredentialID), boolInt(ls.Enabled), ls.Quality, ls.RecordPlan, ls.ScheduleJSON,
		ls.SegmentMin, ls.CheckIntervalSec, ls.Status, ls.LastCheckedAt, ls.LastLiveAt, ls.ErrorNote, ls.UpdatedAt, ls.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNoSuchLiveSource
	}
	return nil
}

// MarkLiveSourceStatus 更新直播源状态（离线检测/开播检测结果）。
func (s *Store) MarkLiveSourceStatus(id, status, errorNote string, live bool) error {
	now := time.Now().UnixMilli()
	var lastLive any
	if live {
		lastLive = now
	}
	res, err := s.RW.Exec(`UPDATE live_sources SET status=?, error_note=?, last_checked_at=?, last_live_at=COALESCE(?, last_live_at), updated_at=? WHERE id=?`,
		status, errorNote, now, lastLive, now, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNoSuchLiveSource
	}
	return nil
}

// DeleteLiveSource 删除直播源（录制记录随之删除）。
func (s *Store) DeleteLiveSource(id string) error {
	res, err := s.RW.Exec(`DELETE FROM live_sources WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNoSuchLiveSource
	}
	return nil
}

// CreateLiveRecording 记一次录制开始。
func (s *Store) CreateLiveRecording(sourceID, taskID string) (*LiveRecording, error) {
	now := time.Now().UnixMilli()
	r := &LiveRecording{ID: newID("rec"), SourceID: sourceID, TaskID: taskID, StartedAt: now, Status: "recording"}
	_, err := s.RW.Exec(`INSERT INTO live_recordings (id, source_id, task_id, started_at, ended_at, segments, size, status, error_note)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		r.ID, sourceID, nullIfEmpty(taskID), now, nil, 0, 0, "recording", "")
	if err != nil {
		return nil, err
	}
	return r, nil
}

// FinishLiveRecording 结束录制。
func (s *Store) FinishLiveRecording(id, status, errorNote string, segments int, size int64) error {
	now := time.Now().UnixMilli()
	_, err := s.RW.Exec(`UPDATE live_recordings SET ended_at=?, status=?, error_note=?, segments=?, size=? WHERE id=?`,
		now, status, errorNote, segments, size, id)
	return err
}

// GetLiveRecordingByTask 按任务 ID 取录制记录（worker 终态回写用）。
// 找不到返回 nil（普通下载任务没有录制记录，不是错误）。
func (s *Store) GetLiveRecordingByTask(taskID string) (*LiveRecording, error) {
	var r LiveRecording
	var errorNote sql.NullString
	var endedAt sql.NullInt64
	err := s.RO.QueryRow(`SELECT id, source_id, task_id, started_at, ended_at, segments, size, status, error_note
		FROM live_recordings WHERE task_id=? ORDER BY started_at DESC LIMIT 1`, taskID).
		Scan(&r.ID, &r.SourceID, &r.TaskID, &r.StartedAt, &endedAt, &r.Segments, &r.Size, &r.Status, &errorNote)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.ErrorNote = errorNote.String
	if endedAt.Valid {
		r.EndedAt = &endedAt.Int64
	}
	return &r, nil
}

// ListLiveRecordings 列出演制历史。
func (s *Store) ListLiveRecordings(sourceID string, limit int) ([]*LiveRecording, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.RO.Query(`SELECT id, source_id, task_id, started_at, ended_at, segments, size, status, error_note
		FROM live_recordings WHERE source_id=? ORDER BY started_at DESC LIMIT ?`, sourceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*LiveRecording{}
	for rows.Next() {
		var r LiveRecording
		var taskID, errorNote sql.NullString
		var endedAt sql.NullInt64
		if err := rows.Scan(&r.ID, &r.SourceID, &taskID, &r.StartedAt, &endedAt, &r.Segments, &r.Size, &r.Status, &errorNote); err != nil {
			return nil, err
		}
		r.TaskID = taskID.String
		r.ErrorNote = errorNote.String
		if endedAt.Valid {
			r.EndedAt = &endedAt.Int64
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

func scanLiveSource(sc rowScanner) (*LiveSource, error) {
	var ls LiveSource
	var title, credID, quality, schedJSON, errorNote sql.NullString
	var enabled int64
	var lastChecked, lastLive sql.NullInt64
	err := sc.Scan(&ls.ID, &ls.Platform, &ls.RoomID, &ls.RoomURL, &title, &credID, &enabled, &quality, &ls.RecordPlan,
		&schedJSON, &ls.SegmentMin, &ls.CheckIntervalSec, &ls.Status, &lastChecked, &lastLive, &errorNote,
		&ls.CreatedAt, &ls.UpdatedAt)
	if err != nil {
		return nil, err
	}
	ls.Title = title.String
	ls.CredentialID = credID.String
	ls.Quality = quality.String
	ls.ScheduleJSON = schedJSON.String
	ls.ErrorNote = errorNote.String
	ls.Enabled = enabled != 0
	if lastChecked.Valid {
		ls.LastCheckedAt = &lastChecked.Int64
	}
	if lastLive.Valid {
		ls.LastLiveAt = &lastLive.Int64
	}
	return &ls, nil
}
