import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { mountViewWithRouter } from '@/test/mount'
import { useAuthStore } from '@/stores/auth'
import Login from '@/views/Login.vue'

/**
 * Login.vue 用例（docs/07 §3：/login | 登录 | ——）。
 * 用真实 pinia store，只把 phase 拨到目标值——组件逻辑与生产一致，不打桩。
 *
 * 注意：mountViewWithRouter 会新建 pinia，所以必须先挂载、再拨 phase
 * （早年在这儿栽过：先拨 phase 再挂载，改的是下一秒就被丢弃的那个 pinia）。
 *
 * 首次初始化不在本页：走 /setup 的向导（components/auth/SetupWizard.vue）。
 * 本机记忆：保存用户名走 localStorage raccoon.username；30 天免登录走 remember 参数。
 */
vi.mock('@/api/client', () => ({
  api: {
    authLogin: vi.fn(async () => ({ user: { id: 'u1', username: 'admin' }, csrf_token: 'csrf' })),
    authSetup: vi.fn(async () => ({ user: { id: 'u1', username: 'admin' }, csrf_token: 'csrf' })),
  },
  setCsrfToken: vi.fn(),
  getCsrfToken: vi.fn(() => ''),
}))

async function mountLogin(phase: 'setup' | 'login', routes: { path: string; component: unknown }[] = []) {
  const { wrapper, router } = mountViewWithRouter(Login as never, routes as never, {
    global: {
      stubs: {
        RaccoonLogo: { template: '<i class="stub-logo" />' },
        // 必须保留 type="submit"：否则点击 stub 不会触发表单 submit，submit() 根本不跑。
        RButton: { template: '<button type="submit" :disabled="disabled"><slot /></button>' },
      },
    },
  })
  useAuthStore().phase = phase
  await flushPromises()
  return { wrapper, router }
}

describe('Login 视图', () => {
  beforeEach(() => localStorage.clear())
  afterEach(() => localStorage.clear())

  it('登录页是两张表单 + 两个勾选项（无确认密码）', async () => {
    const { wrapper: w } = await mountLogin('login')
    expect(w.find('.stub-logo').exists()).toBe(true)
    // 文本输入恰好两张：用户名 + 密码（勾选项是 checkbox，不算表单字段）。
    expect(w.findAll('input[type="text"], input[type="password"]').length).toBe(2)
    expect(w.findAll('input[type="checkbox"]').length).toBe(2)
  })

  it('登录页不含初始化专属的确认口令字段（那属于 /setup 向导）', async () => {
    const { wrapper: w } = await mountLogin('login')
    expect(w.text()).not.toContain('确认口令')
    expect(w.text()).not.toContain('创建账号并进入')
  })

  it('品牌区用 RaccoonLogo 矢量徽标而不是 emoji（docs/07 §4/§5）', async () => {
    const { wrapper: w } = await mountLogin('login')
    expect(w.find('.stub-logo').exists()).toBe(true)
    expect(w.html()).not.toContain('🦝')
  })

  it('空表单不进入提交流程', async () => {
    const { wrapper: w } = await mountLogin('login')
    await w.find('button').trigger('click')
    expect(w.find('.auth').exists()).toBe(true)
  })

  it('登录成功后离开 /login 路由（否则外壳会把登录页再渲染一遍）', async () => {
    const { api } = await import('@/api/client')
    const { wrapper: w, router } = await mountLogin('login', [
      { path: '/login', component: Login as never },
      { path: '/', component: { template: '<div>overview</div>' } as never },
    ])
    await router.push('/login')
    await flushPromises()

    await w.findAll('input')[0].setValue('admin')
    await w.findAll('input')[1].setValue('Hlb27Xlh')
    await w.find('form').trigger('submit')
    await flushPromises()

    expect(vi.mocked(api.authLogin)).toHaveBeenCalled()
    // 关键断言：登录成功后不停在 auth 路由上。
    // /login 的路由组件除了会话 gate 还会被外壳 router-view 渲染，停在这儿不放
    // 就是「侧栏 + 顶栏 + 中间浮一张登录卡」的同屏重复（实测复现过）。
    expect(router.currentRoute.value.path).toBe('/')
  })

  it('两个勾选默认全开；登录成功把用户名与 remember 一起交给后端/localStorage', async () => {
    const { api } = await import('@/api/client')
    const { wrapper: w, router } = await mountLogin('login', [
      { path: '/login', component: Login as never },
      { path: '/', component: { template: '<div>overview</div>' } as never },
    ])
    await router.push('/login')
    await flushPromises()

    expect((w.find('[data-test="save-username"]').element as HTMLInputElement).checked).toBe(true)
    expect((w.find('[data-test="stay-logged-in"]').element as HTMLInputElement).checked).toBe(true)

    await w.findAll('input[type="text"]')[0].setValue('admin')
    await w.findAll('input[type="password"]')[0].setValue('Hlb27Xlh')
    await w.find('form').trigger('submit')
    await flushPromises()

    expect(vi.mocked(api.authLogin)).toHaveBeenCalledWith('admin', 'Hlb27Xlh', true)
    expect(localStorage.getItem('raccoon.username')).toBe('admin')
  })

  it('取消「保存用户名」：登录成功后清掉已存的用户名', async () => {
    localStorage.setItem('raccoon.username', 'admin')
    const { wrapper: w, router } = await mountLogin('login', [
      { path: '/login', component: Login as never },
      { path: '/', component: { template: '<div>overview</div>' } as never },
    ])
    await router.push('/login')
    await flushPromises()

    await w.find('[data-test="save-username"]').setValue(false)
    await w.findAll('input[type="text"]')[0].setValue('admin')
    await w.findAll('input[type="password"]')[0].setValue('Hlb27Xlh')
    await w.find('form').trigger('submit')
    await flushPromises()

    expect(localStorage.getItem('raccoon.username')).toBeNull()
  })

  it('取消「30 天内免登录」：remember=false 交给后端（24h 会话 + 浏览器会话 Cookie）', async () => {
    const { api } = await import('@/api/client')
    const { wrapper: w, router } = await mountLogin('login', [
      { path: '/login', component: Login as never },
      { path: '/', component: { template: '<div>overview</div>' } as never },
    ])
    await router.push('/login')
    await flushPromises()

    await w.find('[data-test="stay-logged-in"]').setValue(false)
    await w.findAll('input[type="text"]')[0].setValue('admin')
    await w.findAll('input[type="password"]')[0].setValue('Hlb27Xlh')
    await w.find('form').trigger('submit')
    await flushPromises()

    expect(vi.mocked(api.authLogin)).toHaveBeenCalledWith('admin', 'Hlb27Xlh', false)
  })

  it('已保存过用户名：进登录页自动预填', async () => {
    localStorage.setItem('raccoon.username', 'gulaotou')
    const { wrapper: w } = await mountLogin('login')
    expect((w.findAll('input[type="text"]')[0].element as HTMLInputElement).value).toBe('gulaotou')
  })
})
