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
	ID          string `json:"id"`
	Platform    string `json:"platform"`
	Kind        string `json:"kind"`
	Label       string `json:"label"`
	Status      string `json:"status"`
	Note        string `json:"note,omitempty"`
	ExpiresAt   *int64 `json:"expires_at,omitempty"`
	LastCheckAt *int64 `json:"last_check_at,omitempty"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
	HasValue    bool   `json:"has_value"` // 是否含有效密文
	Preview     string `json:"preview,omitempty"`
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

// MarkPlatformCredentialStatus 把某平台所有「非 disabled」的凭据标记为指定状态。
//
// 场景一（被动检测）：任务因 AUTH_REQUIRED 失败 → 标 expiring（待确认）。
// 场景二（主动探针）：平台 API 明确返回未登录 → 标 expired（已失效）。
//
// 只改 status 不动 ciphertext：凭据内容可能只是过期而非作废，用户刷新
// Cookie 后应能原地恢复（不新建条目、不丢历史）。
//
// disabled 是用户主动停用，不该被系统改写——跳过。
// expired 不再降级：已是终态的失效凭据不需要重复标记。
// 返回被标记的凭据条数（0 = 该平台没有可标记的凭据）。
func (s *Store) MarkPlatformCredentialStatus(platform, status string) (int, error) {
	if platform == "" || status == "" {
		return 0, nil
	}
	now := time.Now().UnixMilli()
	res, err := s.RW.Exec(`UPDATE credentials SET status=?, last_check_at=?, updated_at=?
		WHERE platform=? AND status != 'disabled' AND status != ?`, status, now, now, platform, status)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
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
