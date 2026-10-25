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
    expect(w.text()).toContain('哔哩哔哩可用扫码登录')
  })
})

describe('平台账号页 · 自定义平台', () => {
  // DetailDrawer 的内容 teleport 到 body：断言与操作一律走 document。
  function q(sel: string): HTMLElement | null {
    return document.querySelector(sel)
  }

  async function openForm() {
    vi.mocked(api.listCredentials).mockResolvedValue({
      items: [{ id: 'c1', platform: 'bilibili', kind: 'session', label: 'B站' }],
    } as never)
    const w = mountView(Accounts, [], { global: { stubs } } as never)
    await flushPromises()
    const btn = w.findAll('button').find((b) => b.text().includes('添加平台凭证'))!
    await btn.trigger('click')
    await flushPromises()
    return w
  }

  async function chooseCustom() {
    const select = document.querySelector('select.field-select, form select') as HTMLSelectElement
    select.value = '__custom__'
    select.dispatchEvent(new Event('change'))
    await flushPromises()
  }

  beforeEach(() => {
    document.body.innerHTML = ''
  })

  it('选「自定义平台…」出现平台标识与 Cookie 域名字段', async () => {
    await openForm()
    await chooseCustom()
    expect(q('input[placeholder="小写字母/数字，如 mynas、emby"]')).not.toBeNull()
    expect(q('input[placeholder=".example.com"]')).not.toBeNull()
  })

  it('内置平台不出现域名字段', async () => {
    await openForm()
    expect(q('input[placeholder=".example.com"]')).toBeNull()
  })

  it('自定义平台提交时把域名拼进凭据内容、平台名转小写', async () => {
    vi.mocked(api.createCredential).mockResolvedValue({ data: {} } as never)
    await openForm()
    await chooseCustom()
    const idInput = document.querySelector('input[placeholder="小写字母/数字，如 mynas、emby"]') as HTMLInputElement
    idInput.value = 'MyNAS'
    idInput.dispatchEvent(new Event('input'))
    const domainInput = document.querySelector('input[placeholder=".example.com"]') as HTMLInputElement
    domainInput.value = '.nas.local'
    domainInput.dispatchEvent(new Event('input'))
    const textarea = document.querySelector('textarea') as HTMLTextAreaElement
    textarea.value = 'PMS=abc; token=xyz'
    textarea.dispatchEvent(new Event('input'))
    ;(document.querySelector('form') as HTMLFormElement).dispatchEvent(new Event('submit'))
    await flushPromises()
    expect(api.createCredential).toHaveBeenCalledWith(
      expect.objectContaining({
        platform: 'mynas',
        value: 'domain=.nas.local; PMS=abc; token=xyz',
      }),
    )
  })

  it('可从自定义模式返回内置列表', async () => {
    await openForm()
    await chooseCustom()
    const back = Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.includes('返回内置平台列表'))!
    back.click()
    await flushPromises()
    expect(document.querySelector('input[placeholder="小写字母/数字，如 mynas、emby"]')).toBeNull()
  })
})
