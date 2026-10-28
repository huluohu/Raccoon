<script setup lang="ts">
// 媒体库（docs/07 §3 / docs/04 §2.8）：文件视角浏览、搜索、预览、下载。
//
// 诚实原则：不支持浏览器播放的编码直接标「不支持预览」并给下载按钮，不假装能播。
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { api } from '@/api/client'
import { useToastStore } from '@/stores/toast'
import { formatBytes, formatDuration, formatRelative, platformText } from '@/utils/format'
import RButton from '@/components/base/RButton.vue'
import PageHeader from '@/components/base/PageHeader.vue'
import REmpty from '@/components/base/REmpty.vue'
import RSelect from '@/components/base/RSelect.vue'
import RTable from '@/components/base/RTable.vue'
import RMenu from '@/components/base/RMenu.vue'
import PlatformBadge from '@/components/task/PlatformBadge.vue'
import FileCard from '@/components/media/FileCard.vue'
import PreviewDrawer from '@/components/media/PreviewDrawer.vue'
import DownloadBtn from '@/components/media/DownloadBtn.vue'
import DeleteFileDialog from '@/components/media/DeleteFileDialog.vue'
import CoverPickerDialog from '@/components/media/CoverPickerDialog.vue'
import MediaMetaDialog from '@/components/media/MediaMetaDialog.vue'
import { fileActions, type FileActionKey } from '@/components/media/fileActions'
import type { MediaFile } from '@/api/types'

const toast = useToastStore()

/** 单页条数：与后端 limit 语义一致，一页 100 个文件。 */
const PAGE_SIZE = 100

const items = ref<MediaFile[]>([])
const total = ref(0)
const platforms = ref<string[]>([])
const loading = ref(true)
/** 「加载更多」在途标记：与主加载分开，主列表不清空。 */
const loadingMore = ref(false)
/** 主加载失败（整页错误态）。 */
const errorMsg = ref('')
/** 加载更多失败：只提示，不清空已经拿到的列表。 */
const moreErrorMsg = ref('')
const query = ref('')
const kindFilter = ref('')
const platformFilter = ref('')
/** 展示形态：卡片网格（FileCard）或表格（FileTable），docs/07 §3 两者并列。 */
const viewMode = ref<'card' | 'table'>('card')

const selected = ref<MediaFile | null>(null)

/** §6：768–1023 表格降级卡片。中屏宽度下六列表格挤到看不清，
 *  该档位直接收回表格形态（不是隐藏内容，是换形态），切换按钮随之禁用并说明原因。 */
const narrowTable = ref(false)
let tableMql: MediaQueryList | null = null

function syncNarrowTable() {
  narrowTable.value = tableMql?.matches ?? false
  if (narrowTable.value) viewMode.value = 'card'
}

onMounted(() => {
  tableMql = window.matchMedia?.('(min-width: 768px) and (max-width: 1023px)') ?? null
  syncNarrowTable()
  tableMql?.addEventListener?.('change', syncNarrowTable)
})

onUnmounted(() => tableMql?.removeEventListener?.('change', syncNarrowTable))

/** 实际生效形态：中屏档位恒定卡片。 */
const effectiveViewMode = computed(() => (narrowTable.value ? 'card' : viewMode.value))

const KINDS = [
  { value: '', label: '全部类型' },
  { value: 'video', label: '视频' },
  { value: 'audio', label: '音频' },
  { value: 'image', label: '图片' },
  { value: 'subtitle', label: '字幕' },
  { value: 'other', label: '其他' },
]

/** 表格列定义（RTable columns）。 */
const COLUMNS = [
  // 封面列：表格视图此前完全没有封面，切表后只能靠文件名辨认——
  // 卡片与表格是同一份数据的两种形态，能力必须等价。
  { key: 'cover', title: '封面', width: '96px' },
  { key: 'name', title: '文件名' },
  { key: 'kind', title: '类型', width: '80px' },
  { key: 'platform', title: '平台', width: '120px' },
  { key: 'size', title: '大小', width: '96px', align: 'right' as const },
  { key: 'duration', title: '时长', width: '88px', align: 'right' as const },
  { key: 'created_at', title: '入库时间', width: '140px' },
]

