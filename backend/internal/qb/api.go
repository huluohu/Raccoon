package qb

// api.go：qBittorrent WebUI API v2 的协议契约（路径常量 + 请求/响应结构体）。
//
// 分工：本文件只放"线上格式"——URL 路径、字段名、JSON tag；client.go 只放
// 业务逻辑（登录态维护、参数拼装、结果归一化）。qB 改版时只改本文件。
//
// 重要说明：本环境没有可用的 qBittorrent 实例，全部路径与字段名依据
// qBittorrent 4.1+ 官方 WebUI API v2 文档编写，未在真实实例实测。

import (
	"errors"
	"fmt"
	"strings"
)

// ---- 接口路径 ----
const (
	// PathAuthLogin 登录并下发会话 Cookie（SID），POST 表单（username/password）。
	PathAuthLogin = "/api/v2/auth/login"
	// PathTorrentsAdd 添加种子（磁力链接或 .torrent 文件），POST multipart。
	PathTorrentsAdd = "/api/v2/torrents/add"
	// PathTorrentsInfo 查询种子状态，GET。
	PathTorrentsInfo = "/api/v2/torrents/info"
	// PathTorrentsDelete 删除种子，POST 表单（hashes/deleteFiles）。
	PathTorrentsDelete = "/api/v2/torrents/delete"
	// PathTorrentsCategories 列出分类，GET。
	PathTorrentsCategories = "/api/v2/torrents/categories"
	// PathAppVersion 读取 qB 版本号，GET（返回纯文本，不是 JSON）。
	PathAppVersion = "/api/v2/app/version"
)

// HashSeparator 多个 info_hash 之间的分隔符（线上格式）。
//
// qB 官方文档写的是竖线（|）而不是逗号：WebUI 后端用同一套 split 规则解析
// 所有 hash 列表参数，逗号会被当成 hash 内容的一部分，导致匹配不到任何种子
// 且不报错（静默失败，比报错更难排查）。这里定义成常量是为了让"万一 qB
// 改了分隔符"只改一行。
//
// 注意这是线上格式；调用方手里的哈希可以是切片，也可以是逗号分隔的整串
// 文本，客户端会在发送前摊平（见 splitHashes）。
const HashSeparator = "|"

// addFileField 上传种子文件时的 multipart 字段名。
//
// 官方 WebUI API v2 文档里这个字段就叫 "torrents"（复数），与普通表单字段
// 同名会互相覆盖，所以必须用这个固定名，错了 qB 会当没收到文件。
const addFileField = "torrents"

// ---- 错误类型 ----

var (
	// ErrAuthFailed 登录失败：用户名或密码错误（qB 返回 403）。
	ErrAuthFailed = errors.New("qb: 认证失败")
	// ErrNoSession 请求缺少有效会话：未登录，或 SID 已被 qB 作废。
	ErrNoSession = errors.New("qb: 会话无效")
	// ErrInvalidMagnet 磁力链接格式非法，或取不到 info_hash。
	ErrInvalidMagnet = errors.New("qb: 磁力链接无效")
)

// APIError qB 接口返回非 2xx 时的错误。
//
// 保留状态码与原始响应体：qB 的失败原因（分类不存在、种子重复、文件已损坏
// 等）只写在响应体里，没有结构化错误码，上层要靠它给用户解释具体原因。
type APIError struct {
	// Endpoint 出错的接口路径。
	Endpoint string
	// Status HTTP 状态码。
	Status int
	// Body 响应体摘要（已截断）。
	Body string
}

func (e *APIError) Error() string {
	body := strings.TrimSpace(e.Body)
	if body == "" {
		body = "<空响应体>"
	}
	return fmt.Sprintf("qb: %s 返回 %d: %s", e.Endpoint, e.Status, body)
}

// ---- 上层业务结构 ----

// Torrent 归一后的种子状态。
type Torrent struct {
	// Hash info_hash（小写十六进制），qB 侧的唯一键。
	Hash string
	// RawState qB 原始状态串（如 stalledDL/uploading）。
	RawState string
	// State 归一后的状态（见 StateXxx 常量）。
	State string
	// Progress 下载进度，0..1（qB 原值）。
	Progress float64
	// Percent 下载进度百分比，0..100（由 Progress 换算）。
	Percent float64
	// Size 种子标称大小（字节）。
	Size int64
	// TotalSize 实际要下载的总大小（字节）。
	TotalSize int64
	// SavePath 保存目录。
	SavePath string
	// Name 种子名。
	Name string
	// AddedOn 加入时间（Unix 秒）。
	AddedOn int64
	// Category 所属分类。
	Category string
	// Tags 标签，qB 用逗号分隔返回。
	Tags string
	// DownloadSpeed 下载速度（字节/秒）。
	DownloadSpeed int64
	// UploadSpeed 上传速度（字节/秒）。
	UploadSpeed int64
	// ETA 预计剩余秒数，qB 用 8640000（100 天）表示"未知"。
	ETA int64
	// NumSeeds 已连接的做种数。
	NumSeeds int64
}

// Category qB 里的一个分类。
type Category struct {
	// Name 分类名。
	Name string
	// SavePath 该分类的默认保存目录。
	SavePath string
}

// AddOptions 添加种子时的可选参数。
//
// 零值表示"用 qB 侧默认值"：例如 SavePath 为空就不下发 savepath 字段，
// qB 会用全局默认目录。
type AddOptions struct {
	// SavePath 保存目录（qB 侧绝对路径）。
	SavePath string
	// Category 分类名；qB 侧没有这个分类时接口返回 409。
	Category string
	// Tags 标签列表，提交时用逗号连接（qB 的分隔符）。
	Tags []string
	// Paused 添加后不自动开始下载。
	Paused bool
	// SkipChecking 跳过完整性校验（大种子可省一次全盘 hash）。
	SkipChecking bool
}

// ---- 线上格式 ----

// torrentInfoWire /api/v2/torrents/info 的单条返回。
type torrentInfoWire struct {
	Hash      string  `json:"hash"`
	State     string  `json:"state"`
	Progress  float64 `json:"progress"`
	Size      int64   `json:"size"`
	TotalSize int64   `json:"total_size"`
	SavePath  string  `json:"save_path"`
	Name      string  `json:"name"`
	AddedOn   int64   `json:"added_on"`
	Category  string  `json:"category"`
	Tags      string  `json:"tags"`
	DLSpeed   int64   `json:"dlspeed"`
	UPSpeed   int64   `json:"upspeed"`
	ETA       int64   `json:"eta"`
	NumSeeds  int64   `json:"num_seeds"`
}

// categoryWire /api/v2/torrents/categories 的单条返回。
//
// 外层是 map（分类名 → 详情），qB 用分类名做 key，详情里又重复了一次 name。
type categoryWire struct {
	Name     string `json:"name"`
	SavePath string `json:"savePath"`
}
