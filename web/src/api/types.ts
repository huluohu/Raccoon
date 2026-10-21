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
  platform: string
  url: string
  title?: string
  thumbnail?: string
  status: TaskStatus
  wait_reason?: string
  quality: string
  save_path?: string
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
  error_code?: string
  error_message?: string
  fix_hint?: string
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

// ---- 统一错误信封（docs/04 §1.2） ----
export interface ApiErrorBody {
  error: {
    code: string
    message: string
    request_id: string
  }
}

export class ApiError extends Error {
  constructor(
    readonly code: string,
    message: string,
    readonly requestId: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}
