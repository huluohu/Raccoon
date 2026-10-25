import { describe, expect, it } from 'vitest'

import WeChatPanel from '@/components/conn/WeChatPanel.vue'
import { RButtonStub, RouterLinkStub } from '@/test/stubs'
import { mount } from '@/test/mount'

const stubs = {
  RButton: RButtonStub,
  RouterLink: RouterLinkStub,
}

describe('WeChatPanel 组件', () => {
  it('已启用：状态显示就绪且展示 Webhook 回调地址', () => {
    const w = mount(WeChatPanel, {
      props: { enabled: true, webhookUrl: '/api/v1/integrations/wechat/webhook' },
      global: { stubs },
    })
    expect(w.find('.int-card').attributes('data-state')).toBe('ok')
    expect(w.find('.int-state-badge').text()).toContain('就绪')
    expect(w.find('.int-glyph').text()).toBe('✓')
    expect(w.find('.int-detail-hint').text()).toContain('/api/v1/integrations/wechat/webhook')
  })

  it('未启用：状态为未启用，并说明需配置 wechat.token', () => {
    const w = mount(WeChatPanel, { global: { stubs } })
    expect(w.find('.int-card').attributes('data-state')).toBe('unconfigured')
    expect(w.find('.int-state-badge').text()).toContain('未启用')
    expect(w.find('.int-glyph').text()).toBe('–')
    expect(w.find('.int-detail-hint').text()).toContain('需配置 wechat.token')
  })

  it('点「刷新状态」抛 refresh 事件，点「填写参数」抛 params 事件', async () => {
    const w = mount(WeChatPanel, { global: { stubs } })
    const btns = w.findAll('button')
    const refreshBtn = btns.find((b) => b.text().includes('刷新状态'))!
    const paramsBtn = btns.find((b) => b.text().includes('填写参数'))!

    await refreshBtn.trigger('click')
    expect(w.emitted('refresh')).toBeTruthy()

    await paramsBtn.trigger('click')
    expect(w.emitted('params')).toBeTruthy()
  })
})
