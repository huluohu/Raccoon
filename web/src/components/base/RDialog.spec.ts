import { afterEach, describe, expect, it, vi } from 'vitest'

import RDialog from '@/components/base/RDialog.vue'
import { mount } from '@/test/mount'

/**
 * RDialog 用例（docs/07 §4 验收点：仅破坏性决策 / 默认焦点「取消」/ ≤480px）。
 *
 * 注意：组件用 <Teleport to="body"> 挂载，DOM 不在 wrapper 子树里，
 * 因此一律查 document.body，不能 w.find()。
 * 焦点是安全属性：误触 Enter 时绝不能直接确认破坏性操作。
 */
function mountDialog(props: Record<string, unknown> = {}) {
  return mount(RDialog, {
    props: { open: true, title: '删除任务？', ...props },
    attachTo: document.body,
  })
}

/** 取 teleport 到 body 的根节点。 */
function root(): HTMLElement {
  return document.body.querySelector('.rdialog-root') as HTMLElement
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('RDialog', () => {
  it('打开时默认焦点落在「取消」上（不在「确认」上）', async () => {
    mountDialog({ danger: true, confirmText: '删除' })
    await vi.waitFor(() => {
      const cancel = document.body.querySelector('[data-rdialog-action="cancel"]') as HTMLElement
      expect(document.activeElement).toBe(cancel)
    })
  })

  it('Esc 关闭并向上抛 update:open(false)', async () => {
    const w = mountDialog()
    root().dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await w.vm.$nextTick()
    expect((w.emitted('update:open') ?? []).slice(-1)[0]).toEqual([false])
  })

  it('点遮罩关闭', async () => {
    const w = mountDialog()
    ;(document.body.querySelector('.rdialog-mask') as HTMLElement).click()
    expect((w.emitted('update:open') ?? []).slice(-1)[0]).toEqual([false])
  })

  it('确认按钮触发 confirm；danger 模式标题与按钮走 danger 语义', async () => {
    const w = mountDialog({ danger: true })
    ;(document.body.querySelector('[data-rdialog-action="confirm"]') as HTMLElement).click()
    expect(w.emitted('confirm')).toBeTruthy()
    // danger 语义落在面板修饰类上
    expect(document.body.querySelector('.rdialog')!.className).toContain('rdialog--danger')
  })

  it('confirmDisabled 时确认按钮禁用（请求进行中防重复提交）', () => {
    mountDialog({ confirmDisabled: true })
    const btn = document.body.querySelector('[data-rdialog-action="confirm"]') as HTMLButtonElement
    expect(btn.disabled).toBe(true)
  })

  it('面板宽度上限 480px（docs/07 §4）', () => {
    mountDialog()
    // 宽度契约改由 scripts/check-design-conformance.py 的 design-check 断言
    // （jsdom 不应用 scoped CSS，源码级断言更可靠，且纳入 make verify 门禁）。
    expect(root().querySelector('.rdialog')).toBeTruthy()
  })

  it('关闭时（open=false）不渲染任何 DOM', () => {
    mount(RDialog, { props: { open: false, title: 'x' } })
    expect(document.body.querySelector('.rdialog-root')).toBeNull()
  })

  it('取消按钮触发 cancel 事件', () => {
    const w = mountDialog()
    ;(document.body.querySelector('[data-rdialog-action="cancel"]') as HTMLElement).click()
    expect(w.emitted('cancel')).toBeTruthy()
  })

  it('aria-modal 与 role=dialog 齐备（读屏可感知模态）', () => {
    mountDialog()
    const r = root()
    expect(r.getAttribute('role')).toBe('dialog')
    expect(r.getAttribute('aria-modal')).toBe('true')
  })
})
