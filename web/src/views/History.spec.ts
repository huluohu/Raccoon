import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { mountView } from '@/test/mount'
import History from '@/views/History.vue'
import { makeTask } from '@/test/fixtures'
import type { Task } from '@/api/types'

/**
 * History.vue 用例（docs/15 §2.3 历史只读页 / docs/07 §8 每视图配 spec）。
 *
 * 关键验收点：
 * 1) 只读是真的只读：行上没有取消/重试/删除，也没有批量工具条与选择框。
 * 2) 日期筛选按本地自然日边界算，默认「近 7 天」，切「全部」能看到更早的记录。
 * 3) 汇总条口径：成功率 = 已完成 / 终态总数，分母为 0 时显示「—」而不是 0%；
 *    列表被截断时如实标注「仅统计已加载的最近 100 条」。
 * 4) 空态是 done 表情的 REmpty，不是白页。
 * 5) 点行开详情抽屉，数据来自 tasks.events + api.taskDetail。
 *
 * 这里用真实 TaskList / TaskCard / TaskRow / RButton：只读的核心是「按钮到底渲不渲染」，
 * 用 stub 断言等于把要验的东西替换掉了。
 */

// ---- 后端接口 mock ----
// vi.mock 的工厂会被提升到文件顶部，引用不到后面才初始化的 const，
// 所以 mock 函数必须放进 vi.hoisted 里先造出来。
const mocks = vi.hoisted(() => ({
  listTasks: vi.fn(),
  taskEvents: vi.fn(),
  taskDetail: vi.fn(),
}))

vi.mock('@/api/client', () => ({ api: mocks }))

const emptyCounts = {
  waiting: 0,
  parsing: 0,
  downloading: 0,
  post: 0,
  done: 0,
  failed: 0,
  cancelled: 0,
}

/** GET /tasks 的响应形状（store.refresh 会读 items / counts / next_cursor）。 */
function listResult(items: Task[], over: Record<string, unknown> = {}) {
  return { items, counts: emptyCounts, next_cursor: null, ...over }
}

const DAY = 24 * 60 * 60 * 1000

// 视图注册了详情抽屉（document 级 Esc 监听），用例结束必须卸载，否则跨用例污染。
beforeEach(() => {
  mocks.listTasks.mockReset().mockResolvedValue(listResult([]))
  mocks.taskEvents.mockReset().mockResolvedValue({ items: [] })
  mocks.taskDetail.mockReset().mockResolvedValue({ data: makeTask(), files: [] })
})

function mountHistory() {
  return mountView(History)
}

/** 按文案找按钮（RButton 渲染成普通 button）。 */
function btnByText(w: ReturnType<typeof mountHistory>, text: string) {
  return w.findAll('button').find((b) => b.text().trim() === text)
}

