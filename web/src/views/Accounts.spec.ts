import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises } from '@vue/test-utils'

import Accounts from '@/views/Accounts.vue'
import { api } from '@/api/client'
import { mountView } from '@/test/mount'

/**
 * 平台账号页：内容源登录态专属页（2026-10-24 裁决，与「连接」分家）。
 * 关键验收：只显示内容平台凭据（Telegram/115/qB 归连接页）；
 * 扫码入口存在；无凭据空态指向「粘贴 Cookie」。
 */
vi.mock('@/api/client', () => ({
  api: {
    listCredentials: vi.fn(),
    createCredential: vi.fn(),
    updateCredential: vi.fn(),
    deleteCredential: vi.fn(),
    checkCredential: vi.fn(),
  },
}))

vi.mock('@/stores/toast', () => ({
  useToastStore: () => ({ success: vi.fn(), error: vi.fn(), warning: vi.fn(), info: vi.fn() }),
}))

const stubs = {
  RButton: { template: '<button><slot /></button>' },
  RouterLink: { template: '<a><slot /></a>' },
}

beforeEach(() => {
  document.body.innerHTML = ''
  vi.mocked(api.listCredentials).mockReset()
})

async function mountWithCreds(creds: unknown[]) {
  vi.mocked(api.listCredentials).mockResolvedValue({ items: creds } as never)
  const w = mountView(Accounts, [], { global: { stubs } } as never)
  await flushPromises()
  return w
}

describe('平台账号页', () => {
  it('按平台归属过滤：内容平台显示，集成组件不显示', async () => {
    const w = await mountWithCreds([
      { id: 'c1', platform: 'bilibili', kind: 'session', label: 'B站扫码' },
      { id: 'c2', platform: 'youtube', kind: 'cookie_file', label: 'YT Cookie' },
      { id: 'i1', platform: 'telegram', kind: 'token', label: 'TG Bot' },
      { id: 'i2', platform: 'qbittorrent', kind: 'account', label: 'qB' },
    ])
    const text = w.text()
    expect(text).toContain('B站扫码')
    expect(text).toContain('YT Cookie')
    expect(text).not.toContain('TG Bot')
    expect(text).not.toContain('qB')
  })

  it('空态有扫码入口与「添加平台凭证」动作；有凭证时页头按钮出现', async () => {
    const empty = await mountWithCreds([])
    const emptyBtns = empty.findAll('button').map((b) => b.text())
    expect(emptyBtns.some((l) => l.includes('扫码'))).toBe(true)
    expect(emptyBtns.some((l) => l.includes('添加平台凭证'))).toBe(true)
    // 空态时页头按钮隐藏（与空态卡 CTA 不同屏，同直播页裁决）
    expect(emptyBtns.filter((l) => l.includes('添加平台凭证')).length).toBe(1)

    const withCreds = await mountWithCreds([
      { id: 'c1', platform: 'bilibili', kind: 'session', label: 'B站扫码' },
    ])
    const btns = withCreds.findAll('button').map((b) => b.text())
    expect(btns.filter((l) => l.includes('添加平台凭证')).length).toBe(1)
  })

  it('空态文案指明内容平台归属与去向', async () => {
    const w = await mountWithCreds([])
    expect(w.text()).toContain('还没有配置任何平台账号')
    expect(w.text()).toContain('B站可用扫码登录')
  })
})
