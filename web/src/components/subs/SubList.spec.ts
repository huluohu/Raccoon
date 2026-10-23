import { describe, expect, it } from 'vitest'

import SubList from '@/components/subs/SubList.vue'
import { RButtonStub, PlatformBadgeStub } from '@/test/stubs'
import type { Subscription } from '@/api/types'
import { mount } from '@/test/mount'

/**
 * SubList 用例（docs/07 §3「/subscriptions → SubList」）。
 *
 * 关键验收点：
 * 1) 卡片信息结构固定：来源类型/模式徽章 → 标题 → URL → 四个计数 → 四项元信息。
 * 2) 计数不该恒显：失败/已跳过为 0 时不占位（0 也是事实，但没信息量）。
 * 3) 「下次检查」必须是人话（已暂停/待调度/即将检查/N 分钟后），不是时间戳。
 * 4) 四个动作各自抛独立事件，父组件据此决定弹什么。
 * 5) 正在检查的那张卡要置灰 + 改文案，避免重复触发。
 * 6) 停用的订阅要能一眼看出（降透明度）。
 */
const stubs = {
  RButton: RButtonStub,
  PlatformBadge: PlatformBadgeStub,
}

function makeSub(over: Partial<Subscription> = {}): Subscription {
  return {
    id: 's-1',
    platform: 'bilibili',
    source_type: 'favorite',
    source_id: 'fid-1',
    source_url: 'https://space.bilibili.com/123/favlist?fid=456',
    title: '我的收藏夹',
    mode: 'new_only',
    interval_min: 30,
    enabled: true,
    next_check_at: 0,
    created_at: 1_700_000_000_000,
    updated_at: 1_700_000_000_000,
    item_counts: { total: 5, downloaded: 2, queued: 1, skipped: 1, failed: 1 },
    ...over,
  }
}

