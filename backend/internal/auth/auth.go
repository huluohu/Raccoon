// Package auth 提供口令散列（Argon2id）与 Web 会话管理（内存会话表 + CSRF）。
// 会话为 HttpOnly Cookie（SameSite=Lax）；API Token 通道见 docs/04 §1.1（Wave 4.9 接入）。
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Argon2id 参数（OWASP 2023 建议量级；自托管单用户场景取中等偏上）。
const (
	argonTime    uint32 = 3
	argonMemory  uint32 = 64 * 1024 // 64 MiB
	argonThreads uint8  = 4
	argonKeyLen  uint32 = 32
)

// MinPasswordLen 口令最小长度（本地部署防弱口令；docs/02 §5）。
const MinPasswordLen = 8

// CheckPasswordPolicy 口令强度校验（纯本地规则，不上传）。
func CheckPasswordPolicy(pw string) error {
	if len([]rune(pw)) < MinPasswordLen {
		return fmt.Errorf("口令至少 %d 位", MinPasswordLen)
	}
	return nil
}

// HashPassword 生成 Argon2id PHC 格式散列：$argon2id$v=19$m=..,t=..,p=..$salt$hash。
func HashPassword(pw string) (string, error) {
	if err := CheckPasswordPolicy(pw); err != nil {
		return "", err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword 恒定时间校验口令。
func VerifyPassword(pw, encoded string) bool {
	salt, key, t, m, p, err := decodePHC(encoded)
	if err != nil {
		return false
	}
	got := argon2IDKey([]byte(pw), salt, t, m, p, uint32(len(key)))
	return subtle.ConstantTimeCompare(got, key) == 1
}

// NeedsRehash 参数与当前策略不符时为 true（登录后透明升级散列）。
func NeedsRehash(encoded string) bool {
	salt, key, t, m, p, err := decodePHC(encoded)
	if err != nil || len(salt) != 16 || len(key) != int(argonKeyLen) {
		return true
	}
	return t != argonTime || m != argonMemory || p != argonThreads
}

func decodePHC(encoded string) (salt, key []byte, t uint32, m uint32, p uint8, err error) {
	parts := strings.Split(encoded, "$")
	// "$argon2id$v=19$m=..,t=..,p=..$salt$hash" → 6 段，首段为空串。
	if len(parts) != 6 || parts[1] != "argon2id" {
		return nil, nil, 0, 0, 0, errors.New("bad phc header")
	}
	var ver int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &ver); err != nil || ver != 19 {
		return nil, nil, 0, 0, 0, errors.New("bad phc version")
	}
	var threadsU uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &threadsU); err != nil {
		return nil, nil, 0, 0, 0, errors.New("bad phc params")
	}
	p = uint8(threadsU)
	if salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil {
		return nil, nil, 0, 0, 0, err
	}
	if key, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil {
		return nil, nil, 0, 0, 0, err
	}
	return salt, key, t, m, p, nil
}

// ---- 会话表（内存；重启需重新登录，可接受：单用户自托管） ----

// Session 一条 Web 会话。
type Session struct {
	UserID    string
	CSRFToken string
	CreatedAt time.Time
	LastSeen  time.Time
}

// SessionStore 内存会话表（docs/04 §1.1「内存会话表」落地）。
type SessionStore struct {
	mu       sync.Mutex
	sessions map[string]*Session
	ttl      time.Duration
}

// NewSessionStore 建会话表；ttl 为有效期（滑动续期）。
func NewSessionStore(ttl time.Duration) *SessionStore {
	return &SessionStore{sessions: map[string]*Session{}, ttl: ttl}
}

// Create 新建会话，返回 sessionID 与 CSRF token。
func (s *SessionStore) Create(userID string) (sid, csrf string, err error) {
	sid, err = randToken(24)
	if err != nil {
		return "", "", err
	}
	csrf, err = randToken(24)
	if err != nil {
		return "", "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sid] = &Session{UserID: userID, CSRFToken: csrf, CreatedAt: time.Now(), LastSeen: time.Now()}
	return sid, csrf, nil
}

// Get 校验会话（滑动续期）；有效返回 UserID。
func (s *SessionStore) Get(sid string) (userID, csrf string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sid]
	if !ok {
		return "", "", false
	}
	if time.Since(sess.LastSeen) > s.ttl {
		delete(s.sessions, sid)
		return "", "", false
	}
	sess.LastSeen = time.Now()
	return sess.UserID, sess.CSRFToken, true
}

// Delete 销毁会话（登出）。
func (s *SessionStore) Delete(sid string) {
	s.mu.Lock()
	delete(s.sessions, sid)
	s.mu.Unlock()
}

// GC 清理过期会话（启动 goroutine 每小时一次）。
func (s *SessionStore) GC() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sid, sess := range s.sessions {
		if time.Since(sess.LastSeen) > s.ttl {
			delete(s.sessions, sid)
		}
	}
}

func randToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
