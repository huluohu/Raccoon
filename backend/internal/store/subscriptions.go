package store

// subscriptions.go：订阅数据层（docs/03 §4.5 subscriptions / subscription_items 表）。

import (
	"database/sql"
	"errors"
	"time"
)

// Subscription 订阅规则。
type Subscription struct {
	ID            string    `json:"id"`
	Platform      string    `json:"platform"`
	SourceType    string    `json:"source_type"`
	SourceID      string    `json:"source_id"`
	SourceURL     string    `json:"source_url"`
	Title         string    `json:"title,omitempty"`
	Mode          string    `json:"mode"`
	RangeFrom     *int64    `json:"range_from,omitempty"`
	RangeTo       *int64    `json:"range_to,omitempty"`
	Quality       string    `json:"quality,omitempty"`
	SaveRule      string    `json:"save_rule,omitempty"`
	IntervalMin   int       `json:"interval_min"`
	Enabled       bool      `json:"enabled"`
	CredentialID  string    `json:"credential_id,omitempty"`
	LastCheckedAt *int64    `json:"last_checked_at,omitempty"`
	LastItemID    string    `json:"last_item_id,omitempty"`
	NextCheckAt   int64     `json:"next_check_at"`
	ErrorNote     string    `json:"error_note,omitempty"`
	CreatedAt     int64     `json:"created_at"`
	UpdatedAt     int64     `json:"updated_at"`
	ItemCounts    SubCounts `json:"item_counts"`
}

// SubCounts 订阅内容统计（详情页展示）。
type SubCounts struct {
	Total      int `json:"total"`
	Downloaded int `json:"downloaded"`
	Queued     int `json:"queued"`
	Skipped    int `json:"skipped"`
	Failed     int `json:"failed"`
}

// SubscriptionItem 订阅发现的一条内容。
type SubscriptionItem struct {
	ID             int64  `json:"id"`
	SubscriptionID string `json:"subscription_id"`
	ItemID         string `json:"item_id"`
	Title          string `json:"title,omitempty"`
	TaskID         string `json:"task_id,omitempty"`
	Status         string `json:"status"`
	DiscoveredAt   int64  `json:"discovered_at"`
}

// ErrNoSuchSubscription 订阅不存在。
var ErrNoSuchSubscription = errors.New("subscription not found")

const subSelect = `SELECT id, platform, source_type, source_id, source_url, title, mode, range_from, range_to,
	quality, save_rule, interval_min, enabled, credential_id, last_checked_at, last_item_id, next_check_at,
	error_note, created_at, updated_at FROM subscriptions`

// ListSubscriptions 列出订阅（含内容统计）。
func (s *Store) ListSubscriptions() ([]*Subscription, error) {
	rows, err := s.RO.Query(subSelect + ` ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Subscription{}
	for rows.Next() {
		sub, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, sub := range out {
		c, err := s.CountSubItems(sub.ID)
		if err == nil {
			sub.ItemCounts = c
		}
	}
	return out, nil
}

// GetSubscription 取订阅详情。
func (s *Store) GetSubscription(id string) (*Subscription, error) {
	sub, err := scanSubscription(s.RO.QueryRow(subSelect+` WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoSuchSubscription
	}
	if err != nil {
		return nil, err
	}
	c, err := s.CountSubItems(id)
	if err == nil {
		sub.ItemCounts = c
	}
	return sub, nil
}

// CreateSubscription 新建订阅。
func (s *Store) CreateSubscription(sub *Subscription) error {
	now := time.Now().UnixMilli()
	sub.ID = newID("sub")
	sub.CreatedAt = now
	sub.UpdatedAt = now
	if sub.Mode == "" {
		sub.Mode = "new_only"
	}
	if sub.IntervalMin <= 0 {
		sub.IntervalMin = 30
	}
	if sub.NextCheckAt == 0 {
		sub.NextCheckAt = now + int64(sub.IntervalMin)*60_000
	}
	_, err := s.RW.Exec(`INSERT INTO subscriptions (id, platform, source_type, source_id, source_url, title, mode,
		range_from, range_to, quality, save_rule, interval_min, enabled, credential_id, last_checked_at, last_item_id,
		next_check_at, error_note, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		sub.ID, sub.Platform, sub.SourceType, sub.SourceID, sub.SourceURL, sub.Title, sub.Mode,
		sub.RangeFrom, sub.RangeTo, sub.Quality, sub.SaveRule, sub.IntervalMin, boolInt(sub.Enabled), sub.CredentialID,
		sub.LastCheckedAt, sub.LastItemID, sub.NextCheckAt, sub.ErrorNote, sub.CreatedAt, sub.UpdatedAt)
	return err
}

// UpdateSubscription 更新订阅规则。
func (s *Store) UpdateSubscription(sub *Subscription) error {
	sub.UpdatedAt = time.Now().UnixMilli()
	res, err := s.RW.Exec(`UPDATE subscriptions SET title=?, mode=?, range_from=?, range_to=?, quality=?,
		save_rule=?, interval_min=?, enabled=?, credential_id=?, next_check_at=?, error_note=?, updated_at=?
		WHERE id=?`,
		sub.Title, sub.Mode, sub.RangeFrom, sub.RangeTo, sub.Quality, sub.SaveRule, sub.IntervalMin,
		boolInt(sub.Enabled), sub.CredentialID, sub.NextCheckAt, sub.ErrorNote, sub.UpdatedAt, sub.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNoSuchSubscription
	}
	return nil
}

// MarkSubscriptionChecked 记录一次检查结果（时间、最后 item、错误、下次检查时间）。
func (s *Store) MarkSubscriptionChecked(id, lastItemID, errorNote string, nextCheckAt int64) error {
	now := time.Now().UnixMilli()
	res, err := s.RW.Exec(`UPDATE subscriptions SET last_checked_at=?, last_item_id=?, error_note=?, next_check_at=?, updated_at=? WHERE id=?`,
		now, lastItemID, errorNote, nextCheckAt, now, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNoSuchSubscription
	}
	return nil
}

// DeleteSubscription 删除订阅（已下载文件不受影响）。
func (s *Store) DeleteSubscription(id string) error {
	res, err := s.RW.Exec(`DELETE FROM subscriptions WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNoSuchSubscription
	}
	return nil
}

