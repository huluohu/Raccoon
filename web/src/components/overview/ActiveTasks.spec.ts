import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ActiveTasks from '@/components/overview/ActiveTasks.vue'
import { makeTask } from '@/test/fixtures'

/**
 * ActiveTasks 用例（docs/07 §3 总览页关键组件）。
 *
 * 关键验收点：
 * 1) 组件只吃 props 不发请求——状态来源唯一是 SSE + /tasks 对账（§7 铁律）。
 * 2) 空态表情按状态取用（§5）：干完过 → done；纯粹在等 → waiting。
 * 3) 加载中显示骨架屏而不是空态（§7 空态/加载/错误三分）。
 */
const stubs = {
  StatusDot: { props: ['status'], template: '<i class="stub-dot" :data-status="status" />' },
  PlatformBadge: { props: ['platform'], template: '<i class="stub-badge" :data-platform="platform" />' },
  TaskProgress: { props: ['progress'], template: '<i class="stub-prog" />' },
  RaccoonFace: { props: ['face'], template: '<i class="stub-face" :data-face="face" />' },
  RButton: { template: '<button><slot /></button>' },
}

describe('ActiveTasks 组件', () => {
  it('渲染每个进行中任务：标题 + 状态点 + 平台徽章', () => {
    const w = mount(ActiveTasks, {
      props: {
        tasks: [
          makeTask({ id: 't-1', title: '第一集', status: 'downloading' }),
          makeTask({ id: 't-2', title: '第二集', status: 'waiting', platform: 'youtube' }),
        ],
      },
      global: { stubs },
    })
    expect(w.text()).toContain('第一集')
    expect(w.text()).toContain('第二集')
    expect(w.findAll('.stub-dot').length).toBe(2)
    expect(w.findAll('.stub-badge').length).toBe(2)
  })

  it('空态：没干完过用 waiting 表情（§5 按状态取用）', () => {
    const w = mount(ActiveTasks, {
      props: { tasks: [], hasDone: false },
      global: { stubs },
    })
    expect(w.find('.stub-face').attributes('data-face')).toBe('waiting')
    expect(w.text()).toContain('眼罩搁下了')
  })

  it('空态：干完过用 done 表情（§5 按状态取用）', () => {
    const w = mount(ActiveTasks, {
      props: { tasks: [], hasDone: true },
      global: { stubs },
    })
    expect(w.find('.stub-face').attributes('data-face')).toBe('done')
    expect(w.text()).toContain('活都搬完了')
  })

  it('加载中显示骨架屏而不是空态（§7 空态/加载/错误三分）', () => {
    const w = mount(ActiveTasks, {
      props: { tasks: [], loading: true },
      global: { stubs },
    })
    // 加载中不该出现空态表情
    expect(w.find('.stub-face').exists()).toBe(false)
  })

  it('「查看全部」抛 more 事件', async () => {
    const w = mount(ActiveTasks, {
      props: { tasks: [makeTask()] },
      global: { stubs },
    })
    const btn = w.findAll('button').find((b) => b.text().includes('查看全部'))!
    await btn.trigger('click')
    expect(w.emitted('more')).toBeTruthy()
  })

  it('空态时「查看全部」仍指向下载页（那是开工入口，不是噪音）', () => {
    const w = mount(ActiveTasks, {
      props: { tasks: [] },
      global: { stubs },
    })
    expect(w.findAll('button').some((b) => b.text().includes('查看全部'))).toBe(true)
  })
})
