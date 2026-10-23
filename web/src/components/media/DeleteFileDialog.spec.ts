import { describe, expect, it } from 'vitest'

import DeleteFileDialog from '@/components/media/DeleteFileDialog.vue'
import { mount } from '@/test/mount'

/**
 * DeleteFileDialog 用例（docs/13 Q1：删文件是独立操作，必须二次确认）。
 *
 * 断言：文案说清「删什么 / 多大 / 什么会保留」；确认与取消分别 emit；
 * 取消不发请求（组件本身不发请求，这里锁住「取消 = 只关窗」）。
 *
 * 每个用例都必须卸载：对话框 attachTo body 且用 querySelector 取 DOM，
 * 上一个用例残留的实例会先被选中，导致断言读到别人的文案与别人的事件。
 */
function mountDialog(props: Record<string, unknown> = {}) {
  return mount(DeleteFileDialog, {
    props: { open: true, fileName: '第一集.mp4', size: 1024 * 1024, ...props },
    attachTo: document.body,
  })
}

describe('DeleteFileDialog', () => {
  it('文案包含文件名、大小与「任务记录会保留」', () => {
    mountDialog()
    const desc = document.body.querySelector('.rdialog-desc')?.textContent ?? ''
    expect(desc).toContain('第一集.mp4')
    expect(desc).toContain('1.0 MB')
    expect(desc).toContain('任务记录会保留')
    expect(desc).toContain('不可撤销')
  })

  it('给了路径时把路径也写出来（帮用户确认删的是哪个）', () => {
    mountDialog({ path: '/data/downloads/第一集.mp4' })
    expect(document.body.querySelector('.rdialog-desc')?.textContent).toContain('/data/downloads/第一集.mp4')
  })

  it('keepTask=false 时不再声称保留任务记录', () => {
    mountDialog({ keepTask: false })
    const desc = document.body.querySelector('.rdialog-desc')?.textContent ?? ''
    expect(desc).not.toContain('任务记录会保留')
  })

  it('点确认 emit confirm；点取消只关窗', async () => {
    const w = mountDialog()
    const cancel = document.body.querySelector<HTMLButtonElement>('[data-rdialog-action="cancel"]')
    cancel?.click()
    await w.vm.$nextTick()
    expect(w.emitted('confirm')).toBeFalsy()
    expect(w.emitted('update:open')?.[0]).toEqual([false])

    document.body.querySelector<HTMLButtonElement>('[data-rdialog-action="confirm"]')?.click()
    await w.vm.$nextTick()
    expect(w.emitted('confirm')).toBeTruthy()
    w.unmount()
  })
})
