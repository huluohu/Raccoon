package server

// task_handler.go：任务域路由（docs/04 §2.2/§2.3/§2.9 的 tasks / parse / events）。

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/huluohu/raccoon/internal/engine"
	"github.com/huluohu/raccoon/internal/httpx"
	"github.com/huluohu/raccoon/internal/sse"
	"github.com/huluohu/raccoon/internal/store"
	"github.com/huluohu/raccoon/internal/subscription"
	"github.com/huluohu/raccoon/internal/task"
)

// setupTasks 装配任务系统（Manager）+ 注册路由。
func (s *Server) setupTasks() {
	eng := engine.NewSet("", "")
	hub := sse.New(s.log)
	s.hub = hub
	tm := task.NewManager(s.store, s.cfg, eng, hub, s.log.With("tasks"))
	s.tasks = tm
	// 通知出口注入（task → notify 单向依赖）。
	tm.SetNotifier(notifierFunc(s.notify))
	// 凭据解析器注入（凭据中心 → 引擎 cookie 文件桥接）。
	tm.SetCredentialResolver(task.NewCookieResolver(s.store, s.vault))
	if err := eng.CheckAll(); err != nil {
		// 引擎不可用不阻断启动：/health 降级、任务提交 503（docs/04 §1.2）。
		s.log.Warn("引擎自检未通过，下载能力降级", map[string]any{"err": err.Error()})
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.shutdownCancel = cancel
	go tm.Start(ctx)

	// 订阅服务（Wave 3）：周期检查 + 走统一任务系统入队。
	s.subs = subscription.New(s.store, s.cfg, eng, tm, s.log.With("subscription"))
	go s.subs.Start(ctx)

	// tasks REST（docs/04 §2.2）。
	s.router.GET("/api/v1/tasks", s.requireAuth(s.handleTasksList))
	s.router.POST("/api/v1/tasks", s.requireAuth(s.csrfForSession(s.handleTasksCreate)))
	s.router.GET("/api/v1/tasks/:id", s.requireAuth(s.handleTaskDetail))
	s.router.GET("/api/v1/tasks/:id/events", s.requireAuth(s.handleTaskEvents))
	s.router.POST("/api/v1/tasks/:id/cancel", s.requireAuth(s.csrfForSession(s.handleTaskCancel)))
	s.router.POST("/api/v1/tasks/:id/retry", s.requireAuth(s.csrfForSession(s.handleTaskRetry)))
	s.router.DELETE("/api/v1/tasks/:id", s.requireAuth(s.csrfForSession(s.handleTaskDelete)))
	s.router.POST("/api/v1/tasks/batch", s.requireAuth(s.csrfForSession(s.handleTasksBatch)))
	// 父子批量（播放列表/频道展开）。必须注册在 GET /tasks/:id 之前吗？——不需要：
	// 本路由是 POST，而 POST /tasks/:id 并不存在，两者不会争抢同一方法。
	// 注意将来若要加 GET /tasks/parent 会被 GET /tasks/:id 抢先匹配（先注册先匹配）。
	s.router.POST("/api/v1/tasks/parent", s.requireAuth(s.csrfForSession(s.handleTaskParentCreate)))
	s.router.GET("/api/v1/tasks/:id/files", s.requireAuth(s.handleTaskFiles))
	s.router.DELETE("/api/v1/tasks/:id/files/:fileId", s.requireAuth(s.csrfForSession(s.handleTaskFileDelete)))

	// parse 预览（不落库）。
	s.router.POST("/api/v1/parse", s.requireAuth(s.csrfForSession(s.handleParse)))

	// SSE 事件流（cookie 会话即可；EventSource 无法带 header）。
	s.router.GET("/api/v1/events", s.requireAuth(s.handleEvents))
}

// ---- tasks list ----

func (s *Server) handleTasksList(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	f := store.TaskFilter{
		Platform: q.Get("platform"),
		Q:        q.Get("q"),
		Kind:     q.Get("kind"),
		Limit:    atoiDefault(q.Get("limit"), 50),
		Cursor:   q.Get("cursor"),
	}
	if st := q.Get("status"); st != "" {
		f.Statuses = strings.Split(st, ",")
	}
	res, err := s.store.ListTasks(f)
	if err != nil {
		s.log.Error("任务列表查询失败", map[string]any{"err": err.Error()})
		return httpx.Internal("任务列表查询失败")
	}
	httpx.WriteJSON(w, http.StatusOK, res)
	return nil
}

// ---- tasks create ----

type createTasksReq struct {
	Items   []submitItemJSON `json:"items"`
	Preview bool             `json:"preview"`
}

type submitItemJSON struct {
	URL      string `json:"url"`
	Quality  string `json:"quality"`
	SavePath string `json:"save_path"`
}

func (s *Server) handleTasksCreate(w http.ResponseWriter, r *http.Request) error {
	var req createTasksReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if len(req.Items) == 0 {
		return httpx.Validation("items 不能为空")
	}
	if len(req.Items) > 50 {
		return httpx.Validation("单次最多提交 50 个任务")
	}
	// SSRF 防护第一层（docs/04 §5）：协议白名单 + 私网/环回拒绝。
	for _, it := range req.Items {
		if err := validateOutboundURL(it.URL); err != nil {
			return err
		}
	}
	if req.Preview {
		// preview:true 只解析不入队（多 URL 取第一个预览）。
		pv, err := s.tasks.Preview(r.Context(), req.Items[0].URL)
		if err != nil {
			return taskErrToHTTP(err)
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"data": pv})
		return nil
	}
	// 引擎可用性（任务入队前自检；缺失 503）。
	if err := s.tasks.Engines().YTDLP.Available(); err != nil {
		return &httpx.Error{HTTP: http.StatusServiceUnavailable, Code: httpx.CodeNoEngine, Message: err.Error()}
	}
	items := make([]task.SubmitItem, 0, len(req.Items))
	for _, it := range req.Items {
		items = append(items, task.SubmitItem{URL: it.URL, Quality: it.Quality, SavePath: it.SavePath})
	}
	u := userFrom(r)
	source := task.SourceWeb
	results, err := s.tasks.Submit(items, source, userRef(u))
	if err != nil {
		return httpx.Internal("任务创建失败: " + err.Error())
	}
	// 单条 dedupe 409：API 语义为「返回冲突项」而非整批失败（docs/04 §4 批量逐个报告）。
	conflicts := []map[string]any{}
	created := []*store.Task{}
	for _, res := range results {
		if res.ExistingID != "" || res.Task == nil {
			conflicts = append(conflicts, map[string]any{"url": "", "existing_task_id": res.ExistingID})
			continue
		}
		created = append(created, res.Task)
	}
	if len(created) == 0 && len(conflicts) > 0 {
		return httpx.Conflict("相同链接的任务已在进行中或队列中")
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"data":      created,
		"conflicts": conflicts,
	})
	return nil
}