/** 预览 / 下载 URL 生成器（注入给子组件，组件不直接依赖 api 层）。 */
const previewUrl = (id: string) => api.filePreviewUrl(id)
const downloadUrl = (id: string) => api.fileDownloadUrl(id)
/** 封面地址：视频/音频有封面时用；无封面时后端返回 404，前端会回退到类型占位。 */
const coverUrl = (id: string) => api.fileCoverUrl(id)

/**
 * 请求序号：只有**最后一次**请求的结果可以落到界面上。
 *
 * 媒体库会同时存在「重新加载」（筛选变化、删除后重拉）与「加载更多」两类请求，
 * 后到的旧响应不能覆盖新结果——否则用户刚改完筛选、列表却被上一批数据盖回去
 * （筛选条件与列表对不上，看着像筛选没生效）。
 */
let requestSeq = 0

/** 组装查询参数：空值一律不下发，避免后端把空串当有效过滤条件。 */
function baseParams(offset: number): Record<string, string> {
  const params: Record<string, string> = { limit: String(PAGE_SIZE), offset: String(offset) }
  if (query.value.trim()) params.q = query.value.trim()
  if (kindFilter.value) params.kind = kindFilter.value
  if (platformFilter.value) params.platform = platformFilter.value
  return params
}

/** 还有更多吗？已加载条数 < 服务端总数 → 还有下一页。 */
const hasMore = computed(() => items.value.length < total.value)

/** 重新加载（第一页）。筛选变化、删除/转换后都走这里。 */
async function load() {
  const seq = ++requestSeq
  loading.value = true
  loadingMore.value = false
  errorMsg.value = ''
  moreErrorMsg.value = ''
  try {
    const res = await api.listFiles(baseParams(0))
    if (seq !== requestSeq) return
    items.value = res.items ?? []
    total.value = res.total
    platforms.value = res.platforms ?? []
  } catch (e) {
    if (seq !== requestSeq) return
    errorMsg.value = e instanceof Error ? e.message : '媒体库加载失败'
  } finally {
    if (seq === requestSeq) loading.value = false
  }
}

/** 加载更多（下一页）：结果追加在已有列表之后。 */
async function loadMore() {
  if (loadingMore.value || !hasMore.value) return
  const seq = ++requestSeq
  const offset = items.value.length
  loadingMore.value = true
  moreErrorMsg.value = ''
  try {
    const res = await api.listFiles(baseParams(offset))
    if (seq !== requestSeq) return
    items.value = [...items.value, ...(res.items ?? [])]
  } catch (e) {
    if (seq !== requestSeq) return
    moreErrorMsg.value = e instanceof Error ? e.message : '加载更多失败'
  } finally {
    if (seq === requestSeq) loadingMore.value = false
  }
}

function openDetail(f: MediaFile) {
  selected.value = f
}

function download(f: MediaFile) {
  toast.success('开始下载：' + f.name)
}

// ---- 封面制作 / 元数据编辑 / AI 刮削 ----
/** 封面制作对话框的目标文件。 */
const coverTarget = ref<MediaFile | null>(null)
/** 元数据编辑对话框的目标文件。 */
const metaTarget = ref<MediaFile | null>(null)

function openCover(f: MediaFile) {
  coverTarget.value = f
}

function openMeta(f: MediaFile) {
  metaTarget.value = f
}

/** 刮削结果先回填表单由用户确认，不直接覆盖。 */
async function scrape(f: MediaFile) {
  try {
    const res = await api.scrapeMedia(f.id)
    const r = res.result
    metaTarget.value = {
      ...f,
      title: r.title || f.title,
      uploader: r.uploader || f.uploader,
      description: r.description || f.description,
      tags: (r.tags ?? []).join(', ') || f.tags,
    }
    toast.success('已刮削出元数据，确认后保存')
  } catch (e) {
    toast.error(e instanceof Error ? e.message : 'AI 刮削失败')
  }
}

/** 封面/元数据保存后重拉列表（封面路径、标题都会变）。 */
async function reloadAfterEdit() {
  await load()
}

// ---- 删文件（docs/07 §4：破坏性决策走 RDialog；与「删任务记录」分离）----
/** 待删文件；null = 确认框关闭。 */
const pendingDelete = ref<MediaFile | null>(null)
const deleting = ref(false)

