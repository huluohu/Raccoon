package store

// settings_store.go：settings / setting_history 表读写（docs/03 §4.2 / docs/06 §5）。
//
// 写库在 config.Persist 里（同包外），这里提供历史查询、回滚与版本读取。

import (
	"database/sql"
	"errors"
	"time"
)

// ErrNoSuchHistory 历史版本不存在（回滚目标不存在）。
var ErrNoSuchHistory = errors.New("setting history not found")

// SettingRecord settings 表的一行（含生效时机与锁定来源）。
type SettingRecord struct {
	Key         string `json:"key"`
	ValueJSON   string `json:"value_json"`
	Effective   string `json:"effective"`
	Sensitive   bool   `json:"sensitive"`
	LockedByEnv string `json:"locked_by_env,omitempty"`
	Version     int    `json:"version"`
	UpdatedAt   int64  `json:"updated_at"`
	UpdatedBy   string `json:"updated_by,omitempty"`
}

// SettingHistoryEntry 配置历史一条。
type SettingHistoryEntry struct {
	ID        int64  `json:"id"`
	Key       string `json:"key"`
	ValueJSON string `json:"value_json"`
	Version   int    `json:"version"`
	ChangedAt int64  `json:"changed_at"`
	ChangedBy string `json:"changed_by,omitempty"`
	Note      string `json:"note,omitempty"`
}

// ListSettings 读 settings 表全部行。
func (s *Store) ListSettings() ([]*SettingRecord, error) {
	rows, err := s.RO.Query(`SELECT key, value_json, effective, sensitive, locked_by_env, version, updated_at, updated_by
		FROM settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*SettingRecord{}
	for rows.Next() {
		var r SettingRecord
		var locked, updatedBy sql.NullString
		var sensitive int64
		if err := rows.Scan(&r.Key, &r.ValueJSON, &r.Effective, &sensitive, &locked, &r.Version, &r.UpdatedAt, &updatedBy); err != nil {
			return nil, err
		}
		r.Sensitive = sensitive != 0
		r.LockedByEnv = locked.String
		r.UpdatedBy = updatedBy.String
		out = append(out, &r)
	}
	return out, rows.Err()
}

// GetSettingVersion 读某键当前版本。
func (s *Store) GetSettingVersion(key string) (int, error) {
	var v int
	err := s.RO.QueryRow(`SELECT version FROM settings WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return v, err
}

// AppendSettingHistory 追加一条历史（保留最近 50 条/键，超出清理）。
func (s *Store) AppendSettingHistory(key, valueJSON string, version int, changedBy, note string) error {
	_, err := s.RW.Exec(`INSERT INTO setting_history (key, value_json, version, changed_at, changed_by, note)
		VALUES (?,?,?,?,?,?)`, key, valueJSON, version, time.Now().UnixMilli(), changedBy, note)
	if err != nil {
		return err
	}
	// 保留最近 50 条/键（docs/06 §5）。
	_, err = s.RW.Exec(`DELETE FROM setting_history WHERE key=? AND id NOT IN (
		SELECT id FROM setting_history WHERE key=? ORDER BY id DESC LIMIT 50)`, key, key)
	return err
}

// ListSettingHistory 列历史（key 为空则列全部，按时间倒序）。
func (s *Store) ListSettingHistory(key string, limit int) ([]*SettingHistoryEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT id, key, value_json, version, changed_at, changed_by, note FROM setting_history`
	var args []any
	if key != "" {
		q += ` WHERE key=?`
		args = append(args, key)
	}
	q += ` ORDER BY changed_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.RO.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*SettingHistoryEntry{}
	for rows.Next() {
		var e SettingHistoryEntry
		var changedBy, note sql.NullString
		if err := rows.Scan(&e.ID, &e.Key, &e.ValueJSON, &e.Version, &e.ChangedAt, &changedBy, &note); err != nil {
			return nil, err
		}
		e.ChangedBy = changedBy.String
		e.Note = note.String
		out = append(out, &e)
	}
	return out, rows.Err()
}

// GetSettingHistoryValue 取某键某版本的历史值（回滚用）。
func (s *Store) GetSettingHistoryValue(key string, version int) (string, error) {
	var v string
	err := s.RO.QueryRow(`SELECT value_json FROM setting_history WHERE key=? AND version=? ORDER BY id DESC LIMIT 1`, key, version).Scan(&v)
	if err == sql.ErrNoRows {
		return "", ErrNoSuchHistory
	}
	return v, err
}
