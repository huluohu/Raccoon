// Package n115 是 115 网盘（115 开放平台 open.115.com）离线下载调度客户端。
//
// 本包只做「协议适配」：把 115 的开放接口包成 Go 方法，供上层任务系统
// 提交离线下载、轮询进度、拉取文件列表、取消任务。业务编排（何时提交、
// 何时重试、下载完成后如何入库）不在本包内。
//
// 重要说明：本环境没有 115 账号，全部接口路径与字段名均「依据 115 开放平台
// 官方文档，未实测」。因此：
//   - 所有路径、字段名集中在本文件（api.go）的常量与结构体 tag 里，
//     便于拿到真实账号后按官方文档逐项核对、修正；
//   - 协议契约（api.go）与业务方法（client.go）严格分离，改字段只动 api.go；
//   - 令牌（access_token / refresh_token / code_verifier）绝不出现在日志与
//     错误消息里：错误摘要统一脱敏并截断。
package n115

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ---- 基础地址 ----
//
// 依据 115 开放平台文档，未实测。115 把能力拆在三个域名上：passportapi 负责
// 授权，proapi 负责业务接口，qrcodeapi 负责扫码状态轮询。三者不是同一个
// 站点，所以 Client 分别持有三个基址（都可注入，测试用 httptest 顶替）。
const (
	DefaultBaseURL       = "https://proapi.115.com"
	DefaultAuthBaseURL   = "https://passportapi.115.com"
	DefaultQRCodeBaseURL = "https://qrcodeapi.115.com"
)

// ---- 接口路径 ----
//
// 依据 115 开放平台文档，未实测。路径常量集中在此处，改版时只改这里。
const (
	// PathAuthDeviceCode 申请设备码（扫码授权入口），POST。
	PathAuthDeviceCode = "/open/authDeviceCode"
	// PathDeviceCodeToToken 用设备码换取 access_token，POST。
	PathDeviceCodeToToken = "/open/deviceCodeToToken"
	// PathRefreshToken 刷新 access_token，POST。
	PathRefreshToken = "/open/refreshToken"
	// PathQRCodeStatus 轮询扫码状态（长轮询），GET。
	PathQRCodeStatus = "/get/status/"
	// PathUserInfo 查询用户信息（校验 token 是否可用），GET。
	PathUserInfo = "/open/user/info"
	// PathOfflineAddTaskURLs 提交离线下载任务（HTTP/FTP/磁力/电驴），POST。
	PathOfflineAddTaskURLs = "/open/offline/add_task_urls"
	// PathOfflineGetTaskList 查询离线任务列表，GET。
	PathOfflineGetTaskList = "/open/offline/get_task_list"
	// PathOfflineDelTask 删除（取消）离线任务，POST。
	PathOfflineDelTask = "/open/offline/del_task"
	// PathUFileFiles 查询目录下的文件列表，GET。
	PathUFileFiles = "/open/ufile/files"
)

// maxErrorBodyBytes 错误响应体摘要的最大字节数。
//
// 115 出错时可能回一大段 HTML/JSON 调试信息；不截断的话错误消息会被撑爆并
// 顺带把上下文里的敏感串带进日志，所以统一按 512 字节封顶。
const maxErrorBodyBytes = 512

// ---- 公共信封 ----

// flexState 兼容 115 不同接口对 state 字段的类型不一致：授权类接口返回数字
// （1 成功 / 0 失败），业务类接口返回布尔（true / false）。统一解析成布尔值，
// 并记录该字段是否真的出现过——有些接口根本不返回 state，只能靠业务码判断。
type flexState struct {
	val bool
	set bool
}

// UnmarshalJSON 解析 state 字段（接受 true/false/1/0 及它们的字符串形式）。
func (s *flexState) UnmarshalJSON(b []byte) error {
	t := strings.Trim(strings.TrimSpace(string(b)), `"`)
	switch t {
	case "true", "1":
		s.val, s.set = true, true
	case "false", "0", "null", "":
		s.val, s.set = false, true
	default:
		return fmt.Errorf("n115: 无法解析 state 字段: %s", t)
	}
	return nil
}

