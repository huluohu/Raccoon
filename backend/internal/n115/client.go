package n115

// client.go：115 开放平台客户端与业务方法。
//
// 分工：api.go 只放协议契约（路径常量 + 请求/响应结构体），client.go 只放业务
// 逻辑（状态机、参数拼装、结果归一化）。改协议只动 api.go，改流程只动本文件。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrNoToken 未取到可用的 access_token。
//
// 上层没配 115 凭据、或凭据已失效时返回它，便于上层区分「未授权」与
// 「网络/协议错误」。
var ErrNoToken = errors.New("n115: 未配置或未取到 access_token")

// ErrNotFound 远端查不到对应对象。
var ErrNotFound = errors.New("n115: 远端对象不存在")

// TokenProvider 提供当前可用的 access_token。
//
// 抽成接口是为了让上层接凭据中心（凭据中心解密、过期刷新、多账号切换都在
// 那一侧做），本包不持有也不落盘任何令牌。
type TokenProvider interface {
	Token(ctx context.Context) (string, error)
}

// TokenRefresher 是 TokenProvider 的可选扩展。
//
// 实现了它，Client 就能在需要时自行刷新令牌并把新令牌回写给上层；凭据中心
// 实现这个接口即可拿到「刷新 + 持久化」的完整闭环。
type TokenRefresher interface {
	TokenProvider
	// RefreshToken 返回当前有效的 refresh_token。
	RefreshToken(ctx context.Context) (string, error)
	// SaveTokens 持久化刷新后的令牌。expiresAt 为 access_token 的到期时间。
	SaveTokens(ctx context.Context, accessToken, refreshToken string, expiresAt time.Time) error
}

// Config 客户端构造参数。
type Config struct {
	// BaseURL 业务接口基址（离线任务、文件列表、用户信息），默认 115 官方地址。
	BaseURL string
	// AuthBaseURL 授权接口基址（设备码、换令牌、刷新令牌），默认 115 官方地址。
	AuthBaseURL string
	// QRCodeBaseURL 扫码状态轮询基址，默认 115 官方地址。
	QRCodeBaseURL string
	// HTTPClient 实际发起请求的客户端；为空时用带超时的默认客户端。
	HTTPClient *http.Client
	// TokenProvider 令牌来源；为空时所有需要鉴权的接口返回 ErrNoToken。
	TokenProvider TokenProvider
	// ClientID 开放平台 APP ID，申请设备码时必填。
	ClientID string
	// UserAgent 请求 UA；为空时不设置。
	UserAgent string
	// DeviceCodeTTL 设备码有效期兜底值。115 文档未给出设备码有效期（未实测），
	// 这里用 10 分钟做保守兜底。
	DeviceCodeTTL time.Duration
	// PollInterval 扫码状态轮询间隔兜底值，默认 3 秒。
	PollInterval time.Duration
}

// Client 115 开放平台客户端。
//
// 三个基址分开持有：115 把授权、业务、扫码轮询分在三个域名上（依据官方
// 文档，未实测），统一成一个 baseURL 会在真实环境里打错主机。
type Client struct {
	baseURL       string
	authBaseURL   string
	qrcodeBaseURL string
	http          *http.Client
	tokens        TokenProvider
	clientID      string
	userAgent     string
	deviceCodeTTL time.Duration
	pollInterval  time.Duration
}

// New 构造客户端。
//
// 未注入的基址回落到 115 官方地址，未注入的 http.Client 用 30 秒超时的默认
// 客户端（扫码状态是长轮询，超时不能太短）。
func New(cfg Config) *Client {
	c := &Client{
		baseURL:       strings.TrimRight(orDefault(cfg.BaseURL, DefaultBaseURL), "/"),
		authBaseURL:   strings.TrimRight(orDefault(cfg.AuthBaseURL, DefaultAuthBaseURL), "/"),
		qrcodeBaseURL: strings.TrimRight(orDefault(cfg.QRCodeBaseURL, DefaultQRCodeBaseURL), "/"),
		http:          cfg.HTTPClient,
		tokens:        cfg.TokenProvider,
		clientID:      cfg.ClientID,
		userAgent:     cfg.UserAgent,
		deviceCodeTTL: cfg.DeviceCodeTTL,
		pollInterval:  cfg.PollInterval,
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: 30 * time.Second}
	}
	if c.deviceCodeTTL <= 0 {
		c.deviceCodeTTL = 10 * time.Minute
	}
	if c.pollInterval <= 0 {
		c.pollInterval = 3 * time.Second
	}
	return c
}

// orDefault 取非空值，否则用默认值。
func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// ---- 授权状态机 ----

// AuthStatus 设备授权流程的状态。
type AuthStatus string

