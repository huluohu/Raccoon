// Package config 实现配置中心的服务端部分：注册表（五要素）+ 默认值 + 环境变量锁定。
// DB 持久化在 Wave 1.2 接入；本文件定义内存模型与解析规则（docs/06）。
package config

import (
	"os"
	"strconv"
	"strings"
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
	{Key: "storage.data_dir", Desc: "数据根目录（数据库/配置/凭据/日志；下载文件在 storage.download_dir）", Default: "/data", Effective: Restart, EnvVar: "RACCOON_DATA_DIR"},
	{Key: "storage.download_dir", Desc: "默认下载目录（绝对路径如 /downloads，或相对数据根；Docker 镜像内由 RACCOON_DOWNLOAD_DIR 锁定为 /downloads）", Default: "downloads", Effective: NextTask, EnvVar: "RACCOON_DOWNLOAD_DIR"},
	{Key: "storage.naming_rule", Desc: "文件命名规则（预留：当前文件名由引擎模板固定为「标题 [ID].扩展名」，修改暂不生效）", Default: "{platform}/{uploader}/{title}.{ext}", Effective: NextTask},
	{Key: "storage.disk_warn_pct", Desc: "磁盘余量预警百分比", Default: 5, Effective: Immediate},
	// ---- 存储策略路由（docs/01 §3.4 / docs/15 §3.7）----
	// local：磁力/种子直接走 qB、其余走本地引擎（存量行为，升级零变化）；
	// n115_first：115 优先，确定失败自动转 qB，结果不确定先查询不转移；
	// qb_fallback：磁力/种子直接走 qB（跳过 115，无运行期转移）——以 task/strategy.go 为准。
	{Key: "storage.strategy", Desc: "存储策略（local/n115_first/qb_fallback）", Default: "local", Effective: NextTask},
	// ---- 网络出口（net.*）----
	// 元数据页可达 ≠ 媒体流可达：不少站点的媒体 CDN 走另一条网络路径，
	// 直连超时（NET_TIMEOUT）时给引擎配代理是用户手里唯一的自救工具。
	// 空串 = 直连（默认行为零变化）；生效时机 NextTask：引擎参数在任务启动时拼装。
	{Key: "net.proxy", Desc: "网络代理（如 http://127.0.0.1:7890 或 socks5://…，留空直连）", Default: "", Effective: NextTask},
	// ---- 成功率预警（docs/14 E2）----
	{Key: "alert.success_rate_threshold", Desc: "成功率预警阈值（百分比，低于则总览浮预警）", Default: 80, Effective: Immediate},
	{Key: "download.max_concurrent", Desc: "全局下载并发上限", Default: 3, Effective: NextTask},
	{Key: "download.platform_cap", Desc: "单平台并发上限", Default: 2, Effective: NextTask},
	{Key: "download.default_quality", Desc: "默认下载质量", Default: "best", Effective: NextTask},
	{Key: "download.rate_limit_bps", Desc: "下载限速（字节/秒，0=不限）", Default: 0, Effective: NextTask},
	{Key: "download.skip_preview", Desc: "添加任务跳过预览直接入队", Default: false, Effective: NextTask},
	{Key: "download.max_attempts", Desc: "失败自动重试次数", Default: 3, Effective: NextTask},
	// 产物增强（docs/04 §2.2 items 说明）：封面落盘是纯下载动作无额外依赖；
	// 元数据内嵌走 yt-dlp 后处理器，mp4/m4a 需要 mutagen（镜像已内置）、
	// mkv/webm 走 ffmpeg。默认关：目录里多出 .jpg/.info 文件不是所有人都想要。
	{Key: "download.save_thumbnail", Desc: "封面图落盘（jpg，与视频同目录）", Default: false, Effective: NextTask},
	{Key: "download.embed_metadata", Desc: "标题/作者等元数据写入媒体文件（需 ffmpeg/mutagen）", Default: false, Effective: NextTask},
	{Key: "subscription.interval_min", Desc: "订阅检查间隔（分钟）", Default: 30, Effective: NextTask},
	{Key: "live.check_interval_sec", Desc: "直播开播检测间隔（秒）", Default: 60, Effective: NextTask},
	{Key: "live.segment_min", Desc: "直播录制分段（分钟）", Default: 60, Effective: NextTask},
	{Key: "notify.task_done", Desc: "任务完成通知", Default: true, Effective: Immediate},
	{Key: "notify.task_failed", Desc: "任务失败通知", Default: true, Effective: Immediate},
	{Key: "notify.disk_warn", Desc: "磁盘预警通知", Default: true, Effective: Immediate},
	// Webhook：任务完成/失败时 POST JSON 到指定 URL（自建通知聚合的通用出口）。
	// 空串 = 不启用；重试 1 次；失败只记日志不影响任务。
	{Key: "notify.webhook_url", Desc: "Webhook 通知地址（POST JSON，留空不启用）", Default: "", Effective: Immediate},
	// Server酱（sct.ftqq.com）：个人微信推送通道。任务完成/失败/磁盘预警时
	// 推到微信「Server酱」服务号。免费额度 5 条/天，超出静默丢弃（失败只记日志）。
	// 这是「微信侧只收通知」的正路：个人微信没有可用的收消息机器人接口，
	// 远程提交下载仍走 Telegram / CLI / 快捷指令（docs/16）。
	{Key: "notify.serverchan_sendkey", Desc: "Server酱 SendKey（微信推送任务通知，留空不启用；免费额度 5 条/天）", Default: "", Effective: Immediate, Sensitive: true},
	{Key: "log.level", Desc: "日志级别（debug/info/warn/error）", Default: "info", Effective: Immediate, EnvVar: "RACCOON_LOG_LEVEL"},
	// 免登录模式（homelab 内网可信场景）：开启后所有 API 匿名放行（伪用户 local），
	// 前端跳过登录页/初始化向导，打开即用。只应部署在隔离网络里——任何能访问
	// 8530 的人都拥有全部权限（提交/删除/凭据/配置）。默认关闭。
	{Key: "security.auth_disabled", Desc: "免登录模式（内网可信环境；开启后任何人可全权访问）", Default: false, Effective: Restart, EnvVar: "RACCOON_AUTH_DISABLED"},
	// ---- 外部集成通道（docs/08 W4.6–W4.8）----
	// 这几项是「服务连接参数」，与凭据中心的「平台账号凭据」是两类东西：
	// 前者指本服务要连到哪里（qB 地址、Bot Token），后者指用谁的账号去下载。
	{Key: "qb.url", Desc: "qBittorrent WebUI 地址（如 http://127.0.0.1:8080）", Default: "", Effective: Immediate},
	{Key: "qb.username", Desc: "qBittorrent 用户名", Default: "", Effective: Immediate},
	{Key: "qb.password", Desc: "qBittorrent 密码", Default: "", Effective: Immediate, Sensitive: true},
	{Key: "n115.access_token", Desc: "115 开放平台 access token", Default: "", Effective: Immediate, Sensitive: true},
	{Key: "n115.refresh_token", Desc: "115 开放平台 refresh token", Default: "", Effective: Immediate, Sensitive: true},
	{Key: "n115.client_id", Desc: "115 开放平台应用 ID", Default: "", Effective: Immediate},
	{Key: "n115.base_url", Desc: "115 业务 API 地址", Default: "https://proapi.115.com", Effective: Immediate},
	{Key: "n115.auth_base_url", Desc: "115 授权 API 地址", Default: "https://passportapi.115.com", Effective: Immediate},
	{Key: "n115.qrcode_base_url", Desc: "115 扫码 API 地址", Default: "https://qrcodeapi.115.com", Effective: Immediate},
	{Key: "telegram.bot_token", Desc: "Telegram Bot Token", Default: "", Effective: Immediate, Sensitive: true},
	{Key: "telegram.allowed_users", Desc: "Telegram 允许的用户 ID（逗号分隔）", Default: "", Effective: Immediate},
	{Key: "telegram.allowed_chats", Desc: "Telegram 允许的会话 ID（逗号分隔，留空不限制）", Default: "", Effective: Immediate},
	// 完成后把产物文件回传到提交会话：小文件（≤50MB，TG bot 上限）直接发文档；
	// 大文件只发文本通知（路径在服务器上，回传不动它）。
	{Key: "telegram.send_files", Desc: "任务完成后回传产物文件到 Telegram（≤50MB）", Default: false, Effective: Immediate},
}

// Config 是运行期配置快照。锁定项只来自环境变量（docs/06 §4）。
type Config struct {
	values  map[string]any
	locked  map[string]string // key -> 来源环境变量名
	version int               // 配置版本（Persist 递增；任务创建时快照）
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

// GetStringList 取列表型配置值（逗号分隔的字符串数组）。
//
// 为什么需要它：Telegram 白名单是「多个 ID」而不是「一个字符串」，
// 但配置表的值是单值 JSON 字符串 —— 用逗号分隔是最省事的表达，
// 也让用户在设置页直接填 "123,456" 而不必理解 JSON。
func (c *Config) GetStringList(key string) []string {
	raw := strings.TrimSpace(c.GetString(key))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

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
