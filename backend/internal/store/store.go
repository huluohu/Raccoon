// Package store 是 SQLite 唯一数据出口：连接管理（WAL/单写）+ 迁移 runner。
package store

import (
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type migration struct {
	Version  int
	Name     string
	SQL      string
	Checksum string
}

// Store 持有读写连接。SQLite 单写多读：RW 串行化写（busy_timeout 兜底），RO 供查询。
type Store struct {
	RW *sql.DB
	RO *sql.DB
}

// Open 打开（不存在则创建）数据库并应用迁移。
func Open(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o750); err != nil {
		return nil, fmt.Errorf("创建数据库目录: %w", err)
	}
	// _txlock=immediate：写事务立即拿写锁，避免 SQLITE_BUSY 升级死锁。
	rw, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	rw.SetMaxOpenConns(1)
	ro, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=query_only(1)")
	if err != nil {
		rw.Close()
		return nil, err
	}
	ro.SetMaxOpenConns(4)

	s := &Store{RW: rw, RO: ro}
	if err := s.Migrate(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	err1 := s.RW.Close()
	err2 := s.RO.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

// Migrate 按版本号事务内应用未执行迁移；已应用迁移 checksum 不符则拒绝启动（防半迁移）。
func (s *Store) Migrate() error {
	if _, err := s.RW.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, checksum TEXT NOT NULL, applied_at INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("初始化迁移表: %w", err)
	}
	applied := map[int]string{}
	rows, err := s.RW.Query(`SELECT version, checksum FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v int
		var sum string
		if err := rows.Scan(&v, &sum); err != nil {
			rows.Close()
			return err
		}
		applied[v] = sum
	}
	rows.Close()

	migs, err := loadMigrations()
	if err != nil {
		return err
	}
	for _, m := range migs {
		if sum, ok := applied[m.Version]; ok {
			if sum != m.Checksum {
				return fmt.Errorf("迁移 %04d 已应用但内容被改动（防半迁移），拒绝启动", m.Version)
			}
			continue
		}
		tx, err := s.RW.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(m.SQL); err != nil {
			tx.Rollback()
			return fmt.Errorf("应用迁移 %04d: %w", m.Version, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version, name, checksum, applied_at) VALUES (?,?,?,?)`,
			m.Version, m.Name, m.Checksum, time.Now().UnixMilli()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// loadMigrations 解析 migrations/<version>_<name>.up.sql。
func loadMigrations() ([]migration, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	var migs []migration
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		version, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil {
			return nil, fmt.Errorf("迁移文件名缺少版本号: %s", name)
		}
		raw, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(raw)
		migs = append(migs, migration{
			Version:  version,
			Name:     strings.TrimSuffix(name, ".up.sql"),
			SQL:      string(raw),
			Checksum: hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(migs, func(i, j int) bool { return migs[i].Version < migs[j].Version })
	return migs, nil
}