/** 确认后真正删：磁盘文件 + 索引一起删，然后重拉列表（不做本地数组过滤）。 */
async function confirmDeleteFile() {
  const f = pendingDelete.value
  pendingDelete.value = null
  if (!f) return
  deleting.value = true
  try {
    await api.deleteFile(f.id)
    toast.success('文件已删除：' + f.name)
    if (selected.value?.id === f.id) selected.value = null
    await load()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '文件删除失败')
  } finally {
    deleting.value = false
  }
}

/**
 * 正在转换的文件 id 集合。
 *
 * 原来是一个全局布尔量：任一文件在转，就把整页按钮都禁用（连别的文件也不能转），
 * 而且重复点击会连发两次转换请求。用 id 集合后：同一个文件不重复提交，
 * 别的文件照常能转。
 */
const convertingIds = ref<string[]>([])

/** 该文件是否正在转换。 */
function isConverting(id: string) {
  return convertingIds.value.includes(id)
}

async function convert(f: MediaFile, target: 'mp3' | 'mp4') {
  if (isConverting(f.id)) return
  convertingIds.value = [...convertingIds.value, f.id]
  try {
    const res = await api.convertFile(f.id, target)
    toast.success(target === 'mp3' ? `已提取音频：${res.file.name}` : `已转换为 MP4：${res.file.name}`)
    await load()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '格式转换失败')
  } finally {
    convertingIds.value = convertingIds.value.filter((id) => id !== f.id)
  }
}

/** 表格视图的行内「更多」菜单项（与卡片共用同一份动作清单）。 */
function tableMenuItems(f: MediaFile) {
  return fileActions(f).map((a) => ({
    label: a.label,
    hint: a.hint,
    // 正在转换的文件不再重复给出转换类动作。
    disabled: (a.key === 'mp3' || a.key === 'mp4') && isConverting(f.id),
    onSelect: () => handleAction(a.key, f),
  }))
}

/** 次要动作分发（表格视图）。 */
function handleAction(key: FileActionKey, f: MediaFile) {
  switch (key) {
    case 'cover':
      openCover(f)
      break
    case 'mp3':
      void convert(f, 'mp3')
      break
    case 'mp4':
      void convert(f, 'mp4')
      break
    case 'meta':
      openMeta(f)
      break
    case 'scrape':
      void scrape(f)
      break
  }
}

/** AI 总结生成后同步列表里的那份副本（不改后端数据，只更新本地状态）。 */
function applySummary(f: MediaFile, summary: string) {
  items.value = items.value.map((it) => (it.id === f.id ? { ...it, summary } : it))
  if (selected.value?.id === f.id) selected.value = { ...selected.value, summary }
}

let timer: number | undefined
watch([query, kindFilter, platformFilter], () => {
  if (timer) window.clearTimeout(timer)
  timer = window.setTimeout(load, 250)
})

// 防抖定时器必须清掉：页面切走后回调仍会发一次请求（并改已卸载组件的 ref）。
onUnmounted(() => {
  if (timer) window.clearTimeout(timer)
  timer = undefined
})

onMounted(load)
</script>