// Bool 返回 state 的布尔值。
func (s flexState) Bool() bool { return s.val }

// Present 返回响应里是否出现过 state 字段。
func (s flexState) Present() bool { return s.set }

// apiEnvelope 115 开放平台统一响应信封。
//
// 依据 115 开放平台文档，未实测。各接口的 data 结构各不相同，统一放在
// apiEnvelope 外层，业务结构体各自定义 data。
type apiEnvelope struct {
	State   flexState `json:"state"`   // 成功标志（数字或布尔，见 flexState）
	Code    int64     `json:"code"`    // 业务码，0 为成功
	Message string    `json:"message"` // 提示信息
	Error   string    `json:"error"`   // 错误信息（部分接口用 error 而非 message）
	Errno   int64     `json:"errno"`   // 错误码（部分接口用 errno 而非 code）
}

// envelope 返回信封自身，供外层结构体以统一方式取用。
func (e *apiEnvelope) envelope() *apiEnvelope { return e }

// ---- 请求结构体（form 编码） ----
//
// 115 的 POST 接口一律 application/x-www-form-urlencoded（依据文档，未实测），
// 因此每个请求结构体都带 form tag；json tag 仅用于阅读时确认字段名。
// 字段名同样集中在此处，改版只改这些 tag。

// deviceCodeRequest 申请设备码请求体。
//
// 依据 115 开放平台《手机扫码授权PKCE模式》文档，未实测。
type deviceCodeRequest struct {
	ClientID            string `json:"client_id"             form:"client_id"`             // 开放平台 APP ID
	CodeChallenge       string `json:"code_challenge"        form:"code_challenge"`        // PKCE 校验值
	CodeChallengeMethod string `json:"code_challenge_method" form:"code_challenge_method"` // 固定 sha256
}

// codeToTokenRequest 用设备码换 token 请求体。
type codeToTokenRequest struct {
	UID          string `json:"uid"           form:"uid"`           // 设备码
	CodeVerifier string `json:"code_verifier" form:"code_verifier"` // PKCE 原值
}

// refreshTokenRequest 刷新 token 请求体。
type refreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" form:"refresh_token"`
}

// addOfflineTaskRequest 提交离线下载任务请求体。
//
// 依据 115 开放平台《添加云下载链接任务》文档，未实测：文档只列出了 urls 与
// wp_path_id；wp_path（保存目录）与 cid 是 115 网页端协议的字段名，本包按
// 调用方需要一并透传，是否被开放平台接受未实测。
type addOfflineTaskRequest struct {
	URLs     string `json:"urls"       form:"urls"`       // 多个链接用换行符分隔
	WPPath   string `json:"wp_path"    form:"wp_path"`    // 保存目录路径
	WPPathID string `json:"wp_path_id" form:"wp_path_id"` // 保存目标文件夹 ID，根目录为 0
	CID      string `json:"cid"        form:"cid"`        // 目标目录 ID（网页端字段）
}

// delOfflineTaskRequest 删除（取消）离线任务请求体。
//
// 依据 115 开放平台《删除用户云下载任务》文档，未实测：参数只有 info_hash 与
// del_source_file，没有独立的「取消」接口，取消语义用删除实现。
type delOfflineTaskRequest struct {
	InfoHash      string `json:"info_hash"       form:"info_hash"`       // 任务 hash
	DelSourceFile int    `json:"del_source_file" form:"del_source_file"` // 1 同时删除已下载源文件
}

// taskListQuery 离线任务列表查询参数。
type taskListQuery struct {
	Page int `json:"page" form:"page"` // 页码，从 1 开始
}

// qrcodeStatusQuery 扫码状态轮询参数。
type qrcodeStatusQuery struct {
	UID  string `json:"uid"  form:"uid"`  // 设备码
	Time int64  `json:"time" form:"time"` // 申请设备码时下发的校验时间戳
	Sign string `json:"sign" form:"sign"` // 申请设备码时下发的签名
}

