import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { mountView, mountViewWithRouter } from '@/test/mount'
import App from '@/App.vue'

/**
 * App.vue 用例：导航信息架构 + 全局快捷键（docs/15 T03 验收 #3、T05 验收 #1）。
 *
 * 为什么这些断言必须挂在 App 上而不是各页面：
 * 1) R5 的问题就是「快捷键只在下载页有效」——把 `/` 与 G 和弦下沉到页面里，
 *    每加一个页面就漏一个。全局键只有一处实现（App 装 useGlobalShortcuts），
 *    所以用例也必须落在 App 上，才能证明「任何页面都能用」。
 * 2) 导航是 10 项 + 底部栏 5 高频 + 「更多」，这是信息架构的可见结论；
 *    断言数量与短标签，比断言「router 里有这条路由」更接近用户看到的东西。
 */

const mocks = vi.hoisted(() => ({
  authStatus: vi.fn(),
  authMe: vi.fn(),
  listTasks: vi.fn(),
  listNotifications: vi.fn(),
}))

vi.mock('@/api/client', () => ({ api: mocks }))

// jsdom 不实现 EventSource，tasks.connect() 需要它
class FakeEventSource {
  static instances: FakeEventSource[] = []
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  closed = false
  constructor(public url: string) {
    FakeEventSource.instances.push(this)
  }
  addEventListener() {}
  close() {
    this.closed = true
  }
}

/** 六个一级页面的占位视图：本文件只验外壳行为，不需要真页面。 */
const PageStub = defineComponent({ template: '<div class="page-stub" />' })

const ROUTES = [
  { path: '/', component: PageStub },
  { path: '/tasks', component: PageStub },
  { path: '/history', component: PageStub },
  { path: '/media', component: PageStub },
  { path: '/subscriptions', component: PageStub },
  { path: '/settings', component: PageStub },
]

beforeEach(() => {
  mocks.authStatus.mockReset().mockResolvedValue({ setup_required: false })
  mocks.authMe.mockReset().mockResolvedValue({
    user: { id: 'u-1', username: 'admin', created_at: 0 },
  })
  mocks.listTasks.mockReset().mockResolvedValue({ items: [], counts: {}, next_cursor: null })
  mocks.listNotifications.mockReset().mockResolvedValue({ items: [], unread: 0 })
  FakeEventSource.instances = []
  vi.stubGlobal('EventSource', FakeEventSource)
})

afterEach(() => {
  vi.unstubAllGlobals()
  // 全局监听挂在 document 上，用例间必须还原滚动锁，避免互相污染
  document.body.style.overflow = ''
  // 主题属性是 App 写在 <html> 上的，用例间必须摘掉，否则下一个用例读到上个的残留
  document.documentElement.removeAttribute('data-theme')
  localStorage.removeItem('raccoon.theme')
})

/** 挂载外壳并等到会话就绪（phase=ready 才会渲染导航与顶栏）。 */
async function mountApp() {
  const w = mountView(App, ROUTES)
  await flushPromises()
  return w
}

/** 在 document 上敲一个键（全局监听挂在 document 上）。 */
function press(key: string, init: KeyboardEventInit = {}) {
  document.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, ...init }))
}

