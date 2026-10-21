// 任务域 store（docs/05 §4「进度即真相」）：
//   初始 GET /tasks 全量对账 → SSE 增量投影 → 事件流断开自动重连 + 重连后再对账一次。
// 前端不维护第二状态：列表行字段一律以服务端返回为准，SSE 只做局部打补丁。
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api } from '@/api/client'
import { ApiError, type Task, type TaskEvent, type TaskStatus } from '@/api/types'
import { useAuthStore } from './auth'

/** SSE 广播事件名（后端 worker/task_handler 侧字面量）：task.status / task.progress /
 *  task.done / task.failed / system.warn。断连期间丢的帧由重连后全量对账兜底。 */

interface TaskProgressPayload {
  task_id: string
  progress: number
  speed_bps: number
  eta_sec: number
  downloaded_size: number
  expected_size: number
  progress_known: boolean
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

  const activeCount = computed(
    () => counts.value.waiting + counts.value.parsing + counts.value.downloading + counts.value.post,
  )

  let source: EventSource | null = null
  let retryIdx = 0
  let reconnectTimer: number | undefined

  async function refresh(): Promise<void> {
    loading.value = true
    error.value = ''
    try {
      const res = await api.listTasks({ limit: '50' })
      items.value = res.items ?? []
      if (res.counts) counts.value = res.counts
    } catch (e) {
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

  function bumpCounts(from: TaskStatus | undefined, to: TaskStatus, id: string): void {
    if (from === to) return
    if (from && from in counts.value) counts.value[from] = Math.max(0, counts.value[from] - 1)
    if (!(to in counts.value)) return
    const known = items.value.some((t) => t.id === id)
    counts.value[to] = Math.max(0, counts.value[to] + (known ? 0 : 1))
  }

  function connect(): void {
    if (source) return
    // EventSource 无法带自定义头，会话 cookie 同源自动携带（后端 /events 允许 cookie 会话）。
    source = new EventSource('/api/v1/events', { withCredentials: true })

    source.onopen = () => {
      connected.value = true
      retryIdx = 0
      // 重连后全量对账一次，补齐断连期间丢的帧。
      void refresh()
    }

    source.onerror = () => {
      connected.value = false
      source?.close()
      source = null
      const step = RECONNECT_STEPS[Math.min(retryIdx, RECONNECT_STEPS.length - 1)]
      retryIdx += 1
      reconnectTimer = window.setTimeout(connect, step * 1000)
    }

    const onStatus = (ev: MessageEvent) => {
      const d = JSON.parse(ev.data) as { task_id: string; status: TaskStatus; wait_reason?: string }
      const row = items.value.find((t) => t.id === d.task_id)
      bumpCounts(row?.status, d.status, d.task_id)
      patch(d.task_id, { status: d.status, wait_reason: d.wait_reason })
      if (d.status === 'done' || d.status === 'failed') void refresh()
    }

    source.addEventListener('task.status', onStatus as EventListener)

    source.addEventListener('task.progress', ((ev: MessageEvent) => {
      const d = JSON.parse(ev.data) as TaskProgressPayload
      patch(d.task_id, {
        progress: d.progress,
        speed_bps: d.speed_bps,
        downloaded_size: d.downloaded_size,
        expected_size: d.expected_size > 0 ? d.expected_size : null,
        status: 'downloading',
      })
    }) as EventListener)

    source.addEventListener('task.done', ((ev: MessageEvent) => {
      const d = JSON.parse(ev.data) as { task_id: string; size: number }
      patch(d.task_id, { status: 'done', progress: 100, speed_bps: 0, downloaded_size: d.size })
      void refresh()
    }) as EventListener)

    source.addEventListener('task.failed', ((ev: MessageEvent) => {
      const d = JSON.parse(ev.data) as { task_id: string; error_code?: string; error_message?: string; wait_reason?: string }
      patch(d.task_id, { status: d.wait_reason === 'auto_retry' ? 'waiting' : 'failed', error_code: d.error_code, error_message: d.error_message })
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
    source?.close()
    source = null
    connected.value = false
  }

  // ---- 操作 ----

  async function submit(url: string, quality?: string): Promise<void> {
    error.value = ''
    try {
      await api.createTasks([{ url, quality }])
      await refresh()
    } catch (e) {
      error.value = e instanceof ApiError ? e.message : '提交失败'
      throw e
    }
  }

  async function preview(url: string) {
    return api.parseUrl(url)
  }

  async function cancel(id: string): Promise<void> {
    await api.cancelTask(id)
    await refresh()
  }

  async function retry(id: string): Promise<void> {
    error.value = ''
    try {
      await api.retryTask(id)
      await refresh()
    } catch (e) {
      error.value = e instanceof ApiError ? e.message : '重试失败'
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
    activeCount,
    refresh,
    connect,
    disconnect,
    submit,
    preview,
    cancel,
    retry,
    remove,
    events,
  }
})