// fileListQuery 文件列表查询参数。
type fileListQuery struct {
	CID     string `json:"cid"      form:"cid"`      // 目录 ID，根目录为 0
	Limit   int    `json:"limit"    form:"limit"`    // 每页数量，115 上限 1150
	Offset  int    `json:"offset"   form:"offset"`   // 起始偏移
	ShowDir int    `json:"show_dir" form:"show_dir"` // 1 显示子目录
}

// ---- 响应结构体 ----

// deviceCodeData 申请设备码返回的数据。
//
// 依据 115 开放平台《手机扫码授权PKCE模式》文档，未实测：115 返回的是
// uid（设备码）/ time / qrcode（二维码内容）/ sign 四个字段，没有标准
// OAuth2 device flow 里的 device_code / user_code / verification_url /
// qrcode_url。下面这四个标准命名一并解析，兼容后到的协议版本（未实测）。
type deviceCodeData struct {
	UID    string `json:"uid"`    // 设备码
	Time   int64  `json:"time"`   // 轮询扫码状态用的校验时间戳
	QRCode string `json:"qrcode"` // 二维码内容（客户端据此渲染二维码）
	Sign   string `json:"sign"`   // 轮询扫码状态用的签名

	DeviceCode      string `json:"device_code"`      // 标准 OAuth2 命名（115 文档未列出，未实测）
	UserCode        string `json:"user_code"`        // 标准 OAuth2 命名（115 文档未列出，未实测）
	VerificationURL string `json:"verification_url"` // 标准 OAuth2 命名（115 文档未列出，未实测）
	QRCodeURL       string `json:"qrcode_url"`       // 标准 OAuth2 命名（115 文档未列出，未实测）
	ExpiresIn       int64  `json:"expires_in"`       // 设备码有效期（秒）（未实测）
	Interval        int64  `json:"interval"`         // 建议轮询间隔（秒）（未实测）
}

// deviceCodeResponse 申请设备码响应。
type deviceCodeResponse struct {
	apiEnvelope
	Data *deviceCodeData `json:"data"`
}

// qrcodeStatusData 扫码状态数据。
//
// 依据 115 开放平台《手机扫码授权PKCE模式》文档，未实测：status 1 表示已扫码
// 等待确认，2 表示用户已确认授权（此时才可换 token）。
type qrcodeStatusData struct {
	Msg     string `json:"msg"`     // 操作提示
	Status  int    `json:"status"`  // 1 扫码成功待确认；2 确认授权
	Version string `json:"version"` // 接口版本
}

// qrcodeStatusResponse 扫码状态响应。
type qrcodeStatusResponse struct {
	apiEnvelope
	Data *qrcodeStatusData `json:"data"`
}

// tokenData 令牌数据。
//
// 依据 115 开放平台《用设备码换取 access_token》《刷新 access_token》文档，
// 未实测：令牌字段在 data 里；open_id 是标准 OAuth2 命名，115 文档未列出。
type tokenData struct {
	AccessToken  string `json:"access_token"`  // 访问令牌
	RefreshToken string `json:"refresh_token"` // 刷新令牌
	ExpiresIn    int64  `json:"expires_in"`    // access_token 有效期（秒）
	UID          string `json:"uid"`           // 用户 uid
	OpenID       string `json:"open_id"`       // 标准 OAuth2 命名（115 文档未列出，未实测）
}

// tokenResponse 换取/刷新令牌响应。
//
// 115 文档把令牌放在 data 下，但部分实现把 access_token 平铺在顶层，两种都
// 解析，取值时优先用 data。
type tokenResponse struct {
	apiEnvelope
	Data *tokenData `json:"data"`

	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	UID          string `json:"uid"`
	OpenID       string `json:"open_id"`
}

// resolved 返回规范化后的令牌数据：data 优先，其次顶层平铺字段。
func (r *tokenResponse) resolved() tokenData {
	var d tokenData
	if r.Data != nil {
		d = *r.Data
	}
	if d.AccessToken == "" {
		d.AccessToken = r.AccessToken
	}
	if d.RefreshToken == "" {
		d.RefreshToken = r.RefreshToken
	}
	if d.ExpiresIn == 0 {
		d.ExpiresIn = r.ExpiresIn
	}
	if d.UID == "" {
		d.UID = r.UID
	}
	if d.OpenID == "" {
		d.OpenID = r.OpenID
	}
	return d
}

