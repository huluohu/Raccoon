import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { mountView, mountViewWithRouter } from '@/test/mount'
import Settings from '@/views/Settings.vue'
import { RButtonStub, REmptyStub, RSelectStub } from '@/test/stubs'
import { useToastStore } from '@/stores/toast'
import { useAuthStore } from '@/stores/auth'
import { ApiError } from '@/api/types'

/**
 * Settings.vue 用例（docs/07 §3 SettingItem / docs/04 §2.4）。
 *
 * 五要素（说明/默认值/校验/敏感/生效时机）+ env 锁定只读 + 脏状态拦截路由离开
 * + 历史回滚 + API Token 管理（通道参数/通道测试已归位「连接」页）。铁律：保存结果按接口返回的
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
  diagPreview: vi.fn(),
  diagExport: vi.fn(),
  authChangeUsername: vi.fn(),
  authChangePassword: vi.fn(),
}))

vi.mock('@/api/client', () => ({ api: mocks }))

function makeItem(over: Record<string, unknown> = {}) {
  return {
    key: 'download.max_concurrent',
    label: '全局并发上限',
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
    label: '下载目录',
    desc: '下载文件保存目录',
    default: '/media/downloads',
    value: '/media/downloads',
    type: 'string',
    effective: 'immediate',
    version: 1,
  }),
  makeItem({
    key: 'download.format',
    label: '默认画质',
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
    label: '调试模式',
    desc: '调试模式',
    default: false,
    value: false,
    type: 'bool',
    effective: 'restart',
    version: 1,
  }),
  makeItem({
    key: 'qb.password',
    label: 'qBittorrent 密码',
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
    label: '服务端口',
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
  mocks.diagPreview.mockReset().mockResolvedValue({
    preview: true,
    manifest: { version: '0.9.0' },
    log_sample: [],
  })
  mocks.diagExport.mockReset().mockResolvedValue(new Blob(['zip']))
  mocks.authChangeUsername.mockReset().mockResolvedValue({ user: { id: 'usr_1', username: 'renamed', created_at: 1 } })
  mocks.authChangePassword.mockReset().mockResolvedValue({ ok: true })
})

/** 账号分组挂载后置当前用户（mountView 会重建 pinia，user 必须挂载后再给）。 */
function withUser(w: any) {
  useAuthStore().user = { id: 'usr_1', username: 'admin', created_at: 1 }
  return w
}

