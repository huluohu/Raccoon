<script setup lang="ts">
// 主页仪表盘（docs/14 §3.2 复刻 SaveXTube 主页 17 块 + Raccoon 自有增强）。
//
// 结构（首屏 → 第二屏）：
//   1) 快速下载引导卡（顶栏 QuickSubmit 的页内镜像，输入框 id 独立）
//   2) 指标行 1：运行时长 / 总下载 / 今日下载 / 成功率
//   3) 指标行 2：下载总量 / 图片 / 视频 / 本周下载
//   4) 指标行 3：订阅下载 / 收藏下载 / 直播录制 / 待处理问题
//   5) 直播录制卡 + 活跃任务 + 状态条（Raccoon 比原版强的部分，保留）
//   6) 监控带（CPU/内存/存储 donut + 带宽数字，点击进 /monitor）
//   7) 7/30 天下载趋势 + 失败拆分（E5）
//   8) 需要处理的问题（IssuesPanel，含 E2 成功率预警）
//
// 纪律：
// - 指标卡由 CARDS 注册表生成，E1「显示/隐藏 + 排序」才有一份可持久化的 key 清单；
// - success_rate 为 null 时显示「—」（不造 0%/100%）；
// - 统计 / 趋势 / 监控三份数据的取数与轮询归 stores/system.ts（docs/15 §2.3），
//   本页只做展示——页面自己再开一套 setInterval 就是第二份轮询真相。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '@/api/client'
import { useTasksStore } from '@/stores/tasks'
import { useSystemStore } from '@/stores/system'
import { useToastStore } from '@/stores/toast'
import { formatBytes } from '@/utils/format'
import {
  applyLayout,
  buildCards,
  CARD_LABELS,
  DEFAULT_LAYOUT,
  DEFAULT_RATE_THRESHOLD,
  type DashboardCard,
  type LayoutPref,
} from '@/utils/dashboardCards'
import type {
  Credential,
  HealthCheck,
  LiveSource,
  QueueStats,
  SystemInfo,
} from '@/api/types'
import RButton from '@/components/base/RButton.vue'
import RDialog from '@/components/base/RDialog.vue'
import StatusStrip from '@/components/shell/StatusStrip.vue'
import QuickSubmit from '@/components/shell/QuickSubmit.vue'
import ActiveTasks from '@/components/overview/ActiveTasks.vue'
import IssuesPanel from '@/components/overview/IssuesPanel.vue'
import MetricCard from '@/components/base/MetricCard.vue'
import DonutChart from '@/components/charts/DonutChart.vue'
import TrendChart from '@/components/charts/TrendChart.vue'
import type { StatusStripItem } from '@/components/shell/StatusStrip.vue'

const router = useRouter()
const tasks = useTasksStore()
const system = useSystemStore()
const toast = useToastStore()

// ---- 数据 ----
// 统计 / 趋势 / 监控来自 system store（轮询也归它）；本页只额外持有
// 仪表盘专用的页面级数据（健康、系统信息、队列、凭据、直播源）。
const summary = computed(() => system.summary)
const trend7 = computed(() => system.trend7)
const trend30 = computed(() => system.trend30)
const monitor = computed(() => system.monitor)
const trendDays = ref<7 | 30>(7)
const health = ref<HealthCheck | null>(null)
const info = ref<SystemInfo | null>(null)
const queue = ref<QueueStats | null>(null)
const creds = ref<Credential[]>([])
const liveSources = ref<LiveSource[]>([])
const pageLoading = ref(true)
const errorMsg = ref('')
/** 首屏与统计请求的加载态合并成一个，视图只关心「能不能画」。 */
const loading = computed(() => pageLoading.value || system.loading)

/** 成功率预警阈值（E2；配置中心 `alert.success_rate_threshold` 未下发时用 80）。 */
const RATE_THRESHOLD = DEFAULT_RATE_THRESHOLD

const activeTasks = computed(() =>
  tasks.items.filter((t) => ['waiting', 'parsing', 'downloading', 'post'].includes(t.status)).slice(0, 5),
)
const failedTasks = computed(() => tasks.items.filter((t) => t.status === 'failed').slice(0, 5))
const hasDone = computed(
  () => tasks.items.some((t) => t.status === 'done') || (queue.value?.counts.done ?? 0) > 0,
)

const credSummary = computed(() => {
  const items = creds.value
  return {
    total: items.length,
    active: items.filter((c) => c.status === 'active').length,
    problem: items.filter((c) => c.status === 'expired' || c.status === 'expiring').length,
  }
})