// userInfoData 用户信息数据。
//
// 依据 115 开放平台《获取用户空间和vip信息》文档，未实测：空间信息放在
// rt_space_info 下，会员信息放在 vip_info 下。
type userInfoData struct {
	UserID      int64  `json:"user_id"`   // 用户 id
	UserName    string `json:"user_name"` // 用户名
	OpenID      string `json:"open_id"`   // 标准 OAuth2 命名（115 文档未列出，未实测）
	RtSpaceInfo struct {
		AllTotal  int64 `json:"all_total"`  // 总空间（字节）
		AllRemain int64 `json:"all_remain"` // 剩余空间（字节）
		AllUse    int64 `json:"all_use"`    // 已用空间（字节）
	} `json:"rt_space_info"`
	VipInfo struct {
		LevelName string `json:"level_name"` // 会员等级名
		Expire    int64  `json:"expire"`     // 会员到期时间戳（秒）
	} `json:"vip_info"`
}

// userInfoResponse 用户信息响应。
type userInfoResponse struct {
	apiEnvelope
	Data *userInfoData `json:"data"`
}

// offlineAddTaskItem 单条离线任务提交结果。
//
// 依据 115 开放平台《添加云下载链接任务》文档，未实测：info_hash 只有提交
// 成功时才返回，它就是这条任务后续查询/取消用的标识。
type offlineAddTaskItem struct {
	State       flexState `json:"state"`       // 该条链接的添加状态
	Code        int64     `json:"code"`        // 该条链接的状态码
	Message     string    `json:"message"`     // 该条链接的状态描述
	InfoHash    string    `json:"info_hash"`   // 任务 hash（= offline_id）
	URL         string    `json:"url"`         // 原始链接
	FileID      string    `json:"file_id"`     // 已分配的文件（夹）ID（= task_id）
	Name        string    `json:"name"`        // 任务名
	WpPathID    string    `json:"wp_path_id"`  // 任务所在父文件夹 ID
	AddTime     int64     `json:"add_time"`    // 提交时间戳（秒）
	LastUpdate  int64     `json:"last_update"` // 最后更新时间戳（秒）
	PercentDone int       `json:"percentDone"` // 下载进度（0-100）
	Size        int64     `json:"size"`        // 任务总大小（字节）
}

// offlineAddTaskResponse 提交离线任务响应。
type offlineAddTaskResponse struct {
	apiEnvelope
	Data flexArray[offlineAddTaskItem] `json:"data"`
}

// offlineTaskItem 离线任务条目。
//
// 依据 115 开放平台《获取用户云下载任务列表》文档，未实测。status：-1 失败、
// 0 分配中、1 下载中、2 成功。
type offlineTaskItem struct {
	InfoHash     string `json:"info_hash"`      // 任务 sha1（= offline_id）
	AddTime      int64  `json:"add_time"`       // 添加时间戳（秒）
	PercentDone  int    `json:"percentDone"`    // 下载进度（0-100）
	Size         int64  `json:"size"`           // 任务总大小（字节）
	Name         string `json:"name"`           // 任务名
	LastUpdate   int64  `json:"last_update"`    // 最后更新时间戳（秒）
	FileID       string `json:"file_id"`        // 完成后对应文件（夹）ID（= task_id）
	DeleteFileID string `json:"delete_file_id"` // 删除源文件时需传递的文件（夹）ID
	Status       int    `json:"status"`         // -1 失败；0 分配中；1 下载中；2 成功
	URL          string `json:"url"`            // 原始链接
	WpPathID     string `json:"wp_path_id"`     // 任务所在父文件夹 ID
	Def2         int    `json:"def2"`           // 视频清晰度
	PlayLong     int    `json:"play_long"`      // 视频时长（秒）
	CanAppeal    int    `json:"can_appeal"`     // 是否可申诉
}

// offlineTaskListData 离线任务列表数据。
type offlineTaskListData struct {
	Page      int               `json:"page"`       // 当前页
	PageCount int               `json:"page_count"` // 总页数
	Count     int               `json:"count"`      // 总数量
	Tasks     []offlineTaskItem `json:"tasks"`      // 任务列表
}

