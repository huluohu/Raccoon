import { describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { mountViewWithRouter } from '@/test/mount'
import { useAuthStore } from '@/stores/auth'
import Login from '@/views/Login.vue'

/**
 * Login.vue 用例（docs/07 §3：/login 与 /setup 共用本页，按 auth.phase 切换）。
 * 用真实 pinia store，只把 phase 拨到目标值——组件逻辑与生产一致，不打桩。
 *
 * 注意：mountViewWithRouter 会新建 pinia，所以必须先挂载、再拨 phase
 * （早年在这儿栽过：先拨 phase 再挂载，改的是下一秒就被丢弃的那个 pinia）。
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
        RaccoonFace: { template: '<i class="stub-face" />' },
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
  it('未初始化时是「创建管理员账号」三表单（含确认密码）', async () => {
    const { wrapper: w } = await mountLogin('setup')
    expect(w.text()).toContain('首次使用：创建管理员账号')
    expect(w.findAll('input').length).toBe(3)
  })

  it('已初始化时是登录两表单（无确认密码）', async () => {
    const { wrapper: w } = await mountLogin('login')
    expect(w.text()).toContain('请登录以继续')
    expect(w.findAll('input').length).toBe(2)
  })

  it('品牌区用 RaccoonFace 而不是 emoji（docs/07 §4/§5）', async () => {
    const { wrapper: w } = await mountLogin('login')
    expect(w.find('.stub-face').exists()).toBe(true)
    expect(w.html()).not.toContain('🦝')
  })

  it('初始化态两次密码不一致时给出明确错误，不静默提交', async () => {
    const { wrapper: w } = await mountLogin('setup')
    const inputs = w.findAll('input')
    await inputs[0].setValue('admin')
    await inputs[1].setValue('password123')
    await inputs[2].setValue('password456')
    // jsdom 不保证点 submit 按钮会派发表单 submit，直接触发 form 的 submit 事件
    // （组件监听的就是 @submit.prevent）。
    await w.find('form').trigger('submit')
    expect(w.find('.error').text()).toBe('两次输入的密码不一致')
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
})
