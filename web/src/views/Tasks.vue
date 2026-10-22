<script setup lang="ts">
// 下载主战场（docs/07 §3）：提交 → 解析预览 → 实时任务列表 → 详情抽屉。
// 状态与进度一律来自服务端（store 内 SSE 打补丁 + 全量对账），本页不推算业务状态。
//
// 父子任务（docs/05 §5）：父任务不显示自己的进度条（其进度是子项聚合值），
// 父卡只显示「已完成 X/Y」；展开后逐条列出子任务（状态点 + 进度条 + 标题）。
// 「重试失败项」一次调用 POST /tasks/batch（action=retry），只传失败的子任务 ID。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useShortcuts } from '@/composables/useShortcuts'
import { useTasksStore } from '@/stores/tasks'
import { useToastStore } from '@/stores/toast'
import type { ParsePreview, Task, TaskEvent, TaskFile, TaskStatus } from '@/api/types'
import { ApiError } from '@/api/types'
import { formatBytes, formatDuration } from '@/utils/format'
import RButton from '@/components/base/RButton.vue'
import RDialog from '@/components/base/RDialog.vue'
import RInput from '@/components/base/RInput.vue'
import REmpty from '@/components/base/REmpty.vue'
import DetailDrawer from '@/components/base/DetailDrawer.vue'
import PlatformBadge from '@/components/task/PlatformBadge.vue'
import TaskCard from '@/components/task/TaskCard.vue'
import TaskRow from '@/components/task/TaskRow.vue'
import TaskDetail from '@/components/task/TaskDetail.vue'

const router = useRouter()
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
const detailFiles = ref<TaskFile[]>([])

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

