package qb

// client.go：qBittorrent WebUI API v2 客户端。
//
// 分工：api.go 只放协议契约（路径 + 线上结构体），本文件只放业务逻辑
// （会话维护、参数拼装、结果归一化）。改协议只动 api.go。
//
// 会话模型：qB 用一个名为 SID 的 Cookie 维持登录态。客户端拿到 SID 后
// 缓存在 Client 里，后续请求都带上它；业务接口返回 403 说明会话已失效
// （qB 重启或被踢下线都会这样），此时自动重登一次并重放请求——种子提交是
// 幂等性要求不高的写操作，重放一次的代价远小于让用户手工重试。

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// defaultTimeout 单次请求的默认超时。
	//
	// 上层轮询种子状态是高频调用，超时必须存在，否则 qB 卡住会把调用方
	// 的 goroutine 一起拖住。
	defaultTimeout = 30 * time.Second

	// maxResponseBytes 响应体读取上限，防止异常响应撑爆内存。
	maxResponseBytes = 8 << 20

	// sessionCookieName 会话 Cookie 名。
	sessionCookieName = "SID"
)

// Config 客户端构造参数。
type Config struct {
	// BaseURL qB WebUI 基址，例如 http://127.0.0.1:8080。
	BaseURL string
	// Username 登录用户名。
	Username string
	// Password 登录密码。
	Password string
	// HTTPClient 实际发起请求的客户端；为空时新建带超时的默认客户端。
	HTTPClient *http.Client
	// Timeout 单次请求超时；为 0 时用 30 秒。
	Timeout time.Duration
	// UserAgent 请求 UA；为空时不设置。
	UserAgent string
}

// Client qBittorrent WebUI API v2 客户端。
//
// 不是并发安全的？——是安全的：会话 Cookie 用互斥量保护，多个 goroutine
// 共用一个 Client 不会互相踩掉登录态。
type Client struct {
	baseURL   string
	username  string
	password  string
	http      *http.Client
	userAgent string
	timeout   time.Duration

	mu  sync.Mutex
	sid string
}

// New 构造客户端。
func New(cfg Config) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	c := &Client{
		baseURL:   strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		username:  cfg.Username,
		password:  cfg.Password,
		http:      cfg.HTTPClient,
		userAgent: cfg.UserAgent,
		timeout:   timeout,
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: timeout}
	}
	return c
}

// ---- 会话 ----

// Login 登录 qB 并保存会话 Cookie（SID）。
//
// qB 对「用户名或密码错误」统一返回 403（响应体固定是 "Fails."），没有更细
// 的错误码，所以 403 直接翻译成 ErrAuthFailed 让上层能区分"凭据错"和
// "网络不通"。
func (c *Client) Login(ctx context.Context) error {
	form := url.Values{}
	form.Set("username", c.username)
	form.Set("password", c.password)

	resp, raw, err := c.send(ctx, apiCall{
		method:      http.MethodPost,
		path:        PathAuthLogin,
		contentType: "application/x-www-form-urlencoded",
		body:        []byte(form.Encode()),
		// 登录请求不带会话 Cookie：拿一个可能已失效的 SID 去登录只会让人
		// 误以为会话还有效。
		noCookie: true,
	})
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%w: 用户名或密码错误", ErrAuthFailed)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{Endpoint: PathAuthLogin, Status: resp.StatusCode, Body: string(raw)}
	}
	sid := ""
	for _, ck := range resp.Cookies() {
		if ck.Name == sessionCookieName {
			sid = ck.Value
			break
		}
	}
	if sid == "" {
		// 没有 SID 就无法复用登录态，后面每个接口都会 403，不如当场报错。
		return fmt.Errorf("qb: %s 登录成功但未返回 %s 会话 Cookie", PathAuthLogin, sessionCookieName)
	}
	c.setSID(sid)
	return nil
}

// IsAvailable 探测 qB 是否可达且能用当前凭据登录。
//
// 判据是"真的登录一次"而不是 ping 端口：端口通不代表 WebUI 正常，更不代表
// 凭据还有效。已缓存的 SID 一律丢弃重登，避免拿一个旧会话冒充可用。
func (c *Client) IsAvailable(ctx context.Context) error {
	c.clearSID()
	if err := c.Login(ctx); err != nil {
		return err
	}
	// 登录成功还不够：再打一个需要登录态的接口，确认会话确实被接受。
	if _, err := c.Version(ctx); err != nil {
		return err
	}
	return nil
}

