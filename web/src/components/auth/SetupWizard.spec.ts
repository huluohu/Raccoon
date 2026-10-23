import { describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { mountView } from '@/test/mount'
import SetupWizard from '@/components/auth/SetupWizard.vue'
import { useAuthStore } from '@/stores/auth'

/**
 * SetupWizard 用例（docs/07 §3「/setup | 初始化管理员 | 向导表单」）。
 *
 * 关键验收点：
 * 1) 分两步：填账号 → 复核确认。第一步不合法不许进第二步。
 * 2) 校验在前端当场做（用户名规则、口令长度、两次一致），不发请求。
 * 3) 确认页要说明「以后去哪改」——初始化是唯一一次从零开始的时刻。
 * 4) 创建失败要回到第一步改，不能把人卡在确认页上。
 */
vi.mock('@/api/client', () => ({
  api: {
    authSetup: vi.fn(async () => ({ user: { id: 'u1', username: 'admin' }, csrf_token: 'csrf' })),
  },
  setCsrfToken: vi.fn(),
  getCsrfToken: vi.fn(() => ''),
}))

const stubs = {
  RaccoonLogo: { template: '<i class="stub-logo" />' },
  // 必须保留 type="submit"：否则点击 stub 不会触发表单 submit。
  RButton: { template: '<button type="submit" :disabled="disabled"><slot /></button>' },
}

function mountWizard() {
  // mountView 会建好 pinia + 空路由（组件里用了 auth store 与 router）。
  return mountView(SetupWizard, [], { global: { stubs } })
}

describe('SetupWizard 组件', () => {
  it('初始是第 1 步「账号信息」，有三个输入框', () => {
    const w = mountWizard()
    expect(w.find('.step.on').text()).toContain('① 账号信息')
    expect(w.findAll('input').length).toBe(3)
  })

  it('步骤指示标明当前进度（不只靠颜色）', () => {
    const w = mountWizard()
    const steps = w.findAll('.step')
    expect(steps.length).toBe(2)
    expect(steps[0].classes()).toContain('on')
    expect(steps[0].text()).toContain('账号信息')
    expect(steps[1].text()).toContain('确认创建')
  })

  it('信息不全时「下一步」不可点，并给出具体差什么', async () => {
    const w = mountWizard()
    const inputs = w.findAll('input')

    await inputs[0].setValue('a')
    await inputs[1].setValue('short')
    await inputs[2].setValue('other')
    await flushPromises()

    expect(w.find('button[type="submit"]').attributes('disabled')).toBeDefined()
    expect(w.text()).toContain('用户名长度需在 2–32 之间')
  })

  it('两次口令不一致时给出明确提示', async () => {
    const w = mountWizard()
    const inputs = w.findAll('input')
    await inputs[0].setValue('admin')
    await inputs[1].setValue('password123')
    await inputs[2].setValue('password456')
    await flushPromises()

    expect(w.text()).toContain('两次输入的口令不一致')
    expect(w.find('button[type="submit"]').attributes('disabled')).toBeDefined()
  })

  it('用户名非法字符时给出规则提示', async () => {
    const w = mountWizard()
    const inputs = w.findAll('input')
    await inputs[0].setValue('名字 带空格')
    await inputs[1].setValue('password123')
    await inputs[2].setValue('password123')
    await flushPromises()

    expect(w.text()).toContain('用户名仅限字母/数字/_/-/中文')
  })

  it('填对后进入第 2 步：复核账号并说明以后去哪改', async () => {
    const w = mountWizard()
    const inputs = w.findAll('input')
    await inputs[0].setValue('admin')
    await inputs[1].setValue('password123')
    await inputs[2].setValue('password123')
    await flushPromises()

    await w.find('form').trigger('submit')
    await flushPromises()

    expect(w.find('.step.on').text()).toContain('② 确认创建')
    expect(w.text()).toContain('admin')
    // 说明后续入口：初始化是唯一一次从零开始的时刻，得告诉人以后去哪改
    expect(w.text()).toContain('设置 → 账号')
    expect(w.text()).toContain('Argon2id')
  })

  it('第 2 步可以退回第 1 步', async () => {
    const w = mountWizard()
    const inputs = w.findAll('input')
    await inputs[0].setValue('admin')
    await inputs[1].setValue('password123')
    await inputs[2].setValue('password123')
    await flushPromises()
    await w.find('form').trigger('submit')
    await flushPromises()

    const back = w.findAll('button').find((b) => b.text().includes('上一步'))!
    await back.trigger('click')
    await flushPromises()

    expect(w.find('.step.on').text()).toContain('① 账号信息')
  })

  it('第 2 步提交才真正创建账号', async () => {
    const { api } = await import('@/api/client')
    const w = mountWizard()
    const inputs = w.findAll('input')
    await inputs[0].setValue('admin')
    await inputs[1].setValue('password123')
    await inputs[2].setValue('password123')
    await flushPromises()
    await w.find('form').trigger('submit')
    await flushPromises()

    // 第一步不该发请求
    expect(vi.mocked(api.authSetup)).not.toHaveBeenCalled()

    await w.find('form').trigger('submit')
    await flushPromises()

    expect(vi.mocked(api.authSetup)).toHaveBeenCalledWith('admin', 'password123')
  })

  it('创建失败时回到第 1 步并显示后端原因', async () => {
    const { api } = await import('@/api/client')
    vi.mocked(api.authSetup).mockRejectedValueOnce(new Error('用户名已存在'))
    const w = mountWizard()
    const inputs = w.findAll('input')
    await inputs[0].setValue('admin')
    await inputs[1].setValue('password123')
    await inputs[2].setValue('password123')
    await flushPromises()
    await w.find('form').trigger('submit')
    await flushPromises()

    await w.find('form').trigger('submit')
    await flushPromises()

    expect(w.find('.step.on').text()).toContain('① 账号信息')
    expect(w.find('.error').text()).toContain('用户名已存在')
  })

  it('创建成功后由 auth store 切到 ready（外壳据此渲染主界面）', async () => {
    const w = mountWizard()
    const inputs = w.findAll('input')
    await inputs[0].setValue('admin')
    await inputs[1].setValue('password123')
    await inputs[2].setValue('password123')
    await flushPromises()
    await w.find('form').trigger('submit')
    await flushPromises()
    await w.find('form').trigger('submit')
    await flushPromises()

    expect(useAuthStore().phase).toBe('ready')
  })

  it('品牌区用 RaccoonLogo 矢量徽标而不是 emoji（§4/§5）', () => {
    const w = mountWizard()
    expect(w.find('.stub-logo').exists()).toBe(true)
    expect(w.html()).not.toContain('🦝')
  })
})
