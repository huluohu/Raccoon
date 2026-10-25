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
import { useTaskTree, type FilterKey } from '@/composables/useTaskTree'
import { useTasksStore } from '@/stores/tasks'
import { useToastStore } from '@/stores/toast'
import type { ParsePreview, Task, TaskEvent, TaskFile, TaskStatus } from '@/api/types'
import { ApiError } from '@/api/types'
import { errorText, formatBytes, formatDuration } from '@/utils/format'
import RButton from '@/components/base/RButton.vue'
import PageHeader from '@/components/base/PageHeader.vue'
import RDialog from '@/components/base/RDialog.vue'
import DetailDrawer from '@/components/base/DetailDrawer.vue'
import QuickSubmit from '@/components/shell/QuickSubmit.vue'
import PlatformBadge from '@/components/task/PlatformBadge.vue'
import TaskList from '@/components/task/TaskList.vue'
import TaskDetail from '@/components/task/TaskDetail.vue'
import DeleteFileDialog from '@/components/media/DeleteFileDialog.vue'

const router = useRouter()
const tasks = useTasksStore()
const toast = useToastStore()

// ---- 单条提交 ----
const url = ref('')
const submitting = ref(false)
const parseErr = ref('')
const preview = ref<ParsePreview | null>(null)
const chosenQuality = ref('')
/** 产物格式：''=源格式（默认）/ mp4 / mp3；post 阶段由 ffmpeg remux 或转码。 */
const chosenFormat = ref('')

async function onPreview(target?: string) {
  // 用 QuickSubmit 抛上来的链接（它已做过格式校验 + 分享文案提取），
  // 不要重新读 url.value —— 那是原始输入，可能还带着一整段分享文案。
  const link = (target ?? url.value).trim()
  if (!link || submitting.value) return
  submitting.value = true
  parseErr.value = ''
  preview.value = null
  try {
    const res = await tasks.preview(link)
    preview.value = res.data
    // 默认选中最高档（后端 formats 已按分辨率降序，末位可能是 audio）。
    const videoFormats = res.data.formats.filter((f) => f.quality !== 'audio')
    chosenQuality.value = videoFormats[0]?.quality ?? 'best'
  } catch (e) {
    parseErr.value = errorText(e, '解析失败')
  } finally {
    submitting.value = false
  }
}

