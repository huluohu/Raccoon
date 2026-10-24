import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises } from '@vue/test-utils'

import BiliQrLogin from '@/components/conn/BiliQrLogin.vue'
import { api } from '@/api/client'
import { RButtonStub } from '@/test/stubs'
import { mountView } from '@/test/mount'

/**
 * BiliQrLogin 用例：扫码登录是一个状态机（取码 → 等扫 → 确认 → 成功），
 * 每个相位都要有可观察反馈；成功后必须抛 done 让连接页刷新凭据列表。
 * API 与 qrcode 库全 mock——组件测试的是状态机与轮询纪律，不是 B 站可达性
 * 或 canvas 渲染（jsdom 无 canvas，真渲染也跑不了）。
 */
vi.mock('@/api/client', () => ({
  api: {
    biliQrStart: vi.fn(),
    biliQrPoll: vi.fn(),
  },
}))
vi.mock('qrcode', () => ({
  default: { toDataURL: vi.fn().mockResolvedValue('data:image/png;base64,fake') },
}))

const stubs = { RButton: RButtonStub }

/** 弹层 teleport 到 body：断言一律走 document，不指望组件树内能找到。 */
function q(sel: string): HTMLElement | null {
  return document.querySelector(sel)
}
function qText(sel: string): string {
  return q(sel)?.textContent ?? ''
}
function allButtons(): HTMLElement[] {
  return Array.from(document.querySelectorAll('button'))
}

beforeEach(() => {
  document.body.innerHTML = ''
  vi.useFakeTimers()
  vi.mocked(api.biliQrStart).mockResolvedValue({
    data: { url: 'https://passport.bilibili.com/scan?qrcode_key=k1', qrcode_key: 'k1', expires_in: 180 },
  })
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

async function openDialog() {
  const w = mountView(BiliQrLogin, [], { global: { stubs } } as never)
  await w.findAll('button')[0].trigger('click')
  await flushPromises()
  return w
}

describe('BiliQrLogin 组件', () => {
  it('点按钮取码并渲染二维码，进入等待扫码态', async () => {
    await openDialog()
    expect(api.biliQrStart).toHaveBeenCalled()
    expect(q('.qr-img')).not.toBeNull()
    expect(qText('.qr-status')).toContain('扫')
  })

  it('已扫码未确认：状态切到「确认登录」', async () => {
    vi.mocked(api.biliQrPoll).mockResolvedValue({ data: { status: 'scanned', message: '' } })
    await openDialog()
    await vi.advanceTimersByTimeAsync(2100)
    expect(qText('.qr-status')).toContain('确认登录')
  })

  it('确认成功：显示成功并抛 done 事件', async () => {
    vi.mocked(api.biliQrPoll).mockResolvedValue({ data: { status: 'success', message: '' } })
    const w = await openDialog()
    await vi.advanceTimersByTimeAsync(2100)
    expect(qText('.qr-status')).toContain('登录成功')
    expect(w.emitted('done')).toBeTruthy()
  })

  it('二维码过期：显示过期并允许重新取码', async () => {
    vi.mocked(api.biliQrPoll).mockResolvedValue({ data: { status: 'expired', message: '' } })
    await openDialog()
    await vi.advanceTimersByTimeAsync(2100)
    expect(qText('.qr-status')).toContain('过期')
    expect(allButtons().some((b) => b.textContent?.includes('重新取码'))).toBe(true)
  })

  it('取码失败：如实显示错误，不渲染二维码', async () => {
    vi.mocked(api.biliQrStart).mockRejectedValue(new Error('B 站接口不可达'))
    await openDialog()
    expect(q('.qr-img')).toBeNull()
    expect(qText('.qr-status')).toContain('B 站接口不可达')
  })

  it('关闭弹窗后轮询停止（旧代数不再发请求）', async () => {
    const poll = vi.mocked(api.biliQrPoll).mockResolvedValue({ data: { status: 'waiting_scan', message: '' } })
    await openDialog()
    ;(q('.qr-close') as HTMLElement).click()
    await flushPromises()
    const calls = poll.mock.calls.length
    await vi.advanceTimersByTimeAsync(6000)
    expect(poll.mock.calls.length).toBe(calls)
  })
})
