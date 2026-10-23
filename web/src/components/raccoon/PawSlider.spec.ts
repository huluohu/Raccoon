import { describe, expect, it } from 'vitest'

import PawSlider from '@/components/raccoon/PawSlider.vue'
import { mount } from '@/test/mount'

/**
 * PawSlider 用例（docs/07 §4「进度条端点爪印滑块（2px 圆点 + 三趾），极简冲突可退圆点」）。
 */
describe('PawSlider', () => {
  it('默认渲染轨道 + 爪印，爪印位置按百分比定位', () => {
    const w = mount(PawSlider, { props: { percent: 40 } })
    expect(w.find('.paw-track').exists()).toBe(true)
    expect(w.find('.paw-print').exists()).toBe(true)
    expect(w.find('.paw-thumb').attributes('style')).toContain('left: 40%')
    expect(w.find('.paw-fill').attributes('style')).toContain('width: 40%')
  })

  it('百分比夹在 0–100：脏数据不能把爪印画到轨道外', () => {
    const over = mount(PawSlider, { props: { percent: 180 } })
    expect(over.find('.paw-thumb').attributes('style')).toContain('left: 100%')
    const nan = mount(PawSlider, { props: { percent: NaN } })
    expect(nan.find('.paw-thumb').attributes('style')).toContain('left: 0%')
  })

  it('极简 / 减少动效时爪印退化为圆点（方案明写「极简冲突可退圆点」）', () => {
    const w = mount(PawSlider, { props: { percent: 50, minimal: true } })
    expect(w.find('.paw-dot').exists()).toBe(true)
    expect(w.find('.paw-print').exists()).toBe(false)
  })

  it('thumbOnly：只出爪印，不渲染轨道与填充（供 TaskProgress 叠加）', () => {
    const w = mount(PawSlider, { props: { percent: 30, thumbOnly: true } })
    expect(w.find('.paw-track').exists()).toBe(false)
    expect(w.find('.paw-fill').exists()).toBe(false)
    expect(w.find('.paw-thumb').exists()).toBe(true)
    // thumbOnly 时不再重复声明 progressbar 角色（由宿主负责，避免读屏念两遍）
    expect(w.find('[role="progressbar"]').exists()).toBe(false)
  })

  it('完整模式带 role=progressbar 与 aria 值', () => {
    const w = mount(PawSlider, { props: { percent: 66.6, label: '任务进度' } })
    const bar = w.find('[role="progressbar"]')
    expect(bar.exists()).toBe(true)
    expect(bar.attributes('aria-valuenow')).toBe('67')
    expect(bar.attributes('aria-label')).toBe('任务进度')
  })

  it('填充色与 tone 语义对齐（parse=info / post=success / 默认=gold）', () => {
    expect(mount(PawSlider, { props: { percent: 10, tone: 'info' } }).find('.paw-fill--info').exists()).toBe(true)
    expect(mount(PawSlider, { props: { percent: 10, tone: 'success' } }).find('.paw-fill--success').exists()).toBe(true)
    expect(mount(PawSlider, { props: { percent: 10 } }).find('.paw-fill--gold').exists()).toBe(true)
  })
})
