/**
 * RaccoonLogo 用例（docs/07 §5 品牌识别：眼罩 / 围巾 / 爪印）。
 *
 * 关键验收点：
 * 1) 品牌徽标与状态表情分工：Logo 是静态对称的识别符号（mark/full 两变体），
 *    不是会换五官的 RaccoonFace——空态表情在 RaccoonFace.spec.ts 覆盖。
 * 2) 识别三件套必须都在：连体眼罩、环纹尾、主题围巾与内耳。
 * 3) 徽标配色走 --logo-* 变量，不写死色值——侧导航深底要提亮毛色，
 *    登录页浅底用默认值，使用方按底色覆盖（§8「禁在模板写死色值」）。
 */
import { describe, expect, it } from 'vitest'

import RaccoonLogo from './RaccoonLogo.vue'
import { mount } from '@/test/mount'

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

  it('识别三件套：连体眼罩 + 环尾 + 换皮围巾/内耳都在图形里', () => {
    const w = mount(RaccoonLogo)
    const html = w.html()
    // 眼罩：整块连体（单一路径，M 10.5 28.5 起笔）——分开的两条斜带是哈士奇纹路
    expect(html).toContain('M 10.5 28.5')
    // 环尾：粗描边尾身 + 同路径虚线环纹（浣熊第二识别物）
    expect((html.match(/stroke-dasharray/g) ?? []).length).toBe(1)
    expect((html.match(/stroke="var\(--logo-fur\)"/g) ?? []).length).toBe(1)
    // 围巾：换皮变量（docs/07 §2 四主题：night=薄荷 / scarf=金 / snow=墨青 / berry=珊瑚）
    expect(html).toContain('var(--logo-scarf)')
    expect(html).toContain('var(--logo-scarf-strong)')
    // 内耳：主题耳色，左右两个三角
    expect((html.match(/var\(--logo-ear\)/g) ?? []).length).toBe(2)
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
