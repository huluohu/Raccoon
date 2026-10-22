<script setup lang="ts">
// 下载主战场（docs/07 §3）：提交 → 解析预览 → 实时任务列表 → 详情抽屉。
// 状态与进度一律来自服务端（store 内 SSE 打补丁 + 全量对账），本页不推算业务状态。
//
// 父子任务（docs/05 §5）：父任务不显示自己的进度条（其进度是子项聚合值），
// 父卡只显示「已完成 X/Y」；展开后逐条列出子任务（状态点 + 进度条 + 标题）。
// 「重试失败项」一次调用 POST /tasks/batch（action=retry），只传失败的子任务 ID。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useTasksStore } from '@/stores/tasks'
import { useToastStore } from '@/stores/toast'
import type { ParsePreview, Task, TaskEvent, TaskStatus } from '@/api/types'
import { ApiError } from '@/api/types'
import { formatBytes, formatDuration, formatRelative, formatTime, WAIT_REASON_TEXT } from '@/utils/format'
import RButton from '@/components/base/RButton.vue'
import REmpty from '@/components/base/REmpty.vue'
import DetailDrawer from '@/components/base/DetailDrawer.vue'
import StatusDot from '@/components/task/StatusDot.vue'
import TaskProgress from '@/components/task/TaskProgress.vue'
import PlatformBadge from '@/components/task/PlatformBadge.vue'

const tasks = useTasksStore()
const toast = useToastStore()

// ---- 单条提交 ----
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
    toast.success('已加入下载队列')
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
    toast.success('已加入下载队列')
  } catch (e) {
    parseErr.value = e instanceof ApiError ? e.message : '提交失败'
  } finally {
    submitting.value = false
  }
}

// ---- 批量导入（多行链接 → 拆行去重 → 逐条解析预览 → 普通入队 / 建父子任务）----

const batchOpen = ref(false)
const batchText = ref('')
const batchParsing = ref(false)
const batchSubmitting = ref(false)
const batchErr = ref('')

interface BatchItem {
  url: string
  preview: ParsePreview | null
  error: string
  quality: string
}

/** 批量条目（与 batchText 同步，保留已解析结果）。 */
const batchItems = ref<BatchItem[]>([])

/** 拆行 + 去重（保持出现顺序）。URL 内不含空白，按空白切分可兼容多行与空格混排。 */
function splitUrls(text: string): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const raw of text.split(/\s+/)) {
    const u = raw.trim()
    if (!u || seen.has(u)) continue
    seen.add(u)
    out.push(u)
  }
  return out
}

function syncBatchItems() {
  const urls = splitUrls(batchText.value)
  const prev = new Map(batchItems.value.map((i) => [i.url, i]))
  batchItems.value = urls.map((u) => prev.get(u) ?? { url: u, preview: null, error: '', quality: '' })
}

/** 已解析成功的条目（提交只用这些；解析失败的不静默丢弃，而是明确提示）。 */
const parsedItems = computed(() => batchItems.value.filter((i) => i.preview !== null))

async function onBatchParse() {
  if (batchParsing.value) return
  batchParsing.value = true
  batchErr.value = ''
  for (const item of batchItems.value) {
    if (item.preview || item.error) continue
    try {
      const res = await tasks.preview(item.url)
      item.preview = res.data
      const videoFormats = res.data.formats.filter((f) => f.quality !== 'audio')
      item.quality = videoFormats[0]?.quality ?? 'best'
    } catch (e) {
      item.error = e instanceof ApiError ? e.message : '解析失败'
    }
  }
  batchItems.value = [...batchItems.value]
  batchParsing.value = false
  const failed = batchItems.value.filter((i) => i.error).length
  if (failed) batchErr.value = `${failed} 个链接解析失败，未纳入本次提交`
}

