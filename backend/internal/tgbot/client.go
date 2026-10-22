package tgbot

// client.go：Telegram Bot API 的 HTTP 客户端（只用标准库，零第三方依赖）。
//
// 分工：client.go 只做「协议适配」——把 Bot API 的四种方法包成 Go 方法，
// 统一处理鉴权（Token 在 URL 里）、错误转换（ok=false → APIError）与退避重试。
// 业务编排（何时拉取、拿到 update 后干什么）在 bot.go。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/huluohu/raccoon/internal/logx"
)

// maxResponseBytes 响应体读取上限，防止异常响应把内存吃满。
const maxResponseBytes = 8 << 20

// maxErrorBodyBytes 错误响应体摘要上限（截断后进错误消息）。
const maxErrorBodyBytes = 512

// Config 客户端与机器人配置。
type Config struct {
	// Token Telegram Bot Token（必填）。
	//
	// Token 只出现在请求 URL 里，绝不写进日志与错误消息。
	Token string
	// BaseURL API 基址，默认 https://api.telegram.org。
	// 测试时用 httptest 顶替。
	BaseURL string
	// HTTPClient 复用的 HTTP 客户端；为空则新建。
	//
	// 不设全局 Timeout：长轮询的耗时由 context 控制（见 GetUpdates），
	// 用 client 级 Timeout 会把长轮询掐死。
	HTTPClient *http.Client
	// AllowedUsers 允许使用本 Bot 的 Telegram 用户 id 白名单。
	//
	// 空列表 = 全部拒绝（fail-closed）：Bot 是对外暴露的入口，
	// 空名单等于把「谁能提交下载任务」交给陌生人。管理员需显式配置。
	AllowedUsers []int64
	// AllowedChats 允许的会话 id 白名单（可选）。
	//
	// 为空表示不按会话过滤；非空时即使发消息的人在用户白名单里，
	// 群聊也必须在此列表内才放行（防止把私聊权限借给群）。
	AllowedChats []int64
	// PollTimeout 长轮询等待时长（Telegram 侧参数，0-50 秒）。
	// Bot 轮询循环用它；GetUpdates 传入更小的值会覆盖。
	PollTimeout time.Duration
	// RequestTimeout 普通请求（非长轮询）的超时，默认 30 秒。
	RequestTimeout time.Duration
	// MaxRetries 单次请求的最大重试次数（不含首次），默认 3。
	MaxRetries int
	// RetryBackoff 重试退避基数，默认 1 秒（指数退避）。
	RetryBackoff time.Duration
	// AllowedUpdates 关注的更新类型，如 message / callback_query。
	// 为空表示不过滤（由 Telegram 返回全部类型）。
	AllowedUpdates []string
	// Logger 日志器；为空用 logx.Default("tgbot")。
	Logger *logx.Logger
}

// Client Telegram Bot API 客户端。
type Client struct {
	token   string
	baseURL string
	http    *http.Client
	cfg     Config
	log     *logx.Logger
}

// New 构造客户端。
//
// 强制要求 Token：没有 Token 的客户端没有任何意义，早失败好过运行期 401。
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, ErrEmptyToken
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{}
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 30 * time.Second
	}
	if cfg.PollTimeout <= 0 {
		cfg.PollTimeout = 30 * time.Second
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = time.Second
	}
	logger := cfg.Logger
	if logger == nil {
		logger = logx.Default("tgbot")
	}
	return &Client{
		token:   strings.TrimSpace(cfg.Token),
		baseURL: cfg.BaseURL,
		http:    cfg.HTTPClient,
		cfg:     cfg,
		log:     logger,
	}, nil
}

// IsUserAllowed 判断用户 id 是否在白名单内。
//
// 白名单为空 → 一律拒绝（fail-closed，见 Config.AllowedUsers 注释）。
func (c *Client) IsUserAllowed(userID int64) bool {
	if len(c.cfg.AllowedUsers) == 0 {
		return false
	}
	for _, id := range c.cfg.AllowedUsers {
		if id == userID {
			return true
		}
	}
	return false
}

// IsChatAllowed 判断会话 id 是否允许。
//
// 会话白名单为空表示不按会话过滤；非空时必须命中。
func (c *Client) IsChatAllowed(chatID int64) bool {
	if len(c.cfg.AllowedChats) == 0 {
		return true
	}
	for _, id := range c.cfg.AllowedChats {
		if id == chatID {
			return true
		}
	}
	return false
}

// ---- 对外方法 ----

