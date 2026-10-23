/**
 * RaccoonLogo 用例（docs/07 §5 品牌识别：眼罩 / 围巾 / 爪印）。
 *
 * 关键验收点：
 * 1) 品牌徽标与状态表情分工：Logo 是静态对称的识别符号（mark/full 两变体），
 *    不是会换五官的 RaccoonFace——空态表情在 RaccoonFace.spec.ts 覆盖。
 * 2) 识别三件套必须都在：眼罩（两条深色斜带）、围巾金、金色内耳。
 * 3) 徽标配色走 --logo-* 变量，不写死色值——侧导航深底要提亮毛色，
 *    登录页浅底用默认值，使用方按底色覆盖（§8「禁在模板写死色值」）。
 */
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import RaccoonLogo from './RaccoonLogo.vue'

describe('RaccoonLogo 组件', () => {
  it('full 变体：徽标 + 字标（侧导航 / 登录页用）', () => {
    const w = mount(RaccoonLogo, { props: { variant: 'full', tagline: '自托管多媒体下载中心' } })
    expect(w.find('.rlogo-mark').exists()).toBe(true)
    expect(w.find('.rlogo-name').text()).toBe('浣熊')
    expect(w.find('.rlogo-en').text()).toBe('Raccoon')
    expect(w.find('.rlogo-tagline').text()).toBe('自托管多媒体下载中心')
  })

  it('mark 变体：只有图形，不渲染字标（favicon / 窄屏用）', () => {
    const w = mount(RaccoonLogo, { props: { variant: 'mark' } })
    expect(w.find('.rlogo-mark').exists()).toBe(true)
    expect(w.find('.rlogo-word').exists()).toBe(false)
  })

  it('识别三件套：眼罩斜带 + 围巾金 + 金色内耳都在图形里', () => {
    const w = mount(RaccoonLogo)
    const html = w.html()
    // 眼罩：两条对称斜带（path d 以 M11 27 / M53 27 起笔）
    expect(html).toContain('M11 27')
    expect(html).toContain('M53 27')
    // 围巾：金色弧带 + 飘带（gold / gold-strong 变量）
    expect(html).toContain('var(--gold)')
    expect(html).toContain('var(--gold-strong)')
    // 内耳金：两个小圆
    expect((html.match(/r="4.2"/g) ?? []).length).toBe(2)
  })

  it('配色走 --logo-* 变量，不写死色值（深底使用方可提亮毛色）', () => {
    const w = mount(RaccoonLogo)
    const html = w.html()
    expect(html).toContain('var(--logo-fur)')
    expect(html).toContain('var(--logo-face)')
    expect(html).toContain('var(--logo-mask)')
    // 组件本体不得出现十六进制硬编码色值
    expect(w.html()).not.toMatch(/#[0-9A-Fa-f]{3,6}/)
  })

  it('size 控制徽标尺寸，full 字标随尺寸等比缩放', () => {
    const small = mount(RaccoonLogo, { props: { size: 24, variant: 'full' } })
    const big = mount(RaccoonLogo, { props: { size: 56, variant: 'full' } })
    expect(small.find('svg').attributes('width')).toBe('24')
    expect(big.find('svg').attributes('width')).toBe('56')
    // 字标主字号 = round(size * 0.52)：24 → 12，56 → 29
    expect(small.find('.rlogo-name').attributes('style')).toContain('font-size: 12px')
    expect(big.find('.rlogo-name').attributes('style')).toContain('font-size: 29px')
  })

  it('品牌图形是 SVG 不是 emoji（§4 禁 emoji 冒充图标）', () => {
    const w = mount(RaccoonLogo)
    expect(w.find('svg').exists()).toBe(true)
    expect(w.text()).not.toContain('🦝')
    expect(w.find('svg').attributes('aria-label')).toBe('浣熊 Raccoon')
  })

  it('tagline 可省略（不留空元素占位）', () => {
    const w = mount(RaccoonLogo, { props: { variant: 'full' } })
    expect(w.find('.rlogo-tagline').exists()).toBe(false)
  })
})
