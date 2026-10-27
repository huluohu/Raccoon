package store

// files.go：媒体库数据层（docs/03 §4.6 media_files 表 / docs/04 §2.8）。
//
// 媒体库是「已落盘产物」的索引视图，与 task_files 的区别：
// task_files 是一次任务的一次产物；media_files 是全局资产库（可来自订阅/直播/批量）。

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// MediaFile 媒体库条目。
type MediaFile struct {
	ID             string `json:"id"`
	TaskID         string `json:"task_id,omitempty"`
	SubscriptionID string `json:"subscription_id,omitempty"`
	Path           string `json:"path"`
	Name           string `json:"name"`
	Size           int64  `json:"size"`
	Mime           string `json:"mime,omitempty"`
	Duration       *int64 `json:"duration,omitempty"`
	Platform       string `json:"platform,omitempty"`
	Uploader       string `json:"uploader,omitempty"`
	Kind           string `json:"kind"`
	Previewable    bool   `json:"previewable"`
	Summary        string `json:"summary,omitempty"` // AI 结构化总结
	// CoverPath 封面图路径（空=无封面，前端回退类型占位）。
	CoverPath string `json:"cover_path,omitempty"`
	// Title 展示标题（可编辑；空串时前端回退到 name）。
	Title string `json:"title,omitempty"`
	// Description 简介 / 描述（可编辑或 AI 刮削填充）。
	Description string `json:"description,omitempty"`
	// Tags 标签，逗号分隔。
	Tags      string `json:"tags,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

// ErrNoSuchFile 媒体文件不存在。
var ErrNoSuchFile = errors.New("media file not found")

// FileFilter 媒体库过滤条件。
type FileFilter struct {
	Platform string
	Uploader string
	Kind     string // video/audio/image/other
	Query    string // 文件名模糊匹配
	Limit    int
	Offset   int
}

const mediaSelect = `SELECT id, task_id, subscription_id, path, name, size, mime, duration, platform, uploader, kind, created_at, summary, cover_path, title, description, tags FROM media_files`

// ListMediaFiles 分页查询媒体库。
func (s *Store) ListMediaFiles(f FileFilter) ([]*MediaFile, int, error) {
	var where []string
	var args []any
	if f.Platform != "" {
		where = append(where, "platform = ?")
		args = append(args, f.Platform)
	}
	if f.Uploader != "" {
		where = append(where, "uploader = ?")
		args = append(args, f.Uploader)
	}
	if f.Kind != "" {
		where = append(where, "kind = ?")
		args = append(args, f.Kind)
	}
	if f.Query != "" {
		where = append(where, "name LIKE ? ESCAPE '\\'")
		args = append(args, "%"+likePattern(f.Query)+"%")
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	var total int
	if err := s.RO.QueryRow(`SELECT COUNT(*) FROM media_files`+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := mediaSelect + clause + ` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, f.Offset)
	rows, err := s.RO.Query(q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*MediaFile{}
	for rows.Next() {
		m, err := scanMediaFile(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, m)
	}
	return out, total, rows.Err()
}

