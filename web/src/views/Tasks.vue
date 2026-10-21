<script setup lang="ts">
// 下载主战场（docs/07 §3）：提交 → 解析预览 → 实时任务列表 → 详情抽屉。
// 状态与进度一律来自服务端（store 内 SSE 打补丁 + 全量对账），本页不推算业务状态。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useTasksStore } from '@/stores/tasks'
import type { ParsePreview, Task, TaskEvent, TaskStatus } from '@/api/types'
import { ApiError } from '@/api/types'

const tasks = useTasksStore()

// ---- 提交 ----
const url = ref('')
const submitting = ref(false)
const parseErr = ref('')
const preview = ref<ParsePreview | null>(null)
const chosenQuality = ref('')

async function onPreview() {
  const target = url.value.trim()
  if (!target || submitting.value) return
  submitting.value = true
  parseErr.value = ''
  preview.value = null
  try {
    const res = await tasks.preview(target)
    preview.value = res.data
    // 默认选中最高档（后端 formats 已按分辨率降序，末位可能是 audio）。
    const videoFormats = res.data.formats.filter((f) => f.quality !== 'audio')
    chosenQuality.value = videoFormats[0]?.quality ?? 'best'
  } catch (e) {
    parseErr.value = e instanceof ApiError ? e.message : '解析失败'
  } finally {
    submitting.value = false
  }
}

async function onAddToDownload() {
  if (!preview.value) return
  submitting.value = true
  parseErr.value = ''
  try {
    await tasks.submit(preview.value.url, chosenQuality.value)
    preview.value = null
    url.value = ''
    tasks.refresh()
  } catch (e) {
    parseErr.value = e instanceof ApiError ? e.message : '加入下载失败'
  } finally {
    submitting.value = false
  }
}

async function onQuickSubmit() {
  const target = url.value.trim()
  if (!target || submitting.value) return
  submitting.value = true
  parseErr.value = ''
  try {
    await tasks.submit(target)
    url.value = ''
    preview.value = null
  } catch (e) {
    parseErr.value = e instanceof ApiError ? e.message : '提交失败'
  } finally {
    submitting.value = false
  }
}

// ---- 筛选 ----
type FilterKey = 'all' | 'active' | 'done' | 'failed' | 'cancelled'
const filter = ref<FilterKey>('all')

const FILTERS: { key: FilterKey; label: string }[] = [
  { key: 'all', label: '全部' },
  { key: 'active', label: '进行中' },
  { key: 'done', label: '已完成' },
  { key: 'failed', label: '失败' },
  { key: 'cancelled', label: '已取消' },
]

const visible = computed<Task[]>(() => {
  const all = tasks.items ?? []
  switch (filter.value) {
    case 'active':
      return all.filter((t) => ['waiting', 'parsing', 'downloading', 'post'].includes(t.status))
    case 'done':
      return all.filter((t) => t.status === 'done')
    case 'failed':
      return all.filter((t) => t.status === 'failed')
    case 'cancelled':
      return all.filter((t) => t.status === 'cancelled')
    default:
      return all
  }
})

function countOf(key: FilterKey): number | null {
  const c = tasks.counts
  if (!c) return null
  switch (key) {
    case 'all':
      return tasks.items?.length ?? 0
    case 'active':
      return (c.waiting ?? 0) + (c.parsing ?? 0) + (c.downloading ?? 0) + (c.post ?? 0)
    default:
      return c[key as TaskStatus] ?? 0
  }
}

// ---- 详情抽屉 ----
const detail = ref<Task | null>(null)
const detailEvents = ref<TaskEvent[]>([])
const detailFiles = ref<{ path: string; size: number }[]>([])

async function openDetail(t: Task) {
  detail.value = t
  detailEvents.value = []
  detailFiles.value = []
  try {
    const ev = await tasks.events(t.id)
    const files = await fetchDetailFiles(t.id)
    // 期间可能已切换到别的任务，丢弃过期响应。
    if (detail.value?.id !== t.id) return
    detailEvents.value = ev
    detailFiles.value = files
  } catch {
    // 详情失败不阻断主列表
  }
}

