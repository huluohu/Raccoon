import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import CredentialList from '@/components/conn/CredentialList.vue'
import { RButtonStub, PlatformBadgeStub } from '@/test/stubs'
import type { Credential } from '@/api/types'

/**
 * CredentialList 用例（docs/07 §3「/connections → CredentialList（打码）」）。
 *
 * 关键验收点：
 * 1) 铁律：前端永远拿不到明文。组件只能渲染后端给的 preview（打码串），
 *    没有任何输入框可以回显明文——这是 §4 的硬要求，不能靠"加个显示明文按钮"绕开。
 * 2) 打码预览用等宽字体呈现，和普通文本明显区分（视觉信号：这是被遮过的内容）。
 * 3) 状态是「色点 + 文字」双重编码。
 * 4) 健康检查结果（ok/bad）如实展示，不美化。
 * 5) 有效期转人话：长期有效 / N 后过期 / 已过期。
 */
const stubs = {
  RButton: RButtonStub,
  PlatformBadge: PlatformBadgeStub,
}

function makeCred(over: Partial<Credential> = {}): Credential {
  return {
    id: 'c-1',
    platform: 'bilibili',
    kind: 'cookie_file',
    label: 'B站主号',
    status: 'active',
    created_at: 1_700_000_000_000,
    updated_at: 1_700_000_000_000,
    has_value: true,
    preview: 'SESSDATA=****abcd',
    ...over,
  }
}

describe('CredentialList 组件', () => {
  it('只渲染打码预览，没有明文输入框（前端拿不到明文的硬保证）', () => {
    const w = mount(CredentialList, { props: { items: [makeCred()] }, global: { stubs } })
    expect(w.find('.mono').text()).toBe('SESSDATA=****abcd')
    expect(w.find('input').exists()).toBe(false)
    expect(w.find('textarea').exists()).toBe(false)
  })

  it('没有 preview 时显示占位符，不留空白', () => {
    const w = mount(CredentialList, {
      props: { items: [makeCred({ preview: undefined })] },
      global: { stubs },
    })
    expect(w.find('.mono').text()).toBe('—')
  })

  it('状态是色点 + 文字双重编码（色点纯装饰）', () => {
    const w = mount(CredentialList, { props: { items: [makeCred()] }, global: { stubs } })
    expect(w.find('.dot').attributes('aria-hidden')).toBe('true')
    expect(w.find('.status').text()).toBe('可用')
    expect(w.find('.status').attributes('style')).toContain('--success')
  })

  it('四种凭据状态各有文案', () => {
    const map: Array<[Credential['status'], string]> = [
      ['active', '可用'],
      ['expiring', '待确认'],
      ['expired', '已失效'],
      ['disabled', '已停用'],
    ]
    for (const [status, text] of map) {
      const w = mount(CredentialList, { props: { items: [makeCred({ status })] }, global: { stubs } })
      expect(w.find('.status').text()).toBe(text)
    }
  })

  it('凭据类型徽章翻译成人话', () => {
    const w = mount(CredentialList, { props: { items: [makeCred()] }, global: { stubs } })
    expect(w.find('.kind').text()).toBe('Cookie')
  })

  it('有效期转人话：长期有效 / 已过期 / N 后过期', () => {
    const w1 = mount(CredentialList, {
      props: { items: [makeCred({ expires_at: undefined })] },
      global: { stubs },
    })
    expect(w1.text()).toContain('长期有效')

    const w2 = mount(CredentialList, {
      props: { items: [makeCred({ expires_at: Date.now() - 60_000 })] },
      global: { stubs },
    })
    expect(w2.text()).toContain('已过期')

    const w3 = mount(CredentialList, {
      props: { items: [makeCred({ expires_at: Date.now() + 3 * 24 * 3600_000 })] },
      global: { stubs },
    })
    expect(w3.text()).toContain('过期')
    expect(w3.text()).not.toContain('已过期')
  })

  it('上次检查时间为空时显示「未检查」', () => {
    const w = mount(CredentialList, {
      props: { items: [makeCred({ last_check_at: undefined })] },
      global: { stubs },
    })
    expect(w.text()).toContain('未检查')
  })

  it('健康检查结果如实展示（ok / bad 各带符号）', () => {
    const w = mount(CredentialList, {
      props: {
        items: [makeCred({ id: 'c-1' }), makeCred({ id: 'c-2' })],
        results: {
          'c-1': { ok: true, message: '登录态正常' },
          'c-2': { ok: false, message: 'Cookie 已失效' },
        },
      },
      global: { stubs },
    })
    const checks = w.findAll('.check')
    expect(checks[0].text()).toContain('✓')
    expect(checks[0].text()).toContain('登录态正常')
    expect(checks[0].classes()).toContain('ok')
    expect(checks[1].text()).toContain('Cookie 已失效')
    expect(checks[1].classes()).toContain('bad')
  })

  it('三个动作各自抛独立事件（带凭据对象）', async () => {
    const cred = makeCred()
    const w = mount(CredentialList, { props: { items: [cred] }, global: { stubs } })
    const btns = w.findAll('button')
    const find = (label: string) => btns.find((b) => b.text().includes(label))!

    await find('健康检查').trigger('click')
    expect(w.emitted('check')![0]).toEqual([cred])

    await find('编辑').trigger('click')
    expect(w.emitted('edit')![0]).toEqual([cred])

    await find('删除').trigger('click')
    expect(w.emitted('remove')![0]).toEqual([cred])
  })

  it('正在检查的凭据：按钮置灰并改文案', () => {
    const w = mount(CredentialList, {
      props: { items: [makeCred({ id: 'c-1' })], checking: 'c-1' },
      global: { stubs },
    })
    const btn = w.findAll('button').find((b) => b.text().includes('检查中'))!
    expect(btn.exists()).toBe(true)
    expect(btn.attributes('disabled')).toBeDefined()
  })

  it('备注存在才显示备注', () => {
    const w1 = mount(CredentialList, { props: { items: [makeCred()] }, global: { stubs } })
    expect(w1.find('.note').exists()).toBe(false)

    const w2 = mount(CredentialList, {
      props: { items: [makeCred({ note: '主号，日常用这个' })] },
      global: { stubs },
    })
    expect(w2.find('.note').text()).toBe('主号，日常用这个')
  })
})
