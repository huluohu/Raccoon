import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from '@/stores/auth'
import Login from '@/views/Login.vue'

/**
 * Login.vue 用例（docs/07 §3：/login 与 /setup 共用本页，按 auth.phase 切换）。
 * 用真实 pinia store，只把 phase 拨到目标值——组件逻辑与生产一致，不打桩。
 */
function mountLogin(phase: 'setup' | 'login') {
  setActivePinia(createPinia())
  const auth = useAuthStore()
  auth.phase = phase
  return mount(Login, {
    global: {
      stubs: {
        RaccoonFace: { template: '<i class="stub-face" />' },
        // 必须保留 type="submit"：否则点击 stub 不会触发表单 submit，submit() 根本不跑。
        RButton: { template: '<button type="submit" :disabled="disabled"><slot /></button>' },
      },
    },
  })
}

describe('Login 视图', () => {
  it('未初始化时是「创建管理员账号」三表单（含确认密码）', () => {
    const w = mountLogin('setup')
    expect(w.text()).toContain('首次使用：创建管理员账号')
    expect(w.findAll('input').length).toBe(3)
  })

  it('已初始化时是登录两表单（无确认密码）', () => {
    const w = mountLogin('login')
    expect(w.text()).toContain('请登录以继续')
    expect(w.findAll('input').length).toBe(2)
  })

  it('品牌区用 RaccoonFace 而不是 emoji（docs/07 §4/§5）', () => {
    const w = mountLogin('login')
    expect(w.find('.stub-face').exists()).toBe(true)
    expect(w.html()).not.toContain('🦝')
  })

  it('初始化态两次密码不一致时给出明确错误，不静默提交', async () => {
    const w = mountLogin('setup')
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
    const w = mountLogin('login')
    await w.find('button').trigger('click')
    expect(w.find('.auth').exists()).toBe(true)
  })
})