<template>
  <section class="page">
    <PageHeader eyebrow="MEDIA" title="媒体库" :sub="`${total} 个文件`" />

    <div class="toolbar">
      <input v-model="query" class="search" type="search" placeholder="搜索文件名…" aria-label="搜索文件名" />
      <RSelect v-model="kindFilter" aria-label="类型筛选" :options="KINDS" />
      <RSelect
        v-model="platformFilter"
        aria-label="平台筛选"
        :options="[{ value: '', label: '全部平台' }, ...platforms.map((p) => ({ value: p, label: platformText(p) }))]"
      />
      <!-- 展示形态切换（FileCard / FileTable） -->
      <div class="view-toggle" role="group" aria-label="展示形态">
        <button
          type="button"
          class="vt-btn"
          :class="{ on: effectiveViewMode === 'card' }"
          :aria-pressed="effectiveViewMode === 'card'"
          @click="viewMode = 'card'"
        >
          卡片
        </button>
        <button
          type="button"
          class="vt-btn"
          :class="{ on: effectiveViewMode === 'table' }"
          :aria-pressed="effectiveViewMode === 'table'"
          :disabled="narrowTable"
          :title="narrowTable ? '当前宽度下表格已降级为卡片' : ''"
          @click="viewMode = 'table'"
        >
          表格
        </button>
      </div>
    </div>

    <div v-if="errorMsg" class="error-card" role="alert">
      <p>{{ errorMsg }}</p>
      <RButton size="sm" @click="load">重试</RButton>
    </div>

    <div v-else-if="loading" class="grid">
      <div v-for="n in 6" :key="n" class="skeleton-card" />
    </div>

    <REmpty
      v-else-if="items.length === 0"
      face="waiting"
      title="媒体库还是空的"
      hint="下载完成的文件会自动入库；也可以在「下载」页提交链接。"
    />

    <!-- 表格视图（FileTable，RTable：表头浅底 / 行高 44 / hover 行操作；768–1023 降级为卡片） -->
    <RTable
      v-else-if="effectiveViewMode === 'table'"
      :rows="items"
      :columns="COLUMNS"
      row-key="id"
      empty-text="没有匹配的文件"
      @row-click="(f: MediaFile) => openDetail(f)"
    >
      <template #cell-cover="{ row }">
        <img
          v-if="(row as MediaFile).cover_path"
          class="cell-cover"
          :src="coverUrl((row as MediaFile).id)"
          :alt="(row as MediaFile).title || (row as MediaFile).name"
          loading="lazy"
        />
        <span v-else class="cell-cover cell-cover--ph" aria-hidden="true">无</span>
      </template>
      <template #cell-name="{ row }">
        <span class="cell-name" :title="(row as MediaFile).name">{{ (row as MediaFile).name }}</span>
      </template>
      <template #cell-kind="{ row }">{{ (row as MediaFile).kind }}</template>
      <template #cell-platform="{ row }">
        <PlatformBadge :platform="(row as MediaFile).platform" />
      </template>
      <template #cell-size="{ row }">
        <span class="tnum">{{ formatBytes((row as MediaFile).size) }}</span>
      </template>
      <template #cell-duration="{ row }">
        <span class="tnum">{{ formatDuration((row as MediaFile).duration) }}</span>
      </template>
      <template #cell-created_at="{ row }">
        <span class="tnum">{{ formatRelative((row as MediaFile).created_at) }}</span>
      </template>
      <template #actions="{ row }">
        <!-- 次要动作收进「更多」：表格行宽有限，横排 5 个按钮会撑破列宽 -->
        <RMenu :items="tableMenuItems(row as MediaFile)" label="更多操作" />
        <DownloadBtn :href="downloadUrl((row as MediaFile).id)" @click="download(row as MediaFile)" />
        <RButton size="sm" variant="ghost" tone="danger" @click="pendingDelete = row as MediaFile">
          删除
        </RButton>
      </template>
    </RTable>

    <ul v-else class="grid">
      <FileCard
        v-for="f in items"
        :key="f.id"
        :file="f"
        :preview-url="previewUrl"
        :cover-url="coverUrl"
        @open="openDetail"
        @download="download"
        @convert="convert"
        @make-cover="openCover"
        @edit-meta="openMeta"
        @scrape="scrape"
        @delete="pendingDelete = $event"
      />
    </ul>

    <!-- 分页：媒体库文件可能远超过一页（原来写死 limit=100，
         第 101 个文件之后的东西根本没法访问）。后端支持 offset，这里接上。 -->
    <div v-if="items.length && !errorMsg" class="pager">
      <span class="pager-count tnum">已加载 {{ items.length }} / 共 {{ total }} 个</span>
      <RButton v-if="hasMore" size="sm" variant="outline" :disabled="loadingMore" @click="loadMore">
        {{ loadingMore ? '加载中…' : '加载更多' }}
      </RButton>
      <span v-else class="pager-hint">已经到底了</span>
    </div>
    <p v-if="moreErrorMsg" class="more-err" role="alert">{{ moreErrorMsg }}</p>

    <!-- 删文件确认（不可撤销：磁盘文件真的会被删掉；任务记录保留） -->
    <DeleteFileDialog
      :open="pendingDelete !== null"
      :file-name="pendingDelete?.name ?? ''"
      :size="pendingDelete?.size ?? 0"
      :path="pendingDelete?.path ?? ''"
      @update:open="(v: boolean) => !v && (pendingDelete = null)"
      @confirm="confirmDeleteFile"
    />
    <p v-if="deleting" class="deleting-hint" role="status">正在删除…</p>
    <p v-if="convertingIds.length" class="deleting-hint" role="status">
      正在执行格式转换/提取音频（{{ convertingIds.length }} 个文件）…
    </p>

    <!-- 文件预览抽屉（方案 §3 视图级组件 PreviewDrawer） -->
    <PreviewDrawer
      :file="selected"
      :preview-url="previewUrl"
      :download-url="downloadUrl"
      @close="selected = null"
      @download="download"
      @convert="convert"
      @summary="applySummary"
    />

    <!-- 封面制作（抽帧让用户挑） -->
    <CoverPickerDialog
      :open="coverTarget !== null"
      :file="coverTarget"
      @update:open="(v: boolean) => !v && (coverTarget = null)"
      @done="reloadAfterEdit"
    />

    <!-- 元数据编辑（含 AI 刮削入口） -->
    <MediaMetaDialog
      :open="metaTarget !== null"
      :file="metaTarget"
      @update:open="(v: boolean) => !v && (metaTarget = null)"
      @done="reloadAfterEdit"
    />
  </section>