// GetMediaFile 按 ID 取媒体条目。
func (s *Store) GetMediaFile(id string) (*MediaFile, error) {
	m, err := scanMediaFile(s.RO.QueryRow(mediaSelect+` WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoSuchFile
	}
	return m, err
}

// UpsertMediaFile 登记/更新媒体文件（按 path 唯一；重复下载覆盖元信息）。
func (s *Store) UpsertMediaFile(m *MediaFile) error {
	now := time.Now().UnixMilli()
	if m.ID == "" {
		m.ID = newID("mf")
	}
	if m.CreatedAt == 0 {
		m.CreatedAt = now
	}
	var taskID, subID, mime, platform, uploader any
	if m.TaskID != "" {
		taskID = m.TaskID
	}
	if m.SubscriptionID != "" {
		subID = m.SubscriptionID
	}
	if m.Mime != "" {
		mime = m.Mime
	}
	if m.Platform != "" {
		platform = m.Platform
	}
	if m.Uploader != "" {
		uploader = m.Uploader
	}
	// ON CONFLICT：同一路径重新登记（如重新下载同一文件、格式转换产物入库）时，
	// **不覆盖用户手动编辑过的元数据**（标题/简介/标签/封面）。
	// 原实现直接 `cover_path=excluded.cover_path`，而 worker.finish 登记产物时
	// 这些字段是空的 → 用户在媒体库改好的标题与封面会被一次重新下载清空
	// （2026-10-25 定位）。空值不覆盖非空值是这里的正确语义：
	// 「没有这个字段的新值」≠「把这个字段清空」——清空要走专门的编辑接口。
	_, err := s.RW.Exec(`INSERT INTO media_files (id, task_id, subscription_id, path, name, size, mime, duration, platform, uploader, kind, created_at, cover_path, title, description, tags)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(path) DO UPDATE SET size=excluded.size, mime=excluded.mime,
		  duration=excluded.duration, platform=excluded.platform, uploader=excluded.uploader, kind=excluded.kind,
		  cover_path=CASE WHEN excluded.cover_path<>'' THEN excluded.cover_path ELSE media_files.cover_path END,
		  title=CASE WHEN excluded.title<>'' THEN excluded.title ELSE media_files.title END,
		  description=CASE WHEN excluded.description<>'' THEN excluded.description ELSE media_files.description END,
		  tags=CASE WHEN excluded.tags<>'' THEN excluded.tags ELSE media_files.tags END`,
		m.ID, taskID, subID, m.Path, m.Name, m.Size, mime, m.Duration, platform, uploader, m.Kind, m.CreatedAt,
		m.CoverPath, m.Title, m.Description, m.Tags)
	return err
}

// MediaMetadataUpdate 媒体元数据可编辑字段（指针区分「没传」与「传空串清空」）。
type MediaMetadataUpdate struct {
	Title       *string
	Uploader    *string
	Description *string
	Tags        *string
	Duration    *int64
}

// UpdateMediaMetadata 更新媒体元数据（只更新传入的字段）。
func (s *Store) UpdateMediaMetadata(id string, u MediaMetadataUpdate) error {
	sets := make([]string, 0, 5)
	args := make([]any, 0, 6)
	if u.Title != nil {
		sets = append(sets, "title=?")
		args = append(args, *u.Title)
	}
	if u.Uploader != nil {
		sets = append(sets, "uploader=?")
		args = append(args, *u.Uploader)
	}
	if u.Description != nil {
		sets = append(sets, "description=?")
		args = append(args, *u.Description)
	}
	if u.Tags != nil {
		sets = append(sets, "tags=?")
		args = append(args, *u.Tags)
	}
	if u.Duration != nil {
		sets = append(sets, "duration=?")
		args = append(args, *u.Duration)
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	res, err := s.RW.Exec(`UPDATE media_files SET `+strings.Join(sets, ", ")+` WHERE id=?`, args...)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNoSuchFile
	}
	return nil
}

// UpdateMediaCover 更新媒体封面路径。
func (s *Store) UpdateMediaCover(id, coverPath string) error {
	res, err := s.RW.Exec(`UPDATE media_files SET cover_path=? WHERE id=?`, coverPath, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNoSuchFile
	}
	return nil
}

// DeleteMediaFile 移除媒体索引记录（不删磁盘文件）。
func (s *Store) DeleteMediaFile(id string) error {
	res, err := s.RW.Exec(`DELETE FROM media_files WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNoSuchFile
	}
	return nil
}

// CountMediaFiles 媒体库总数（总览页统计）。
func (s *Store) CountMediaFiles() (int, error) {
	var n int
	err := s.RO.QueryRow(`SELECT COUNT(*) FROM media_files`).Scan(&n)
	return n, err
}

// MediaTotalSize 媒体库总体积。
func (s *Store) MediaTotalSize() (int64, error) {
	var total sql.NullInt64
	err := s.RO.QueryRow(`SELECT SUM(size) FROM media_files`).Scan(&total)
	if err != nil {
		return 0, err
	}
	return total.Int64, nil
}

// ListMediaPlatforms 媒体库已有平台（过滤器选项）。
func (s *Store) ListMediaPlatforms() ([]string, error) {
	rows, err := s.RO.Query(`SELECT DISTINCT platform FROM media_files WHERE platform IS NOT NULL AND platform <> '' ORDER BY platform`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanMediaFile(sc rowScanner) (*MediaFile, error) {
	var m MediaFile
	var taskID, subID, mime, platform, uploader sql.NullString
	var duration sql.NullInt64
	var kind, summary, coverPath, title, description, tags sql.NullString
	err := sc.Scan(&m.ID, &taskID, &subID, &m.Path, &m.Name, &m.Size, &mime, &duration, &platform, &uploader, &kind, &m.CreatedAt, &summary,
		&coverPath, &title, &description, &tags)
	if err != nil {
		return nil, err
	}
	m.TaskID = taskID.String
	m.SubscriptionID = subID.String
	m.Mime = mime.String
	m.Platform = platform.String
	m.Uploader = uploader.String
	m.Kind = kind.String
	m.Summary = summary.String
	m.CoverPath = coverPath.String
	m.Title = title.String
	m.Description = description.String
	m.Tags = tags.String
	if duration.Valid {
		m.Duration = &duration.Int64
	}
	m.Previewable = isPreviewable(m.Mime, m.Name)
	return &m, nil
}

// UpdateMediaSummary 更新媒体文件的 AI 总结内容。
func (s *Store) UpdateMediaSummary(id, summary string) error {
	res, err := s.RW.Exec(`UPDATE media_files SET summary=? WHERE id=?`, summary, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNoSuchFile
	}
	return nil
}

// MediaKindOf 按 mime/扩展名判定媒体类别（公开给 worker 登记产物时用）。
func MediaKindOf(mime, name string) string { return mediaKindOf(mime, name) }

// mediaKindOf 按 mime/扩展名判定媒体类别。
func mediaKindOf(mime, name string) string {
	m := strings.ToLower(mime)
	switch {
	case strings.HasPrefix(m, "video/"):
		return "video"
	case strings.HasPrefix(m, "audio/"):
		return "audio"
	case strings.HasPrefix(m, "image/"):
		return "image"
	case strings.Contains(m, "subtitle") || strings.Contains(m, "srt") || strings.Contains(m, "vtt"):
		return "subtitle"
	}
	ext := strings.ToLower(name)
	if i := strings.LastIndex(ext, "."); i >= 0 {
		ext = ext[i+1:]
	}
	switch ext {
	case "mp4", "webm", "ogv", "m4v", "mkv", "mov", "flv", "avi":
		return "video"
	case "mp3", "m4a", "opus", "ogg", "wav", "flac", "aac":
		return "audio"
	case "jpg", "jpeg", "png", "gif", "webp", "avif", "bmp":
		return "image"
	case "srt", "vtt", "ass":
		return "subtitle"
	}
	return "other"
}

// isPreviewable 浏览器能否直接播（编码白名单；不支持就返回 false，界面给下载按钮）。
func isPreviewable(mime, name string) bool {
	m := strings.ToLower(mime)
	switch {
	case strings.HasPrefix(m, "video/"):
		return strings.Contains(m, "mp4") || strings.Contains(m, "webm") || strings.Contains(m, "ogg")
	case strings.HasPrefix(m, "audio/"):
		return strings.Contains(m, "mpeg") || strings.Contains(m, "mp4") || strings.Contains(m, "ogg") || strings.Contains(m, "wav")
	case strings.HasPrefix(m, "image/"):
		return true
	}
	ext := strings.ToLower(name)
	if i := strings.LastIndex(ext, "."); i >= 0 {
		ext = ext[i+1:]
	}
	switch ext {
	case "mp4", "webm", "ogv", "m4v", "mp3", "m4a", "ogg", "wav", "flac", "jpg", "jpeg", "png", "gif", "webp", "avif":
		return true
	}
	return false
}