async function fetchDetailFiles(id: string): Promise<TaskFile[]> {
  const { api } = await import('@/api/client')
  const res = await api.taskDetail(id)
  return res.files ?? []
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

// ---- 新任务插入高亮（docs/07 §6 列表变更 180ms）----
/** 本轮新出现的任务 ID：SSE 推送 / 提交成功后列表里多出来的那几条。
 *  长列表里「刚进来的在哪」是最常遇到的眼力问题，用金色软底扫一遍指一下位置，
 *  600ms 后自动撤（.row-flash 由 styles/motion.css 提供，不在组件里另写时长）。 */
const flashIds = ref<Set<string>>(new Set())
let flashTimers: number[] = []
/** 首屏对账不算「新插入」：用户第一次打开页面时，整个列表都是「刚看到」而不是「刚到来」，
 *  全部打高亮等于没有高亮。第一次变化只用来播种，之后多出来的才算新任务。 */
let flashSeeded = false

watch(
  () => (tasks.items ?? []).map((t) => t.id).join(','),
  (cur, prev) => {
    const prevIds = new Set((prev ?? '').split(',').filter(Boolean))
    const added = cur.split(',').filter((id) => id && !prevIds.has(id))
    if (!flashSeeded) {
      flashSeeded = true
      return
    }
    if (!added.length) return
    const next = new Set(flashIds.value)
    added.forEach((id) => next.add(id))
    flashIds.value = next
    added.forEach((id) => {
      flashTimers.push(
        window.setTimeout(() => {
          const clean = new Set(flashIds.value)
          clean.delete(id)
          flashIds.value = clean
        }, 600),
      )
    })
  },
)

// ---- 多选（docs/07 §7：Shift+Space 多选、↑↓ 移动，供批量重试/取消使用）----
const selectedIds = ref<Set<string>>(new Set())
/** 当前可聚焦的行索引（↑↓ 移动用）。 */
const cursorIndex = ref(0)

// ---- 删除确认（docs/07 §4：破坏性决策走 RDialog，默认焦点「取消」）----
/** 待删除的任务；null = 对话框关闭。 */
const pendingDelete = ref<Task | null>(null)

function isSelected(id: string): boolean {
  return selectedIds.value.has(id)
}

/** 行点击选中：Shift 表示追加多选，普通点击切换单选。 */
function onSelect(t: Task, ev?: MouseEvent) {
  const next = new Set(selectedIds.value)
  if (ev?.shiftKey) next.add(t.id)
  else if (next.has(t.id)) next.delete(t.id)
  else next.add(t.id)
  selectedIds.value = next
}

function toggleSelect(id: string) {
  const next = new Set(selectedIds.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selectedIds.value = next
}

function gotoConnections() {
  detail.value = null
  router.push('/connections')
}

// ---- 快捷键（docs/07 §7）----
// `/` 优先聚焦本页提交框（下载主战场就在这），不在本页时退回顶栏快速提交框。
useShortcuts({
  focusSubmit: () => {
    const local = document.getElementById('task-url-input')
    ;(local ?? document.getElementById('quick-submit'))?.focus()
  },
  rows: () => visibleRoots.value.map((t) => ({ id: t.id, status: t.status })),
  cursorIndex,
  openRow: (id) => {
    const t = visibleRoots.value.find((r) => r.id === id)
    if (t) void openDetail(t)
  },
  toggleSelect,
  // Esc：先关详情；详情已关时清空多选。返回 false 表示还有下一层要处理。
  onEsc: () => {
    if (detail.value) {
      detail.value = null
      return false
    }
    if (selectedIds.value.size) {
      selectedIds.value = new Set()
      return false
    }
    return false
  },
  retryRow: (id) => void runAction(id, tasks.retry),
})

/** 确认删除：仅在用户于 RDialog 中明确确认后才真正删除。 */
async function confirmDelete() {
  const t = pendingDelete.value
  pendingDelete.value = null
  if (!t) return
  await runAction(t.id, tasks.remove)
}

const isActive = (t: Task) => ['waiting', 'parsing', 'downloading', 'post'].includes(t.status)

onMounted(async () => {
  await tasks.refresh()
  tasks.connect()
})

onBeforeUnmount(() => {
  flashTimers.forEach((t) => window.clearTimeout(t))
  flashTimers = []
  tasks.disconnect()
})
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
      <RInput
        v-model="url"
        id="task-url-input"
        type="url"
        label="任务链接"
        placeholder="粘贴视频 / 音频 / 合集链接，支持 B 站、YouTube 等 yt-dlp 可解析站点"
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

    <!-- 列表（父 → 子）。父任务用 TaskCard（子项聚合计数 + 重试失败项插槽），
         普通任务与子项用 TaskRow 紧凑行；选中态供 §7 键盘多选使用。 -->
    <div v-else class="list">
      <template v-for="t in visibleRoots" :key="t.id">
        <TaskCard
          v-if="isParent(t)"
          class="root-card"
          :class="{ on: detail?.id === t.id, 'row-flash': flashIds.has(t.id) }"
          :task="t"
          parent
          :child-stats="{
            total: statOf(t.id).total,
            failed: statOf(t.id).failed,
            done: statOf(t.id).done,
          }"
          :selected="isSelected(t.id)"
          @open="openDetail"
          @select="onSelect"
        >
          <template #actions>
            <RButton size="sm" variant="ghost" @click.stop="toggleExpand(t.id)">
              {{ isExpanded(t.id) ? '收起' : '展开子项' }}
            </RButton>
            <RButton
              size="sm"
              variant="ghost"
              tone="danger"
              :disabled="busyId === t.id"
              @click.stop="pendingDelete = t"
            >
              删除
            </RButton>
          </template>
          <template #retry-failed>
            <RButton size="sm" :disabled="busyId === t.id" @click.stop="retryFailedChildren(t)">
              重试失败项
            </RButton>
          </template>
        </TaskCard>

        <TaskRow
          v-else
          :task="t"
          :class="{ 'row-flash': flashIds.has(t.id) }"
          :selected="isSelected(t.id)"
          @open="openDetail"
          @select="onSelect"
        >
          <template #actions>
            <RButton
              v-if="isActive(t)"
              size="sm"
              variant="ghost"
              :disabled="busyId === t.id"
              @click.stop="runAction(t.id, tasks.cancel)"
            >
              取消
            </RButton>
            <RButton
              v-if="t.status === 'failed' || t.status === 'cancelled'"
              size="sm"
              :disabled="busyId === t.id"
              @click.stop="runAction(t.id, tasks.retry)"
            >
              重试
            </RButton>
            <RButton
              size="sm"
              variant="ghost"
              tone="danger"
              :disabled="busyId === t.id"
              @click.stop="pendingDelete = t"
            >
              删除
            </RButton>
          </template>
        </TaskRow>

        <!-- 子任务列表：父卡展开后逐条列出（状态点 + 迷你进度 + 标题） -->
        <ul v-if="isParent(t) && isExpanded(t.id)" class="children">
          <TaskRow
            v-for="c in shownChildren(t.id)"
            :key="c.id"
            :task="c"
            size="sm"
            :class="{ 'row-flash': flashIds.has(c.id) }"
            :selected="isSelected(c.id)"
            @open="openDetail"
            @select="onSelect"
          >
            <template #actions>
              <RButton
                v-if="isActive(c)"
                size="sm"
                variant="ghost"
                :disabled="busyId === c.id"
                @click.stop="runAction(c.id, tasks.cancel)"
              >
                取消
              </RButton>
              <RButton
                v-if="c.status === 'failed' || c.status === 'cancelled'"
                size="sm"
                :disabled="busyId === c.id"
                @click.stop="runAction(c.id, tasks.retry)"
              >
                重试
              </RButton>
            </template>
          </TaskRow>
        </ul>
      </template>
    </div>

    <!-- 删除确认框（docs/07 §4：破坏性决策走 RDialog；默认焦点在「取消」） -->
    <RDialog
      :open="pendingDelete !== null"
      title="删除任务？"
      danger
      confirm-text="删除"
      :description="pendingDelete ? `将删除「${pendingDelete.title || pendingDelete.url}」及其本地文件，此操作不可撤销。` : ''"
      @update:open="(v) => !v && (pendingDelete = null)"
      @confirm="confirmDelete"
      @cancel="pendingDelete = null"
    />

    <!-- 详情抽屉：五区（概览 / 进度 / 日志 / 错误与建议 / 操作），Cookie 过期一键跳 connections -->
    <DetailDrawer :open="detail !== null" title="任务详情" @close="closeDetail">
      <TaskDetail
        v-if="detail"
        :task="detail"
        :events="detailEvents"
        :files="detailFiles"
        :busy="busyId === detail.id"
        @goto-connections="gotoConnections"
        @goto-settings="router.push('/settings')"
        @retry="(t) => runAction(t.id, tasks.retry)"
        @cancel="(t) => runAction(t.id, tasks.cancel)"
        @remove="(t) => runAction(t.id, tasks.remove)"
      />
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

/* 提交区（RInput 自带标签常驻，按钮与输入框底对齐） */
.submit { display: flex; gap: 8px; flex-wrap: wrap; align-items: flex-end; }
.submit .rinput { flex: 1; min-width: 260px; }

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

/* 列表（父卡 TaskCard + 行 TaskRow 由各自组件负责样式，这里只管间距） */
.list { display: flex; flex-direction: column; gap: 8px; }
.root-card { width: 100%; }
.root-card.on { border-color: var(--gold); }

/* 子任务容器（父卡展开后） */
.children {
  list-style: none; margin: 0; padding: 8px 0 0 0; width: 100%;
  display: flex; flex-direction: column; gap: 6px; border-top: 1px solid var(--border);
}

</style>