</template>

<style scoped>
.count { font-size: var(--font-aux); color: var(--text-3); }
.toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; margin-bottom: 16px; }
/* 搜索框定宽：flex:1 会把它拉满整行（实测 1400px 宽的输入框对着两个小下拉，比例失衡）；
   340px 足够长文件名搜索，剩余空间留给筛选与视图切换。 */
.search {
  flex: 0 1 340px;
  min-width: 220px;
  padding: 8px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-ctrl);
  background: var(--surface);
  color: var(--text-1);
  font-size: var(--font-body);
}
/* 展示形态切换（卡片 / 表格）：右推贴齐工具栏尾部，高度与其余控件一致。 */
.view-toggle { margin-left: auto; display: inline-flex; border: 1px solid var(--border); border-radius: var(--radius-ctrl); overflow: hidden; }
.vt-btn {
  padding: 8px 14px; border: none; background: var(--surface); color: var(--text-2);
  font-size: var(--font-body); cursor: pointer; transition: background 120ms ease-out, color 120ms ease-out;
}
.vt-btn + .vt-btn { border-left: 1px solid var(--border); }
.vt-btn.on { background: var(--accent); color: var(--on-accent); }
.cell-name { display: block; max-width: 420px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
/* 表格里的封面小图：与卡片同一套 16:9 契约，缩到 96 宽也保持同一形状。 */
.cell-cover {
  display: block;
  width: 96px;
  aspect-ratio: 16 / 9;
  height: auto;
  object-fit: cover;
  border-radius: var(--radius-ctrl);
  background: var(--surface-2);
}
.cell-cover--ph {
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-3);
  font-size: var(--font-aux);
}
/* 卡片网格：auto-fill 保持卡片固有尺寸（2026-10-24 用户裁决，推翻早先的 auto-fit）。
   auto-fit 的空轨道塌陷会把唯一有内容的卡片拉伸占满整行——
   「1 个视频排满全宽」比右侧留白更丑（用户实测截图裁决）。 */
.grid { list-style: none; margin: 0; padding: 0; display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 16px; }.skeleton-card { height: 200px; border-radius: var(--radius-card); background: var(--surface-2); }
.error-card { background: var(--surface); border: 1px solid var(--danger); border-radius: var(--radius-card); padding: 16px 20px; color: var(--danger); display: flex; align-items: center; gap: 12px; }
.deleting-hint { margin: 8px 0 0; font-size: var(--font-aux); color: var(--text-3); }
/* 分页条：计数与「加载更多」并排靠左。
   不靠右的原因：桌面宠物浮层固定在视口右下角（bottom:84px/right:28px），
   右对齐的按钮会被它盖住点不到——可交互的东西不放在那一块。 */
.pager {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 16px;
  padding-top: 12px;
  border-top: 1px solid var(--border);
}
.pager-count { font-size: var(--font-aux); color: var(--text-3); }
.pager-hint { font-size: var(--font-aux); color: var(--text-3); }
.more-err {
  margin: 8px 0 0;
  font-size: var(--font-aux);
  color: var(--danger);
}</style>
