import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { mountView } from '@/test/mount'
import Monitor from '@/views/Monitor.vue'
import { REmptyStub } from '@/test/stubs'
import type { MonitorHistory, MonitorSnapshot } from '@/api/types'

/**
 * Monitor.vue 用例（docs/15 T05 验收 #6）：
 *   1) 2s 轮询生命周期 —— 卸载必须停轮询，否则离开页面还在打接口；
 *   2) 非 Linux 降级 —— 只留 REmpty，不画任何图（没有数据来源就不画）；
 *   3) 正常渲染 —— 三环 + 带宽数字；series 里的 null 不当 0（跳过断点）；
 *   4) 历史 Tab —— 切 24h 才请求，粒度换算成人话。
 *
 * 图表组件用真实实现：本页的断言（.donut 存在与否、ring 的 stroke 颜色）
 * 依赖它们真实产出的 DOM，stub 掉就什么也验不到了。
 * REmpty 用 stub：真实 REmpty 内含浣熊线稿 svg，「页面上没有任何 svg」这条断言会被它干扰。
 */

const mocks = vi.hoisted(() => ({
  monitorSnapshot: vi.fn(),
  monitorHistory: vi.fn(),
  statsSummary: vi.fn(),
  statsTrend: vi.fn(),
}))

vi.mock('@/api/client', () => ({ api: mocks }))

// 每个用例结束都卸载：本页在 onMounted 里起定时器，漏卸载会把 2s 轮询带进下一个用例。
function snapshot(over: Partial<MonitorSnapshot> = {}): MonitorSnapshot {
  return {
    ok: true,
    sampled_at: new Date('2026-10-23T14:05:32').getTime(),
    cpu: { used_pct: 42 },
    memory: { used_pct: 68, used_bytes: 4 * 1024 ** 3, free_bytes: 4 * 1024 ** 3, total_bytes: 8 * 1024 ** 3 },
    disk: { used_pct: 91, free_bytes: 100 * 1024 ** 3, total_bytes: 1000 * 1024 ** 3 },
    net: { up_bps: 2048, down_bps: 1024 * 1024 },
    series: {
      cpu: [1, null, 3],
      memory: [50, null, 52],
      net_up: [null, 100, 200],
      net_down: [200, 300, 400],
    },
    ...over,
  }
}

function history(range: '1h' | '24h'): MonitorHistory {
  return {
    range,
    granularity_sec: range === '1h' ? 60 : 1440,
    points: [
      { ts: new Date('2026-10-23T10:00:00').getTime(), cpu: 20, memory: 40, disk: 50, net_rx: 1, net_tx: 2 },
      { ts: new Date('2026-10-23T10:24:00').getTime(), cpu: 30, memory: 45, disk: 51, net_rx: 3, net_tx: 4 },
    ],
  }
}

beforeEach(() => {
  mocks.monitorSnapshot.mockReset().mockResolvedValue(snapshot())
  mocks.monitorHistory.mockReset().mockImplementation((r: '1h' | '24h') => Promise.resolve(history(r)))
  // store 的 summary/trend 轮询也在跑（5s/60s），用例里推进时间会打到它们。
  mocks.statsSummary.mockReset().mockResolvedValue(null)
  mocks.statsTrend.mockReset().mockResolvedValue(null)
})

afterEach(() => {
  vi.useRealTimers()
})

function mountMonitor() {
  return mountView(Monitor, [], { global: { stubs: { REmpty: REmptyStub } } })
}

describe('Monitor 视图', () => {
  it('挂载即轮询：每 2 秒拉一次快照，卸载后不再拉', async () => {
    vi.useFakeTimers()
    const w = mountMonitor()

    // onMounted 里同步发起首拉
    expect(mocks.monitorSnapshot).toHaveBeenCalledTimes(1)

    vi.advanceTimersByTime(2000)
    await w.vm.$nextTick()
    expect(mocks.monitorSnapshot).toHaveBeenCalledTimes(2)

    w.unmount()
    vi.advanceTimersByTime(6000)
    await w.vm.$nextTick()
    // 锁住「卸载必须 stopPolling」：少这一次，离开页面后接口会被持续打 2s/次
    expect(mocks.monitorSnapshot).toHaveBeenCalledTimes(2)
  })

  it('非 Linux（ok=false）整页降级：只剩一句说明，没有图也没有历史 Tab', async () => {
    mocks.monitorSnapshot.mockResolvedValue({ ok: false, message: '当前平台不支持', sampled_at: 0 })
    const w = mountMonitor()
    await flushPromises()

    expect(w.find('.stub-empty').exists()).toBe(true)
    expect(w.text()).toContain('当前平台不支持资源监控')
    // 服务端给的原因原样展示，不自己编
    expect(w.text()).toContain('当前平台不支持')

    expect(w.findAll('.donut').length).toBe(0)
    expect(w.find('svg').exists()).toBe(false)
    // 历史 Tab 也一并藏掉：非 Linux 下历史接口同样是空的
    expect(w.findAll('[role="tab"]').length).toBe(0)
    expect(w.text()).not.toContain('24 小时')
  })

  it('正常渲染三个环与带宽数字，series 里的 null 不会崩也不会被当 0', async () => {
    const w = mountMonitor()
    await flushPromises()

    // 三个环：CPU / 内存 / 存储
    expect(w.findAll('.donut').length).toBe(3)

    // 阈值分档：cpu 42 → info，disk 91 → danger（环色跟着数据走）
    const rings = w.findAll('.donut-ring')
    expect(rings.length).toBe(3)
    expect(rings[0].attributes('stroke')).toBe('var(--info)')
    expect(rings[2].attributes('stroke')).toBe('var(--danger)')

    // 带宽：formatSpeed(2048) = 2.0 KB/s，formatSpeed(1MB) = 1.0 MB/s
    expect(w.text()).toContain('上行')
    expect(w.text()).toContain('2.0 KB/s')
    expect(w.text()).toContain('下行')
    expect(w.text()).toContain('1.0 MB/s')

    // cpu: [1, null, 3] → 只有 2 个有效点（null 被跳过，不折成 0 画出假谷底）
    expect(w.text()).toContain('2 个有效点')
    expect(w.findAll('.tchart-svg').length).toBeGreaterThan(0)
  })

  it('切到 24 小时才请求历史，并把粒度换算成人话', async () => {
    const w = mountMonitor()
    await flushPromises()
    // 实时 Tab 不该预取历史
    expect(mocks.monitorHistory).not.toHaveBeenCalled()

    const tab24 = w.findAll('[role="tab"]').find((b) => b.text().includes('24 小时'))
    await tab24!.trigger('click')
    await flushPromises()

    expect(mocks.monitorHistory).toHaveBeenCalledWith('24h')
    expect(w.text()).toContain('共 2 个采样点')
    // 1440 秒 = 24 分钟一个点（按分钟说，人才算得清）
    expect(w.text()).toContain('每 24 分钟一个点')
    // CPU 与内存各一条折线
    expect(w.findAll('.tchart-svg').length).toBe(2)
  })

  it('历史为空时给「没有采样记录」，不画空白图', async () => {
    mocks.monitorHistory.mockResolvedValue({ range: '1h', granularity_sec: 60, points: [] })
    const w = mountMonitor()
    await flushPromises()

    const tab1h = w.findAll('[role="tab"]').find((b) => b.text().includes('1 小时'))
    await tab1h!.trigger('click')
    await flushPromises()

    expect(w.text()).toContain('这段时间没有采样记录')
    expect(w.findAll('.tchart-svg').length).toBe(0)
  })
})