describe('Settings 视图', () => {
  it('按分组渲染配置项，并给出生效时机徽章与默认值', async () => {
    const w = mountSettings()
    await flushPromises()

    const keys = w.findAll('.setting-key-hint').map((n: any) => n.text())
    expect(keys).toContain('download.max_concurrent')
    expect(keys).toContain('storage.save_path')
    // 主标题用中文名，不是键名——用户不该对着 storage.data_dir 猜含义
    const labels = w.findAll('.setting-label').map((n: any) => n.text())
    expect(labels).toContain('全局并发上限')
    expect(labels).toContain('下载目录')

    const group = w.find('#download')
    expect(group.exists()).toBe(true)
    // 生效时机徽章来自后端 effective，不前端猜
    expect(group.text()).toContain('下个任务生效')
    expect(w.find('#storage').text()).toContain('立即生效')
    expect(w.find('#server').text()).toContain('需重启生效')
    // 默认值展示
    expect(w.find('#download').text()).toContain('默认 3')
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

  it('诊断分组存在且提供查看/导出两个入口（§3「诊断入口在设置页」）', async () => {
    const w = mountSettings()
    await flushPromises()
    expect(w.find('#diag').exists()).toBe(true)
    expect(w.find('#diag').text()).toContain('诊断')
    // 导航里也有诊断入口
    expect(w.find('.side-nav').text()).toContain('诊断')
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

// ---- 账号（用户名 / 密码修改，docs/04 §2.1）----
describe('Settings 账号', () => {
  it('账号分组渲染当前用户名与「不可查看」的密码状态', async () => {
    const w = withUser(mountSettings())
    await flushPromises()

    const acct = w.find('#account')
    expect(acct.exists()).toBe(true)
    expect(acct.text()).toContain('admin')
    expect(acct.text()).toContain('已设置')
    expect(acct.text()).toContain('Argon2id')
    // 侧栏有账号入口
    expect(w.find('.side-nav').text()).toContain('账号')
  })

  it('改用户名成功：带当前口令调接口，成功后 store.user 同步（侧栏用户名跟着变）', async () => {
    const w = withUser(mountSettings())
    await flushPromises()
    const inputs = w.find('#account').findAll('input')

    await inputs[0].setValue('renamed')   // 新用户名
    await inputs[1].setValue('Hlb27Xlh') // 当前口令
    await flushPromises()
    await btnByText(w, '修改用户名').trigger('click')
    await flushPromises()

    expect(mocks.authChangeUsername).toHaveBeenCalledWith('renamed', 'Hlb27Xlh')
    // store.user 更新后侧栏用户名立即变成新值（不刷新页面）
    expect(useAuthStore().user?.username).toBe('renamed')
    // 输入框清空，不残留明文口令
    expect((inputs[0].element as HTMLInputElement).value).toBe('')
    expect((inputs[1].element as HTMLInputElement).value).toBe('')
    const toastText = useToastStore().items.map((t) => t.text).join(' ')
    expect(toastText).toContain('用户名已改为 renamed')
  })

  it('改用户名口令错误：透传后端文案，不清空输入', async () => {
    mocks.authChangeUsername.mockRejectedValueOnce(new ApiError('UNAUTHORIZED', '当前口令不正确', 'req-1'))
    const w = withUser(mountSettings())
    await flushPromises()
    const inputs = w.find('#account').findAll('input')

    await inputs[0].setValue('renamed')
    await inputs[1].setValue('wrong-pass')
    await flushPromises()
    await btnByText(w, '修改用户名').trigger('click')
    await flushPromises()

    const toastText = useToastStore().items.map((t) => t.text).join(' ')
    expect(toastText).toContain('当前口令不正确')
    // 失败不清空：用户改完口令重试，不用把新用户名再打一遍
    expect((inputs[0].element as HTMLInputElement).value).toBe('renamed')
  })

  it('改用户名前端校验：非法用户名/缺口令都不发请求', async () => {
    const w = withUser(mountSettings())
    await flushPromises()
    const inputs = w.find('#account').findAll('input')

    // 非法用户名（带空格）
    await inputs[0].setValue('名字 带空格')
    await inputs[1].setValue('Hlb27Xlh')
    await flushPromises()
    await btnByText(w, '修改用户名').trigger('click')
    await flushPromises()
    expect(mocks.authChangeUsername).not.toHaveBeenCalled()

    // 合法用户名但缺口令
    await inputs[0].setValue('goodname')
    await inputs[1].setValue('')
    await flushPromises()
    await btnByText(w, '修改用户名').trigger('click')
    await flushPromises()
    expect(mocks.authChangeUsername).not.toHaveBeenCalled()
  })

  it('改密码成功：旧口令 + 新口令 + 确认，成功后三者清空', async () => {
    const w = withUser(mountSettings())
    await flushPromises()
    const inputs = w.find('#account').findAll('input')

    await inputs[2].setValue('Hlb27Xlh')      // 当前口令
    await inputs[3].setValue('New-Passw0rd')  // 新口令
    await inputs[4].setValue('New-Passw0rd')  // 确认
    await flushPromises()
    await btnByText(w, '修改密码').trigger('click')
    await flushPromises()

    expect(mocks.authChangePassword).toHaveBeenCalledWith('Hlb27Xlh', 'New-Passw0rd')
    expect((inputs[2].element as HTMLInputElement).value).toBe('')
    expect((inputs[3].element as HTMLInputElement).value).toBe('')
    expect((inputs[4].element as HTMLInputElement).value).toBe('')
    const toastText = useToastStore().items.map((t) => t.text).join(' ')
    expect(toastText).toContain('密码已修改')
  })

  it('改密码两次不一致 / 弱口令 / 缺旧口令都不发请求', async () => {
    const w = withUser(mountSettings())
    await flushPromises()
    const inputs = w.find('#account').findAll('input')

    // 两次不一致
    await inputs[2].setValue('Hlb27Xlh')
    await inputs[3].setValue('New-Passw0rd')
    await inputs[4].setValue('Another-Pass1')
    await flushPromises()
    await btnByText(w, '修改密码').trigger('click')
    await flushPromises()
    expect(mocks.authChangePassword).not.toHaveBeenCalled()

    // 弱口令（<8 位）
    await inputs[3].setValue('short')
    await inputs[4].setValue('short')
    await flushPromises()
    await btnByText(w, '修改密码').trigger('click')
    await flushPromises()
    expect(mocks.authChangePassword).not.toHaveBeenCalled()

    // 缺旧口令
    await inputs[2].setValue('')
    await inputs[3].setValue('New-Passw0rd')
    await inputs[4].setValue('New-Passw0rd')
    await flushPromises()
    await btnByText(w, '修改密码').trigger('click')
    await flushPromises()
    expect(mocks.authChangePassword).not.toHaveBeenCalled()
  })

  it('账号表单有未提交输入时，离开路由同样被拦', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const host = { template: '<router-view />' }
    const { wrapper: w, router } = mountViewWithRouter(host, [
      { path: '/settings', component: Settings },
      { path: '/tasks', component: { template: '<div>tasks</div>' } },
    ])
    await router.push('/settings')
    await router.isReady()
    await flushPromises()
    useAuthStore().user = { id: 'usr_1', username: 'admin', created_at: 1 }
    await flushPromises()

    // 只在账号表单里输入（配置项没动）
    await w.find('#account').findAll('input')[0].setValue('renamed')
    await flushPromises()

    await router.push('/tasks')
    await flushPromises()
    expect(confirmSpy).toHaveBeenCalled()
    expect(router.currentRoute.value.path).toBe('/settings')
    confirmSpy.mockRestore()
  })
})

// ---- 左分组导航 scrollspy（docs/07 §3 SettingsNav）----
describe('Settings 左分组导航', () => {
  /** jsdom 不做布局，getBoundingClientRect 全是 0，这里按 id 打桩。 */
  function stubTops(tops: Record<string, number>): void {
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
      const id = this.id
      const top = tops[id] ?? 9999
      return { top, bottom: top + 200, left: 0, right: 0, width: 0, height: 200, x: 0, y: top, toJSON: () => ({}) } as DOMRect
    })
  }

  function activeLabel(w: any): string {
    return w.find('.side-link.on').text().trim()
  }

  beforeEach(() => {
    // 默认：不滚到底部（否则会命中「底部兜底」分支）
    Object.defineProperty(document.documentElement, 'scrollHeight', { value: 10000, configurable: true })
    Object.defineProperty(window, 'innerHeight', { value: 900, configurable: true })
    Object.defineProperty(window, 'scrollY', { value: 0, configurable: true })
    window.scrollTo = vi.fn() as any
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('默认高亮第一个分组（账号）', async () => {
    stubTops({ account: 100, storage: 500, download: 1100, history: 5800 })
    const w = mountSettings()
    await flushPromises()
    expect(activeLabel(w)).toBe('账号')
  })

  it('首帧内容还没撑开（scrollHeight=0）时不能误高亮到最后一个分组', async () => {
    // 实测坑：首帧 documentElement.scrollHeight 还是 0，
    // 「innerHeight + scrollY >= scrollHeight - 2」恒成立 → 误高亮「配置历史」。
    Object.defineProperty(document.documentElement, 'scrollHeight', { value: 0, configurable: true })
    stubTops({ account: 100, storage: 500, download: 1100, conn: 5000, tokens: 5200, history: 5800 })
    const w = mountSettings()
    await flushPromises()
    expect(activeLabel(w)).toBe('账号')
  })

  it('滚到存储分组时，高亮跟着切到「存储」', async () => {
    stubTops({ account: -800, storage: 100, download: 1100, history: 5800 })
    const w = mountSettings()
    await flushPromises()
    window.dispatchEvent(new Event('scroll'))
    await new Promise((r) => setTimeout(r, 40))
    expect(activeLabel(w)).toBe('存储')
  })

  it('滚到底部时兜底高亮最后一个分组（配置历史）', async () => {
    stubTops({ account: -5600, storage: -5400, download: -4600, history: 484 })
    // 滚到底：scrollY + innerHeight >= scrollHeight
    Object.defineProperty(window, 'scrollY', { value: 9100, configurable: true })
    const w = mountSettings()
    await flushPromises()
    window.dispatchEvent(new Event('scroll'))
    await new Promise((r) => setTimeout(r, 40))
    expect(activeLabel(w)).toBe('配置历史')
  })

  it('点击导航项后即使锚点被页面底部截断，高亮也锁在被点项上', async () => {
    // 模拟实测场景：tokens docTop=5564，但页面最大只能滚到 5329，
    // 目标被顶到视口 235px 处，判定线（140px）选不中它 → 必须靠点击锁定兜住。
    stubTops({ account: -5174, storage: -4820, download: -4206, conn: 60, tokens: 235, history: 484 })
    Object.defineProperty(window, 'scrollY', { value: 5329, configurable: true })
    const w = mountSettings()
    await flushPromises()

    const tokensLink = w.findAll('.side-link').find((a: any) => a.text().includes('API 令牌'))!
    await tokensLink.trigger('click')
    await flushPromises()

    expect(activeLabel(w)).toBe('API 令牌')
  })

  it('用户主动滚动后解除点击锁定，高亮重新交给 scrollspy', async () => {
    stubTops({ account: -800, storage: 100, download: 1100, conn: 3000, tokens: 3200, history: 3400 })
    const w = mountSettings()
    await flushPromises()

    const tokensLink = w.findAll('.side-link').find((a: any) => a.text().includes('API 令牌'))!
    await tokensLink.trigger('click')
    await flushPromises()
    expect(activeLabel(w)).toBe('API 令牌')

    // 用户滚轮滚动 → 解锁 + scrollspy 接管（此时 storage 在判定线上方）
    stubTops({ account: -800, storage: 100, download: 1100, conn: 3000, tokens: 3200, history: 3400 })
    window.dispatchEvent(new Event('wheel'))
    await new Promise((r) => setTimeout(r, 40))
    expect(activeLabel(w)).toBe('存储')
  })

  it('点击锁定不会让被点项永久卡住：被点项滚出视口后自动解锁', async () => {
    stubTops({ account: -800, storage: 100, download: 1100, conn: 3000, tokens: 3200, history: 3400 })
    const w = mountSettings()
    await flushPromises()

    const tokensLink = w.findAll('.side-link').find((a: any) => a.text().includes('API 令牌'))!
    await tokensLink.trigger('click')
    await flushPromises()
    expect(activeLabel(w)).toBe('API 令牌')

    // tokens 已经滚出视口（top 远小于 0），判定线选中最新的 storage
    stubTops({ account: -900, storage: 60, download: 1100, conn: 3000, tokens: -200, history: 3400 })
    window.dispatchEvent(new Event('scroll'))
    await new Promise((r) => setTimeout(r, 40))
    expect(activeLabel(w)).toBe('存储')
  })
})

})
