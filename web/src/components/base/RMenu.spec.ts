import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import RMenu from '@/components/base/RMenu.vue'

/**
 * RMenu 用例（docs/07 §4）。
 *
 * 关键验收点：
 * 1) 默认收起，点击触发器展开——动作区默认不能是一堆平铺按钮（卡片放不下）。
 * 2) 选中菜单项后自动收起（一次操作一个菜单）。
 * 3) 禁用项不可触发。
 * 4) Esc 关闭。
 */
describe('RMenu 组件', () => {
  it('默认收起，点击触发器展开菜单', async () => {
    const w = mount(RMenu, {
      props: { items: [{ label: '提取 MP3 音频', onSelect: vi.fn() }] },
    })
    expect(w.find('.rmenu-pop').exists()).toBe(false)

    await w.find('.rmenu-trigger-badge').trigger('click')
    expect(w.find('.rmenu-pop').exists()).toBe(true)
    expect(w.find('.rmenu-item').text()).toContain('提取 MP3 音频')
  })

  it('点击菜单项触发回调并自动收起', async () => {
    const onSelect = vi.fn()
    const w = mount(RMenu, {
      props: { items: [{ label: '制作封面', onSelect }] },
    })
    await w.find('.rmenu-trigger-badge').trigger('click')
    await w.find('.rmenu-item').trigger('click')
    expect(onSelect).toHaveBeenCalledTimes(1)
    expect(w.find('.rmenu-pop').exists()).toBe(false)
  })

  it('禁用项不可触发', async () => {
    const onSelect = vi.fn()
    const w = mount(RMenu, {
      props: { items: [{ label: 'AI 刮削', onSelect, disabled: true }] },
    })
    await w.find('.rmenu-trigger-badge').trigger('click')
    const item = w.find('.rmenu-item')
    expect(item.attributes('disabled')).toBeDefined()
    await item.trigger('click')
    expect(onSelect).not.toHaveBeenCalled()
  })

  it('Esc 关闭菜单', async () => {
    const w = mount(RMenu, {
      props: { items: [{ label: '编辑元数据', onSelect: vi.fn() }] },
    })
    await w.find('.rmenu-trigger-badge').trigger('click')
    expect(w.find('.rmenu-pop').exists()).toBe(true)

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await w.vm.$nextTick()
    expect(w.find('.rmenu-pop').exists()).toBe(false)
  })

  it('危险项带 danger 样式类', async () => {
    const w = mount(RMenu, {
      props: { items: [{ label: '删除文件', onSelect: vi.fn(), danger: true }] },
    })
    await w.find('.rmenu-trigger-badge').trigger('click')
    expect(w.find('.rmenu-item').classes()).toContain('is-danger')
  })
})
