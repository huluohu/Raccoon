import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { mountView } from '@/test/mount'
import Connections from '@/views/Connections.vue'
import { DetailDrawerStub, PlatformBadgeStub, RButtonStub, REmptyStub, RouterLinkStub } from '@/test/stubs'

/**
 * Connections.vue 用例（docs/07 §3 Connections / docs/04 §2.5）。
 *
 * 两条铁律：
 * 1) 前端永远拿不到明文，只能看到打码 preview；编辑时 value 留空 = 不改内容，
 *    提交空值等于「保持原样」而不是「清空」。
 * 2) 外部集成通道如实区分「未配置 / 状态不明 / 可用 / 不可用」，不假装可用。
 */
const mocks = vi.hoisted(() => ({
  listCredentials: vi.fn(),
  createCredential: vi.fn(),
  updateCredential: vi.fn(),
  deleteCredential: vi.fn(),
  checkCredential: vi.fn(),
  integrationStatus: vi.fn(),
  telegramStatus: vi.fn(),
  testQB: vi.fn(),
  testN115: vi.fn(),
  getConfig: vi.fn(),
  saveConfig: vi.fn(),
}))

vi.mock('@/api/client', () => ({ api: mocks }))

function makeCred(over: Record<string, unknown> = {}) {
  return {
    id: 'c-1',
    platform: 'qbittorrent',
    kind: 'account',
    label: 'qB 凭据',
    status: 'active',
    note: '主号，PASS 全部',
    expires_at: Date.now() + 30 * 86_400_000,
    last_check_at: Date.now() - 3600_000,
    created_at: 1_700_000_000_000,
    updated_at: 1_700_000_000_000,
    has_value: true,
    preview: 'SESSDATA=ab********cd',
    ...over,
  }
}

const stubs = {
  RButton: RButtonStub,
  REmpty: REmptyStub,
  DetailDrawer: DetailDrawerStub,
  PlatformBadge: PlatformBadgeStub,
  'router-link': RouterLinkStub,
}

function mountConn() {
  return mountView(Connections, [], { global: { stubs } })
}

function btnByText(w: any, text: string) {
  return w.findAll('button').find((b: any) => b.text().includes(text))
}

beforeEach(() => {
  mocks.listCredentials.mockReset().mockResolvedValue({ items: [] })
  mocks.createCredential.mockReset().mockResolvedValue({ data: makeCred() })
  mocks.updateCredential.mockReset().mockResolvedValue({ data: makeCred() })
  mocks.deleteCredential.mockReset().mockResolvedValue({ ok: true })
  mocks.checkCredential.mockReset().mockResolvedValue({ ok: true, status: 'active', message: 'Cookie 有效' })
  mocks.integrationStatus.mockReset().mockResolvedValue({
    data: {
      qb: { configured: false },
      n115: { configured: false },
      telegram: { configured: false },
    },
  })
  mocks.telegramStatus.mockReset().mockResolvedValue({ data: { enabled: false, allowlist: 0 } })
  mocks.testQB.mockReset().mockResolvedValue({ ok: true, version: '4.6.0' })
  mocks.testN115.mockReset().mockResolvedValue({ ok: false })
})

