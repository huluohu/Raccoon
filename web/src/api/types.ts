// API 类型：与后端 JSON 字段同源（docs/04）。禁止在此发明后端没有的字段。

export interface SystemInfo {
  version: string
  go: string
  arch: string
  uptime: number
  data_dir: string
}

export interface HealthCheck {
  status: boolean
  checks: {
    db: string
    disk_used_pct?: number
  }
}

// ---- 认证（docs/04 §2.1） ----

export interface AuthStatus {
  setup_required: boolean
}

export interface AuthUser {
  id: string
  username: string
  created_at: number
  last_login_at?: number
}

export interface AuthResult {
  csrf_token: string
  user: AuthUser
}

// ---- 任务域（docs/04 §2.2/§2.3） ----

/** 七态之一（docs/05 §2）：waiting/parsing/downloading/post/done/failed/cancelled。 */
export type TaskStatus =
  | 'waiting'
  | 'parsing'
  | 'downloading'
  | 'post'
  | 'done'
  | 'failed'
  | 'cancelled'

export interface Task {
  id: string
  kind: string
  source: string
  source_ref?: string
  parent_id?: string
  platform: string
  url: string
  title?: string
  thumbnail?: string
  status: TaskStatus
  wait_reason?: string
  quality: string
  save_path?: string
  /** 产物目标格式：缺省=源格式 / mp4 / mp3（post 阶段 ffmpeg 处理）。 */
  convert_to?: string
  expected_size?: number | null
  downloaded_size: number
  speed_bps: number
  progress: number
  resume_supported: boolean
  engine?: string
  engine_version?: string
  config_version: number
  attempt: number
  max_attempts: number
  cancel_requested: boolean
  /** 父任务子项总数（后端 store.Task，omitempty；仅父任务有）。 */
  child_total?: number
  /** 父任务失败子项数（后端 store.Task，omitempty；仅父任务有）。 */
  child_failed?: number
  error_code?: string
  error_message?: string
  fix_hint?: string
  /**
   * 到期时间（毫秒）：waiting 且 wait_reason='auto_retry' 时是退避结束点，
   * 前端据此显示「Ns 后自动重试」倒计时；其余等待态为空。
   */
  scheduled_at?: number | null
  /**
   * 「走了哪条路」的人类可读链路（docs/15 §3.7）。
   * 由后端在创建时写入初始值、运行期追补转移（如「115 优先 → 115 提交中 → 115 确定失败 → 转 qB」）。
   * 老数据 / 非磁力任务可能为空串——界面按「—」显示，不猜。
   */
  route_path?: string
  /** 领取顺序权重：0=普通（默认），>0=置顶插队（waiting 队列 priority DESC, created_at ASC）。 */
  priority?: number
  created_at: number
  updated_at: number
  started_at?: number | null
  finished_at?: number | null
}

export interface TaskFile {
  id: string
  task_id: string
  path: string
  size: number
  mime?: string
  kind: string
  created_at: number
}

export interface TaskEvent {
  id: number
  task_id: string
  ts: number
  level: string
  event: string
  message: string
  fields?: string
}

export interface TaskListResult {
  items: Task[] | null
  next_cursor?: string
  total: number
  counts: Record<TaskStatus, number>
}

export interface TaskDetailResult {
  data: Task
  files: TaskFile[] | null
}

/** 解析预览（docs/04 §2.6）。 */
export interface FormatOption {
  quality: string
  size?: number | null
  note?: string
}

export interface ParsePreview {
  url: string
  platform: string
  title: string
  thumbnail?: string
  uploader?: string
  duration?: number | null
  is_playlist: boolean
  kind: string
  formats: FormatOption[]
}

export interface CreateTasksResponse {
  data: Task[]
  conflicts: { url: string; existing_task_id: string }[]
}

// ---- 父子任务（docs/05 §5 / docs/04 §2.2）----

/** POST /tasks/parent 响应：父任务 + 已建子任务树。 */
export interface CreateParentTasksResponse {
  data: Task
  children: Task[] | null
  child_total: number
  child_failed: number
}

/** POST /tasks/batch 响应：逐个执行报告（单条失败不影响其余）。 */
export interface TaskBatchResult {
  succeeded: string[]
  failed: { id: string; reason: string }[]
}

