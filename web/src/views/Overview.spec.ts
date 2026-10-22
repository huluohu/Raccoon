import { describe, expect, it, vi } from 'vitest'
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
 * Overview.vue 用例（docs/07 §3：QuickSubmit / ActiveTasks / IssuesPanel / StatusStrip）。
 */
vi.mock('@/api/client', () => ({
  api: {
    systemInfo: vi.fn(async () => ({ version: '0.1.0', uptime: 3661, arch: 'arm64' })),
    health: vi.fn(async () => ({ status: 'ok', checks: { disk_used_pct: 42 } })),
    queueStats: vi.fn(async () => ({
      counts: { waiting: 1, parsing: 1, downloading: 1, post: 0, done: 3, failed: 1, cancelled: 0 },
      media_count: 12,
      media_bytes: 5 * 1024 * 1024 * 1024,
    })),
    listCredentials: vi.fn(async () => ({ items: [] })),
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

function mountOverview() {
  return mountView(Overview, [], { global: { stubs } })
}

describe('Overview 视图', () => {
  it('加载后展示核心指标卡（进行中 / 文件数 / 总体积 / 版本）', async () => {
    const w = mountOverview()
    await flushPromises()
    expect(w.text()).toContain('进行中')
    expect(w.text()).toContain('个文件')
    expect(w.text()).toContain('0.1.0')
    // 5.00 GB（格式化后可读）
    expect(w.text()).toMatch(/总体积/)
  })

  it('无进行中任务时显示拟人空态（RaccoonFace + 一句人话），不是空白', async () => {
    const w = mountOverview()
    await flushPromises()
    expect(w.find('.inline-empty').exists()).toBe(true)
    // mock 里 counts.done = 3 → 有成功记录 → 走 done 分支（§5「表情按状态取用」）
    expect(w.find('.inline-empty-title').text()).toContain('活都搬完了')
    expect(w.find('.inline-empty .stub-face').attributes('data-face')).toBe('done')
  })

  it('没有成功记录时空态用 waiting 表情（§5 表情按状态取用）', async () => {
    const { api } = await import('@/api/client')
    vi.mocked(api.queueStats).mockResolvedValueOnce({
      counts: { waiting: 0, parsing: 0, downloading: 0, post: 0, done: 0, failed: 0, cancelled: 0 },
      media_count: 0,
      media_bytes: 0,
    } as any)
    const w = mountOverview()
    await flushPromises()
    expect(w.find('.inline-empty-title').text()).toContain('眼罩搁下了')
    expect(w.find('.inline-empty .stub-face').attributes('data-face')).toBe('waiting')
  })

  it('有进行中任务时逐行展示（状态点 + 进度 + 标题）', async () => {
    // 先挂载（mountView 会 new 一个新 pinia），再从同一个 pinia 取 store 灌数据；
    // 反过来写在挂载前，改的是上一个被丢弃的 pinia 里的 store，视图拿到的是空 store。
    const w = mountOverview()
    const tasks = useTasksStore()
    tasks.items = [makeTask({ id: 'a', status: 'downloading', title: '第一集' })]
    await flushPromises()
    expect(w.find('.task-list').exists()).toBe(true)
    expect(w.findAll('.task-row').length).toBe(1)
    expect(w.find('.stub-dot').attributes('data-status')).toBe('downloading')
  })

  it('失败任务进 IssuesPanel 且最多 5 条（docs/07 §3）', async () => {
    const w = mountOverview()
    const tasks = useTasksStore()
    tasks.items = Array.from({ length: 8 }, (_, i) => makeTask({ id: `f${i}`, status: 'failed', title: `失败${i}` }))
    await flushPromises()
    expect(w.find('.issue-panel').exists()).toBe(true)
    expect(w.findAll('.issue-row').length).toBe(5)
  })

  it('未配凭据时如实说明「还没配平台凭据」，不假装可用', async () => {
    const w = mountOverview()
    await flushPromises()
    expect(w.text()).toContain('还没配平台凭据')
  })

  it('接口失败显示错误卡而不是白屏', async () => {
    const { api } = await import('@/api/client')
    vi.mocked(api.systemInfo).mockRejectedValueOnce(new Error('系统信息拉不到'))
    const w = mountOverview()
    await flushPromises()
    expect(w.find('.error-card').exists()).toBe(true)
  })
})