describe('App 外壳', () => {
  it('侧导航 11 项，顺序与 docs/15 裁决一致（2026-10-24 新增平台账号）', async () => {
    const w = await mountApp()
    // 双行导航（中文主标签 + 等宽英文副标签），断言只取中文主标签
    const links = w.findAll('.nav nav a')
    expect(links.map((a) => a.find('.nav-link-text').text())).toEqual([
      '总览',
      '下载',
      '历史',
      '媒体库',
      '订阅',
      '直播',
      '平台账号',
      '连接',
      '日志',
      '监控',
      '设置',
    ])
    // 英文副标签与导航同序（双语信息密度是控制台方向的签名）
    expect(links.map((a) => a.find('.nav-en').text())).toEqual([
      'OVERVIEW', 'TASKS', 'HISTORY', 'MEDIA', 'SUBS', 'LIVE', 'ACCOUNTS', 'CHANNELS', 'LOGS', 'MONITOR', 'SETTINGS',
    ])
  })

  it('底部栏 5 个高频入口 + 「更多」，更多面板里是其余 6 项', async () => {
    const w = await mountApp()
    const tabs = w.findAll('.tabbar a')
    expect(tabs.map((a) => a.text())).toEqual(['主页', '任务', '历史', '文件', '设置'])

    // 面板未打开时不渲染任何「更多」项
    expect(w.findAll('.more-item').length).toBe(0)

    await w.find('.tabbar-more').trigger('click')
    expect(w.findAll('.more-item').map((a) => a.text())).toEqual([
      '订阅',
      '直播',
      '平台账号',
      '连接',
      '日志',
      '监控',
    ])

    // 再点一次收起（按钮要有可逆的开关语义）
    await w.find('.tabbar-more').trigger('click')
    expect(w.findAll('.more-item').length).toBe(0)
  })

  it('「更多」面板按 Esc 关闭（零成本退出路径）', async () => {
    const w = await mountApp()
    await w.find('.tabbar-more').trigger('click')
    expect(w.find('.more-sheet').exists()).toBe(true)
    press('Escape')
    await flushPromises()
    expect(w.find('.more-sheet').exists()).toBe(false)
  })

  // R5：`/` 与 G 和弦必须在**每个页面**都能用，不是只有下载页
  it.each(['/', '/tasks', '/history', '/media', '/subscriptions', '/settings'])(
    '%s 页按 `/` 都能把焦点送进顶栏快速提交框',
    async (path) => {
      const { wrapper, router } = mountViewWithRouter(App, ROUTES)
      await flushPromises()
      await router.push(path)
      await flushPromises()

      press('/')
      await flushPromises()
      expect(document.activeElement?.id).toBe('quick-submit')
      wrapper.unmount()
    },
  )

  it('G D 跳下载页、G S 跳设置页（和弦键）', async () => {
    const { wrapper, router } = mountViewWithRouter(App, ROUTES)
    await flushPromises()

    press('g')
    press('d')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/tasks')

    press('g')
    press('s')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/settings')
    wrapper.unmount()
  })

  // 深色主题必须挂在 <html> 上：RDialog / RToast / DetailDrawer 都是 <Teleport to="body">，
  // 浮层 DOM 是 .app 的兄弟节点。属性只写在 .app 上时 [data-theme='dark'] 匹配不到它们，
  // 深色模式下对话框 / 抽屉 / Toast 会整套退回 :root 的浅色 token（白卡片浮在深色界面上）。
  it('主题属性写在 <html> 上，Teleport 浮层才能继承主题 token', async () => {
    // 四套浣熊主题（docs/07 §2）：默认 night
    localStorage.setItem('raccoon.theme', 'night')
    const w = await mountApp()
    expect(document.documentElement.getAttribute('data-theme')).toBe('night')
    // .app 自身不再重复挂属性：留着会让人误以为浮层也在它的作用域内
    expect(w.find('.app').attributes('data-theme')).toBeUndefined()

    // 切换主题：属性跟着走（主题按钮 2026-10-24 挪至顶栏）
    await w.find('.theme-topbar-btn').trigger('click')
    await flushPromises()
    const items = w.findAll('.theme-item')
    expect(items.length).toBe(4)
    await items[2].trigger('click') // snow
    await flushPromises()
    expect(document.documentElement.getAttribute('data-theme')).toBe('snow')

    // 卸载不留状态
    w.unmount()
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false)
  })

  it('旧主题值迁移：dark→night、light→snow（升级零漂移）', async () => {
    localStorage.setItem('raccoon.theme', 'dark')
    const w = await mountApp()
    expect(document.documentElement.getAttribute('data-theme')).toBe('night')
    w.unmount()

    localStorage.setItem('raccoon.theme', 'light')
    const w2 = await mountApp()
    expect(document.documentElement.getAttribute('data-theme')).toBe('snow')
    w2.unmount()
  })

  it('在提交框里打字时 `/` 不被劫持（输入框内一律让行）', async () => {
    const w = await mountApp()
    const input = w.find('#quick-submit')
    // 必须真的 focus：trigger('focus') 只是派发一个 FocusEvent，jsdom 不会
    // 因此移动 document.activeElement，断言就变成「自证 focus 过」的空转。
    ;(input.element as HTMLInputElement).focus()
    const ev = new KeyboardEvent('keydown', { key: '/', bubbles: true })
    input.element.dispatchEvent(ev)
    await flushPromises()
    // 焦点仍在输入框里，且事件没有被 preventDefault（字符能正常输入）
    expect(document.activeElement?.id).toBe('quick-submit')
    expect(ev.defaultPrevented).toBe(false)
  })
})
