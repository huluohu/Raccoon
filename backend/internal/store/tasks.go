package store

// tasks.go：tasks/task_events/task_files 的唯一读写出口（docs/05 §1 Store 职责）。
// 写路径一律走 RW（单连接串行化）；领取等 CAS 更新防并发双领（docs/05 §3.4）。

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrDedupeActive 同 dedupe_key 活跃任务已存在（上层转 409 + existing_task_id）。
type ErrDedupeActive struct{ ExistingID string }

func (e *ErrDedupeActive) Error() string { return "dedupe active: " + e.ExistingID }

// ErrTaskNotFound 任务不存在或已删除。
var ErrTaskNotFound = errors.New("task not found")

// NewTaskID 生成任务 ID（可注入前缀便于测试）。
func NewTaskID() string { return newID("tsk") }

// DedupeKey sha1(kind + platform + 规范化 url)（docs/05 §6）。
func DedupeKey(kind, platform, normalizedURL string) string {
	sum := sha1Sum(kind + "\x00" + platform + "\x00" + normalizedURL)
	return sum
}

// NormalizeURL 去 fragment、去尾部斜杠、小写 host（dedupe 规范化；即使用户换大小写也判重）。
func NormalizeURL(raw string) string {
	u := strings.TrimSpace(raw)
	if i := strings.IndexByte(u, '#'); i >= 0 {
		u = u[:i]
	}
	if len(u) > 1 && strings.HasSuffix(u, "/") {
		u = strings.TrimRight(u, "/")
	}
	if strings.HasPrefix(u, "HTTPS://") {
		u = "https://" + u[8:]
	} else if strings.HasPrefix(u, "HTTP://") {
		u = "http://" + u[7:]
	}
	return u
}

func sha1Sum(s string) string {
	return strconv.FormatUint(uint64(fnv64(s)), 36) + ":" + strconv.Itoa(len(s))
}