// GetUpdates 拉取入站更新（长轮询）。
//
// offset 是「已确认处理的 update_id + 1」：Telegram 只返回 update_id 大于
// offset 的更新，因此 offset 推进后旧更新不会重复下发。
//
// timeout 是服务端挂起等待的时长（0-50 秒）：没有新消息时服务端会挂住
// 这么久再返回空数组，比「空转轮询」省得多（也省 Telegram 的配额）。
//
// 失败时按退避策略重试：网络抖动、429 限流、5xx 都值得重试；
// 4xx（如 401 token 错）不重试——重试只会继续失败。
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout time.Duration) ([]Update, error) {
	sec := int(timeout / time.Second)
	if sec < 0 {
		sec = 0
	}
	if sec > 50 {
		sec = 50
	}
	q := url.Values{}
	if offset > 0 {
		q.Set("offset", strconv.FormatInt(offset, 10))
	}
	if sec > 0 {
		q.Set("timeout", strconv.Itoa(sec))
	}
	if len(c.cfg.AllowedUpdates) > 0 {
		// allowed_updates 用 JSON 数组字符串传。
		raw, err := json.Marshal(c.cfg.AllowedUpdates)
		if err != nil {
			return nil, fmt.Errorf("tgbot: 序列化 allowed_updates 失败: %w", err)
		}
		q.Set("allowed_updates", string(raw))
	}
	// 长轮询的 HTTP 超时必须大于服务端挂起时长，否则客户端会先于服务端断开。
	callTimeout := time.Duration(sec)*time.Second + 15*time.Second
	reqCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	updates, err := call[[]Update](reqCtx, c, apiRequest{
		method:     MethodGetUpdates,
		httpMethod: http.MethodGet,
		query:      q,
		retry:      retryPolicyFull,
	})
	if err != nil {
		return nil, err
	}
	return updates, nil
}

// SendMessage 发送文本消息。
//
// parseMode 决定文本渲染方式（空串 = 纯文本）。业务侧拿不准时用纯文本，
// 否则 Markdown 语法错误会直接 400。
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string, parseMode string) (*Message, error) {
	form := url.Values{}
	form.Set("chat_id", strconv.FormatInt(chatID, 10))
	form.Set("text", text)
	if parseMode != "" {
		form.Set("parse_mode", parseMode)
	}
	msg, err := call[Message](ctx, c, apiRequest{
		method:     MethodSendMessage,
		httpMethod: http.MethodPost,
		form:       form,
		retry:      retryPolicyRateLimitOnly,
	})
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

// SendDocument 上传并发送本地文件。
//
// 走 multipart/form-data：Telegram 收文件必须用 multipart，表单编码传不了
// 二进制。文件用 io.Pipe 流式上传，不把整个文件读进内存（视频动辄几百 MB）。
func (c *Client) SendDocument(ctx context.Context, chatID int64, filePath string, caption string) (*Message, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("tgbot: 打开待发送文件失败: %w", err)
	}
	defer f.Close()

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		defer pw.Close()
		if err := mw.WriteField("chat_id", strconv.FormatInt(chatID, 10)); err != nil {
			pw.CloseWithError(err)
			return
		}
		if caption != "" {
			if err := mw.WriteField("caption", caption); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		part, err := mw.CreateFormFile("document", filepath.Base(filePath))
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, f); err != nil {
			pw.CloseWithError(err)
			return
		}
		if err := mw.Close(); err != nil {
			pw.CloseWithError(err)
		}
	}()

	msg, err := call[Message](ctx, c, apiRequest{
		method:      MethodSendDocument,
		httpMethod:  http.MethodPost,
		body:        pr,
		contentType: mw.FormDataContentType(),
		retry:       retryPolicyRateLimitOnly,
	})
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

// AnswerCallbackQuery 应答内联按钮回调。
//
// 必须调：Telegram 客户端在收到应答前会一直转圈。text 是提示语（可空）。
func (c *Client) AnswerCallbackQuery(ctx context.Context, callbackID string, text string) error {
	form := url.Values{}
	form.Set("callback_query_id", callbackID)
	if text != "" {
		form.Set("text", text)
	}
	_, err := call[bool](ctx, c, apiRequest{
		method:     MethodAnswerCallbackQuery,
		httpMethod: http.MethodPost,
		form:       form,
		retry:      retryPolicyRateLimitOnly,
	})
	return err
}

// ---- HTTP 底座 ----

// retryPolicy 重试策略。
type retryPolicy int