async function onBatchSubmit(mode: 'normal' | 'parent') {
  if (batchSubmitting.value) return
  const items = parsedItems.value
  if (!items.length) {
    batchErr.value = '没有已解析成功的链接，请先解析预览'
    return
  }
  // 服务端上限：POST /tasks 最多 50 条，POST /tasks/parent 最多 200 条（backend task_handler.go）。
  const limit = mode === 'parent' ? 200 : 50
  if (items.length > limit) {
    batchErr.value = `本次有 ${items.length} 个链接，超过单次上限 ${limit} 条，请分批提交`
    return
  }
  batchSubmitting.value = true
  batchErr.value = ''
  const payload = items.map((i) => ({ url: i.url, quality: i.quality || 'best' }))
  try {
    if (mode === 'parent') {
      // 父任务创建走 POST /tasks/parent（docs/05 §5）：父卡聚合子进度。
      const res = await tasks.submitParent(payload, payload[0].quality)
      toast.success(`已创建批量任务，共 ${res.child_total} 个子项`)
    } else {
      // 普通任务走 POST /tasks。
      const res = await tasks.submitBatch(payload)
      const conflicts = res.conflicts?.length ?? 0
      if (conflicts) toast.warning(`已加入 ${res.data.length} 个，${conflicts} 个链接已在队列中`)
      else toast.success(`已加入 ${res.data.length} 个下载任务`)
    }
    batchText.value = ''
    batchItems.value = []
  } catch (e) {
    batchErr.value = e instanceof ApiError ? e.message : '批量提交失败'
  } finally {
    batchSubmitting.value = false
  }
}

/** 把单条预览（多半是合集）送进批量面板。 */
function pushPreviewToBatch() {
  if (!preview.value) return
  batchOpen.value = true
  batchText.value = batchText.value ? `${batchText.value}\n${preview.value.url}` : preview.value.url
  syncBatchItems()
  preview.value = null
  url.value = ''
}

watch(batchText, syncBatchItems)

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

function matchesFilter(t: Task): boolean {
  switch (filter.value) {
    case 'active':
      return ['waiting', 'parsing', 'downloading', 'post'].includes(t.status)
    case 'done':
      return t.status === 'done'
    case 'failed':
      return t.status === 'failed'
    case 'cancelled':
      return t.status === 'cancelled'
    default:
      return true
  }
}

// ---- 任务树（父 → 子）----
/** 全部任务。 */
const allTasks = computed<Task[]>(() => tasks.items ?? [])

/** parent_id → 子任务列表。 */
const childrenByParent = computed(() => {
  const m = new Map<string, Task[]>()
  for (const t of allTasks.value) {
    if (!t.parent_id) continue
    const arr = m.get(t.parent_id)
    if (arr) arr.push(t)
    else m.set(t.parent_id, [t])
  }
  return m
})

const knownIds = computed(() => new Set(allTasks.value.map((t) => t.id)))

/** 顶层任务：无父，或父不在当前列表里（孤儿子项，按顶层展示，不隐藏）。 */
const rootTasks = computed<Task[]>(() => allTasks.value.filter((t) => !t.parent_id || !knownIds.value.has(t.parent_id)))

function childrenOf(id: string): Task[] {
  return childrenByParent.value.get(id) ?? []
}

/** 父任务展开后展示的子项：按当前筛选过滤（全部时展示全部）。 */
function shownChildren(id: string): Task[] {
  const all = childrenOf(id)
  if (filter.value === 'all') return all
  return all.filter(matchesFilter)
}

/** 父任务判定：后端建父任务时 platform 固定为 "batch"（backend task_extra_handler.go），
 *  子任务的 platform 是按 URL 推断的平台名。列表里出现子项时也认作父。 */
function isParent(t: Task): boolean {
  return t.platform === 'batch' || childrenOf(t.id).length > 0
}

/** 顶层筛选：自身命中，或任一子项命中（否则「失败」筛选会漏掉父卡里的失败子项）。 */
const visibleRoots = computed<Task[]>(() =>
  rootTasks.value.filter((r) => matchesFilter(r) || childrenOf(r.id).some(matchesFilter)),
)

/** 展开状态（父任务 ID 集合）。 */
const expanded = ref<Set<string>>(new Set())

function isExpanded(id: string): boolean {
  return expanded.value.has(id)
}

