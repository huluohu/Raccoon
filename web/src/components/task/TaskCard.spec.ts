import { describe, expect, it } from 'vitest'

import TaskCard from '@/components/task/TaskCard.vue'
import { makeTask } from '@/test/fixtures'
import { mount } from '@/test/mount'

/**
 * TaskCard 用例（docs/07 §4：状态徽章 + 迷你进度 + 速度/大小/平台徽章；父任务子计数）。
 */
describe('TaskCard', () => {
  const stubs = {
    StatusDot: { template: '<i class="stub-dot" />' },
    TaskProgress: { template: '<div class="stub-prog" />' },
    PlatformBadge: { template: '<i class="stub-badge" />' },
    RButton: { template: '<button><slot /></button>' },
  }

  it('展示状态/进度/平台/速度/大小徽章', () => {
    const w = mount(TaskCard, { props: { task: makeTask() }, global: { stubs } })
    expect(w.find('.stub-dot').exists()).toBe(true)
    expect(w.find('.stub-prog').exists()).toBe(true)
    expect(w.find('.stub-badge').exists()).toBe(true)
    expect(w.text()).toContain('测试视频 · 第 1 集')
  })

  it('父任务显示子项聚合计数「已完成 X/Y」', () => {
    const w = mount(TaskCard, {
      props: { task: makeTask({ platform: 'batch' }), parent: true, childStats: { total: 7, failed: 1, done: 3 } },
      global: { stubs },
    })
    expect(w.text()).toContain('已完成 3/7')
    expect(w.text()).toContain('失败 1')
  })

  it('未传 done 时只显示总数，不谎报 0/N', () => {
    const w = mount(TaskCard, {
      props: { task: makeTask({ platform: 'batch' }), parent: true, childStats: { total: 7, failed: 0 } },
      global: { stubs },
    })
    expect(w.text()).toContain('共 7 项')
    expect(w.text()).not.toContain('已完成 0/7')
  })

  it('失败子项 >0 时才渲染 retry-failed 插槽', () => {
    const withFailed = mount(TaskCard, {
      props: { task: makeTask({ platform: 'batch' }), parent: true, childStats: { total: 5, failed: 2 } },
      slots: { 'retry-failed': '<button class="retry-btn">重试失败项</button>' },
      global: { stubs },
    })
    expect(withFailed.find('.retry-btn').exists()).toBe(true)

    const noFailed = mount(TaskCard, {
      props: { task: makeTask({ platform: 'batch' }), parent: true, childStats: { total: 5, failed: 0 } },
      slots: { 'retry-failed': '<button class="retry-btn">重试失败项</button>' },
      global: { stubs },
    })
    expect(noFailed.find('.retry-btn').exists()).toBe(false)
  })

  it('等待原因透传（waiting + 合法 wait_reason）', () => {
    // wait_reason 合法值见 utils/format.ts WAIT_REASON_TEXT：auto_retry/interrupted/
    // worker_panic/need_login/disk_check。写不存在的值会静默不展示——这正是要防的。
    const w = mount(TaskCard, {
      props: { task: makeTask({ status: 'waiting', wait_reason: 'auto_retry' }) },
      global: { stubs },
    })
    expect(w.find('.tcard-wait').exists()).toBe(true)
    expect(w.find('.tcard-wait').text()).toBe('等待自动重试')
  })

  it('选中态与 open/select 事件（选择框只改选中，主体区域才开详情）', async () => {
    const w = mount(TaskCard, { props: { task: makeTask(), selected: true }, global: { stubs } })
    expect(w.classes()).toContain('is-selected')

    // 第一个按钮是选择框：点了只 emit select，不 emit open（防误触丢焦点）。
    await w.find('.tcard-pick').trigger('click')
    expect(w.emitted('select')).toBeTruthy()
    expect(w.emitted('open')).toBeFalsy()

    // 主体区域（role=button）才开详情。
    await w.find('[role="button"]').trigger('click')
    expect(w.emitted('open')).toBeTruthy()
  })

  it('readonly：操作区整块不渲染，父级传了 actions 也不渲染', () => {
    // 只读页上出现的按钮都该能用；留着点了没反应的删除，比没有按钮更让人困惑。
    const w = mount(TaskCard, {
      props: { task: makeTask(), readonly: true },
      slots: { actions: '<button class="op-del">删除</button>' },
      global: { stubs },
    })
    expect(w.find('.tcard-ops').exists()).toBe(false)
    expect(w.find('.op-del').exists()).toBe(false)
    // 多选框只为批量操作服务，只读页一并隐去
    expect(w.find('.tcard-pick').exists()).toBe(false)
    // 数据行照旧
    expect(w.text()).toContain('测试视频 · 第 1 集')
  })
})