async function onAddToDownload() {
  if (!preview.value) return
  submitting.value = true
  parseErr.value = ''
  try {
    await tasks.submit(preview.value.url, chosenQuality.value, chosenFormat.value)
    preview.value = null
    url.value = ''
    toast.success('已加入下载队列')
  } catch (e) {
    parseErr.value = errorText(e, '加入下载失败')
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
      item.error = errorText(e, '解析失败')
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
    batchErr.value = errorText(e, '批量提交失败')
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
const filter = ref<FilterKey>('all')

const FILTERS: { key: FilterKey; label: string }[] = [
  { key: 'all', label: '全部' },
  { key: 'active', label: '进行中' },
  { key: 'done', label: '已完成' },
  { key: 'failed', label: '失败' },
  { key: 'cancelled', label: '已取消' },
]

// ---- 任务树（父 → 子）----
// 树与筛选逻辑放在 composable 里：列表组件和键盘导航必须看同一份「可见行」，
// 两边各算一份会漂移（筛选变了、键盘光标还停在看不见的行上）。
const { visibleRoots, childrenOf, shownChildren, isParent, statOf } = useTaskTree({
  allTasks: computed<Task[]>(() => tasks.items ?? []),
  filter,
  childStats: computed(() => tasks.childStats),
})

/** 展开状态（父任务 ID 集合）。 */
const expanded = ref<Set<string>>(new Set())

function toggleExpand(id: string) {
  const next = new Set(expanded.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  expanded.value = next
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
    const files = await fetchDetail(t.id)
    // 期间可能已切换到别的任务，丢弃过期响应。
    if (detail.value?.id !== t.id) return
    detailEvents.value = ev
    detailFiles.value = files
  } catch {
    // 详情失败不阻断主列表
  }
}

/** 拉详情并同步最新任务体：列表行是快照，error/scheduled_at 等字段可能在打开期间已变
 *  （如重试等待期的 auto_retry 错误），详情区必须以 GET /tasks/{id} 的最新值为准。
 *  用 Object.assign 合并而非整体替换：detail 与列表行是同一对象，SSE patch 才能继续生效。 */
async function fetchDetail(id: string): Promise<TaskFile[]> {
  const { api } = await import('@/api/client')
  const res = await api.taskDetail(id)
  if (res.data && detail.value?.id === res.data.id) Object.assign(detail.value, res.data)
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
/** 置顶/取消置顶（仅 waiting）。 */
async function pinTask(t: Task) {
  try {
    await tasks.setPriority(t.id, (t.priority ?? 0) > 0 ? 0 : 1)
    toast.success((t.priority ?? 0) > 0 ? '已取消置顶' : '已置顶：下一个空位优先领取')
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '置顶失败')
  }
}

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

/**
 * 批量操作（方案 §3「筛选/批量工具条」的另一半）。
 *
 * 批量删除是破坏性决策 → 走 RDialog 二次确认（§4「仅破坏性决策」）；
 * 批量重试/取消直接执行，逐条报告结果（后端 batch 接口语义）。
 * 操作完成后清空选择——留着旧选择会让用户误以为还能再操作一次已处理的行。
 */
async function onBatch(action: 'cancel' | 'retry' | 'delete', ids: string[]) {
  if (!ids.length) return
  if (action === 'delete') {
    pendingBatchDelete.value = ids
    return
  }
  busyId.value = 'batch'
  try {
    const res = await tasks.batch(action, ids)
    if (res.failed?.length) {
      toast.warning(`已处理 ${res.succeeded?.length ?? 0} 项，${res.failed.length} 项失败：${res.failed[0].reason}`)
    } else {
      toast.success(`已处理 ${res.succeeded?.length ?? ids.length} 项`)
    }
    selectedIds.value = new Set()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '批量操作失败')
  } finally {
    busyId.value = ''
  }
}

/** 批量删除的待确认任务 ID（null = 不弹确认框）。 */
const pendingBatchDelete = ref<string[] | null>(null)

async function confirmBatchDelete() {
  const ids = pendingBatchDelete.value
  pendingBatchDelete.value = null
  if (!ids?.length) return
  busyId.value = 'batch'
  try {
    const res = await tasks.batch('delete', ids)
    if (res.failed?.length) {
      toast.warning(`已删除 ${res.succeeded?.length ?? 0} 项，${res.failed.length} 项失败：${res.failed[0].reason}`)
    } else {
      toast.success(`已删除 ${res.succeeded?.length ?? ids.length} 项`)
    }
    selectedIds.value = new Set()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '批量删除失败')
  } finally {
    busyId.value = ''
  }
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

/**
 * 删单个产物文件（详情抽屉里点「删除」触发）。
 *
 * 分两步：先只记下待删文件并弹共用确认框（DeleteFileDialog），确认后才真发请求。
 * 详情组件只发事件、不弹窗，破坏性决策的实现只此一处。
 *
 * 与「删任务记录」彻底分离：任务记录走 DELETE /tasks/{id}（软删，文件留在磁盘），
 * 这里走 DELETE /tasks/{id}/files/{fileId}（真删磁盘 + 索引）。
 * 成功后重拉产物列表——不能本地数组过滤，否则与服务端出现第二份真相。
 */
const pendingDetailFile = ref<TaskFile | null>(null)

async function confirmDeleteDetailFile() {
  const file = pendingDetailFile.value
  const t = detail.value
  pendingDetailFile.value = null
  if (!file || !t) return
  busyId.value = t.id
  try {
    const { api } = await import('@/api/client')
    await api.deleteTaskFile(t.id, file.id)
    detailFiles.value = await fetchDetail(t.id)
    toast.success('文件已删除')
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '文件删除失败')
  } finally {
    busyId.value = ''
  }
}

/** 产物文件名（取路径末段，确认框里展示给人看）。 */
function baseName(path: string): string {
  const parts = path.split('/')
  return parts[parts.length - 1] || path
}

onMounted(async () => {
  await tasks.refresh()
})

onBeforeUnmount(() => {
  flashTimers.forEach((t) => window.clearTimeout(t))
  flashTimers = []
  // 注意：这里**不能**调 tasks.disconnect()。
  // SSE 是全局单连接，生命周期归 App.vue 管（会话就绪时建、会话失效时断）。
  // 页面级视图擅自断开，离开本页后就没有任何人重连——
  // 侧栏的连接徽章会永远停在「重连中」，而服务端那条连接其实早断了。
})
</script>

