import { describe, expect, it } from 'vitest'

import MetricCard from '@/components/base/MetricCard.vue'
import { mount } from '@/test/mount'

/**
 * MetricCard 用例（docs/14 §3.3）：结构四件套 / tone 语义色 / 可点击跳转 / 加载占位。
 *
 * 断言落在可观测结果上：大数字文案、tabular-nums 类、语义色变量名、
 * 点击是否 emit 目标路由——不测内部 computed。
 */
describe('MetricCard', () => {
  it('渲染图标 / 大数字 / 标签 / 副行四件套', () => {
    const w = mount(MetricCard, {
      props: { label: '今日下载', value: '12', hint: '较昨日 +3', icon: 'M2 8h12' },
    })
    expect(w.find('.mcard-value').text()).toBe('12')
    expect(w.find('.mcard-label').text()).toBe('今日下载')
    expect(w.find('.mcard-hint').text()).toBe('较昨日 +3')
    expect(w.find('svg path').attributes('d')).toBe('M2 8h12')
  })

  it('数字用 display 档 + tabular-nums（docs/07 §2 铁律）', () => {
    const w = mount(MetricCard, { props: { label: '总量', value: '1,024' } })
    const value = w.find('.mcard-value')
    expect(value.classes()).toContain('tnum')
  })

  it('tone 决定语义色（不写死色值）', () => {
    const w = mount(MetricCard, { props: { label: '成功率', value: '98%', tone: 'warning' } })
    expect(w.find('.mcard-value').attributes('style')).toContain('var(--warning)')
    expect(w.find('.mcard-icon').attributes('style')).toContain('var(--warning)')
    expect(w.find('.mcard-icon').attributes('style')).toContain('color-mix')
  })

  it('给了 to 就变成按钮并 emit open（跳转由父层负责）', async () => {
    const w = mount(MetricCard, { props: { label: 'CPU', value: '12%', to: '/monitor' } })
    expect(w.element.tagName).toBe('BUTTON')
    await w.trigger('click')
    expect(w.emitted('open')).toEqual([['/monitor']])
  })

  it('没给 to 是静态卡片，点击不 emit', async () => {
    const w = mount(MetricCard, { props: { label: '版本', value: '0.1.0' } })
    expect(w.element.tagName).toBe('ARTICLE')
    await w.trigger('click')
    expect(w.emitted('open')).toBeFalsy()
  })

  it('加载中显示占位条而不是 0（0 是数据，占位不是）', () => {
    const w = mount(MetricCard, { props: { label: '今日下载', value: '0', loading: true } })
    expect(w.find('.mcard-skel').exists()).toBe(true)
    expect(w.find('.mcard-value').text()).not.toContain('0')
  })
})