// setSID 记下会话 Cookie。
func (c *Client) setSID(sid string) {
	c.mu.Lock()
	c.sid = sid
	c.mu.Unlock()
}

// clearSID 丢弃当前会话（下次请求会重新登录）。
func (c *Client) clearSID() {
	c.mu.Lock()
	c.sid = ""
	c.mu.Unlock()
}

// sessionID 取当前会话 Cookie 值。
func (c *Client) sessionID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sid
}

// ---- 种子管理 ----

// AddMagnet 添加磁力链接，返回归一后的 info_hash。
//
// info_hash 在提交前就从链接里解析出来：qB 的 add 接口只回一个 "Ok."，不给
// 哈希，上层要拿哈希入库/去重就得自己解析。
func (c *Client) AddMagnet(ctx context.Context, magnet string, opts AddOptions) (string, error) {
	hash, err := ParseInfoHash(magnet)
	if err != nil {
		return "", err
	}
	fields := opts.formFields()
	fields["urls"] = magnet
	body, contentType, err := buildMultipart(fields, "", nil)
	if err != nil {
		return "", err
	}
	if err := c.doAuth(ctx, apiCall{
		method:      http.MethodPost,
		path:        PathTorrentsAdd,
		contentType: contentType,
		body:        body,
	}, nil); err != nil {
		return "", err
	}
	return hash, nil
}

// AddTorrentFile 以 multipart 方式上传 .torrent 文件。
func (c *Client) AddTorrentFile(ctx context.Context, filename string, data []byte, opts AddOptions) error {
	if len(data) == 0 {
		return errors.New("qb: 种子文件内容为空")
	}
	body, contentType, err := buildMultipart(opts.formFields(), filename, data)
	if err != nil {
		return err
	}
	return c.doAuth(ctx, apiCall{
		method:      http.MethodPost,
		path:        PathTorrentsAdd,
		contentType: contentType,
		body:        body,
	}, nil)
}

// AddTorrentBase64 添加 base64 编码的 .torrent 内容。
//
// qB 的 add 接口只认 multipart 文件段，没有 base64 入口；本方法是为
// "上游手里只有 base64 文本"（例如从配置中心/数据库取出的内容）准备的入口，
// 内部先解码再走与 AddTorrentFile 完全相同的 multipart 通道。
func (c *Client) AddTorrentBase64(ctx context.Context, filename string, encoded string, opts AddOptions) error {
	data, err := decodeBase64(encoded)
	if err != nil {
		return err
	}
	return c.AddTorrentFile(ctx, filename, data, opts)
}

// Torrents 查询种子状态；hashes 为空时返回全部种子。
//
// hashes 可以是切片，也可以是逗号分隔的整串文本（上层从任务表里读出来的
// 常见形态），两种都能传；线上发给 qB 时按 qB 要求的分隔符拼。
func (c *Client) Torrents(ctx context.Context, hashes []string) ([]Torrent, error) {
	query := url.Values{}
	list := splitHashes(hashes)
	if len(list) > 0 {
		query.Set("hashes", strings.Join(list, HashSeparator))
	}
	var wire []torrentInfoWire
	if err := c.doAuth(ctx, apiCall{
		method: http.MethodGet,
		path:   PathTorrentsInfo,
		query:  query,
	}, &wire); err != nil {
		return nil, err
	}
	out := make([]Torrent, 0, len(wire))
	for _, w := range wire {
		out = append(out, convertTorrent(w))
	}
	return out, nil
}

// Delete 删除种子。
//
// hashes 可以是切片，也可以是逗号分隔的整串文本。deleteFiles 为 true 时连同
// 已下载文件一起删除（qB 的 deleteFiles 参数）——这是不可逆的破坏性操作，
// 由调用方决定是否开。
func (c *Client) Delete(ctx context.Context, hashes []string, deleteFiles bool) error {
	list := splitHashes(hashes)
	if len(list) == 0 {
		return errors.New("qb: 未指定要删除的种子哈希")
	}
	form := url.Values{}
	form.Set("hashes", strings.Join(list, HashSeparator))
	form.Set("deleteFiles", strconv.FormatBool(deleteFiles))
	return c.doAuth(ctx, apiCall{
		method:      http.MethodPost,
		path:        PathTorrentsDelete,
		contentType: "application/x-www-form-urlencoded",
		body:        []byte(form.Encode()),
	}, nil)
}