// DueSubscriptions 取到期需检查的订阅。
func (s *Store) DueSubscriptions(now int64, limit int) ([]*Subscription, error) {
	rows, err := s.RO.Query(subSelect+` WHERE enabled=1 AND next_check_at <= ? ORDER BY next_check_at LIMIT ?`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Subscription{}
	for rows.Next() {
		sub, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// UpsertSubscriptionItem 记录发现的内容（已存在则返回 false 表示跳过，防重复入队）。
func (s *Store) UpsertSubscriptionItem(subID, itemID, title, taskID, status string) (bool, error) {
	now := time.Now().UnixMilli()
	res, err := s.RW.Exec(`INSERT INTO subscription_items (subscription_id, item_id, title, task_id, status, discovered_at)
		VALUES (?,?,?,?,?,?) ON CONFLICT(subscription_id, item_id) DO NOTHING`,
		subID, itemID, title, nullIfEmpty(taskID), status, now)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// UpdateSubscriptionItemStatus 更新内容状态（downloaded/skipped/failed 等）。
func (s *Store) UpdateSubscriptionItemStatus(subID, itemID, status, taskID string) error {
	_, err := s.RW.Exec(`UPDATE subscription_items SET status=?, task_id=COALESCE(NULLIF(?,''), task_id) WHERE subscription_id=? AND item_id=?`,
		status, taskID, subID, itemID)
	return err
}

// ListSubscriptionItems 列出订阅发现的内容。
func (s *Store) ListSubscriptionItems(subID string, limit int) ([]*SubscriptionItem, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.RO.Query(`SELECT id, subscription_id, item_id, title, task_id, status, discovered_at
		FROM subscription_items WHERE subscription_id=? ORDER BY discovered_at DESC, id DESC LIMIT ?`, subID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*SubscriptionItem{}
	for rows.Next() {
		var it SubscriptionItem
		var title, taskID sql.NullString
		if err := rows.Scan(&it.ID, &it.SubscriptionID, &it.ItemID, &title, &taskID, &it.Status, &it.DiscoveredAt); err != nil {
			return nil, err
		}
		it.Title = title.String
		it.TaskID = taskID.String
		out = append(out, &it)
	}
	return out, rows.Err()
}

// CountSubItems 统计订阅内容各状态数量。
func (s *Store) CountSubItems(subID string) (SubCounts, error) {
	var c SubCounts
	rows, err := s.RO.Query(`SELECT status, COUNT(*) FROM subscription_items WHERE subscription_id=? GROUP BY status`, subID)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return c, err
		}
		c.Total += n
		switch status {
		case "downloaded":
			c.Downloaded = n
		case "queued":
			c.Queued = n
		case "skipped":
			c.Skipped = n
		case "failed":
			c.Failed = n
		}
	}
	return c, rows.Err()
}

// HasSubscriptionItem 判断内容是否已记录（防重复入队，跨重启有效）。
func (s *Store) HasSubscriptionItem(subID, itemID string) (bool, error) {
	var n int
	err := s.RO.QueryRow(`SELECT COUNT(*) FROM subscription_items WHERE subscription_id=? AND item_id=?`, subID, itemID).Scan(&n)
	return n > 0, err
}

func scanSubscription(sc rowScanner) (*Subscription, error) {
	var sub Subscription
	var title, quality, saveRule, credID, lastItem, errorNote sql.NullString
	var rangeFrom, rangeTo, lastChecked sql.NullInt64
	var enabled int64
	err := sc.Scan(&sub.ID, &sub.Platform, &sub.SourceType, &sub.SourceID, &sub.SourceURL, &title, &sub.Mode,
		&rangeFrom, &rangeTo, &quality, &saveRule, &sub.IntervalMin, &enabled, &credID, &lastChecked, &lastItem,
		&sub.NextCheckAt, &errorNote, &sub.CreatedAt, &sub.UpdatedAt)
	if err != nil {
		return nil, err
	}
	sub.Title = title.String
	sub.Quality = quality.String
	sub.SaveRule = saveRule.String
	sub.CredentialID = credID.String
	sub.LastItemID = lastItem.String
	sub.ErrorNote = errorNote.String
	sub.Enabled = enabled != 0
	if rangeFrom.Valid {
		sub.RangeFrom = &rangeFrom.Int64
	}
	if rangeTo.Valid {
		sub.RangeTo = &rangeTo.Int64
	}
	if lastChecked.Valid {
		sub.LastCheckedAt = &lastChecked.Int64
	}
	return &sub, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
