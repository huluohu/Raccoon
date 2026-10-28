import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'

import TaskProgress from '@/components/task/TaskProgress.vue'
import type { Task } from '@/api/types'

/**
 * TaskProgress 用例（docs/07 §4：进度条三段配色 + 端点爪印）。
 *
 * 关键验收点（2026-10-28 用户截图裁决）：
 * **完成态必须是成功绿**。之前 done/failed/cancelled 全落进 idle，
 * 而 idle 用 --text-3（弱化文字色）——100% 宽的灰蓝条看着像「还没开始下载」。
 * 端点爪印也要跟着状态变（PawSlider 的爪印曾写死 --accent）。
 */
function makeTask(over: Partial<Task> = {}): Task {
  return {
    id: 't1',
    url: 'https://example.com/v',
    status: 'downloading',
    progress: 40,
    created_at: 1_700_000_000_000,
    ...over,
  } as Task
}

describe('TaskProgress 组件', () => {
  it('下载中用强调色（--accent）', () => {
    const w = mount(TaskProgress, { props: { task: makeTask({ status: 'downloading', progress: 40 }) } })
    expect(w.classes()).toContain('tprog--download')
  })

  it('解析中用信息色（--info）', () => {
    const w = mount(TaskProgress, { props: { task: makeTask({ status: 'parsing' }) } })
    expect(w.classes()).toContain('tprog--parse')
  })

  it('已完成是独立终态（不再是 idle），进度按 100% 计', () => {
    const w = mount(TaskProgress, { props: { task: makeTask({ status: 'done', progress: 12 }) } })
    expect(w.classes()).toContain('tprog--done')
    expect(w.classes()).not.toContain('tprog--idle')
    // 完成态的进度数值应是 100%（旧实现只在 done 时强制 100，保留）
    expect(w.text()).toContain('100%')
  })

  it('失败与取消各自成终态，不落回 idle', () => {
    const failed = mount(TaskProgress, { props: { task: makeTask({ status: 'failed' }) } })
    expect(failed.classes()).toContain('tprog--failed')
    const cancelled = mount(TaskProgress, { props: { task: makeTask({ status: 'cancelled' }) } })
    expect(cancelled.classes()).toContain('tprog--cancelled')
  })

  it('等待中仍是 idle（弱化灰），不与终态混淆', () => {
    const w = mount(TaskProgress, { props: { task: makeTask({ status: 'waiting' }) } })
    expect(w.classes()).toContain('tprog--idle')
  })

  it('端点爪印跟随终态颜色（done=success / failed=danger）', () => {
    const done = mount(TaskProgress, { props: { task: makeTask({ status: 'done' }) } })
    expect(done.find('.paw-thumb').exists()).toBe(true)
    const doneStyle = done.find('.paw').attributes('style') ?? ''
    expect(doneStyle).toContain('var(--success)')

    const failed = mount(TaskProgress, { props: { task: makeTask({ status: 'failed' }) } })
    const failedStyle = failed.find('.paw').attributes('style') ?? ''
    expect(failedStyle).toContain('var(--danger)')
  })

  it('后处理是不确定进度（条纹 + 文字说明），不编造百分比', () => {
    const w = mount(TaskProgress, { props: { task: makeTask({ status: 'post' }) } })
    expect(w.classes()).toContain('tprog--post')
    expect(w.text()).toContain('后处理中')
    expect(w.find('.tprog-bar--indeterminate').exists()).toBe(true)
  })
})
