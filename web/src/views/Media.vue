<script setup lang="ts">
// 媒体库（docs/07 §3 / docs/04 §2.8）：文件视角浏览、搜索、预览、下载。
//
// 诚实原则：不支持浏览器播放的编码直接标「不支持预览」并给下载按钮，不假装能播。
import { computed, onMounted, ref, watch } from 'vue'
import { api } from '@/api/client'
import { useToastStore } from '@/stores/toast'
import { formatBytes, formatDuration, formatRelative, platformText } from '@/utils/format'
import type { MediaFile } from '@/api/types'
import RButton from '@/components/base/RButton.vue'
import REmpty from '@/components/base/REmpty.vue'
import DetailDrawer from '@/components/base/DetailDrawer.vue'
import PlatformBadge from '@/components/task/PlatformBadge.vue'

const toast = useToastStore()

const items = ref<MediaFile[]>([])
const total = ref(0)
const platforms = ref<string[]>([])
const loading = ref(true)
const errorMsg = ref('')
const query = ref('')
const kindFilter = ref('')
const platformFilter = ref('')

const selected = ref<MediaFile | null>(null)

const KINDS = [
  { value: '', label: '全部类型' },
  { value: 'video', label: '视频' },
  { value: 'audio', label: '音频' },
  { value: 'image', label: '图片' },
  { value: 'subtitle', label: '字幕' },
  { value: 'other', label: '其他' },
]

const isVideo = computed(() => selected.value?.kind === 'video')
const isAudio = computed(() => selected.value?.kind === 'audio')
const isImage = computed(() => selected.value?.kind === 'image')
const previewUrl = computed(() => (selected.value ? api.filePreviewUrl(selected.value.id) : ''))

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
  window.open(api.fileDownloadUrl(f.id), '_blank')
  toast.success('开始下载：' + f.name)
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
    <header class="page-head">
      <h1 class="page-title">媒体库</h1>
      <span class="count tnum">{{ total }} 个文件</span>
    </header>

    <div class="toolbar">
      <input v-model="query" class="search" type="search" placeholder="搜索文件名…" aria-label="搜索文件名" />
      <select v-model="kindFilter" class="select" aria-label="按类型筛选">
        <option v-for="k in KINDS" :key="k.value" :value="k.value">{{ k.label }}</option>
      </select>
      <select v-model="platformFilter" class="select" aria-label="按平台筛选">
        <option value="">全部平台</option>
        <option v-for="p in platforms" :key="p" :value="p">{{ platformText(p) }}</option>
      </select>
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

    <ul v-else class="grid">
      <li v-for="f in items" :key="f.id" class="file-card">
        <button class="file-open" type="button" @click="openDetail(f)">
          <div class="file-thumb" :class="`file-thumb--${f.kind}`">
            <span v-if="f.kind === 'image'" class="file-thumb-img">
              <img :src="api.filePreviewUrl(f.id)" :alt="f.name" loading="lazy" />
            </span>
            <span v-else class="file-kind">{{ f.kind }}</span>
          </div>
          <div class="file-body">
            <p class="file-name" :title="f.name">{{ f.name }}</p>
            <div class="file-meta">
              <PlatformBadge :platform="f.platform" />
              <span class="tnum">{{ formatBytes(f.size) }}</span>
              <span v-if="f.duration" class="tnum">{{ formatDuration(f.duration) }}</span>
            </div>
          </div>
        </button>
        <div class="file-actions">
          <RButton size="sm" variant="ghost" @click="download(f)">下载</RButton>
          <RButton size="sm" variant="ghost" @click="openDetail(f)">详情</RButton>
        </div>
      </li>
    </ul>

    <DetailDrawer :open="selected !== null" title="文件详情" @close="selected = null">
      <div v-if="selected" class="detail">
        <div v-if="isImage && selected.previewable" class="detail-preview">
          <img :src="previewUrl" :alt="selected.name" />
        </div>
        <div v-else-if="isVideo && selected.previewable" class="detail-preview">
          <video :src="previewUrl" controls preload="metadata" />
        </div>
        <div v-else-if="isAudio && selected.previewable" class="detail-preview">
          <audio :src="previewUrl" controls preload="metadata" />
        </div>
        <div v-else class="detail-unsupported">
          <p>该文件编码浏览器无法直接播放，下载后用本地播放器打开。</p>
        </div>

        <dl class="kv">
          <dt>文件名</dt><dd>{{ selected.name }}</dd>
          <dt>大小</dt><dd class="tnum">{{ formatBytes(selected.size) }}</dd>
          <dt>类型</dt><dd>{{ selected.kind }}</dd>
          <dt>平台</dt><dd>{{ platformText(selected.platform) }}</dd>
          <dt>作者</dt><dd>{{ selected.uploader || '—' }}</dd>
          <dt>入库时间</dt><dd class="tnum">{{ formatRelative(selected.created_at) }}</dd>
          <dt>路径</dt><dd class="path">{{ selected.path }}</dd>
        </dl>

        <RButton variant="accent" block @click="download(selected)">下载文件</RButton>
      </div>
    </DetailDrawer>
  </section>