const recordingSources = computed(() => liveSources.value.filter((s) => s.status === 'recording'))

/** 待处理问题数：失败任务 + 凭据异常（仪表盘第 12 块）。 */
const issueCount = computed(() => failedTasks.value.length + credSummary.value.problem)

// ---- 指标卡：注册表在 utils/dashboardCards.ts（E1 的 key 清单同源） ----
const cards = computed<DashboardCard[]>(() =>
  buildCards({
    summary: summary.value,
    info: info.value,
    issueCount: issueCount.value,
    recordingCount: recordingSources.value.length,
  }),
)

// ---- E1：布局偏好（显示/隐藏 + 排序） ----
const layout = ref<LayoutPref>({ ...DEFAULT_LAYOUT })
const customOpen = ref(false)
/** 编辑中的草稿（取消不落库）。 */
const draftHidden = ref<string[]>([])
const draftOrder = ref<string[]>([])

/** 按偏好排列后的可见卡片（排序语义在 applyLayout 里，视图不重复实现）。 */
const visibleCards = computed<DashboardCard[]>(() => applyLayout(cards.value, layout.value))

async function loadLayout() {
  try {
    const res = await api.getPreferences()
    const raw = res?.data?.layout
    if (!raw) return
    const parsed = JSON.parse(raw) as Partial<LayoutPref>
    layout.value = {
      hidden: Array.isArray(parsed.hidden) ? parsed.hidden.filter((k) => typeof k === 'string') : [],
      order: Array.isArray(parsed.order) ? parsed.order.filter((k) => typeof k === 'string') : [],
    }
  } catch {
    // 偏好读不到就按默认全量布局渲染——仪表盘不能因为一个偏好接口白屏
  }
}

function openCustom() {
  draftHidden.value = [...layout.value.hidden]
  draftOrder.value = visibleCards.value.map((c) => c.key)
  customOpen.value = true
}

function toggleDraft(key: string) {
  const i = draftHidden.value.indexOf(key)
  if (i >= 0) draftHidden.value.splice(i, 1)
  else draftHidden.value.push(key)
}

function moveDraft(key: string, delta: number) {
  const i = draftOrder.value.indexOf(key)
  const j = i + delta
  if (i < 0 || j < 0 || j >= draftOrder.value.length) return
  const next = [...draftOrder.value]
  ;[next[i], next[j]] = [next[j], next[i]]
  draftOrder.value = next
}

async function saveCustom() {
  const next: LayoutPref = { hidden: [...draftHidden.value], order: [...draftOrder.value] }
  layout.value = next
  customOpen.value = false
  try {
    await api.savePreferences(JSON.stringify(next))
    toast.success('主页布局已保存')
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '布局保存失败')
  }
}

// ---- 状态条 ----
const stripItems = computed<StatusStripItem[]>(() => {
  const out: StatusStripItem[] = []
  const diskPct = health.value?.checks.disk_used_pct
  if (diskPct !== undefined) {
    out.push({
      label: '磁盘',
      value: `已用 ${diskPct}%`,
      tone: diskPct >= 90 ? 'bad' : diskPct >= 75 ? 'warn' : 'ok',
    })
  }
  const db = health.value?.checks.db
  if (db) out.push({ label: '数据库', value: db === 'ok' ? '正常' : String(db), tone: db === 'ok' ? 'ok' : 'bad' })
  out.push({ label: '实时连接', value: tasks.connected ? '已连接' : '重连中', tone: tasks.connected ? 'ok' : 'warn' })
  if (credSummary.value.total > 0) {
    out.push({
      label: '凭据',
      value: credSummary.value.problem > 0 ? `${credSummary.value.problem} 个需处理` : '正常',
      tone: credSummary.value.problem > 0 ? 'warn' : 'ok',
      hint: `${credSummary.value.active}/${credSummary.value.total} 可用`,
    })
  }
  return out
})

// ---- E2：成功率预警条目 ----
const alerts = computed<{ title: string; detail: string; to: string }[]>(() => {
  const r = summary.value?.success_rate
  if (r === null || r === undefined || r >= RATE_THRESHOLD) return []
  return [
    {
      title: `近 7 天成功率 ${r.toFixed(1)}%，低于阈值 ${RATE_THRESHOLD}%`,
      detail: '失败多集中在少数平台，去监控页看失败拆分再决定是换凭据还是换引擎参数。',
      to: '/monitor',
    },
  ]
})

