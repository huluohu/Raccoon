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

// 统一错误信封（docs/04 §1.2）。
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
