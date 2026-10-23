<script setup lang="ts">
// 监控页（docs/15 T05 / docs/14 §2.5）：实时快照 + 1h/24h 历史。
//
// 三条纪律：
// 1) 轮询一律交给 system store —— 页面自己 setInterval 会让「离开页面停轮询」这条
//    承诺在某个视图里悄悄失效（store 里已按 document.hidden 跳过后台标签页）。
// 2) 采集失败（series 里的 null）跳过该点，不当 0：把断线画成归零等于伪造一次
//    「CPU 掉到 0%」的事故现场，用户会按假曲线排查。
// 3) 非 Linux（ok=false）整页只留一句「当前平台不支持」，历史 Tab 与图表全部不渲染
//    —— 宁可少一块界面，也不画一条没有数据来源的线。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { api } from '@/api/client'
import { useSystemStore } from '@/stores/system'
import type { MonitorHistory } from '@/api/types'
import DonutChart from '@/components/charts/DonutChart.vue'
import TrendChart from '@/components/charts/TrendChart.vue'
import REmpty from '@/components/base/REmpty.vue'
import { formatBytes, formatSpeed, formatTime } from '@/utils/format'

const store = useSystemStore()

// ---- Tab ----
type TabKey = 'live' | '1h' | '24h'

const TABS: { value: TabKey; label: string }[] = [
  { value: 'live', label: '实时' },
  { value: '1h', label: '1 小时' },
  { value: '24h', label: '24 小时' },
]

const tab = ref<TabKey>('live')

// ---- 实时快照（store 内 2s 轮询） ----
const snapshot = computed(() => store.monitor)
/** 非 Linux：整页降级（store 已把「ok=false」收敛成一个 computed）。 */
const unavailable = computed(() => store.monitorUnavailable)

/** 首轮请求已回来但仍没有数据：接口不通，不能一直挂着「正在读取」。 */
const firstTryDone = ref(false)
const loadFailed = computed(() => firstTryDone.value && snapshot.value === null)

const sampledText = computed(() => sampledAt(snapshot.value?.sampled_at))

/**
 * 采样时刻补到秒：轮询是 2 秒一次，只显示到分钟的话，
 * 曲线停住时用户根本分不清是「卡住了」还是「刚好这一分钟没变化」。
 */
function sampledAt(ts: number | undefined): string {
  if (!ts) return '—'
  const sec = new Date(ts).getSeconds()
  return `${formatTime(ts)}:${String(sec).padStart(2, '0')}`
}

/**
 * 阈值分档（docs/15 T05 验收 #6）：<70 正常、70–85 偏高、>85 危险。
 * 分档只决定颜色，不决定文案——数值永远照实显示。
 */
type Tone = 'info' | 'warning' | 'danger' | 'neutral'

function toneOf(pct: number | null): Tone {
  if (pct === null) return 'neutral'
  if (pct > 85) return 'danger'
  if (pct >= 70) return 'warning'
  return 'info'
}

const cpuPct = computed(() => snapshot.value?.cpu?.used_pct ?? null)
const memPct = computed(() => snapshot.value?.memory?.used_pct ?? null)
const diskPct = computed(() => snapshot.value?.disk?.used_pct ?? null)

const memLegend = computed(() => {
  const m = snapshot.value?.memory
  if (!m) return []
  return [
    { label: '已用', value: formatBytes(m.used_bytes) },
    { label: '共', value: formatBytes(m.total_bytes) },
  ]
})

const diskLegend = computed(() => {
  const d = snapshot.value?.disk
  if (!d) return []
  return [
    { label: '剩余', value: formatBytes(d.free_bytes) },
    { label: '共', value: formatBytes(d.total_bytes) },
  ]
})

/**
 * 序列 → 折线点：null 直接跳过（采集失败），并保留原始下标做 x 轴文案，
 * 这样断点位置仍然可读，而不是把 null 折成 0 画出一个假的谷底。
 */
function seriesPoints(series: (number | null)[] | undefined): { date: string; value: number }[] {
  if (!series?.length) return []
  const out: { date: string; value: number }[] = []
  series.forEach((v, i) => {
    if (v === null || v === undefined || Number.isNaN(v)) return
    out.push({ date: String(i), value: v })
  })
  return out
}

const cpuPoints = computed(() => seriesPoints(snapshot.value?.series?.cpu))
const upPoints = computed(() => seriesPoints(snapshot.value?.series?.net_up))
const downPoints = computed(() => seriesPoints(snapshot.value?.series?.net_down))

