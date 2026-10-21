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
    try {
      const body = (await resp.json()) as ApiErrorBody
      if (body?.error) {
        code = body.error.code
        message = body.error.message
        requestId = body.error.request_id
      }
    } catch {
      // 非 JSON 错误响应，保留 HTTP 级信息
    }
    throw new ApiError(code, message, requestId)
  }
  if (resp.status === 204) return undefined as T
  return (await resp.json()) as T
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
  authMe: () => request<{ data: import('./types').AuthUser }>('/auth/me'),

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
}
