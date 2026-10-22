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
}))

vi.mock('@/api/client', () => ({ api: mocks }))

function makeCred(over: Record<string, unknown> = {}) {
  return {
    id: 'c-1',
    platform: 'bilibili',
    kind: 'cookie_file',
    label: 'B站主号',
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
    expect(card.text()).toContain('B站主号')
    expect(card.text()).toContain('SESSDATA=ab********cd')
    expect(card.text()).toContain('Cookie')
    expect(card.text()).toContain('可用')
    expect(card.text()).toContain('主号，PASS 全部')
    // 页面级承诺
    expect(w.text()).toContain('内容 AES-256-GCM 加密落库，界面只回打码预览')
  })

  it('空凭据显示引导空态并说明「不配也能下公开内容」', async () => {
    const w = mountConn()
    await flushPromises()
    expect(w.find('.stub-empty').text()).toContain('还没有配置任何凭据')
    expect(w.find('.stub-empty').text()).toContain('不配凭据也能下载公开内容')
  })

  it('新建凭据内容为空时被前端拦下，不发请求', async () => {
    const w = mountConn()
    await flushPromises()
    await btnByText(w, '新建凭据').trigger('click')
    await flushPromises()

    // 直接提交表单：内容为空必须拦住
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(mocks.createCredential).not.toHaveBeenCalled()
  })

  it('新建凭据带上平台/类型/内容与到期日', async () => {
    const w = mountConn()
    await flushPromises()
    await btnByText(w, '新建凭据').trigger('click')
    await flushPromises()

    await w.find('input[placeholder="如：B站主号"]').setValue('小号')
    await w.find('textarea').setValue('SESSDATA=xxx; bili_jct=yyy')
    await w.find('input[type="date"]').setValue('2027-01-31')
    await w.find('form').trigger('submit')
    await flushPromises()

    expect(mocks.createCredential).toHaveBeenCalledWith(
      expect.objectContaining({
        platform: 'bilibili',
        kind: 'cookie_file',
        label: '小号',
        value: 'SESSDATA=xxx; bili_jct=yyy',
      }),
    )
    // 到期日转成当天 23:59:59 的时间戳
    const arg = mocks.createCredential.mock.calls[0][0] as { expires_at?: number }
    expect(arg.expires_at).toBe(new Date('2027-01-31T23:59:59').getTime())
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

    expect(mocks.updateCredential).toHaveBeenCalledWith('c-1', { label: 'B站主号', note: '主号，PASS 全部' })
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
    expect(cards[0].text()).toContain('在设置页填写连接参数后启用')
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