async function fetchDetailFiles(id: string) {
  const { api } = await import('@/api/client')
  const res = await api.taskDetail(id)
  return (res.files ?? []).map((f) => ({ path: f.path, size: f.size }))
}

function closeDetail() {
  detail.value = null
}

// ---- 行操作 ----
const busyId = ref('')
async function runAction(id: string, fn: (taskId: string) => Promise<void>) {
  busyId.value = id
  try {
    await fn(id)
  } catch {
    // 错误已在 store.error
  } finally {
    busyId.value = ''
  }
}

const isActive = (t: Task) => ['waiting', 'parsing', 'downloading', 'post'].includes(t.status)

// ---- 展示工具 ----
const STATUS_TEXT: Record<TaskStatus, string> = {
  waiting: '排队中',
  parsing: '解析中',
  downloading: '下载中',
  post: '处理中',
  done: '已完成',
  failed: '失败',
  cancelled: '已取消',
}

const WAIT_REASON: Record<string, string> = {
  auto_retry: '失败后自动重试排队',
  interrupted: '进程重启，重新排队',
  rate_limited: '平台限流退避',
  disk_low: '磁盘水位不足',
}

function statusText(t: Task): string {
  if (t.status === 'waiting' && t.wait_reason && WAIT_REASON[t.wait_reason]) return WAIT_REASON[t.wait_reason]
  return STATUS_TEXT[t.status]
}

function fmtBytes(n: number | null | undefined): string {
  if (!n || n <= 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i += 1
  }
  return `${v.toFixed(v >= 10 || i === 0 ? 0 : 1)} ${units[i]}`
}

function fmtSpeed(bps: number): string {
  if (!bps || bps <= 0) return ''
  return `${fmtBytes(bps)}/s`
}

function fmtDuration(sec: number | null | undefined): string {
  if (!sec || sec <= 0) return '—'
  const m = Math.floor(sec / 60)
  const s = Math.round(sec % 60)
  return m > 0 ? `${m} 分 ${s} 秒` : `${s} 秒`
}

function fmtTime(ms: number): string {
  const d = new Date(ms)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

function ago(ms: number): string {
  const diff = Date.now() - ms
  if (diff < 60_000) return '刚刚'
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)} 分钟前`
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)} 小时前`
  return `${Math.floor(diff / 86_400_000)} 天前`
}

onMounted(async () => {
  await tasks.refresh()
  tasks.connect()
})

onBeforeUnmount(() => tasks.disconnect())
</script>