function toggleExpand(id: string) {
  const next = new Set(expanded.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  expanded.value = next
}

/** 父卡聚合统计。
 *  - 总数/失败数：服务端上报的 child_total / child_failed（SSE 聚合帧 + 创建响应）优先；
 *  - 「已完成」数：只能从已加载的子项数出来（后端未下发 done 子项计数）；
 *  - 列表被截断时（子项没全加载到）如实标注，不假装数全了。 */
function parentStat(parent: Task, children: Task[]) {
  const meta = tasks.childStats[parent.id]
  const loadedTotal = children.length
  const loadedDone = children.filter((c) => c.status === 'done').length
  const loadedFailed = children.filter((c) => c.status === 'failed').length
  const total = Math.max(meta?.total ?? 0, loadedTotal)
  const failed = Math.max(meta?.failed ?? 0, loadedFailed)
  const truncated = (meta?.total ?? 0) > loadedTotal || (meta?.failed ?? 0) > loadedFailed
  return { done: loadedDone, total, failed, loadedTotal, loadedDone, loadedFailed, truncated }
}

/** 父卡统计（模板里多处用，算一次即可）。 */
const parentStats = computed(() => {
  const m = new Map<string, ReturnType<typeof parentStat>>()
  for (const r of rootTasks.value) {
    if (!isParent(r)) continue
    m.set(r.id, parentStat(r, childrenOf(r.id)))
  }
  return m
})

function statOf(id: string): ReturnType<typeof parentStat> {
  return (
    parentStats.value.get(id) ?? {
      done: 0,
      total: 0,
      failed: 0,
      loadedTotal: 0,
      loadedDone: 0,
      loadedFailed: 0,
      truncated: false,
    }
  )
}

/** 等待原因（StatusDot 只表达状态，等待原因另行说明）。 */
function waitReasonText(t: Task): string {
  if (t.status !== 'waiting' || !t.wait_reason) return ''
  return WAIT_REASON_TEXT[t.wait_reason] ?? ''
}

function countOf(key: FilterKey): number {
  const c = tasks.counts
  if (!c) return 0
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
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '操作失败')
  } finally {
    busyId.value = ''
  }
}

/** 父卡「重试失败项」：一次 batch 调用，只传失败的子任务 ID（docs/05 §5）。 */
async function retryFailedChildren(parent: Task) {
  const failedChildren = childrenOf(parent.id).filter((c) => c.status === 'failed')
  if (!failedChildren.length) {
    toast.warning('没有可重试的失败子项')
    return
  }
  busyId.value = parent.id
  try {
    const res = await tasks.batch('retry', failedChildren.map((c) => c.id))
    if (res.failed.length) {
      toast.warning(`已重试 ${res.succeeded.length} 项，${res.failed.length} 项失败：${res.failed[0].reason}`)
    } else {
      toast.success(`已重试 ${res.succeeded.length} 个失败子项`)
    }
    // 服务端上报的失败数大于已加载的失败子项数时，如实说明只重试了能看到的那些。
    const stat = statOf(parent.id)
    if (stat.failed > stat.loadedFailed) {
      toast.info(`服务端共 ${stat.failed} 项失败，当前列表只加载到 ${stat.loadedFailed} 项，已重试的部分如上`)
    }
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '重试失败项失败')
  } finally {
    busyId.value = ''
  }
}

const isActive = (t: Task) => ['waiting', 'parsing', 'downloading', 'post'].includes(t.status)

onMounted(async () => {
  await tasks.refresh()
  tasks.connect()
})

onBeforeUnmount(() => tasks.disconnect())
</script>

