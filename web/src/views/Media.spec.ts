import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import Media from '@/views/Media.vue'
import type { MediaFile } from '@/api/types'
import { mount } from '@/test/mount'
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
 *
 * 2026-10-25 全量 review 后补：分页（文件超过一页也能取到）、请求竞态守卫、
 * 表格视图与卡片的动作等价性。
 */

// vi.mock 的工厂会被提升到文件顶部，引用不到后面才初始化的 const，故用 vi.hoisted 造 mock。
const mocks = vi.hoisted(() => ({
  listFiles: vi.fn(),
  deleteFile: vi.fn(),
  filePreviewUrl: (id: string) => `/api/v1/files/${id}/preview`,
  fileDownloadUrl: (id: string) => `/api/v1/files/${id}/download`,
}))

vi.mock('@/api/client', () => ({ api: mocks }))

/** 默认列表页（两个文件：视频 + 图片）。 */
function defaultPage() {
  return {
    items: [
      { id: 'f1', name: '第一集.mp4', size: 1024, kind: 'video', platform: 'bilibili', duration: 600, created_at: 1_700_000_000_000, path: '/d/1.mp4', previewable: true },
      { id: 'f2', name: '封面.png', size: 2048, kind: 'image', platform: 'youtube', created_at: 1_700_000_100_000, path: '/d/2.png', previewable: true },
    ] as MediaFile[],
    total: 2,
    platforms: ['bilibili', 'youtube'],
  }
}