describe('Connections 视图', () => {
  it('凭据卡只展示打码预览（前端不拿明文）', async () => {
    mocks.listCredentials.mockResolvedValueOnce({ items: [makeCred()] })
    const w = mountConn()
    await flushPromises()

    const card = w.find('.card')
    expect(card.text()).toContain('qB 凭据')
    expect(card.text()).toContain('SESSDATA=ab********cd')
    expect(card.text()).toContain('账号')
    expect(card.text()).toContain('可用')
    expect(card.text()).toContain('主号，PASS 全部')
    // 页面级承诺
    expect(w.text()).toContain('内容平台的登录态在「平台账号」页')
  })

  it('空凭据显示引导空态并说明「不配也能下公开内容」', async () => {
    const w = mountConn()
    await flushPromises()
    expect(w.find('.stub-empty').text()).toContain('还没有集成组件的凭据')
    expect(w.find('.stub-empty').text()).toContain('平台账号')
  })

  it('编辑态留空 value = 保持原样，不把空串写上去', async () => {
    mocks.listCredentials.mockResolvedValueOnce({ items: [makeCred()] })
    const w = mountConn()
    await flushPromises()

    await btnByText(w, '编辑').trigger('click')
    await flushPromises()

    // 抽屉标题切到编辑态，平台/类型锁定不可改
    expect(w.find('.stub-drawer-title').text()).toContain('编辑凭据')
    // value 输入框 placeholder 明示「留空表示不修改」，且带出当前打码预览
    const valueField = w.find('textarea')
    expect(valueField.attributes('placeholder')).toContain('留空表示不修改')
    expect(valueField.attributes('placeholder')).toContain('SESSDATA=ab********cd')

    await w.find('form').trigger('submit')
    await flushPromises()

    expect(mocks.updateCredential).toHaveBeenCalledWith('c-1', { label: 'qB 凭据', note: '主号，PASS 全部' })
    const body = mocks.updateCredential.mock.calls[0][1] as Record<string, unknown>
    expect('value' in body).toBe(false)
  })

  it('健康检查结果内联展示（成功/失败分别着色）', async () => {
    // 检查成功后视图会重新 load()，所以这里必须给持久 mock，不能用 Once
    mocks.listCredentials.mockResolvedValue({ items: [makeCred()] })
    const w = mountConn()
    await flushPromises()

    await btnByText(w, '健康检查').trigger('click')
    await flushPromises()

    expect(mocks.checkCredential).toHaveBeenCalledWith('c-1')
    const result = w.find('.check')
    expect(result.exists()).toBe(true)
    expect(result.text()).toContain('Cookie 有效')
    expect(result.classes()).toContain('ok')
  })

  it('失效/待确认凭据给出汇总告警', async () => {
    mocks.listCredentials.mockResolvedValueOnce({
      items: [
        makeCred({ id: 'c-1', status: 'expiring', label: '快过期' }),
        makeCred({ id: 'c-2', status: 'expired', label: '已失效' }),
        makeCred({ id: 'c-3', status: 'active', label: '好的' }),
      ],
    })
    const w = mountConn()
    await flushPromises()

    const alert = w.find('.alert')
    expect(alert.text()).toContain('有 2 条凭据已失效或待确认')
    expect(alert.text()).toContain('失效后高清晰度与会员内容会下载失败')
  })

  it('外部集成三通道未配置时如实显示「未配置」', async () => {
    const w = mountConn()
    await flushPromises()

    const cards = w.findAll('.int-card')
    expect(cards.length).toBe(3)
    expect(cards[0].attributes('data-state')).toBe('unconfigured')
    expect(cards[0].text()).toContain('未配置')
    expect(cards[0].text()).toContain('点「填写参数」就地配置后启用')
    // Telegram 没有测试端点：按钮是「刷新状态」而不是「测试连接」
    expect(cards[2].text()).toContain('刷新状态')
  })

  it('已配置但后端没给实探结果时显示「状态不明」，不假装可用', async () => {
    mocks.integrationStatus.mockResolvedValueOnce({
      data: {
        qb: { configured: true },
        n115: { configured: false },
        telegram: { configured: false },
      },
    })
    const w = mountConn()
    await flushPromises()

    const cards = w.findAll('.int-card')
    expect(cards[0].attributes('data-state')).toBe('unknown')
    expect(cards[0].text()).toContain('状态不明')
    expect(cards[0].text()).toContain('尚未探测到实探结果')
  })

  it('Telegram 启用态单独由 /telegram/status 决定（enabled + 白名单人数）', async () => {
    mocks.telegramStatus.mockResolvedValueOnce({ data: { enabled: true, allowlist: 3 } })
    const w = mountConn()
    await flushPromises()

    const tg = w.findAll('.int-card')[2]
    expect(tg.attributes('data-state')).toBe('ok')
    expect(tg.text()).toContain('可用')
    expect(tg.text()).toContain('白名单 3 人')
  })

  it('测试 qBittorrent 成功时展示版本号', async () => {
    mocks.integrationStatus.mockResolvedValueOnce({
      data: {
        qb: { configured: true, ok: true },
        n115: { configured: false },
        telegram: { configured: false },
      },
    })
    const w = mountConn()
    await flushPromises()

    const qbCard = w.findAll('.int-card')[0]
    await qbCard.findAll('button').find((b: any) => b.text().includes('测试连接'))!.trigger('click')
    await flushPromises()

    expect(mocks.testQB).toHaveBeenCalled()
    const test = qbCard.find('.int-test')
    expect(test.text()).toContain('连接成功（版本 4.6.0）')
    expect(test.classes()).toContain('ok')
  })

  it('115 测试失败时如实报失败', async () => {
    mocks.integrationStatus.mockResolvedValueOnce({
      data: {
        qb: { configured: false },
        n115: { configured: true, ok: false, message: 'Cookie 过期' },
        telegram: { configured: false },
      },
    })
    const w = mountConn()
    await flushPromises()

    const n115Card = w.findAll('.int-card')[1]
    await n115Card.findAll('button').find((b: any) => b.text().includes('测试连接'))!.trigger('click')
    await flushPromises()

    expect(mocks.testN115).toHaveBeenCalled()
    expect(mocks.testQB).not.toHaveBeenCalled()
    const test = n115Card.find('.int-test')
    expect(test.text()).toContain('连接失败')
    expect(test.classes()).toContain('bad')
  })

  it('删除凭据（破坏性按钮带二次确认语义）', async () => {
    mocks.listCredentials.mockResolvedValueOnce({ items: [makeCred()] })
    const w = mountConn()
    await flushPromises()

    await btnByText(w, '删除').trigger('click')
    await flushPromises()
    expect(mocks.deleteCredential).toHaveBeenCalledWith('c-1')
  })
})

