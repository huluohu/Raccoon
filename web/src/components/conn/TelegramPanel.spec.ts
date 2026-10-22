import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import TelegramPanel from '@/components/conn/TelegramPanel.vue'
import { RButtonStub, RouterLinkStub } from '@/test/stubs'

/**
 * TelegramPanel 用例（docs/07 §3「/connections → TelegramPanel」）。
 *
 * 关键验收点：
 * 1) Telegram 与 qB/115 语义不同：它没有测试端点，可用性来自
 *    「token 配了 + 白名单非空」。所以面板里**不能**有「测试连接」按钮，
 *    只有「刷新状态」——按钮语义不能混。
 * 2) 未启用时要说清缺什么：需配置 bot token 且白名单非空。
 * 3) 已启用要给出白名单人数（真实数字，不是「已配置」）。
 * 4) 状态是符号 + 文字双重编码。
 */
const stubs = {
  RButton: RButtonStub,
  RouterLink: RouterLinkStub,
}

describe('TelegramPanel 组件', () => {
  it('已启用：状态为可用 + 白名单人数', () => {
    const w = mount(TelegramPanel, { props: { enabled: true, allowlist: 3 }, global: { stubs } })
    expect(w.find('.int-card').attributes('data-state')).toBe('ok')
    expect(w.find('.int-state').text()).toContain('可用')
    expect(w.find('.int-glyph').text()).toBe('✓')
    expect(w.find('.int-detail').text()).toContain('已启用')
    expect(w.find('.int-detail').text()).toContain('3 人')
  })

  it('未启用：状态为未启用，并说清缺什么', () => {
    const w = mount(TelegramPanel, { global: { stubs } })
    expect(w.find('.int-card').attributes('data-state')).toBe('unconfigured')
    expect(w.find('.int-state').text()).toContain('未启用')
    expect(w.find('.int-glyph').text()).toBe('–')
    expect(w.find('.int-detail').text()).toContain('需配置 bot token 且白名单非空')
  })

  it('没有「测试连接」按钮（Telegram 没有测试端点，只有刷新状态）', () => {
    const w = mount(TelegramPanel, { props: { enabled: true }, global: { stubs } })
    const labels = w.findAll('button').map((b) => b.text())
    expect(labels.some((l) => l.includes('测试连接'))).toBe(false)
    expect(labels.some((l) => l.includes('刷新状态'))).toBe(true)
  })

  it('点「刷新状态」抛 refresh 事件', async () => {
    const w = mount(TelegramPanel, { global: { stubs } })
    const btn = w.findAll('button').find((b) => b.text().includes('刷新状态'))!
    await btn.trigger('click')
    expect(w.emitted('refresh')).toBeTruthy()
  })

  it('刷新中：按钮置灰并改文案', () => {
    const w = mount(TelegramPanel, { props: { loading: true }, global: { stubs } })
    const btn = w.findAll('button').find((b) => b.text().includes('刷新中'))!
    expect(btn.exists()).toBe(true)
    expect(btn.attributes('disabled')).toBeDefined()
  })

  it('有「去设置页填参数」的入口', () => {
    const w = mount(TelegramPanel, { global: { stubs } })
    const link = w.find('.int-link')
    expect(link.attributes('href')).toBe('/settings')
  })
})
