import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { mountView } from '@/test/mount'
import Overview from '@/views/Overview.vue'
import { useTasksStore } from '@/stores/tasks'
import { makeTask } from '@/test/fixtures'
import {
  PlatformBadgeStub,
  RButtonStub,
  RouterLinkStub,
  REmptyStub,
  RaccoonFaceStub,
  StatusDotStub,
  TaskProgressStub,
} from '@/test/stubs'

/**
 * Overview.vue 用例（docs/14 §3.2 主页 17 块 / E1 布局 / E2 预警 / 诚实原则）。
 *
 * 断言落在可观测结果上：卡片文案与数量、成功率空值显示「—」、
 * 隐藏卡不再渲染、保存布局真的调了接口、监控降级不画假图。
 */
vi.mock('@/api/client', () => ({
  api: {
    statsSummary: vi.fn(async () => ({
      uptime_sec: 3661,
      total_downloads: 42,
      today_downloads: 3,
      week_downloads: 11,
      success_rate: 97.6,
      media_bytes: 5 * 1024 * 1024 * 1024,
      media_by_kind: { video: 8, audio: 1, image: 4, subtitle: 0, other: 0 },
      sub_downloads: 9,
      favorite_downloads: 5,
      live_recording: 1,
    })),
    statsTrend: vi.fn(async (days: number) => ({
      days,
      points: Array.from({ length: days }, (_, i) => ({
        date: `2026-10-${String(i + 1).padStart(2, '0')}`,
        count: i % 3,
        bytes: 1024,
      })),
      failures: { by_platform: [], by_reason: [{ key: 'COOKIE_EXPIRED', count: 3 }] },
    })),
    monitorSnapshot: vi.fn(async () => ({
      ok: true,
      sampled_at: 1730000000,
      cpu: { used_pct: 12.5 },
      memory: { used_pct: 41, used_bytes: 4e9, free_bytes: 6e9, total_bytes: 1e10 },
      disk: { used_pct: 67, free_bytes: 12e9, total_bytes: 40e9 },
      net: { up_bps: 1200, down_bps: 2_500_000 },
      series: { cpu: [1], memory: [1], net_up: [0], net_down: [0] },
    })),
    getPreferences: vi.fn(async () => ({ data: { layout: '' } })),
    savePreferences: vi.fn(async (layout: string) => ({ data: { layout } })),
    systemInfo: vi.fn(async () => ({ version: '0.1.0', uptime: 3661, arch: 'arm64' })),
    health: vi.fn(async () => ({ status: 'ok', checks: { disk_used_pct: 42 } })),
    queueStats: vi.fn(async () => ({
      counts: { waiting: 1, parsing: 1, downloading: 1, post: 0, done: 3, failed: 1, cancelled: 0 },
      total_tasks: 7,
      media_count: 12,
      media_bytes: 5 * 1024 * 1024 * 1024,
    })),
    listCredentials: vi.fn(async () => ({ items: [] })),
    listLiveSources: vi.fn(async () => ({
      items: [
        { id: 'l1', platform: 'bilibili', room_url: 'https://live.bilibili.com/1', title: '晚间直播', status: 'recording' },
      ],
    })),
  },
}))

const stubs = {
  RButton: RButtonStub,
  REmpty: REmptyStub,
  RaccoonFace: RaccoonFaceStub,
  StatusDot: StatusDotStub,
  TaskProgress: TaskProgressStub,
  PlatformBadge: PlatformBadgeStub,
  'router-link': RouterLinkStub,
}

// 每个用例前清调用记录（保留默认实现）：否则「trend 被调了几次」这类断言
// 会把上一个用例的调用一起算进来。
beforeEach(() => {
  vi.clearAllMocks()
})

function mountOverview() {
  return mountView(Overview, [], { global: { stubs } })
}

