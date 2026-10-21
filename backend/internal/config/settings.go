package config

// settings.go：配置中心的服务端持久化（Wave 1.2 收尾）——settings 表读写 + 配置版本号。
//
// 版本号语义（docs/05 §6 配置版本快照）：每次保存 version+1，任务创建时快照
// 当前版本号落 tasks.config_version；手动重试按原版本回放配置，保证一致性。
// 生效时机（docs/06 §2）：immediate 立即改内存；next_task 不动运行中任务；
// restart 仅写库，下次启动生效。

import (
	"database/sql"
	"encoding/json"
	"time"
)

// LoadFromStore 用 settings 表覆盖默认值（启动时调用；env 锁定项优先，不被库覆盖）。
func (c *Config) LoadFromStore(db *sql.DB) error {
	rows, err := db.Query(`SELECT key, value_json FROM settings`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			return err
		}
		if _, locked := c.locked[key]; locked {
			continue // env 锁定优先（docs/06 §4）
		}
		// 按注册表默认值类型反序列化，保证类型稳定。
		for _, s := range Registry {
			if s.Key != key {
				continue
			}
			var v any
			if err := json.Unmarshal([]byte(raw), &v); err != nil {
				continue // 单行坏数据不阻塞启动
			}
			switch s.Default.(type) {
			case int:
				if f, ok := v.(float64); ok {
					c.values[key] = int(f)
				}
			case bool:
				if b, ok := v.(bool); ok {
					c.values[key] = b
				}
			default:
				c.values[key] = v
			}
		}
	}
	// 版本基线取库里最大 version。
	var maxV sql.NullInt64
	if err := db.QueryRow(`SELECT MAX(version) FROM settings`).Scan(&maxV); err == nil && maxV.Valid {
		c.version = int(maxV.Int64)
	}
	return rows.Err()
}

// Persist 保存单项到 settings 表并推进版本号（docs/06 §3 保存五步流程的写库步）。
// 锁定键与非注册键在上层已拦截；此处防御性再查一次。
func (c *Config) Persist(db *sql.DB, key string, value any, userID string) (int, error) {
	if _, locked := c.locked[key]; locked {
		return 0, &LockedError{Key: key, EnvVar: c.locked[key]}
	}
	def := c.defaultType(key)
	if def == nil {
		return 0, &UnknownKeyError{Key: key}
	}
	if err := checkType(key, value, def); err != nil {
		return 0, err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	now := time.Now().UnixMilli()
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	// upsert + version 递增（同事务）。
	if _, err := tx.Exec(`INSERT INTO settings (key, value_json, effective, sensitive, version, updated_at, updated_by)
		VALUES (?,?,?,?,1,?,?)
		ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json,
		  version=settings.version+1, updated_at=excluded.updated_at, updated_by=excluded.updated_by`,
		key, string(raw), effectiveOf(key), sensitiveOf(key), now, userID); err != nil {
		tx.Rollback()
		return 0, err
	}
	var newV int
	if err := tx.QueryRow(`SELECT version FROM settings WHERE key=?`, key).Scan(&newV); err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	c.values[key] = value
	if newV > c.version {
		c.version = newV
	}
	return newV, nil
}

// Version 返回当前配置版本（任务创建时快照）。
func (c *Config) Version() int { return c.version }

// SettingMeta 返回某键的注册表项指针（供上层 API 渲染五要素）。
func SettingMeta(key string) (Setting, bool) {
	for _, s := range Registry {
		if s.Key == key {
			return s, true
		}
	}
	return Setting{}, false
}

func (c *Config) defaultType(key string) any {
	for _, s := range Registry {
		if s.Key == key {
			return s.Default
		}
	}
	return nil
}

func effectiveOf(key string) Effective {
	if s, ok := SettingMeta(key); ok {
		return s.Effective
	}
	return NextTask
}

func sensitiveOf(key string) bool {
	if s, ok := SettingMeta(key); ok {
		return s.Sensitive
	}
	return false
}

// checkType 强类型校验：int 接受 float64 整数（JSON 解码产物）与 int；bool 同理。
func checkType(key string, value any, def any) error {
	switch def.(type) {
	case int:
		switch v := value.(type) {
		case int, int64, float64:
			if f, ok := toFloat(value); ok && f == float64(int(f)) {
				return nil
			}
			return &TypeError{Key: key, Want: "整数"}
		default:
			_ = v
			return &TypeError{Key: key, Want: "整数"}
		}
	case bool:
		if _, ok := value.(bool); !ok {
			return &TypeError{Key: key, Want: "布尔"}
		}
	case string:
		if _, ok := value.(string); !ok {
			return &TypeError{Key: key, Want: "字符串"}
		}
	}
	return nil
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

// TypeError 配置值类型不符（上层转 400 VALIDATION）。
type TypeError struct {
	Key, Want string
}

func (e *TypeError) Error() string {
	return "配置项 " + e.Key + " 需要" + e.Want + "类型的值"
}
