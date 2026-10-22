import { ApiError, type ApiErrorBody } from './types'

// 统一 fetch 封装：JSON 解析 + 错误信封转换。后续每资源一个 client（tasks.ts/config.ts/...）。
const BASE = '/api/v1'

/** CSRF token 存储键：后端只在登录/初始化响应体下发（会话 cookie 为 HttpOnly），故前端自存。 */
const CSRF_KEY = 'raccoon.csrf'

export function getCsrfToken(): string {
  try {
    return localStorage.getItem(CSRF_KEY) ?? ''
  } catch {
    return ''
  }
}

export function setCsrfToken(token: string): void {
  try {
    if (token) localStorage.setItem(CSRF_KEY, token)
    else localStorage.removeItem(CSRF_KEY)
  } catch {
    // 隐私模式下 localStorage 不可用：写操作会被后端 403，前端降级为只读。
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const method = (init?.method ?? 'GET').toUpperCase()
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (init?.body) headers['Content-Type'] = 'application/json'
  // 非安全方法带 CSRF token（后端 csrfForSession 中间件校验 X-CSRF-Token）。
  if (method !== 'GET' && method !== 'HEAD') {
    const csrf = getCsrfToken()
    if (csrf) headers['X-CSRF-Token'] = csrf
  }
  const resp = await fetch(BASE + path, {
    credentials: 'same-origin',
    ...init,
    headers: { ...headers, ...(init?.headers as Record<string, string> | undefined) },
  })
  if (!resp.ok) {
    let code = 'HTTP_' + resp.status
    let message = resp.statusText
    let requestId = ''
    let detail = ''
    try {
      const body = (await resp.json()) as ApiErrorBody
      if (body?.error) {
        code = body.error.code
        message = body.error.message
        requestId = body.error.request_id
        detail = body.error.detail ?? ''
      }
    } catch {
      // 非 JSON 错误响应，保留 HTTP 级信息
    }
    throw new ApiError(code, message, requestId, detail)
  }
  if (resp.status === 204) return undefined as T
  return (await resp.json()) as T
}

/**
 * 下载二进制文件（如诊断包 zip）。
 *
 * 为什么不能复用 request()：那个函数按 JSON 解析响应体，
 * 诊断包是 zip 字节流，走 JSON.parse 会直接抛错。
 */
async function requestBlob(path: string): Promise<Blob> {
  const resp = await fetch(BASE + path, { credentials: 'same-origin' })
  if (!resp.ok) {
    let code = 'HTTP_' + resp.status
    let message = resp.statusText
    try {
      const body = (await resp.json()) as ApiErrorBody
      if (body?.error) {
        code = body.error.code
        message = body.error.message
      }
    } catch {
      // 非 JSON 错误响应，保留 HTTP 级信息
    }
    throw new ApiError(code, message, '')
  }
  return resp.blob()
}

export const api = {
  systemInfo: () => request<import('./types').SystemInfo>('/system/info'),
  health: () => request<import('./types').HealthCheck>('/system/health'),

  // ---- 认证（docs/04 §2.1）----
  authStatus: () => request<import('./types').AuthStatus>('/auth/status', { method: 'POST' }),
  authSetup: (username: string, password: string) =>
    request<import('./types').AuthResult>('/auth/setup', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),
  authLogin: (username: string, password: string) =>
    request<import('./types').AuthResult>('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),
  authLogout: () => request<{ ok: boolean }>('/auth/logout', { method: 'POST' }),
  // 后端 handleAuthMe 返回 {"user": {...}}（非 data 信封，docs/04 §2.1）。
  authMe: () => request<{ user: import('./types').AuthUser }>('/auth/me'),
  /** 改密码（docs/04 §2.1）：旧口令 + 新口令，弱口令由后端策略拦（400）。 */
  authChangePassword: (oldPassword: string, newPassword: string) =>
    request<{ ok: boolean }>('/auth/password', {
      method: 'PUT',
      body: JSON.stringify({ old_password: oldPassword, new_password: newPassword }),
    }),
  /** 改用户名（docs/04 §2.1）：必须带当前口令；撞名 409、非法用户名 400。 */
  authChangeUsername: (username: string, password: string) =>
    request<{ user: import('./types').AuthUser }>('/auth/username', {
      method: 'PUT',
      body: JSON.stringify({ username, password }),
    }),

  // ---- 任务（docs/04 §2.2/§2.3）----
  listTasks: (params: Record<string, string> = {}) => {
    const qs = new URLSearchParams(params).toString()
    return request<import('./types').TaskListResult>('/tasks' + (qs ? '?' + qs : ''))
  },
  createTasks: (items: { url: string; quality?: string; save_path?: string }[], preview = false) =>
    request<import('./types').CreateTasksResponse>('/tasks', {
      method: 'POST',
      body: JSON.stringify({ items, preview }),
    }),
  /** 父子批量（docs/05 §5）：父任务 + 子任务树，父卡聚合子进度。 */
  createParentTasks: (items: { url: string; quality?: string; save_path?: string }[], quality?: string) =>
    request<import('./types').CreateParentTasksResponse>('/tasks/parent', {
      method: 'POST',
      body: JSON.stringify({ items, quality }),
    }),
  /** 批量操作（docs/04 §2.2）：逐个执行报告，单条失败不影响其余。 */
  batchTasks: (action: 'cancel' | 'retry' | 'delete', ids: string[]) =>
    request<import('./types').TaskBatchResult>('/tasks/batch', {
      method: 'POST',
      body: JSON.stringify({ action, ids }),
    }),
  taskDetail: (id: string) => request<import('./types').TaskDetailResult>(`/tasks/${id}`),
  taskEvents: (id: string, after = 0) =>
    request<{ items: import('./types').TaskEvent[] | null }>(`/tasks/${id}/events?after=${after}`),
  cancelTask: (id: string) => request<{ ok: boolean }>(`/tasks/${id}/cancel`, { method: 'POST' }),
  retryTask: (id: string) => request<{ ok: boolean }>(`/tasks/${id}/retry`, { method: 'POST' }),
  deleteTask: (id: string) => request<{ ok: boolean }>(`/tasks/${id}`, { method: 'DELETE' }),
  parseUrl: (url: string) =>
    request<{ data: import('./types').ParsePreview }>('/parse', {
      method: 'POST',
      body: JSON.stringify({ url }),
    }),

  // ---- 配置中心（docs/04 §2.4）----
  getConfig: () => request<import('./types').ConfigResponse>('/config'),
  saveConfig: (settings: Record<string, unknown>) =>
    request<import('./types').ConfigSaveResponse>('/config', {
      method: 'PUT',
      body: JSON.stringify({ settings }),
    }),
  testConnection: (target: string, params: Record<string, unknown> = {}) =>
    request<import('./types').ConnTestResult>('/config/test-connection', {
      method: 'POST',
      body: JSON.stringify({ target, params }),
    }),
  exportConfig: () => request<Record<string, unknown>>('/config/export'),
  importConfig: (settings: Record<string, unknown>, dryRun = false) =>
    request<{ diff: { key: string; current: unknown; incoming: unknown }[]; applied: boolean }>('/config/import', {
      method: 'POST',
      body: JSON.stringify({ settings, dry_run: dryRun }),
    }),
  getConfigHistory: (key?: string) =>
    request<{ items: import('./types').ConfigHistoryEntry[] }>(
      '/config/history' + (key ? '?key=' + encodeURIComponent(key) : ''),
    ),
  rollbackConfig: (key: string, version: number) =>
    request<import('./types').ConfigSaveResponse>('/config/rollback', {
      method: 'POST',
      body: JSON.stringify({ key, version }),
    }),

  // ---- 凭据中心（docs/04 §2.5）----
  listCredentials: () => request<{ items: import('./types').Credential[] }>('/credentials'),
  createCredential: (body: {
    platform: string
    kind: string
    label: string
    note?: string
    value: string
    expires_at?: number
  }) =>
    request<{ data: import('./types').Credential }>('/credentials', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  updateCredential: (
    id: string,
    body: { label?: string; note?: string; value?: string },
  ) =>
    request<{ data: import('./types').Credential }>(`/credentials/${id}`, {
      method: 'PUT',
      body: JSON.stringify(body),
    }),
  deleteCredential: (id: string) =>
    request<{ ok: boolean }>(`/credentials/${id}`, { method: 'DELETE' }),
  checkCredential: (id: string) =>
    request<{ ok: boolean; status: string; message: string }>(`/credentials/${id}/check`, {
      method: 'POST',
    }),

  // ---- 媒体库（docs/04 §2.8）----
  listFiles: (params: Record<string, string> = {}) => {
    const qs = new URLSearchParams(params).toString()
    return request<import('./types').MediaListResponse>('/files' + (qs ? '?' + qs : ''))
  },
  fileDownloadUrl: (id: string) => `${BASE}/files/${id}/download`,
  filePreviewUrl: (id: string) => `${BASE}/files/${id}/preview`,

  // ---- 订阅（docs/04 §2.6）----
  listSubscriptions: () => request<{ items: import('./types').Subscription[] }>('/subscriptions'),
  createSubscription: (body: {
    source_url: string
    source_type?: string
    mode?: string
    quality?: string
    save_rule?: string
    interval_min?: number
    credential_id?: string
    range_from?: number
    range_to?: number
    preview?: boolean
  }) =>
    request<{ data: import('./types').Subscription | import('./types').ParsePreview; preview?: boolean }>(
      '/subscriptions',
      { method: 'POST', body: JSON.stringify(body) },
    ),
  updateSubscription: (
    id: string,
    body: {
      mode?: string
      quality?: string
      save_rule?: string
      interval_min?: number
      enabled?: boolean
      range_from?: number
      range_to?: number
    },
  ) =>
    request<{ data: import('./types').Subscription }>(`/subscriptions/${id}`, {
      method: 'PUT',
      body: JSON.stringify(body),
    }),
  deleteSubscription: (id: string) =>
    request<{ ok: boolean }>(`/subscriptions/${id}`, { method: 'DELETE' }),
  checkSubscriptionNow: (id: string) =>
    request<{ ok: boolean; queued?: number; checking?: boolean; message?: string }>(
      `/subscriptions/${id}/check-now`,
      { method: 'POST' },
    ),
  listSubscriptionItems: (id: string) =>
    request<{ items: import('./types').SubscriptionItem[] }>(`/subscriptions/${id}/items`),

  // ---- 直播（docs/04 §2.7）----
  listLiveSources: () => request<import('./types').LiveListResponse>('/live-sources'),
  createLiveSource: (body: {
    platform: string
    room_url: string
    title?: string
    quality?: string
    record_plan?: string
    segment_min?: number
    check_interval_sec?: number
    credential_id?: string
  }) =>
    request<{ data: import('./types').LiveSource }>('/live-sources', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  updateLiveSource: (
    id: string,
    body: {
      title?: string
      quality?: string
      record_plan?: string
      segment_min?: number
      check_interval_sec?: number
      enabled?: boolean
      credential_id?: string
    },
  ) =>
    request<{ data: import('./types').LiveSource }>(`/live-sources/${id}`, {
      method: 'PUT',
      body: JSON.stringify(body),
    }),
  deleteLiveSource: (id: string) =>
    request<{ ok: boolean }>(`/live-sources/${id}`, { method: 'DELETE' }),
  listLiveRecordings: (id: string) =>
    request<{ items: import('./types').LiveRecording[] }>(`/live-sources/${id}/recordings`),
  recordLive: (id: string) =>
    request<{ data: import('./types').LiveRecording; task: import('./types').Task }>(
      `/live-sources/${id}/record`,
      { method: 'POST' },
    ),

  // ---- 通知（docs/04 §2.9）----
  listNotifications: (unreadOnly = false) =>
    request<{ items: import('./types').Notification[]; unread: number }>(
      '/notifications' + (unreadOnly ? '?unread=1' : ''),
    ),
  markNotificationsRead: (ids: string[] = [], all = false) =>
    request<{ updated: number; unread: number }>('/notifications/read', {
      method: 'POST',
      body: JSON.stringify({ ids, all }),
    }),

  // ---- API Token（docs/04 §1.1）----
  listTokens: () => request<{ items: import('./types').ApiToken[] }>('/tokens'),
  createToken: (name: string, scope: number, expiresInHours = 0) =>
    request<{ data: import('./types').ApiToken; token: string; notice: string }>('/tokens', {
      method: 'POST',
      body: JSON.stringify({ name, scope, expires_in_hours: expiresInHours }),
    }),
  revokeToken: (id: string) => request<{ ok: boolean }>(`/tokens/${id}`, { method: 'DELETE' }),

  // ---- 外部集成通道（docs/04 §2.10 / docs/08 W4.6–W4.8）----
  integrationStatus: () =>
    request<{ data: import('./types').IntegrationStatus }>('/integrations/status'),
  testQB: () => request<import('./types').IntegrationTestResult>('/integrations/qb/test', { method: 'POST' }),
  testN115: () => request<import('./types').IntegrationTestResult>('/integrations/n115/test', { method: 'POST' }),
  telegramStatus: () => request<{ data: import('./types').TelegramStatus }>('/integrations/telegram/status'),

  // ---- 系统（docs/04 §2.9）----
  queueStats: () => request<import('./types').QueueStats>('/system/queue'),
  restartService: () =>
    request<{ ok: boolean; waited_sec: number; still_running: number; message: string }>(
      '/system/restart',
      { method: 'POST' },
    ),
  diagPreview: () =>
    request<{ preview: boolean; manifest: Record<string, unknown>; log_sample: string[] }>(
      '/system/diag/export?preview=1',
    ),
  /** 下载诊断包（zip 字节流，非 JSON）。 */
  diagExport: () => requestBlob('/system/diag/export'),
}
