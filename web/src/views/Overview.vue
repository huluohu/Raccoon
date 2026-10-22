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

/** 凭据健康统计。 */
const credSummary = computed(() => {
  const items = creds.value
  return {
    total: items.length,
    active: items.filter((c) => c.status === 'active').length,
    problem: items.filter((c) => c.status === 'expired' || c.status === 'expiring').length,
  }
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
          <RaccoonFace face="waiting" :size="48" />
          <div>
            <p class="inline-empty-title">眼罩搁下了，眼下没活</p>
            <p class="inline-empty-hint">粘贴链接到顶栏就能开工，或者去订阅页挂个自动追更。</p>
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
          <p v-if="health" class="muted">
            数据库 {{ health.checks.db === 'ok' ? '正常' : health.checks.db }}
            <template v-if="health.checks.disk_used_pct !== undefined">
              · 磁盘已用 <span class="tnum">{{ health.checks.disk_used_pct }}%</span>
            </template>
          </p>
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
.page-title { font-size: var(--font-title-1); font-weight: var(--weight-title); margin-bottom: 16px; border-left: 4px solid var(--gold); padding-left: 12px; }
.cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 16px; margin-bottom: 16px; }
.card { background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-card); padding: 16px 20px; box-shadow: var(--shadow-1); }
.card--wide { margin-bottom: 16px; }
.card-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 12px; }
.card h2 { font-size: var(--font-aux); font-weight: var(--weight-semibold); color: var(--text-3); }
.kv { display: grid; grid-template-columns: auto 1fr; gap: 4px 12px; margin: 0 0 8px; font-size: var(--font-aux); }
.kv dt { color: var(--text-3); }
.kv dd { margin: 0; color: var(--text-1); }
.metric { display: flex; align-items: baseline; gap: 6px; margin-bottom: 4px; }
.metric-num { font-size: var(--font-display); font-weight: var(--weight-title); color: var(--text-1); line-height: 1.1; }
.metric-label { font-size: var(--font-aux); color: var(--text-3); }
.muted { color: var(--text-3); font-size: var(--font-aux); }
.warn-line { color: var(--warning); font-size: var(--font-aux); margin: 4px 0; }
.error-card { background: var(--surface); border: 1px solid var(--danger); border-radius: var(--radius-card); padding: 16px 20px; color: var(--danger); display: flex; align-items: center; gap: 12px; }
.task-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 12px; }
.task-row-main { display: flex; flex-direction: column; gap: 6px; }
.task-row-head { display: flex; align-items: center; gap: 8px; }
.task-title { font-size: var(--font-aux); color: var(--text-1); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.inline-empty { display: flex; align-items: center; gap: 14px; padding: 12px 0; }
.inline-empty-title { font-size: var(--font-aux); color: var(--text-1); font-weight: var(--weight-caption); }
.inline-empty-hint { font-size: var(--font-caption); color: var(--text-3); margin-top: 2px; }
.skeleton-rows { display: flex; flex-direction: column; gap: 10px; }
.skeleton-row { height: 34px; border-radius: var(--radius-ctrl); background: var(--surface-2); }
.issue-panel { border-color: color-mix(in srgb, var(--danger) 40%, var(--border)); }
.issue-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 10px; }
.issue-row { display: flex; align-items: center; gap: 12px; }
.issue-main { flex: 1; min-width: 0; }
.issue-title { font-size: var(--font-aux); color: var(--text-1); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.issue-reason { display: flex; gap: 8px; font-size: var(--font-caption); color: var(--text-3); margin-top: 2px; }
.issue-reason code { background: var(--surface-2); padding: 1px 5px; border-radius: 4px; color: var(--danger); }
</style>