// ---- 配置中心（docs/04 §2.4） ----

/** 生效时机（docs/06 §2）。 */
export type Effective = 'immediate' | 'next_task' | 'restart'

export interface ConfigItem {
  key: string
  desc: string
  default: unknown
  value: unknown
  type: 'int' | 'bool' | 'string'
  effective: Effective
  sensitive: boolean
  locked: boolean
  env_var?: string
  version: number
  /** 枚举项的合法取值（后端下发，前端渲染成下拉；缺省表示自由文本）。 */
  choices?: string[]
}

export interface ConfigResponse {
  items: ConfigItem[]
  version: number
}

export interface ConfigSaveResponse {
  saved: string[]
  effective_summary: Record<Effective, string[]>
  version: number
}

export interface ConfigHistoryEntry {
  id: number
  key: string
  value_json: string
  version: number
  changed_at: number
  changed_by?: string
  note?: string
}

export interface ConnTestResult {
  ok: boolean
  target: string
  message: string
  latency_ms: number
}

// ---- 外部集成通道（docs/04 §2.10 / docs/08 W4.6–W4.8）----

/** 单通道状态（GET /integrations/status）。未探测时 ok/message 缺省（=状态不明）。 */
export interface IntegrationChannelStatus {
  /** 是否已配置（服务端读到配置项）。 */
  configured: boolean
  /** 实探结果：仅在服务端构造出客户端时下发；缺省=状态不明。 */
  ok?: boolean
  /** 实探失败原因。 */
  message?: string
}

export interface IntegrationStatus {
  qb: IntegrationChannelStatus
  n115: IntegrationChannelStatus
  telegram: IntegrationChannelStatus
}

/** Telegram 通道状态（GET /integrations/telegram/status）。 */
export interface TelegramStatus {
  enabled: boolean
  allowlist: number
}

/** 通道测试结果（POST /integrations/{qb,n115}/test）。 */
export interface IntegrationTestResult {
  ok: boolean
  version?: string
}

// ---- 凭据中心（docs/04 §2.5） ----

export interface Credential {
  id: string
  platform: string
  kind: 'cookie_file' | 'account' | 'token' | 'session'
  label: string
  status: 'active' | 'expiring' | 'expired' | 'disabled'
  note?: string
  expires_at?: number
  last_check_at?: number
  created_at: number
  updated_at: number
  has_value: boolean
  preview?: string
}

// ---- 媒体库（docs/04 §2.8） ----

export interface MediaFile {
  id: string
  task_id?: string
  subscription_id?: string
  path: string
  name: string
  size: number
  mime?: string
  duration?: number
  platform?: string
  uploader?: string
  kind: string
  previewable: boolean
  created_at: number
}

export interface MediaListResponse {
  items: MediaFile[]
  total: number
  platforms: string[]
}

// ---- 订阅（docs/04 §2.6） ----

export interface SubCounts {
  total: number
  downloaded: number
  queued: number
  skipped: number
  failed: number
}

export interface Subscription {
  id: string
  platform: string
  source_type: 'favorite' | 'uploader' | 'list'
  source_id: string
  source_url: string
  title?: string
  mode: 'new_only' | 'backfill' | 'range'
  range_from?: number
  range_to?: number
  quality?: string
  save_rule?: string
  interval_min: number
  enabled: boolean
  credential_id?: string
  last_checked_at?: number
  last_item_id?: string
  next_check_at: number
  error_note?: string
  created_at: number
  updated_at: number
  item_counts: SubCounts
}

export interface SubscriptionItem {
  id: number
  subscription_id: string
  item_id: string
  title?: string
  task_id?: string
  status: 'pending' | 'queued' | 'downloaded' | 'skipped' | 'failed'
  discovered_at: number
}

// ---- 直播（docs/04 §2.7） ----

export interface LiveSource {
  id: string
  platform: string
  room_id: string
  room_url: string
  title?: string
  credential_id?: string
  enabled: boolean
  quality?: string
  record_plan: 'manual' | 'always' | 'scheduled'
  schedule_json?: string
  segment_min: number
  check_interval_sec: number
  status: 'offline' | 'live' | 'recording' | 'error'
  last_checked_at?: number
  last_live_at?: number
  error_note?: string
  created_at: number
  updated_at: number
  recording_count: number
}