</template>

<style scoped>
.page-head { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; margin-bottom: 12px; }
.page-title { font-size: var(--font-title-1); font-weight: 750; border-left: 4px solid var(--gold); padding-left: 12px; }
.count { font-size: var(--font-aux); color: var(--text-3); }
.toolbar { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 16px; }
.search {
  flex: 1;
  min-width: 200px;
  padding: 8px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-ctrl);
  background: var(--surface);
  color: var(--text-1);
  font-size: var(--font-aux);
}
.select {
  padding: 8px 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-ctrl);
  background: var(--surface);
  color: var(--text-1);
  font-size: var(--font-aux);
}
.grid { list-style: none; margin: 0; padding: 0; display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 14px; }
.file-card { background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-card); overflow: hidden; box-shadow: var(--shadow-1); display: flex; flex-direction: column; }
.file-open { border: none; background: transparent; padding: 0; cursor: pointer; text-align: left; display: flex; flex-direction: column; }
.file-thumb { height: 120px; background: var(--surface-2); display: flex; align-items: center; justify-content: center; color: var(--text-3); font-size: var(--font-caption); }
.file-thumb--video { background: linear-gradient(135deg, var(--accent-soft), var(--surface-2)); }
.file-thumb--audio { background: linear-gradient(135deg, var(--gold-soft), var(--surface-2)); }
.file-thumb-img { width: 100%; height: 100%; }
.file-thumb-img img { width: 100%; height: 100%; object-fit: cover; display: block; }
.file-body { padding: 10px 12px 6px; }
.file-name { font-size: var(--font-aux); color: var(--text-1); line-height: 1.4; max-height: 2.8em; overflow: hidden; margin-bottom: 6px; }
.file-meta { display: flex; align-items: center; gap: 8px; font-size: var(--font-caption); color: var(--text-3); }
.file-actions { display: flex; gap: 6px; padding: 0 8px 8px; }
.skeleton-card { height: 180px; border-radius: var(--radius-card); background: var(--surface-2); }
.error-card { background: var(--surface); border: 1px solid var(--danger); border-radius: var(--radius-card); padding: 16px 20px; color: var(--danger); display: flex; align-items: center; gap: 12px; }
.detail { display: flex; flex-direction: column; gap: 14px; }
.detail-preview { background: var(--surface-2); border-radius: var(--radius-ctrl); overflow: hidden; }
.detail-preview img, .detail-preview video { width: 100%; display: block; }
.detail-preview audio { width: 100%; display: block; padding: 8px; }
.detail-unsupported { background: var(--gold-soft); border-radius: var(--radius-ctrl); padding: 12px; font-size: var(--font-aux); color: var(--text-2); }
.kv { display: grid; grid-template-columns: auto 1fr; gap: 6px 12px; margin: 0; font-size: var(--font-aux); }
.kv dt { color: var(--text-3); }
.kv dd { margin: 0; color: var(--text-1); word-break: break-all; }
.path { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: var(--font-caption); color: var(--text-2); }
</style>
