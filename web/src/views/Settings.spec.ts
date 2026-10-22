import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { mountView, mountViewWithRouter } from '@/test/mount'
import Settings from '@/views/Settings.vue'
import { RButtonStub, REmptyStub, RSelectStub } from '@/test/stubs'
import { useToastStore } from '@/stores/toast'

/**
 * Settings.vue 用例（docs/07 §3 SettingItem / docs/04 §2.4）。
 *
 * 五要素（说明/默认值/校验/敏感/生效时机）+ env 锁定只读 + 脏状态拦截路由离开
 * + 通道测试 + 历史回滚 + API Token 管理。铁律：保存结果按接口返回的
 * effective_summary 展示，不在前端猜生效时机。
 */
const mocks = vi.hoisted(() => ({
  getConfig: vi.fn(),
  saveConfig: vi.fn(),
  getConfigHistory: vi.fn(),
  rollbackConfig: vi.fn(),
  listTokens: vi.fn(),
  createToken: vi.fn(),
  revokeToken: vi.fn(),
  testQB: vi.fn(),
  testN115: vi.fn(),
  telegramStatus: vi.fn(),
}))

vi.mock('@/api/client', () => ({ api: mocks }))

function makeItem(over: Record<string, unknown> = {}) {
  return {
    key: 'download.max_concurrent',
    desc: '同时下载的任务数上限',
    default: 3,
    value: 3,
    type: 'int',
    effective: 'next_task',
    sensitive: false,
    locked: false,
    version: 2,
    ...over,
  }
}

const configItems = [
  makeItem(),
  makeItem({
    key: 'storage.save_path',
    desc: '下载文件保存目录',
    default: '/media/downloads',
    value: '/media/downloads',
    type: 'string',
    effective: 'immediate',
    version: 1,
  }),
  makeItem({
    key: 'download.format',
    desc: '下载格式优先级',
    default: 'mp4',
    value: 'mp4',
    type: 'string',
    effective: 'immediate',
    choices: ['mp4', 'mkv', 'webm'],
    version: 1,
  }),
  makeItem({
    key: 'server.debug',
    desc: '调试模式',
    default: false,
    value: false,
    type: 'bool',
    effective: 'restart',
    version: 1,
  }),
  makeItem({
    key: 'qb.password',
    desc: 'qBittorrent 密码',
    default: '',
    value: '******',
    type: 'string',
    effective: 'immediate',
    sensitive: true,
    version: 4,
  }),
  makeItem({
    key: 'server.port',
    desc: '服务端口',
    default: 8080,
    value: 8080,
    type: 'int',
    effective: 'restart',
    locked: true,
    env_var: 'RACCOON_PORT',
    version: 1,
  }),
]

const stubs = {
  RButton: RButtonStub,
  REmpty: REmptyStub,
  RSelect: RSelectStub,
}

function mountSettings() {
  return mountView(Settings, [], { global: { stubs } })
}

function btnByText(w: any, text: string) {
  return w.findAll('button').find((b: any) => b.text().includes(text))
}

beforeEach(() => {
  mocks.getConfig.mockReset().mockResolvedValue({ items: configItems, version: 7 })
  mocks.saveConfig.mockReset().mockResolvedValue({
    effective_summary: { immediate: ['storage.save_path'], next_task: ['download.max_concurrent'], restart: [] },
  })
  mocks.getConfigHistory.mockReset().mockResolvedValue({ items: [] })
  mocks.listTokens.mockReset().mockResolvedValue({ items: [] })
  mocks.createToken.mockReset().mockResolvedValue({ token: 'rk_live_abcdef123456' })
  mocks.revokeToken.mockReset().mockResolvedValue({ ok: true })
  mocks.testQB.mockReset().mockResolvedValue({ ok: true, version: '4.6.0' })
  mocks.testN115.mockReset().mockResolvedValue({ ok: false })
  mocks.telegramStatus.mockReset().mockResolvedValue({ data: { enabled: false, allowlist: 0 } })
})