const (
	// AuthStatusPending 已申请到设备码，等待用户扫码/确认。
	AuthStatusPending AuthStatus = "pending"
	// AuthStatusAuthorized 已拿到 access_token。
	AuthStatusAuthorized AuthStatus = "authorized"
	// AuthStatusExpired 设备码过期或二维码失效，需要重新申请。
	AuthStatusExpired AuthStatus = "expired"
	// AuthStatusFailed 授权过程出错，详见 AuthState.LastError。
	AuthStatusFailed AuthStatus = "failed"
)

// AuthState 设备授权流程的状态快照。
//
// 一次流程的顺序：RequestDeviceCode 拿到 pending 的状态 → 用户扫码 → 轮询到
// status=2 换到令牌 → 状态变 authorized。整个过程任何一步出错都会把状态
// 置为 failed 并把原因写进 LastError；设备码过期置为 expired（需要重新申请，
// 不是错误）。
type AuthState struct {
	// Status 当前状态机的位置。
	Status AuthStatus
	// UserCode 用户可见的授权码。
	//
	// 注意：115 官方的 PKCE 设备码模式只下发 uid/qrcode/sign，没有独立的
	// user_code（依据官方文档，未实测）；本字段用于兼容标准 OAuth2 设备授权
	// 的字段形态，115 未下发时为空。
	UserCode string
	// DeviceCode 设备码。115 侧对应 data.uid。
	DeviceCode string
	// VerificationURL 用户完成授权的地址。115 官方模式未下发（未实测）。
	VerificationURL string
	// QRCodeURL 二维码地址。115 官方模式未下发（未实测）。
	QRCodeURL string
	// QRCodeContent 二维码内容，客户端据此渲染二维码给用户扫。
	//
	// 115 官方模式下发的是 data.qrcode（内容，不是 URL），依据官方文档，
	// 未实测。
	QRCodeContent string
	// PollTime 轮询扫码状态要带的时间戳（115 的 data.time）。
	PollTime int64
	// PollSign 轮询扫码状态要带的签名（115 的 data.sign）。
	PollSign string
	// CodeVerifier PKCE 校验原值，换令牌时要原样带回，不能泄露。
	CodeVerifier string
	// ExpiresAt 设备码到期时间。
	ExpiresAt time.Time
	// Interval 建议的轮询间隔。
	Interval time.Duration
	// AccessToken 访问令牌（授权成功后有值）。
	AccessToken string
	// RefreshToken 刷新令牌（授权成功后有值）。
	RefreshToken string
	// UID 用户 uid。
	UID string
	// OpenID 开放平台用户标识。115 官方文档未列出（未实测）。
	OpenID string
	// ExpiresIn access_token 有效期（秒）。
	ExpiresIn int64
	// LastError 失败原因摘要（不含令牌）。
	LastError string
	// UpdatedAt 状态最后更新时间。
	UpdatedAt time.Time
}

// Authorized 返回是否已授权。
func (s *AuthState) Authorized() bool { return s != nil && s.Status == AuthStatusAuthorized }

// String 返回脱敏后的可读描述。
//
// 刻意实现 Stringer：AuthState 带着令牌，日志里打 %v 是最常见的用法，
// 不做脱敏就会把 access_token 写进日志。
func (s *AuthState) String() string {
	if s == nil {
		return "<nil>"
	}
	return fmt.Sprintf("AuthState{Status:%s DeviceCode:%s AccessToken:%s ExpiresAt:%s}",
		s.Status, s.DeviceCode, maskSecret(s.AccessToken), s.ExpiresAt.Format(time.RFC3339))
}

// maskSecret 把敏感串打码，只留首尾各 2 位。
func maskSecret(v string) string {
	if v == "" {
		return ""
	}
	r := []rune(v)
	if len(r) <= 8 {
		return "****"
	}
	return string(r[:2]) + "****" + string(r[len(r)-2:])
}

// ---- 设备授权流程 ----

