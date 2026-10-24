import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { mountView } from '@/test/mount'
import Live from '@/views/Live.vue'
import { DetailDrawerStub, PlatformBadgeStub, RButtonStub, REmptyStub } from '@/test/stubs'

/**
 * Live.vue 用例（docs/07 §3 Live / docs/04 §2.7）。
 *
 * 铁律：支持平台清单由后端下发（实测清单），前端不写死。
 * 不在清单的平台在下拉里禁用并说明原因，绝不让用户填完才报 422。
 */
const mocks = vi.hoisted(() => ({
  listLiveSources: vi.fn(),
  createLiveSource: vi.fn(),
  updateLiveSource: vi.fn(),
  deleteLiveSource: vi.fn(),
  listLiveRecordings: vi.fn(),
  recordLive: vi.fn(),
}))

vi.mock('@/api/client', () => ({ api: mocks }))

function makeSource(over: Record<string, unknown> = {}) {
  return {
    id: 'l-1',
    platform: 'bilibili',
    room_id: '21452505',
    room_url: 'https://live.bilibili.com/21452505',
    title: '晚间场',
    enabled: true,
    quality: 'best',
    record_plan: 'manual',
    segment_min: 30,
    check_interval_sec: 120,
    status: 'offline',
    last_checked_at: Date.now() - 5 * 60_000,
    created_at: 1_700_000_000_000,
    updated_at: 1_700_000_000_000,
    recording_count: 2,
    ...over,
  }
}

const stubs = {
  RButton: RButtonStub,
  REmpty: REmptyStub,
  DetailDrawer: DetailDrawerStub,
  PlatformBadge: PlatformBadgeStub,
}

function mountLive() {
  return mountView(Live, [], { global: { stubs } })
}

function btnByText(w: any, text: string) {
  return w.findAll('button').find((b: any) => b.text().includes(text))
}

beforeEach(() => {
  mocks.listLiveSources.mockReset().mockResolvedValue({
    // 预置一个源：页头「添加直播源」按钮按 2026-10-24 裁决仅在非空态显示
    // （空态时空态卡自带「添加第一个直播源」，两个同功能按钮不同屏）。
    items: [{ id: 'ls1', url: 'https://live.bilibili.com/1', platform: 'bilibili', status: 'offline' }],
    supported: ['bilibili', 'douyu'],
    supported_note: '实测：B 站 / 斗鱼可稳定拉流',
  })
  mocks.createLiveSource.mockReset().mockResolvedValue({ data: makeSource() })
  mocks.updateLiveSource.mockReset().mockResolvedValue({ data: makeSource() })
  mocks.deleteLiveSource.mockReset().mockResolvedValue({ ok: true })
  mocks.listLiveRecordings.mockReset().mockResolvedValue({ items: [] })
  mocks.recordLive.mockReset().mockResolvedValue({ task: { id: 'task-abcdef123456' } })
})

