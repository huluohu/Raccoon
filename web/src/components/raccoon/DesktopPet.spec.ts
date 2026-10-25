import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import DesktopPet from '@/components/raccoon/DesktopPet.vue'
import { mount } from '@/test/mount'

const stubs = {
  RaccoonFace: { props: ['face', 'size'], template: '<div class="stub-face" :data-face="face" />' },
  RButton: { template: '<button @click="$emit(\'click\')"><slot /></button>' },
}

describe('DesktopPet 桌面常驻互动宠物组件', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })
  it('正常挂载渲染宠物头像', () => {
    const w = mount(DesktopPet, { global: { stubs } })
    expect(w.find('#desktop-pet-widget').exists()).toBe(true)
    expect(w.find('.stub-face').exists()).toBe(true)
  })

  it('点击宠物本体切换展开控制菜单', async () => {
    const w = mount(DesktopPet, { global: { stubs } })
    expect(w.find('.pet-popup').exists()).toBe(false)

    // 点击头像展开
    await w.find('.pet-avatar-btn').trigger('click')
    expect(w.find('.pet-popup').exists()).toBe(true)
    expect(w.find('.pet-popup-name').text()).toContain('浣熊小伴侣')

    // 再次点击关闭
    await w.find('.pet-popup-close').trigger('click')
    expect(w.find('.pet-popup').exists()).toBe(false)
  })

  it('点击暂时收起隐藏宠物', async () => {
    const w = mount(DesktopPet, { global: { stubs } })
    await w.find('.pet-avatar-btn').trigger('click')
    const hideBtn = w.find('.pet-popup-hide-hint')
    expect(hideBtn.exists()).toBe(true)
    await hideBtn.trigger('click')
    expect(w.find('#desktop-pet-widget').exists()).toBe(false)
  })
})
