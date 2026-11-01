// 任务域 store（docs/05 §4「进度即真相」）：
//   初始 GET /tasks 全量对账 → SSE 增量投影 → 事件流断开自动重连 + 重连后再对账一次。
// 前端不维护第二状态：列表行字段一律以服务端返回为准，SSE 只做局部打补丁。
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api } from '@/api/client'
import { ApiError, type CreateParentTasksResponse, type CreateTasksResponse, type Task, type TaskBatchResult, type TaskEvent, type TaskStatus } from '@/api/types'
import { useAuthStore } from './auth'

/** SSE 广播事件名（后端 worker/task_handler 侧字面量）：task.status / task.progress /
 *  task.done / task.failed / system.warn。断连期间丢的帧由重连后全量对账兜底。 */

interface TaskProgressPayload {
  task_id: string
  progress: number
  speed_bps?: number
  eta_sec?: number
  downloaded_size?: number
  expected_size?: number
  progress_known?: boolean
  /** 父任务聚合帧（backend internal/task/parent.go）：仅父任务带这两个字段。 */
  child_total?: number
  child_failed?: number
}

/** 重连退避（前端不阻塞业务请求）。 */
const RECONNECT_STEPS = [1, 2, 5, 10, 30]

export const useTasksStore = defineStore('tasks', () => {
  const items = ref<Task[]>([])
  const counts = ref<Record<TaskStatus, number>>({
    waiting: 0,
    parsing: 0,
    downloading: 0,
    post: 0,
    done: 0,
    failed: 0,
    cancelled: 0,
  })
  const loading = ref(false)
  const error = ref('')
  const connected = ref(false)
  const warning = ref('')
  /** SSE notif.new 计数：通知 store watch 它来增量拉未读（避免第二条 SSE 连接）。 */
  const notifTick = ref(0)
  /** 父任务子项计数：SSE 聚合帧才带 child_total/child_failed（GET /tasks 不返回这两列），
   *  故按父任务 ID 单独留存，避免整表对账后丢失。 */
  const childStats = ref<Record<string, { total: number; failed: number }>>({})
  /** 列表被截断（服务端还有下一页）：子项统计只能覆盖已加载部分，UI 需如实说明。 */
  const listTruncated = ref(false)

  const activeCount = computed(
    () => counts.value.waiting + counts.value.parsing + counts.value.downloading + counts.value.post,
  )

  let source: EventSource | null = null
  let retryIdx = 0
  let reconnectTimer: number | undefined
  /** 断线宽限定时器：onerror 后 8s 内保持「已连接」显示（SSE 正常时
   *  1~2s 内就重连成功；无宽限的话每次网络抖动/心跳间隙抖动都会闪
   *  「重连中」，观感就是「老是重连中」）。 */
  let graceTimer: number | undefined

  /** refresh 请求序号：只认最后一次的结果（见 refresh 注释）。 */
  let refreshSeq = 0

  async function refresh(): Promise<void> {
    loading.value = true
    error.value = ''
    // 请求序号：并发 refresh 时**只认最后一次**的结果。
    //
    // 2026-10-30 修（响应乱序覆盖新数据）：一次批量提交会在几秒内产生几十个
    // done/failed 事件，每个都 void refresh() —— 这些请求并发发出、无先后保证，
    // 后到的旧响应会把更新的数据覆盖掉（用户看到任务状态"倒退"）。
    const seq = ++refreshSeq
    try {
      // limit=100（后端上限）：父子任务树要求子项行也在列表里，
      // 否则父卡展开后看不到子任务。
      const res = await api.listTasks({ limit: '100' })
      if (seq !== refreshSeq) return // 已有更新的请求在飞，丢弃本次结果
      items.value = res.items ?? []
      listTruncated.value = Boolean(res.next_cursor)
      if (res.counts) counts.value = res.counts
    } catch (e) {
      if (seq !== refreshSeq) return
      if (e instanceof ApiError && (e.code === 'UNAUTHORIZED' || e.code === 'FORBIDDEN')) {
        useAuthStore().markUnauthenticated()
        return
      }
      error.value = e instanceof ApiError ? e.message : '任务列表加载失败'
    } finally {
      loading.value = false
    }
  }

  /** 单任务局部更新（不整表重拉：SSE 打补丁）。 */
  function patch(id: string, fields: Partial<Task>): void {
    const row = items.value.find((t) => t.id === id)
    if (row) Object.assign(row, fields)
  }

  /**
   * SSE 状态迁移时同步筛选标签上的计数。
   *
   * 2026-10-30 修（计数漂移）：原先只在「列表里没见过这个任务」时才给目标状态
   * +1。但服务端 counts 是**全库**计数，而列表只装了前 100 条——于是列表内的
   * 任务每次迁移都变成「源 -1、目标 +0」，净减 1，筛选标签上的数字持续少算，
   * 直到下一次 refresh 才纠正。
   *
   * 现在无条件 ±1：源状态 -1、目标状态 +1，净值为 0，与「列表内任务换了个状态」
   * 的真实含义一致（总数不变）。
   */
  function bumpCounts(from: TaskStatus | undefined, to: TaskStatus, id: string): void {
    if (from === to) return
    if (from && from in counts.value) counts.value[from] = Math.max(0, counts.value[from] - 1)
    if (to in counts.value) counts.value[to] = Math.max(0, counts.value[to] + 1)
    void id
  }

  /**
   * 关闭系统告警条。
   *
   * 2026-10-30 修（告警永挂）：原先 warning 只有 setter、没有任何地方清空——
   * 磁盘低水位这类告警会**永久**挂在页面上，即使条件早已恢复。
   * 给一个「知道了」入口，用户能手动关掉。
   */
  function dismissWarning(): void {
    warning.value = ''
  }

  function connect(): void {
    // 自愈：已存在的连接若已 CLOSED（被误断开 / 网络断过），重建一条。
    // 否则 source 非空就直接 return，连接死了也没人管——侧栏会永远停在「重连中」。
    //
    // CLOSED = 2（EventSource.readyState 的标准值）。这里写字面量而不是
    // EventSource.CLOSED：部分运行环境（如测试桩）没有这个静态常量。
    if (source) {
      if (source.readyState !== 2) return
      source.close()
      source = null
    }
    // EventSource 无法带自定义头，会话 cookie 同源自动携带（后端 /events 允许 cookie 会话）。
    source = new EventSource('/api/v1/events', { withCredentials: true })

    source.onopen = () => {
      if (graceTimer) {
        window.clearTimeout(graceTimer)
        graceTimer = undefined
      }
      connected.value = true
      retryIdx = 0
      // 重连后全量对账一次，补齐断连期间丢的帧。
      void refresh()
    }

    source.onerror = () => {
      source?.close()
      source = null
      // 宽限：8s 内暂不翻「重连中」（短暂断流重连很快，闪烁只是噪音）；
      // 持续失败才如实显示离线。
      if (graceTimer === undefined) {
        graceTimer = window.setTimeout(() => {
          graceTimer = undefined
          connected.value = false
        }, 8000)
      }
      const step = RECONNECT_STEPS[Math.min(retryIdx, RECONNECT_STEPS.length - 1)]
      retryIdx += 1
      reconnectTimer = window.setTimeout(connect, step * 1000)
    }

    const onStatus = (ev: MessageEvent) => {
      const d = JSON.parse(ev.data) as {
        task_id: string
        status: TaskStatus
        wait_reason?: string
        child_total?: number
        child_failed?: number
        /** auto_retry 帧携带：重试等待期就能看到「错误与建议」与倒计时，不等 failed 终态。 */
        attempt?: number
        max_attempts?: number
        error_code?: string
        error_message?: string
        scheduled_at?: number
      }
      const row = items.value.find((t) => t.id === d.task_id)
      bumpCounts(row?.status, d.status, d.task_id)
      const fields: Partial<Task> = { status: d.status, wait_reason: d.wait_reason }
      if (d.error_code !== undefined) {
        fields.error_code = d.error_code
        fields.error_message = d.error_message ?? ''
      }
      if (d.attempt !== undefined) fields.attempt = d.attempt
      if (d.max_attempts !== undefined) fields.max_attempts = d.max_attempts
      if (d.scheduled_at !== undefined) fields.scheduled_at = d.scheduled_at
      patch(d.task_id, fields)
      // 父任务聚合帧带子项计数（backend internal/task/parent.go）。
      if (d.child_total !== undefined) {
        patch(d.task_id, { child_total: d.child_total, child_failed: d.child_failed })
        childStats.value = {
          ...childStats.value,
          [d.task_id]: { total: d.child_total, failed: d.child_failed ?? 0 },
        }
      }
      if (d.status === 'done' || d.status === 'failed') void refresh()
    }

    source.addEventListener('task.status', onStatus as EventListener)

    source.addEventListener('task.progress', ((ev: MessageEvent) => {
      const d = JSON.parse(ev.data) as TaskProgressPayload
      // 父任务聚合帧只有 progress/downloaded_size + 子项计数，没有 speed/expected_size；
      // 绝不能拿它覆盖父任务自身的状态与速度（父任务没有自己的下载过程）。
      if (d.child_total !== undefined) {
        patch(d.task_id, {
          progress: d.progress,
          downloaded_size: d.downloaded_size ?? 0,
          child_total: d.child_total,
          child_failed: d.child_failed,
        })
        childStats.value = {
          ...childStats.value,
          [d.task_id]: { total: d.child_total, failed: d.child_failed ?? 0 },
        }
        return
      }
      patch(d.task_id, {
        progress: d.progress,
        speed_bps: d.speed_bps ?? 0,
        downloaded_size: d.downloaded_size ?? 0,
        expected_size: d.expected_size && d.expected_size > 0 ? d.expected_size : null,
        status: 'downloading',
      })
    }) as EventListener)

    source.addEventListener('task.done', ((ev: MessageEvent) => {
      const d = JSON.parse(ev.data) as { task_id: string; size: number }
      patch(d.task_id, { status: 'done', progress: 100, speed_bps: 0, downloaded_size: d.size })
      void refresh()
    }) as EventListener)

    source.addEventListener('task.failed', ((ev: MessageEvent) => {
      const d = JSON.parse(ev.data) as { task_id: string; error_code?: string; error_message?: string; wait_reason?: string; fix_hint?: string }
      // 2026-10-30 修（错误文案被覆盖成空）：后端 task.failed 帧**不带**
      // error_message（只有 error_code 与 fix_hint），而这里无条件写
      // error_message: d.error_message → 把已有文案覆盖成 undefined，
      // 列表里只剩一个孤零零的错误码。改成「有值才写」。
      const fields: Partial<Task> = {
        status: d.wait_reason === 'auto_retry' ? 'waiting' : 'failed',
      }
      if (d.error_code !== undefined) fields.error_code = d.error_code
      if (d.error_message !== undefined) fields.error_message = d.error_message
      if (d.fix_hint !== undefined) fields.fix_hint = d.fix_hint
      patch(d.task_id, fields)
      void refresh()
    }) as EventListener)

    source.addEventListener('system.warn', ((ev: MessageEvent) => {
      const d = JSON.parse(ev.data) as { code: string; message: string }
      warning.value = d.message
    }) as EventListener)


    // 通知中心复用这条连接：后端 notification_handler 在 notify() 里广播 notif.new。
    // 这里只置个时间戳，由 notifications store 的 watch 触发拉取（保持单一数据源，不在前端拼通知对象）。
    source.addEventListener('notif.new', (() => {
      notifTick.value++
    }) as EventListener)
  }

  function disconnect(): void {
    if (reconnectTimer) window.clearTimeout(reconnectTimer)
    if (graceTimer) {
      window.clearTimeout(graceTimer)
      graceTimer = undefined
    }
    source?.close()
    source = null
    connected.value = false
  }

  // ---- 操作 ----

  async function submit(url: string, quality?: string, convertTo?: string, kind?: string): Promise<void> {
    error.value = ''
    try {
      await api.createTasks([{ url, quality, convert_to: convertTo || undefined, kind: kind || undefined }])
      await refresh()
    } catch (e) {
      error.value = e instanceof ApiError ? e.message : '提交失败'
      throw e
    }
  }

  /** 提交父任务（批量）：父卡 + 子任务树（docs/05 §5）。 */
  async function submitParent(
    items: { url: string; quality?: string; save_path?: string; convert_to?: string; kind?: string }[],
    quality?: string,
  ): Promise<CreateParentTasksResponse> {
    error.value = ''
    try {
      const res = await api.createParentTasks(items, quality)
      // 创建响应里的子项计数是权威值（GET /tasks 不返回这两列）。
      childStats.value = {
        ...childStats.value,
        [res.data.id]: { total: res.child_total, failed: res.child_failed },
      }
      await refresh()
      return res
    } catch (e) {
      error.value = e instanceof ApiError ? e.message : '批量提交失败'
      throw e
    }
  }

  /** 普通批量提交（POST /tasks，一次多条，冲突项单独报出）。 */
  async function submitBatch(
    items: { url: string; quality?: string; save_path?: string; convert_to?: string; kind?: string }[],
  ): Promise<CreateTasksResponse> {
    error.value = ''
    try {
      const res = await api.createTasks(items)
      await refresh()
      return res
    } catch (e) {
      error.value = e instanceof ApiError ? e.message : '批量提交失败'
      throw e
    }
  }

  /** 批量操作（docs/04 §2.2）：逐个报告，单条失败不影响其余。 */
  async function batch(action: 'cancel' | 'retry' | 'delete', ids: string[]): Promise<TaskBatchResult> {
    error.value = ''
    try {
      const res = await api.batchTasks(action, ids)
      await refresh()
      return res
    } catch (e) {
      error.value = e instanceof ApiError ? e.message : '批量操作失败'
      throw e
    }
  }

  async function preview(url: string, proxy?: string) {
    return proxy ? api.parseUrl(url, proxy) : api.parseUrl(url)
  }

  async function cancel(id: string): Promise<void> {
    await api.cancelTask(id)
    await refresh()
  }

  async function retry(
    id: string,
    kind?: 'n115',
    proxyOpts?: { proxy?: string; use_proxy?: boolean },
  ): Promise<void> {
    error.value = ''
    try {
      await api.retryTask(id, kind, proxyOpts)
      await refresh()
    } catch (e) {
      error.value = e instanceof ApiError ? e.message : '重试失败'
      throw e
    }
  }

  /** 置顶（仅 waiting）：后端 409 时把原因抛给 toast。 */
  async function setPriority(id: string, priority: number): Promise<void> {
    error.value = ''
    try {
      await api.setTaskPriority(id, priority)
      await refresh()
    } catch (e) {
      error.value = e instanceof ApiError ? e.message : '置顶失败'
      throw e
    }
  }

  async function remove(id: string): Promise<void> {
    await api.deleteTask(id)
    await refresh()
  }

  async function events(id: string): Promise<TaskEvent[]> {
    const res = await api.taskEvents(id)
    return res.items ?? []
  }

  return {
    items,
    counts,
    loading,
    error,
    connected,
    warning,
    notifTick,
    childStats,
    listTruncated,
    activeCount,
    refresh,
    connect,
    disconnect,
    submit,
    submitParent,
    submitBatch,
    dismissWarning,
    bumpCounts,
    batch,
    preview,
    cancel,
    retry,
    setPriority,
    remove,
    events,
  }
})