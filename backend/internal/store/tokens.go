package store

// tokens.go：api_tokens 表读写（docs/03 §4.1 / docs/04 §1.1 Token 通道）。

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

// TokenScope 位掩码（docs/04 §1.1：scope 位掩码 + 撤销即时失效）。
const (
	ScopeRead  = 1 << 0 // 读：查询任务/列表/详情
	ScopeWrite = 1 << 1 // 写：创建/取消/重试/删除
	ScopeAdmin = 1 << 2 // 管理：配置/凭据/订阅/直播/令牌
)

// ScopeAll 全部权限。
const ScopeAll = ScopeRead | ScopeWrite | ScopeAdmin

// APIToken 令牌元信息（明文 token 只在创建时返回一次）。
type APIToken struct {
	ID         string `json:"id"`
	UserID     string `json:"user_id"`
	Name       string `json:"name"`
	Scope      int    `json:"scope"`
	ExpiresAt  *int64 `json:"expires_at,omitempty"`
	RevokedAt  *int64 `json:"revoked_at,omitempty"`
	CreatedAt  int64  `json:"created_at"`
	LastUsedAt *int64 `json:"last_used_at,omitempty"`
	Revoked    bool   `json:"revoked"`
}

// ErrNoSuchToken 令牌不存在。
var ErrNoSuchToken = errors.New("api token not found")

// ErrTokenRevoked 令牌已撤销。
var ErrTokenRevoked = errors.New("api token revoked")

// ErrTokenExpired 令牌已过期。
var ErrTokenExpired = errors.New("api token expired")

// HashToken 计算令牌哈希（明文不落库）。
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// ListAPITokens 列出令牌（不含哈希）。
func (s *Store) ListAPITokens() ([]*APIToken, error) {
	rows, err := s.RO.Query(`SELECT id, user_id, name, scope, expires_at, revoked_at, created_at, last_used_at
		FROM api_tokens ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*APIToken{}
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetAPITokenByHash 按哈希取令牌（认证时用）。
func (s *Store) GetAPITokenByHash(hash string) (*APIToken, error) {
	t, err := scanToken(s.RO.QueryRow(`SELECT id, user_id, name, scope, expires_at, revoked_at, created_at, last_used_at
		FROM api_tokens WHERE token_hash=?`, hash))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoSuchToken
	}
	return t, err
}

// CreateAPIToken 落库新令牌（返回 ID；明文由上层生成）。
func (s *Store) CreateAPIToken(userID, name, tokenHash string, scope int, expiresAt *int64) (*APIToken, error) {
	now := time.Now().UnixMilli()
	t := &APIToken{
		ID:        newID("tok"),
		UserID:    userID,
		Name:      name,
		Scope:     scope,
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}
	_, err := s.RW.Exec(`INSERT INTO api_tokens (id, user_id, name, token_hash, scope, expires_at, revoked_at, created_at, last_used_at)
		VALUES (?,?,?,?,?,?,NULL,?,NULL)`, t.ID, userID, name, tokenHash, scope, expiresAt, now)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// RevokeAPIToken 撤销令牌（立即生效）。
func (s *Store) RevokeAPIToken(id string) error {
	res, err := s.RW.Exec(`UPDATE api_tokens SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, time.Now().UnixMilli(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNoSuchToken
	}
	return nil
}

// TouchToken 使用时更新 last_used_at。
func (s *Store) TouchToken(id string) {
	_, _ = s.RW.Exec(`UPDATE api_tokens SET last_used_at=? WHERE id=?`, time.Now().UnixMilli(), id)
}

func scanToken(sc rowScanner) (*APIToken, error) {
	var t APIToken
	var expires, revoked, lastUsed sql.NullInt64
	err := sc.Scan(&t.ID, &t.UserID, &t.Name, &t.Scope, &expires, &revoked, &t.CreatedAt, &lastUsed)
	if err != nil {
		return nil, err
	}
	if expires.Valid {
		t.ExpiresAt = &expires.Int64
	}
	if revoked.Valid {
		t.RevokedAt = &revoked.Int64
	}
	if lastUsed.Valid {
		t.LastUsedAt = &lastUsed.Int64
	}
	t.Revoked = revoked.Valid
	return &t, nil
}
