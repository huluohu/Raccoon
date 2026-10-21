// Package config 实现配置中心的服务端部分：注册表（五要素）+ 默认值 + 环境变量锁定。
// DB 持久化在 Wave 1.2 接入；本文件定义内存模型与解析规则（docs/06）。
package config

import (
	"os"
	"strconv"
)

// Effective 生效时机（docs/06 §2）。
type Effective string

const (
	Immediate Effective = "immediate"
	NextTask  Effective = "next_task"
	Restart   Effective = "restart"
)

// Setting 是五要素注册表的一行（docs/06 §1）。
type Setting struct {
	Key       string    `json:"key"`
	Desc      string    `json:"desc"`
	Default   any       `json:"default"`
	Effective Effective `json:"effective"`
	Sensitive bool      `json:"sensitive"`
	EnvVar    string    `json:"env_var"` // 锁定来源变量名（空=未锁定）
}

// Registry 全部设置的定义；新增设置必须五要素齐全（评审门禁）。
var Registry = []Setting{
	{Key: "server.addr", Desc: "HTTP 监听地址", Default: ":8530", Effective: Restart, EnvVar: "RACCOON_SERVER_ADDR"},
	{Key: "storage.data_dir", Desc: "数据根目录（数据库/配置/凭据/日志/默认下载）", Default: "/data", Effective: Restart, EnvVar: "RACCOON_DATA_DIR"},
	{Key: "storage.download_dir", Desc: "默认下载目录（相对数据根）", Default: "downloads", Effective: NextTask},
	{Key: "storage.naming_rule", Desc: "目录与文件名命名规则", Default: "{platform}/{uploader}/{title}.{ext}", Effective: NextTask},
	{Key: "storage.disk_warn_pct", Desc: "磁盘余量预警百分比", Default: 5, Effective: Immediate},
	{Key: "download.max_concurrent", Desc: "全局下载并发上限", Default: 3, Effective: NextTask},
	{Key: "download.platform_cap", Desc: "单平台并发上限", Default: 2, Effective: NextTask},
	{Key: "download.default_quality", Desc: "默认下载质量", Default: "best", Effective: NextTask},
	{Key: "download.rate_limit_bps", Desc: "下载限速（字节/秒，0=不限）", Default: 0, Effective: NextTask},
	{Key: "download.skip_preview", Desc: "添加任务跳过预览直接入队", Default: false, Effective: NextTask},
	{Key: "download.max_attempts", Desc: "失败自动重试次数", Default: 3, Effective: NextTask},
	{Key: "subscription.interval_min", Desc: "订阅检查间隔（分钟）", Default: 30, Effective: NextTask},
	{Key: "live.check_interval_sec", Desc: "直播开播检测间隔（秒）", Default: 60, Effective: NextTask},
	{Key: "live.segment_min", Desc: "直播录制分段（分钟）", Default: 60, Effective: NextTask},
	{Key: "notify.task_done", Desc: "任务完成通知", Default: true, Effective: Immediate},
	{Key: "notify.task_failed", Desc: "任务失败通知", Default: true, Effective: Immediate},
	{Key: "notify.disk_warn", Desc: "磁盘预警通知", Default: true, Effective: Immediate},
	{Key: "log.level", Desc: "日志级别（debug/info/warn/error）", Default: "info", Effective: Immediate, EnvVar: "RACCOON_LOG_LEVEL"},
}

// Config 是运行期配置快照。锁定项只来自环境变量（docs/06 §4）。
type Config struct {
	values map[string]any
	locked map[string]string // key -> 来源环境变量名
}

// New 用默认值构建，并应用环境变量锁定。
func New() *Config {
	c := &Config{values: map[string]any{}, locked: map[string]string{}}
	for _, s := range Registry {
		c.values[s.Key] = s.Default
	}
	c.applyEnvLocks()
	return c
}

func (c *Config) applyEnvLocks() {
	for _, s := range Registry {
		if s.EnvVar == "" {
			continue
		}
		raw, ok := os.LookupEnv(s.EnvVar)
		if !ok {
			continue
		}
		c.values[s.Key] = coerce(s.Default, raw)
		c.locked[s.Key] = s.EnvVar
	}
}

// coerce 按默认值类型转换环境变量字符串；解析失败保持默认值。
func coerce(def any, raw string) any {
	switch def.(type) {
	case int:
		if n, err := strconv.Atoi(raw); err == nil {
			return n
		}
	case bool:
		if b, err := strconv.ParseBool(raw); err == nil {
			return b
		}
	}
	return raw
}

// Get 取配置值；调用方按已知类型断言。
func (c *Config) Get(key string) any { return c.values[key] }

func (c *Config) GetString(key string) string {
	if v, ok := c.values[key].(string); ok {
		return v
	}
	return ""
}

func (c *Config) GetInt(key string) int {
	if v, ok := c.values[key].(int); ok {
		return v
	}
	return 0
}

func (c *Config) GetBool(key string) bool {
	if v, ok := c.values[key].(bool); ok {
		return v
	}
	return false
}

// LockedBy 返回锁定该键的环境变量名，未锁定返回空串。
func (c *Config) LockedBy(key string) string { return c.locked[key] }

// Set 更新值；锁定键拒绝（调用方转 409 LOCKED_BY_ENV）。
func (c *Config) Set(key string, value any) error {
	if env, locked := c.locked[key]; locked {
		return &LockedError{Key: key, EnvVar: env}
	}
	// 仅接受注册表内已声明的键，防未知键写入。
	for _, s := range Registry {
		if s.Key == key {
			c.values[key] = value
			return nil
		}
	}
	return &UnknownKeyError{Key: key}
}

// Snapshot 返回当前值 + 锁定标记（界面渲染；敏感项由上层打码）。
func (c *Config) Snapshot() map[string]any {
	out := make(map[string]any, len(c.values))
	for k, v := range c.values {
		out[k] = v
	}
	return out
}

type LockedError struct {
	Key, EnvVar string
}

func (e *LockedError) Error() string {
	return "配置项 " + e.Key + " 被环境变量 " + e.EnvVar + " 锁定"
}

type UnknownKeyError struct{ Key string }

func (e *UnknownKeyError) Error() string { return "未注册的配置项: " + e.Key }