// RequestDeviceCode 申请设备码与二维码内容。
//
// 依据 115 开放平台《手机扫码授权PKCE模式》文档（未实测）：POST 提交
// client_id + code_challenge + code_challenge_method=sha256，返回 uid/time/
// qrcode/sign。本方法同时解析标准 OAuth2 设备授权的 device_code、user_code、
// verification_url、qrcode_url 字段（115 未下发时留空）。
func (c *Client) RequestDeviceCode(ctx context.Context) (*AuthState, error) {
	if strings.TrimSpace(c.clientID) == "" {
		return nil, errors.New("n115: 未配置开放平台 ClientID，无法申请设备码")
	}
	verifier, err := generateCodeVerifier()
	if err != nil {
		return nil, err
	}
	form := encodeForm(deviceCodeRequest{
		ClientID:            c.clientID,
		CodeChallenge:       codeChallenge(verifier),
		CodeChallengeMethod: "sha256",
	})
	var resp deviceCodeResponse
	if err := c.do(ctx, apiCall{
		method:   http.MethodPost,
		rawURL:   c.authBaseURL + PathAuthDeviceCode,
		form:     form,
		needAuth: false,
	}, &resp); err != nil {
		return nil, err
	}
	if resp.Data == nil {
		return nil, errors.New("n115: 申请设备码返回空数据")
	}
	d := resp.Data
	now := time.Now()
	state := &AuthState{
		Status:          AuthStatusPending,
		DeviceCode:      firstNonEmpty(d.UID, d.DeviceCode),
		UserCode:        d.UserCode,
		VerificationURL: d.VerificationURL,
		QRCodeURL:       d.QRCodeURL,
		QRCodeContent:   d.QRCode,
		PollTime:        d.Time,
		PollSign:        d.Sign,
		CodeVerifier:    verifier,
		Interval:        time.Duration(d.Interval) * time.Second,
		UpdatedAt:       now,
	}
	if state.Interval <= 0 {
		state.Interval = c.pollInterval
	}
	// 115 官方文档未给出设备码有效期（未实测），用兜底值推进到期时间。
	ttl := time.Duration(d.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = c.deviceCodeTTL
	}
	state.ExpiresAt = now.Add(ttl)
	if state.DeviceCode == "" {
		return nil, errors.New("n115: 申请设备码未返回设备码")
	}
	return state, nil
}

// PollDeviceStatus 轮询一次扫码状态。
//
// 依据 115 开放平台文档（未实测）：GET /get/status/?uid=&time=&sign=，
// state=1 表示继续轮询、state=0 表示二维码失效（结束轮询）；data.status=1
// 表示已扫码待确认、data.status=2 表示用户已确认授权（此时换令牌）。
//
// 该接口是长轮询：状态不变时服务端不一定立刻返回，调用方需要给足超时。
// 确认授权后本方法内部直接换到令牌，返回的 AuthState.Status 为 authorized。
func (c *Client) PollDeviceStatus(ctx context.Context, state *AuthState) (*AuthState, error) {
	if state == nil || state.DeviceCode == "" {
		return nil, errors.New("n115: 缺少设备码，无法轮询扫码状态")
	}
	if state.Status == AuthStatusExpired {
		return state, nil
	}
	form := encodeForm(qrcodeStatusQuery{
		UID:  state.DeviceCode,
		Time: state.PollTime,
		Sign: state.PollSign,
	})
	var resp qrcodeStatusResponse
	if err := c.do(ctx, apiCall{
		method:       http.MethodGet,
		rawURL:       c.qrcodeBaseURL + PathQRCodeStatus,
		form:         form,
		skipEnvelope: true,
	}, &resp); err != nil {
		state.Status = AuthStatusFailed
		state.LastError = err.Error()
		state.UpdatedAt = time.Now()
		return state, err
	}
	// 扫码状态接口的 state 语义与其它接口相反：0 表示二维码失效、结束轮询，
	// 1 才是继续轮询，所以不能套用通用信封判定，这里单独处理。
	if resp.Code != 0 && resp.Code != 200 {
		state.Status = AuthStatusFailed
		state.LastError = resp.Message
		state.UpdatedAt = time.Now()
		return state, envelopeError(http.StatusOK, resp.State, resp.Code, resp.Errno, resp.Message, resp.Error)
	}
	// state=0 明确表示二维码失效，设备码不能再用。
	if resp.State.Present() && !resp.State.Bool() {
		state.Status = AuthStatusExpired
		state.LastError = "扫码二维码已失效，请重新申请设备码"
		state.UpdatedAt = time.Now()
		return state, nil
	}
	// data 只在用户扫码或输入设备码之后才有值；status=2 表示已确认授权。
	if resp.Data != nil && resp.Data.Status == 2 {
		return c.ExchangeDeviceCode(ctx, state)
	}
	state.Status = AuthStatusPending
	if resp.Data != nil && resp.Data.Msg != "" {
		state.LastError = resp.Data.Msg
	}
	state.UpdatedAt = time.Now()
	return state, nil
}

