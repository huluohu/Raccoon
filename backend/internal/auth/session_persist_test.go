package auth

// session_persist_test.go：会话持久化回归（migration 0005）。
// 锁死一个真实用户反馈：会话原来只存内存，应用重启后浏览器里仍有效的
// Cookie 立刻作废，被迫重新登录。现在会话落 SQLite，重启后同一把 sid 必须照样通过。

import (
	"testing"
	"time"

	"github.com/huluohu/raccoon/internal/store"
)

// newSessionTestStore 建测试库 + 一个真实用户（auth_sessions.user_id 有外键约束）。
// 返回库与该用户的 ID，会话一律挂在这个用户名下。
func newSessionTestStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	db, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("打开测试库失败：%v", err)
	}
	t.Cleanup(func() { db.Close() })
	u, err := db.CreateUser("tester", "$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHRzYWx0c2FsdA$aGFzaGhhc2hoYXNoaGFzaA")
	if err != nil {
		t.Fatalf("建测试用户失败：%v", err)
	}
	return db, u.ID
}

// TestSession_重启后仍有效 新建会话 → 用全新的 SessionStore（模拟进程重启）→ 同 sid 仍可校验。
func TestSession_重启后仍有效(t *testing.T) {
	db, uid := newSessionTestStore(t)
	ttl := 30 * 24 * time.Hour

	s1 := NewSessionStore(db, ttl, 24*time.Hour)
	sid, csrf, err := s1.Create(uid, true)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}

	// 模拟重启：新进程、同一份数据库。
	s2 := NewSessionStore(db, ttl, 24*time.Hour)
	gotUID, gotCSRF, ok := s2.Get(sid)
	if !ok {
		t.Fatal("重启后会话应仍有效（会话已持久化到 SQLite）")
	}
	if gotUID != uid || gotCSRF != csrf {
		t.Fatalf("重启后 uid/csrf 应原样恢复，实际 uid=%q csrf=%q", gotUID, gotCSRF)
	}
}

// TestSession_两种有效期 remember 会话按 30 天算，普通会话按 24h 算（均以 last_seen 计）。
func TestSession_两种有效期(t *testing.T) {
	db, uid := newSessionTestStore(t)
	rememberTTL := 30 * 24 * time.Hour
	defaultTTL := 24 * time.Hour

	s := NewSessionStore(db, rememberTTL, defaultTTL)
	remSID, _, err := s.Create(uid, true)
	if err != nil {
		t.Fatalf("建 remember 会话失败：%v", err)
	}
	defSID, _, err := s.Create(uid, false)
	if err != nil {
		t.Fatalf("建普通会话失败：%v", err)
	}

	// 把两条会话的 last_seen 都拨回 25 天前。
	old := time.Now().Add(-25 * 24 * time.Hour)
	for _, sid := range []string{remSID, defSID} {
		if err := db.TouchSession(sid, old); err != nil {
			t.Fatalf("拨 last_seen 失败：%v", err)
		}
	}

	if _, _, ok := s.Get(remSID); !ok {
		t.Fatal("remember 会话 25 天前活跃，30 天窗口内应仍有效")
	}
	if _, _, ok := s.Get(defSID); ok {
		t.Fatal("普通会话 25 天前活跃，超过 24h 窗口应失效")
	}
}

// TestSession_GC 清理按双 TTL 分别过期，未到期的不动。
func TestSession_GC(t *testing.T) {
	db, uid := newSessionTestStore(t)
	rememberTTL := 30 * 24 * time.Hour
	defaultTTL := 24 * time.Hour

	s := NewSessionStore(db, rememberTTL, defaultTTL)
	remSID, _, _ := s.Create(uid, true)
	defSID, _, _ := s.Create(uid, false)

	if err := db.GCSessionRows(time.Now(), rememberTTL, defaultTTL); err != nil {
		t.Fatalf("GC 失败：%v", err)
	}
	if _, _, ok := s.Get(remSID); !ok {
		t.Fatal("刚建的 remember 会话不应被 GC")
	}
	if _, _, ok := s.Get(defSID); !ok {
		t.Fatal("刚建的普通会话不应被 GC")
	}

	// 全部拨回 40 天前 → 双双过期清掉。
	old := time.Now().Add(-40 * 24 * time.Hour)
	_ = db.TouchSession(remSID, old)
	_ = db.TouchSession(defSID, old)
	if err := db.GCSessionRows(time.Now(), rememberTTL, defaultTTL); err != nil {
		t.Fatalf("GC 失败：%v", err)
	}
	if _, _, ok := s.Get(remSID); ok {
		t.Fatal("40 天前活跃的 remember 会话应被 GC")
	}
	if _, _, ok := s.Get(defSID); ok {
		t.Fatal("40 天前活跃的普通会话应被 GC")
	}
}

// TestSession_Delete 登出删除后同 sid 立即失效。
func TestSession_Delete(t *testing.T) {
	db, uid := newSessionTestStore(t)
	s := NewSessionStore(db, 30*24*time.Hour, 24*time.Hour)
	sid, _, err := s.Create(uid, true)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}
	s.Delete(sid)
	if _, _, ok := s.Get(sid); ok {
		t.Fatal("登出删除后会话应立即失效")
	}
}