// ---- 趋势 ----
const activeTrend = computed(() => (trendDays.value === 7 ? trend7.value : trend30.value))
const trendPoints = computed(() =>
  (activeTrend.value?.points ?? []).map((p) => ({ date: p.date, value: p.count })),
)
/** E5：失败按原因拆分的条形（取前 3，宽度按占比）。 */
const failureBars = computed(() => {
  const rows = activeTrend.value?.failures?.by_reason ?? []
  if (!rows.length) return []
  const max = Math.max(...rows.map((r) => r.count), 1)
  return rows.slice(0, 3).map((r) => ({ ...r, pct: Math.round((r.count / max) * 100) }))
})

// ---- 监控 ----
const monitorUnavailable = computed(() => system.monitorUnavailable)

function netText(bps: number): string {
  const mbps = bps / 1e6
  if (mbps >= 1) return `${mbps.toFixed(1)} Mbps`
  return `${(bps / 1e3).toFixed(0)} Kbps`
}

// ---- 加载与轮询 ----
/**
 * 页面级数据（健康 / 系统信息 / 队列 / 凭据 / 直播源）。
 * 这些不在 system store 的契约里（docs/15 §3.1 只定统计/监控/日志/偏好），
 * 但它们变化慢，30s 刷一次足够——和 5s 的统计节奏绑在一起是白烧请求。
 */
async function loadPage() {
  const [h, i, q, c, l] = await Promise.all([
    api.health().catch(() => null),
    api.systemInfo().catch(() => null),
    api.queueStats().catch(() => null),
    api.listCredentials().catch(() => ({ items: [] as Credential[] })),
    api.listLiveSources().catch(() => ({ items: [] as LiveSource[] })),
  ])
  health.value = h
  info.value = i
  queue.value = q
  creds.value = c.items ?? []
  liveSources.value = l.items ?? []
}

async function load() {
  pageLoading.value = true
  errorMsg.value = ''
  try {
    await Promise.all([loadPage(), loadLayout(), system.loadAll()])
    if (system.error) errorMsg.value = system.error
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    pageLoading.value = false
  }
}

let pageTimer: number | undefined

onMounted(async () => {
  await load()
  // 统计 / 趋势 / 监控的节奏由 store 统一管（5s / 60s / 2s，页面不可见时跳过）
  system.startPolling({ monitor: true })
  pageTimer = window.setInterval(() => {
    if (!document.hidden) void loadPage()
  }, 30_000)
})

onBeforeUnmount(() => {
  system.stopPolling()
  if (pageTimer) window.clearInterval(pageTimer)
})

// ---- 快速下载（页内入口，与顶栏共用 store 的 submit） ----
const quickUrl = ref('')
const quickErr = ref('')
const quickBusy = ref(false)

async function quickSubmit(value: string) {
  const target = value.trim()
  if (!target) return
  quickBusy.value = true
  quickErr.value = ''
  try {
    await tasks.submit(target)
    quickUrl.value = ''
    toast.success('已加入下载队列')
  } catch (e) {
    quickErr.value = e instanceof Error ? e.message : '提交失败'
  } finally {
    quickBusy.value = false
  }
}

async function retryTask(id: string) {
  try {
    await tasks.retry(id)
    toast.success('已重新排队')
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '重试失败')
  }
}

function goto(to: string) {
  router.push(to)
}
</script>

