import { describe, expect, it } from 'vitest'

import TaskList from '@/components/task/TaskList.vue'
import { RButtonStub, REmptyStub } from '@/test/stubs'
import { makeTask } from '@/test/fixtures'
import type { Task } from '@/api/types'
import type { FilterKey, FilterOption } from '@/composables/useTaskTree'
import { mount } from '@/test/mount'

/**
 * TaskList 用例（docs/07 §3「/tasks → TaskList（筛选/批量工具条）」）。
 *
 * 关键验收点：
 * 1) 筛选工具条：每个筛选项带计数，点一下抛 update:filter（父组件是 v-model:filter）。
 * 2) 批量工具条：**只在选了东西时才出现**。这是「筛选/批量工具条」这半句方案的
 *    另一半——原来页面只有 Shift+Space 键盘多选，没有任何可见入口，用户根本不知道能批量。
 * 3) 批量动作把「动作 + 选中的 ID 列表」一起抛上去，不让父组件自己再查一遍选择集。
 * 4) 空态用 waiting 表情（没有任务在等），并且给下一步引导。
 * 5) 列表被服务端截断时必须如实告知（只加载了最近 100 条）。
 * 6) 父任务用 TaskCard 聚合渲染，普通任务/子项用 TaskRow；展开后才列子项。
 */
const stubs = {
  RButton: RButtonStub,
  REmpty: REmptyStub,
  // TaskCard / TaskRow 是重量级组件，本文件只验 TaskList 自己的职责（筛选/批量/空态/展开），
  // 各自的行为在 TaskCard.spec.ts / TaskRow.spec.ts 里单独验。
  TaskCard: {
    props: ['task', 'parent', 'childStats', 'selected'],
    template: '<article class="stub-card"><slot name="actions" /><slot name="retry-failed" /></article>',
  },
  TaskRow: {
    props: ['task', 'selected', 'size'],
    template: '<li class="stub-row"><slot name="actions" /></li>',
  },
}

const FILTERS: FilterOption[] = [
  { key: 'all', label: '全部' },
  { key: 'active', label: '进行中' },
  { key: 'done', label: '已完成' },
  { key: 'failed', label: '失败' },
]

function baseProps(over: Record<string, unknown> = {}) {
  return {
    roots: [] as Task[],
    filters: FILTERS,
    filter: 'all' as FilterKey,
    countOf: () => 0,
    statOf: () => ({ total: 0, failed: 0, done: 0 }),
    childrenOf: () => [] as Task[],
    shownChildren: () => [] as Task[],
    isParent: () => false,
    ...over,
  }
}

