import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { mount } from '@vue/test-utils'
import DiagPanel from '@/components/settings/DiagPanel.vue'
import { RButtonStub, REmptyStub } from '@/test/stubs'

/**
 * DiagPanel 用例（docs/07 §3「诊断入口在设置页」）。
 *
 * 关键验收点：
 * 1) 预览如实展示后端给的 manifest，不编造缺失字段（缺就少一行，不填假值）。
 * 2) 预览失败要显示错误原因，不静默吞掉。
 * 3) 导出走后端字节流，前端不自己拼 zip（脱敏规则只有后端一份）。
 */
const mocks = vi.hoisted(() => ({
  diagPreview: vi.fn(),
  diagExport: vi.fn(),
}))

vi.mock('@/api/client', () => ({ api: mocks }))

const stubs = { RButton: RButtonStub, REmpty: REmptyStub }

function mountPanel() {
  return mount(DiagPanel, { global: { stubs } })
}

function btnByText(w: ReturnType<typeof mountPanel>, text: string) {
  return w.findAll('button').find((b) => b.text().includes(text))
}

beforeEach(() => {
  // jsdom 没有 Blob URL 能力，这里补上以便验证「取字节流 → 生成下载链接」这条主路径。
  ;(URL as unknown as { createObjectURL: (b: Blob) => string }).createObjectURL = vi.fn(
    () => 'blob:mock-url',
  )
  ;(URL as unknown as { revokeObjectURL: (u: string) => void }).revokeObjectURL = vi.fn()
  mocks.diagPreview.mockReset().mockResolvedValue({
    preview: true,
    manifest: {
      version: '0.9.0',
      generated_at: 1_700_000_000_000,
      log_lines: 200,
      task_counts: { waiting: 1, downloading: 2, done: 30 },
      config: { 'download.max_concurrent': '3', 'qb.password': '***' },
    },
    log_sample: ['line one', 'line two'],
    redaction: 'cookie/token/password/key 等字段名强制替换为 ***',
  })
  mocks.diagExport.mockReset().mockResolvedValue(new Blob(['zip'], { type: 'application/zip' }))
})

describe('DiagPanel 组件', () => {
  it('未加载时显示引导空态而不是空白', () => {
    const w = mountPanel()
    expect(w.find('.stub-empty').exists()).toBe(true)
    expect(w.find('.stub-empty').text()).toContain('还没看过诊断信息')
  })

  it('点击查看诊断信息后展示 manifest 关键项', async () => {
    const w = mountPanel()
    await btnByText(w, '查看诊断信息')!.trigger('click')
    await flushPromises()

    expect(mocks.diagPreview).toHaveBeenCalled()
    const text = w.find('.diag-manifest').text()
    expect(text).toContain('版本')
    expect(text).toContain('0.9.0')
    expect(text).toContain('日志行数')
    expect(text).toContain('200')
    expect(text).toContain('waiting 1')
    expect(text).toContain('done 30')
  })

  it('manifest 缺字段时不编造（只显示后端真给了的）', async () => {
    mocks.diagPreview.mockResolvedValueOnce({
      preview: true,
      manifest: { version: '0.9.0' },
      log_sample: [],
    })
    const w = mountPanel()
    await btnByText(w, '查看诊断信息')!.trigger('click')
    await flushPromises()

    const rows = w.findAll('.diag-row').map((r) => r.find('.diag-key').text())
    expect(rows).toEqual(['版本'])
    // 缺的不补假值
    expect(w.text()).not.toContain('日志行数')
  })

  it('展示日志样本与脱敏说明', async () => {
    const w = mountPanel()
    await btnByText(w, '查看诊断信息')!.trigger('click')
    await flushPromises()

    expect(w.find('.diag-log').text()).toContain('line one')
    expect(w.find('.diag-redaction').text()).toContain('强制替换为 ***')
  })

  it('预览失败时显示真实错误原因', async () => {
    mocks.diagPreview.mockRejectedValueOnce(new Error('诊断接口 503'))
    const w = mountPanel()
    await btnByText(w, '查看诊断信息')!.trigger('click')
    await flushPromises()

    expect(w.find('.diag-error').text()).toContain('诊断接口 503')
    // 失败时不留半个空面板
    expect(w.find('.diag-manifest').exists()).toBe(false)
  })

  it('导出诊断包走后端字节流（前端不自己拼 zip）', async () => {
    const w = mountPanel()
    await btnByText(w, '导出诊断包')!.trigger('click')
    await flushPromises()

    expect(mocks.diagExport).toHaveBeenCalled()
    // 导出失败走 error 事件，不静默
    expect(w.emitted('error')).toBeUndefined()
  })

  it('导出失败时上抛 error 事件（不假装成功）', async () => {
    mocks.diagExport.mockRejectedValueOnce(new Error('磁盘只读'))
    const w = mountPanel()
    await btnByText(w, '导出诊断包')!.trigger('click')
    await flushPromises()

    expect(w.emitted('error')![0]).toEqual(['磁盘只读'])
  })
})
