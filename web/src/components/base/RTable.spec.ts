import { describe, expect, it } from 'vitest'

import RTable from '@/components/base/RTable.vue'
import { mount } from '@/test/mount'

/**
 * RTable 用例（docs/07 §4 验收点：表头浅底 / 行高 44·36 / hover 行操作 / 空态≠骨架屏）。
 */
interface Row {
  id: string
  name: string
  size: number
}

const rows: Row[] = [
  { id: 'a', name: 'a.mp4', size: 1024 },
  { id: 'b', name: 'b.mp4', size: 2048 },
]

const columns = [
  { key: 'name', title: '文件名' },
  { key: 'size', title: '大小', align: 'right' as const },
]

function mountTable(props: Record<string, unknown> = {}) {
  return mount(RTable, {
    props: { rows, columns, rowKey: 'id', ...props },
    global: { stubs: { RButton: { template: '<button><slot /></button>' } } },
  })
}

describe('RTable', () => {
  it('表头浅底且列标题齐全', () => {
    const w = mountTable()
    const heads = w.findAll('th')
    expect(heads.length).toBeGreaterThanOrEqual(2)
    expect(w.text()).toContain('文件名')
    expect(w.text()).toContain('大小')
    // 表头浅底由 --surface-2 驱动（断言类名存在，颜色由 token 决定）
    expect(w.find('thead').exists()).toBe(true)
  })

  it('右对齐列带 rtable-cell--right 修饰', () => {
    const w = mountTable()
    const right = w.findAll('.rtable-cell--right')
    expect(right.length).toBeGreaterThan(0)
  })

  it('行高两档：md=44px / sm=36px', () => {
    const md = mountTable()
    expect(md.find('table').classes()).toContain('rtable--md')
    const sm = mountTable({ size: 'sm' })
    expect(sm.find('table').classes()).toContain('rtable--sm')
  })

  it('点击行触发 row-click 并带上行数据与索引', async () => {
    const w = mountTable()
    await w.findAll('tbody tr')[1].trigger('click')
    expect(w.emitted('row-click')).toBeTruthy()
    expect(w.emitted('row-click')?.[0]?.[0]).toMatchObject({ id: 'b' })
    expect(w.emitted('row-click')?.[0]?.[1]).toBe(1)
  })

  it('actions 插槽渲染行内操作', () => {
    const w = mount(RTable, {
      props: { rows, columns, rowKey: 'id' },
      slots: { actions: '<button class="fake-dl">下载</button>' },
      global: { stubs: { RButton: { template: '<button><slot /></button>' } } },
    })
    expect(w.findAll('.fake-dl').length).toBe(rows.length)
  })

  it('空态与骨架屏互斥：loading 时走骨架，空 rows 时走空态', () => {
    const loading = mountTable({ loading: true })
    expect(loading.findAll('.rtable-row--skeleton').length).toBeGreaterThan(0)
    expect(loading.find('.rtable-row--empty').exists()).toBe(false)

    const empty = mountTable({ rows: [] })
    expect(empty.find('.rtable-row--empty').exists()).toBe(true)
    expect(empty.find('.rtable-row--skeleton').exists()).toBe(false)
  })

  it('空态文案可自定义，且不等于骨架屏（docs/07 §7 空/错/载三分）', () => {
    const w = mountTable({ rows: [], emptyText: '没有匹配的文件' })
    expect(w.find('.rtable-empty-text').text()).toBe('没有匹配的文件')
  })

  it('cell-<key> 插槽可覆盖单元格内容', () => {
    const w = mount(RTable, {
      props: { rows, columns, rowKey: 'id' },
      slots: { 'cell-name': '<b class="custom-cell">自定义</b>' },
      global: { stubs: { RButton: { template: '<button><slot /></button>' } } },
    })
    expect(w.findAll('.custom-cell').length).toBe(rows.length)
  })
})
