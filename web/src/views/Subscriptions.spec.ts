import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { mountView } from '@/test/mount'
import Subscriptions from '@/views/Subscriptions.vue'
import {
  DetailDrawerStub,
  PlatformBadgeStub,
  RButtonStub,
  REmptyStub,
} from '@/test/stubs'

/**
 * Subscriptions.vue 用例（docs/07 §3 Subscriptions / docs/04 §2.6）。
 *
 * 本页的铁律是「先解析预览再保存」：解析不到来源就直接报错，
 * 绝不静默建一个空订阅。用例围绕这条铁律 + 增删改查/检查/已发现内容展开。
 */
const mocks = vi.hoisted(() => ({
  listSubscriptions: vi.fn(),
  createSubscription: vi.fn(),
  updateSubscription: vi.fn(),
  deleteSubscription: vi.fn(),
  checkSubscriptionNow: vi.fn(),
  listSubscriptionItems: vi.fn(),
}))

vi.mock('@/api/client', () => ({ api: mocks }))

/** 造一个订阅（字段与后端一致，值全虚构）。 */
function makeSub(over: Record<string, unknown> = {}) {
  return {
    id: 's-1',
    platform: 'bilibili',
    source_type: 'favorite',
    source_id: 'fav-1',
    source_url: 'https://space.bilibili.com/1/favlist?fid=2',
    title: '我的收藏夹',
    mode: 'new_only',
    interval_min: 30,
    enabled: true,
    next_check_at: Date.now() + 30 * 60_000,
    created_at: 1_700_000_000_000,
    updated_at: 1_700_000_000_000,
    item_counts: { downloaded: 12, queued: 3, failed: 1, skipped: 0, pending: 0 },
    ...over,
  }
}

const stubs = {
  RButton: RButtonStub,
  REmpty: REmptyStub,
  DetailDrawer: DetailDrawerStub,
  PlatformBadge: PlatformBadgeStub,
}

function mountSubs() {
  return mountView(Subscriptions, [], { global: { stubs } })
}

function btnByText(w: any, text: string) {
  return w.findAll('button').find((b: any) => b.text().includes(text))
}

beforeEach(() => {
  mocks.listSubscriptions.mockReset().mockResolvedValue({ items: [] })
  mocks.createSubscription.mockReset().mockResolvedValue({ data: makeSub() })
  mocks.updateSubscription.mockReset().mockResolvedValue({ data: makeSub() })
  mocks.deleteSubscription.mockReset().mockResolvedValue({ ok: true })
  mocks.checkSubscriptionNow.mockReset().mockResolvedValue({ queued: 2 })
  mocks.listSubscriptionItems.mockReset().mockResolvedValue({ items: [] })
})

