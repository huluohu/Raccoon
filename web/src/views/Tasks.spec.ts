import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { h } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { mountView } from '@/test/mount'
import Tasks from '@/views/Tasks.vue'
import { makeTask } from '@/test/fixtures'
import { useTasksStore } from '@/stores/tasks'
import { ApiError } from '@/api/types'
import {
  DetailDrawerStub,
  PlatformBadgeStub,
  RButtonStub,
  REmptyStub,
  RouterLinkStub,
} from '@/test/stubs'

/**
 * Tasks.vue 用例（docs/07 §3：QuickSubmit / ActiveTasks / IssuesPanel / StatusStrip；
 * docs/05 §5 父子任务树；§7 快捷键；§4 破坏性决策走 RDialog）。
 *
 * 说明：RInput 用真实组件（要断言 `/` 快捷键真的把焦点送进 id=task-url-input 的输入框），
 * RDialog 用真实组件（删除确认是真实用户路径：点删除 → 弹窗默认焦点在「取消」→ 确认才真删），
 * TaskCard/TaskRow/TaskDetail 用本地 stub——它们的渲染细节由各自的 spec 负责，
 * 这里只断言视图层的编排（谁该用卡片、谁该用行、展开后子项在不在）。
 */

// ---- 后端接口 mock ----
// vi.mock 的工厂会被提升到文件顶部，引用不到后面才初始化的 const，
// 所以 mock 函数必须放进 vi.hoisted 里先造出来。
const mocks = vi.hoisted(() => ({
  listTasks: vi.fn(),
  parseUrl: vi.fn(),
  createTasks: vi.fn(),
  createParentTasks: vi.fn(),
  batchTasks: vi.fn(),
  taskDetail: vi.fn(),
  taskEvents: vi.fn(),
  cancelTask: vi.fn(),
  retryTask: vi.fn(),
  deleteTask: vi.fn(),
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

beforeEach(() => {
  mocks.listTasks.mockReset().mockResolvedValue({ items: [], counts: emptyCounts, next_cursor: null })
  mocks.parseUrl.mockReset().mockResolvedValue({
    data: {
      url: 'https://www.bilibili.com/video/BV1xx411c7mD',
      platform: 'bilibili',
      title: '测试视频 · 合集',
      uploader: '测试 UP',
      duration: 600,
      is_playlist: true,
      kind: 'video',
      formats: [
        { quality: '1080p', size: 100 * 1024 * 1024 },
        { quality: '720p', size: 50 * 1024 * 1024 },
        { quality: 'audio', size: 5 * 1024 * 1024 },
      ],
    },
  })
  mocks.createTasks.mockReset().mockResolvedValue({ data: [makeTask()], conflicts: [] })
  mocks.createParentTasks.mockReset().mockResolvedValue({
    data: makeTask({ id: 'p-1', platform: 'batch' }),
    children: [],
    child_total: 2,
    child_failed: 0,
  })
  mocks.batchTasks.mockReset().mockResolvedValue({ succeeded: [], failed: [] })
  mocks.taskDetail.mockReset().mockResolvedValue({ data: makeTask(), files: [] })
  mocks.taskEvents.mockReset().mockResolvedValue({ items: [] })
  mocks.cancelTask.mockReset().mockResolvedValue({ ok: true })
  mocks.retryTask.mockReset().mockResolvedValue({ ok: true })
  mocks.deleteTask.mockReset().mockResolvedValue({ ok: true })

  FakeEventSource.instances = []
  vi.stubGlobal('EventSource', FakeEventSource)
})

// ---- EventSource：jsdom 不实现，store.connect() 需要它 ----
class FakeEventSource {
  static instances: FakeEventSource[] = []
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  closed = false
  private listeners = new Map<string, EventListener[]>()
  constructor(
    public url: string,
    public init?: EventSourceInit,
  ) {
    FakeEventSource.instances.push(this)
  }
  addEventListener(type: string, fn: EventListener) {
    const arr = this.listeners.get(type) ?? []
    arr.push(fn)
    this.listeners.set(type, arr)
  }
  close() {
    this.closed = true
  }
}

// ---- 任务树组件 stub（渲染细节归各自 spec，这里只留可断言锚点）----
const TaskCardStub = {
  props: {
    task: { type: Object, required: true },
    parent: { type: Boolean, default: false },
    childStats: { type: Object, default: undefined },
    selected: { type: Boolean, default: false },
  },
  emits: ['open', 'select'],
  setup(props: Record<string, any>, { slots, emit }: any) {
    return () =>
      h('article', { class: 'stub-card', 'data-task': props.task.id }, [
        h('span', { class: 'stub-card-stats' }, `共 ${props.childStats?.total ?? 0} 项`),
        h('button', { class: 'stub-card-open', onClick: () => emit('open', props.task) }, '打开'),
        slots.actions?.({ task: props.task }),
        slots['retry-failed']?.({ task: props.task, failed: props.childStats?.failed ?? 0 }),
      ])
  },
}

const TaskRowStub = {
  props: {
    task: { type: Object, required: true },
    size: { type: String, default: 'md' },
    selected: { type: Boolean, default: false },
  },
  emits: ['open', 'select'],
  setup(props: Record<string, any>, { slots, emit }: any) {
    return () =>
      h('li', { class: 'stub-row', 'data-task': props.task.id, 'data-size': props.size }, [
        h('button', { class: 'stub-row-open', onClick: () => emit('open', props.task) }, '打开'),
        slots.actions?.({ task: props.task }),
      ])
  },
}

const TaskDetailStub = {
  props: {
    task: { type: Object, required: true },
    events: { type: Array, default: () => [] },
    files: { type: Array, default: () => [] },
    busy: { type: Boolean, default: false },
  },
  emits: ['goto-connections', 'goto-settings', 'retry', 'cancel', 'remove'],
  setup(props: Record<string, any>) {
    return () => h('div', { class: 'stub-detail', 'data-task': props.task.id })
  },
}

const stubs = {
  RButton: RButtonStub,
  REmpty: REmptyStub,
  DetailDrawer: DetailDrawerStub,
  PlatformBadge: PlatformBadgeStub,
  TaskCard: TaskCardStub,
  TaskRow: TaskRowStub,
  TaskDetail: TaskDetailStub,
  'router-link': RouterLinkStub,
}

function mountTasks() {
  return mountView(Tasks, [], { global: { stubs } })
}

/** 按按钮文案找按钮（RButton stub 渲染成普通 button）。 */
function btnByText(w: any, text: string) {
  return w.findAll('button').find((b: any) => b.text().includes(text))
}

afterEach(() => {
  vi.unstubAllGlobals()
  // RDialog 真实挂载会改 body.style.overflow，用例间必须还原，否则互相污染。
  document.body.style.overflow = ''
})

describe('Tasks 视图', () => {
  it('挂载后拉全量任务并接上 SSE（实时连接徽章文案随之变化）', async () => {
    const w = mountTasks()
    await flushPromises()
    expect(mocks.listTasks).toHaveBeenCalled()
    // limit=100：父子任务树要求子项行也在列表里（store 内注释）
    expect(mocks.listTasks.mock.calls[0]?.[0]).toMatchObject({ limit: '100' })
    expect(FakeEventSource.instances.length).toBe(1)
    expect(FakeEventSource.instances[0].url).toBe('/api/v1/events')
    expect(w.text()).toContain('实时未连接')
    w.unmount()
  })

  it('空列表显示引导空态，不甩白页', async () => {
    const w = mountTasks()
    await flushPromises()
    expect(w.find('.stub-empty').exists()).toBe(true)
    expect(w.find('.stub-empty').text()).toContain('这个筛选下还没有任务')
  })

  it('解析预览后出现预览卡，默认选中最高档（不含 audio）', async () => {
    const w = mountTasks()
    await flushPromises()

    await w.find('#task-url-input').setValue('https://www.bilibili.com/video/BV1xx411c7mD')
    await btnByText(w, '解析预览').trigger('click')
    await flushPromises()

    expect(mocks.parseUrl).toHaveBeenCalledWith('https://www.bilibili.com/video/BV1xx411c7mD')
    const preview = w.find('.preview')
    expect(preview.exists()).toBe(true)
    expect(preview.text()).toContain('测试视频 · 合集')
    expect(preview.text()).toContain('合集')
    // 默认选中 1080p：formats 已按分辨率降序，audio 不能当默认档
    expect(preview.find('.q.on').text()).toContain('1080p')
  })

  it('预览卡「加入下载」走 POST /tasks 并带上选中的画质', async () => {
    const w = mountTasks()
    await flushPromises()

    await w.find('#task-url-input').setValue('https://www.bilibili.com/video/BV1xx411c7mD')
    await btnByText(w, '解析预览').trigger('click')
    await flushPromises()
    await w.find('.q:not(.on)').trigger('click') // 换成 720p
    await btnByText(w, '加入下载').trigger('click')
    await flushPromises()

    expect(mocks.createTasks).toHaveBeenCalledWith([
      { url: 'https://www.bilibili.com/video/BV1xx411c7mD', quality: '720p' },
    ])
    // 提交成功后预览卡收起、输入框清空
    expect(w.find('.preview').exists()).toBe(false)
    expect((w.find('#task-url-input').element as HTMLInputElement).value).toBe('')
  })

  it('解析失败时错误信息内联展示，不静默', async () => {
    const w = mountTasks()
    await flushPromises()
    mocks.parseUrl.mockRejectedValueOnce(new ApiError('PARSE_FAILED', '链接解析不了', 'req-1'))

    await w.find('#task-url-input').setValue('https://example.com/nope')
    await btnByText(w, '解析预览').trigger('click')
    await flushPromises()

    expect(w.find('.err').text()).toContain('链接解析不了')
    expect(w.find('.preview').exists()).toBe(false)
  })

  it('失败筛选只留失败任务（docs/07 §3 ActiveTasks 筛选）', async () => {
    mocks.listTasks.mockResolvedValueOnce({
      items: [
        makeTask({ id: 'ok', status: 'done', title: '好的' }),
        makeTask({ id: 'bad', status: 'failed', title: '坏的' }),
      ],
      counts: { ...emptyCounts, done: 1, failed: 1 },
      next_cursor: null,
    })
    const w = mountTasks()
    await flushPromises()

    await btnByText(w, '失败').trigger('click')
    await flushPromises()

    const rows = w.findAll('.stub-row')
    expect(rows.length).toBe(1)
    expect(rows[0].attributes('data-task')).toBe('bad')
  })

  it('父任务渲染 TaskCard，展开后子任务以 TaskRow(sm) 逐条列出', async () => {
    mocks.listTasks.mockResolvedValueOnce({
      items: [
        makeTask({ id: 'p-1', platform: 'batch', title: '批量任务' }),
        makeTask({ id: 'c-1', parent_id: 'p-1', title: '子 1' }),
        makeTask({ id: 'c-2', parent_id: 'p-1', title: '子 2' }),
      ],
      counts: emptyCounts,
      next_cursor: null,
    })
    const w = mountTasks()
    await flushPromises()

    // 父任务走卡片，不是行
    expect(w.findAll('.stub-card').length).toBe(1)
    expect(w.findAll('.stub-row').length).toBe(0)

    await btnByText(w, '展开子项').trigger('click')
    await flushPromises()

    const children = w.findAll('.children .stub-row')
    expect(children.length).toBe(2)
    expect(children.every((r: any) => r.attributes('data-size') === 'sm')).toBe(true)
    expect(w.find('.stub-card-stats').text()).toContain('共 2 项')
  })

  it('删除必须走 RDialog 二次确认：默认焦点在「取消」，确认后才真删', async () => {
    mocks.listTasks.mockResolvedValueOnce({
      items: [makeTask({ id: 't-del', title: '待删除' })],
      counts: emptyCounts,
      next_cursor: null,
    })
    const w = mountTasks()
    await flushPromises()

    await btnByText(w, '删除').trigger('click')
    await flushPromises()

    // RDialog Teleport 到 body，要在 document 上找
    const dialog = document.body.querySelector('.rdialog-root')
    expect(dialog).not.toBeNull()
    expect(dialog?.querySelector('.rdialog-title')?.textContent).toContain('删除任务？')
    expect(dialog?.querySelector('.rdialog-desc')?.textContent).toContain('待删除')
    // 默认焦点必须是「取消」（误触回车不会直接删）
    await vi.waitFor(() => {
      expect((document.activeElement as HTMLElement)?.dataset?.rdialogAction).toBe('cancel')
    })

    // Esc 直接关闭，不删除
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await vi.waitFor(() => {
      expect(document.body.querySelector('.rdialog-root')).toBeNull()
    })
    expect(mocks.deleteTask).not.toHaveBeenCalled()

    // 再点删除 → 确认 → 才真正调用删除接口
    await btnByText(w, '删除').trigger('click')
    await flushPromises()
    const confirmBtn = document.body.querySelector<HTMLButtonElement>('[data-rdialog-action="confirm"]')
    confirmBtn?.click()
    await flushPromises()
    expect(mocks.deleteTask).toHaveBeenCalledWith('t-del')
    w.unmount()
  })

  it('点行打开详情抽屉并拉取事件/文件', async () => {
    mocks.listTasks.mockResolvedValueOnce({
      items: [makeTask({ id: 't-open', title: '看详情' })],
      counts: emptyCounts,
      next_cursor: null,
    })
    const w = mountTasks()
    await flushPromises()

    await w.find('.stub-row-open').trigger('click')
    await flushPromises()

    expect(mocks.taskEvents).toHaveBeenCalledWith('t-open')
    expect(mocks.taskDetail).toHaveBeenCalledWith('t-open')
    expect(w.find('.stub-detail').attributes('data-task')).toBe('t-open')
  })

  it('`/` 快捷键把焦点送进本页提交框（docs/07 §7，输入框内不劫持）', async () => {
    const w = mountTasks()
    await flushPromises()

    // 焦点先在别处
    ;(document.body as HTMLElement).focus?.()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: '/', bubbles: true }))
    await flushPromises()
    expect(document.activeElement?.id).toBe('task-url-input')

    // 在输入框里打字时，快捷键一律让行（除 Esc）——按 r 不该触发重试
    const input = w.find('#task-url-input')
    await input.trigger('focus')
    input.element.dispatchEvent(new KeyboardEvent('keydown', { key: 'r', bubbles: true }))
    await flushPromises()
    expect(mocks.listTasks.mock.calls.length).toBe(1)
  })

  it('接口失败显示错误条而不是白屏', async () => {
    mocks.listTasks.mockRejectedValueOnce(new ApiError('INTERNAL', '任务列表拉不到', 'req-1'))
    const w = mountTasks()
    await flushPromises()
    expect(w.find('.err').text()).toContain('任务列表拉不到')
  })

  it('新插入的任务打上 row-flash 高亮，旧任务不打（docs/07 §6 列表变更）', async () => {
    mocks.listTasks.mockResolvedValue({
      items: [makeTask({ id: 'old-1', title: '老的' })],
      counts: emptyCounts,
      next_cursor: null,
    })
    const w = mountTasks()
    await flushPromises()
    expect(w.find('[data-task="old-1"]').classes()).not.toContain('row-flash')

    // 模拟 SSE 推来一条新任务：从同一个 pinia 取 store 追加
    const tasks = useTasksStore()
    tasks.items = [...tasks.items, makeTask({ id: 'new-1', title: '新来的' })]
    await flushPromises()

    expect(w.find('[data-task="new-1"]').classes()).toContain('row-flash')
    expect(w.find('[data-task="old-1"]').classes()).not.toContain('row-flash')
  })
})