// ---- 通道参数抽屉（参数在连接页就地填写；设置页不再放通道组）----

function makeConfigItems() {
  return {
    items: [
      { key: 'qb.url', value: 'http://127.0.0.1:8080', sensitive: false },
      { key: 'qb.username', value: 'admin', sensitive: false },
      { key: 'qb.password', value: 'ad****rd', sensitive: true },
      { key: 'n115.client_id', value: '', sensitive: false },
      { key: 'n115.access_token', value: '', sensitive: true },
      { key: 'n115.refresh_token', value: '', sensitive: true },
      { key: 'telegram.bot_token', value: '12****ef', sensitive: true },
      { key: 'telegram.allowed_users', value: '123', sensitive: false },
      { key: 'telegram.allowed_chats', value: '', sensitive: false },
    ],
    version: 1,
  }
}

it('通道参数：打开抽屉回填非敏感项，敏感项留空并提示「已设置」', async () => {
  mocks.getConfig.mockResolvedValueOnce(makeConfigItems())
  const w = mountConn()
  await flushPromises()

  const btn = w.findAll('button').find((b: any) => b.text().includes('填写参数'))
  await btn!.trigger('click')
  await flushPromises()

  expect(mocks.getConfig).toHaveBeenCalled()
  const drawer = w.findAll('.stub-drawer')[w.findAll('.stub-drawer').length - 1]!
  const inputs = drawer.findAll('input')
  // qb.url / qb.username 回填实际值；密码项为空
  expect((inputs[0].element as HTMLInputElement).value).toBe('http://127.0.0.1:8080')
  expect((inputs[1].element as HTMLInputElement).value).toBe('admin')
  expect((inputs[2].element as HTMLInputElement).value).toBe('')
  expect(drawer.text()).toContain('保持现有值不变')
})

it('通道参数：保存时敏感项留空不提交（不打码串回写、不清空），保存后自动实探', async () => {
  mocks.getConfig.mockResolvedValueOnce(makeConfigItems())
  mocks.testQB.mockResolvedValueOnce({ ok: true, version: '4.6.0' })
  const w = mountConn()
  await flushPromises()

  const btn = w.findAll('button').find((b: any) => b.text().includes('填写参数'))
  await btn!.trigger('click')
  await flushPromises()

  const drawer = w.findAll('.stub-drawer')[w.findAll('.stub-drawer').length - 1]!
  // 用户只改了地址，密码留空
  const inputs = drawer.findAll('input')
  await inputs[0]!.setValue('http://127.0.0.1:8081')

  const save = drawer.findAll('button').find((b: any) => b.text().includes('保存并测试'))!
  await save.trigger('click')
  await flushPromises()

  expect(mocks.saveConfig).toHaveBeenCalledTimes(1)
  const sent = mocks.saveConfig.mock.calls[0][0] as Record<string, unknown>
  expect(sent['qb.url']).toBe('http://127.0.0.1:8081')
  expect('qb.password' in sent).toBe(false) // 敏感项留空 = 保持不变
  // 保存后自动实探 qB
  expect(mocks.testQB).toHaveBeenCalled()
})