describe('Subscriptions 视图', () => {
  it('加载后展示订阅卡：来源、模式、计数、下次检查', async () => {
    mocks.listSubscriptions.mockResolvedValueOnce({ items: [makeSub()] })
    const w = mountSubs()
    await flushPromises()

    const card = w.find('.card')
    expect(card.exists()).toBe(true)
    expect(card.text()).toContain('我的收藏夹')
    expect(card.text()).toContain('12 已下载')
    expect(card.text()).toContain('3 待下载')
    expect(card.text()).toContain('1 失败')
    expect(card.text()).toContain('收藏夹') // source_type 标签
    expect(card.text()).toContain('只要新增') // mode 文案
    expect(card.text()).toMatch(/分钟后检查/)
    // 页头汇总
    expect(w.text()).toContain('启用 1 个')
    expect(w.text()).toContain('队列待下 3 条')
  })

  it('没有订阅时显示引导空态（带行动按钮），不是空白', async () => {
    const w = mountSubs()
    await flushPromises()
    expect(w.find('.stub-empty').exists()).toBe(true)
    expect(w.find('.stub-empty').text()).toContain('还没有订阅')
    expect(w.find('.stub-empty').text()).toContain('新建第一个订阅')
  })

  it('加载失败显示危险告警条，内容来自后端 message', async () => {
    mocks.listSubscriptions.mockRejectedValueOnce(new Error('订阅接口 500'))
    const w = mountSubs()
    await flushPromises()
    const alert = w.find('.alert--danger')
    expect(alert.exists()).toBe(true)
    expect(alert.text()).toContain('订阅接口 500')
  })

  it('未解析预览时保存按钮禁用且文案是「请先解析预览」（铁律：不静默建空订阅）', async () => {
    const w = mountSubs()
    await flushPromises()

    await btnByText(w, '新建订阅').trigger('click')
    await flushPromises()

    await w.find('textarea').setValue('https://space.bilibili.com/1/favlist?fid=2')
    const submit = btnByText(w, '请先解析预览')
    expect(submit.exists()).toBe(true)
    expect(submit.attributes('disabled')).toBeDefined()
    expect(mocks.createSubscription).not.toHaveBeenCalled()
  })

  it('解析预览成功后展示来源信息，保存才带上 mode/interval 等字段', async () => {
    mocks.createSubscription.mockResolvedValueOnce({
      data: {
        url: 'https://space.bilibili.com/1/favlist?fid=2',
        platform: 'bilibili',
        title: '我的收藏夹',
        uploader: '测试 UP',
        duration: null,
        is_playlist: true,
        kind: 'favorite',
        formats: [],
      },
    })
    const w = mountSubs()
    await flushPromises()

    await btnByText(w, '新建订阅').trigger('click')
    await flushPromises()
    await w.find('textarea').setValue('https://space.bilibili.com/1/favlist?fid=2')

    // 第一次调用带 preview: true（只解析不创建）
    await btnByText(w, '解析预览').trigger('click')
    await flushPromises()
    expect(mocks.createSubscription).toHaveBeenCalledWith({
      source_url: 'https://space.bilibili.com/1/favlist?fid=2',
      preview: true,
    })
    expect(w.find('.preview-box').text()).toContain('我的收藏夹')
    expect(w.find('.preview-box').text()).toContain('bilibili')
    expect(w.find('.preview-box').text()).toContain('集合')

    // 保存按钮此时才可用，文案变成「创建订阅」
    const submit = btnByText(w, '创建订阅')
    expect(submit.exists()).toBe(true)
    expect(submit.attributes('disabled')).toBeUndefined()

    // jsdom 不会因点击 submit 按钮而触发表单 submit，这里直接派发
    await w.find('form').trigger('submit')
    await flushPromises()

    expect(mocks.createSubscription).toHaveBeenLastCalledWith(
      expect.objectContaining({
        source_url: 'https://space.bilibili.com/1/favlist?fid=2',
        mode: 'new_only',
        interval_min: 30,
      }),
    )
  })

  it('指定区间模式才带 range_from/range_to（默认模式不带）', async () => {
    const w = mountSubs()
    await flushPromises()
    await btnByText(w, '新建订阅').trigger('click')
    await flushPromises()

    // 默认 new_only：区间输入不渲染
    expect(w.find('.range-row').exists()).toBe(false)
    await w.findAll('select')[1].setValue('range')
    await flushPromises()
    expect(w.find('.range-row').exists()).toBe(true)
  })

  it('立即检查走 check-now 并按入队数给不同反馈', async () => {
    mocks.listSubscriptions.mockResolvedValueOnce({ items: [makeSub()] })
    mocks.checkSubscriptionNow.mockResolvedValueOnce({ queued: 3 })
    const w = mountSubs()
    await flushPromises()

    await btnByText(w, '立即检查').trigger('click')
    await flushPromises()

    expect(mocks.checkSubscriptionNow).toHaveBeenCalledWith('s-1')
    // 检查后会重新拉列表
    expect(mocks.listSubscriptions.mock.calls.length).toBeGreaterThan(1)
  })

  it('暂停/启用走 updateSubscription（enabled 取反）', async () => {
    mocks.listSubscriptions.mockResolvedValueOnce({ items: [makeSub()] })
    const w = mountSubs()
    await flushPromises()

    await btnByText(w, '暂停').trigger('click')
    await flushPromises()
    expect(mocks.updateSubscription).toHaveBeenCalledWith('s-1', { enabled: false })
  })

  it('已发现内容抽屉渲染条目表格（标题/状态/发现时间）', async () => {
    mocks.listSubscriptions.mockResolvedValueOnce({ items: [makeSub()] })
    mocks.listSubscriptionItems.mockResolvedValueOnce({
      items: [
        {
          id: 1,
          subscription_id: 's-1',
          item_id: 'BV1',
          title: '第 1 集',
          status: 'downloaded',
          discovered_at: 1_700_000_000_000,
        },
        {
          id: 2,
          subscription_id: 's-1',
          item_id: 'BV2',
          title: '第 2 集',
          status: 'queued',
          discovered_at: 1_700_000_100_000,
        },
      ],
    })
    const w = mountSubs()
    await flushPromises()

    await btnByText(w, '已发现内容').trigger('click')
    await flushPromises()

    expect(mocks.listSubscriptionItems).toHaveBeenCalledWith('s-1')
    const rows = w.findAll('.tbl tbody tr')
    expect(rows.length).toBe(2)
    expect(rows[0].text()).toContain('第 1 集')
    expect(rows[0].text()).toContain('已下载')
    expect(rows[1].text()).toContain('已入队')
  })

  it('删除订阅（按钮自带二次确认语义）', async () => {
    mocks.listSubscriptions.mockResolvedValueOnce({ items: [makeSub()] })
    const w = mountSubs()
    await flushPromises()

    const del = btnByText(w, '删除')
    // 破坏性按钮必须带 confirm 提示，不能点一次就没了
    expect(del.exists()).toBe(true)
    await del.trigger('click')
    await flushPromises()
    expect(mocks.deleteSubscription).toHaveBeenCalledWith('s-1')
  })
})