// ---- task detail / cancel / retry / delete ----

func (s *Server) handleTaskDetail(w http.ResponseWriter, r *http.Request) error {
	id := param(r, "id")
	t, err := s.store.GetTask(id)
	if err != nil {
		return taskErrToHTTP(err)
	}
	files, _ := s.store.TaskFiles(id)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"data": t, "files": files})
	return nil
}

func (s *Server) handleTaskCancel(w http.ResponseWriter, r *http.Request) error {
	id := param(r, "id")
	if _, err := s.store.GetTask(id); err != nil {
		return taskErrToHTTP(err)
	}
	if err := s.tasks.Cancel(id); err != nil {
		return taskErrToHTTP(err)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "cancelled"})
	return nil
}

func (s *Server) handleTaskRetry(w http.ResponseWriter, r *http.Request) error {
	id := param(r, "id")
	if _, err := s.store.GetTask(id); err != nil {
		return taskErrToHTTP(err)
	}
	if err := s.tasks.Retry(id); err != nil {
		var notAllowed *task.ErrRetryNotAllowed
		if errors.As(err, &notAllowed) {
			return httpx.Conflict("仅失败或已取消的任务可以重试")
		}
		return taskErrToHTTP(err)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "waiting"})
	return nil
}

func (s *Server) handleTaskDelete(w http.ResponseWriter, r *http.Request) error {
	id := param(r, "id")
	if _, err := s.store.GetTask(id); err != nil {
		return taskErrToHTTP(err)
	}
	if err := s.store.SoftDelete(id); err != nil {
		return taskErrToHTTP(err)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

func (s *Server) handleTaskEvents(w http.ResponseWriter, r *http.Request) error {
	id := param(r, "id")
	if _, err := s.store.GetTask(id); err != nil {
		return taskErrToHTTP(err)
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	events, _, err := s.store.ListEvents(id, after, 100)
	if err != nil {
		return httpx.Internal("事件查询失败")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": events})
	return nil
}

// ---- parse ----

type parseReq struct {
	URL string `json:"url"`
}

func (s *Server) handleParse(w http.ResponseWriter, r *http.Request) error {
	if !s.parseLimiter.Allow(clientIP(r)) {
		w.Header().Set("Retry-After", "60")
		return &httpx.Error{HTTP: http.StatusTooManyRequests, Code: httpx.CodeRateLimited, Message: "解析请求过于频繁，请稍后再试"}
	}
	var req parseReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := validateOutboundURL(req.URL); err != nil {
		return err
	}
	if err := s.tasks.Engines().YTDLP.Available(); err != nil {
		return &httpx.Error{HTTP: http.StatusServiceUnavailable, Code: httpx.CodeNoEngine, Message: err.Error()}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	pv, err := s.tasks.Preview(ctx, strings.TrimSpace(req.URL))
	if err != nil {
		return taskErrToHTTP(err)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"data": pv})
	return nil
}

// ---- events (SSE) ----

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) error {
	s.hub.ServeHTTP(w, r)
	return nil
}

// ---- 辅助 ----

func param(r *http.Request, name string) string {
	return Params(r)[name]
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func userRef(u *store.User) string {
	if u == nil {
		return ""
	}
	return u.Username
}

// taskErrToHTTP store/task 错误 → HTTP 信封。
func taskErrToHTTP(err error) error {
	switch {
	case errors.Is(err, store.ErrTaskNotFound):
		return httpx.NotFound("任务不存在或已删除")
	case errors.Is(err, store.ErrNoSuchUser):
		return httpx.NotFound("用户不存在")
	}
	var ue *engine.UnavailableError
	if errors.As(err, &ue) {
		return &httpx.Error{HTTP: http.StatusServiceUnavailable, Code: httpx.CodeNoEngine, Message: ue.Error()}
	}
	var ee *engine.EngineError
	if errors.As(err, &ee) {
		he := httpx.Validation(ee.Message)
		// 引擎真实原因随 detail 带出（如「The page needs to be reloaded.」），
		// 用户才知道是链接问题、登录问题还是引擎版本问题。
		if ee.Detail != "" {
			he.WithDetail(ee.Detail)
		}
		// 引擎不可用（缺失/过旧）属于 503，与文档 §1.2 的 ENGINE_UNAVAILABLE 一致。
		if ee.Code == "ENGINE_UNAVAILABLE" || ee.Code == "ENGINE_OUTDATED" {
			he.HTTP = http.StatusServiceUnavailable
			he.Code = httpx.CodeNoEngine
		}
		return he
	}
	return httpx.Internal("服务内部错误")
}

// validateOutboundURL SSRF 第一层（docs/04 §5）：协议白名单 + 主机名不得为
// 私网/环回/链路本地 IPv4/IPv6 字面量。DNS 解析后的重定向逐跳复查由 yt-dlp
// 内部处理；本服务极限是入口 URL 校验（已知限制：DNS rebinding 纵深防护留待 v0.2）。
func validateOutboundURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return httpx.Validation("URL 不能为空")
	}
	u := strings.ToLower(raw)
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return httpx.Validation("仅支持 http/https 链接")
	}
	host := hostOf(raw)
	if host == "" {
		return httpx.Validation("URL 缺少主机名")
	}
	if isPrivateHost(host) {
		return httpx.Validation("不允许访问内网地址（防 SSRF）")
	}
	if err := validateYouTubeID(raw); err != nil {
		return err
	}
	return nil
}

// validateYouTubeID YouTube watch 链接的视频 ID 长度校验。
//
// YouTube 视频 ID 恒为 11 位（[A-Za-z0-9_-]）。粘贴时多带/少带一位是最常见的
// 输错形式，而引擎对这种链接只会报一句「This video is unavailable」——
// 用户看不出是自己链接敲错了。这里在提交前就拦下来并说清楚。
func validateYouTubeID(raw string) error {
	host := strings.ToLower(hostOf(raw))
	if host != "youtube.com" && host != "www.youtube.com" && host != "m.youtube.com" {
		return nil
	}
	// 只校验 watch?v= 形式（shorts / embed / playlist 的 ID 规则不同，不在此拦）。
	idx := strings.Index(raw, "watch?v=")
	if idx < 0 {
		return nil
	}
	id := raw[idx+len("watch?v="):]
	if i := strings.IndexAny(id, "&#"); i >= 0 {
		id = id[:i]
	}
	// 11 位是唯一合法长度；其它长度一律提示，绝不放行到引擎。
	if len(id) == 11 {
		return nil
	}
	if id == "" {
		return httpx.Validation("YouTube 链接缺少视频 ID（应形如 watch?v=XXXXXXXXXXX）")
	}
	return httpx.Validation(fmt.Sprintf(
		"YouTube 视频 ID 应为 11 位，当前为 %d 位（%q）——请检查链接是否粘贴完整",
		len(id), id))
}

// isPrivateHost 主机名（IP 字面量或常见保留名）私网判定。
func isPrivateHost(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	switch h {
	case "localhost", "ip6-localhost", "ip6-loopback":
		return true
	}
	if strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".internal") || strings.HasSuffix(h, ".lan") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ipIsPrivate(ip)
	}
	return false // 域名交给 yt-dlp 解析（已知限制见函数注释）
}

// ipIsPrivate 私网/环回/链路本地/未指定/组播/保留段。
func ipIsPrivate(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	// IPv4 保留段（net.IP.IsPrivate 已覆盖 RFC1918；补 0.0.0.0/8 与 100.64/10 CGNAT）。
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 0: // 0.0.0.0/8
			return true
		case v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127: // CGNAT
			return true
		case v4[0] == 192 && v4[1] == 0 && v4[2] == 0: // 192.0.0.0/24
			return true
		case v4[0] == 198 && v4[1] == 18 || v4[0] == 198 && v4[1] == 19: // 基准测试段
			return true
		case v4[0] >= 240: // 240.0.0.0/4 保留
			return true
		}
	}
	return false
}

// hostOf 从 URL 提取主机名。
func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
