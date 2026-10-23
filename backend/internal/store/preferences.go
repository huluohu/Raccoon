package store

// preferences.go：用户偏好（主页布局等）单行持久化（docs/15 §3.3 / docs/14 E 系列）。
//
// 单行表 user_preferences（CHECK id=1，迁移 0007）：layout JSON 字符串原样存取，
// 结构校验在 handler 层做（白名单字段），store 层不解析业务语义。

import (
	"database/sql"
	"time"
)

// GetPreferencesLayout 读主页布局 JSON（无行时返回空串，调用方回落默认）。
func (s *Store) GetPreferencesLayout() (string, error) {
	var layout string
	err := s.RO.QueryRow(`SELECT layout FROM user_preferences WHERE id = 1`).Scan(&layout)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return layout, nil
}

// SetPreferencesLayout UPSERT 主页布局 JSON（单行覆盖写）。
func (s *Store) SetPreferencesLayout(layout string) error {
	_, err := s.RW.Exec(`INSERT INTO user_preferences (id, layout, updated_at) VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET layout = excluded.layout, updated_at = excluded.updated_at`,
		layout, time.Now().UnixMilli())
	return err
}
