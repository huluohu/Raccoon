<script setup lang="ts">
// 总览页（方案 §8）：现在什么情况、我要做什么。
//
// 卡片：活跃任务 / 队列 / 媒体库 / 连接 / 系统健康。
// 状态来源：SSE + /tasks 对账（前端不推算）；本页只做聚合展示。
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '@/api/client'
import { useTasksStore } from '@/stores/tasks'
import { useToastStore } from '@/stores/toast'
import { formatBytes } from '@/utils/format'
import type { Credential, HealthCheck, QueueStats, SystemInfo } from '@/api/types'
import RaccoonFace from '@/components/raccoon/RaccoonFace.vue'
import RButton from '@/components/base/RButton.vue'
import StatusDot from '@/components/task/StatusDot.vue'
import PlatformBadge from '@/components/task/PlatformBadge.vue'
import TaskProgress from '@/components/task/TaskProgress.vue'
import StatusStrip from '@/components/shell/StatusStrip.vue'
import type { StatusStripItem } from '@/components/shell/StatusStrip.vue'

const router = useRouter()
const tasks = useTasksStore()
const toast = useToastStore()

const info = ref<SystemInfo | null>(null)
const health = ref<HealthCheck | null>(null)
const stats = ref<QueueStats | null>(null)
const creds = ref<Credential[]>([])
const errorMsg = ref('')
const loading = ref(true)

/** 活跃任务（进行中的前 5 条）。 */
const activeTasks = computed(() =>
  tasks.items.filter((t) => ['waiting', 'parsing', 'downloading', 'post'].includes(t.status)).slice(0, 5),
)

/** 最近失败（供 Issues 面板）。 */
const failedTasks = computed(() => tasks.items.filter((t) => t.status === 'failed').slice(0, 5))

/** 是否有成功完成的记录：决定空态用 done（完成）还是 waiting（等待）表情。 */
const hasDone = computed(
  () => tasks.items.some((t) => t.status === 'done') || (stats.value?.counts.done ?? 0) > 0,
)

/** 凭据健康统计。 */
const credSummary = computed(() => {
  const items = creds.value
  return {
    total: items.length,
    active: items.filter((c) => c.status === 'active').length,
    problem: items.filter((c) => c.status === 'expired' || c.status === 'expiring').length,
  }
})

/**
 * 系统状态条（方案 §3 StatusStrip）。
 *
 * 严重度判定规则写死在这里，不散落到模板：
 *   磁盘 ≥90% → bad；≥75% → warn；否则 ok
 *   数据库 ok → ok，其它 → bad
 *   凭据有异常 → warn，全正常 → ok
 * 阈值不在模板里算——模板里写 `pct >= 90 ? 'bad' : ...` 会让「什么算危险」
 * 变成隐式知识，改阈值要翻模板。
 */
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
  if (db) {
    out.push({
      label: '数据库',
      value: db === 'ok' ? '正常' : String(db),
      tone: db === 'ok' ? 'ok' : 'bad',
    })
  }

  out.push({
    label: '实时连接',
    value: tasks.connected ? '已连接' : '重连中',
    tone: tasks.connected ? 'ok' : 'warn',
  })

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

function formatUptime(sec: number): string {
  if (sec < 60) return `${sec} 秒`
  if (sec < 3600) return `${Math.floor(sec / 60)} 分钟`
  if (sec < 86400) return `${Math.floor(sec / 3600)} 小时 ${Math.floor((sec % 3600) / 60)} 分`
  return `${Math.floor(sec / 86400)} 天 ${Math.floor((sec % 86400) / 3600)} 小时`
}