<template>
  <section class="page">
    <header class="page-head">
      <div>
        <h1 class="page-title">下载</h1>
        <p class="page-sub">粘贴链接 → 解析预览 → 加入队列，进度实时推送</p>
      </div>
      <div class="conn" :class="tasks.connected ? 'ok' : 'off'">
        <span class="dot" aria-hidden="true" />
        {{ tasks.connected ? '实时已连接' : '实时未连接' }}
      </div>
    </header>

    <p v-if="tasks.warning" class="alert">{{ tasks.warning }}</p>

    <!-- 提交区 -->
    <div class="submit">
      <input
        v-model="url"
        type="url"
        placeholder="粘贴视频 / 音频 / 合集链接，支持 B 站、YouTube 等 yt-dlp 可解析站点"
        aria-label="任务链接"
        @keyup.enter="onPreview"
      />
      <RButton :disabled="submitting || !url.trim()" @click="onPreview">解析预览</RButton>
      <RButton variant="accent" :disabled="submitting || !url.trim()" @click="onQuickSubmit">直接下载</RButton>
      <RButton variant="ghost" @click="batchOpen = !batchOpen">
        {{ batchOpen ? '收起批量导入' : '批量导入' }}
      </RButton>
    </div>
    <p v-if="parseErr" class="err">{{ parseErr }}</p>
    <p v-else-if="tasks.error" class="err">{{ tasks.error }}</p>

    <!-- 解析预览卡（单条） -->
    <article v-if="preview" class="card preview">
      <img
        v-if="preview.thumbnail"
        :src="preview.thumbnail"
        :alt="preview.title"
        class="thumb"
        referrerpolicy="no-referrer"
      />
      <div v-else class="thumb ph" aria-hidden="true" />
      <div class="pv-body">
        <p class="pv-title">{{ preview.title || preview.url }}</p>
        <p class="pv-meta">
          <PlatformBadge :platform="preview.platform" />
          <span v-if="preview.uploader">{{ preview.uploader }}</span>
          <span class="tnum">时长 {{ formatDuration(preview.duration) }}</span>
          <span v-if="preview.is_playlist" class="pill pill--warn">合集</span>
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
            {{ f.quality }}<em v-if="f.size">{{ formatBytes(f.size) }}</em>
          </button>
        </div>
      </div>
      <div class="pv-actions">
        <RButton variant="accent" :disabled="submitting" @click="onAddToDownload">加入下载</RButton>
        <RButton variant="ghost" @click="pushPreviewToBatch">加入批量</RButton>
        <RButton variant="ghost" @click="preview = null">取消</RButton>
      </div>
    </article>

    <!-- 批量导入面板 -->
    <section v-if="batchOpen" class="card batch">
      <header class="batch-head">
        <h2 class="card-title">批量导入</h2>
        <span class="muted">一行一个链接，自动拆行去重 · 共 {{ batchItems.length }} 个</span>
      </header>
      <textarea
        v-model="batchText"
        class="textarea"
        rows="4"
        spellcheck="false"
        placeholder="https://www.youtube.com/watch?v=...&#10;https://www.bilibili.com/video/BV..."
        aria-label="批量链接"
      />
      <div class="batch-ops">
        <RButton size="sm" :disabled="!batchItems.length || batchParsing" @click="onBatchParse">
          {{ batchParsing ? '解析中…' : '解析预览' }}
        </RButton>
        <RButton
          size="sm"
          variant="accent"
          :disabled="!parsedItems.length || batchSubmitting"
          @click="onBatchSubmit('normal')"
        >
          普通入队
        </RButton>
        <RButton
          size="sm"
          variant="accent"
          :disabled="!parsedItems.length || batchSubmitting"
          @click="onBatchSubmit('parent')"
        >
          建批量任务（父子）
        </RButton>
      </div>
      <p v-if="batchErr" class="err">{{ batchErr }}</p>

      <ul v-if="batchItems.length" class="batch-list">
        <li v-for="item in batchItems" :key="item.url" class="batch-item">
          <img
            v-if="item.preview?.thumbnail"
            :src="item.preview.thumbnail"
            :alt="item.preview.title"
            class="thumb sm"
            referrerpolicy="no-referrer"
            loading="lazy"
          />
          <div v-else class="thumb sm ph" aria-hidden="true" />
          <div class="bi-body">
            <p class="bi-title" :title="item.preview?.title || item.url">
              {{ item.preview?.title || item.url }}
            </p>
            <p class="bi-meta">
              <PlatformBadge v-if="item.preview" :platform="item.preview.platform" />
              <span v-if="item.preview" class="tnum">时长 {{ formatDuration(item.preview.duration) }}</span>
              <span v-if="item.preview?.is_playlist" class="pill pill--warn">合集</span>
              <span v-if="item.error" class="bi-err">{{ item.error }}</span>
              <span v-else-if="!item.preview" class="muted">待解析</span>
            </p>
            <div v-if="item.preview" class="qualities">
              <button
                v-for="f in item.preview.formats"
                :key="f.quality"
                type="button"
                class="q"
                :class="{ on: item.quality === f.quality }"
                @click="item.quality = f.quality"
              >
                {{ f.quality }}<em v-if="f.size">{{ formatBytes(f.size) }}</em>
              </button>
            </div>
          </div>
        </li>
      </ul>
      <p v-else class="muted">还没有链接。粘贴多行链接后会在这里逐条展示解析预览。</p>
    </section>

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
        {{ f.label }}<em class="tnum">{{ countOf(f.key) }}</em>
      </button>
    </div>

    <!-- 列表（父 → 子） -->
    <p v-if="tasks.loading && !tasks.items.length" class="muted">加载中…</p>

    <p v-if="tasks.listTruncated" class="alert">
      任务较多，当前只加载了最近 100 条；更早的父子任务未在列表中。
    </p>

    <REmpty
      v-else-if="!visibleRoots.length"
      face="waiting"
      title="这个筛选下还没有任务"
      hint="粘贴链接开始第一次下载；合集或频道用「批量导入」一次建一个父子任务。"
    />

    <ul v-else class="list">
      <li v-for="t in visibleRoots" :key="t.id" class="card row" :class="{ on: detail?.id === t.id }">
        <!-- 父任务卡（不显示自己的进度条：其进度是子项聚合值） -->
        <template v-if="isParent(t)">
          <div class="row-main" @click="openDetail(t)">
            <div class="row-head">
              <PlatformBadge platform="batch" />
              <StatusDot :status="t.status" />
              <span class="title">批量任务 · {{ statOf(t.id).total }} 项</span>
              <span v-if="waitReasonText(t)" class="muted">{{ waitReasonText(t) }}</span>
              <span class="muted tnum">{{ formatRelative(t.created_at) }}</span>
            </div>
            <p class="agg">
              <span class="agg-done tnum">
                已完成 {{ statOf(t.id).done }}/{{ statOf(t.id).total }}
              </span>
              <!-- 父卡不画进度条（其进度是子项聚合值）；只如实显示服务端下发的聚合百分比。 -->
              <span v-if="t.progress > 0" class="agg-pct tnum">· 聚合 {{ t.progress.toFixed(0) }}%</span>
              <span v-if="statOf(t.id).failed > 0" class="agg-failed tnum">
                · {{ statOf(t.id).failed }} 项失败
              </span>
              <span v-if="statOf(t.id).truncated" class="agg-note">
                （列表仅加载到 {{ statOf(t.id).loadedTotal }} 项子任务，失败数以服务端上报为准）
              </span>
            </p>
            <p v-if="t.error_message" class="err-line">{{ t.error_code }}：{{ t.error_message }}</p>
          </div>
          <div class="row-side">
            <RButton size="sm" variant="ghost" @click="toggleExpand(t.id)">
              {{ isExpanded(t.id) ? '收起' : '展开子项' }}
            </RButton>
            <RButton
              v-if="statOf(t.id).failed > 0"
              size="sm"
              :disabled="busyId === t.id"
              @click="retryFailedChildren(t)"
            >
              重试失败项
            </RButton>
            <RButton
              size="sm"
              variant="ghost"
              tone="danger"
              confirm="再点一次确认删除"
              :disabled="busyId === t.id"
              @click="runAction(t.id, tasks.remove)"
            >
              删除
            </RButton>
          </div>

          <!-- 子任务列表 -->
          <ul v-if="isExpanded(t.id)" class="children">
            <li v-for="c in shownChildren(t.id)" :key="c.id" class="child">
              <StatusDot :status="c.status" />
              <PlatformBadge :platform="c.platform" compact />
              <span class="child-title" :title="c.title || c.url">{{ c.title || c.url }}</span>
              <div class="child-prog">
                <TaskProgress :task="c" compact />
              </div>
              <span class="child-size tnum">{{ formatBytes(c.downloaded_size) }}</span>
              <div class="child-ops" @click.stop>
                <RButton
                  v-if="isActive(c)"
                  size="sm"
                  variant="ghost"
                  :disabled="busyId === c.id"
                  @click="runAction(c.id, tasks.cancel)"
                >
                  取消
                </RButton>
                <RButton
                  v-if="c.status === 'failed' || c.status === 'cancelled'"
                  size="sm"
                  :disabled="busyId === c.id"
                  @click="runAction(c.id, tasks.retry)"
                >
                  重试
                </RButton>
              </div>
            </li>
          </ul>
        </template>

        <!-- 普通任务卡 -->
        <template v-else>
          <img
            v-if="t.thumbnail"
            :src="t.thumbnail"
            :alt="t.title || t.url"
            class="thumb sm"
            referrerpolicy="no-referrer"
            loading="lazy"
          />
          <div v-else class="thumb sm ph" aria-hidden="true" />
          <div class="row-main" @click="openDetail(t)">
            <div class="row-head">
              <StatusDot :status="t.status" />
              <PlatformBadge :platform="t.platform" />
              <span class="title" :title="t.title || t.url">{{ t.title || t.url }}</span>
            </div>
            <p class="row-meta">
              <span class="tnum">{{ t.quality }}</span>
              <span class="muted tnum">{{ formatRelative(t.created_at) }}</span>
              <span v-if="t.attempt > 0" class="muted tnum">
                第 {{ t.attempt + 1 }}/{{ t.max_attempts }} 次
              </span>
            </p>
            <TaskProgress v-if="isActive(t)" :task="t" />
            <p v-if="t.error_message" class="err-line">{{ t.error_code }}：{{ t.error_message }}</p>
          </div>
          <div class="row-side">
            <span class="size tnum">{{ formatBytes(t.downloaded_size) }}</span>
            <div class="row-ops">
              <RButton
                v-if="isActive(t)"
                size="sm"
                variant="ghost"
                :disabled="busyId === t.id"
                @click="runAction(t.id, tasks.cancel)"
              >
                取消
              </RButton>
              <RButton
                v-if="t.status === 'failed' || t.status === 'cancelled'"
                size="sm"
                :disabled="busyId === t.id"
                @click="runAction(t.id, tasks.retry)"
              >
                重试
              </RButton>
              <RButton
                size="sm"
                variant="ghost"
                tone="danger"
                confirm="再点一次确认删除"
                :disabled="busyId === t.id"
                @click="runAction(t.id, tasks.remove)"
              >
                删除
              </RButton>
            </div>
          </div>
        </template>
      </li>
    </ul>

    <!-- 详情抽屉 -->
    <DetailDrawer :open="detail !== null" title="任务详情" @close="closeDetail">
      <template v-if="detail">
        <p class="d-title">{{ detail.title || detail.url }}</p>
        <dl class="kv">
          <dt>状态</dt><dd><StatusDot :status="detail.status" /></dd>
          <dt>平台</dt><dd><PlatformBadge :platform="detail.platform" /></dd>
          <dt>清晰度</dt><dd>{{ detail.quality }}</dd>
          <dt>引擎</dt><dd>{{ detail.engine || '—' }} {{ detail.engine_version || '' }}</dd>
          <dt>已下载</dt><dd class="tnum">{{ formatBytes(detail.downloaded_size) }}</dd>
          <dt>创建</dt><dd class="tnum">{{ formatTime(detail.created_at) }}</dd>
          <template v-if="detail.finished_at">
            <dt>完成</dt><dd class="tnum">{{ formatTime(detail.finished_at) }}</dd>
          </template>
          <dt>链接</dt>
          <dd>
            <a :href="detail.url" target="_blank" rel="noreferrer noopener" class="link">{{ detail.url }}</a>
          </dd>
        </dl>

        <template v-if="childrenOf(detail.id).length">
          <h3 class="d-sub">子任务（{{ childrenOf(detail.id).length }}）</h3>
          <ul class="d-children">
            <li v-for="c in childrenOf(detail.id)" :key="c.id">
              <StatusDot :status="c.status" />
              <span class="child-title" :title="c.title || c.url">{{ c.title || c.url }}</span>
            </li>
          </ul>
        </template>

        <h3 class="d-sub">事件</h3>
        <ol class="timeline">
          <li v-for="(e, i) in detailEvents" :key="i" :data-level="e.level">
            <span class="ts tnum">{{ formatTime(e.ts) }}</span>
            <span class="msg">{{ e.message }}</span>
          </li>
          <li v-if="!detailEvents.length" class="muted">暂无事件</li>
        </ol>

        <template v-if="detailFiles.length">
          <h3 class="d-sub">产物</h3>
          <ul class="files">
            <li v-for="f in detailFiles" :key="f.path">
              <span class="fname" :title="f.path">{{ f.path.split('/').pop() }}</span>
              <span class="muted tnum">{{ formatBytes(f.size) }}</span>
            </li>
          </ul>
        </template>
      </template>
    </DetailDrawer>
  </section>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 14px; }