const (
	// retryPolicyNone 不重试。
	retryPolicyNone retryPolicy = iota
	// retryPolicyRateLimitOnly 只在 429 限流时重试。
	//
	// 发送类接口用它：429 意味着请求根本没被处理，重试安全；
	// 而网络超时/5xx 时服务端可能已经处理过了，重试会发重复消息。
	retryPolicyRateLimitOnly
	// retryPolicyFull 网络错误、429、5xx 都重试。
	//
	// 只用于 getUpdates：它是只读接口，重复请求没有副作用。
	retryPolicyFull
)

// apiRequest 一次 API 调用的描述。
type apiRequest struct {
	// method Telegram 方法名（用于错误信息与日志）。
	method string
	// httpMethod HTTP 动词。
	httpMethod string
	// query URL 查询参数。
	query url.Values
	// form 表单参数（application/x-www-form-urlencoded）。
	form url.Values
	// body 请求体（multipart 上传用）。
	body io.Reader
	// contentType 请求体类型；为空表示无请求体或用表单。
	contentType string
	// retry 重试策略。
	retry retryPolicy
}

// call 发起一次调用并解析响应信封。
//
// 泛型放在包级函数里：Go 不允许方法带类型参数，而各方法 result 形状不同
// （数组 / 对象 / 布尔），只能靠泛型统一。
func call[T any](ctx context.Context, c *Client, req apiRequest) (T, error) {
	var zero T
	raw, _, apiErr, err := c.doWithRetry(ctx, req)
	if err != nil {
		return zero, err
	}
	if apiErr != nil {
		return zero, apiErr
	}
	var env apiEnvelope[T]
	if err := json.Unmarshal(raw, &env); err != nil {
		return zero, &ProtocolError{
			Method: req.method,
			Err:    err,
			Body:   truncate(string(raw), maxErrorBodyBytes),
		}
	}
	return env.Result, nil
}

// doWithRetry 执行请求并按策略重试。
//
// 返回 (响应体, HTTP 状态码, Telegram 业务错误, 传输层错误)。
// 业务错误与传输错误分开返回：429 要重试，4xx 不要，两者的处理路径不同。
func (c *Client) doWithRetry(ctx context.Context, req apiRequest) ([]byte, int, *APIError, error) {
	attempts := 1
	// 带流式 body 的请求（multipart 文件上传）不能重试：body 是一次性的
	// 管道，重试时已经读完，发出去的是空内容。宁可失败也不发坏数据。
	if req.retry != retryPolicyNone && req.body == nil {
		attempts += c.cfg.MaxRetries
	}
	var lastErr error
	var lastAPI *APIError
	for attempt := 1; attempt <= attempts; attempt++ {
		raw, status, apiErr, err := c.attempt(ctx, req)
		if err == nil && apiErr == nil {
			return raw, status, nil, nil
		}
		if err != nil {
			lastErr = err
			lastAPI = nil
		} else {
			lastAPI = apiErr
			lastErr = nil
		}
		if attempt == attempts || !shouldRetry(req.retry, status, apiErr, err != nil) {
			break
		}
		wait := c.backoff(attempt, apiErr)
		c.log.Debug("Telegram 调用失败，准备重试", map[string]any{
			"method":  req.method,
			"attempt": attempt,
			"wait_ms": wait.Milliseconds(),
		})
		select {
		case <-ctx.Done():
			return nil, status, nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	return nil, 0, lastAPI, lastErr
}

// attempt 执行一次请求（不重试）。
func (c *Client) attempt(ctx context.Context, req apiRequest) ([]byte, int, *APIError, error) {
	// 非长轮询请求给一个兜底超时；长轮询的 deadline 已在 GetUpdates 里设过
	// （它必须大于服务端挂起时长，用通用的 RequestTimeout 会把长轮询掐死）。
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.cfg.RequestTimeout)
		defer cancel()
	}
	target := c.baseURL + "/bot" + c.token + "/" + req.method
	if len(req.query) > 0 {
		target += "?" + req.query.Encode()
	}
	var body io.Reader
	contentType := req.contentType
	// 表单编码与流式 body（multipart 上传）二选一：后者优先，
	// 因为它是调用方显式构造的（已经带好了 boundary）。
	if req.body != nil {
		body = req.body
	} else if req.form != nil {
		body = strings.NewReader(req.form.Encode())
		contentType = "application/x-www-form-urlencoded"
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.httpMethod, target, body)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("tgbot: 构造 %s 请求失败: %w", req.method, sanitizeErr(err, c.token))
	}
	if contentType != "" {
		httpReq.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("tgbot: 请求 %s 失败: %w", req.method, sanitizeErr(err, c.token))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, resp.StatusCode, nil, fmt.Errorf("tgbot: 读取 %s 响应失败: %w", req.method, sanitizeErr(err, c.token))
	}
	// Telegram 的 ok=false 语义优先于 HTTP 状态码：它把错误码放在响应体里，
	// HTTP 状态码只是同一个数字。这里统一从响应体解析，解析不出来再退回
	// HTTP 状态码。
	apiErr := parseAPIError(raw, resp.StatusCode, req.method)
	return raw, resp.StatusCode, apiErr, nil
}