// splitHashes 摊平调用方给的哈希列表。
//
// 上层拿到的哈希常见两种形态：切片（逐个收集），或一整串逗号分隔的文本
// （从数据库字段/任务表里直接读出来的）。这里把两种都拆平，调用方不必
// 自己判断，避免"传进来一串逗号，被当成一个哈希去查，什么都查不到"。
func splitHashes(hashes []string) []string {
	out := make([]string, 0, len(hashes))
	for _, h := range hashes {
		for _, part := range strings.Split(h, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// Categories 列出 qB 上的分类。
//
// qB 返回的是 map（分类名 → 详情），这里转成按名称排序的切片：map 遍历
// 顺序随机，排序后上层展示与测试断言才是稳定的。
func (c *Client) Categories(ctx context.Context) ([]Category, error) {
	var wire map[string]categoryWire
	if err := c.doAuth(ctx, apiCall{
		method: http.MethodGet,
		path:   PathTorrentsCategories,
	}, &wire); err != nil {
		return nil, err
	}
	out := make([]Category, 0, len(wire))
	for name, w := range wire {
		// 外层 key 才是权威分类名，详情里的 name 字段允许为空。
		if w.Name == "" {
			w.Name = name
		}
		out = append(out, Category{Name: w.Name, SavePath: w.SavePath})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Version 返回 qB 版本串（例如 v4.6.0）。
//
// 该接口返回纯文本而不是 JSON，所以走文本分支而不是 JSON 解析。
func (c *Client) Version(ctx context.Context) (string, error) {
	var text string
	if err := c.doAuth(ctx, apiCall{
		method: http.MethodGet,
		path:   PathAppVersion,
	}, &text); err != nil {
		return "", err
	}
	return strings.TrimSpace(text), nil
}

// convertTorrent 把线上结构体转成上层结构体，并做状态与进度归一。
func convertTorrent(w torrentInfoWire) Torrent {
	return Torrent{
		Hash:          w.Hash,
		RawState:      w.State,
		State:         NormalizeTorrentState(w.State, w.Progress),
		Progress:      w.Progress,
		Percent:       ProgressPercent(w.Progress),
		Size:          w.Size,
		TotalSize:     w.TotalSize,
		SavePath:      w.SavePath,
		Name:          w.Name,
		AddedOn:       w.AddedOn,
		Category:      w.Category,
		Tags:          w.Tags,
		DownloadSpeed: w.DLSpeed,
		UploadSpeed:   w.UPSpeed,
		ETA:           w.ETA,
		NumSeeds:      w.NumSeeds,
	}
}

// formFields 把 AddOptions 摊平成 qB 期望的表单字段。
//
// 布尔值统一显式下发 true/false：qB 只认字符串 "true"，省掉 false 字段虽然
// 也对，但显式下发能让"这次到底传了什么"在抓包时一眼看清。
func (o AddOptions) formFields() map[string]string {
	tags := ""
	if len(o.Tags) > 0 {
		tags = strings.Join(o.Tags, ",")
	}
	return map[string]string{
		"savepath":      o.SavePath,
		"category":      o.Category,
		"tags":          tags,
		"paused":        strconv.FormatBool(o.Paused),
		"skip_checking": strconv.FormatBool(o.SkipChecking),
	}
}

// ---- HTTP 底座 ----

// apiCall 描述一次对 qB 接口的调用。
//
// 用结构体而不是一长串位置参数：字段一多就容易把 query 和 body 传反位，
// 而这类错误编译期发现不了。
type apiCall struct {
	method      string
	path        string
	query       url.Values
	contentType string
	// body 请求体（已编码成字节）。
	//
	// 之所以是 []byte 而不是 io.Reader：会话失效时需要原样重放请求，
	// stream 形式的 body 读过一次就没了。
	body []byte
	// noCookie 表示不带会话 Cookie（登录接口用）。
	noCookie bool
}

// doAuth 发起需要登录态的请求。
//
// 没有会话先登录；遇到 403（会话失效）清掉旧 SID 重登一次并重放请求。
func (c *Client) doAuth(ctx context.Context, call apiCall, out any) error {
	if c.sessionID() == "" {
		if err := c.Login(ctx); err != nil {
			return err
		}
	}
	err := c.do(ctx, call, out)
	if !errors.Is(err, ErrNoSession) {
		return err
	}
	// 会话失效：重登后重放一次。请求体是 []byte，可安全重放。
	c.clearSID()
	if err := c.Login(ctx); err != nil {
		return err
	}
	return c.do(ctx, call, out)
}

// do 发起一次请求并解析响应。
//
// out 为 *string 时按纯文本处理（qB 的 version 接口如此），其余按 JSON 解析。
func (c *Client) do(ctx context.Context, call apiCall, out any) error {
	resp, raw, err := c.send(ctx, call)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusForbidden {
		// 业务接口的 403 只会是"会话没了"，与登录接口的 403 含义不同。
		return fmt.Errorf("qb: %s 返回 403，会话已失效: %w", call.path, ErrNoSession)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{Endpoint: call.path, Status: resp.StatusCode, Body: string(raw)}
	}
	switch v := out.(type) {
	case nil:
		return nil
	case *string:
		*v = string(raw)
		return nil
	default:
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("qb: 解析 %s 响应失败: %w", call.path, err)
		}
		return nil
	}
}

// send 发起一次 HTTP 请求，返回响应与响应体。
func (c *Client) send(ctx context.Context, call apiCall) (*http.Response, []byte, error) {
	// 超时落在 ctx 上而不是只依赖 http.Client.Timeout：调用方注入自己的
	// Client 时可能没设超时，这里保证每个请求都有上界。
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	target := c.baseURL + call.path
	if len(call.query) > 0 {
		target += "?" + call.query.Encode()
	}
	var reader io.Reader
	if call.body != nil {
		reader = bytes.NewReader(call.body)
	}
	req, err := http.NewRequestWithContext(ctx, call.method, target, reader)
	if err != nil {
		return nil, nil, fmt.Errorf("qb: 构造 %s 请求失败: %w", call.path, err)
	}
	if call.contentType != "" {
		req.Header.Set("Content-Type", call.contentType)
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	if !call.noCookie {
		if sid := c.sessionID(); sid != "" {
			req.Header.Set("Cookie", sessionCookieName+"="+sid)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, nil, ctxErr
		}
		// url.Error 会把完整 URL 带进消息；这里只留路径。
		return nil, nil, fmt.Errorf("qb: 请求 %s 失败: %w", call.path, unwrapURLError(err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, nil, fmt.Errorf("qb: 读取 %s 响应失败: %w", call.path, err)
	}
	return resp, raw, nil
}

// unwrapURLError 剥掉 url.Error 外壳，只留内层错误。
func unwrapURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return urlErr.Err
	}
	return err
}

// buildMultipart 组装 multipart 请求体。
//
// 字段按键名排序写入：map 遍历顺序随机，虽然 qB 不在意顺序，但固定顺序让
// 抓包与测试断言可复现。
func buildMultipart(fields map[string]string, filename string, fileData []byte) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := w.WriteField(k, fields[k]); err != nil {
			return nil, "", fmt.Errorf("qb: 写入 multipart 字段 %s 失败: %w", k, err)
		}
	}
	if filename != "" {
		// 文件段名必须是 "torrents"，qB 按这个名字挑种子文件。
		part, err := w.CreateFormFile(addFileField, filename)
		if err != nil {
			return nil, "", fmt.Errorf("qb: 创建 multipart 文件段失败: %w", err)
		}
		if _, err := part.Write(fileData); err != nil {
			return nil, "", fmt.Errorf("qb: 写入 multipart 文件段失败: %w", err)
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", fmt.Errorf("qb: 关闭 multipart 失败: %w", err)
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

// decodeBase64 解 base64 文本，容忍缺 padding。
func decodeBase64(encoded string) ([]byte, error) {
	s := strings.TrimSpace(encoded)
	if s == "" {
		return nil, errors.New("qb: base64 种子内容为空")
	}
	if data, err := base64.StdEncoding.DecodeString(s); err == nil {
		return data, nil
	}
	data, err := base64.RawStdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("qb: base64 解码失败: %w", err)
	}
	return data, nil
}