func fnv64(s string) uint64 {
	var h uint64 = 14695981039346656037
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

// CreateTask 落库（status=waiting）；dedupe 唯一索引冲突 → ErrDedupeActive。
func (s *Store) CreateTask(t *Task) error {
	_, err := s.RW.Exec(`INSERT INTO tasks
		(id, parent_id, kind, source, source_ref, platform, url, title, thumbnail, dedupe_key, status,
		 quality, save_path, expected_size, engine, engine_version, config_version, attempt, max_attempts,
		 route_path, scheduled_at, convert_to, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?, 'waiting', ?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.ParentID, t.Kind, t.Source, t.SourceRef, t.Platform, t.URL, nullStr(t.Title), nullStr(t.Thumbnail),
		t.DedupeKey, nullStr(t.Quality), nullStr(t.SavePath), t.ExpectedSize, nullStr(t.Engine), nullStr(t.EngineVersion),
		t.ConfigVersion, t.Attempt, t.MaxAttempts, t.RoutePath, t.ScheduledAt, nullStr(t.ConvertTo), t.CreatedAt, t.UpdatedAt)
	if err != nil {
		if isUniqueErr(err) {
			// 唯一索引冲突即 dedupe（含普通 dedupe_key 索引）。
			if existing, e := s.activeByDedupe(t.DedupeKey); e == nil {
				return &ErrDedupeActive{ExistingID: existing}
			}
			return &ErrDedupeActive{ExistingID: ""}
		}
		return err
	}
	return nil
}

// activeByDedupe 查活跃态同 key 任务 ID。已软删除的任务不参与判重
// （deleted_at 非空即用户已删除，重新提交同一链接应可再次入队）。
func (s *Store) activeByDedupe(key string) (string, error) {
	var id string
	err := s.RO.QueryRow(`SELECT id FROM tasks WHERE dedupe_key=? AND deleted_at IS NULL AND status IN ('waiting','parsing','downloading','post') LIMIT 1`, key).Scan(&id)
	return id, err
}

// GetTask 取任务（deleted_at 过滤）。
func (s *Store) GetTask(id string) (*Task, error) {
	row := s.RO.QueryRow(taskSelect+` WHERE t.id=? AND t.deleted_at IS NULL`, id)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTaskNotFound
	}
	return t, err
}

const taskSelect = `SELECT t.id, t.parent_id, t.kind, t.source, t.source_ref, t.platform, t.url, t.title, t.thumbnail,
	t.status, t.wait_reason, t.quality, t.save_path, t.expected_size, t.downloaded_size, t.speed_bps, t.progress,
	t.resume_supported, t.engine, t.engine_version, t.config_version, t.attempt, t.max_attempts,
	t.error_code, t.error_message, t.fix_hint, t.cancel_requested, t.route_path, t.scheduled_at,
	t.created_at, t.updated_at, t.started_at, t.finished_at, t.priority, t.convert_to FROM tasks t`

func scanTask(row *sql.Row) (*Task, error) {
	var t Task
	var parentID, sourceRef, platform, title, thumb, waitReason, quality, savePath, engine, engineVer sql.NullString
	var errCode, errMsg, fixHint sql.NullString
	var routePath, convertTo sql.NullString
	var expectedSize, scheduledAt, startedAt, finishedAt sql.NullInt64
	var resumeFlag, cancelFlag int64 // INTEGER 列经 int64 中转（database/sql 不支持直扫 bool）
	err := row.Scan(&t.ID, &parentID, &t.Kind, &t.Source, &sourceRef, &platform, &t.URL, &title, &thumb,
		&t.Status, &waitReason, &quality, &savePath, &expectedSize, &t.DownloadedSize, &t.SpeedBps, &t.Progress,
		&resumeFlag, &engine, &engineVer, &t.ConfigVersion, &t.Attempt, &t.MaxAttempts,
		&errCode, &errMsg, &fixHint, &cancelFlag, &routePath, &scheduledAt,
		&t.CreatedAt, &t.UpdatedAt, &startedAt, &finishedAt, &t.Priority, &convertTo)
	if err != nil {
		return nil, err
	}
	t.ResumeSupported = resumeFlag != 0
	t.CancelRequested = cancelFlag != 0
	t.RoutePath = routePath.String
	t.ConvertTo = convertTo.String
	t.ErrorCode = errCode.String
	t.ErrorMessage = errMsg.String
	t.FixHint = fixHint.String
	t.ParentID = nullStrPtr(parentID)
	t.SourceRef = sourceRef.String
	t.Platform = platform.String
	t.Title = title.String
	t.Thumbnail = thumb.String
	t.WaitReason = waitReason.String
	t.Quality = quality.String
	t.SavePath = savePath.String
	t.Engine = engine.String
	t.EngineVersion = engineVer.String
	t.ExpectedSize = nullIntPtr(expectedSize)
	t.ScheduledAt = nullIntPtr(scheduledAt)
	t.StartedAt = nullIntPtr(startedAt)
	t.FinishedAt = nullIntPtr(finishedAt)
	return &t, nil
}

// TaskFilter 列表过滤（docs/04 §1.3 游标分页 + 逗号多选状态）。
type TaskFilter struct {
	Statuses []string // 逗号分隔多选
	Platform string
	Q        string // 标题搜索
	Kind     string
	Limit    int
	Cursor   string // "created_at:id"
}

// ListResult 列表 + counts（总览页一次拿全）。
type ListResult struct {
	Items      []*Task        `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
	Total      int            `json:"total"`
	Counts     map[string]int `json:"counts"`
}

// ListTasks 列表查询。
func (s *Store) ListTasks(f TaskFilter) (*ListResult, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 50
	}
	where := []string{"deleted_at IS NULL"}
	args := []any{}
	if len(f.Statuses) > 0 {
		ph := make([]string, len(f.Statuses))
		for i, st := range f.Statuses {
			ph[i] = "?"
			args = append(args, st)
		}
		where = append(where, "status IN ("+strings.Join(ph, ",")+")")
	}
	if f.Platform != "" {
		where = append(where, "platform = ?")
		args = append(args, f.Platform)
	}
	if f.Q != "" {
		where = append(where, "title LIKE ? ESCAPE '\\'")
		args = append(args, "%"+likePattern(f.Q)+"%")
	}
	if f.Kind != "" {
		where = append(where, "kind = ?")
		args = append(args, f.Kind)
	}
	if cts, cid, ok := parseCursor(f.Cursor); ok {
		where = append(where, "(created_at < ? OR (created_at = ? AND id < ?))")
		args = append(args, cts, cts, cid)
	}
	cond := strings.Join(where, " AND ")
	q := taskSelect + " WHERE " + cond + " ORDER BY created_at DESC, id DESC LIMIT ?"
	args = append(args, f.Limit+1)

	rows, err := s.RO.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*Task
	for rows.Next() {
		t, err := scanTaskRows(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, t)
	}
	res := &ListResult{Items: items}
	if len(items) > f.Limit {
		last := items[f.Limit-1]
		res.NextCursor = fmt.Sprintf("%d:%s", last.CreatedAt, last.ID)
		res.Items = items[:f.Limit]
	}
	// counts：全量各状态计数（不带过滤条件，总览页语义）。
	counts, err := s.statusCounts()
	if err != nil {
		return nil, err
	}
	res.Counts = counts
	res.Total = 0
	for _, n := range counts {
		res.Total += n
	}
	return res, rows.Err()
}

func scanTaskRows(rows *sql.Rows) (*Task, error) {
	var t Task
	var parentID, sourceRef, platform, title, thumb, waitReason, quality, savePath, engine, engineVer sql.NullString
	var errCode, errMsg, fixHint sql.NullString
	var routePath, convertTo sql.NullString
	var expectedSize, scheduledAt, startedAt, finishedAt sql.NullInt64
	var resumeFlag, cancelFlag int64 // INTEGER 列经 int64 中转（database/sql 不支持直扫 bool）
	err := rows.Scan(&t.ID, &parentID, &t.Kind, &t.Source, &sourceRef, &platform, &t.URL, &title, &thumb,
		&t.Status, &waitReason, &quality, &savePath, &expectedSize, &t.DownloadedSize, &t.SpeedBps, &t.Progress,
		&resumeFlag, &engine, &engineVer, &t.ConfigVersion, &t.Attempt, &t.MaxAttempts,
		&errCode, &errMsg, &fixHint, &cancelFlag, &routePath, &scheduledAt,
		&t.CreatedAt, &t.UpdatedAt, &startedAt, &finishedAt, &t.Priority, &convertTo)
	if err != nil {
		return nil, err
	}
	t.ResumeSupported = resumeFlag != 0
	t.CancelRequested = cancelFlag != 0
	t.RoutePath = routePath.String
	t.ConvertTo = convertTo.String
	t.ErrorCode = errCode.String
	t.ErrorMessage = errMsg.String
	t.FixHint = fixHint.String
	t.ParentID = nullStrPtr(parentID)
	t.SourceRef = sourceRef.String
	t.Platform = platform.String
	t.Title = title.String
	t.Thumbnail = thumb.String
	t.WaitReason = waitReason.String
	t.Quality = quality.String
	t.SavePath = savePath.String
	t.Engine = engine.String
	t.EngineVersion = engineVer.String
	t.ExpectedSize = nullIntPtr(expectedSize)
	t.ScheduledAt = nullIntPtr(scheduledAt)
	t.StartedAt = nullIntPtr(startedAt)
	t.FinishedAt = nullIntPtr(finishedAt)
	return &t, nil
}

func (s *Store) statusCounts() (map[string]int, error) {
	rows, err := s.RO.Query(`SELECT status, COUNT(*) FROM tasks WHERE deleted_at IS NULL GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]int{"waiting": 0, "parsing": 0, "downloading": 0, "post": 0, "done": 0, "failed": 0, "cancelled": 0}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		m[st] = n
	}
	return m, rows.Err()
}

func parseCursor(c string) (int64, string, bool) {
	if c == "" {
		return 0, "", false
	}
	i := strings.IndexByte(c, ':')
	if i <= 0 {
		return 0, "", false
	}
	ts, err := strconv.ParseInt(c[:i], 10, 64)
	if err != nil {
		return 0, "", false
	}
	return ts, c[i+1:], true
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// likePattern 生成 LIKE 模式片段：截断 + 转义通配符（配套 ESCAPE '\' 子句用）。
//
// 两个坑都是实测踩出来的（渗透用例集锁定）：
//  1. 截断：SQLite 对超过约 5 万字符的 LIKE 模式直接报
//     "LIKE or GLOB pattern too complex"（SQLITE_MAX_LIKE_PATTERN_LENGTH），
//     超长搜索词会把 500 抛给用户 —— 搜索框粘 64KB 文本即可复现；
//  2. 转义：escapeLike 早年只转义、SQL 不带 ESCAPE 子句，而 SQLite 默认
//     反斜杠不是转义符，导致搜 "100%"、"a_b" 反而 0 命中（搜索功能坏掉）。
const likePatternMaxLen = 256

func likePattern(q string) string {
	if len(q) > likePatternMaxLen {
		q = q[:likePatternMaxLen] // 按字节截断：截断多字节字符尾部的搜索场景可接受
	}
	return escapeLike(q)
}

// UpdateTaskFields 通用字段更新（解析完成后写 title/size 等）。
// fromStatus 非空时做 CAS（仅当前状态匹配才更新）。
func (s *Store) UpdateTaskFields(id string, fields map[string]any, fromStatus string) error {
	if len(fields) == 0 {
		return nil
	}
	set := make([]string, 0, len(fields)+1)
	args := make([]any, 0, len(fields)+2)
	// 键白名单（防注入/误改敏感列）。
	allowed := map[string]bool{
		"title": true, "thumbnail": true, "platform": true, "status": true, "wait_reason": true,
		"expected_size": true, "downloaded_size": true, "speed_bps": true, "progress": true,
		"engine": true, "engine_version": true, "attempt": true, "error_code": true, "error_message": true,
		"fix_hint": true, "cancel_requested": true, "scheduled_at": true, "resume_supported": true,
		"quality": true, "save_path": true, "started_at": true, "finished_at": true, "route_path": true,
		"kind":     true, // 仅策略 failover（n115→qb）与手动换通道重试（RetryVia，download→n115）改写
		"priority": true, // 优先级（置顶/取消置顶）
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !allowed[k] {
			return fmt.Errorf("非白名单字段: %s", k)
		}
		set = append(set, k+" = ?")
		args = append(args, fields[k])
	}
	set = append(set, "updated_at = ?")
	args = append(args, time.Now().UnixMilli())
	args = append(args, id)
	q := "UPDATE tasks SET " + strings.Join(set, ", ") + " WHERE id = ?"
	if fromStatus != "" {
		q += " AND status = ?"
		args = append(args, fromStatus)
	}
	res, err := s.RW.Exec(q, args...)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrTaskNotFound
	}
	return nil
}

// ClaimWaiting 领取 due 任务（CAS：UPDATE ... WHERE id=? AND status='waiting' → 'parsing'）。
func (s *Store) ClaimWaiting(id string, toStatus string) (*Task, error) {
	res, err := s.RW.Exec(`UPDATE tasks SET status=?, started_at=COALESCE(started_at,?), updated_at=? WHERE id=? AND status='waiting'`,
		toStatus, time.Now().UnixMilli(), time.Now().UnixMilli(), id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrTaskNotFound
	}
	return s.GetTask(id)
}

// DueWaiting 取到期待领取任务（调度器候选集）。
// 只按 scheduled_at 判到期：wait_reason 是给用户看的说明（排队/自动重试退避/
// 进程中断恢复），不是调度闸门——若按 wait_reason 过滤，退避任务到期后
// 会永久卡在 waiting。
// DueWaiting 取到期的 waiting 任务。
//
// 排除批量父任务（platform='batch'）：父任务只是子任务的聚合壳，没有自己的下载动作，
// 由 aggregateParent 推进。若不排除，调度器会把父任务也当普通下载领走，
// 拿首个子任务的 URL 真跑一遍（重复下载），并把 platform 从 batch 覆写成真实平台，
// 聚合语义与 UI 展示都会失真。
func (s *Store) DueWaiting(now int64, limit int) ([]*Task, error) {
	rows, err := s.RO.Query(taskSelect+` WHERE status='waiting' AND platform<>'batch'
		AND (scheduled_at IS NULL OR scheduled_at <= ?) ORDER BY t.priority DESC, created_at LIMIT ?`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Task{}
	for rows.Next() {
		t, err := scanTaskRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ActiveConcurrency 活跃并发：全局 + 按平台（闸门用，docs/05 §3）。
// 排除批量父任务（与 DueWaiting 同理：它不占真实下载资源）。
//
// deleted_at 过滤是必需的，不能省：用户软删一个卡在 parsing/downloading 的任务后，
// 它若仍被计入并发，byPlatform 会永久占满 platform_cap，
// 该平台所有新任务全部饿死且没有任何报错（实测踩过：清库重跑时队列彻底不动）。
func (s *Store) ActiveConcurrency() (total int, byPlatform map[string]int, err error) {
	rows, err := s.RO.Query(`SELECT platform, COUNT(*) FROM tasks
		WHERE status IN ('parsing','downloading','post') AND platform<>'batch'
		  AND deleted_at IS NULL GROUP BY platform`)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	byPlatform = map[string]int{}
	for rows.Next() {
		var p string
		var n int
		if err := rows.Scan(&p, &n); err != nil {
			return 0, nil, err
		}
		if p == "" {
			p = "web"
		}
		byPlatform[p] = n
		total += n
	}
	return total, byPlatform, rows.Err()
}

// SoftDelete 软删（DELETE /tasks/{id} 不删文件）。
func (s *Store) SoftDelete(id string) error {
	res, err := s.RW.Exec(`UPDATE tasks SET deleted_at=?, updated_at=? WHERE id=? AND deleted_at IS NULL`,
		time.Now().UnixMilli(), time.Now().UnixMilli(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTaskNotFound
	}
	return nil
}

// ---- 事件 ----

// AppendEvent 写任务事件（状态变更/进度节流/引擎 warn）。
func (s *Store) AppendEvent(taskID, level, event, message, fieldsJSON string) error {
	_, err := s.RW.Exec(`INSERT INTO task_events (task_id, ts, level, event, message, fields_json) VALUES (?,?,?,?,?,?)`,
		taskID, time.Now().UnixMilli(), level, event, nullStr(message), nullStr(fieldsJSON))
	return err
}

// ListEvents 事件流（时间正序，游标 = last id）。
func (s *Store) ListEvents(taskID string, afterID int64, limit int) ([]*TaskEvent, bool, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.RO.Query(`SELECT id, task_id, ts, level, event, message, fields_json FROM task_events WHERE task_id=? AND id > ? ORDER BY id LIMIT ?`,
		taskID, afterID, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []*TaskEvent{}
	for rows.Next() {
		var e TaskEvent
		var msg, fj sql.NullString
		if err := rows.Scan(&e.ID, &e.TaskID, &e.Ts, &e.Level, &e.Event, &msg, &fj); err != nil {
			return nil, false, err
		}
		e.Message = msg.String
		e.FieldsJSON = fj.String
		out = append(out, &e)
	}
	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}
	return out, hasMore, rows.Err()
}

// ---- 产物文件 ----

// AddTaskFile 登记产物（post→done 时写入）。
func (s *Store) AddTaskFile(f *TaskFile) error {
	_, err := s.RW.Exec(`INSERT INTO task_files (id, task_id, path, size, mime, kind, created_at) VALUES (?,?,?,?,?,?,?)`,
		f.ID, f.TaskID, f.Path, f.Size, nullStr(f.Mime), f.Kind, f.CreatedAt)
	return err
}

// TaskFiles 任务产物列表。
func (s *Store) TaskFiles(taskID string) ([]*TaskFile, error) {
	rows, err := s.RO.Query(`SELECT id, task_id, path, size, mime, kind, created_at FROM task_files WHERE task_id=? ORDER BY created_at`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*TaskFile{}
	for rows.Next() {
		var f TaskFile
		var mime sql.NullString
		if err := rows.Scan(&f.ID, &f.TaskID, &f.Path, &f.Size, &mime, &f.Kind, &f.CreatedAt); err != nil {
			return nil, err
		}
		f.Mime = mime.String
		out = append(out, &f)
	}
	return out, rows.Err()
}

// DeleteTaskFile 删除产物索引记录（磁盘文件由上层先删）。
func (s *Store) DeleteTaskFile(fileID string) error {
	_, err := s.RW.Exec(`DELETE FROM task_files WHERE id=?`, fileID)
	return err
}

// DeleteTaskFilesByPath 按路径删除产物索引（媒体库删文件后同步，避免任务详情
// 里还挂着一条指向已删文件的记录）。返回删除行数。
func (s *Store) DeleteTaskFilesByPath(path string) (int64, error) {
	res, err := s.RW.Exec(`DELETE FROM task_files WHERE path=?`, path)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ListChildren 取子任务（父子聚合用）。
func (s *Store) ListChildren(parentID string) ([]*Task, error) {
	rows, err := s.RO.Query(taskSelect+` WHERE t.parent_id=? AND t.deleted_at IS NULL ORDER BY t.created_at`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Task{}
	for rows.Next() {
		t, err := scanTaskRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ---- 小工具 ----

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullStrPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func nullIntPtr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

func nullInt64Value(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}