<template>
  <section class="page">
    <header class="head">
      <div>
        <h1>下载</h1>
        <p class="sub">粘贴链接 → 解析预览 → 加入队列，进度实时推送</p>
      </div>
      <div class="conn" :class="tasks.connected ? 'ok' : 'off'">
        <span class="dot" aria-hidden="true" />
        {{ tasks.connected ? '实时已连接' : '实时未连接' }}
      </div>
    </header>

    <p v-if="tasks.warning" class="warn">{{ tasks.warning }}</p>

    <!-- 提交区 -->
    <div class="submit">
      <input
        v-model="url"
        type="url"
        placeholder="粘贴视频 / 音频 / 合集链接，支持 B 站、YouTube 等 yt-dlp 可解析站点"
        aria-label="任务链接"
        @keyup.enter="onPreview"
      />
      <button class="btn ghost" :disabled="submitting || !url.trim()" @click="onPreview">解析预览</button>
      <button class="btn primary" :disabled="submitting || !url.trim()" @click="onQuickSubmit">直接下载</button>
    </div>
    <p v-if="parseErr" class="err">{{ parseErr }}</p>
    <p v-else-if="tasks.error" class="err">{{ tasks.error }}</p>

    <!-- 解析预览卡 -->
    <div v-if="preview" class="preview">
      <img v-if="preview.thumbnail" :src="preview.thumbnail" :alt="preview.title" class="thumb" referrerpolicy="no-referrer" />
      <div v-else class="thumb ph" aria-hidden="true" />
      <div class="pv-body">
        <p class="pv-title">{{ preview.title || preview.url }}</p>
        <p class="pv-meta">
          <span class="tag">{{ preview.platform }}</span>
          <span v-if="preview.uploader">{{ preview.uploader }}</span>
          <span>时长 {{ fmtDuration(preview.duration) }}</span>
          <span v-if="preview.is_playlist" class="tag warn">合集</span>
        </p>
        <div class="qualities">
          <button
            v-for="f in preview.formats"
            :key="f.quality"
            type="button"
            class="q"
            :class="{ on: chosenQuality === f.quality }"
            @click="chosenQuality = f.quality"
          >
            {{ f.quality }}<em v-if="f.size">{{ fmtBytes(f.size) }}</em>
          </button>
        </div>
      </div>
      <div class="pv-actions">
        <button class="btn primary" :disabled="submitting" @click="onAddToDownload">加入下载</button>
        <button class="btn ghost" @click="preview = null">取消</button>
      </div>
    </div>

    <!-- 筛选 -->
    <div class="filters">
      <button
        v-for="f in FILTERS"
        :key="f.key"
        type="button"
        class="tab"
        :class="{ on: filter === f.key }"
        @click="filter = f.key"
      >
        {{ f.label }}<em>{{ countOf(f.key) ?? 0 }}</em>
      </button>
    </div>

    <!-- 列表 -->
    <p v-if="tasks.loading && !tasks.items.length" class="muted">加载中…</p>
    <p v-else-if="!visible.length" class="muted empty">这个筛选下还没有任务。粘贴链接开始第一次下载吧。</p>

    <ul v-else class="list">
      <li v-for="t in visible" :key="t.id" class="row" :class="{ on: detail?.id === t.id }" @click="openDetail(t)">
        <img v-if="t.thumbnail" :src="t.thumbnail" :alt="t.title || t.url" class="thumb sm" referrerpolicy="no-referrer" loading="lazy" />
        <div v-else class="thumb sm ph" aria-hidden="true">{{ t.platform.slice(0, 2).toUpperCase() }}</div>

        <div class="main">
          <p class="title" :title="t.title || t.url">{{ t.title || t.url }}</p>
          <p class="meta">
            <span class="tag">{{ t.platform }}</span>
            <span class="st" :data-status="t.status">{{ statusText(t) }}</span>
            <span>{{ t.quality }}</span>
            <span class="muted">{{ ago(t.created_at) }}</span>
            <span v-if="t.attempt > 0" class="muted">第 {{ t.attempt + 1 }}/{{ t.max_attempts }} 次</span>
          </p>
          <div v-if="isActive(t)" class="bar" role="progressbar" :aria-valuenow="Math.round(t.progress)" aria-valuemin="0" aria-valuemax="100">
            <div class="fill" :data-status="t.status" :style="{ width: Math.max(2, t.progress) + '%' }" />
          </div>
          <p v-if="t.error_message" class="err-line">{{ t.error_code }}：{{ t.error_message }}</p>
        </div>

        <div class="side">
          <span class="size">{{ fmtBytes(t.downloaded_size) }}</span>
          <span v-if="fmtSpeed(t.speed_bps)" class="speed">{{ fmtSpeed(t.speed_bps) }}</span>
          <div class="ops" @click.stop>
            <button v-if="isActive(t)" class="btn tiny ghost" :disabled="busyId === t.id" @click="runAction(t.id, tasks.cancel)">取消</button>
            <button v-if="t.status === 'failed' || t.status === 'cancelled'" class="btn tiny" :disabled="busyId === t.id" @click="runAction(t.id, tasks.retry)">重试</button>
            <button class="btn tiny ghost" :disabled="busyId === t.id" @click="runAction(t.id, tasks.remove)">删除</button>
          </div>
        </div>
      </li>
    </ul>

    <!-- 详情抽屉 -->
    <aside v-if="detail" class="drawer">
      <header>
        <h2>任务详情</h2>
        <button class="btn tiny ghost" @click="closeDetail">关闭</button>
      </header>
      <p class="d-title">{{ detail.title || detail.url }}</p>
      <dl class="kv">
        <dt>状态</dt><dd>{{ statusText(detail) }}</dd>
        <dt>平台</dt><dd>{{ detail.platform }}</dd>
        <dt>清晰度</dt><dd>{{ detail.quality }}</dd>
        <dt>引擎</dt><dd>{{ detail.engine || '—' }} {{ detail.engine_version || '' }}</dd>
        <dt>已下载</dt><dd>{{ fmtBytes(detail.downloaded_size) }}</dd>
        <dt>创建</dt><dd>{{ fmtTime(detail.created_at) }}</dd>
        <dt v-if="detail.finished_at">完成</dt>
        <dd v-if="detail.finished_at">{{ fmtTime(detail.finished_at) }}</dd>
        <dt>链接</dt>
        <dd><a :href="detail.url" target="_blank" rel="noreferrer noopener" class="link">{{ detail.url }}</a></dd>
      </dl>

      <h3>事件</h3>
      <ol class="timeline">
        <li v-for="(e, i) in detailEvents" :key="i" :data-level="e.level">
          <span class="ts">{{ fmtTime(e.ts) }}</span>
          <span class="msg">{{ e.message }}</span>
        </li>
        <li v-if="!detailEvents.length" class="muted">暂无事件</li>
      </ol>

      <template v-if="detailFiles.length">
        <h3>产物</h3>
        <ul class="files">
          <li v-for="f in detailFiles" :key="f.path">
            <span class="fname" :title="f.path">{{ f.path.split('/').pop() }}</span>
            <span class="muted">{{ fmtBytes(f.size) }}</span>
          </li>
        </ul>
      </template>
    </aside>
  </section>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 14px; }
