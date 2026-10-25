import { beforeEach, describe, expect, it, vi } from 'vitest'
import Remote from '@/views/Remote.vue'
import { mountView } from '@/test/mount'

class FakeEventSource {
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  addEventListener = vi.fn()
  close = vi.fn()
}
// @ts-expect-error test mock
globalThis.EventSource = FakeEventSource

const mocks = vi.hoisted(() => ({
  listTasks: vi.fn().mockResolvedValue({ items: [], counts: {}, next_cursor: null }),
  listFiles: vi.fn().mockResolvedValue({ items: [], total: 0, platforms: [] }),
  createTasks: vi.fn().mockResolvedValue({ data: [] }),
  filePreviewUrl: (id: string) => `/api/v1/files/${id}/preview`,
}))

vi.mock('@/api/client', () => ({ api: mocks }))

const stubs = {
  RaccoonFace: { template: '<div class="stub-face" />' },
  PlatformBadge: { template: '<div class="stub-badge" />' },
  RButton: { template: '<button @click="$emit(\'click\')"><slot /></button>' },
}

describe('Remote 移动端遥控器视图', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('渲染遥控器顶栏与品牌标题', () => {
    const w = mountView(Remote, [], { global: { stubs } })
    expect(w.find('.remote-title').text()).toContain('Raccoon 遥控器')
    expect(w.find('.remote-input').exists()).toBe(true)
  })

  it('输入链接并下发任务', async () => {
    const w = mountView(Remote, [], { global: { stubs } })
    const input = w.find('.remote-input')
    await input.setValue('https://www.bilibili.com/video/BV1GJ411x7h7')
    expect((input.element as HTMLInputElement).value).toBe('https://www.bilibili.com/video/BV1GJ411x7h7')

    const btn = w.find('.remote-submit-btn')
    expect(btn.exists()).toBe(true)
  })

  it('无活跃任务时显示引导空态', () => {
    const w = mountView(Remote, [], { global: { stubs } })
    expect(w.find('.remote-empty').text()).toContain('当前无下载任务')
  })
})
