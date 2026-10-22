import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ItemsTable from '@/components/subs/ItemsTable.vue'
import { RaccoonFaceStub } from '@/test/stubs'
import type { SubscriptionItem } from '@/api/types'

/**
 * ItemsTable 用例（docs/07 §3「/subscriptions → ItemsTable」）。
 *
 * 关键验收点：
 * 1) 表头/表体字号必须分明（表头 caption 12px、表体 body 16px）——
 *    这条规则和表结构绑在一起，靠 CSS 实现；这里断言的是「表结构本身分得清」：
 *    有 thead/th、有 tbody/td，表头固定三列。
 * 2) 状态文字 + 颜色成对出现（§4：禁颜色唯一载体），色弱用户读文字即可。
 * 3) 追更到货横幅固定用 surprise 表情（§5 表情语义表：彩蛋、追更到货）。
 * 4) 待处理数 = pending + queued（不是全部条目数）。
 * 5) 标题缺失时回落到 item_id，不渲染空白单元格。
 */
const stubs = { RaccoonFace: RaccoonFaceStub }

function makeItem(over: Partial<SubscriptionItem> = {}): SubscriptionItem {
  return {
    id: 1,
    subscription_id: 's-1',
    item_id: 'BV1xx411c7mD',
    title: '测试视频',
    status: 'pending',
    discovered_at: Date.now() - 60_000,
    ...over,
  }
}

describe('ItemsTable 组件', () => {
  it('追更到货横幅固定用 surprise 表情（§5）', () => {
    const w = mount(ItemsTable, { props: { items: [makeItem()] }, global: { stubs } })
    expect(w.find('.stub-face').attributes('data-face')).toBe('surprise')
    expect(w.find('.found-title').text()).toBe('追更到了！')
  })

  it('横幅给出数字事实：发现总数 + 待处理数', () => {
    const w = mount(ItemsTable, {
      props: {
        items: [
          makeItem({ id: 1, status: 'pending' }),
          makeItem({ id: 2, status: 'queued' }),
          makeItem({ id: 3, status: 'downloaded' }),
        ],
      },
      global: { stubs },
    })
    expect(w.find('.found-hint').text()).toContain('本次发现 3 条')
    expect(w.find('.found-hint').text()).toContain('待处理 2 条')
  })

  it('表结构分明：thead 三列 + tbody 行', () => {
    const w = mount(ItemsTable, { props: { items: [makeItem()] }, global: { stubs } })
    const headers = w.findAll('thead th').map((th) => th.text())
    expect(headers).toEqual(['标题', '状态', '发现时间'])
    expect(w.findAll('tbody tr').length).toBe(1)
  })

  it('状态文字与颜色成对出现（不靠颜色单独承载信息）', () => {
    const w = mount(ItemsTable, {
      props: {
        items: [
          makeItem({ id: 1, status: 'downloaded' }),
          makeItem({ id: 2, status: 'failed' }),
          makeItem({ id: 3, status: 'skipped' }),
        ],
      },
      global: { stubs },
    })
    const cells = w.findAll('.st')
    expect(cells[0].text()).toBe('已下载')
    expect(cells[0].attributes('style')).toContain('--success')
    expect(cells[1].text()).toBe('失败')
    expect(cells[1].attributes('style')).toContain('--danger')
    expect(cells[2].text()).toBe('已跳过')
  })

  it('未收录状态回落到原值（不显示空白）', () => {
    const w = mount(ItemsTable, {
      props: { items: [makeItem({ status: 'weird' as SubscriptionItem['status'] })] },
      global: { stubs },
    })
    expect(w.find('.st').text()).toBe('weird')
  })

  it('标题缺失时回落到 item_id', () => {
    const w = mount(ItemsTable, {
      props: { items: [makeItem({ title: '' })] },
      global: { stubs },
    })
    expect(w.find('.ttl').text()).toBe('BV1xx411c7mD')
  })
})