async function load() {
  loading.value = true
  errorMsg.value = ''
  try {
    const [i, h, s, c] = await Promise.all([
      api.systemInfo(),
      api.health(),
      api.queueStats(),
      api.listCredentials().catch(() => ({ items: [] as Credential[] })),
    ])
    info.value = i
    health.value = h
    stats.value = s
    creds.value = c.items ?? []
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loading.value = false
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

onMounted(load)
</script>

<template>
  <section class="overview">
    <h1 class="page-title">总览</h1>

    <div v-if="errorMsg" class="error-card" role="alert">
      <p>加载失败：{{ errorMsg }}</p>
      <RButton size="sm" @click="load">重试</RButton>
    </div>

    <template v-else>
      <!-- 活跃任务 -->
      <article class="card card--wide">
        <header class="card-head">
          <h2>正在搬的活</h2>
          <RButton variant="ghost" size="sm" @click="router.push('/tasks')">查看全部 →</RButton>
        </header>
        <div v-if="loading" class="skeleton-rows">
          <div v-for="n in 3" :key="n" class="skeleton-row" />
        </div>
        <div v-else-if="activeTasks.length === 0" class="inline-empty">
          <!-- 表情按状态取用（方案 §5）：手上有活干完且有成功记录 → done（眨眼笑），
               纯粹没活 → waiting（眼罩半搭，等着接单）。 -->
          <RaccoonFace :face="hasDone ? 'done' : 'waiting'" :size="52" />
          <div>
            <p class="inline-empty-title">{{ hasDone ? '活都搬完了，眼罩可以摘' : '眼罩搁下了，眼下没活' }}</p>
            <p class="inline-empty-hint">
              {{ hasDone ? '干完的都在媒体库里躺着，新活随时可以开工。' : '粘贴链接到顶栏就能开工，或者去订阅页挂个自动追更。' }}
            </p>
          </div>
        </div>
        <ul v-else class="task-list">
          <li v-for="t in activeTasks" :key="t.id" class="task-row">
            <div class="task-row-main">
              <div class="task-row-head">
                <StatusDot :status="t.status" :wait-reason="t.wait_reason" />
                <PlatformBadge :platform="t.platform" />
                <span class="task-title">{{ t.title || t.url }}</span>
              </div>
              <TaskProgress :task="t" />
            </div>
          </li>
        </ul>
      </article>

      <!-- 系统状态条（方案 §3 StatusStrip）：磁盘 / 数据库 / 实时连接 / 凭据健康。
           这些是「扫一眼」的指标，单独成条比塞在卡片里更容易被看到。 -->
      <StatusStrip :items="stripItems" />

      <!-- 指标卡 -->
      <div class="cards">
        <article class="card">
          <h2>队列</h2>
          <div v-if="stats" class="metric">
            <span class="metric-num tnum">{{ stats.counts.waiting + stats.counts.parsing + stats.counts.downloading + stats.counts.post }}</span>
            <span class="metric-label">进行中</span>
          </div>
          <p v-if="stats" class="muted tnum">
            共 {{ stats.total_tasks }} 个任务 · 已完成 {{ stats.counts.done }} · 失败 {{ stats.counts.failed }}
          </p>
          <p v-else class="muted">加载中…</p>
        </article>

        <article class="card">
          <h2>媒体库</h2>
          <div v-if="stats" class="metric">
            <span class="metric-num tnum">{{ stats.media_count }}</span>
            <span class="metric-label">个文件</span>
          </div>
          <p v-if="stats" class="muted tnum">总体积 {{ formatBytes(stats.media_bytes) }}</p>
          <p v-else class="muted">加载中…</p>
        </article>

        <article class="card">
          <h2>连接</h2>
          <div v-if="credSummary.total > 0" class="metric">
            <span class="metric-num tnum">{{ credSummary.active }}</span>
            <span class="metric-label">个可用凭据</span>
          </div>
          <p v-if="credSummary.problem > 0" class="warn-line">
            {{ credSummary.problem }} 个凭据需要处理（过期或即将过期）
          </p>
          <p v-else-if="credSummary.total === 0" class="muted">还没配平台凭据，公开内容可直接下载。</p>
          <p v-else class="muted">凭据状态正常。</p>
          <RButton variant="ghost" size="sm" @click="router.push('/connections')">去连接页 →</RButton>
        </article>

        <article class="card">
          <h2>系统</h2>
          <dl v-if="info" class="kv">
            <dt>版本</dt><dd class="tnum">{{ info.version }}</dd>
            <dt>运行</dt><dd class="tnum">{{ formatUptime(info.uptime) }}</dd>
            <dt>架构</dt><dd class="tnum">{{ info.arch }}</dd>
          </dl>
          <p v-else class="muted">加载中…</p>
        </article>
      </div>

      <!-- 需要处理的问题 -->
      <article v-if="failedTasks.length > 0" class="card card--wide issue-panel">
        <header class="card-head">
          <h2>有几单没成</h2>
          <RButton variant="ghost" size="sm" @click="router.push('/tasks')">全部任务 →</RButton>
        </header>
        <ul class="issue-list">
          <li v-for="t in failedTasks" :key="t.id" class="issue-row">
            <RaccoonFace face="failed" :size="32" />
            <div class="issue-main">
              <div class="issue-title">{{ t.title || t.url }}</div>
              <div class="issue-reason">
                <code>{{ t.error_code }}</code>
                <span>{{ t.error_message }}</span>
              </div>
            </div>
            <RButton size="sm" @click="retryTask(t.id)">重试</RButton>
          </li>
        </ul>
      </article>
    </template>
  </section>
</template>

<style scoped>
.page-title { font-size: var(--font-title-1); font-weight: var(--weight-title); margin-bottom: 20px; border-left: 4px solid var(--gold); padding-left: 12px; }
/* 卡片网格：minmax 抬到 280px（方案 §6「≥1280 双栏」的最小可读宽度），
   auto-fit 让空轨道塌陷——auto-fill 会给 2 个卡片也留 4 条轨道，右侧空一大片。 */
.cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 18px; margin-bottom: 18px; }
.card { background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-card); padding: 20px 24px; box-shadow: var(--shadow-1); }
.card--wide { margin-bottom: 18px; }
.card-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 14px; }
/* 卡片标题：18px（--font-title-3）。原来用 13.5px 的 aux，
   和正文同级，标题就「塌」成了普通文字，卡片没有层级——这是「界面很懒」的直接观感来源。 */
