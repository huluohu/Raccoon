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
