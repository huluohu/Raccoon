// 系统域 store：仪表盘统计 / 监控 / 偏好的取数与轮询（docs/15 §2.3）。
//
// 为什么单独一个 store 而不是各视图自己 setInterval：
// 1) 轮询生命周期要跟「页面在不在前台」绑：视图里各写一遍必然漏掉某个页面；
// 2) 主页与监控页看的是同一份 monitor 快照，两处各拉一次等于双倍请求；
// 3) 测试只需要 mock api 层，不用在每个视图 spec 里管定时器。
//
// 诚实原则：接口失败不清空已有数据（保留上一帧 + 暴露 error），
// 否则监控带会周期性闪成空白。
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api } from '@/api/client'
import type { MonitorSnapshot, SummaryStats, TrendResult } from '@/api/types'

/** 轮询节奏（docs/14 §3.2：summary 5s / trend 60s / monitor 2s）。 */
const SUMMARY_MS = 5000
const TREND_MS = 60_000
const MONITOR_MS = 2000

export const useSystemStore = defineStore('system', () => {
  const summary = ref<SummaryStats | null>(null)
  const trend7 = ref<TrendResult | null>(null)
  const trend30 = ref<TrendResult | null>(null)
  const monitor = ref<MonitorSnapshot | null>(null)
  const loading = ref(false)
  const error = ref('')

  /** 非 Linux 平台：整条监控链路降级（界面显示「当前平台不支持」，不画假图）。 */
  const monitorUnavailable = computed(() => monitor.value !== null && !monitor.value.ok)

  let summaryTimer: number | undefined
  let trendTimer: number | undefined
  let monitorTimer: number | undefined

  async function refreshSummary(): Promise<void> {
    try {
      summary.value = await api.statsSummary()
      error.value = ''
    } catch (e) {
      error.value = e instanceof Error ? e.message : '统计加载失败'
    }
  }

  async function refreshTrend(): Promise<void> {
    const [t7, t30] = await Promise.all([
      api.statsTrend(7).catch(() => null),
      api.statsTrend(30).catch(() => null),
    ])
    if (t7) trend7.value = t7
    if (t30) trend30.value = t30
  }

  async function refreshMonitor(): Promise<void> {
    try {
      monitor.value = await api.monitorSnapshot()
    } catch {
      // 监控失败保留上一帧：主页监控带不该周期性闪空白
    }
  }

  /** 首屏一次拉齐（loading 只在这一轮里为真）。 */
  async function loadAll(): Promise<void> {
    loading.value = true
    try {
      await refreshSummary()
      await Promise.all([refreshTrend(), refreshMonitor()])
    } finally {
      loading.value = false
    }
  }

  /**
   * 起轮询。页面不可见时跳过（后台标签页不该继续打接口）。
   * @param opts.monitor 是否轮询监控（只有主页与监控页需要）
   */
  function startPolling(opts: { monitor?: boolean } = {}): void {
    stopPolling()
    summaryTimer = window.setInterval(() => {
      if (!document.hidden) void refreshSummary()
    }, SUMMARY_MS)
    trendTimer = window.setInterval(() => {
      if (!document.hidden) void refreshTrend()
    }, TREND_MS)
    if (opts.monitor !== false) {
      monitorTimer = window.setInterval(() => {
        if (!document.hidden) void refreshMonitor()
      }, MONITOR_MS)
    }
  }

  function stopPolling(): void {
    if (summaryTimer) window.clearInterval(summaryTimer)
    if (trendTimer) window.clearInterval(trendTimer)
    if (monitorTimer) window.clearInterval(monitorTimer)
    summaryTimer = undefined
    trendTimer = undefined
    monitorTimer = undefined
  }

  return {
    summary,
    trend7,
    trend30,
    monitor,
    loading,
    error,
    monitorUnavailable,
    refreshSummary,
    refreshTrend,
    refreshMonitor,
    loadAll,
    startPolling,
    stopPolling,
  }
})