// ExchangeDeviceCode 用设备码换取 access_token。
//
// 依据 115 开放平台《用设备码换取 access_token》文档（未实测）：POST
// uid + code_verifier，返回 access_token / refresh_token / expires_in。
func (c *Client) ExchangeDeviceCode(ctx context.Context, state *AuthState) (*AuthState, error) {
	if state == nil || state.DeviceCode == "" || state.CodeVerifier == "" {
		return nil, errors.New("n115: 缺少设备码或 PKCE 校验原值，无法换取令牌")
	}
	form := encodeForm(codeToTokenRequest{
		UID:          state.DeviceCode,
		CodeVerifier: state.CodeVerifier,
	})
	var resp tokenResponse
	if err := c.do(ctx, apiCall{
		method: http.MethodPost,
		rawURL: c.authBaseURL + PathDeviceCodeToToken,
		form:   form,
	}, &resp); err != nil {
		state.Status = AuthStatusFailed
		state.LastError = err.Error()
		state.UpdatedAt = time.Now()
		return state, err
	}
	d := resp.resolved()
	if d.AccessToken == "" {
		err := errors.New("n115: 换取令牌未返回 access_token")
		state.Status = AuthStatusFailed
		state.LastError = err.Error()
		state.UpdatedAt = time.Now()
		return state, err
	}
	state.Status = AuthStatusAuthorized
	state.AccessToken = d.AccessToken
	state.RefreshToken = d.RefreshToken
	state.UID = firstNonEmpty(d.UID, state.UID)
	state.OpenID = d.OpenID
	state.ExpiresIn = d.ExpiresIn
	state.LastError = ""
	state.UpdatedAt = time.Now()
	return state, nil
}