<template>
  <section class="overview">
    <div v-if="errorMsg" class="error-card" role="alert">
      <p>加载失败：{{ errorMsg }}</p>
      <RButton size="sm" @click="load">重试</RButton>
    </div>

    <template v-else>
      <!-- 1) 快速下载引导卡：主页内的入口镜像（顶栏随时可贴，这里给第一次来的人一个明确起点） -->
      <article class="card quick-card">
        <div class="quick-head">
          <h2>粘贴链接就能下</h2>
          <p class="quick-hint">顶栏随时可贴；这里也可以。支持视频、音乐、播放列表与收藏夹。</p>
        </div>
        <QuickSubmit
          v-model="quickUrl"
          input-id="overview-submit"
          :submitting="quickBusy"
          :error="quickErr"
          actions-layout="row"
          button-text="开始下载"
          @submit="quickSubmit"
        />
      </article>

      <!-- 2–4) 指标行：12 张卡由注册表生成（E1 可隐藏/排序） -->
      <div class="cards">
        <MetricCard
          v-for="c in visibleCards"
          :key="c.key"
          :label="c.label"
          :value="c.value"
          :hint="c.hint"
          :tone="c.tone"
          :icon="c.icon"
          :to="c.to"
          :loading="loading && summary === null"
          @open="goto"
        />
        <article v-if="!loading" class="card customize-card">
          <h2>自定义</h2>
          <p class="customize-hint">隐藏不需要的卡片，或调整它们的顺序。设置会保存到账号，换设备也生效。</p>
          <RButton size="sm" variant="ghost" @click="openCustom">调整主页布局</RButton>
        </article>
      </div>

      <!-- 5) 活跃任务（Raccoon 自有：正在搬的活） -->
      <ActiveTasks :tasks="activeTasks" :loading="loading" :has-done="hasDone" @more="goto('/tasks')" />

      <!-- 系统状态条 -->
      <StatusStrip :items="stripItems" />

      <!-- 直播录制卡 -->
      <article class="card live-card">
        <header class="card-head">
          <h2>直播录制</h2>
          <RButton variant="ghost" size="sm" @click="goto('/live')">去直播页 →</RButton>
        </header>
        <p v-if="recordingSources.length === 0" class="muted">当前没有在录的直播间。</p>
        <ul v-else class="live-list">
          <li v-for="s in recordingSources" :key="s.id">
            <span class="live-dot" aria-hidden="true" />
            <span class="live-title">{{ s.title || s.room_url }}</span>
            <span class="live-state">录制中</span>
          </li>
        </ul>
      </article>

      <!-- 6) 监控带：三环 + 带宽，点击进监控页 -->
      <article class="card monitor-band">
        <header class="card-head">
          <h2>资源监控</h2>
          <RButton variant="ghost" size="sm" @click="goto('/monitor')">看实时曲线 →</RButton>
        </header>

        <p v-if="monitorUnavailable" class="muted">
          {{ monitor?.message || '当前平台不支持资源监控' }}
        </p>
        <div v-else class="monitor-row">
          <DonutChart
            :percent="monitor?.cpu?.used_pct ?? null"
            center-label="CPU"
            tone="info"
            label="CPU 使用率"
            :size="112"
          />
          <DonutChart
            :percent="monitor?.memory?.used_pct ?? null"
            center-label="内存"
            tone="warning"
            label="内存使用率"
            :size="112"
            :legend="
              monitor?.memory
                ? [
                    { label: '已用', value: formatBytes(monitor.memory.used_bytes) },
                    { label: '空闲', value: formatBytes(monitor.memory.free_bytes) },
                  ]
                : []
            "
          />
          <DonutChart
            :percent="monitor?.disk?.used_pct ?? null"
            center-label="存储"
            :tone="(monitor?.disk?.used_pct ?? 0) >= 90 ? 'danger' : 'warning'"
            label="存储使用率"
            :size="112"
            :legend="
              monitor?.disk
                ? [
                    { label: '空闲', value: formatBytes(monitor.disk.free_bytes) },
                    { label: '总量', value: formatBytes(monitor.disk.total_bytes) },
                  ]
                : []
            "
          />
          <div class="net-col">
            <span class="net-label">网络</span>
            <span class="net-value tnum">↑ {{ netText(monitor?.net?.up_bps ?? 0) }}</span>
            <span class="net-value tnum">↓ {{ netText(monitor?.net?.down_bps ?? 0) }}</span>
          </div>
        </div>
      </article>

      <!-- 7) 下载趋势（7 / 30 天切换）+ 失败拆分 -->
      <article class="card trend-card">
        <header class="card-head">
          <h2>下载趋势</h2>
          <div class="trend-tabs" role="group" aria-label="趋势时间范围">
            <button
              type="button"
              class="trend-tab"
              :class="{ on: trendDays === 7 }"
              :aria-pressed="trendDays === 7"
              @click="trendDays = 7"
            >
              7 天
            </button>
            <button
              type="button"
              class="trend-tab"
              :class="{ on: trendDays === 30 }"
              :aria-pressed="trendDays === 30"
              @click="trendDays = 30"
            >
              30 天
            </button>
          </div>
        </header>

        <TrendChart
          :points="trendPoints"
          :label="`近 ${trendDays} 天下载趋势`"
          :format="(v: number) => `${v} 个`"
        />

        <div v-if="failureBars.length" class="fail-block">
          <h3 class="fail-title">失败原因（近 {{ trendDays }} 天）</h3>
          <ul class="fail-list">
            <li v-for="f in failureBars" :key="f.key">
              <span class="fail-key mono">{{ f.key }}</span>
              <span class="fail-bar" :style="{ width: f.pct + '%' }" aria-hidden="true" />
              <span class="fail-count tnum">{{ f.count }}</span>
            </li>
          </ul>
        </div>
      </article>

      <!-- 8) 需要处理的问题（含 E2 成功率预警） -->
      <IssuesPanel
        :tasks="failedTasks"
        :alerts="alerts"
        @retry="retryTask"
        @more="goto('/tasks')"
        @open="goto"
      />
    </template>

    <!-- E1 布局对话框 -->
    <RDialog
      :open="customOpen"
      title="调整主页布局"
      confirm-text="保存"
      @update:open="(v: boolean) => !v && (customOpen = false)"
      @confirm="saveCustom"
      @cancel="customOpen = false"
    >
      <ul class="layout-list">
        <li v-for="(key, idx) in draftOrder" :key="key">
          <label class="layout-item">
            <input
              type="checkbox"
              :checked="!draftHidden.includes(key)"
              @change="toggleDraft(key)"
            />
            <span>{{ CARD_LABELS[key] ?? key }}</span>
          </label>
          <span class="layout-move">
            <button type="button" :disabled="idx === 0" :aria-label="`上移 ${key}`" @click="moveDraft(key, -1)">
              ↑
            </button>
            <button
              type="button"
              :disabled="idx === draftOrder.length - 1"
              :aria-label="`下移 ${key}`"
              @click="moveDraft(key, 1)"
            >
              ↓
            </button>
          </span>
        </li>
      </ul>
    </RDialog>
  </section>
