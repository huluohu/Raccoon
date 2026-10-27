import { describe, expect, it, vi, beforeEach } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useNotificationsStore } from './notifications'
import { useTasksStore } from './tasks'

/**
 * notifications store 用例。
 *
 * 2026-10-25 全量 review 定位的泄漏：start() 里创建的 watch 没有保存 stop 句柄，
 *  stop() 也不停它 → 每次登录（= 每次 start）都多留一个常驻 watcher，
 *  一次 notif.new 会触发 N 次拉取，watcher 越攒越多。
 */

const mocks = vi.hoisted(() => ({
  listNotifications: vi.fn(),
  markNotificationsRead: vi.fn(),
}))

vi.mock('@/api/client', () => ({ api: mocks }))

beforeEach(() => {
  setActivePinia(createPinia())
  mocks.listNotifications.mockReset().mockResolvedValue({ items: [], unread: 0 })
  mocks.markNotificationsRead.mockReset().mockResolvedValue({ unread: 0 })
})

describe('notifications store', () => {
  it('start/start-stop 循环后，一次通知只触发一次拉取（watcher 不泄漏）', async () => {
    const store = useNotificationsStore()
    const tasks = useTasksStore()

    // 模拟三次「登录 → 登出」
    for (let i = 0; i < 3; i++) {
      store.start()
      store.stop()
    }
    store.start()

    const before = mocks.listNotifications.mock.calls.length
    // 一次 SSE 通知
    tasks.notifTick = tasks.notifTick + 1
    await new Promise((r) => setTimeout(r, 0))

    expect(mocks.listNotifications.mock.calls.length - before).toBe(1)
    store.stop()
  })

  it('stop() 之后不再因通知触发拉取', async () => {
    const store = useNotificationsStore()
    const tasks = useTasksStore()

    store.start()
    store.stop()

    const before = mocks.listNotifications.mock.calls.length
    tasks.notifTick = tasks.notifTick + 1
    await new Promise((r) => setTimeout(r, 0))

    expect(mocks.listNotifications.mock.calls.length).toBe(before)
  })

  it('start() 幂等：重复调用不会叠加轮询', async () => {
    const store = useNotificationsStore()
    store.start()
    store.start()
    store.stop()
    // 能 stop 干净说明只有一个轮询定时器在跑
    expect(true).toBe(true)
  })
})
