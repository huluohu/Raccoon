import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import RButton from './RButton.vue'

describe('RButton', () => {
  it('默认渲染 <button>，不渲染 <a>', () => {
    const w = mount(RButton, { slots: { default: '点我' } })
    expect(w.find('button').exists()).toBe(true)
    expect(w.find('a').exists()).toBe(false)
    expect(w.text()).toBe('点我')
  })

  it('传 href 时渲染成 <a> 并带 href（下载/外链场景的链接语义）', () => {
    const w = mount(RButton, { props: { href: '/api/files/f1/download' }, slots: { default: '下载' } })
    expect(w.find('a').exists()).toBe(true)
    expect(w.find('a').attributes('href')).toBe('/api/files/f1/download')
  })

  it('download 属性：传 true 时 <a> 带 download', () => {
    const w = mount(RButton, { props: { href: '/x', download: true } })
    expect(w.find('a').attributes('download')).toBeDefined()
  })

  it('不传 download 时 <a> 不带 download 属性', () => {
    const w = mount(RButton, { props: { href: '/x' } })
    expect(w.find('a').attributes('download')).toBeUndefined()
  })

  it('<a> 禁用时：去掉 href、加 aria-disabled、并用 CSS pointer-events 阻断点击', () => {
    const w = mount(RButton, { props: { href: '/x', disabled: true } })
    expect(w.find('a').attributes('href')).toBeUndefined()
    expect(w.find('a').attributes('aria-disabled')).toBe('true')
    expect(w.find('a').classes()).toContain('rbtn--disabled')
  })

  it('<button> 禁用时是原生 disabled 属性', () => {
    const w = mount(RButton, { props: { disabled: true } })
    expect(w.find('button').attributes('disabled')).toBeDefined()
  })

  it('三级样式：accent 实色 / outline 描边 / ghost 文字', () => {
    expect(mount(RButton, { props: { variant: 'accent' } }).find('button').classes()).toContain('rbtn--accent')
    expect(mount(RButton, { props: { variant: 'outline' } }).find('button').classes()).toContain('rbtn--outline')
    expect(mount(RButton, { props: { variant: 'ghost' } }).find('button').classes()).toContain('rbtn--ghost')
  })

  it('danger 色调：accent 与 outline 各自一套', () => {
    expect(
      mount(RButton, { props: { variant: 'accent', tone: 'danger' } }).find('button').classes(),
    ).toContain('rbtn--danger')
    expect(
      mount(RButton, { props: { variant: 'outline', tone: 'danger' } }).find('button').classes(),
    ).toContain('rbtn--danger')
  })

  it('block 属性加块级类', () => {
    expect(mount(RButton, { props: { block: true } }).find('button').classes()).toContain('rbtn--block')
  })

  it('点击触发 click 事件', async () => {
    const w = mount(RButton)
    await w.find('button').trigger('click')
    expect(w.emitted('click')).toBeTruthy()
  })

  it('禁用时不触发 click', async () => {
    const w = mount(RButton, { props: { disabled: true } })
    await w.find('button').trigger('click')
    expect(w.emitted('click')).toBeFalsy()
  })

  it('confirm 二次确认：第一次点击只进入确认态，第二次才真正触发', async () => {
    const w = mount(RButton, { props: { confirm: '确定删除？' } })
    await w.find('button').trigger('click')
    expect(w.emitted('click')).toBeFalsy()
    await w.find('button').trigger('click')
    expect(w.emitted('click')).toBeTruthy()
  })

  it('aria-label 等 fallthrough 属性落到根元素上', () => {
    const w = mount(RButton, { attrs: { 'aria-label': '下载文件' } })
    expect(w.find('button').attributes('aria-label')).toBe('下载文件')
  })
})