beforeEach(() => {
  mocks.listFiles.mockReset().mockImplementation(async () => defaultPage())
  mocks.deleteFile.mockReset().mockResolvedValue({ ok: true })
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

/** 造一个媒体文件（默认视频）。 */
function mkFile(id: string, over: Partial<MediaFile> = {}): MediaFile {
  return {
    id,
    name: `${id}.mp4`,
    size: 1024,
    kind: 'video',
    platform: 'bilibili',
    created_at: 1_700_000_000_000,
    path: `/d/${id}.mp4`,
    previewable: true,
    ...over,
  } as MediaFile
}

/** 按文案找按钮。 */
function btnByText(w: any, text: string) {
  return w.findAll('button').find((b: any) => b.text().includes(text))
}

/** 最近一次 listFiles 的参数。 */
function lastParams(): Record<string, string> {
  const calls = mocks.listFiles.mock.calls
  return calls[calls.length - 1][0]
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
    mocks.listFiles.mockResolvedValueOnce({ items: [], total: 0, platforms: [] })
    const w = mountMedia()
    await flushPromises()
    expect(w.find('.stub-empty').exists()).toBe(true)
  })

  it('加载失败显示错误卡 + 重试入口', async () => {
    mocks.listFiles.mockRejectedValueOnce(new Error('炸了'))
    const w = mountMedia()
    await flushPromises()
    expect(w.find('.error-card').exists()).toBe(true)
    expect(w.find('.error-card').text()).toContain('炸了')
  })

  it('搜索关键词变化会重新请求（防抖后）', async () => {
    const w = mountMedia()
    await flushPromises()
    const before = mocks.listFiles.mock.calls.length
    await w.find('.search').setValue('测试')
    await new Promise((r) => setTimeout(r, 400))
    expect(mocks.listFiles.mock.calls.length).toBeGreaterThan(before)
    // 关键词须以 q 参数传给后端
    expect(lastParams()).toMatchObject({ q: '测试' })
  })

  it('删文件必须二次确认，确认后才真删并重拉列表（与删记录分离）', async () => {
    const w = mountMedia()
    await flushPromises()
    const before = mocks.listFiles.mock.calls.length

    // 卡片上点「删除」：先出确认框，此时不能发请求
    const delBtn = w.findAll('.file-card')[0].findAll('button').find((b) => b.text() === '删除')
    expect(delBtn).toBeTruthy()
    await delBtn!.trigger('click')
    await flushPromises()

    const dialog = document.body.querySelector('.rdialog-root')
    expect(dialog).not.toBeNull()
    expect(dialog?.querySelector('.rdialog-title')?.textContent).toContain('删除这个文件？')
    expect(dialog?.querySelector('.rdialog-desc')?.textContent).toContain('不可撤销')
    expect(mocks.deleteFile).not.toHaveBeenCalled()

    // 确认后才真删，并重拉列表（不本地过滤）
    document.body.querySelector<HTMLButtonElement>('[data-rdialog-action="confirm"]')?.click()
    await flushPromises()
    expect(mocks.deleteFile).toHaveBeenCalledWith('f1')
    expect(mocks.listFiles.mock.calls.length).toBeGreaterThan(before)
  })

  it('768–1023 档位表格降级为卡片，「表格」按钮禁用并说明原因（docs/07 §6）', async () => {    // jsdom 的 matchMedia 恒返回 matches:false，这里按档位桩一个真的
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

  // ---- 分页 / 请求竞态（2026-10-25 全量 review 定位）----

  it('文件超过一页时能继续加载：加载更多带 offset，列表变长', async () => {
    mocks.listFiles
      .mockResolvedValueOnce({ items: [mkFile('f1')], total: 2, platforms: [] })
      .mockResolvedValueOnce({ items: [mkFile('f2')], total: 2, platforms: [] })

    const w = mountMedia()
    await flushPromises()
    expect(w.findAll('.file-card').length).toBe(1)
    expect(w.text()).toContain('已加载 1 / 共 2 个')

    const more = btnByText(w, '加载更多')
    expect(more).toBeTruthy()
    await more!.trigger('click')
    await flushPromises()

    // 第二页的起点必须是已加载条数
    expect(lastParams()).toMatchObject({ offset: '1', limit: '100' })
    expect(w.findAll('.file-card').length).toBe(2)
    // 全部取完 → 不再显示「加载更多」
    expect(btnByText(w, '加载更多')).toBeUndefined()
    expect(w.text()).toContain('已经到底了')
  })

  it('旧请求后到不会覆盖新结果（请求序号守卫）', async () => {
    let resolveOld: ((v: { items: MediaFile[]; total: number; platforms: string[] }) => void) | undefined
    // 第一次请求挂住不返回
    mocks.listFiles.mockImplementationOnce(
      () => new Promise((res) => { resolveOld = res }) as never,
    )
    // 第二次请求立刻返回新结果
    mocks.listFiles.mockResolvedValueOnce({ items: [mkFile('新的')], total: 1, platforms: [] })

    const w = mountMedia()
    await flushPromises()

    // 改筛选 → 触发第二次请求
    await w.find('.search').setValue('新')
    await new Promise((r) => setTimeout(r, 400))
    await flushPromises()
    expect(w.text()).toContain('新的')

    // 旧请求这时才回来：不能把新结果盖掉
    resolveOld?.({ items: [mkFile('旧的')], total: 1, platforms: [] })
    await flushPromises()
    expect(w.text()).toContain('新的')
    expect(w.text()).not.toContain('旧的')
  })

  it('加载更多失败只提示，不清空已经拿到的列表', async () => {
    mocks.listFiles
      .mockResolvedValueOnce({ items: [mkFile('f1')], total: 5, platforms: [] })
      .mockRejectedValueOnce(new Error('第二页炸了'))

    const w = mountMedia()
    await flushPromises()
    await btnByText(w, '加载更多')!.trigger('click')
    await flushPromises()

    expect(w.find('.more-err').text()).toContain('第二页炸了')
    // 已有列表还在（整页错误态会把文件全藏掉）
    expect(w.findAll('.file-card').length).toBe(1)
  })

  it('表格视图也提供次要动作（与卡片等价，不能只卡片有）', async () => {
    const w = mountMedia()
    await flushPromises()
    await w.findAll('.vt-btn')[1].trigger('click')
    expect(w.find('.stub-table').exists()).toBe(true)

    // 行操作里有「更多」菜单（提音频 / 转 MP4 / 制作封面 / 元数据 / 刮削）
    const trigger = w.findAll('.stub-actions .rmenu-trigger-badge')
    expect(trigger.length).toBe(2)
    await trigger[0].trigger('click')
    const items = w.findAll('.rmenu-item').map((i) => i.text())
    expect(items.join('|')).toContain('提取 MP3 音频')
    expect(items.join('|')).toContain('制作封面')
    expect(items.join('|')).toContain('编辑元数据')
  })
})