describe('Overview 视图', () => {
  it('首屏渲染 12 张指标卡（复刻原版 17 块的信息密度）', async () => {
    const w = mountOverview()
    await flushPromises()
    const labels = w.findAll('.mcard-label').map((n) => n.text())
    for (const want of [
      '运行时长', '总下载', '今日下载', '本周下载', '成功率',
      '下载总量', '图片', '视频', '订阅下载', '收藏下载', '直播录制', '待处理问题',
    ]) {
      expect(labels).toContain(want)
    }
  })

  it('指标值来自 summary 接口（不前端瞎算）', async () => {
    const w = mountOverview()
    await flushPromises()
    const text = w.text()
    expect(text).toContain('97.6%')
    expect(text).toContain('5.0 GB')
    expect(text).toContain('本次启动至今')
  })

  it('成功率分母为 0 时显示「—」，不造 0%/100%', async () => {
    const { api } = await import('@/api/client')
    vi.mocked(api.statsSummary).mockResolvedValueOnce({
      uptime_sec: 10,
      total_downloads: 0,
      today_downloads: 0,
      week_downloads: 0,
      success_rate: null,
      media_bytes: 0,
      media_by_kind: {},
      sub_downloads: 0,
      favorite_downloads: 0,
      live_recording: 0,
    })
    const w = mountOverview()
    await flushPromises()
    const rate = w.findAll('.mcard').find((c) => c.text().includes('成功率'))
    expect(rate?.text()).toContain('—')
  })

  it('趋势图与失败拆分都来自 trend 接口（7 天与 30 天都取）', async () => {
    const { api } = await import('@/api/client')
    const w = mountOverview()
    await flushPromises()
    expect(vi.mocked(api.statsTrend).mock.calls.map((c) => c[0])).toEqual([7, 30])
    expect(w.find('polyline').exists()).toBe(true)
    expect(w.text()).toContain('COOKIE_EXPIRED')

    await w.findAll('.trend-tab')[1].trigger('click')
    expect(w.findAll('.trend-tab')[1].classes()).toContain('on')
  })

  it('监控不可用时如实说明，不画假环（诚实原则）', async () => {
    const { api } = await import('@/api/client')
    vi.mocked(api.monitorSnapshot).mockResolvedValueOnce({
      ok: false,
      message: '当前平台不支持资源监控（仅 Linux 采集 /proc）',
      sampled_at: 0,
    })
    const w = mountOverview()
    await flushPromises()
    expect(w.text()).toContain('当前平台不支持资源监控')
    expect(w.findAll('.donut').length).toBe(0)
  })

  it('监控可用时三环 + 带宽数字都在', async () => {
    const w = mountOverview()
    await flushPromises()
    expect(w.findAll('.donut').length).toBe(3)
    expect(w.text()).toContain('Mbps')
  })

  it('E1：被隐藏的卡片不渲染（偏好来自 /user/preferences）', async () => {
    const { api } = await import('@/api/client')
    vi.mocked(api.getPreferences).mockResolvedValueOnce({
      data: { layout: JSON.stringify({ hidden: ['images', 'videos'], order: [] }) },
    })
    const w = mountOverview()
    await flushPromises()
    const labels = w.findAll('.mcard-label').map((n) => n.text())
    expect(labels).not.toContain('图片')
    expect(labels).not.toContain('视频')
    expect(labels).toContain('今日下载')
  })

  it('E1：自定义布局保存后调 savePreferences 并落成新布局', async () => {
    const { api } = await import('@/api/client')
    const w = mountOverview()
    await flushPromises()

    const openBtn = w.findAll('button').find((b) => b.text().includes('调整主页布局'))
    expect(openBtn).toBeTruthy()
    await openBtn!.trigger('click')
    await flushPromises()

    // 对话框在 body 里（Teleport）：取消勾选第一项 → 保存
    const first = document.body.querySelector<HTMLInputElement>('.layout-item input')
    expect(first).not.toBeNull()
    first!.click()
    await flushPromises()
    document.body.querySelector<HTMLButtonElement>('[data-rdialog-action="confirm"]')?.click()
    await flushPromises()

    expect(vi.mocked(api.savePreferences)).toHaveBeenCalled()
    const calls = vi.mocked(api.savePreferences).mock.calls
    const saved = JSON.parse(calls[calls.length - 1][0] as string)
    expect(saved.hidden.length).toBe(1)
    expect(saved.order.length).toBeGreaterThan(0)
  })

  it('E1：隐藏过的卡片仍在布局面板里，能再勾回来（不能一藏就消失）', async () => {
    const { api } = await import('@/api/client')
    vi.mocked(api.getPreferences).mockResolvedValueOnce({
      data: { layout: JSON.stringify({ hidden: ['images', 'videos'], order: [] }) },
    })
    const w = mountOverview()
    await flushPromises()
    const openBtn = w.findAll('button').find((b) => b.text().includes('调整主页布局'))
    await openBtn!.trigger('click')
    await flushPromises()

    // 面板必须列出**全部**卡片：只列可见的话，隐藏项没有入口能恢复
    const items = Array.from(document.body.querySelectorAll('.layout-item'))
    const labels = items.map((li) => li.textContent?.trim() ?? '')
    expect(labels).toContain('图片')
    expect(labels).toContain('视频')

    const imgBox = items
      .find((li) => li.textContent?.includes('图片'))
      ?.querySelector('input') as HTMLInputElement
    expect(imgBox.checked).toBe(false) // 隐藏 = 未勾选
    imgBox.click()
    await flushPromises()
    document.body.querySelector<HTMLButtonElement>('[data-rdialog-action="confirm"]')?.click()
    await flushPromises()

    const calls = vi.mocked(api.savePreferences).mock.calls
    const saved = JSON.parse(calls[calls.length - 1][0] as string)
    expect(saved.hidden).not.toContain('images')
    expect(saved.hidden).toContain('videos')
  })

  it('E2：成功率低于阈值时浮出预警条目并可跳转', async () => {
    const { api } = await import('@/api/client')
    vi.mocked(api.statsSummary).mockResolvedValueOnce({
      uptime_sec: 10,
      total_downloads: 10,
      today_downloads: 1,
      week_downloads: 2,
      success_rate: 72,
      media_bytes: 0,
      media_by_kind: {},
      sub_downloads: 0,
      favorite_downloads: 0,
      live_recording: 0,
    })
    const w = mountOverview()
    await flushPromises()
    expect(w.find('.alert-list').exists()).toBe(true)
    expect(w.text()).toContain('低于阈值')
  })

  it('无进行中任务时显示拟人空态（RaccoonFace + 一句人话），不是空白', async () => {
    const w = mountOverview()
    await flushPromises()
    expect(w.find('.inline-empty').exists()).toBe(true)
    expect(w.find('.inline-empty-title').text()).toContain('活都搬完了')
    expect(w.find('.inline-empty .stub-face').attributes('data-face')).toBe('done')
  })

  it('有进行中任务时逐行展示（状态点 + 进度 + 标题）', async () => {
    const w = mountOverview()
    const tasks = useTasksStore()
    tasks.items = [makeTask({ id: 'a', status: 'downloading', title: '第一集' })]
    await flushPromises()
    expect(w.find('.task-list').exists()).toBe(true)
    expect(w.findAll('.task-row').length).toBe(1)
    expect(w.find('.stub-dot').attributes('data-status')).toBe('downloading')
  })

  it('失败任务进 IssuesPanel 且最多 5 条', async () => {
    const w = mountOverview()
    const tasks = useTasksStore()
    tasks.items = Array.from({ length: 8 }, (_, i) => makeTask({ id: `f${i}`, status: 'failed', title: `失败${i}` }))
    await flushPromises()
    expect(w.find('.issue-panel').exists()).toBe(true)
    expect(w.findAll('.issue-row').length).toBe(5)
  })

  it('接口失败显示错误卡而不是白屏', async () => {
    const { api } = await import('@/api/client')
    vi.mocked(api.statsSummary).mockRejectedValueOnce(new Error('统计拉不到'))
    const w = mountOverview()
    await flushPromises()
    expect(w.find('.error-card').exists()).toBe(true)
  })
})