const cpuFmt = (v: number) => `${v.toFixed(0)}%`

// ---- 历史（E3：1h / 24h） ----
const history = ref<MonitorHistory | null>(null)
const historyLoading = ref(false)
const historyError = ref('')

/** ts → HH:MM：历史窗口最长 24h，日期冗余，时刻才是判断点密度的信息。 */
function clock(ts: number): string {
  const d = new Date(ts)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

const historyCpu = computed(() =>
  (history.value?.points ?? []).map((p) => ({ date: clock(p.ts), value: p.cpu })),
)
const historyMem = computed(() =>
  (history.value?.points ?? []).map((p) => ({ date: clock(p.ts), value: p.memory })),
)
const historyCount = computed(() => history.value?.points.length ?? 0)

/** 粒度说明：后端给秒，读的人只关心「一个点隔多久」。 */
const granularityText = computed(() => {
  const g = history.value?.granularity_sec
  if (!g) return ''
  if (g >= 60 && g % 60 === 0) return `每 ${g / 60} 分钟一个点`
  return `每 ${g} 秒一个点`
})

async function loadHistory(range: '1h' | '24h'): Promise<void> {
  historyLoading.value = true
  historyError.value = ''
  // 先清空：否则切到 24h 的头几百毫秒里，界面会拿 1h 的点冒充 24h。
  history.value = null
  try {
    history.value = await api.monitorHistory(range)
  } catch (e) {
    historyError.value = e instanceof Error ? e.message : '历史数据加载失败'
  } finally {
    historyLoading.value = false
  }
}

function selectTab(next: TabKey): void {
  if (tab.value === next) return
  tab.value = next
  if (next !== 'live') void loadHistory(next)
}

// ---- 生命周期：轮询起停跟页面绑死 ----
onMounted(() => {
  // 先起轮询再首拉：两条路径同一个数据源，顺序不影响结果，
  // 但同步注册定时器能让「挂载即有轮询」这件事在测试里可断言。
  store.startPolling({ monitor: true })
  void store.refreshMonitor().finally(() => {
    firstTryDone.value = true
  })
})

onBeforeUnmount(() => {
  // 必须停：离开监控页还在 2s 打接口是纯浪费，且会污染其它页面的 store 快照。
  store.stopPolling()
})

const degradeHint = computed(
  () =>
    store.monitor?.message ||
    'CPU / 内存 / 磁盘采集依赖 Linux 的 /proc，当前平台无法提供这些数据。',
)
</script>

<template>
  <section class="monitor">
    <!-- 非 Linux：整页降级，不渲染任何图表与 Tab（没有数据来源就不画） -->
    <REmpty
      v-if="unavailable"
      face="surprise"
      title="当前平台不支持资源监控"
      :hint="degradeHint"
    />

    <template v-else>
      <div class="monitor-bar">
        <p class="freshness">
          每 2 秒刷新 · 最近一次采样
          <span class="tnum">{{ sampledText }}</span>
        </p>

        <div class="tabs" role="tablist" aria-label="监控时间范围">
          <button
            v-for="t in TABS"
            :key="t.value"
            type="button"
            role="tab"
            class="tab"
            :class="{ on: tab === t.value }"
            :aria-selected="tab === t.value"
            @click="selectTab(t.value)"
          >
            {{ t.label }}
          </button>
        </div>
      </div>

      <!-- 实时 -->
      <template v-if="tab === 'live'">
        <p v-if="loadFailed" class="state" role="alert">
          暂时读不到监控数据：接口没有响应。轮询仍在继续，恢复后会自动补上。
        </p>
        <p v-else-if="snapshot === null" class="state">正在读取资源快照…</p>

        <template v-else>
          <div class="donuts">
            <article class="card donut-card">
              <h2>CPU</h2>
              <DonutChart
                :percent="cpuPct"
                center-label="已用"
                :tone="toneOf(cpuPct)"
                label="CPU 使用率"
                :size="132"
              />
            </article>

            <article class="card donut-card">
              <h2>内存</h2>
              <DonutChart
                :percent="memPct"
                center-label="已用"
                :tone="toneOf(memPct)"
                label="内存使用率"
                :size="132"
                :legend="memLegend"
              />
            </article>

            <article class="card donut-card">
              <h2>存储</h2>
              <DonutChart
                :percent="diskPct"
                center-label="已用"
                :tone="toneOf(diskPct)"
                label="存储使用率"
                :size="132"
                :legend="diskLegend"
              />
            </article>
          </div>

          <!-- 带宽：数字 + sparkline（无轴，只给形状） -->
          <article class="card bw-card">
            <header class="card-head">
              <h2>带宽</h2>
              <span class="card-note">最近 {{ upPoints.length }} 次采样</span>
            </header>
            <div class="bw-grid">
              <div class="bw-item">
                <span class="bw-label">上行</span>
                <span class="bw-value tnum">{{ formatSpeed(snapshot.net?.up_bps) }}</span>
                <TrendChart
                  :points="upPoints"
                  :height="48"
                  :show-axis="false"
                  :format="formatSpeed"
                  label="上行带宽最近 60 次采样"
                />
              </div>
              <div class="bw-item">
                <span class="bw-label">下行</span>
                <span class="bw-value tnum">{{ formatSpeed(snapshot.net?.down_bps) }}</span>
                <TrendChart
                  :points="downPoints"
                  :height="48"
                  :show-axis="false"
                  :format="formatSpeed"
                  label="下行带宽最近 60 次采样"
                />
              </div>
            </div>
          </article>

          <!-- 60 点实时序列（主折线，带轴） -->
          <article class="card">
            <header class="card-head">
              <h2>CPU 实时序列</h2>
              <span class="card-note">{{ cpuPoints.length }} 个有效点 · 断点已跳过</span>
            </header>
            <TrendChart
              :points="cpuPoints"
              :height="150"
              :format="cpuFmt"
              label="CPU 使用率最近 60 次采样"
            />
          </article>
        </template>
      </template>

      <!-- 历史（E3） -->
      <template v-else>
        <p v-if="historyLoading" class="state">正在读取历史采样…</p>
        <p v-else-if="historyError" class="state state-err" role="alert">
          {{ historyError }}
        </p>
        <p v-else-if="historyCount === 0" class="state">这段时间没有采样记录</p>

        <template v-else>
          <p class="granularity">
            共 <span class="tnum">{{ historyCount }}</span> 个采样点 · {{ granularityText }}
          </p>

          <article class="card">
            <header class="card-head">
              <h2>CPU 使用率</h2>
            </header>
            <TrendChart :points="historyCpu" :height="150" :format="cpuFmt" label="CPU 使用率历史" />
          </article>

          <article class="card">
            <header class="card-head">
              <h2>内存使用率</h2>
            </header>
            <TrendChart :points="historyMem" :height="150" :format="cpuFmt" label="内存使用率历史" />
          </article>
        </template>
      </template>
    </template>
  </section>
</template>

<style scoped>
.monitor { display: flex; flex-direction: column; gap: 18px; }
.monitor-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
/* 数据新鲜度是正文说明，不是表头，走 aux 档（12px 只留给表头/徽章） */
.freshness { margin: 0; font-size: var(--font-aux); color: var(--text-3); }
.tabs { display: inline-flex; border: 1px solid var(--border); border-radius: var(--radius-ctrl); overflow: hidden; }
.tab {
  padding: 6px 14px;
  border: none;
  background: var(--surface);
  color: var(--text-2);
  font-size: var(--font-aux);
  cursor: pointer;
}
.tab + .tab { border-left: 1px solid var(--border); }
.tab.on { background: var(--accent); color: var(--on-accent); }
.tab:focus-visible { outline: 2px solid var(--info); outline-offset: -2px; }

.card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  padding: 20px 24px;
  box-shadow: var(--shadow-1);
}
.card-head { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; margin-bottom: 12px; }
.card h2 { font-size: var(--font-title-3); font-weight: var(--weight-strong); color: var(--text-1); }
.card-note { font-size: var(--font-aux); color: var(--text-3); }

/* 三环：240px 以下放不下「环 + 图例」，auto-fit 让少数据时不空出轨道 */
.donuts { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 18px; }
.donut-card { display: flex; flex-direction: column; align-items: center; gap: 12px; }
.donut-card h2 { align-self: flex-start; }

.bw-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 18px; }
.bw-item { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
.bw-label { font-size: var(--font-aux); color: var(--text-3); }
.bw-value { font-size: var(--font-title-2); color: var(--text-1); }

.state { margin: 0; font-size: var(--font-body); color: var(--text-3); }
.state-err { color: var(--danger); }
.granularity { margin: 0; font-size: var(--font-aux); color: var(--text-3); }
</style>
