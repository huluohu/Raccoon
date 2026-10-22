import { describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import Media from '@/views/Media.vue'
import type { MediaFile } from '@/api/types'
import {
  DetailDrawerStub,
  PlatformBadgeStub,
  RButtonStub,
  REmptyStub,
  RSelectStub,
  RTableStub,
} from '@/test/stubs'

/**
 * Media.vue 用例（docs/07 §3：FileTable/FileCard 双形态；空态/加载/错误三分）。
 */
vi.mock('@/api/client', () => {
  const listFiles = vi.fn(async () => ({
    items: [
      { id: 'f1', name: '第一集.mp4', size: 1024, kind: 'video', platform: 'bilibili', duration: 600, created_at: 1_700_000_000_000, path: '/d/1.mp4', previewable: true },
      { id: 'f2', name: '封面.png', size: 2048, kind: 'image', platform: 'youtube', created_at: 1_700_000_100_000, path: '/d/2.png', previewable: true },
    ] as MediaFile[],
    total: 2,
    platforms: ['bilibili', 'youtube'],
  }))
  return {
    api: {
      listFiles,
      filePreviewUrl: (id: string) => `/api/v1/files/${id}/preview`,
      fileDownloadUrl: (id: string) => `/api/v1/files/${id}/download`,
    },
  }
})

const stubs = {
  RButton: RButtonStub,
  RSelect: RSelectStub,
  RTable: RTableStub,
  DetailDrawer: DetailDrawerStub,
  PlatformBadge: PlatformBadgeStub,
  REmpty: REmptyStub,
}

function mountMedia() {
  setActivePinia(createPinia())
  return mount(Media, { global: { stubs } })
}

describe('Media 视图', () => {
  it('加载后展示文件数与卡片网格', async () => {
    const w = mountMedia()
    await flushPromises()
    expect(w.text()).toContain('2 个文件')
    expect(w.findAll('.file-card').length).toBe(2)
  })

  it('卡片 / 表格双视图可切换（docs/07 §3 FileTable 与 FileCard 并列）', async () => {
    const w = mountMedia()
    await flushPromises()
    expect(w.find('.grid').exists()).toBe(true)

    await w.findAll('.vt-btn')[1].trigger('click')
    expect(w.find('.stub-table').exists()).toBe(true)
    expect(w.find('.grid').exists()).toBe(false)
  })

  it('空数据显示引导空态（而不是空白页）', async () => {
    const { api } = await import('@/api/client')
    vi.mocked(api.listFiles).mockResolvedValueOnce({ items: [], total: 0, platforms: [] })
    const w = mountMedia()
    await flushPromises()
    expect(w.find('.stub-empty').exists()).toBe(true)
  })

  it('加载失败显示错误卡 + 重试入口', async () => {
    const { api } = await import('@/api/client')
    vi.mocked(api.listFiles).mockRejectedValueOnce(new Error('炸了'))
    const w = mountMedia()
    await flushPromises()
    expect(w.find('.error-card').exists()).toBe(true)
    expect(w.find('.error-card').text()).toContain('炸了')
  })

  it('搜索关键词变化会重新请求（防抖后）', async () => {
    const { api } = await import('@/api/client')
    const w = mountMedia()
    await flushPromises()
    const before = vi.mocked(api.listFiles).mock.calls.length
    await w.find('.search').setValue('测试')
    await new Promise((r) => setTimeout(r, 400))
    expect(vi.mocked(api.listFiles).mock.calls.length).toBeGreaterThan(before)
    // 关键词须以 q 参数传给后端
    const calls = vi.mocked(api.listFiles).mock.calls
    expect(calls[calls.length - 1]?.[0]).toMatchObject({ q: '测试' })
  })

  it('768–1023 档位表格降级为卡片，「表格」按钮禁用并说明原因（docs/07 §6）', async () => {
    // jsdom 的 matchMedia 恒返回 matches:false，这里按档位桩一个真的
    const spy = vi.spyOn(window, 'matchMedia').mockImplementation((q: string) => ({
      matches: q.includes('max-width: 1023px'),
      media: q,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    }) as MediaQueryList)

    const w = mountMedia()
    await flushPromises()

    // 该档位恒定卡片：不渲染表格
    expect(w.find('.grid').exists()).toBe(true)
    expect(w.find('.stub-table').exists()).toBe(false)
    // 手动切表格也切不过去，且按钮说明原因
    const tableBtn = w.findAll('.vt-btn')[1]
    expect(tableBtn.attributes('disabled')).toBeDefined()
    expect(tableBtn.attributes('title')).toContain('已降级为卡片')

    spy.mockRestore()
  })
})
