import { describe, expect, it } from 'vitest'

import DonutChart from '@/components/charts/DonutChart.vue'
import { mount } from '@/test/mount'

/**
 * DonutChart 用例（docs/14 §3.4）：dashoffset 比例、null 降级、图例、语义色。
 */
describe('DonutChart', () => {
  it('50% → dashoffset 为周长的一半', () => {
    const w = mount(DonutChart, { props: { percent: 50 } })
    const ring = w.find('.donut-ring')
    const offset = Number(ring.attributes('stroke-dashoffset'))
    const circ = 2 * Math.PI * 16
    expect(offset).toBeCloseTo(circ / 2, 5)
  })

  it('100% → 无偏移（整圈）', () => {
    const w = mount(DonutChart, { props: { percent: 100 } })
    expect(Number(w.find('.donut-ring').attributes('stroke-dashoffset'))).toBeCloseTo(0, 5)
  })

  it('percent=null 时不画数据环，中心显示「—」（不假装 0%）', () => {
    const w = mount(DonutChart, { props: { percent: null } })
    expect(w.find('.donut-ring').exists()).toBe(false)
    expect(w.find('.donut-value').text()).toBe('—')
  })

  it('中心文案默认取整百分比，也可由调用方覆盖', () => {
    const w = mount(DonutChart, { props: { percent: 66.7 } })
    expect(w.find('.donut-value').text()).toBe('67%')

    const custom = mount(DonutChart, { props: { percent: 66.7, centerValue: '2.3 GB' } })
    expect(custom.find('.donut-value').text()).toBe('2.3 GB')
  })

  it('环色与图例色点用同一语义色变量（不写死色值）', () => {
    const w = mount(DonutChart, {
      props: { percent: 30, tone: 'warning', legend: [{ label: '已用', value: '3 GB' }] },
    })
    expect(w.find('.donut-ring').attributes('stroke')).toBe('var(--warning)')
    expect(w.find('.donut-dot').attributes('style')).toContain('var(--warning)')
    expect(w.find('.donut-legend-value').text()).toBe('3 GB')
  })
})
