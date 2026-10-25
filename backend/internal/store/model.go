package store

// model.go：任务域模型与 SQLite 表一一对应（docs/03）。JSON tag 对齐 API 契约（docs/04）。
// 存于 store 包避免 task(逻辑) ↔ store(数据) 循环依赖；状态机纯函数仍在 internal/task。
type Task struct {
	ID              string  `json:"id"`
	ParentID        *string `json:"parent_id,omitempty"`
	Kind            string  `json:"kind"`
	Source          string  `json:"source"`
	SourceRef       string  `json:"source_ref,omitempty"`
	Platform        string  `json:"platform,omitempty"`
	URL             string  `json:"url"`
	Title           string  `json:"title,omitempty"`
	Thumbnail       string  `json:"thumbnail,omitempty"`
	DedupeKey       string  `json:"-"`
	Status          string  `json:"status"`
	WaitReason      string  `json:"wait_reason,omitempty"`
	Quality         string  `json:"quality,omitempty"`
	SavePath        string  `json:"save_path,omitempty"`
	ConvertTo       string  `json:"convert_to,omitempty"` // 产物目标格式：""=源格式 / mp4 / mp3（post 阶段 ffmpeg 处理）
	Proxy           string  `json:"proxy,omitempty"`      // 任务级代理配置（""=跟随系统，有值=独立走代理）
	ExpectedSize    *int64  `json:"expected_size"`        // NULL=未知，禁估算
	DownloadedSize  int64   `json:"downloaded_size"`
	SpeedBps        int64   `json:"speed_bps"`
	Progress        float64 `json:"progress"`
	ResumeSupported bool    `json:"resume_supported"`
	Engine          string  `json:"engine,omitempty"`
	EngineVersion   string  `json:"engine_version,omitempty"`
	ConfigVersion   int     `json:"config_version"`
	Attempt         int     `json:"attempt"`
	MaxAttempts     int     `json:"max_attempts"`
	ErrorCode       string  `json:"error_code,omitempty"`
	ErrorMessage    string  `json:"error_message,omitempty"`
	FixHint         string  `json:"fix_hint,omitempty"` // JSON：{action,label}
	CancelRequested bool    `json:"cancel_requested"`
	RoutePath       string  `json:"route_path,omitempty"` // 存储策略「走了哪条路」（docs/01 §3.4）
	Priority        int     `json:"priority"`             // 领取顺序权重：>0 插队（waiting 队列 priority DESC, created_at ASC）
	ScheduledAt     *int64  `json:"scheduled_at,omitempty"`
	ChildTotal      int     `json:"child_total,omitempty"`  // 仅父任务：子任务数
	ChildFailed     int     `json:"child_failed,omitempty"` // 仅父任务：失败子任务数
	Summary         string  `json:"summary,omitempty"`      // AI 结构化总结
	CreatedAt       int64   `json:"created_at"`
	UpdatedAt       int64   `json:"updated_at"`
	StartedAt       *int64  `json:"started_at,omitempty"`
	FinishedAt      *int64  `json:"finished_at,omitempty"`
}

// TaskFile 任务产物（docs/03 §4.3）。
type TaskFile struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	Mime      string `json:"mime,omitempty"`
	Kind      string `json:"kind"`
	CreatedAt int64  `json:"created_at"`
}

// TaskEvent 任务事件/日志（docs/03 §4.2）。
type TaskEvent struct {
	ID         int64  `json:"id"`
	TaskID     string `json:"task_id"`
	Ts         int64  `json:"ts"`
	Level      string `json:"level"`
	Event      string `json:"event"`
	Message    string `json:"message,omitempty"`
	FieldsJSON string `json:"fields,omitempty"`
}

// ParsePreview 是 POST /parse 的返回（docs/04 §2.3），不落库不入队。
type ParsePreview struct {
	URL        string         `json:"url"`
	Platform   string         `json:"platform"`
	Kind       string         `json:"kind"` // video/audio/image/playlist/unknown
	Title      string         `json:"title"`
	Thumbnail  string         `json:"thumbnail,omitempty"`
	Uploader   string         `json:"uploader,omitempty"`
	Duration   *int64         `json:"duration,omitempty"` // 秒
	IsPlaylist bool           `json:"is_playlist"`
	Formats    []FormatOption `json:"formats"`
	ProxyUsed  string         `json:"proxy_used,omitempty"`
	RouteNote  string         `json:"route_note,omitempty"` // 如「直连异常，已自动切换代理」
}

// FormatOption 一个可选质量；Size 为 nil 时前端显示「未知」（禁估算）。
type FormatOption struct {
	Quality string `json:"quality"`
	Size    *int64 `json:"size"`
	Note    string `json:"note,omitempty"`
}