// AwaitAuthorization 循环轮询直到用户完成授权。
//
// 设备码过期、轮询出错、上下文取消都会停止等待并把状态写进返回的 AuthState。
// 成功时返回的 AuthState.Status 为 authorized，且带上了 access_token。
func (c *Client) AwaitAuthorization(ctx context.Context, state *AuthState) (*AuthState, error) {
	if state == nil {
		return nil, errors.New("n115: 缺少授权状态")
	}
	interval := state.Interval
	if interval <= 0 {
		interval = c.pollInterval
	}
	for {
		if err := ctx.Err(); err != nil {
			state.Status = AuthStatusFailed
			state.LastError = err.Error()
			state.UpdatedAt = time.Now()
			return state, err
		}
		if !state.ExpiresAt.IsZero() && time.Now().After(state.ExpiresAt) {
			state.Status = AuthStatusExpired
			state.LastError = "设备码已过期，请重新申请"
			state.UpdatedAt = time.Now()
			return state, nil
		}
		next, err := c.PollDeviceStatus(ctx, state)
		if next != nil {
			state = next
		}
		if err != nil {
			return state, err
		}
		switch state.Status {
		case AuthStatusAuthorized, AuthStatusExpired, AuthStatusFailed:
			return state, nil
		}
		select {
		case <-ctx.Done():
			state.Status = AuthStatusFailed
			state.LastError = ctx.Err().Error()
			state.UpdatedAt = time.Now()
			return state, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// ---- 令牌刷新 ----

// TokenPair 令牌及其有效期。
type TokenPair struct {
	AccessToken  string
	RefreshToken string
	// ExpiresIn access_token 有效期（秒）。
	ExpiresIn int64
	// ExpiresAt access_token 到期时间。
	ExpiresAt time.Time
}

// String 返回脱敏后的可读描述。
//
// 同 AuthState.String：令牌不能进日志。
func (p *TokenPair) String() string {
	if p == nil {
		return "<nil>"
	}
	return fmt.Sprintf("TokenPair{AccessToken:%s RefreshToken:%s ExpiresAt:%s}",
		maskSecret(p.AccessToken), maskSecret(p.RefreshToken), p.ExpiresAt.Format(time.RFC3339))
}

// RefreshToken 用 refresh_token 换新的 access_token。
//
// 依据 115 开放平台《刷新 access_token》文档（未实测）：POST
// refresh_token，返回新的 access_token / refresh_token / expires_in。
//
// 115 对刷新有 IP 频控（文档明确提示不要频繁刷新），所以这里不做自动重试，
// 由上层按需调用。
func (c *Client) RefreshToken(ctx context.Context, refreshToken string) (*TokenPair, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return nil, errors.New("n115: 缺少 refresh_token，无法刷新令牌")
	}
	form := encodeForm(refreshTokenRequest{RefreshToken: refreshToken})
	var resp tokenResponse
	if err := c.do(ctx, apiCall{
		method:  http.MethodPost,
		rawURL:  c.authBaseURL + PathRefreshToken,
		form:    form,
		secrets: []string{refreshToken},
	}, &resp); err != nil {
		return nil, err
	}
	d := resp.resolved()
	if d.AccessToken == "" {
		return nil, errors.New("n115: 刷新令牌未返回 access_token")
	}
	pair := &TokenPair{
		AccessToken:  d.AccessToken,
		RefreshToken: firstNonEmpty(d.RefreshToken, refreshToken),
		ExpiresIn:    d.ExpiresIn,
	}
	if d.ExpiresIn > 0 {
		pair.ExpiresAt = time.Now().Add(time.Duration(d.ExpiresIn) * time.Second)
	}
	return pair, nil
}

// RefreshFromProvider 从令牌提供器取 refresh_token 刷新，并把新令牌回写。
//
// 只有当配置的 TokenProvider 同时实现了 TokenRefresher 时可用；否则返回
// ErrNoToken。回写失败同样返回错误——令牌没落盘就等于没刷新成功。
func (c *Client) RefreshFromProvider(ctx context.Context) (*TokenPair, error) {
	refresher, ok := c.tokens.(TokenRefresher)
	if !ok {
		return nil, ErrNoToken
	}
	refreshToken, err := refresher.RefreshToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("n115: 取 refresh_token 失败: %w", err)
	}
	pair, err := c.RefreshToken(ctx, refreshToken)
	if err != nil {
		return nil, err
	}
	if err := refresher.SaveTokens(ctx, pair.AccessToken, pair.RefreshToken, pair.ExpiresAt); err != nil {
		return nil, fmt.Errorf("n115: 保存刷新后的令牌失败: %w", err)
	}
	return pair, nil
}

// ---- 离线下载任务 ----

// OfflineStatus 离线任务状态。
type OfflineStatus string

const (
	// OfflineStatusPending 已提交、排队中。
	OfflineStatusPending OfflineStatus = "pending"
	// OfflineStatusRunning 下载中。
	OfflineStatusRunning OfflineStatus = "running"
	// OfflineStatusDone 下载完成。
	OfflineStatusDone OfflineStatus = "done"
	// OfflineStatusFailed 下载失败。
	OfflineStatusFailed OfflineStatus = "failed"
)

// OfflineTask 离线下载任务状态。
type OfflineTask struct {
	// OfflineID 任务标识，对应 115 的 info_hash，查询/取消都用它。
	OfflineID string
	// TaskID 任务完成后对应网盘文件（夹）的 ID，对应 115 的 file_id。
	// 任务未完成时为空（此时远端还没分配文件）。
	TaskID string
	// Name 任务名。
	Name string
	// URL 原始下载链接。
	URL string
	// Status 任务状态。
	Status OfflineStatus
	// Progress 下载进度（0-100）。
	Progress int
	// Size 任务总大小（字节）。
	Size int64
	// WpPathID 任务所在父文件夹 ID。
	WpPathID string
	// AddTime 提交时间戳（秒）。
	AddTime int64
	// LastUpdate 最后更新时间戳（秒）。
	LastUpdate int64
	// Files 任务完成后目标目录下的文件列表；仅在按需拉取时填充。
	Files []File
}

// OfflineRequest 提交离线下载任务的参数。
type OfflineRequest struct {
	// URL 下载链接，支持 magnet / http(s) / ftp / ed2k（依据官方文档，未实测）。
	URL string
	// WPPath 保存目录路径。
	WPPath string
	// WPPathID 保存目标文件夹 ID，根目录为 "0"。
	WPPathID string
	// CID 目标目录 ID（115 网页端字段名，官方开放平台文档未列出，未实测）。
	CID string
}

// SubmitOffline 提交离线下载任务。
//
// 依据 115 开放平台《添加云下载链接任务》文档（未实测）：POST 提交 urls
// （多链接换行分隔）与 wp_path_id，返回的 data 里带 info_hash——它就是后续
// 查询与取消用的 offline_id。
//
// 注意：115 只在任务完成后才分配 file_id，所以刚提交时返回的 TaskID 为空，
// 需要轮询任务列表才能拿到。
func (c *Client) SubmitOffline(ctx context.Context, req OfflineRequest) (*OfflineTask, error) {
	if strings.TrimSpace(req.URL) == "" {
		return nil, errors.New("n115: 离线任务缺少下载链接")
	}
	form := encodeForm(addOfflineTaskRequest{
		URLs:     req.URL,
		WPPath:   req.WPPath,
		WPPathID: req.WPPathID,
		CID:      req.CID,
	})
	var resp offlineAddTaskResponse
	if err := c.do(ctx, apiCall{
		method:   http.MethodPost,
		rawURL:   c.baseURL + PathOfflineAddTaskURLs,
		form:     form,
		needAuth: true,
	}, &resp); err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, errors.New("n115: 提交离线任务未返回任务信息")
	}
	// 多链接提交时逐条给结果；任一条成功即认为提交成功，取第一条成功的。
	var lastErr string
	for _, item := range resp.Data {
		if item.State.Present() && !item.State.Bool() {
			lastErr = item.Message
			continue
		}
		if item.InfoHash == "" {
			lastErr = item.Message
			continue
		}
		return &OfflineTask{
			OfflineID:  item.InfoHash,
			TaskID:     item.FileID,
			URL:        item.URL,
			Name:       item.Name,
			Status:     OfflineStatusPending,
			WpPathID:   item.WpPathID,
			AddTime:    item.AddTime,
			LastUpdate: item.LastUpdate,
			Progress:   item.PercentDone,
			Size:       item.Size,
		}, nil
	}
	if lastErr == "" {
		lastErr = "未知原因"
	}
	return nil, fmt.Errorf("n115: 提交离线任务失败: %s", lastErr)
}