<template>
  <section class="page">
    <PageHeader eyebrow="TASKS" title="下载" sub="粘贴链接 → 解析预览 → 加入队列，进度实时推送">
      <template #actions>
        <div class="conn" :class="tasks.connected ? 'ok' : 'off'">
          <span class="dot" aria-hidden="true" />
          {{ tasks.connected ? '实时已连接' : '实时未连接' }}
        </div>
      </template>
    </PageHeader>

    <p v-if="tasks.warning" class="alert">{{ tasks.warning }}</p>

    <!-- 解析预览区（方案 §3：/tasks 的关键组件是 TaskList + TaskDetail，
         提交入口是全局顶栏 QuickSubmit）。这里只放「先看看有什么清晰度再选」的
         独有能力——一步到位的直接下载走顶栏，不在页面里重复一个同功能按钮，
         否则用户会对着两个「下载」犹豫该点哪个。
         错误分两类：解析/列表这类页面级错误走下方 .err 段落（带重试上下文），
         提交框自身的校验错误交给 QuickSubmit 内联。 -->
    <div class="submit">
      <p class="submit-label">先解析看看清晰度（要直接下就把链接贴到顶栏）</p>
      <QuickSubmit
        v-model="url"
        input-id="task-url-input"
        actions-layout="row"
        placeholder="粘贴视频 / 音频 / 合集链接，支持 B 站、YouTube 等 yt-dlp 可解析站点"
        :submitting="submitting"
        @submit="onPreview"
      >
        <template #actions="{ submit, disabled }">
          <RButton variant="accent" :disabled="disabled" @click="submit">解析预览</RButton>
          <RButton variant="ghost" @click="batchOpen = !batchOpen">
            {{ batchOpen ? '收起批量导入' : '批量导入' }}
          </RButton>
        </template>
      </QuickSubmit>
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
        <!-- 产物格式：默认源格式零开销；MP4 优先无损 remux（不兼容才转码）、MP3 提取音轨。 -->
        <div class="fmt-row" role="radiogroup" aria-label="产物格式">
          <span class="fmt-label">格式</span>
          <button
            v-for="opt in [
              { v: '', label: '源格式' },
              { v: 'mp4', label: 'MP4' },
              { v: 'mp3', label: 'MP3' },
            ]"
            :key="opt.v"
            type="button"
            class="q fmt"
            :class="{ on: chosenFormat === opt.v }"
            @click="chosenFormat = opt.v"
          >
            {{ opt.label }}
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

    <!-- 任务列表（方案 §3 视图级组件 TaskList：筛选 + 批量工具条） -->
    <TaskList
      v-model:filter="filter"
      :roots="visibleRoots"
      :filters="FILTERS"
      :count-of="countOf"
      :loading="tasks.loading"
      :truncated="tasks.listTruncated"
      :selected-ids="selectedIds"
      :expanded-ids="expanded"
      :flash-ids="flashIds"
      :busy-id="busyId"
      :stat-of="statOf"
      :children-of="childrenOf"
      :shown-children="shownChildren"
      :is-parent="isParent"
      @open="openDetail"
      @select="onSelect"
      @toggle-expand="toggleExpand"
      @retry="(id) => runAction(id, tasks.retry)"
      @cancel="(id) => runAction(id, tasks.cancel)"
      @remove="(t) => (pendingDelete = t)"
      @retry-failed-children="retryFailedChildren"
      @pin="pinTask"
      @batch="onBatch"
    />

    <!-- 批量删除确认框（破坏性决策，§4：默认焦点在「取消」） -->
    <RDialog
      :open="pendingBatchDelete !== null"
      title="批量删除任务？"
      danger
      confirm-text="删除"
      :description="pendingBatchDelete ? `将删除选中的 ${pendingBatchDelete.length} 个任务记录，磁盘上的文件会保留。此操作不可撤销。` : ''"
      @update:open="(v) => !v && (pendingBatchDelete = null)"
      @confirm="confirmBatchDelete"
      @cancel="pendingBatchDelete = null"
    />

    <!-- 删除确认框（docs/07 §4：破坏性决策走 RDialog；默认焦点在「取消」） -->
    <RDialog
      :open="pendingDelete !== null"
      title="删除任务？"
      danger
      confirm-text="删除"
      :description="pendingDelete ? `将删除「${pendingDelete.title || pendingDelete.url}」的任务记录，磁盘上的文件会保留。要连文件一起删，请在详情里逐个删除。此操作不可撤销。` : ''"
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
        @retry-via="(t, kind) => runAction(t.id, (id) => tasks.retry(id, kind))"
        @retry-proxy="(t) => runAction(t.id, (id) => tasks.retry(id, undefined, { use_proxy: true }))"
        @cancel="(t) => runAction(t.id, tasks.cancel)"
        @remove="(t) => runAction(t.id, tasks.remove)"
        @delete-file="(f) => (pendingDetailFile = f)"
      />
    </DetailDrawer>

    <!-- 删产物确认框：与媒体库共用同一份文案（删哪个文件 / 多大 / 任务记录保留） -->
    <DeleteFileDialog
      :open="pendingDetailFile !== null"
      :file-name="pendingDetailFile ? baseName(pendingDetailFile.path) : ''"
      :size="pendingDetailFile?.size ?? 0"
      :path="pendingDetailFile?.path ?? ''"
      @update:open="(v: boolean) => !v && (pendingDetailFile = null)"
      @confirm="confirmDeleteDetailFile"
    />
  </section>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 18px; }
