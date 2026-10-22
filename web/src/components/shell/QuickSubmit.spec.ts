import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import QuickSubmit from '@/components/shell/QuickSubmit.vue'

/**
 * QuickSubmit 用例（docs/07 §3 全局关键组件）。
 *
 * 关键验收点：
 * 1) 组件只管「输入 + 聚焦 + 错误内联」，提交意图以 emit 上抛——
 *    它不调接口、不弹 toast，职责边界清晰（§7 状态唯一来源）。
 * 2) 空白输入不触发提交（避免误触）。
 * 3) 错误内联在输入框下方，不是浮层。
 * 4) expose focus() 给 `/` 快捷键用（§7）。
 */
describe('QuickSubmit 组件', () => {
  it('输入变化以 update:modelValue 上抛', async () => {
    const w = mount(QuickSubmit, { props: { modelValue: '' } })
    await w.find('input').setValue('https://example.com/v')
    expect(w.emitted('update:modelValue')?.[0]).toEqual(['https://example.com/v'])
  })

  it('回车提交非空内容', async () => {
    const w = mount(QuickSubmit, { props: { modelValue: 'https://example.com/v' } })
    await w.find('input').trigger('keyup.enter')
    expect(w.emitted('submit')?.[0]).toEqual(['https://example.com/v'])
  })

  it('空白输入不提交', async () => {
    const w = mount(QuickSubmit, { props: { modelValue: '   ' } })
    await w.find('input').trigger('keyup.enter')
    expect(w.emitted('submit')).toBeUndefined()
  })

  it('提交中禁止重复提交', async () => {
    const w = mount(QuickSubmit, {
      props: { modelValue: 'https://example.com/v', submitting: true },
    })
    await w.find('input').trigger('keyup.enter')
    expect(w.emitted('submit')).toBeUndefined()
    expect(w.find('input').attributes('disabled')).toBeDefined()
  })

  it('错误内联在输入框下方（role=alert），不是浮层', () => {
    const w = mount(QuickSubmit, {
      props: { modelValue: '', error: '链接解析不了' },
    })
    expect(w.find('.qsubmit-err').text()).toBe('链接解析不了')
    expect(w.find('.qsubmit-err').attributes('role')).toBe('alert')
  })

  it('无错误时不留错误位', () => {
    const w = mount(QuickSubmit, { props: { modelValue: '' } })
    expect(w.find('.qsubmit-err').exists()).toBe(false)
  })

  it('expose focus()：`/` 快捷键聚焦入口', () => {
    const w = mount(QuickSubmit, { props: { modelValue: '' }, attachTo: document.body })
    w.vm.focus()
    expect(document.activeElement).toBe(w.find('input').element)
    w.unmount()
  })

  it('inputId 可自定义（页面内多个提交框要能区分）', () => {
    const w = mount(QuickSubmit, { props: { modelValue: '', inputId: 'task-url-input' } })
    expect(w.find('input').attributes('id')).toBe('task-url-input')
  })

  it('actions 插槽可替换默认按钮（下载页放「解析预览 / 直接下载」）', () => {
    const w = mount(QuickSubmit, {
      props: { modelValue: 'x' },
      slots: { actions: '<button class="custom-btn">解析预览</button>' },
    })
    expect(w.find('.custom-btn').exists()).toBe(true)
    expect(w.find('.qsubmit-btn').exists()).toBe(false)
  })
})