// GetOfflineTask 查询单个离线任务。
//
// ref 可以是 offline_id（115 的 info_hash）或 task_id（115 的 file_id）。
//
// 依据 115 开放平台文档（未实测），开放平台只提供任务列表接口，没有单任务
// 查询接口，所以这里按页遍历列表匹配，找到即返回。
func (c *Client) GetOfflineTask(ctx context.Context, ref string) (*OfflineTask, error) {
	if strings.TrimSpace(ref) == "" {
		return nil, errors.New("n115: 缺少离线任务标识")
	}
	for page := 1; page <= maxOfflinePages; page++ {
		list, err := c.ListOfflineTasks(ctx, page)
		if err != nil {
			return nil, err
		}
		for i := range list.Tasks {
			if list.Tasks[i].OfflineID == ref || (list.Tasks[i].TaskID != "" && list.Tasks[i].TaskID == ref) {
				task := list.Tasks[i]
				return &task, nil
			}
		}
		if page >= list.PageCount || list.PageCount == 0 {
			break
		}
	}
	return nil, ErrNotFound
}

// ListOfflineTasks 分页拉取离线任务列表。
//
// 依据 115 开放平台《获取用户云下载任务列表》文档（未实测）：GET 带 page 参数。
func (c *Client) ListOfflineTasks(ctx context.Context, page int) (*OfflineTaskPage, error) {
	if page <= 0 {
		page = 1
	}
	form := encodeForm(taskListQuery{Page: page})
	var resp offlineTaskListResponse
	if err := c.do(ctx, apiCall{
		method:   http.MethodGet,
		rawURL:   c.baseURL + PathOfflineGetTaskList,
		form:     form,
		needAuth: true,
	}, &resp); err != nil {
		return nil, err
	}
	d := resp.resolved()
	out := &OfflineTaskPage{
		Page:      d.Page,
		PageCount: d.PageCount,
		Count:     d.Count,
	}
	for _, item := range d.Tasks {
		out.Tasks = append(out.Tasks, convertOfflineTask(item))
	}
	return out, nil
}

// OfflineTaskPage 离线任务列表页。
type OfflineTaskPage struct {
	// Page 当前页码。
	Page int
	// PageCount 总页数。
	PageCount int
	// Count 任务总数。
	Count int
	// Tasks 本页任务。
	Tasks []OfflineTask
}

// convertOfflineTask 把协议层任务条目归一化成业务结构。
//
// 115 的 status 数值：-1 失败、0 分配中、1 下载中、2 成功（依据官方文档，
// 未实测）。
func convertOfflineTask(item offlineTaskItem) OfflineTask {
	return OfflineTask{
		OfflineID:  item.InfoHash,
		TaskID:     item.FileID,
		Name:       item.Name,
		URL:        item.URL,
		Status:     convertOfflineStatus(item.Status),
		Progress:   item.PercentDone,
		Size:       item.Size,
		WpPathID:   item.WpPathID,
		AddTime:    item.AddTime,
		LastUpdate: item.LastUpdate,
	}
}

// convertOfflineStatus 把 115 的数值状态映射成业务状态。
func convertOfflineStatus(code int) OfflineStatus {
	switch code {
	case 2:
		return OfflineStatusDone
	case 1, 0:
		return OfflineStatusRunning
	case -1:
		return OfflineStatusFailed
	default:
		return OfflineStatusPending
	}
}

// CancelOfflineTask 取消（删除）离线任务。
//
// 依据 115 开放平台《删除用户云下载任务》文档（未实测）：开放平台没有单独的
// 「取消」接口，del_task 是唯一的任务终止手段，因此取消语义落在删除上。
// deleteFiles 为 true 时同时删除已下载到网盘的源文件。
func (c *Client) CancelOfflineTask(ctx context.Context, offlineID string, deleteFiles bool) error {
	if strings.TrimSpace(offlineID) == "" {
		return errors.New("n115: 缺少离线任务标识，无法取消")
	}
	del := 0
	if deleteFiles {
		del = 1
	}
	form := encodeForm(delOfflineTaskRequest{InfoHash: offlineID, DelSourceFile: del})
	var resp struct {
		apiEnvelope
		Data flexArray[string] `json:"data"`
	}
	if err := c.do(ctx, apiCall{
		method:   http.MethodPost,
		rawURL:   c.baseURL + PathOfflineDelTask,
		form:     form,
		needAuth: true,
	}, &resp); err != nil {
		return err
	}
	return nil
}

