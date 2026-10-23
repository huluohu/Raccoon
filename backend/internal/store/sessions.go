package store

// sessions.go：auth_sessions 表读写（docs/03；migration 0005）。
// Web 会话持久化的唯一出口——auth.SessionStore 薄封装这层，重启后照常校验。

import (
	"database/sql"
	"errors"
	"time"
)

// AuthSession 一条持久化 Web 会话。
type AuthSession struct {
	SID       string
	UserID    string
	CSRFToken string
	Remember  bool
	CreatedAt int64
	LastSeen  int64
}

// ErrNoSuchSession 会话不存在（含被 GC / 登出删除）。
var ErrNoSuchSession = errors.New("session not found")

// CreateSession 新建会话（sid/csrf 由 auth 层生成，这里只落库）。
func (s *Store) CreateSession(sid, userID, csrf string, remember bool) error {
	now := time.Now().UnixMilli()
	rem := 0
	if remember {
		rem = 1
	}
	_, err := s.RW.Exec(`INSERT INTO auth_sessions (sid, user_id, csrf_token, remember, created_at, last_seen) VALUES (?,?,?,?,?,?)`,
		sid, userID, csrf, rem, now, now)
	return err
}

// GetSession 按 sid 取会话；不存在返回 ErrNoSuchSession。
func (s *Store) GetSession(sid string) (*AuthSession, error) {
	row := s.RO.QueryRow(`SELECT sid, user_id, csrf_token, remember, created_at, last_seen FROM auth_sessions WHERE sid=?`, sid)
	var sess AuthSession
	var rem int
	err := row.Scan(&sess.SID, &sess.UserID, &sess.CSRFToken, &rem, &sess.CreatedAt, &sess.LastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoSuchSession
	}
	if err != nil {
		return nil, err
	}
	sess.Remember = rem == 1
	return &sess, nil
}

// TouchSession 滑动续期：刷新 last_seen。
func (s *Store) TouchSession(sid string, when time.Time) error {
	_, err := s.RW.Exec(`UPDATE auth_sessions SET last_seen=? WHERE sid=?`, when.UnixMilli(), sid)
	return err
}

// DeleteSession 删除会话（登出）。
func (s *Store) DeleteSession(sid string) error {
	_, err := s.RW.Exec(`DELETE FROM auth_sessions WHERE sid=?`, sid)
	return err
}

// GCSessionRows 清理过期会话：remember 行按 rememberTTL，其余按 defaultTTL（均以 last_seen 计）。
func (s *Store) GCSessionRows(now time.Time, rememberTTL, defaultTTL time.Duration) error {
	_, err := s.RW.Exec(`DELETE FROM auth_sessions WHERE last_seen < CASE WHEN remember=1 THEN ? ELSE ? END`,
		now.Add(-rememberTTL).UnixMilli(), now.Add(-defaultTTL).UnixMilli())
	return err
}
