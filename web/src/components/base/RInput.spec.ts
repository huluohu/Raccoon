import { describe, expect, it } from 'vitest'

import RInput from '@/components/base/RInput.vue'
import { mount } from '@/test/mount'

/**
 * RInput 用例（docs/07 §4 验收点：标签常驻 / 错误内联定位 / 清除与粘贴反馈）。
 * 用 @vue/test-utils 真实挂载，断言 DOM 而不是 mock 掉组件。
 */
describe('RInput', () => {
  const base = { props: { label: '任务链接' } }

  it('标签常驻显示（不是用 placeholder 冒充 label）', () => {
    const w = mount(RInput, { ...base })
    const label = w.find('.rinput-label')
    expect(label.exists()).toBe(true)
    expect(label.text()).toBe('任务链接')
  })

  it('v-model 双向绑定：输入触发 update:modelValue', async () => {
    const w = mount(RInput, { ...base })
    await w.find('.rinput-field').setValue('https://example.com/x')
    expect(w.emitted('update:modelValue')).toBeTruthy()
    expect((w.emitted('update:modelValue') ?? []).slice(-1)[0]).toEqual(['https://example.com/x'])
  })

  it('错误信息内联在输入框下方，且输入框描边转 danger 色', () => {
    const w = mount(RInput, { props: { label: '链接', error: '这不是合法链接' } })
    const msg = w.find('.rinput-msg--error')
    expect(msg.exists()).toBe(true)
    expect(msg.text()).toBe('这不是合法链接')
    expect(msg.attributes('role')).toBe('alert')
    // 描边转 danger：错误态落在输入框自身类名上（颜色由 token 驱动）
    expect(w.find('.rinput-field').classes()).toContain('rinput-field--error')
  })

  it('有值且非 password 时才显示清除按钮；空值不显示', async () => {
    const w = mount(RInput, { props: { label: '链接', modelValue: 'abc' } })
    expect(w.find('.rinput-clear').exists()).toBe(true)
    await w.find('.rinput-clear').trigger('click')
    expect(w.emitted('clear')).toBeTruthy()
    expect((w.emitted('update:modelValue') ?? []).slice(-1)[0]).toEqual([''])

    const empty = mount(RInput, { props: { label: '链接', modelValue: '' } })
    expect(empty.find('.rinput-clear').exists()).toBe(false)
  })

  it('password 类型永不显示清除按钮（避免明文泄露入口）', () => {
    const w = mount(RInput, { props: { label: '密码', type: 'password', modelValue: 'secret' } })
    expect(w.find('.rinput-clear').exists()).toBe(false)
    expect(w.find('.rinput-field').attributes('type')).toBe('password')
  })

  it('粘贴事件上抛给上层（批量导入靠这个拿剪贴板内容）', async () => {
    const w = mount(RInput, { ...base })
    await w.find('.rinput-field').trigger('paste')
    expect(w.emitted('paste')).toBeTruthy()
  })
})