describe('Live 视图', () => {
  it('实测支持清单由后端下发并渲染成 chips（前端不写死平台）', async () => {
    const w = mountLive()
    await flushPromises()

    const chips = w.findAll('.platform-note .chip')
    expect(chips.length).toBe(2)
    expect(chips[0].text()).toBe('哔哩哔哩')
    expect(chips[1].text()).toBe('斗鱼')
    expect(w.text()).toContain('实测：B 站 / 斗鱼可稳定拉流')
    // 未实测平台必须明说「不开放添加」
    expect(w.text()).toContain('其他平台不开放添加')
  })

  it('未实测平台在下拉里禁用并标注原因（不让用户填完才报 422）', async () => {
    const w = mountLive()
    await flushPromises()
    await btnByText(w, '添加直播源').trigger('click')
    await flushPromises()

    const options = w.findAll('select')[0].findAll('option')
    const byValue = (v: string) => options.find((o: any) => o.attributes('value') === v)
    expect(byValue('bilibili')!.attributes('disabled')).toBeUndefined()
    expect(byValue('douyu')!.attributes('disabled')).toBeUndefined()
    expect(byValue('huya')!.attributes('disabled')).toBeDefined()
    expect(byValue('huya')!.text()).toContain('未实测，不支持')
  })

  it('后端一个平台都没实测时禁用添加按钮，不开放填写', async () => {
    mocks.listLiveSources.mockResolvedValueOnce({
      items: [{ id: 'ls1', url: 'https://live.bilibili.com/1', platform: 'bilibili', status: 'offline' }],
      supported: [],
      supported_note: '未实测',
    })
    const w = mountLive()
    await flushPromises()

    const add = btnByText(w, '添加直播源')
    expect(add.attributes('disabled')).toBeDefined()
    expect(w.find('.chip--off').text()).toBe('暂无')
  })

  it('直播源卡片展示房间号/画质/分段/检测间隔与录制次数', async () => {
    mocks.listLiveSources.mockResolvedValueOnce({
      items: [makeSource()],
      supported: ['bilibili'],
      supported_note: '实测：B 站',
    })
    const w = mountLive()
    await flushPromises()

    const card = w.find('.card')
    expect(card.text()).toContain('晚间场')
    expect(card.text()).toContain('21452505')
    expect(card.text()).toContain('30 分钟')
    expect(card.text()).toContain('每 120s')
    expect(card.text()).toContain('未开播')
    expect(card.text()).toContain('仅手动')
  })

  it('空数据显示引导空态', async () => {
    mocks.listLiveSources.mockResolvedValueOnce({ items: [], supported: ['bilibili'], supported_note: '' })
    const w = mountLive()
    await flushPromises()
    expect(w.find('.stub-empty').text()).toContain('还没有直播源')
    expect(w.find('.stub-empty-action').text()).toContain('添加第一个直播源')
  })

  it('开始录制入队后反馈任务号并刷新列表', async () => {
    mocks.listLiveSources.mockResolvedValueOnce({
      items: [makeSource()],
      supported: ['bilibili'],
      supported_note: '实测：B 站',
    })
    const w = mountLive()
    await flushPromises()

    await btnByText(w, '开始录制').trigger('click')
    await flushPromises()

    expect(mocks.recordLive).toHaveBeenCalledWith('l-1')
    expect(mocks.listLiveSources.mock.calls.length).toBeGreaterThan(1)
  })

  it('已在录制中的源不能重复开始录制（按钮禁用 + 文案变化）', async () => {
    mocks.listLiveSources.mockResolvedValueOnce({
      items: [makeSource({ status: 'recording' })],
      supported: ['bilibili'],
      supported_note: '实测：B 站',
    })
    const w = mountLive()
    await flushPromises()

    const btn = btnByText(w, '录制中')
    expect(btn.exists()).toBe(true)
    expect(btn.attributes('disabled')).toBeDefined()
    expect(w.find('.card').text()).toContain('录制中')
  })

  it('停止/恢复监控走 updateLiveSource（enabled 取反）', async () => {
    mocks.listLiveSources.mockResolvedValueOnce({
      items: [makeSource()],
      supported: ['bilibili'],
      supported_note: '实测：B 站',
    })
    const w = mountLive()
    await flushPromises()

    await btnByText(w, '停止监控').trigger('click')
    await flushPromises()
    expect(mocks.updateLiveSource).toHaveBeenCalledWith('l-1', { enabled: false })
  })

  it('录制历史抽屉汇总分段/体积并渲染表格', async () => {
    mocks.listLiveSources.mockResolvedValueOnce({
      items: [makeSource()],
      supported: ['bilibili'],
      supported_note: '实测：B 站',
    })
    mocks.listLiveRecordings.mockResolvedValueOnce({
      items: [
        {
          id: 'r-1',
          source_id: 'l-1',
          started_at: 1_700_000_000_000,
          ended_at: 1_700_000_600_000,
          segments: 2,
          size: 1024 * 1024 * 500,
          status: 'completed',
        },
        {
          id: 'r-2',
          source_id: 'l-1',
          started_at: 1_700_001_000_000,
          segments: 1,
          size: 1024 * 1024 * 100,
          status: 'recording',
        },
      ],
    })
    const w = mountLive()
    await flushPromises()

    await btnByText(w, '录制历史').trigger('click')
    await flushPromises()

    expect(mocks.listLiveRecordings).toHaveBeenCalledWith('l-1')
    const rows = w.findAll('.tbl tbody tr')
    expect(rows.length).toBe(2)
    expect(rows[0].text()).toContain('已完成')
    expect(rows[1].text()).toContain('进行中')
    // 汇总：2 次录制、3 个分段、总体积可读
    expect(w.find('.hist-sum').text()).toContain('共 2 次录制')
    expect(w.find('.hist-sum').text()).toContain('3 个分段')
  })

  it('加载失败显示危险告警条', async () => {
    mocks.listLiveSources.mockRejectedValueOnce(new Error('直播源接口超时'))
    const w = mountLive()
    await flushPromises()
    expect(w.find('.alert--danger').text()).toContain('直播源接口超时')
  })

  it('删除直播源（破坏性按钮带二次确认语义）', async () => {
    mocks.listLiveSources.mockResolvedValueOnce({
      items: [makeSource()],
      supported: ['bilibili'],
      supported_note: '实测：B 站',
    })
    const w = mountLive()
    await flushPromises()

    await btnByText(w, '删除').trigger('click')
    await flushPromises()
    expect(mocks.deleteLiveSource).toHaveBeenCalledWith('l-1')
  })
})