// ---- 文件列表 ----

// File 网盘里的一个文件（夹）。
type File struct {
	// FileID 文件（夹）ID。
	FileID string
	// ParentID 父目录 ID。
	ParentID string
	// Name 文件名。
	Name string
	// IsDir 是否是目录（115 的 fc：0 目录、1 文件）。
	IsDir bool
	// Size 文件大小（字节）。
	Size int64
	// SHA1 文件 sha1。
	SHA1 string
	// PickCode 提取码。
	PickCode string
	// Ext 扩展名（不含点）。
	Ext string
	// ModifiedAt 修改时间。
	ModifiedAt time.Time
	// UploadedAt 上传时间。
	UploadedAt time.Time
}

// FileListOptions 文件列表查询参数。
type FileListOptions struct {
	// Limit 每页数量；0 用默认值。
	Limit int
	// Offset 起始偏移量。
	Offset int
	// IncludeDirs 是否包含子目录（115 的 show_dir）。
	IncludeDirs bool
}

// ListFiles 列出目录下的文件。
//
// 依据 115 开放平台《获取文件列表》文档（未实测）：GET /open/ufile/files，
// 参数 cid / limit / offset / show_dir。离线任务完成后，用任务的 file_id 当
// cid 就能拿到入库所需的文件列表。
func (c *Client) ListFiles(ctx context.Context, cid string, opts FileListOptions) ([]File, error) {
	if strings.TrimSpace(cid) == "" {
		cid = "0"
	}
	showDir := 0
	if opts.IncludeDirs {
		showDir = 1
	}
	form := encodeForm(fileListQuery{
		CID:     cid,
		Limit:   opts.Limit,
		Offset:  opts.Offset,
		ShowDir: showDir,
	})
	var resp fileListResponse
	if err := c.do(ctx, apiCall{
		method:   http.MethodGet,
		rawURL:   c.baseURL + PathUFileFiles,
		form:     form,
		needAuth: true,
	}, &resp); err != nil {
		return nil, err
	}
	out := make([]File, 0, len(resp.Data))
	for _, item := range resp.Data {
		out = append(out, convertFile(item))
	}
	return out, nil
}

// convertFile 把协议层文件条目归一化成业务结构。
func convertFile(item fileItem) File {
	return File{
		FileID:     item.Fid,
		ParentID:   item.Pid,
		Name:       item.Fn,
		IsDir:      item.Fc == "0",
		Size:       item.Fs,
		SHA1:       item.Sha1,
		PickCode:   item.Pc,
		Ext:        item.Ico,
		ModifiedAt: time.Unix(item.Uet, 0),
		UploadedAt: time.Unix(item.Uppt, 0),
	}
}

// ---- 连通性 ----

// UserInfo 用户信息。
type UserInfo struct {
	// UserID 用户 id。
	UserID int64
	// UserName 用户名。
	UserName string
	// OpenID 开放平台用户标识（115 官方文档未列出，未实测）。
	OpenID string
	// AllTotal 空间总量（字节）。
	AllTotal int64
	// AllRemain 剩余空间（字节）。
	AllRemain int64
	// AllUse 已用空间（字节）。
	AllUse int64
	// VIPLevel 会员等级名。
	VIPLevel string
}

// UserInfo 查询用户信息。
//
// 依据 115 开放平台《获取用户空间和vip信息》文档（未实测）：GET /open/user/info。
// 这是最轻量的鉴权探针，令牌是否有效看它返回什么。
func (c *Client) UserInfo(ctx context.Context) (*UserInfo, error) {
	var resp userInfoResponse
	if err := c.do(ctx, apiCall{
		method:   http.MethodGet,
		rawURL:   c.baseURL + PathUserInfo,
		needAuth: true,
	}, &resp); err != nil {
		return nil, err
	}
	if resp.Data == nil {
		return nil, errors.New("n115: 用户信息返回空数据")
	}
	d := resp.Data
	return &UserInfo{
		UserID:    d.UserID,
		UserName:  d.UserName,
		OpenID:    d.OpenID,
		AllTotal:  d.RtSpaceInfo.AllTotal,
		AllRemain: d.RtSpaceInfo.AllRemain,
		AllUse:    d.RtSpaceInfo.AllUse,
		VIPLevel:  d.VipInfo.LevelName,
	}, nil
}

