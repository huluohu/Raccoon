import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
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
  listTasks: vi.fn(),
  listFiles: vi.fn(),
  createTasks: vi.fn(),
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
    mocks.listTasks.mockResolvedValue({ items: [], counts: {}, next_cursor: null })
    mocks.listFiles.mockResolvedValue({ items: [], total: 0, platforms: [] })
    mocks.createTasks.mockResolvedValue({ data: [] })
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

  // ---- 2026-10-25 全量 review 定位 ----

  it('媒体库加载失败要说出来，不能假装成「没有文件」', async () => {
    mocks.listFiles.mockRejectedValueOnce(new Error('后端连不上'))
    const w = mountView(Remote, [], { global: { stubs } })
    await flushPromises()

    expect(w.find('.remote-media-error').text()).toContain('后端连不上')
    // 失败时不能显示「还没有下载完成的文件」这种误导性空态
    expect(w.text()).not.toContain('还没有下载完成的文件')
    // 有重试入口
    expect(w.find('.remote-retry-hint').exists()).toBe(true)
  })

  it('浏览器播不了的文件不进播放器（不渲染坏掉的播放器）', async () => {
    mocks.listFiles.mockResolvedValueOnce({
      items: [
        { id: 'f1', name: '打不开.mkv', size: 1024, kind: 'video', previewable: false, created_at: 1 },
      ],
      total: 1,
      platforms: [],
    })
    const w = mountView(Remote, [], { global: { stubs } })
    await flushPromises()

    await w.find('.remote-media-item').trigger('click')
    await flushPromises()

    expect(w.find('.remote-player-box').exists()).toBe(false)
    // 文件仍列出来，并标出「需下载」
    expect(w.find('.remote-media-noplay-badge').exists()).toBe(true)
  })

  it('能播的文件点开就出播放器', async () => {
    mocks.listFiles.mockResolvedValueOnce({
      items: [
        { id: 'f2', name: '能播.mp4', size: 1024, kind: 'video', previewable: true, created_at: 1 },
      ],
      total: 1,
      platforms: [],
    })
    const w = mountView(Remote, [], { global: { stubs } })
    await flushPromises()

    await w.find('.remote-media-item').trigger('click')
    await flushPromises()
    expect(w.find('.remote-player-box').exists()).toBe(true)
    expect(w.find('video').attributes('src')).toBe('/api/v1/files/f2/preview')
  })
})