.head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
h1 { margin: 0; font-size: 24px; font-weight: 750; border-left: 4px solid var(--gold); padding-left: 12px; }
.sub { margin: 6px 0 0 16px; font-size: var(--font-aux); color: var(--text-2); }
.conn { display: flex; align-items: center; gap: 6px; font-size: var(--font-caption); color: var(--text-2); padding: 4px 10px; border: 1px solid var(--border); border-radius: var(--radius-pill); background: var(--surface); }
.conn .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--neutral); }
.conn.ok .dot { background: var(--success); }
.conn.off .dot { background: var(--danger); }
.warn { margin: 0; padding: 8px 12px; border-radius: var(--radius-ctrl); background: var(--gold-soft); color: var(--gold-strong); font-size: var(--font-aux); }
.err { margin: 0; color: var(--danger); font-size: var(--font-aux); }

/* 提交区 */
.submit { display: flex; gap: 8px; }
.submit input { flex: 1; padding: 10px 14px; border: 1px solid var(--border); border-radius: var(--radius-ctrl); background: var(--surface); color: var(--text-1); font-size: var(--font-body); }
.submit input:focus { outline: 2px solid var(--gold); outline-offset: 1px; }
.btn { padding: 10px 16px; border: 1px solid var(--border); border-radius: var(--radius-ctrl); background: var(--surface-2); color: var(--text-1); font-size: var(--font-aux); font-weight: 600; cursor: pointer; white-space: nowrap; }
.btn.primary { background: var(--accent); border-color: var(--accent); color: #fff; }
.btn.ghost { background: var(--surface); color: var(--text-2); }
.btn.tiny { padding: 4px 10px; font-size: var(--font-caption); }
.btn:disabled { opacity: 0.55; cursor: not-allowed; }

/* 预览卡 */
.preview { display: flex; gap: 14px; padding: 14px; background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-card); box-shadow: var(--shadow-1); }
.thumb { width: 120px; height: 68px; object-fit: cover; border-radius: var(--radius-ctrl); background: var(--surface-3); flex-shrink: 0; }
.thumb.ph { display: flex; align-items: center; justify-content: center; color: var(--text-3); font-size: var(--font-caption); }
.pv-body { flex: 1; min-width: 0; }
.pv-title { margin: 0 0 6px; font-weight: 650; font-size: var(--font-body); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pv-meta { margin: 0 0 10px; display: flex; flex-wrap: wrap; gap: 10px; font-size: var(--font-caption); color: var(--text-2); }
.qualities { display: flex; flex-wrap: wrap; gap: 6px; }
.q { padding: 5px 10px; border: 1px solid var(--border); border-radius: var(--radius-ctrl); background: var(--surface); font-size: var(--font-caption); color: var(--text-2); cursor: pointer; }
.q em { font-style: normal; margin-left: 6px; color: var(--text-3); }
.q.on { border-color: var(--gold); background: var(--gold-soft); color: var(--text-1); font-weight: 650; }
.pv-actions { display: flex; flex-direction: column; gap: 8px; justify-content: center; }

/* 筛选 */
.filters { display: flex; gap: 6px; flex-wrap: wrap; }
.tab { padding: 6px 12px; border: 1px solid var(--border); border-radius: var(--radius-pill); background: var(--surface); color: var(--text-2); font-size: var(--font-aux); cursor: pointer; }
.tab em { font-style: normal; margin-left: 6px; color: var(--text-3); font-size: var(--font-caption); }
.tab.on { background: var(--accent); border-color: var(--accent); color: #fff; }
.tab.on em { color: rgb(255 255 255 / 75%); }

/* 列表 */
.list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 8px; }
.row { display: flex; gap: 12px; padding: 12px; background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-card); cursor: pointer; transition: border-color 0.15s; }
.row:hover { border-color: var(--accent); }
.row.on { border-color: var(--gold); }
.thumb.sm { width: 84px; height: 48px; }
.main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 6px; }
.title { margin: 0; font-weight: 600; font-size: var(--font-body); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.meta { margin: 0; display: flex; flex-wrap: wrap; gap: 10px; align-items: center; font-size: var(--font-caption); color: var(--text-2); }
.tag { padding: 1px 7px; border-radius: var(--radius-pill); background: var(--accent-soft); color: var(--accent); font-size: var(--font-caption); }
.tag.warn { background: var(--gold-soft); color: var(--gold-strong); }
.st { font-weight: 650; }
.st[data-status='done'] { color: var(--success); }
.st[data-status='failed'] { color: var(--danger); }
.st[data-status='downloading'], .st[data-status='parsing'], .st[data-status='post'] { color: var(--info); }
.st[data-status='waiting'], .st[data-status='cancelled'] { color: var(--neutral); }
.bar { height: 5px; border-radius: var(--radius-pill); background: var(--surface-3); overflow: hidden; }
.fill { height: 100%; background: var(--accent); border-radius: var(--radius-pill); transition: width 0.4s ease; }
.fill[data-status='downloading'] { background: var(--info); }
.fill[data-status='failed'] { background: var(--danger); }
.err-line { margin: 0; font-size: var(--font-caption); color: var(--danger); }
.side { display: flex; flex-direction: column; align-items: flex-end; justify-content: space-between; gap: 8px; flex-shrink: 0; }
.size { font-size: var(--font-aux); color: var(--text-2); font-variant-numeric: tabular-nums; }
.speed { font-size: var(--font-caption); color: var(--info); font-variant-numeric: tabular-nums; }
.ops { display: flex; gap: 6px; }
.muted { color: var(--text-3); }
.empty { padding: 32px; text-align: center; border: 1px dashed var(--border); border-radius: var(--radius-card); }

/* 抽屉 */
.drawer { position: sticky; top: 0; padding: 16px; background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-card); box-shadow: var(--shadow-1); }
.drawer header { display: flex; justify-content: space-between; align-items: center; }
.drawer h2 { margin: 0; font-size: var(--font-body); }
.drawer h3 { margin: 18px 0 8px; font-size: var(--font-aux); color: var(--text-2); }
.d-title { margin: 10px 0 12px; font-weight: 650; }
.kv { display: grid; grid-template-columns: 72px 1fr; gap: 6px 12px; margin: 0; font-size: var(--font-aux); }
.kv dt { color: var(--text-3); }
.kv dd { margin: 0; word-break: break-all; }
.link { color: var(--info); }
.timeline { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 8px; font-size: var(--font-caption); }
.timeline li { display: flex; gap: 10px; align-items: baseline; }
.timeline .ts { color: var(--text-3); flex-shrink: 0; font-variant-numeric: tabular-nums; }
.timeline li[data-level='error'] .msg { color: var(--danger); }
.timeline li[data-level='warn'] .msg { color: var(--warning); }
.files { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; font-size: var(--font-caption); }
.files li { display: flex; justify-content: space-between; gap: 10px; }
.fname { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>