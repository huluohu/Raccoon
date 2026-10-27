// notifications.ts：通知中心 store（未读徽章 + 抽屉列表 + 标记已读）。
//
// 单一数据源：列表与未读数都来自后端 /notifications；SSE notif.new 只当「有变化」的信号，
// 不在前端拼装通知对象（避免前后端两套文案）。断连/丢帧由 60s 轮询兜底。
import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import { api } from '@/api/client'
import type { Notification } from '@/api/types'
import { useTasksStore } from './tasks'

/** 兜底轮询间隔：SSE 断了也能把未读数刷出来。 */
const POLL_MS = 60_000

export const useNotificationsStore = defineStore('notifications', () => {
  const items = ref<Notification[]>([])
  const unread = ref(0)
  const loading = ref(false)
  const error = ref('')
  let pollTimer: number | undefined
  /** SSE 触发的静默刷新 watcher。
   *  必须留住 stop 句柄：watch 的返回值是停止函数，不调用它，
   *  每次 start()（= 每次登录）都会留下一个常驻 watcher，
   *  旧 watcher 还在监听同一个 notifTick → 一次通知触发 N 次请求，且 watcher 越攒越多。 */
  let stopWatch: (() => void) | undefined

  async function load(silent = false) {
    if (!silent) loading.value = true
    error.value = ''
    try {
      const res = await api.listNotifications()
      items.value = res.items ?? []
      unread.value = res.unread ?? 0
    } catch (e) {
      error.value = e instanceof Error ? e.message : '通知加载失败'
    } finally {
      loading.value = false
    }
  }

  async function markRead(ids: string[] = [], all = false) {
    try {
      const res = await api.markNotificationsRead(ids, all)
      if (all) {
        items.value = items.value.map((n) => ({ ...n, read: true }))
      } else {
        const set = new Set(ids)
        items.value = items.value.map((n) => (set.has(n.id) ? { ...n, read: true } : n))
      }
      unread.value = res.unread ?? 0
    } catch (e) {
      error.value = e instanceof Error ? e.message : '标记已读失败'
    }
  }

  function start() {
    if (pollTimer) return
    void load(true)
    pollTimer = window.setInterval(() => void load(true), POLL_MS)
    // SSE 有新通知 → 静默刷新（抽屉开着时用户立刻看到）。
    const tasks = useTasksStore()
    stopWatch = watch(
      () => tasks.notifTick,
      () => void load(true),
    )
  }

  function stop() {
    if (pollTimer) window.clearInterval(pollTimer)
    pollTimer = undefined
    if (stopWatch) {
      stopWatch()
      stopWatch = undefined
    }
    items.value = []
    unread.value = 0
  }

  return { items, unread, loading, error, load, markRead, start, stop }
})