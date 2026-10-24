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
import PlatformBadge from '@/components/task/PlatformBadge.vue'
import FileCard from '@/components/media/FileCard.vue'
import PreviewDrawer from '@/components/media/PreviewDrawer.vue'
import DownloadBtn from '@/components/media/DownloadBtn.vue'
import DeleteFileDialog from '@/components/media/DeleteFileDialog.vue'
import type { MediaFile } from '@/api/types'

const toast = useToastStore()

const items = ref<MediaFile[]>([])
const total = ref(0)
const platforms = ref<string[]>([])
const loading = ref(true)
const errorMsg = ref('')
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

async function load() {
  loading.value = true
  errorMsg.value = ''
  try {
    const params: Record<string, string> = { limit: '100' }
    if (query.value.trim()) params.q = query.value.trim()
    if (kindFilter.value) params.kind = kindFilter.value
    if (platformFilter.value) params.platform = platformFilter.value
    const res = await api.listFiles(params)
    items.value = res.items ?? []
    total.value = res.total
    platforms.value = res.platforms ?? []
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : '媒体库加载失败'
  } finally {
    loading.value = false
  }
}

function openDetail(f: MediaFile) {
  selected.value = f
}

function download(f: MediaFile) {
  toast.success('开始下载：' + f.name)
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

let timer: number | undefined
watch([query, kindFilter, platformFilter], () => {
  if (timer) window.clearTimeout(timer)
  timer = window.setTimeout(load, 250)
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
        @open="openDetail"
        @download="download"
        @delete="pendingDelete = $event"
      />
    </ul>

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

    <!-- 文件预览抽屉（方案 §3 视图级组件 PreviewDrawer） -->
    <PreviewDrawer
      :file="selected"
      :preview-url="previewUrl"
      :download-url="downloadUrl"
      @close="selected = null"
      @download="download"
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
/* 卡片网格：auto-fill 保持卡片固有尺寸（2026-10-24 用户裁决，推翻早先的 auto-fit）。
   auto-fit 的空轨道塌陷会把唯一有内容的卡片拉伸占满整行——
   「1 个视频排满全宽」比右侧留白更丑（用户实测截图裁决）。 */
.grid { list-style: none; margin: 0; padding: 0; display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 16px; }.skeleton-card { height: 200px; border-radius: var(--radius-card); background: var(--surface-2); }
.error-card { background: var(--surface); border: 1px solid var(--danger); border-radius: var(--radius-card); padding: 16px 20px; color: var(--danger); display: flex; align-items: center; gap: 12px; }
.deleting-hint { margin: 8px 0 0; font-size: var(--font-aux); color: var(--text-3); }</style>
