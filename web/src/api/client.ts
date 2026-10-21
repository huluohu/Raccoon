import { ApiError, type ApiErrorBody } from './types'

// 统一 fetch 封装：JSON 解析 + 错误信封转换。后续每资源一个 client（tasks.ts/config.ts/...）。
const BASE = '/api/v1'

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(BASE + path, {
    credentials: 'same-origin',
    headers: { Accept: 'application/json', ...(init?.body ? { 'Content-Type': 'application/json' } : {}) },
    ...init,
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
}
