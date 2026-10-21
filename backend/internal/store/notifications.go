package store

// notifications.go：通知中心数据层（docs/03 §4.7 notifications 表 / docs/04 §2.9）。

import (
	"database/sql"
	"time"
)

// Notification 一条通知。
type Notification struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Severity  string `json:"severity"`
	Title     string `json:"title"`
	Body      string `json:"body,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	Read      bool   `json:"read"`
	CreatedAt int64  `json:"created_at"`
}

const notifSelect = `SELECT id, kind, severity, title, body, task_id, read, created_at FROM notifications`

// ListNotifications 列通知（未读优先? 契约是时间倒序；未读徽章单独统计）。
func (s *Store) ListNotifications(limit int, unreadOnly bool) ([]*Notification, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := notifSelect
	var args []any
	if unreadOnly {
		q += ` WHERE read=0`
	}
	q += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.RO.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Notification{} // 非 nil：空列表序列化为 [] 而非 null
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// CountUnreadNotifications 未读计数（顶栏徽章）。
func (s *Store) CountUnreadNotifications() (int, error) {
	var n int
	err := s.RO.QueryRow(`SELECT COUNT(*) FROM notifications WHERE read=0`).Scan(&n)
	return n, err
}

// CreateNotification 写一条通知。
func (s *Store) CreateNotification(n *Notification) error {
	now := time.Now().UnixMilli()
	n.ID = newID("ntf")
	n.CreatedAt = now
	if n.Severity == "" {
		n.Severity = "info"
	}
	_, err := s.RW.Exec(`INSERT INTO notifications (id, kind, severity, title, body, task_id, read, created_at)
		VALUES (?,?,?,?,?,?,0,?)`,
		n.ID, n.Kind, n.Severity, n.Title, n.Body, nullIfEmpty(n.TaskID), now)
	if err != nil {
		return err
	}
	return nil
}

// MarkNotificationsRead 标记已读（all=true 时全标）。
func (s *Store) MarkNotificationsRead(ids []string, all bool) (int64, error) {
	if all {
		res, err := s.RW.Exec(`UPDATE notifications SET read=1 WHERE read=0`)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	}
	if len(ids) == 0 {
		return 0, nil
	}
	var total int64
	for _, id := range ids {
		res, err := s.RW.Exec(`UPDATE notifications SET read=1 WHERE id=?`, id)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
	}
	return total, nil
}

// DeleteOldNotifications 清理历史通知（保留最近 keep 条）。
func (s *Store) DeleteOldNotifications(keep int) error {
	if keep <= 0 {
		keep = 200
	}
	_, err := s.RW.Exec(`DELETE FROM notifications WHERE id NOT IN (
		SELECT id FROM notifications ORDER BY created_at DESC LIMIT ?)`, keep)
	return err
}

func scanNotification(sc rowScanner) (*Notification, error) {
	var n Notification
	var body, taskID sql.NullString
	var read int64
	err := sc.Scan(&n.ID, &n.Kind, &n.Severity, &n.Title, &body, &taskID, &read, &n.CreatedAt)
	if err != nil {
		return nil, err
	}
	n.Body = body.String
	n.TaskID = taskID.String
	n.Read = read != 0
	return &n, nil
}
