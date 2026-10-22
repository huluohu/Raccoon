// tasks store 单测（docs/08 W5.3 前端 vitest 门禁）。
//
// 覆盖 store 的三类核心逻辑：
//   1. 对账投影：refresh 把服务端响应写进 items/counts，并如实标记列表截断；
//   2. 父任务子项统计：创建响应里的 child_total/child_failed 是权威值
//      （GET /tasks 不返回这两列），必须按父 ID 留存，否则父卡展开后计数丢失；
//   3. 错误映射：ApiError 走 message，401/403 触发登出标记（不断言 UI）。
//
// SSE 连接逻辑（connect/disconnect）不在本文件覆盖：jsdom 无 EventSource，
// 其行为由后端广播契约 + e2e 验证（docs/10）。
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

// vi.hoisted：mock 工厂先于 import 执行，需要提升引用的假函数。
const h = vi.hoisted(() => ({
  listTasks: vi.fn(),
  createTasks: vi.fn(),
  createParentTasks: vi.fn(),
  batchTasks: vi.fn(),
  parseUrl: vi.fn(),
  cancelTask: vi.fn(),
  retryTask: vi.fn(),
  deleteTask: vi.fn(),
  taskEvents: vi.fn(),
  markUnauthenticated: vi.fn(),
}))

vi.mock('@/api/client', () => ({ api: h }))
vi.mock('./auth', () => ({
  useAuthStore: () => ({ markUnauthenticated: h.markUnauthenticated }),
}))

import { ApiError, type Task } from '@/api/types'
import { useTasksStore } from './tasks'

function task(id: string, status: Task['status'] = 'waiting'): Task {
  return {
    id,
    kind: 'download',
    source: 'web',
    url: `https://example.com/${id}`,
    status,
    expected_size: null,
    downloaded_size: 0,
    speed_bps: 0,
    progress: 0,
    resume_supported: false,
    config_version: 1,
    attempt: 0,
    max_attempts: 3,
    cancel_requested: false,
    created_at: 1,
    updated_at: 1,
  }
}

const emptyCounts = {
  waiting: 0,
  parsing: 0,
  downloading: 0,
  post: 0,
  done: 0,
  failed: 0,
  cancelled: 0,
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.clearAllMocks()
})

describe('tasks store · 对账投影', () => {
  it('refresh 写入 items/counts，next_cursor 存在时标记列表截断', async () => {
    h.listTasks.mockResolvedValue({
      items: [task('t1'), task('t2', 'downloading')],
      counts: { ...emptyCounts, waiting: 1, downloading: 1 },
      next_cursor: '100:t2',
    })
    const store = useTasksStore()

    await store.refresh()

    expect(store.items.map((t) => t.id)).toEqual(['t1', 't2'])
    expect(store.counts.downloading).toBe(1)
    expect(store.listTruncated).toBe(true)
    expect(store.loading).toBe(false)
    // limit=100：父子树要求子项行也在列表里（后端上限）。
    expect(h.listTasks).toHaveBeenCalledWith({ limit: '100' })
  })

  it('无 next_cursor 时 listTruncated 为 false', async () => {
    h.listTasks.mockResolvedValue({ items: [], counts: emptyCounts })
    const store = useTasksStore()

    await store.refresh()

    expect(store.listTruncated).toBe(false)
  })

  it('401/403 触发登出标记且不展示错误（交由登录页处理）', async () => {
    h.listTasks.mockRejectedValue(new ApiError('UNAUTHORIZED', '登录已过期', ''))
    const store = useTasksStore()

    await store.refresh()

    expect(h.markUnauthenticated).toHaveBeenCalledTimes(1)
    expect(store.error).toBe('')
  })

  it('普通错误写入 error.value', async () => {
    h.listTasks.mockRejectedValue(new ApiError('INTERNAL', '服务端炸了', ''))
    const store = useTasksStore()

    await store.refresh()

    expect(store.error).toBe('服务端炸了')
    expect(h.markUnauthenticated).not.toHaveBeenCalled()
  })
})

describe('tasks store · 父任务子项统计', () => {
  it('submitParent 按父 ID 留存 child_total/child_failed 并刷新列表', async () => {
    h.createParentTasks.mockResolvedValue({
      data: task('parent-1'),
      child_total: 12,
      child_failed: 0,
    })
    h.listTasks.mockResolvedValue({ items: [task('parent-1')], counts: emptyCounts })
    const store = useTasksStore()

    const res = await store.submitParent([{ url: 'https://example.com/pl' }], '1080p')

    expect(res.child_total).toBe(12)
    expect(store.childStats['parent-1']).toEqual({ total: 12, failed: 0 })
    expect(h.createParentTasks).toHaveBeenCalledWith([{ url: 'https://example.com/pl' }], '1080p')
    expect(h.listTasks).toHaveBeenCalled() // 创建后必须对账，不能只信创建响应
  })

  it('submitParent 失败时写 error 并向上抛（调用方要阻止重复提交）', async () => {
    h.createParentTasks.mockRejectedValue(new ApiError('CONFLICT', '重复提交', ''))
    const store = useTasksStore()

    await expect(store.submitParent([{ url: 'https://example.com/pl' }])).rejects.toThrow(ApiError)
    expect(store.error).toBe('重复提交')
    expect(store.childStats['parent-1']).toBeUndefined()
  })
})

describe('tasks store · 批量操作', () => {
  it('batch 透传 action 与 ids，返回逐条结果', async () => {
    const batchResult = { results: [{ id: 't1', ok: true }, { id: 't2', ok: false, error: 'not found' }] }
    h.batchTasks.mockResolvedValue(batchResult)
    h.listTasks.mockResolvedValue({ items: [], counts: emptyCounts })
    const store = useTasksStore()

    const res = await store.batch('retry', ['t1', 't2'])

    expect(h.batchTasks).toHaveBeenCalledWith('retry', ['t1', 't2'])
    expect(res).toEqual(batchResult)
    expect(h.listTasks).toHaveBeenCalled()
  })

  it('retry 失败时写 error 并上抛', async () => {
    h.retryTask.mockRejectedValue(new ApiError('CONFLICT', '任务不在失败态', ''))
    const store = useTasksStore()

    await expect(store.retry('t1')).rejects.toThrow(ApiError)
    expect(store.error).toBe('任务不在失败态')
  })
})
