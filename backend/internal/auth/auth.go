// Package auth 提供口令散列（Argon2id）与 Web 会话管理（SQLite 持久化会话表 + CSRF）。
// 会话为 HttpOnly Cookie（SameSite=Lax）；API Token 通道见 docs/04 §1.1（Wave 4.9 接入）。
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/huluohu/raccoon/internal/store"
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

// ---- 会话表（SQLite 持久化，migration 0005；应用重启不清登录态） ----

// SessionStore 持久化会话表（docs/04 §1.1「内存会话表」的升级版：
// 存储下沉到 store.auth_sessions 表，进程重启后同一把 sid 仍可校验通过）。
//
// 有效期两种（均滑动续期，以 last_seen 计）：
//   - remember=true：「30 天内免登录」，Cookie 同步带 30 天 Max-Age；
//   - remember=false：普通会话 24h，Cookie 不带 Max-Age（浏览器关闭即失效）。
type SessionStore struct {
	store       *store.Store
	rememberTTL time.Duration
	defaultTTL  time.Duration
}

// NewSessionStore 建会话表：db 为 SQLite 出口，两个 TTL 分别对应 remember 两种会话。
func NewSessionStore(db *store.Store, rememberTTL, defaultTTL time.Duration) *SessionStore {
	return &SessionStore{store: db, rememberTTL: rememberTTL, defaultTTL: defaultTTL}
}

// Create 新建会话，返回 sessionID 与 CSRF token。
func (s *SessionStore) Create(userID string, remember bool) (sid, csrf string, err error) {
	sid, err = randToken(24)
	if err != nil {
		return "", "", err
	}
	csrf, err = randToken(24)
	if err != nil {
		return "", "", err
	}
	if err := s.store.CreateSession(sid, userID, csrf, remember); err != nil {
		return "", "", err
	}
	return sid, csrf, nil
}

// renewInterval 滑动续期落库节流：距上次刷新不足该间隔不写库，
// 避免每个请求都产生一次 UPDATE（单用户场景 60s 粒度足够）。
const renewInterval = time.Minute

// Get 校验会话（滑动续期）；有效返回 UserID。
func (s *SessionStore) Get(sid string) (userID, csrf string, ok bool) {
	sess, err := s.store.GetSession(sid)
	if err != nil {
		return "", "", false
	}
	ttl := s.defaultTTL
	if sess.Remember {
		ttl = s.rememberTTL
	}
	lastSeen := time.UnixMilli(sess.LastSeen)
	if time.Since(lastSeen) > ttl {
		_ = s.store.DeleteSession(sid)
		return "", "", false
	}
	// 续期节流：last_seen 落后超过 renewInterval 才写一次库。
	if time.Since(lastSeen) > renewInterval {
		_ = s.store.TouchSession(sid, time.Now())
	}
	return sess.UserID, sess.CSRFToken, true
}

// Delete 销毁会话（登出）。
func (s *SessionStore) Delete(sid string) {
	_ = s.store.DeleteSession(sid)
}

// GC 清理过期会话（启动 goroutine 每小时一次；重启后补跑，等价于恢复时清洗）。
func (s *SessionStore) GC() {
	_ = s.store.GCSessionRows(time.Now(), s.rememberTTL, s.defaultTTL)
}

func randToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