.page-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.page-title { margin: 0; font-size: 24px; font-weight: 750; border-left: 4px solid var(--gold); padding-left: 12px; }
.page-sub { margin: 6px 0 0 16px; font-size: var(--font-aux); color: var(--text-2); }
.conn {
  display: flex; align-items: center; gap: 6px; font-size: var(--font-caption); color: var(--text-2);
  padding: 4px 10px; border: 1px solid var(--border); border-radius: var(--radius-pill); background: var(--surface);
}
.conn .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--neutral); }
.conn.ok .dot { background: var(--success); }
.conn.off .dot { background: var(--danger); }
.alert {
  margin: 0; padding: 8px 12px; border-radius: var(--radius-ctrl);
  background: var(--gold-soft); color: var(--gold-strong); font-size: var(--font-aux);
}
.err { margin: 0; color: var(--danger); font-size: var(--font-aux); }
.muted { color: var(--text-3); font-size: var(--font-caption); }
.tnum { font-variant-numeric: tabular-nums; }

/* 卡片通用 */
.card {
  background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-card);
  box-shadow: var(--shadow-1); padding: 12px 14px;
}
.card-title { margin: 0; font-size: var(--font-aux); font-weight: 600; color: var(--text-1); }

/* 提交区 */
.submit { display: flex; gap: 8px; flex-wrap: wrap; }
.submit input {
  flex: 1; min-width: 260px; padding: 10px 14px; border: 1px solid var(--border);
  border-radius: var(--radius-ctrl); background: var(--surface); color: var(--text-1); font-size: var(--font-body);
}
.submit input:focus { outline: 2px solid var(--gold); outline-offset: 1px; }

