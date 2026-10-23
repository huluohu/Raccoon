import { describe, expect, it } from 'vitest'

import TrendChart from '@/components/charts/TrendChart.vue'
import { mount } from '@/test/mount'

/**
 * TrendChart 用例（docs/14 §3.4）：纯 SVG、点序正确、空态不画假线。
 *
 * 断言 polyline 的 `points` 属性——SVG 在 jsdom 里是真 DOM，
 * 这正是选 SVG 而不是 canvas 的理由（可测）。
 */
const pts = [
  { date: '2026-10-17', value: 0 },
  { date: '2026-10-18', value: 4 },
  { date: '2026-10-19', value: 2 },
]

describe('TrendChart', () => {
  it('按点序画出折线（x 递增，值越大 y 越小）', () => {
    const w = mount(TrendChart, { props: { points: pts } })
    const points = w.find('polyline').attributes('points') ?? ''
    const coords = points
      .split(' ')
      .filter(Boolean)
      .map((p) => p.split(',').map(Number))
    expect(coords).toHaveLength(3)
    // x 单调递增
    expect(coords[0][0]).toBeLessThan(coords[1][0])
    expect(coords[1][0]).toBeLessThan(coords[2][0])
    // 峰值（4）比 0 值更靠上（y 更小）
    expect(coords[1][1]).toBeLessThan(coords[0][1])
  })

  it('末点有数据圆点（最新一天）', () => {
    const w = mount(TrendChart, { props: { points: pts } })
    const dot = w.find('.tchart-dot')
    expect(dot.exists()).toBe(true)
    expect(Number(dot.attributes('cx'))).toBeGreaterThan(0)
  })

  it('面积路径闭合回基线（不是一条悬空的折线）', () => {
    const w = mount(TrendChart, { props: { points: pts } })
    const d = w.find('.tchart-area').attributes('d') ?? ''
    expect(d.startsWith('M ')).toBe(true)
    expect(d.trim().endsWith('Z')).toBe(true)
  })

  it('空数据只显示「暂无数据」，不画折线', () => {
    const w = mount(TrendChart, { props: { points: [] } })
    expect(w.find('polyline').exists()).toBe(false)
    expect(w.find('.tchart-empty').text()).toContain('暂无数据')
  })

  it('x 轴刻度取首/中/末（30 点不挤成黑条）', () => {
    const many = Array.from({ length: 30 }, (_, i) => ({
      date: `2026-09-${String(i + 1).padStart(2, '0')}`,
      value: i,
    }))
    const w = mount(TrendChart, { props: { points: many } })
    const ticks = w.findAll('.tchart-tick')
    expect(ticks).toHaveLength(3)
    expect(ticks[0].text()).toBe('09-01')
    expect(ticks[2].text()).toBe('09-30')
  })

  it('峰值/最新一句人话，格式化由调用方给', () => {
    const w = mount(TrendChart, { props: { points: pts, format: (v: number) => `${v} 个` } })
    const sum = w.find('.tchart-sum').text()
    expect(sum).toContain('4 个')
    expect(sum).toContain('2 个')
  })
})
