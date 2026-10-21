package store

// credentials.go：凭据中心数据层（docs/03 §4.4 credentials 表 / docs/04 §2.5）。
//
// 密文落库（AES-256-GCM，密钥在 data/keys/master.key，权限 0600）。
// 本文件只管存取与元信息，明文永不出现在 API 响应里。

import (
	"database/sql"
	"errors"
	"time"
)

// Credential 凭据元信息（不含密文）。
type Credential struct {
	ID          string  `json:"id"`
	Platform    string  `json:"platform"`
	Kind        string  `json:"kind"`
	Label       string  `json:"label"`
	Status      string  `json:"status"`
	Note        string  `json:"note,omitempty"`
	ExpiresAt   *int64  `json:"expires_at,omitempty"`
	LastCheckAt *int64  `json:"last_check_at,omitempty"`
	CreatedAt   int64   `json:"created_at"`
	UpdatedAt   int64   `json:"updated_at"`
	HasValue    bool    `json:"has_value"` // 是否含有效密文
	Preview     string  `json:"preview,omitempty"`
}

// ErrNoSuchCredential 凭据不存在。
var ErrNoSuchCredential = errors.New("credential not found")

// credentialSelect 列表/详情统一列序（不含密文本体，只带「是否有值」标记与打码预览）。
//
// 打码预览用 SQL 的 hex() 直接取 ciphertext 首尾各 2 字节，不解密：
// 一是列表页本来不需要明文，二是避免每个列表请求都做一次解密（还要拿主密钥）。
// AES-GCM 的 nonce 段字节随每次加密不同，因此这个预览只用于「认出大致是同一份凭据」，
// 不保证与 credential.Mask(明文) 的结果一致（后者只在 create/update 响应里出现）。
const credentialSelect = `SELECT id, platform, kind, label, status, note, expires_at, last_check_at, created_at, updated_at,
       length(ciphertext) > 0,
       CASE WHEN length(ciphertext) > 4 THEN substr(hex(ciphertext), 1, 4) || '****' || substr(hex(ciphertext), length(hex(ciphertext)) - 3, 4)
            WHEN length(ciphertext) > 0 THEN '****' ELSE '' END
  FROM credentials`

// ListCredentials 列出全部凭据（不含密文；按平台+创建时间排序）。
func (s *Store) ListCredentials() ([]*Credential, error) {
	rows, err := s.RO.Query(credentialSelect + ` ORDER BY platform, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Credential{}
	for rows.Next() {
		c, err := scanCredential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCredential 取凭据元信息 + 密文（解密由上层做）。
func (s *Store) GetCredential(id string) (*Credential, []byte, error) {
	row := s.RO.QueryRow(credentialSelect+` WHERE id=?`, id)
	c, err := scanCredential(row)
	if err != nil {
		return nil, nil, err
	}
	var cipher []byte
	if err := s.RO.QueryRow(`SELECT ciphertext FROM credentials WHERE id=?`, id).Scan(&cipher); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrNoSuchCredential
		}
		return nil, nil, err
	}
	return c, cipher, nil
}

// CreateCredential 新建凭据（密文已由上层加密）。
func (s *Store) CreateCredential(c *Credential, ciphertext []byte) error {
	now := time.Now().UnixMilli()
	c.ID = newID("cred")
	c.Status = "active"
	c.CreatedAt = now
	c.UpdatedAt = now
	_, err := s.RW.Exec(`INSERT INTO credentials (id, platform, kind, label, ciphertext, status, note, expires_at, last_check_at, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.Platform, c.Kind, c.Label, ciphertext, c.Status, c.Note, c.ExpiresAt, c.LastCheckAt, c.CreatedAt, c.UpdatedAt)
	if err != nil {
		return err
	}
	return nil
}

// UpdateCredential 更新凭据内容/标签（可只改标签不改密文）。
func (s *Store) UpdateCredential(id, label, note string, ciphertext []byte) error {
	now := time.Now().UnixMilli()
	if ciphertext == nil {
		res, err := s.RW.Exec(`UPDATE credentials SET label=?, note=?, updated_at=? WHERE id=?`, label, note, now, id)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return ErrNoSuchCredential
		}
		return nil
	}
	res, err := s.RW.Exec(`UPDATE credentials SET label=?, note=?, ciphertext=?, updated_at=? WHERE id=?`, label, note, ciphertext, now, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNoSuchCredential
	}
	return nil
}

// UpdateCredentialStatus 更新健康状态（active/expiring/expired/disabled）。
func (s *Store) UpdateCredentialStatus(id, status string) error {
	res, err := s.RW.Exec(`UPDATE credentials SET status=?, last_check_at=?, updated_at=? WHERE id=?`,
		status, time.Now().UnixMilli(), time.Now().UnixMilli(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNoSuchCredential
	}
	return nil
}

// DeleteCredential 删除凭据（已下载文件不受影响）。
func (s *Store) DeleteCredential(id string) error {
	res, err := s.RW.Exec(`DELETE FROM credentials WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNoSuchCredential
	}
	return nil
}

// rowScanner 兼容 sql.Row / sql.Rows。
type rowScanner interface {
	Scan(dest ...any) error
}

func scanCredential(sc rowScanner) (*Credential, error) {
	var c Credential
	var note sql.NullString
	var expires, lastCheck sql.NullInt64
	var hasValue int64
	var preview sql.NullString
	err := sc.Scan(&c.ID, &c.Platform, &c.Kind, &c.Label, &c.Status, &note, &expires, &lastCheck,
		&c.CreatedAt, &c.UpdatedAt, &hasValue, &preview)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoSuchCredential
	}
	if err != nil {
		return nil, err
	}
	c.Note = note.String
	if expires.Valid {
		c.ExpiresAt = &expires.Int64
	}
	if lastCheck.Valid {
		c.LastCheckAt = &lastCheck.Int64
	}
	c.HasValue = hasValue != 0
	c.Preview = preview.String
	return &c, nil
}