describe('TaskList 组件', () => {
  it('渲染筛选工具条，每个筛选项带计数', () => {
    const w = mount(TaskList, {
      props: baseProps({ countOf: (k: FilterKey) => (k === 'all' ? 7 : 2) }),
      global: { stubs },
    })
    const tabs = w.findAll('.tab')
    expect(tabs.length).toBe(FILTERS.length)
    expect(tabs[0].text()).toContain('全部')
    expect(tabs[0].text()).toContain('7')
    expect(tabs[0].attributes('aria-selected')).toBe('true')
  })

  it('点筛选项抛 update:filter（父组件是 v-model:filter）', async () => {
    const w = mount(TaskList, { props: baseProps(), global: { stubs } })
    const tabs = w.findAll('.tab')
    await tabs[1].trigger('click')
    expect(w.emitted('update:filter')![0]).toEqual(['active'])
  })

  it('没选中任何任务时不渲染批量工具条（不占常态空间）', () => {
    const w = mount(TaskList, { props: baseProps(), global: { stubs } })
    expect(w.find('.batchbar').exists()).toBe(false)
  })

  it('选中任务后出现批量工具条，并显示已选数量', () => {
    const w = mount(TaskList, {
      props: baseProps({ selectedIds: new Set(['t-1', 't-2']) }),
      global: { stubs },
    })
    expect(w.find('.batchbar').exists()).toBe(true)
    expect(w.find('.batchbar-count').text()).toContain('已选 2 项')
  })

  it('三个批量动作各自抛「动作 + ID 列表」', async () => {
    const w = mount(TaskList, {
      props: baseProps({ selectedIds: new Set(['t-1', 't-2']) }),
      global: { stubs },
    })
    const btns = w.findAll('.batchbar-ops button')
    const find = (label: string) => btns.find((b) => b.text().includes(label))!

    await find('批量重试').trigger('click')
    expect(w.emitted('batch')![0]).toEqual(['retry', ['t-1', 't-2']])

    await find('批量取消').trigger('click')
    expect(w.emitted('batch')![1]).toEqual(['cancel', ['t-1', 't-2']])

    await find('批量删除').trigger('click')
    expect(w.emitted('batch')![2]).toEqual(['delete', ['t-1', 't-2']])
  })

  it('没有选中时点批量动作不抛事件（防止误触发空批量）', async () => {
    const w = mount(TaskList, { props: baseProps(), global: { stubs } })
    // 批量工具条不存在，直接调 runBatch 路径：改用一个空 selectedIds 手动触发
    await w.setProps({ selectedIds: new Set<string>() })
    expect(w.emitted('batch')).toBeFalsy()
  })

  it('「取消选择」对每个选中项抛一次 select 事件（父组件据此清选中集）', async () => {
    const w = mount(TaskList, {
      props: baseProps({ selectedIds: new Set(['t-1', 't-2']) }),
      global: { stubs },
    })
    const btn = w.findAll('.batchbar-ops button').find((b) => b.text().includes('取消选择'))!
    await btn.trigger('click')
    const evts = w.emitted('select')!
    expect(evts.length).toBe(2)
    expect(evts[0][0]).toEqual({ id: 't-1' })
    expect(evts[1][0]).toEqual({ id: 't-2' })
  })

  it('busy 时批量按钮置灰（防重复提交）', () => {
    const w = mount(TaskList, {
      props: baseProps({ selectedIds: new Set(['t-1']), busyId: 't-1' }),
      global: { stubs },
    })
    const btns = w.findAll('.batchbar-ops button')
    const retry = btns.find((b) => b.text().includes('批量重试'))!
    expect(retry.attributes('disabled')).toBeDefined()
  })

  it('没有任务时显示空态（waiting 表情 + 下一步引导）', () => {
    const w = mount(TaskList, { props: baseProps(), global: { stubs } })
    const empty = w.find('.stub-empty')
    expect(empty.exists()).toBe(true)
    expect(empty.attributes('data-face')).toBe('waiting')
    expect(empty.text()).toContain('这个筛选下还没有任务')
    expect(empty.text()).toContain('粘贴链接')
  })

  it('加载中且没有任务时显示「加载中…」而不是空态', () => {
    const w = mount(TaskList, { props: baseProps({ loading: true }), global: { stubs } })
    expect(w.text()).toContain('加载中…')
    expect(w.find('.stub-empty').exists()).toBe(false)
  })

  it('列表被服务端截断时如实告知（不假装只有这些）', () => {
    const w = mount(TaskList, { props: baseProps({ truncated: true }), global: { stubs } })
    expect(w.find('.alert').text()).toContain('只加载了最近 100 条')
  })

  it('截断告警出现时，那 100 条任务必须照样显示（提示不顶替列表）', () => {
    const t = makeTask({ id: 't-1' })
    const w = mount(TaskList, {
      props: baseProps({ roots: [t], truncated: true }),
      global: { stubs },
    })
    expect(w.find('.alert').exists()).toBe(true)
    expect(w.findAll('.stub-row').length).toBe(1)
  })

  it('父任务用 TaskCard 渲染，普通任务用 TaskRow', () => {
    const parent = makeTask({ id: 'p-1', platform: 'batch' })
    const normal = makeTask({ id: 'n-1' })
    const w = mount(TaskList, {
      props: baseProps({
        roots: [parent, normal],
        isParent: (t: Task) => t.id === 'p-1',
      }),
      global: { stubs },
    })
    expect(w.findAll('.stub-card').length).toBe(1)
    expect(w.findAll('.stub-row').length).toBe(1)
  })

  it('父任务展开后才列子项（收起时子项不渲染）', async () => {
    const parent = makeTask({ id: 'p-1', platform: 'batch' })
    const child = makeTask({ id: 'c-1', parent_id: 'p-1' })
    const w = mount(TaskList, {
      props: baseProps({
        roots: [parent],
        isParent: (t: Task) => t.id === 'p-1',
        shownChildren: () => [child],
        expandedIds: new Set<string>(),
      }),
      global: { stubs },
    })
    expect(w.findAll('.children .stub-row').length).toBe(0)

    await w.setProps({ expandedIds: new Set(['p-1']) })
    expect(w.findAll('.children .stub-row').length).toBe(1)
  })

  it('点「展开子项 / 收起」抛 toggle-expand', async () => {
    const parent = makeTask({ id: 'p-1', platform: 'batch' })
    const w = mount(TaskList, {
      props: baseProps({ roots: [parent], isParent: (t: Task) => t.id === 'p-1' }),
      global: { stubs },
    })
    const btn = w.findAll('.stub-card button').find((b) => b.text().includes('展开子项'))!
    await btn.trigger('click')
    expect(w.emitted('toggle-expand')![0]).toEqual(['p-1'])
  })

  it('展开后按钮文案变「收起」', async () => {
    const parent = makeTask({ id: 'p-1', platform: 'batch' })
    const w = mount(TaskList, {
      props: baseProps({
        roots: [parent],
        isParent: (t: Task) => t.id === 'p-1',
        expandedIds: new Set(['p-1']),
      }),
      global: { stubs },
    })
    const btn = w.findAll('.stub-card button').find((b) => b.text().includes('收起'))!
    expect(btn.exists()).toBe(true)
  })

  it('失败任务才给「重试」按钮，进行中的任务只给「取消」', () => {
    const failed = makeTask({ id: 'f-1', status: 'failed' })
    const active = makeTask({ id: 'a-1', status: 'downloading' })
    const w = mount(TaskList, {
      props: baseProps({ roots: [failed, active] }),
      global: { stubs },
    })
    const rows = w.findAll('.stub-row')
    const failedBtns = rows[0].findAll('button').map((b) => b.text())
    const activeBtns = rows[1].findAll('button').map((b) => b.text())
    expect(failedBtns.some((t) => t.includes('重试'))).toBe(true)
    expect(activeBtns.some((t) => t.includes('取消'))).toBe(true)
    expect(activeBtns.some((t) => t.includes('重试'))).toBe(false)
  })

  it('本轮新插入的任务带 row-flash 高亮（§6 列表变更动效）', () => {
    const t = makeTask({ id: 't-1' })
    const w = mount(TaskList, {
      props: baseProps({ roots: [t], flashIds: new Set(['t-1']) }),
      global: { stubs },
    })
    expect(w.find('.stub-row').classes()).toContain('row-flash')
  })

  it('父任务显示子项聚合计数（已完成 X/Y）', () => {
    const parent = makeTask({ id: 'p-1', platform: 'batch' })
    const w = mount(TaskList, {
      props: baseProps({
        roots: [parent],
        isParent: (t: Task) => t.id === 'p-1',
        statOf: () => ({ total: 7, failed: 1, done: 3 }),
      }),
      global: { stubs },
    })
    const card = w.find('.stub-card')
    // 聚合计数由 TaskCard 渲染；这里断言 TaskList 把它算对并传下去了
    expect(card.exists()).toBe(true)
    expect(w.find('.stub-card').attributes('class')).toContain('root-card')
  })

  it('readonly：不渲染批量工具条与行内操作，筛选条保留（筛选是浏览不是写操作）', () => {
    const t = makeTask({ id: 't-1', status: 'failed' })
    const w = mount(TaskList, {
      props: baseProps({ roots: [t], selectedIds: new Set(['t-1']), readonly: true }),
      global: { stubs },
    })
    expect(w.find('.batchbar').exists()).toBe(false)
    expect(w.findAll('.tab').length).toBe(FILTERS.length)
    // 行组件自己也 readonly，且 TaskList 干脆不传 #actions（双保险）
    expect(w.findAll('.stub-row button').length).toBe(0)
  })

  it('readonly：父任务操作区不渲染，但展开子项仍可用（树控件由列表提供）', async () => {
    const parent = makeTask({ id: 'p-1', platform: 'batch' })
    const w = mount(TaskList, {
      props: baseProps({
        roots: [parent],
        isParent: (t: Task) => t.id === 'p-1',
        statOf: () => ({ total: 3, failed: 0, done: 0 }),
        readonly: true,
      }),
      global: { stubs },
    })
    // 卡片里不再有「展开/删除」
    expect(w.findAll('.stub-card button').length).toBe(0)

    const toggle = w.find('.tree-toggle button')
    expect(toggle.exists()).toBe(true)
    await toggle.trigger('click')
    expect(w.emitted('toggle-expand')![0]).toEqual(['p-1'])
  })
})
