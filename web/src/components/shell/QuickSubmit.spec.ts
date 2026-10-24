import { describe, expect, it } from 'vitest'

import QuickSubmit from '@/components/shell/QuickSubmit.vue'
import { mount } from '@/test/mount'

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

  // ---- 动作区：inline / row 两种布局必须共用同一份默认按钮 ----
  // 回归背景：早先 row 布局是独立的第二个 <slot name="actions"> outlet，
  // 而默认按钮只写在 inline 那个 outlet 的 fallback 里 → row 布局且调用方没传
  // #actions 时（主页引导卡）整个动作区渲染为空，首屏 CTA 消失。465 个用例
  // 没有一条覆盖 row 布局的默认按钮，所以它烂在线上没人发现。
  it('row 布局未传 #actions 时默认按钮仍然存在（主页引导卡 CTA 回归）', async () => {
    const w = mount(QuickSubmit, {
      props: { modelValue: 'https://example.com/v', actionsLayout: 'row', buttonText: '开始下载' },
    })
    const btn = w.find('.qsubmit-btn')
    expect(btn.exists()).toBe(true)
    expect(btn.text()).toBe('开始下载')
    // 布局差异只走 CSS（输入框占满整行 → 按钮换行），不再靠换容器
    expect(w.find('.qsubmit-row').classes()).toContain('qsubmit-row--stack')
    await btn.trigger('click')
    expect(w.emitted('submit')?.[0]).toEqual(['https://example.com/v'])
  })

  it('row 布局默认按钮与 inline 布局同源：空输入 / 提交中都禁用', () => {
    const empty = mount(QuickSubmit, { props: { modelValue: '   ', actionsLayout: 'row' } })
    expect(empty.find('.qsubmit-btn').attributes('disabled')).toBeDefined()

    const busy = mount(QuickSubmit, {
      props: { modelValue: 'https://example.com/v', actionsLayout: 'row', submitting: true },
    })
    expect(busy.find('.qsubmit-btn').attributes('disabled')).toBeDefined()
    expect(busy.find('.qsubmit-btn').text()).toBe('提交中…')
  })

  it('inline 布局（顶栏）默认按钮不受 row 改动影响', () => {
    const w = mount(QuickSubmit, {
      props: { modelValue: 'https://example.com/v', actionsLayout: 'inline' },
    })
    expect(w.find('.qsubmit-btn').exists()).toBe(true)
  })

  // ---- 格式闸（utils/link.ts）----
  it('格式不对时不提交，并就地提示（不发请求）', async () => {
    const w = mount(QuickSubmit, { props: { modelValue: 'www.example.com/v' } })
    await w.find('input').trigger('keyup.enter')
    expect(w.emitted('submit')).toBeUndefined()
    expect(w.find('.qsubmit-err').text()).toContain('http://')
  })

  it('分享文案提交的是提取后的链接，不是整段文案', async () => {
    const w = mount(QuickSubmit, {
      props: { modelValue: '【标题】https://b23.tv/abc 复制打开bilibili' },
    })
    await w.find('input').trigger('keyup.enter')
    expect(w.emitted('submit')?.[0]).toEqual(['https://b23.tv/abc'])
  })

  it('用户动过输入框才提示格式错误（页面刚加载不挂噪音提示）', async () => {
    const w = mount(QuickSubmit, { props: { modelValue: 'www.example.com/v' } })
    // 初始：模型值有内容但用户没动过 → 不提示
    expect(w.find('.qsubmit-err').exists()).toBe(false)
    // 动过 → 提示出现
    await w.find('input').setValue('www.example.com/v')
    expect(w.find('.qsubmit-err').exists()).toBe(true)
  })

  it('格式错误优先于接口错误位（格式错时根本发不出请求）', async () => {
    const w = mount(QuickSubmit, {
      props: { modelValue: 'www.example.com/v', error: '解析失败' },
    })
    await w.find('input').setValue('www.example.com/v')
    expect(w.find('.qsubmit-err').text()).toContain('http://')
    expect(w.find('.qsubmit-err').text()).not.toContain('解析失败')
  })
})
