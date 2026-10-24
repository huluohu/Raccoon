import { describe, expect, it } from 'vitest'

import TaskDetail from '@/components/task/TaskDetail.vue'
import { makeEvent, makeFile, makeTask } from '@/test/fixtures'
import { mount } from '@/test/mount'

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

  it('点删文件只 emit delete-file，确认框交给上层（组件不自己弹窗）', async () => {
    // 详情面板是纯展示组件：不 fetch、不弹窗、不决定破坏性操作怎么做。
    // 确认框统一由上层（Tasks.vue / Media.vue 的 DeleteFileDialog）负责，
    // 这样「删文件」的文案与交互只有一处实现。
    const file = makeFile()
    const w = mount(TaskDetail, {
      props: { task: makeTask(), files: [file] },
      global: { stubs },
    })
    await w.find('.td-file-del').trigger('click')

    const emitted = w.emitted('delete-file')
    expect(emitted).toBeTruthy()
    expect((emitted![0] as unknown[])[0]).toMatchObject({ id: file.id })
    // 组件内部不能自己冒出确认框
    expect(document.body.querySelector('.rdialog-root')).toBeNull()
    w.unmount()
  })

  it('概览区展示「走了哪条路」，没有留痕时如实显示「—」', () => {
    // route_path 是后端写入的人类可读链路（docs/15 §3.7）。
    // 老数据没有这个字段，界面不能编一条看起来合理的路径。
    const withPath = mount(TaskDetail, {
      props: { task: makeTask({ route_path: '115 优先 → 115 提交中 → 转 qB' }) },
      global: { stubs },
    })
    const labels = withPath.findAll('.td-kv dt').map((n) => n.text())
    expect(labels).toContain('走了哪条路')
    expect(withPath.text()).toContain('115 优先 → 115 提交中 → 转 qB')

    const noPath = mount(TaskDetail, { props: { task: makeTask() }, global: { stubs } })
    expect(noPath.text()).toContain('走了哪条路')
    expect(noPath.find('.td-kv').text()).toContain('—')
  })

  it('活跃任务的操作用含暂停说明（引擎不支持暂停，给出替代路径）', () => {
    // R10：界面上没有暂停按钮，必须说清为什么 + 该怎么中断，否则会被当成功能缺失。
    const active = mount(TaskDetail, { props: { task: makeTask({ status: 'downloading' }) }, global: { stubs } })
    expect(active.find('.td-note').text()).toContain('不支持暂停/续传')
    expect(active.find('.td-note').text()).toContain('重新入队会保留原配置版本')

    // 已结束的任务不需要这条说明（说了反而像在暗示它还能暂停）
    const done = mount(TaskDetail, { props: { task: makeTask({ status: 'done' }) }, global: { stubs } })
    expect(done.find('.td-note').exists()).toBe(false)
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

  it('引擎原文提升到错误区：分类文案 + 原文两件套都在', () => {
    // 后端把引擎 stderr 摘要写进 event='engine_error' 的事件里（backend worker.go）。
    // 只显示 error_message 的话，用户只知道「哪类问题」，不知道「到底报了什么」。
    const w = mount(TaskDetail, {
      props: {
        task: makeTask({
          status: 'failed',
          error_code: 'FORMAT_UNAVAILABLE',
          error_message: '所选清晰度不可用，可换其他清晰度重试',
        }),
        events: [
          makeEvent({ id: 1, level: 'error', event: 'engine_error', message: 'Requested format is not available' }),
        ],
      },
      global: { stubs },
    })
    expect(w.text()).toContain('所选清晰度不可用')
    expect(w.find('.td-engine-detail').text()).toBe('Requested format is not available')
    expect(w.text()).toContain('引擎原文')
  })

  it('没有引擎原文时不留空位（不显示「引擎原文：—」）', () => {
    const w = mount(TaskDetail, {
      props: {
        task: makeTask({ status: 'failed', error_code: 'HTTP_403', error_message: '对方不放' }),
        events: [makeEvent({ id: 1, event: 'state_change', message: '普通事件' })],
      },
      global: { stubs },
    })
    expect(w.find('.td-engine-detail').exists()).toBe(false)
    expect(w.text()).not.toContain('引擎原文')
  })

  it('多条引擎原文取最后一条（最新那次尝试的报错才是当前事实）', () => {
    const w = mount(TaskDetail, {
      props: {
        task: makeTask({ status: 'failed', error_code: 'PARSE_FAILED', error_message: '解析失败' }),
        events: [
          makeEvent({ id: 1, level: 'error', event: 'engine_error', message: '第一次的报错' }),
          makeEvent({ id: 2, level: 'error', event: 'engine_error', message: '第二次的报错' }),
        ],
      },
      global: { stubs },
    })
    expect(w.find('.td-engine-detail').text()).toBe('第二次的报错')
  })

  // ---- 重试等待期（waiting + auto_retry）：错误可见 + 立即重试（docs/05 §7） ----

  it('重试等待期显示错误与倒计时结论，重试按钮是「不等了，立即重试」', () => {
    // 后端在 auto_retry 分支把 error_code/scheduled_at 落库并随 SSE 广播，
    // 前端不等 failed 终态就要能把「错在哪、还有多久重试」讲清楚。
    const w = mount(TaskDetail, {
      props: {
        task: makeTask({
          status: 'waiting',
          wait_reason: 'auto_retry',
          attempt: 1,
          max_attempts: 3,
          error_code: 'NET_TIMEOUT',
          error_message: '网络连接失败或被重置，稍后自动重试',
          scheduled_at: Date.now() + 25_000,
        }),
      },
      global: { stubs },
    })
    expect(w.text()).not.toContain('本次没有错误记录')
    // 结论讲事实：第几次没成 + 什么时候再试；语气是「在等」不是「完了」。
    expect(w.text()).toContain('第 2 次没搬成')
    expect(w.text()).toContain('秒后自动再试')
    // 还没终态化，不摆失败腔。
    expect(w.text()).not.toContain('这单没成。')
    const retry = w.findAll('button').find((b) => b.text().includes('立即重试'))
    expect(retry).toBeTruthy()
  })

  it('重试等待期点「立即重试」emit retry（跳过退避）', async () => {
    const w = mount(TaskDetail, {
      props: {
        task: makeTask({
          status: 'waiting',
          wait_reason: 'auto_retry',
          attempt: 1,
          error_code: 'NET_TIMEOUT',
          error_message: '网络连接失败或被重置',
          scheduled_at: Date.now() + 120_000,
        }),
      },
      global: { stubs },
    })
    const retry = w.findAll('button').find((b) => b.text().includes('立即重试'))
    await retry!.trigger('click')
    expect(w.emitted('retry')?.[0]?.[0]).toMatchObject({ id: 't-1' })
  })

  // ---- 换通道重试（转 115，docs/05 §7）：网络类错误才给出口，平台类错误不骚扰 ----

  it('网络类失败的本地下载任务显示「转 115 通道重试」并 emit retry-via', async () => {
    const w = mount(TaskDetail, {
      props: {
        task: makeTask({
          status: 'failed',
          kind: 'download',
          error_code: 'NET_TIMEOUT',
          error_message: '网络连接失败或被重置',
        }),
      },
      global: { stubs },
    })
    const reroute = w.findAll('button').find((b) => b.text().includes('转 115'))
    expect(reroute).toBeTruthy()
    await reroute!.trigger('click')
    const emitted = w.emitted('retry-via')
    expect(emitted).toBeTruthy()
    expect((emitted![0] as unknown[])[0]).toMatchObject({ id: 't-1' })
    expect((emitted![0] as unknown[])[1]).toBe('n115')
  })

  it('重试等待期的网络错误同样给换通道出口', () => {
    const w = mount(TaskDetail, {
      props: {
        task: makeTask({
          status: 'waiting',
          wait_reason: 'auto_retry',
          kind: 'download',
          error_code: 'HTTP_5XX',
          error_message: '对方服务临时不可用',
          scheduled_at: Date.now() + 60_000,
        }),
      },
      global: { stubs },
    })
    expect(w.findAll('button').some((b) => b.text().includes('转 115'))).toBe(true)
  })

  it('平台类错误（登录/404）不显示换通道按钮（换道无意义）', () => {
    for (const code of ['AUTH_REQUIRED', 'NOT_FOUND', 'FORMAT_UNAVAILABLE']) {
      const w = mount(TaskDetail, {
        props: {
          task: makeTask({ status: 'failed', kind: 'download', error_code: code, error_message: 'x' }),
        },
        global: { stubs },
      })
      expect(w.findAll('button').some((b) => b.text().includes('转 115'))).toBe(false)
    }
  })

  it('非本地下载任务（已在 115 通道）不显示换通道按钮', () => {
    const w = mount(TaskDetail, {
      props: {
        task: makeTask({ status: 'failed', kind: 'n115', error_code: 'NET_TIMEOUT', error_message: 'x' }),
      },
      global: { stubs },
    })
    expect(w.findAll('button').some((b) => b.text().includes('转 115'))).toBe(false)
  })
})