// parseAPIError 从响应体解析 Telegram 的错误信封。
//
// 解析失败（响应不是合法 JSON）时按 HTTP 状态码兜底造一个 APIError：
// 这样上层无论遇到哪种失败都只需处理 *APIError 一种类型。
func parseAPIError(raw []byte, status int, method string) *APIError {
	var probe struct {
		// OK 用指针：能区分「ok:false」（Telegram 明确报错）和
		// 「压根没有 ok 字段」（响应不是 Telegram 报文，交给上层按协议错处理）。
		OK          *bool               `json:"ok"`
		ErrorCode   int                 `json:"error_code"`
		Description string              `json:"description"`
		Parameters  *ResponseParameters `json:"parameters"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		if status >= 200 && status < 300 {
			// 2xx 但 body 不是 JSON：不是业务错误，交给上层按协议错误处理。
			return nil
		}
		return &APIError{
			ErrorCode:        status,
			ErrorDescription: truncate(string(raw), maxErrorBodyBytes),
			Method:           method,
		}
	}
	if probe.OK == nil {
		// 有 JSON 但不是 Telegram 信封：同样不是业务错误。
		return nil
	}
	if *probe.OK {
		return nil
	}
	code := probe.ErrorCode
	if code == 0 {
		code = status
	}
	retryAfter := 0
	if probe.Parameters != nil {
		retryAfter = probe.Parameters.RetryAfter
	}
	return &APIError{
		ErrorCode:         code,
		ErrorDescription:  probe.Description,
		Method:            method,
		RetryAfterSeconds: retryAfter,
	}
}

// shouldRetry 判断这次失败是否值得重试。
//
// transportErr 表示是网络层失败（没拿到 HTTP 响应）。
func shouldRetry(policy retryPolicy, status int, apiErr *APIError, transportErr bool) bool {
	switch policy {
	case retryPolicyFull:
		if transportErr {
			// 网络抖动：重试。
			return true
		}
		if status == http.StatusTooManyRequests {
			return true
		}
		// 5xx 是服务端临时故障；4xx（如 401）重试只会继续失败。
		return status >= 500
	case retryPolicyRateLimitOnly:
		if transportErr {
			// 发送类接口遇到网络错误不重试：服务端可能已收到，
			// 重试会发出重复消息。
			return false
		}
		return status == http.StatusTooManyRequests ||
			(apiErr != nil && apiErr.ErrorCode == http.StatusTooManyRequests)
	default:
		return false
	}
}

// backoff 计算第 attempt 次重试前的等待时长（指数退避 + 上限）。
func (c *Client) backoff(attempt int, apiErr *APIError) time.Duration {
	// 429 时按 Telegram 给的 retry_after 等待，最省请求。
	if apiErr != nil && apiErr.RetryAfterSeconds > 0 {
		return time.Duration(apiErr.RetryAfterSeconds) * time.Second
	}
	wait := c.cfg.RetryBackoff
	for i := 1; i < attempt; i++ {
		wait *= 2
		if wait >= 30*time.Second {
			return 30 * time.Second
		}
	}
	return wait
}

// sanitizeErr 清洗错误消息里的 Bot Token。
//
// http.Client 的错误（*url.Error）会把完整 URL 拼进消息，而 URL 里就有
// Token——这等于把密码写进日志。必须洗掉。
func sanitizeErr(err error, token string) error {
	if err == nil {
		return nil
	}
	// 先剥掉 url.Error 外壳：内层错误通常不含 URL。
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		err = urlErr.Err
	}
	if token == "" {
		return err
	}
	msg := err.Error()
	if !strings.Contains(msg, token) {
		return err
	}
	// 仍含 Token（例如内层错误也带了 URL）时只能降级：换成不含原文的新错误。
	return errors.New(strings.ReplaceAll(msg, token, "***"))
}

// truncate 截断字符串，超长时补省略标记。
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…(已截断)"
}