// offlineTaskListResponse 离线任务列表响应。
//
// 115 可能把分页信息放在 data 下，也可能平铺在顶层（依据文档与实现差异，未实测），
// 两种都解析。
type offlineTaskListResponse struct {
	apiEnvelope
	Data *offlineTaskListData `json:"data"`

	Page      int               `json:"page"`
	PageCount int               `json:"page_count"`
	Count     int               `json:"count"`
	Tasks     []offlineTaskItem `json:"tasks"`
}

// resolved 返回规范化后的任务列表数据：data 优先，其次顶层平铺字段。
func (r *offlineTaskListResponse) resolved() offlineTaskListData {
	if r.Data != nil && (len(r.Data.Tasks) > 0 || r.Data.Page > 0) {
		return *r.Data
	}
	return offlineTaskListData{
		Page:      r.Page,
		PageCount: r.PageCount,
		Count:     r.Count,
		Tasks:     r.Tasks,
	}
}

// fileItem 目录下的一个文件（夹）。
//
// 依据 115 开放平台《获取文件列表》文档，未实测。fc：0 文件夹、1 文件。
type fileItem struct {
	Fid      string `json:"fid"`       // 文件 ID
	Aid      string `json:"aid"`       // 文件状态：1 正常、7 回收站、120 彻底删除
	Pid      string `json:"pid"`       // 父目录 ID
	Fc       string `json:"fc"`        // 文件分类：0 文件夹、1 文件
	Fn       string `json:"fn"`        // 文件名
	Pc       string `json:"pc"`        // 提取码
	Sha1     string `json:"sha1"`      // sha1
	Fs       int64  `json:"fs"`        // 文件大小（字节）
	Ico      string `json:"ico"`       // 扩展名
	Uet      int64  `json:"uet"`       // 修改时间戳（秒）
	Uppt     int64  `json:"uppt"`      // 上传时间戳（秒）
	Isv      int64  `json:"isv"`       // 是否视频
	Def2     int64  `json:"def2"`      // 视频清晰度
	PlayLong int64  `json:"play_long"` // 音视频时长（秒）
}

// fileListResponse 文件列表响应。
type fileListResponse struct {
	apiEnvelope
	Data   []fileItem `json:"data"`
	Count  int64      `json:"count"`  // 当前目录文件数量
	Limit  int64      `json:"limit"`  // 分页量
	Offset int64      `json:"offset"` // 偏移量
}

// ---- 通用工具 ----

// flexArray 兼容 115 某些接口把 data 同时可能返回数组或对象的情况。
type flexArray[T any] []T

// UnmarshalJSON 数组按数组解析；单对象按单元素数组解析。
func (f *flexArray[T]) UnmarshalJSON(b []byte) error {
	t := strings.TrimSpace(string(b))
	if t == "" || t == "null" {
		*f = nil
		return nil
	}
	if t[0] == '[' {
		var arr []T
		if err := json.Unmarshal(b, &arr); err != nil {
			return err
		}
		*f = arr
		return nil
	}
	var one T
	if err := json.Unmarshal(b, &one); err != nil {
		return err
	}
	*f = []T{one}
	return nil
}

// encodeForm 把带 form tag 的结构体编码成 url.Values。
//
// 115 的 POST 接口只吃表单，不吃 JSON，所以这里手写一个极小的编码器（标准库
// 没有 struct -> url.Values 的实现）。
func encodeForm(v any) url.Values {
	values := url.Values{}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return values
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return values
	}
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("form")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" {
			continue
		}
		field := rv.Field(i)
		switch field.Kind() {
		case reflect.String:
			if field.String() != "" {
				values.Set(name, field.String())
			}
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if field.Int() != 0 {
				values.Set(name, strconv.FormatInt(field.Int(), 10))
			}
		case reflect.Bool:
			if field.Bool() {
				values.Set(name, "1")
			}
		}
	}
	return values
}