describe('Settings 视图', () => {
  it('按分组渲染配置项，并给出生效时机徽章与默认值', async () => {
    const w = mountSettings()
    await flushPromises()

    const keys = w.findAll('.setting-key').map((n: any) => n.text())
    expect(keys).toContain('download.max_concurrent')
    expect(keys).toContain('storage.save_path')

    const group = w.find('#download')
    expect(group.exists()).toBe(true)
    // 生效时机徽章来自后端 effective，不前端猜
    expect(group.text()).toContain('下个任务生效')
    expect(w.find('#storage').text()).toContain('立即生效')
    expect(w.find('#server').text()).toContain('需重启生效')
    // 默认值展示
    expect(w.find('#download').text()).toContain('默认 3')
  })

  it('敏感项不回填掩码：输入框为空 + 「当前」展示掩码，留空不算脏', async () => {
    const w = mountSettings()
    await flushPromises()

    const qb = w.find('#qb')
    const input = qb.find('input[type="password"]') as any
    // 后端只回掩码，绝不能把掩码当值回填进输入框（否则保存会把掩码写进配置）
    expect(input.element.value).toBe('')
    expect(qb.text()).toContain('当前：******')
    expect(qb.text()).toContain('敏感')
    // 未做任何修改 → 保存按钮禁用、没有未保存标记
    expect(btnByText(w, '保存改动')!.attributes('disabled')).toBeDefined()
    expect(w.find('.dirty-dot').exists()).toBe(false)
  })

  it('改一个值即进入脏状态，保存时只提交改动项并展示生效时机', async () => {
    const w = mountSettings()
    await flushPromises()

    const input = w.find('#download input[type="number"]')
    await input.setValue('6')
    await flushPromises()

    expect(w.find('.dirty-dot').exists()).toBe(true)
    expect(w.find('#download .dirty-pip').exists()).toBe(true)

    await btnByText(w, '保存改动').trigger('click')
    await flushPromises()

    expect(mocks.saveConfig).toHaveBeenCalledWith({ 'download.max_concurrent': 6 })
    // 生效时机必须按接口返回的 effective_summary 展示（走 toast，不在页面文本里）
    const toastText = useToastStore().items.map((t) => t.text).join(' ')
    expect(toastText).toContain('已保存')
    expect(toastText).toContain('立即生效：storage.save_path')
    expect(toastText).toContain('下个任务生效：download.max_concurrent')
    // 保存后脏状态清空
    expect(w.find('.dirty-dot').exists()).toBe(false)
  })

  it('把值改回原值会自动退出脏状态（不算未保存改动）', async () => {
    const w = mountSettings()
    await flushPromises()

    const input = w.find('#download input[type="number"]')
    await input.setValue('6')
    await flushPromises()
    expect(w.find('.dirty-dot').exists()).toBe(true)

    await input.setValue('3')
    await flushPromises()
    expect(w.find('.dirty-dot').exists()).toBe(false)
  })

  it('env 锁定的项只读，并标注是哪个环境变量锁的', async () => {
    const w = mountSettings()
    await flushPromises()

    const server = w.find('#server')
    expect(server.text()).toContain('RACCOON_PORT')
    const portInput = server.findAll('input').find((i: any) => i.attributes('value') === '8080')!
    expect(portInput.attributes('disabled')).toBeDefined()
    expect(portInput.element.value).toBe('8080')
  })

  it('枚举项用后端 choices 渲染下拉（前端不写死值域）', async () => {
    const w = mountSettings()
    await flushPromises()

    const select = w.find('#download select')
    const opts = select.findAll('option').map((o: any) => o.attributes('value'))
    expect(opts).toEqual(['mp4', 'mkv', 'webm'])
    // 当前值被选中
    expect((select.element as HTMLSelectElement).value).toBe('mp4')
  })

  it('bool 项用开关渲染，切换即脏', async () => {
    const w = mountSettings()
    await flushPromises()

    const box = w.find('#server input[type="checkbox"]')
    expect((box.element as HTMLInputElement).checked).toBe(false)
    await box.setValue(true)
    await flushPromises()
    expect(w.find('.dirty-dot').exists()).toBe(true)
  })

  it('有未保存改动时离开路由会被拦（window.confirm）', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false)
    // 必须经 router-view 渲染：onBeforeRouteLeave 是挂在路由记录上的守卫，
    // 直接 mount 组件（没有 router-view）不会触发离开拦截。
    const host = { template: '<router-view />' }
    const { wrapper: w, router } = mountViewWithRouter(host, [
      { path: '/settings', component: Settings },
      { path: '/tasks', component: { template: '<div>tasks</div>' } },
    ])
    await router.push('/settings')
    await router.isReady()
    await flushPromises()

    // 先改脏
    await w.find('#download input[type="number"]').setValue('9')
    await flushPromises()

    // 尝试离开：应被 confirm 拦住，路由没变
    await router.push('/tasks')
    await flushPromises()
    expect(confirmSpy).toHaveBeenCalled()
    expect(router.currentRoute.value.path).toBe('/settings')

    // 用户确认后放行
    confirmSpy.mockReturnValue(true)
    await router.push('/tasks')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/tasks')
    confirmSpy.mockRestore()
  })

  it('没改动时离开路由不弹确认', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const host = { template: '<router-view />' }
    const { router } = mountViewWithRouter(host, [
      { path: '/settings', component: Settings },
      { path: '/tasks', component: { template: '<div>tasks</div>' } },
    ])
    await router.push('/settings')
    await router.isReady()
    await flushPromises()

    await router.push('/tasks')
    await flushPromises()
    expect(confirmSpy).not.toHaveBeenCalled()
    expect(router.currentRoute.value.path).toBe('/tasks')
    confirmSpy.mockRestore()
  })

  it('外部通道测试：qB 成功带版本号，115 失败如实报', async () => {
    const w = mountSettings()
    await flushPromises()

    await btnByText(w, '测 qBittorrent').trigger('click')
    await flushPromises()
    expect(mocks.testQB).toHaveBeenCalled()
    const qbResult = w.findAll('.conn-result')[0]
    expect(qbResult.text()).toContain('连接正常（版本 4.6.0）')
    expect(qbResult.classes()).toContain('ok')

    await btnByText(w, '测 115').trigger('click')
    await flushPromises()
    const n115Result = w.findAll('.conn-result')[1]
    expect(n115Result.text()).toContain('连接失败')
    expect(n115Result.classes()).toContain('bad')
  })

  it('Telegram 没有 test 端点：用 enabled 如实表达', async () => {
    mocks.telegramStatus.mockResolvedValueOnce({ data: { enabled: true, allowlist: 5 } })
    const w = mountSettings()
    await flushPromises()

    await btnByText(w, '查 Telegram 状态').trigger('click')
    await flushPromises()

    const tgResult = w.findAll('.conn-result')[0]
    expect(tgResult.text()).toContain('通道已启用，白名单 5 人')
    expect(tgResult.classes()).toContain('ok')
  })

  it('签发令牌：明文只显示这一次，可复制', async () => {
    const w = mountSettings()
    await flushPromises()

    await w.find('#tokens input').setValue('my-laptop')
    await btnByText(w, '签发').trigger('click')
    await flushPromises()

    expect(mocks.createToken).toHaveBeenCalledWith('my-laptop', 3)
    const plain = w.find('.token-plain')
    expect(plain.exists()).toBe(true)
    expect(plain.find('code').text()).toBe('rk_live_abcdef123456')
    // 输入框清空，避免明文残留
    expect((w.find('#tokens input').element as HTMLInputElement).value).toBe('')
  })

  it('令牌没起名字时不给签发', async () => {
    const w = mountSettings()
    await flushPromises()
    await btnByText(w, '签发').trigger('click')
    await flushPromises()
    expect(mocks.createToken).not.toHaveBeenCalled()
  })

  it('撤销令牌后从列表消失', async () => {
    mocks.listTokens.mockResolvedValue({
      items: [{ id: 'tk-1', name: 'my-laptop', scope: 3, created_at: 1_700_000_000_000, revoked: false }],
    })
    const w = mountSettings()
    await flushPromises()

    const row = w.find('.token-row')
    expect(row.text()).toContain('my-laptop')
    expect(row.text()).toContain('scope 3')

    await btnByText(w, '撤销').trigger('click')
    await flushPromises()
    expect(mocks.revokeToken).toHaveBeenCalledWith('tk-1')
  })

  it('配置历史可回滚到指定版本（写入为新版本）', async () => {
    mocks.getConfigHistory.mockResolvedValue({
      items: [
        {
          id: 1,
          key: 'download.max_concurrent',
          value: 2,
          version: 1,
          changed_at: 1_700_000_000_000,
          changed_by: 'admin',
        },
      ],
    })
    const w = mountSettings()
    await flushPromises()

    const row = w.find('.history-row')
    expect(row.text()).toContain('download.max_concurrent')
    expect(row.text()).toContain('v1')

    await btnByText(w, '回滚到此版本').trigger('click')
    await flushPromises()
    expect(mocks.rollbackConfig).toHaveBeenCalledWith('download.max_concurrent', 1)
  })

  it('没有配置历史时显示空态而不是空白', async () => {
    const w = mountSettings()
    await flushPromises()
    const empty = w.find('#history .stub-empty')
    expect(empty.exists()).toBe(true)
    expect(empty.text()).toContain('还没有配置历史')
  })

  it('加载失败显示错误卡 + 重试入口', async () => {
    mocks.getConfig.mockRejectedValueOnce(new Error('配置接口 503'))
    const w = mountSettings()
    await flushPromises()

    const card = w.find('.error-card')
    expect(card.exists()).toBe(true)
    expect(card.text()).toContain('配置接口 503')

    // 重试后恢复
    await btnByText(w, '重试').trigger('click')
    await flushPromises()
    expect(w.find('.error-card').exists()).toBe(false)
    expect(w.find('#download').exists()).toBe(true)
  })
})