.card h2 { font-size: var(--font-title-3); font-weight: var(--weight-strong); color: var(--text-1); }
/* 键值对：正文档（16px）。dt 是字段名用次级色，dd 是值用主色。 */
.kv { display: grid; grid-template-columns: auto 1fr; gap: 6px 16px; margin: 0 0 10px; font-size: var(--font-body); }
.kv dt { color: var(--text-3); }
.kv dd { margin: 0; color: var(--text-1); }
.metric { display: flex; align-items: baseline; gap: 8px; margin-bottom: 6px; }
.metric-num { font-size: var(--font-display); font-weight: var(--weight-title); color: var(--text-1); line-height: 1.1; }
.metric-label { font-size: var(--font-aux); color: var(--text-3); }
/* 说明句：正文档（16px）。aux 档留给真正的辅助信息（时间戳、计数徽章）。 */
.muted { color: var(--text-3); font-size: var(--font-body); line-height: 1.7; }
.warn-line { color: var(--warning); font-size: var(--font-body); margin: 4px 0; }
.error-card { background: var(--surface); border: 1px solid var(--danger); border-radius: var(--radius-card); padding: 16px 20px; color: var(--danger); display: flex; align-items: center; gap: 12px; }
.task-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 14px; }
.task-row-main { display: flex; flex-direction: column; gap: 8px; }
.task-row-head { display: flex; align-items: center; gap: 8px; }
/* 任务标题是这一行的主角，正文档 16px。 */
.task-title { font-size: var(--font-body); color: var(--text-1); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.inline-empty { display: flex; align-items: center; gap: 16px; padding: 16px 0; }
.inline-empty-title { font-size: var(--font-title-3); color: var(--text-1); font-weight: var(--weight-strong); }
.inline-empty-hint { font-size: var(--font-aux); color: var(--text-3); margin-top: 4px; }
.skeleton-rows { display: flex; flex-direction: column; gap: 12px; }
.skeleton-row { height: 40px; border-radius: var(--radius-ctrl); background: var(--surface-2); }
.issue-panel { border-color: color-mix(in srgb, var(--danger) 40%, var(--border)); }
.issue-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 12px; }
.issue-row { display: flex; align-items: center; gap: 14px; }
.issue-main { flex: 1; min-width: 0; }
.issue-title { font-size: var(--font-body); color: var(--text-1); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.issue-reason { display: flex; gap: 8px; font-size: var(--font-aux); color: var(--text-3); margin-top: 4px; }
.issue-reason code { background: var(--surface-2); padding: 1px 5px; border-radius: 4px; color: var(--danger); }
</style>
