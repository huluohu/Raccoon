import { describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { mountView } from '@/test/mount'
import Setup from '@/views/Setup.vue'

/**
 * Setup.vue 用例（docs/07 §3「/setup | 初始化管理员 | 向导表单」）。
 *
 * 这个视图只是向导的宿主：断言它确实渲染的是向导（而不是登录表单）。
 */
vi.mock('@/api/client', () => ({
  api: {
    authSetup: vi.fn(async () => ({ user: { id: 'u1', username: 'admin' }, csrf_token: 'csrf' })),
  },
  setCsrfToken: vi.fn(),
  getCsrfToken: vi.fn(() => ''),
}))

describe('Setup 视图', () => {
  it('渲染初始化向导（分步表单），不是登录表单', async () => {
    const w = mountView(Setup, [], {
      global: { stubs: { RaccoonFace: { template: '<i class="stub-face" />' } } },
    })
    await flushPromises()

    expect(w.text()).toContain('首次使用')
    expect(w.text()).toContain('第 1 / 2 步')
    // 向导有确认口令（登录没有）
    expect(w.findAll('input').length).toBe(3)
  })
})
