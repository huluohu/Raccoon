package store

// users.go：users / api_tokens 表读写（docs/03 §4.1）。

import (
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

// User 对应用户表；PasswordHash 不出 API（json:"-"）。
type User struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	CreatedAt    int64  `json:"created_at"`
	LastLoginAt  *int64 `json:"last_login_at,omitempty"`
}

// ErrNoSuchUser 用户不存在（上层转 401 文案）。
var ErrNoSuchUser = errors.New("user not found")

// ErrUserExists 用户名已占用（setup/注册冲突）。
var ErrUserExists = errors.New("username already exists")

func newID(prefix string) string { return prefix + "_" + uuid.NewString() }

// CountUsers 用户总数（==0 → setup_required）。
func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.RO.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// GetUserByUsername 按用户名取用户（区分不存在/不存在错误码）。
func (s *Store) GetUserByUsername(username string) (*User, error) {
	return s.scanUser(s.RO.QueryRow(`SELECT id, username, password_hash, created_at, last_login_at FROM users WHERE username=?`, username))
}

// GetUserByID 按 ID 取用户。
func (s *Store) GetUserByID(id string) (*User, error) {
	return s.scanUser(s.RO.QueryRow(`SELECT id, username, password_hash, created_at, last_login_at FROM users WHERE id=?`, id))
}

func (s *Store) scanUser(row *sql.Row) (*User, error) {
	var u User
	var lastLogin sql.NullInt64
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt, &lastLogin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoSuchUser
	}
	if err != nil {
		return nil, err
	}
	if lastLogin.Valid {
		u.LastLoginAt = &lastLogin.Int64
	}
	return &u, nil
}

// CreateUser 建用户（用户名冲突 → ErrUserExists；由 setup 流程调用，此时应无用户）。
func (s *Store) CreateUser(username, passwordHash string) (*User, error) {
	now := time.Now().UnixMilli()
	id := newID("usr")
	_, err := s.RW.Exec(`INSERT INTO users (id, username, password_hash, created_at, updated_at) VALUES (?,?,?,?,?)`,
		id, username, passwordHash, now, now)
	if err != nil {
		if isUniqueErr(err) {
			return nil, ErrUserExists
		}
		return nil, err
	}
	return &User{ID: id, Username: username, PasswordHash: passwordHash, CreatedAt: now}, nil
}

// UpdatePassword 改密码（PUT /auth/password）。
func (s *Store) UpdatePassword(userID, passwordHash string) error {
	res, err := s.RW.Exec(`UPDATE users SET password_hash=?, updated_at=? WHERE id=?`,
		passwordHash, time.Now().UnixMilli(), userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNoSuchUser
	}
	return nil
}

// TouchLogin 更新最后登录时间。
func (s *Store) TouchLogin(userID string) error {
	_, err := s.RW.Exec(`UPDATE users SET last_login_at=?, updated_at=? WHERE id=?`,
		time.Now().UnixMilli(), time.Now().UnixMilli(), userID)
	return err
}

// isUniqueErr SQLite UNIQUE 约束冲突（modernc 驱动错误串含 "UNIQUE"）。
func isUniqueErr(err error) bool {
	return err != nil && contains(err.Error(), "UNIQUE")
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