// generateCodeVerifier 生成 PKCE 的 code_verifier。
//
// 115 文档要求 43~128 位随机串；这里取 32 字节随机数做 base64url 无填充
// 编码，正好 43 位，符合下限。
func generateCodeVerifier() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("n115: 生成 PKCE 校验原值失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// codeChallenge 计算 PKCE 的 code_challenge。
//
// 依据 115 开放平台文档（未实测）：code_challenge = url_safe(base64(sha256(code_verifier)))，
// 且 2025-04-07 的更新记录说明该参数已兼容 url safe 编码。
func codeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// ---- 错误类型 ----

// APIError 115 接口返回的错误。
//
// Status 是 HTTP 状态码；Code 是 115 业务错误码（信封里的 code/errno）；
// Message 是响应体摘要——已脱敏且截断到 512 字节，不会带出令牌。
type APIError struct {
	Status  int
	Code    string
	Message string
}

// Error 实现 error 接口。
func (e *APIError) Error() string {
	code := e.Code
	if code == "" {
		code = "-"
	}
	return fmt.Sprintf("115 接口错误: HTTP %d, code=%s: %s", e.Status, code, e.Message)
}

// newAPIError 由 HTTP 状态码与响应体构造 APIError。
//
// 响应体优先取信封里的 message/error；取不到才回落到原始 body 摘要。
// secrets 里的字符串会被替换掉，防止令牌泄漏到错误消息里。
func newAPIError(status int, body []byte, secrets ...string) *APIError {
	e := &APIError{Status: status}
	var env struct {
		Code    int64  `json:"code"`
		Message string `json:"message"`
		Error   string `json:"error"`
		Errno   int64  `json:"errno"`
	}
	if err := json.Unmarshal(body, &env); err == nil {
		switch {
		case env.Code != 0:
			e.Code = strconv.FormatInt(env.Code, 10)
		case env.Errno != 0:
			e.Code = strconv.FormatInt(env.Errno, 10)
		}
		msg := env.Message
		if msg == "" {
			msg = env.Error
		}
		if msg != "" {
			e.Message = truncateUTF8(redact(msg, secrets...), maxErrorBodyBytes)
			return e
		}
	}
	e.Message = truncateUTF8(redact(string(body), secrets...), maxErrorBodyBytes)
	return e
}

// envelopeError 依据统一信封判定业务失败，成功返回 nil。
//
// status 是 HTTP 状态码（业务失败时通常仍是 200，115 用信封表达错误）。
//
// 判定用「或」而不是「或」链：115 有的接口失败时只填 code 不填 state，
// 有的只填 state 不填 code，任一不满足即算失败，漏判会静默吞掉错误。
func envelopeError(httpStatus int, st flexState, code, errno int64, message, errText string, secrets ...string) error {
	failed := (code != 0 && code != 200) || errno != 0 || (st.Present() && !st.Bool())
	if !failed {
		return nil
	}
	e := &APIError{Status: httpStatus}
	switch {
	case code != 0:
		e.Code = strconv.FormatInt(code, 10)
	case errno != 0:
		e.Code = strconv.FormatInt(errno, 10)
	}
	msg := message
	if msg == "" {
		msg = errText
	}
	e.Message = truncateUTF8(redact(msg, secrets...), maxErrorBodyBytes)
	return e
}

// secretPattern 匹配响应体里可能出现的令牌字段。
var secretPattern = regexp.MustCompile(`(?i)("?(?:access_token|refresh_token|code_verifier|device_code|token)"?\s*[:=]\s*"?)([^",}\s]+)`)

// redact 把消息里的令牌片段替换掉。
//
// 已知明文（secrets）直接替换；未知的令牌字段按名字擦除。两条保险一起上，
// 避免 115 调试信息把凭据回显出来。
func redact(s string, secrets ...string) string {
	for _, secret := range secrets {
		if len(secret) >= 8 {
			s = strings.ReplaceAll(s, secret, "***")
		}
	}
	return secretPattern.ReplaceAllString(s, "${1}***")
}

// truncateUTF8 按字节截断字符串，且不切断多字节字符。
func truncateUTF8(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit - len("…")
	if cut < 0 {
		cut = 0
	}
	for cut > 0 && !utf8.ValidString(s[:cut]) {
		cut--
	}
	return s[:cut] + "…"
}
