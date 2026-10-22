import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import StatusStrip from '@/components/shell/StatusStrip.vue'

/**
 * StatusStrip 用例（docs/07 §3 总览页关键组件）。
 *
 * 关键验收点：
 * 1) 三重编码——色点 + 文字 + 数值（§4 禁颜色唯一载体）：色点 aria-hidden，
 *    真正的信息在 label/value 文字里，色弱用户读文字即可。
 * 2) 空数组不渲染（不留一条空白横杠）。
 * 3) 严重度只决定颜色，不决定是否显示——warn 和 bad 的项都要出现。
 */
describe('StatusStrip 组件', () => {
  it('渲染每条指标：标签 + 数值', () => {
    const w = mount(StatusStrip, {
      props: {
        items: [
          { label: '磁盘', value: '已用 42%', tone: 'ok' },
          { label: '数据库', value: '正常', tone: 'ok' },
        ],
      },
    })
    expect(w.findAll('.sstrip-item').length).toBe(2)
    expect(w.text()).toContain('磁盘')
    expect(w.text()).toContain('已用 42%')
  })

  it('色点是纯装饰（aria-hidden），信息在文字里（§4 禁颜色唯一载体）', () => {
    const w = mount(StatusStrip, {
      props: { items: [{ label: '磁盘', value: '已用 95%', tone: 'bad' }] },
    })
    const dot = w.find('.sstrip-dot')
    expect(dot.attributes('aria-hidden')).toBe('true')
    expect(dot.classes()).toContain('sstrip-dot--bad')
    // 文字里必须有同样的事实，不依赖颜色传达
    expect(w.text()).toContain('已用 95%')
  })

  it('没有数据时整条不渲染（不留空白横杠）', () => {
    const w = mount(StatusStrip, { props: { items: [] } })
    expect(w.find('.sstrip').exists()).toBe(false)
  })

  it('缺少 label 或 value 的条目被过滤掉', () => {
    const w = mount(StatusStrip, {
      props: {
        items: [
          { label: '磁盘', value: '已用 42%' },
          { label: '', value: '孤儿值' },
          { label: '孤儿标签', value: '' },
        ],
      },
    })
    expect(w.findAll('.sstrip-item').length).toBe(1)
  })

  it('hint 作为补充说明跟在值后面', () => {
    const w = mount(StatusStrip, {
      props: { items: [{ label: '凭据', value: '2/3 可用', tone: 'warn', hint: '1 个需处理' }] },
    })
    expect(w.find('.sstrip-hint').text()).toBe('1 个需处理')
  })
})
