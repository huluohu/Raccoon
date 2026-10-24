import { describe, expect, it } from 'vitest'

import QbTestPanel from '@/components/conn/QbTestPanel.vue'
import { RButtonStub, RouterLinkStub } from '@/test/stubs'
import { mount } from '@/test/mount'

/**
 * QbTestPanel 用例（docs/07 §3「/connections → QbTestPanel」）。
 *
 * 关键验收点：
 * 1) 四态必须如实区分，不能退化：
 *    未配置 / 已配置但后端没实探（状态不明）/ 可用 / 不可用。
 *    「已配置但没实探」绝不能显示成「可用」——那是假装可用。
 * 2) 手动测试结果优先于后端轮询状态（用户刚点完测试，看到的应该是这次的结果）。
 * 3) 状态是「符号 + 文字」双重编码，不靠颜色单独承载信息。
 * 4) 测试中按钮置灰防重复点。
 * 5) 必须给出「填写参数」入口（emit params，参数在「连接」页就地填写）。
 */
const stubs = {
  RButton: RButtonStub,
  RouterLink: RouterLinkStub,
}

describe('QbTestPanel 组件', () => {
  it('四态各有自己的文案，不许退化', () => {
    const cases: Array<[string, string]> = [
      ['unconfigured', '未配置'],
      ['unknown', '状态不明'],
      ['ok', '可用'],
      ['bad', '不可用'],
    ]
    for (const [state, text] of cases) {
      const w = mount(QbTestPanel, { props: { state: state as 'ok' }, global: { stubs } })
      expect(w.find('.int-state').text()).toContain(text)
      expect(w.find('.int-card').attributes('data-state')).toBe(state)
    }
  })

  it('「已配置但没实探」显示状态不明，不是可用（不假装可用）', () => {
    const w = mount(QbTestPanel, { props: { state: 'unknown' }, global: { stubs } })
    expect(w.text()).not.toContain('可用')
    expect(w.find('.int-state').text()).toContain('状态不明')
  })

  it('状态符号 + 文字双重编码（形状也带信息）', () => {
    const w = mount(QbTestPanel, { props: { state: 'bad' }, global: { stubs } })
    expect(w.find('.int-glyph').text()).toBe('✕')
    expect(w.find('.int-glyph').attributes('aria-hidden')).toBe('true')

    const ok = mount(QbTestPanel, { props: { state: 'ok' }, global: { stubs } })
    expect(ok.find('.int-glyph').text()).toBe('✓')

    const un = mount(QbTestPanel, { props: { state: 'unconfigured' }, global: { stubs } })
    expect(un.find('.int-glyph').text()).toBe('–')

    const unk = mount(QbTestPanel, { props: { state: 'unknown' }, global: { stubs } })
    expect(unk.find('.int-glyph').text()).toBe('?')
  })

  it('未传 state 时按「状态不明」处理（不默认成可用）', () => {
    const w = mount(QbTestPanel, { global: { stubs } })
    expect(w.find('.int-card').attributes('data-state')).toBe('unknown')
    expect(w.find('.int-state').text()).toContain('状态不明')
  })

  it('手动测试结果优先于后端上报状态', () => {
    const w = mount(QbTestPanel, {
      props: { state: 'ok', result: { ok: false, text: '连接被拒绝' } },
      global: { stubs },
    })
    expect(w.find('.int-card').attributes('data-state')).toBe('bad')
    expect(w.find('.int-state').text()).toContain('不可用')
    expect(w.find('.int-test').text()).toContain('连接被拒绝')
  })

  it('手动测试通过时覆盖后端旧状态', () => {
    const w = mount(QbTestPanel, {
      props: { state: 'unknown', result: { ok: true, text: '握手成功' } },
      global: { stubs },
    })
    expect(w.find('.int-card').attributes('data-state')).toBe('ok')
    expect(w.find('.int-test').text()).toContain('握手成功')
  })

  it('测试中：按钮置灰并改文案', () => {
    const w = mount(QbTestPanel, { props: { testing: true }, global: { stubs } })
    const btn = w.findAll('button').find((b) => b.text().includes('测试中'))!
    expect(btn.exists()).toBe(true)
    expect(btn.attributes('disabled')).toBeDefined()
  })

  it('点「测试连接」抛 test 事件', async () => {
    const w = mount(QbTestPanel, { global: { stubs } })
    const btn = w.findAll('button').find((b) => b.text().includes('测试连接'))!
    await btn.trigger('click')
    expect(w.emitted('test')).toBeTruthy()
  })

  it('有「填写参数」入口并 emit params（参数在连接页就地填写）', async () => {
    const w = mount(QbTestPanel, { global: { stubs } })
    const btn = w.findAll('button').find((b) => b.text().includes('填写参数'))
    expect(btn).toBeTruthy()
    await btn!.trigger('click')
    expect(w.emitted('params')).toBeTruthy()
  })

  it('状态说明 detail 存在才显示', () => {
    const w1 = mount(QbTestPanel, { global: { stubs } })
    expect(w1.find('.int-detail').exists()).toBe(false)

    const w2 = mount(QbTestPanel, { props: { detail: 'v4.6.0 · 端口 8080' }, global: { stubs } })
    expect(w2.find('.int-detail').text()).toBe('v4.6.0 · 端口 8080')
  })
})