// IsAvailable 检查 access_token 是否可用。
//
// 判据：先向令牌提供器取令牌（取不到或为空即视为不可用），再用它打一次
// /open/user/info——只有远端认这个令牌才算真的可用。返回 nil 表示可用。
func (c *Client) IsAvailable(ctx context.Context) error {
	token, err := c.accessToken(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(token) == "" {
		return ErrNoToken
	}
	if _, err := c.UserInfo(ctx); err != nil {
		return err
	}
	return nil
}

// ---- HTTP 底座 ----

// maxOfflinePages 单任务查询时最多翻多少页。
//
// 离线任务列表是按页给的，没有服务端筛选接口，只能本地翻页匹配；翻太多页
// 会拖慢轮询，所以设个上限。
const maxOfflinePages = 50

// maxResponseBytes 响应体读取上限，防止异常响应把内存吃满。
const maxResponseBytes = 4 << 20

// accessToken 从令牌提供器取 access_token。
func (c *Client) accessToken(ctx context.Context) (string, error) {
	if c.tokens == nil {
		return "", ErrNoToken
	}
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return "", fmt.Errorf("n115: 取 access_token 失败: %w", err)
	}
	return token, nil
}

// apiCall 描述一次对 115 接口的调用。
type apiCall struct {
	method string
	rawURL string
	form   url.Values
	// bearer 显式令牌；为空且 needAuth 为 true 时从 TokenProvider 取。
	bearer string
	// needAuth 是否带 Authorization 头。
	needAuth bool
	// skipEnvelope 跳过通用信封成败判定。
	//
	// 只有扫码状态接口需要：它的 state 语义与其它接口相反（0 表示二维码失效、
	// 结束轮询；1 才是继续轮询），套用通用判定会把「二维码失效」误报成错误。
	skipEnvelope bool
	// secrets 参与错误消息脱敏的敏感串。
	secrets []string
}

// do 发起一次请求并解析响应。
//
// needAuth 时自动带 Authorization 头；secrets 与令牌都会在错误消息里被抹掉——
// 令牌必须留在请求里，不能流进日志。
func (c *Client) do(ctx context.Context, call apiCall, out any) error {
	var body io.Reader
	target := call.rawURL
	if call.form != nil {
		if call.method == http.MethodGet {
			// GET 的参数走 query string：115 的查询接口不接受请求体。
			if encoded := call.form.Encode(); encoded != "" {
				sep := "?"
				if strings.Contains(target, "?") {
					sep = "&"
				}
				target += sep + encoded
			}
		} else {
			body = strings.NewReader(call.form.Encode())
		}
	}
	req, err := http.NewRequestWithContext(ctx, call.method, target, body)
	if err != nil {
		// url.Error 会带上完整 URL（含 query），这里只报路径，避免把签名等
		// 查询参数带进错误消息。
		return fmt.Errorf("n115: 构造 %s 请求失败: %w", pathOf(call.rawURL), unwrapURLError(err))
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	// secrets 复制一份再追加令牌：直接改 call.secrets 会踩到调用方传进来的
	// 切片底层数组。
	secrets := make([]string, 0, len(call.secrets)+1)
	secrets = append(secrets, call.secrets...)
	if call.needAuth {
		token := call.bearer
		if token == "" {
			token, err = c.accessToken(ctx)
			if err != nil {
				return err
			}
		}
		if strings.TrimSpace(token) == "" {
			return ErrNoToken
		}
		req.Header.Set("Authorization", "Bearer "+token)
		secrets = append(secrets, token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		// 同上：*url.Error 内含完整 URL，只取内层错误。
		return fmt.Errorf("n115: 请求 %s 失败: %w", pathOf(call.rawURL), unwrapURLError(err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("n115: 读取响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return newAPIError(resp.StatusCode, raw, secrets...)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("n115: 解析 %s 响应失败: %w", pathOf(call.rawURL), err)
	}
	if call.skipEnvelope {
		return nil
	}
	carrier, ok := out.(envelopeCarrier)
	if !ok {
		return nil
	}
	env := carrier.envelope()
	return envelopeError(resp.StatusCode, env.State, env.Code, env.Errno, env.Message, env.Error, secrets...)
}

// envelopeCarrier 让带信封的响应结构体统一暴露信封。
type envelopeCarrier interface {
	envelope() *apiEnvelope
}

// pathOf 取 URL 的路径部分，用于错误消息（不带查询参数，避免泄露）。
func pathOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "115 接口"
	}
	return u.Path
}

// unwrapURLError 剥掉 url.Error 外壳，只留内层错误。
//
// *url.Error 的 Error() 会把完整 URL（含 query string）拼进消息。115 的查询
// 接口把 uid/sign 放在 query 里，这类值等同凭据，不能进日志。
func unwrapURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return urlErr.Err
	}
	return err
}

// firstNonEmpty 取第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
