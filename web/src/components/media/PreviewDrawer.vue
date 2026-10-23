<script setup lang="ts">
// PreviewDrawer：媒体库文件预览抽屉（方案 §3「/media → PreviewDrawer」）。
//
// 为什么单独抽：
// 1) 预览是「诚实展示」的关键位置——浏览器播不了的编码要直说，
//    不能假装能播（方案 §4 TaskDetail 同款要求）。这段逻辑如果内联在视图里，
//    很容易被后来的一次改动顺手改成「都试着播一下」。
// 2) 预览地址由后端接口生成（/api/files/{id}/preview），
//    组件通过注入的函数拿 URL，不直接依赖 api 层（便于单测）。
// 3) 抽屉本体复用 DetailDrawer（桌面右侧 360–400px、移动全屏，§4）。
import { computed } from 'vue'
import DetailDrawer from '@/components/base/DetailDrawer.vue'
import RButton from '@/components/base/RButton.vue'
import { formatBytes, formatRelative, platformText } from '@/utils/format'
import type { MediaFile } from '@/api/types'

const props = defineProps<{
  /** 当前预览的文件；null = 抽屉关闭。 */
  file: MediaFile | null
  /** 预览 URL 生成器（由父组件注入）。 */
  previewUrl: (id: string) => string
  /** 下载 URL 生成器（由父组件注入）。 */
  downloadUrl: (id: string) => string
}>()

const emit = defineEmits<{
  close: []
  download: [file: MediaFile]
}>()

const isVideo = computed(() => props.file?.kind === 'video')
const isAudio = computed(() => props.file?.kind === 'audio')
const isImage = computed(() => props.file?.kind === 'image')

/** 预览地址：只在抽屉打开时算，避免空文件时拼出坏 URL。 */
const previewUrl = computed(() => (props.file ? props.previewUrl(props.file.id) : ''))
</script>

<template>
  <DetailDrawer :open="file !== null" title="文件详情" @close="emit('close')">
    <div v-if="file" class="detail">
      <!-- 图片 → 直接看图 -->
      <div v-if="isImage && file.previewable" class="detail-preview">
        <img :src="previewUrl" :alt="file.name" />
      </div>
      <!-- 视频 → 原生播放器 -->
      <div v-else-if="isVideo && file.previewable" class="detail-preview">
        <video :src="previewUrl" controls preload="metadata" />
      </div>
      <!-- 音频 → 原生播放器 -->
      <div v-else-if="isAudio && file.previewable" class="detail-preview">
        <audio :src="previewUrl" controls preload="metadata" />
      </div>
      <!-- 其余：直说播不了，给下载按钮（诚实原则） -->
      <div v-else class="detail-unsupported">
        <p>该文件编码浏览器无法直接播放，下载后用本地播放器打开。</p>
      </div>

      <dl class="kv">
        <dt>文件名</dt><dd>{{ file.name }}</dd>
        <dt>大小</dt><dd class="tnum">{{ formatBytes(file.size) }}</dd>
        <dt>类型</dt><dd>{{ file.kind }}</dd>
        <dt>平台</dt><dd>{{ platformText(file.platform) }}</dd>
        <dt>作者</dt><dd>{{ file.uploader || '—' }}</dd>
        <dt>入库时间</dt><dd class="tnum">{{ formatRelative(file.created_at) }}</dd>
        <dt>路径</dt><dd class="path">{{ file.path }}</dd>
      </dl>

      <RButton variant="accent" block :href="downloadUrl(file.id)" @click="emit('download', file)">
        下载文件
      </RButton>
    </div>
  </DetailDrawer>
</template>

<style scoped>
.detail { display: flex; flex-direction: column; gap: 14px; }
.detail-preview { background: var(--surface-2); border-radius: var(--radius-ctrl); overflow: hidden; }
.detail-preview img,
.detail-preview video { width: 100%; display: block; }
/* R11：深色模式下画面与抽屉底色接近，不留边界就会「浮空」——
   用 1px 描边把播放区圈出来（浅色模式不需要，画面自带白边）。 */
:global([data-theme='dark']) .detail-preview { box-shadow: inset 0 0 0 1px var(--border); }
.detail-preview audio { width: 100%; display: block; padding: 8px; }
.detail-unsupported {
  background: var(--gold-soft);
  border-radius: var(--radius-ctrl);
  padding: 12px;
  font-size: var(--font-body);
  color: var(--text-2);
}
/* 详情键值：正文档 16px。 */
.kv { display: grid; grid-template-columns: auto 1fr; gap: 6px 16px; margin: 0; font-size: var(--font-body); }
.kv dt { color: var(--text-3); }
.kv dd { margin: 0; color: var(--text-1); word-break: break-all; }
.path { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: var(--font-aux); color: var(--text-2); }
</style>
