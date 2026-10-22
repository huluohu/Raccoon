import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import TaskDetail from '@/components/task/TaskDetail.vue'
import { makeEvent, makeFile, makeTask } from '@/test/fixtures'

/**
 * TaskDetail 用例（docs/07 §4：概览/进度/日志/错误与建议/操作五区；
 * Cookie 过期 → 一键跳 connections）。
 */
describe('TaskDetail', () => {
  const stubs = {
    StatusDot: { template: '<i class="stub-dot" />' },
    TaskProgress: { template: '<div class="stub-prog" />' },
    PlatformBadge: { template: '<i class="stub-badge" />' },
    RButton: { template: '<button @click="$emit(\'click\')"><slot /></button>' },
  }

  it('五个区块齐全（概览 / 进度 / 日志 / 错误与建议 / 操作）', () => {
    const w = mount(TaskDetail, { props: { task: makeTask() }, global: { stubs } })
    const titles = w.findAll('.td-sec-title').map((n) => n.text())
    expect(titles).toEqual(['概览', '进度', '日志', '错误与建议', '操作'])
  })

  it('事件日志按序渲染，空日志有空态', () => {
    const w = mount(TaskDetail, {
      props: { task: makeTask(), events: [makeEvent({ id: 1, message: '第一条' }), makeEvent({ id: 2, message: '第二条' })] },
      global: { stubs },
    })
    const msgs = w.findAll('.td-log-msg').map((n) => n.text())
    expect(msgs).toEqual(['第一条', '第二条'])

    const empty = mount(TaskDetail, { props: { task: makeTask(), events: [] }, global: { stubs } })
    expect(empty.find('.td-logs-empty').exists()).toBe(true)
  })

  it('产物文件列表渲染文件名与大小', () => {
    const w = mount(TaskDetail, {
      props: { task: makeTask(), files: [makeFile()] },
      global: { stubs },
    })
    expect(w.find('.td-file-name').text()).toContain('测试视频.mp4')
  })

  it('凭据类错误给出「一键跳 connections」入口并 emit goto-connections', async () => {
    // needsCredentials 只认三类：fix_hint.action=goto_credentials / error_code=AUTH_REQUIRED /
    // waiting + wait_reason=need_login。这里用 AUTH_REQUIRED。
    const w = mount(TaskDetail, {
      props: { task: makeTask({ status: 'failed', error_code: 'AUTH_REQUIRED', error_message: '需要登录' }) },
      global: { stubs },
    })
    const jump = w.findAll('button').find((b) => b.text().includes('连接'))
    expect(jump).toBeTruthy()
    await jump!.trigger('click')
    expect(w.emitted('goto-connections')).toBeTruthy()
  })

  it('非凭据类错误不给跳转入口（不拿无关按钮骚扰用户）', () => {
    const w = mount(TaskDetail, {
      props: { task: makeTask({ status: 'failed', error_code: 'HTTP_403', error_message: '对方不放' }) },
      global: { stubs },
    })
    expect(w.findAll('button').some((b) => b.text().includes('连接'))).toBe(false)
  })

  it('引擎不可用错误给「去设置页检查引擎」入口', () => {
    const w = mount(TaskDetail, {
      props: { task: makeTask({ status: 'failed', error_code: 'ENGINE_UNAVAILABLE', error_message: 'yt-dlp 缺失' }) },
      global: { stubs },
    })
    const jump = w.findAll('button').find((b) => b.text().includes('设置'))
    expect(jump).toBeTruthy()
  })

  it('可重试态（failed）触发 retry 并带上任务', async () => {
    const w = mount(TaskDetail, {
      props: { task: makeTask({ status: 'failed', error_code: 'HTTP_403', error_message: '对方不放' }) },
      global: { stubs },
    })
    const retry = w.findAll('button').find((b) => b.text().includes('重试'))
    expect(retry).toBeTruthy()
    await retry!.trigger('click')
    expect(w.emitted('retry')?.[0]?.[0]).toMatchObject({ id: 't-1' })
  })

  it('下载中态可取消', async () => {
    const w = mount(TaskDetail, { props: { task: makeTask({ status: 'downloading' }) }, global: { stubs } })
    const cancel = w.findAll('button').find((b) => b.text().includes('取消'))
    expect(cancel).toBeTruthy()
    await cancel!.trigger('click')
    expect(w.emitted('cancel')).toBeTruthy()
  })

  it('busy 时操作按钮禁用（防重复提交）', () => {
    const w = mount(TaskDetail, {
      props: { task: makeTask({ status: 'downloading' }), busy: true },
      global: { stubs },
    })
    const cancel = w.findAll('button').find((b) => b.text().includes('取消'))
    expect(cancel!.attributes('disabled')).toBeDefined()
  })

  it('无错误时不显示错误区内容，而是明说「本次没有错误记录」', () => {
    const w = mount(TaskDetail, { props: { task: makeTask({ status: 'done', progress: 100 }) }, global: { stubs } })
    expect(w.text()).toContain('本次没有错误记录')
  })
})