describe('SubList 组件', () => {
  it('渲染来源类型/模式徽章、标题与 URL', () => {
    const w = mount(SubList, { props: { items: [makeSub()] }, global: { stubs } })
    expect(w.find('.src-type').text()).toBe('收藏夹')
    expect(w.find('.mode').text()).toBe('只要新增')
    expect(w.find('.card-title').text()).toBe('我的收藏夹')
    expect(w.find('.card-url').text()).toBe('https://space.bilibili.com/123/favlist?fid=456')
    expect(w.find('.stub-badge').attributes('data-platform')).toBe('bilibili')
  })

  it('已下载/待下载恒显，失败/已跳过为 0 时不占位', () => {
    const w = mount(SubList, {
      props: {
        items: [
          makeSub({
            item_counts: { total: 2, downloaded: 2, queued: 0, skipped: 0, failed: 0 },
          }),
        ],
      },
      global: { stubs },
    })
    expect(w.find('.cnt.done').text()).toBe('2 已下载')
    expect(w.find('.cnt.queued').text()).toBe('0 待下载')
    expect(w.find('.cnt.failed').exists()).toBe(false)
    expect(w.find('.cnt.skip').exists()).toBe(false)
  })

  it('失败/已跳过大于 0 时才显示对应计数', () => {
    const w = mount(SubList, { props: { items: [makeSub()] }, global: { stubs } })
    expect(w.find('.cnt.failed').text()).toBe('1 失败')
    expect(w.find('.cnt.skip').text()).toBe('1 已跳过')
  })

  it('「下次检查」是人话：已暂停 / 待调度 / 即将检查 / N 分钟后检查', () => {
    const cases: Array<[Partial<Subscription>, string]> = [
      [{ enabled: false }, '已暂停'],
      [{ next_check_at: 0 }, '待调度'],
      [{ next_check_at: Date.now() - 1000 }, '即将检查'],
      [{ next_check_at: Date.now() + 10 * 60_000 }, '10 分钟后检查'],
    ]
    for (const [over, expectText] of cases) {
      const w = mount(SubList, { props: { items: [makeSub(over)] }, global: { stubs } })
      const dds = w.findAll('.meta dd')
      // 元信息顺序：上次检查 / 下次 / 画质 / 周期
      expect(dds[1].text()).toBe(expectText)
    }
  })

  it('画质留空时显示「跟随全局」，不显示空白', () => {
    const w = mount(SubList, { props: { items: [makeSub({ quality: undefined })] }, global: { stubs } })
    expect(w.text()).toContain('跟随全局')
  })

  it('上次检查时间为空时显示「从未」', () => {
    const w = mount(SubList, { props: { items: [makeSub({ last_checked_at: undefined })] }, global: { stubs } })
    const dds = w.findAll('.meta dd')
    expect(dds[0].text()).toBe('从未')
  })

  it('有错误说明时显示提示文案与角标', () => {
    const w = mount(SubList, {
      props: { items: [makeSub({ error_note: 'Cookie 已失效' })] },
      global: { stubs },
    })
    expect(w.find('.err-note').text()).toBe('Cookie 已失效')
    expect(w.find('.warn-mark').attributes('title')).toBe('Cookie 已失效')
  })

  it('四个动作各自抛独立事件（带订阅对象）', async () => {
    const sub = makeSub()
    const w = mount(SubList, { props: { items: [sub] }, global: { stubs } })
    const btns = w.findAll('button')
    const find = (label: string) => btns.find((b) => b.text().includes(label))!

    await find('立即检查').trigger('click')
    expect(w.emitted('check')![0]).toEqual([sub])

    await find('已发现内容').trigger('click')
    expect(w.emitted('detail')![0]).toEqual([sub])

    await find('暂停').trigger('click')
    expect(w.emitted('toggle')![0]).toEqual([sub])

    await find('删除').trigger('click')
    expect(w.emitted('remove')![0]).toEqual([sub])
  })

  it('停用的订阅显示「启用」而不是「暂停」', () => {
    const w = mount(SubList, { props: { items: [makeSub({ enabled: false })] }, global: { stubs } })
    const labels = w.findAll('button').map((b) => b.text())
    expect(labels.some((l) => l.includes('启用'))).toBe(true)
    expect(labels.some((l) => l.includes('暂停'))).toBe(false)
  })

  it('正在检查的订阅：按钮置灰 + 文案改成「检查中…」', () => {
    const w = mount(SubList, {
      props: { items: [makeSub({ id: 's-1' })], checkingId: 's-1' },
      global: { stubs },
    })
    const btn = w.findAll('button').find((b) => b.text().includes('检查中'))!
    expect(btn.exists()).toBe(true)
    expect(btn.attributes('disabled')).toBeDefined()
  })

  it('停用的卡片降透明度（一眼看出哪些在跑）', () => {
    const w = mount(SubList, { props: { items: [makeSub({ enabled: false })] }, global: { stubs } })
    expect(w.find('.card').classes()).toContain('off')
  })
})

/**
 * 真实 RButton 下的破坏性动作。
 *
 * 上面那组用的是 RButtonStub（把 confirm 直接跳过），只能证明「事件连线对」；
 * 线上「删除点了没反应」恰恰死在真按钮的二次确认态不可见上，stub 测不出来。
 * 这组不 stub RButton，断言必须落在用户看得见的东西上。
 */
describe('SubList 破坏性动作（真实 RButton）', () => {
  const realStubs = { PlatformBadge: PlatformBadgeStub }

  const findBtn = (w: ReturnType<typeof mount>, ...labels: string[]) =>
    w.findAll('button').find((b) => labels.some((l) => b.text().includes(l)))!

  it('删除要点两次：第一次变成「确认删除？」且不抛事件，第二次才抛', async () => {
    const sub = makeSub()
    const w = mount(SubList, { props: { items: [sub] }, global: { stubs: realStubs } })

    await findBtn(w, '删除').trigger('click')
    expect(w.emitted('remove')).toBeFalsy()
    expect(w.text()).toContain('确认删除？')
    expect(findBtn(w, '确认删除？').classes()).toContain('rbtn--confirming')

    await findBtn(w, '确认删除？').trigger('click')
    expect(w.emitted('remove')![0]).toEqual([sub])
  })

  it('暂停只需一次点击（非破坏性动作不该有二次确认）', async () => {
    const sub = makeSub()
    const w = mount(SubList, { props: { items: [sub] }, global: { stubs: realStubs } })
    await findBtn(w, '暂停').trigger('click')
    expect(w.emitted('toggle')![0]).toEqual([sub])
  })
})
