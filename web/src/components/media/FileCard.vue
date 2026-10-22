<script setup lang="ts">
// FileCard：媒体库文件卡片（方案 §3「/media → FileTable/FileCard」）。
//
// 为什么抽成组件：
// 1) 卡片是媒体库的主要浏览单元，缩略图 + 文件名 + 平台/大小/时长 + 两个动作。
//    留在视图里，Media.vue 会被这套结构撑成 300 行，而真正该在视图里的
//    是「筛选条件怎么和后端对账」这类页面级逻辑。
// 2) 图片缩略图用后端预览接口（api.filePreviewUrl），只有图片类才有图；
//    视频/音频用类型化渐变占位——不给假图，也不假装能播。
import PlatformBadge from '@/components/task/PlatformBadge.vue'
import RButton from '@/components/base/RButton.vue'
import { formatBytes, formatDuration } from '@/utils/format'
import type { MediaFile } from '@/api/types'

defineProps<{
  /** 文件。 */
  file: MediaFile
  /** 缩略图 URL 生成器（由父组件注入，组件不直接依赖 api 层）。 */
  previewUrl: (id: string) => string
}>()

const emit = defineEmits<{
  /** 打开详情。 */
  open: [file: MediaFile]
  /** 下载。 */
  download: [file: MediaFile]
}>()
</script>

<template>
  <li class="file-card">
    <button class="file-open" type="button" @click="emit('open', file)">
      <div class="file-thumb" :class="`file-thumb--${file.kind}`">
        <span v-if="file.kind === 'image'" class="file-thumb-img">
          <img :src="previewUrl(file.id)" :alt="file.name" loading="lazy" />
        </span>
        <span v-else class="file-kind">{{ file.kind }}</span>
      </div>
      <div class="file-body">
        <p class="file-name" :title="file.name">{{ file.name }}</p>
        <div class="file-meta">
          <PlatformBadge :platform="file.platform" />
          <span class="tnum">{{ formatBytes(file.size) }}</span>
          <span v-if="file.duration" class="tnum">{{ formatDuration(file.duration) }}</span>
        </div>
      </div>
    </button>
    <div class="file-actions">
      <RButton size="sm" variant="ghost" @click="emit('download', file)">下载</RButton>
      <RButton size="sm" variant="ghost" @click="emit('open', file)">详情</RButton>
    </div>
  </li>
</template>

<style scoped>
.file-card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  overflow: hidden;
  box-shadow: var(--shadow-1);
  display: flex;
  flex-direction: column;
}
.file-open {
  border: none;
  background: transparent;
  padding: 0;
  cursor: pointer;
  text-align: left;
  display: flex;
  flex-direction: column;
}
.file-thumb {
  height: 140px;
  background: var(--surface-2);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-3);
  font-size: var(--font-caption);
}
/* 类型化占位：视频偏结构蓝、音频偏围巾金，跟方案 §2 的语义分工一致。 */
.file-thumb--video { background: linear-gradient(135deg, var(--accent-soft), var(--surface-2)); }
.file-thumb--audio { background: linear-gradient(135deg, var(--gold-soft), var(--surface-2)); }
.file-thumb-img { width: 100%; height: 100%; }
.file-thumb-img img { width: 100%; height: 100%; object-fit: cover; display: block; }
.file-body { padding: 12px 14px 8px; }
/* 文件名是卡片主角：正文档 16px。 */
.file-name {
  font-size: var(--font-body);
  color: var(--text-1);
  line-height: 1.5;
  max-height: 3em;
  overflow: hidden;
  margin-bottom: 8px;
}
.file-meta { display: flex; align-items: center; gap: 10px; font-size: var(--font-aux); color: var(--text-3); }
.file-actions { display: flex; gap: 6px; padding: 0 10px 10px; }
</style>