</template>

<style scoped>
.overview { display: flex; flex-direction: column; gap: 18px; }
.card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  padding: 20px 24px;
  box-shadow: var(--shadow-1);
}
.card-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 14px; }
.card h2 { font-size: var(--font-title-3); font-weight: var(--weight-strong); color: var(--text-1); }
/* 卡片网格：260px 下限（docs/14 §3.3），auto-fit 让空轨道塌陷。 */
.cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 18px; }
.quick-card { display: flex; flex-direction: column; gap: 12px; }
.quick-hint { margin: 4px 0 0; font-size: var(--font-body); color: var(--text-2); }
.customize-card { display: flex; flex-direction: column; gap: 8px; justify-content: center; }
.customize-hint { margin: 0; font-size: var(--font-aux); color: var(--text-3); }
.live-card { display: flex; flex-direction: column; }
.live-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 10px; }
.live-list li { display: flex; align-items: center; gap: 10px; font-size: var(--font-body); }
.live-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--danger); flex-shrink: 0; }
.live-title { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-1); }
.live-state { font-size: var(--font-aux); color: var(--danger); }
.monitor-row { display: flex; flex-wrap: wrap; align-items: center; gap: 28px; }
.net-col { display: flex; flex-direction: column; gap: 4px; }
.net-label { font-size: var(--font-aux); color: var(--text-3); }
.net-value { font-size: var(--font-title-3); color: var(--text-1); }
.trend-tabs { display: inline-flex; border: 1px solid var(--border); border-radius: var(--radius-ctrl); overflow: hidden; }
.trend-tab {
  padding: 6px 12px; border: none; background: var(--surface); color: var(--text-2);
  font-size: var(--font-aux); cursor: pointer;
}
.trend-tab + .trend-tab { border-left: 1px solid var(--border); }
.trend-tab.on { background: var(--accent); color: var(--on-accent); }
.fail-block { margin-top: 16px; }
.fail-title { font-size: var(--font-title-3); font-weight: var(--weight-strong); color: var(--text-1); margin-bottom: 8px; }
.fail-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
.fail-list li { display: flex; align-items: center; gap: 10px; font-size: var(--font-aux); }
.fail-key { color: var(--text-2); min-width: 140px; }
.fail-bar { height: 8px; border-radius: 999px; background: color-mix(in srgb, var(--danger) 55%, transparent); }
.fail-count { color: var(--text-1); }
.muted { margin: 0; color: var(--text-3); font-size: var(--font-body); }
.error-card {
  background: var(--surface); border: 1px solid var(--danger); border-radius: var(--radius-card);
  padding: 16px 20px; color: var(--danger); display: flex; align-items: center; gap: 12px;
}
.layout-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 8px; }
.layout-list li { display: flex; align-items: center; justify-content: space-between; gap: 12px; font-size: var(--font-body); }
.layout-item { display: flex; align-items: center; gap: 8px; cursor: pointer; }
.layout-move { display: inline-flex; gap: 4px; }
.layout-move button {
  width: 28px; height: 28px; border: 1px solid var(--border); border-radius: var(--radius-ctrl);
  background: var(--surface); color: var(--text-2); cursor: pointer;
}
.layout-move button:disabled { opacity: 0.4; cursor: not-allowed; }
</style>