.conn {
  display: flex; align-items: center; gap: 6px; font-size: var(--font-caption); color: var(--text-2);
  padding: 4px 10px; border: 1px solid var(--border); border-radius: var(--radius-pill); background: var(--surface);
}
.conn .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--neutral); }
.conn.ok .dot { background: var(--success); }
.conn.off .dot { background: var(--danger); }
.alert {
  margin: 0; padding: 10px 14px; border-radius: var(--radius-ctrl);
  background: color-mix(in srgb, var(--warning) 12%, transparent);
  color: var(--warning); font-size: var(--font-body);
}
.err { margin: 0; color: var(--danger); font-size: var(--font-body); }
.muted { color: var(--text-3); font-size: var(--font-aux); }
.tnum { font-variant-numeric: tabular-nums; }

/* 卡片通用 */
.card {
  background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-card);
  box-shadow: var(--shadow-1); padding: 16px 20px;
}
/* 卡片标题 18px（--font-title-3）。原来 13.5px 和正文同级，标题没有层级。 */
.card-title { margin: 0; font-size: var(--font-title-2); font-weight: var(--weight-strong); color: var(--text-1); }

/* 提交区：QuickSubmit 组件（标签/输入/按钮一体）。
   输入框不随内容区无限拉宽：URL 输入 640px 足够展示长链接，
   再宽只会把按钮挤到屏幕尽头、整行比例失衡。 */
.submit { display: flex; flex-direction: column; gap: 8px; }
/* 区域说明：正文档 16px，一句话说清这块是干什么的（和顶栏的分工）。 */
.submit-label { margin: 0; font-size: var(--font-body); color: var(--text-2); }

/* 预览卡 */
.preview { display: flex; gap: 14px; }
.thumb { width: 120px; height: 68px; object-fit: cover; border-radius: var(--radius-ctrl); background: var(--surface-3); flex-shrink: 0; }
.thumb.sm { width: 84px; height: 48px; }
.thumb.ph { display: flex; align-items: center; justify-content: center; color: var(--text-3); font-size: var(--font-caption); }
.pv-body { flex: 1; min-width: 0; }
.pv-title { margin: 0 0 6px; font-weight: var(--weight-medium); font-size: var(--font-body); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pv-meta { margin: 0 0 10px; display: flex; flex-wrap: wrap; gap: 10px; align-items: center; font-size: var(--font-aux); color: var(--text-2); }
.qualities { display: flex; flex-wrap: wrap; gap: 6px; }
/* 格式选择行：与清晰度同一套按钮，前面加一个固定标签说明这排是什么。 */
.fmt-row { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
.fmt-label { font-size: var(--font-aux); color: var(--text-3); margin-right: 2px; }
/* 清晰度选项：可点按钮，辅助档 13.5px（12px 是给不可点的表头/徽章的）。 */
.q {
  padding: 6px 12px; border: 1px solid var(--border); border-radius: var(--radius-ctrl);
  background: var(--surface); font-size: var(--font-aux); color: var(--text-2); cursor: pointer;
}
.q em { font-style: normal; margin-left: 6px; color: var(--text-3); }
.q.on { border-color: var(--accent); background: var(--accent-soft); color: var(--text-1); font-weight: var(--weight-medium); }
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
.textarea:focus { outline: 2px solid var(--accent); outline-offset: 1px; }
.batch-ops { display: flex; gap: 8px; flex-wrap: wrap; }
.batch-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 8px; }
.batch-item {
  display: flex; gap: 10px; padding: 8px; background: var(--surface-2);
  border-radius: var(--radius-ctrl); border: 1px solid var(--border);
}
.bi-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; }
/* 批量导入条目的链接是用户要读的主信息，正文档 16px。 */
.bi-title { margin: 0; font-size: var(--font-body); font-weight: var(--weight-medium); color: var(--text-1); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.bi-meta { margin: 0; display: flex; flex-wrap: wrap; gap: 10px; align-items: center; font-size: var(--font-aux); color: var(--text-2); }
.bi-err { color: var(--danger); }

</style>