describe('History 视图', () => {
  it('只读：行内没有取消/重试/删除，也没有批量工具条与选择框', async () => {
    const now = Date.now()
    mocks.listTasks.mockResolvedValueOnce(
      listResult([
        makeTask({ id: 'ok-1', status: 'done', finished_at: now - 1000 }),
        makeTask({ id: 'bad-1', status: 'failed', finished_at: now - 2000 }),
        makeTask({ id: 'cancel-1', status: 'cancelled', finished_at: now - 3000 }),
      ]),
    )
    const w = mountHistory()
    await flushPromises()

    // 三条终态任务都渲染出来了，说明「没按钮」不是因为列表空
    expect(w.findAll('.trow').length).toBe(3)

    const labels = w.findAll('button').map((b) => b.text().trim())
    expect(labels).not.toContain('取消')
    expect(labels).not.toContain('重试')
    expect(labels).not.toContain('删除')
    expect(labels).not.toContain('重试失败项')
    // 批量工具条、行操作区容器、多选框一律不存在
    expect(w.find('.batchbar').exists()).toBe(false)
    expect(w.find('.trow-ops').exists()).toBe(false)
    expect(w.find('.trow-pick').exists()).toBe(false)
  })

  it('日期筛选按自然日算：默认近 7 天只留今天与 3 天前，切「全部」三条都在', async () => {
    const now = Date.now()
    mocks.listTasks.mockResolvedValueOnce(
      listResult([
        makeTask({ id: 'today', title: '今天的', status: 'done', finished_at: now - 60_000 }),
        makeTask({ id: 'd3', title: '三天前的', status: 'done', finished_at: now - 3 * DAY }),
        makeTask({ id: 'd40', title: '四十天前的', status: 'failed', finished_at: now - 40 * DAY }),
      ]),
    )
    const w = mountHistory()
    await flushPromises()

    // 默认「近 7 天」
    expect(w.findAll('.trow').length).toBe(2)
    expect(w.text()).toContain('三天前的')
    expect(w.text()).not.toContain('四十天前的')

    await btnByText(w, '全部')!.trigger('click')
    await flushPromises()
    expect(w.findAll('.trow').length).toBe(3)
    expect(w.text()).toContain('四十天前的')
  })

  it('汇总条：成功率 = 已完成/终态总数；分母为 0 显示「—」；截断时如实标注', async () => {
    const now = Date.now()
    mocks.listTasks.mockResolvedValueOnce(
      listResult([
        makeTask({ id: 'ok-1', status: 'done', finished_at: now - 1000, downloaded_size: 100 * 1024 * 1024 }),
        makeTask({ id: 'ok-2', status: 'done', finished_at: now - 2000, downloaded_size: 50 * 1024 * 1024 }),
        makeTask({ id: 'bad-1', status: 'failed', finished_at: now - 3000, downloaded_size: 0 }),
      ]),
    )
    const w = mountHistory()
    await flushPromises()

    const summary = w.find('.summary')
    expect(summary.text()).toContain('3 条记录')
    expect(summary.text()).toContain('150.0 MB')
    expect(summary.text()).toContain('67%') // 2/3 向上取整
    expect(summary.text()).not.toContain('仅统计已加载')
  })

  it('只有进行中的任务时，汇总显示「—」而不是 0%，列表也不把进行中当历史', async () => {
    mocks.listTasks.mockResolvedValueOnce(
      listResult([makeTask({ id: 'run-1', status: 'downloading', finished_at: null })]),
    )
    const w = mountHistory()
    await flushPromises()

    expect(w.findAll('.trow').length).toBe(0)
    expect(w.find('.summary').text()).toContain('0 条记录')
    expect(w.find('.summary').text()).toContain('—')
    expect(w.find('.summary').text()).not.toContain('0%')
  })

  it('列表被服务端截断时，汇总条旁写明只统计已加载的最近 100 条', async () => {
    const now = Date.now()
    mocks.listTasks.mockResolvedValueOnce(
      listResult([makeTask({ id: 'ok-1', status: 'done', finished_at: now - 1000 })], {
        next_cursor: 'cursor-2',
      }),
    )
    const w = mountHistory()
    await flushPromises()

    expect(w.find('.sm-note').text()).toContain('仅统计已加载的最近 100 条')
  })

  it('没有记录时显示 done 表情的空态并给出下一步', async () => {
    const w = mountHistory()
    await flushPromises()

    const empty = w.find('.rempty')
    expect(empty.exists()).toBe(true)
    expect(empty.text()).toContain('这段时间没有下载记录')
    expect(empty.text()).toContain('去「任务」页')
  })

  it('点行打开详情抽屉，事件与产物分别来自 tasks.events 与 api.taskDetail', async () => {
    const now = Date.now()
    mocks.listTasks.mockResolvedValueOnce(
      listResult([makeTask({ id: 'ok-1', status: 'done', title: '看这条历史', finished_at: now - 1000 })]),
    )
    mocks.taskEvents.mockResolvedValue({ items: [] })
    mocks.taskDetail.mockResolvedValue({ data: makeTask({ id: 'ok-1' }), files: [] })

    const w = mountHistory()
    await flushPromises()

    await w.find('.trow-main').trigger('click')
    await flushPromises()

    expect(mocks.taskEvents).toHaveBeenCalledWith('ok-1')
    expect(mocks.taskDetail).toHaveBeenCalledWith('ok-1')
    // 抽屉 Teleport 到 body
    expect(document.body.querySelector('.dd-panel')?.textContent).toContain('看这条历史')
  })
})
