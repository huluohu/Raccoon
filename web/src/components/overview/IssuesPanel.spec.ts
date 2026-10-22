import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import IssuesPanel from '@/components/overview/IssuesPanel.vue'
import { makeTask } from '@/test/fixtures'

/**
 * IssuesPanel 用例（docs/07 §3 总览页关键组件）。
 *
 * 关键验收点：
 * 1) 失败原因要带「错误码 + 原文」两件套（§5 讲事实 + 给下一步）。
 * 2) 表情固定用 failed（§5 按状态取用）。
 * 3) 没有失败任务时不占位（不留一张空卡）。
 * 4) 重试抛的是任务 id，父组件据此决定重试哪条。
 */
const stubs = {
  RaccoonFace: { props: ['face'], template: '<i class="stub-face" :data-face="face" />' },
  RButton: { template: '<button @click="$emit(\'click\')"><slot /></button>' },
}

describe('IssuesPanel 组件', () => {
  it('渲染失败任务：标题 + 错误码 + 错误原文', () => {
    const w = mount(IssuesPanel, {
      props: {
        tasks: [
          makeTask({
            id: 't-1',
            title: '下不下来的那个',
            status: 'failed',
            error_code: 'HTTP_403',
            error_message: '对方不放（403）',
          }),
        ],
      },
      global: { stubs },
    })
    expect(w.text()).toContain('下不下来的那个')
    expect(w.text()).toContain('HTTP_403')
    expect(w.text()).toContain('对方不放（403）')
  })

  it('失败一律用 failed 表情（§5）', () => {
    const w = mount(IssuesPanel, {
      props: { tasks: [makeTask({ status: 'failed', error_code: 'E', error_message: 'm' })] },
      global: { stubs },
    })
    expect(w.find('.stub-face').attributes('data-face')).toBe('failed')
  })

  it('没有标题时退回显示 URL（别渲染空白）', () => {
    const w = mount(IssuesPanel, {
      props: {
        tasks: [
          makeTask({ title: '', url: 'https://example.com/x', status: 'failed', error_code: 'E', error_message: 'm' }),
        ],
      },
      global: { stubs },
    })
    expect(w.text()).toContain('https://example.com/x')
  })

  it('没有失败任务时整块不渲染（不留空卡）', () => {
    const w = mount(IssuesPanel, { props: { tasks: [] }, global: { stubs } })
    expect(w.find('.issue-panel').exists()).toBe(false)
  })

  it('点重试抛出对应任务 id', async () => {
    const w = mount(IssuesPanel, {
      props: {
        tasks: [
          makeTask({ id: 't-1', status: 'failed', error_code: 'E', error_message: 'm' }),
          makeTask({ id: 't-2', status: 'failed', error_code: 'E', error_message: 'm' }),
        ],
      },
      global: { stubs },
    })
    const retryBtns = w.findAll('button').filter((b) => b.text().includes('重试'))
    await retryBtns[1].trigger('click')
    expect(w.emitted('retry')![0]).toEqual(['t-2'])
  })

  it('「全部任务」抛 more 事件', async () => {
    const w = mount(IssuesPanel, {
      props: { tasks: [makeTask({ status: 'failed', error_code: 'E', error_message: 'm' })] },
      global: { stubs },
    })
    const btn = w.findAll('button').find((b) => b.text().includes('全部任务'))!
    await btn.trigger('click')
    expect(w.emitted('more')).toBeTruthy()
  })
})