/* 预览卡 */
.preview { display: flex; gap: 14px; }
.thumb { width: 120px; height: 68px; object-fit: cover; border-radius: var(--radius-ctrl); background: var(--surface-3); flex-shrink: 0; }
.thumb.sm { width: 84px; height: 48px; }
.thumb.ph { display: flex; align-items: center; justify-content: center; color: var(--text-3); font-size: var(--font-caption); }
.pv-body { flex: 1; min-width: 0; }
.pv-title { margin: 0 0 6px; font-weight: 650; font-size: var(--font-body); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pv-meta { margin: 0 0 10px; display: flex; flex-wrap: wrap; gap: 10px; align-items: center; font-size: var(--font-caption); color: var(--text-2); }
.qualities { display: flex; flex-wrap: wrap; gap: 6px; }
.q {
  padding: 5px 10px; border: 1px solid var(--border); border-radius: var(--radius-ctrl);
  background: var(--surface); font-size: var(--font-caption); color: var(--text-2); cursor: pointer;
}
.q em { font-style: normal; margin-left: 6px; color: var(--text-3); }
.q.on { border-color: var(--gold); background: var(--gold-soft); color: var(--text-1); font-weight: 650; }
.pv-actions { display: flex; flex-direction: column; gap: 8px; justify-content: center; }
.pill { padding: 1px 8px; border-radius: var(--radius-pill); font-size: var(--font-caption); }
.pill--warn { background: var(--gold-soft); color: var(--gold-strong); }

/* 批量导入 */
.batch { display: flex; flex-direction: column; gap: 10px; }
.batch-head { display: flex; align-items: baseline; justify-content: space-between; gap: 10px; flex-wrap: wrap; }
.textarea {
  width: 100%; box-sizing: border-box; padding: 10px 12px; border: 1px solid var(--border);
  border-radius: var(--radius-ctrl); background: var(--surface); color: var(--text-1);
  font-size: var(--font-aux); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; resize: vertical;
}
.textarea:focus { outline: 2px solid var(--gold); outline-offset: 1px; }
.batch-ops { display: flex; gap: 8px; flex-wrap: wrap; }
.batch-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 8px; }
.batch-item {
  display: flex; gap: 10px; padding: 8px; background: var(--surface-2);
  border-radius: var(--radius-ctrl); border: 1px solid var(--border);
}
.bi-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; }
.bi-title { margin: 0; font-size: var(--font-aux); font-weight: 600; color: var(--text-1); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.bi-meta { margin: 0; display: flex; flex-wrap: wrap; gap: 8px; align-items: center; font-size: var(--font-caption); color: var(--text-2); }
.bi-err { color: var(--danger); }

/* 筛选 */
.filters { display: flex; gap: 6px; flex-wrap: wrap; }
.tab {
  padding: 6px 12px; border: 1px solid var(--border); border-radius: var(--radius-pill);
  background: var(--surface); color: var(--text-2); font-size: var(--font-aux); cursor: pointer;
}
.tab em { font-style: normal; margin-left: 6px; color: var(--text-3); font-size: var(--font-caption); }
.tab.on { background: var(--accent); border-color: var(--accent); color: #fff; }
.tab.on em { color: rgb(255 255 255 / 75%); }

/* 列表 */
.list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 8px; }
.row { display: flex; gap: 12px; flex-wrap: wrap; align-items: flex-start; }
.row.on { border-color: var(--gold); }
.row-main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 6px; cursor: pointer; }
.row-head { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.title { font-weight: 600; font-size: var(--font-body); color: var(--text-1); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.row-meta { margin: 0; display: flex; flex-wrap: wrap; gap: 10px; align-items: center; font-size: var(--font-caption); color: var(--text-2); }
.agg { margin: 0; font-size: var(--font-aux); color: var(--text-2); }
.agg-done { color: var(--text-1); font-weight: 600; }
.agg-pct { color: var(--text-3); }
.agg-failed { color: var(--danger); }
.agg-note { color: var(--text-3); font-size: var(--font-caption); }
.err-line { margin: 0; font-size: var(--font-caption); color: var(--danger); }
.row-side { display: flex; flex-direction: column; align-items: flex-end; gap: 6px; flex-shrink: 0; }
.row-ops { display: flex; gap: 6px; }
.size { font-size: var(--font-aux); color: var(--text-2); }

/* 子任务 */
.children {
  list-style: none; margin: 0; padding: 8px 0 0 0; width: 100%;
  display: flex; flex-direction: column; gap: 6px; border-top: 1px solid var(--border);
}
.child {
  display: flex; align-items: center; gap: 8px; padding: 6px 8px 6px 22px;
  background: var(--surface-2); border-radius: var(--radius-ctrl);
  border-left: 2px solid var(--border);
}
.child-title { flex: 1; min-width: 0; font-size: var(--font-aux); color: var(--text-1); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.child-prog { width: 140px; flex-shrink: 0; }
.child-size { font-size: var(--font-caption); color: var(--text-3); flex-shrink: 0; }
.child-ops { display: flex; gap: 4px; flex-shrink: 0; }

/* 抽屉 */
.d-title { margin: 0 0 12px; font-weight: 650; }
.d-sub { margin: 18px 0 8px; font-size: var(--font-aux); color: var(--text-2); }
.kv { display: grid; grid-template-columns: 72px 1fr; gap: 6px 12px; margin: 0; font-size: var(--font-aux); }
.kv dt { color: var(--text-3); }
.kv dd { margin: 0; word-break: break-all; }
.link { color: var(--info); }
.d-children { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
.d-children li { display: flex; gap: 8px; align-items: center; font-size: var(--font-caption); }
.timeline { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 8px; font-size: var(--font-caption); }
.timeline li { display: flex; gap: 10px; align-items: baseline; }
.timeline .ts { color: var(--text-3); flex-shrink: 0; }
.timeline li[data-level='error'] .msg { color: var(--danger); }
.timeline li[data-level='warn'] .msg { color: var(--warning); }
.files { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; font-size: var(--font-caption); }
.files li { display: flex; justify-content: space-between; gap: 10px; }
.fname { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