export interface LiveRecording {
  id: string
  source_id: string
  task_id?: string
  started_at: number
  ended_at?: number
  segments: number
  size: number
  status: 'recording' | 'completed' | 'failed'
  error_note?: string
}

export interface LiveListResponse {
  items: LiveSource[]
  supported: string[]
  supported_note: string
}

// ---- 通知（docs/04 §2.9） ----

export interface Notification {
  id: string
  kind: 'task_done' | 'task_failed' | 'sub_new' | 'live_start' | 'disk_warn' | 'system'
  severity: 'info' | 'success' | 'warning' | 'error'
  title: string
  body?: string
  task_id?: string
  read: boolean
  created_at: number
}

// ---- API Token（docs/04 §1.1） ----

export interface ApiToken {
  id: string
  user_id: string
  name: string
  scope: number
  expires_at?: number
  revoked_at?: number
  created_at: number
  last_used_at?: number
  revoked: boolean
}

// ---- 队列统计（总览页） ----

export interface QueueStats {
  counts: Record<TaskStatus, number>
  total_tasks: number
  running: number
  media_count: number
  media_bytes: number
  unread_notifs: number
}

// ---- 仪表盘：统计 / 监控 / 日志 / 偏好（docs/04 §2.10、§2.13） ----

/** 主页指标聚合（docs/15 §3.1-a）。success_rate 分母 0 时为 null，界面显示「—」。 */
export interface SummaryStats {
  uptime_sec: number
  total_downloads: number
  today_downloads: number
  week_downloads: number
  success_rate: number | null
  media_bytes: number
  media_by_kind: Record<string, number>
  sub_downloads: number
  favorite_downloads: number
  live_recording: number
}

/** 趋势单点：date = 本地时区 YYYY-MM-DD，无数据日由后端补 0。 */
export interface TrendPoint {
  date: string
  count: number
  bytes: number
}

/** 失败拆分条目（E5）。 */
export interface NamedCount {
  key: string
  count: number
}

export interface TrendResult {
  days: number
  points: TrendPoint[]
  failures: { by_platform: NamedCount[] | null; by_reason: NamedCount[] | null }
}

/** 实时监控快照（非 Linux：ok=false + message，界面显示「当前平台不支持」）。 */
export interface MonitorSnapshot {
  ok: boolean
  message?: string
  sampled_at: number
  cpu?: { used_pct: number }
  memory?: { used_pct: number; used_bytes: number; free_bytes: number; total_bytes: number }
  disk?: { used_pct: number; free_bytes: number; total_bytes: number }
  net?: { up_bps: number; down_bps: number }
  /** 60 点序列；null = 该时刻采集失败（前端断线，不插值）。 */
  series?: {
    cpu: (number | null)[]
    memory: (number | null)[]
    net_up: (number | null)[]
    net_down: (number | null)[]
  }
}

/** 监控历史点（1h = 60s 粒度，24h = 1440s 均值）。 */
export interface MonitorHistoryPoint {
  ts: number
  cpu: number
  memory: number
  disk: number
  net_rx: number
  net_tx: number
}

export interface MonitorHistory {
  range: '1h' | '24h'
  granularity_sec: number
  points: MonitorHistoryPoint[]
}

/** 运行日志条目（msg 已在服务端脱敏）。 */
export interface LogItem {
  ts: number
  level: 'debug' | 'info' | 'warn' | 'error'
  module: string
  msg: string
}

export interface LogPage {
  items: LogItem[]
  /** 下一段起始行号；无更多时等于请求的 cursor（前端据此停「加载更多」）。 */
  next_cursor: string
}

/** 统一错误信封（docs/04 §1.2） */
export interface ApiErrorBody {
  error: {
    code: string
    message: string
    request_id: string
    /** 补充说明：引擎/上游的真实原因（如 yt-dlp 的原始报错）。可选。 */
    detail?: string
  }
}

export class ApiError extends Error {
  constructor(
    readonly code: string,
    message: string,
    readonly requestId: string,
    /** 引擎/上游真实原因（可选），用于把「引擎退出码 1」还原成可判断的原因。 */
    readonly detail?: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}
