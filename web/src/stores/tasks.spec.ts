import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

/**
 * tasks store 的 SSE 连接状态（宽限期行为）。
 *
 * 背景：网络/转发层的短暂抖动会让 EventSource 立即 error 又快速重连成功。
 * 无宽限时每次抖动 UI 都闪「重连中」，观感就是「老是重连中」；
 * 现在断线后 8s 内保持「已连接」显示，持续失败才如实翻离线。
 */

class FakeEventSource {
  static instances: FakeEventSource[] = []
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  listeners: Record<string, EventListener> = {}
  url: string
  closed = false
  /** readyState：0=CONNECTING 1=OPEN 2=CLOSED（对齐浏览器 EventSource 常量）。 */
  readyState = 0

  constructor(url: string) {
    this.url = url
    FakeEventSource.instances.push(this)
  }

  addEventListener(name: string, fn: EventListener) {
    this.listeners[name] = fn
  }

  close() {
    this.closed = true
    this.readyState = 2
  }

  // 测试驱动口
  fireOpen() {
    this.readyState = 1
    this.onopen?.()
  }

  fireError() {
    this.onerror?.()
  }
}

async function setupStore() {
  vi.stubGlobal('EventSource', FakeEventSource as unknown as typeof EventSource)
  const { useTasksStore } = await import('./tasks')
  const store = useTasksStore()
  return store
}

describe('tasks store · SSE 连接宽限', () => {
  beforeEach(() => {
    FakeEventSource.instances = []
    setActivePinia(createPinia())
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('断线后 8s 内不翻「重连中」（短暂抖动用户无感）', async () => {
    const store = await setupStore()
    const api = await import('@/api/client')
    vi.spyOn(api.api, 'listTasks').mockResolvedValue({
      items: [],
      next_cursor: '',
      total: 0,
      counts: { waiting: 0, parsing: 0, downloading: 0, post: 0, done: 0, failed: 0, cancelled: 0 },
    } as never)

    store.connect()
    const es = FakeEventSource.instances[0]!
    es.fireOpen()
    expect(store.connected).toBe(true)

    es.fireError() // 抖动：立即断
    vi.advanceTimersByTime(2_000) // 2s 后（已在重连路上）
    expect(store.connected).toBe(true) // 宽限期内不闪离线

    vi.advanceTimersByTime(6_100) // 超过 8s 宽限
    expect(store.connected).toBe(false) // 持续失败才如实显示
  })

  it('宽限期内重连成功：状态始终在线，退避计数归零', async () => {
    const store = await setupStore()
    const api = await import('@/api/client')
    vi.spyOn(api.api, 'listTasks').mockResolvedValue({
      items: [], next_cursor: '', total: 0,
      counts: { waiting: 0, parsing: 0, downloading: 0, post: 0, done: 0, failed: 0, cancelled: 0 },
    } as never)

    store.connect()
    const first = FakeEventSource.instances[0]!
    first.fireOpen()
    first.fireError()

    // 1s 后自动重连（退避第一步），新连接 open 成功
    vi.advanceTimersByTime(1_050)
    const second = FakeEventSource.instances[FakeEventSource.instances.length - 1]!
    expect(second).not.toBe(first)
    second.fireOpen()

    expect(store.connected).toBe(true)
    // 再过 10s 也不应翻离线（宽限定时器已被 onopen 清掉）
    vi.advanceTimersByTime(10_000)
    expect(store.connected).toBe(true)
  })

  it('连接已 CLOSED 时 connect() 自愈重建（不被死连接挡住）', async () => {
    const store = await setupStore()
    const api = await import('@/api/client')
    vi.spyOn(api.api, 'listTasks').mockResolvedValue({
      items: [], next_cursor: '', total: 0,
      counts: { waiting: 0, parsing: 0, downloading: 0, post: 0, done: 0, failed: 0, cancelled: 0 },
    } as never)

    store.connect()
    const first = FakeEventSource.instances[0]!
    first.fireOpen()
    expect(store.connected).toBe(true)

    // 连接被外部关掉（误 disconnect / 浏览器回收）：source 仍非空但已 CLOSED
    first.close()
    expect(store.connected).toBe(true) // 此刻徽章还显示在线

    // 再调 connect()：应自愈重建，而不是因为 source 非空就 return
    store.connect()
    expect(FakeEventSource.instances.length).toBe(2)
    const second = FakeEventSource.instances[1]!
    expect(second).not.toBe(first)
    second.fireOpen()
    expect(store.connected).toBe(true)
  })

  it('连接仍 OPEN 时 connect() 不重复建（幂等）', async () => {
    const store = await setupStore()
    const api = await import('@/api/client')
    vi.spyOn(api.api, 'listTasks').mockResolvedValue({
      items: [], next_cursor: '', total: 0,
      counts: { waiting: 0, parsing: 0, downloading: 0, post: 0, done: 0, failed: 0, cancelled: 0 },
    } as never)

    store.connect()
    FakeEventSource.instances[0]!.fireOpen()
    store.connect()
    store.connect()
    expect(FakeEventSource.instances.length).toBe(1)
  })
})
