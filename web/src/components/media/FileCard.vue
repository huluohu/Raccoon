<script setup lang="ts">
// FileCard：媒体库文件卡片（方案 §3「/media → FileTable/FileCard」）。
//
// 为什么抽成组件：
// 1) 卡片是媒体库的主要浏览单元，缩略图 + 文件名 + 平台/大小/时长 + 动作区。
//    留在视图里，Media.vue 会被这套结构撑成 300 行，而真正该在视图里的
//    是「筛选条件怎么和后端对账」这类页面级逻辑。
// 2) 缩略图：图片类用后端预览接口（api.filePreviewUrl）；视频/音频有封面时
//    用封面（api.fileCoverUrl）；两者都没有才用「平台品牌色淡化渐变 + 类型
//    SVG 图标」占位——不给假图，也不假装能播，更不是一块灰底扔个英文 kind 原词。
// 3) 动作区：主行只留高频动作（下载/详情/删除），次要动作（提音频/转格式/
//    制作封面/编辑元数据）收进「更多」菜单——横排 5 个按钮装不下，
//    最后一个「删除」会被挤出卡片边框（实测截图）。
import { computed } from 'vue'
import PlatformBadge from '@/components/task/PlatformBadge.vue'
import RButton from '@/components/base/RButton.vue'
import RMenu from '@/components/base/RMenu.vue'
import { fileActions, type FileActionKey } from '@/components/media/fileActions'
import { formatBytes, formatDuration, platformTone } from '@/utils/format'
import type { MediaFile } from '@/api/types'

const props = defineProps<{
  /** 文件。 */
  file: MediaFile
  /** 缩略图 URL 生成器（由父组件注入，组件不直接依赖 api 层）。 */
  previewUrl: (id: string) => string
  /** 封面 URL 生成器（视频/音频有封面时用；无封面返回空串）。 */
  coverUrl?: (id: string) => string
}>()

const emit = defineEmits<{
  /** 打开详情。 */
  open: [file: MediaFile]
  /** 下载。 */
  download: [file: MediaFile]
  /** 二次格式转换 / 提取音频。 */
  convert: [file: MediaFile, target: 'mp3' | 'mp4']
  /** 制作封面（抽帧候选让用户挑）。 */
  makeCover: [file: MediaFile]
  /** 编辑元数据。 */
  editMeta: [file: MediaFile]
  /** AI 刮削元数据。 */
  scrape: [file: MediaFile]
  /** 删除文件（危险操作，二次确认由上层弹窗负责）。 */
  delete: [file: MediaFile]
}>()

/** 类型文案：占位区不渲染 kind 英文原词——那是给机器看的，屏幕上就是没做完的占位。 */
const KIND_TEXT: Record<string, string> = { video: '视频', audio: '音频', image: '图片' }
const kindText = computed(() => KIND_TEXT[props.file.kind] ?? '文件')

/** 有封面就用封面（视频/音频）；图片走预览接口。 */
const thumbUrl = computed(() => {
  if (props.file.kind === 'image') return props.previewUrl(props.file.id)
  if (props.file.cover_path && props.coverUrl) return props.coverUrl(props.file.id)
  return ''
})

/** 展示标题：用户改过就用改的，否则回退文件名。 */
const displayTitle = computed(() => props.file.title || props.file.name)

/** 次要动作菜单项（动作清单来自共享定义 fileActions，行为在这里绑定）。 */
const menuItems = computed(() =>
  fileActions(props.file).map((a) => ({
    label: a.label,
    hint: a.hint,
    onSelect: () => dispatch(a.key),
  })),
)

/** 把菜单动作映射到组件事件。 */
function dispatch(key: FileActionKey) {
  switch (key) {
    case 'cover':
      emit('makeCover', props.file)
      break
    case 'mp3':
      emit('convert', props.file, 'mp3')
      break
    case 'mp4':
      emit('convert', props.file, 'mp4')
      break
    case 'meta':
      emit('editMeta', props.file)
      break
    case 'scrape':
      emit('scrape', props.file)
      break
  }
}

/**
 * 占位区底色：平台品牌色淡化 20% 铺渐变。
 *
 * 视频/音频没有真封面可展示（不给假图，也不假装能播），但也不能是一块灰底
 * 加个英文词。用平台品牌色让卡片在网格里可辨认（B站蓝、YouTube红一扫就知道
 * 是谁家的），类型图标说明内容形态。image 类不走这里——有真图。
 */
const thumbTint = computed(() => {
  if (props.file.kind === 'image') return undefined
  const tone = platformTone(props.file.platform)
  return {
    background: `linear-gradient(135deg, color-mix(in srgb, ${tone} 20%, var(--surface-2)), var(--surface-2))`,
  }
})
</script>

<template>
  <li class="file-card">
    <button class="file-open" type="button" @click="emit('open', file)">
      <div class="file-thumb" :class="`file-thumb--${file.kind}`" :style="thumbTint">
        <span v-if="thumbUrl" class="file-thumb-img">
          <img :src="thumbUrl" :alt="displayTitle" loading="lazy" />
        </span>
        <!-- 无封面：SVG 线稿类型图标（§4 禁 emoji 冒充）+ 中文类型名，
             衬在平台品牌色淡化渐变上。 -->
        <span v-else class="file-kind">
          <svg
            v-if="file.kind === 'video'"
            class="file-kind-icon"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            aria-hidden="true"
          >
            <rect x="3" y="5" width="18" height="14" rx="2.5" />
            <path d="M3 9.5h18M8 5v4.5M16 5v4.5" stroke-linecap="round" />
          </svg>
          <svg
            v-else-if="file.kind === 'audio'"
            class="file-kind-icon"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            aria-hidden="true"
          >
            <path d="M9 18.5V6.2l10-2v12.3" stroke-linecap="round" stroke-linejoin="round" />
            <circle cx="6.5" cy="18.5" r="2.5" />
            <circle cx="16.5" cy="16.5" r="2.5" />
          </svg>
          <svg
            v-else
            class="file-kind-icon"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            aria-hidden="true"
          >
            <path d="M7 3.5h7l4.5 4.5v12a1.5 1.5 0 0 1-1.5 1.5H7a1.5 1.5 0 0 1-1.5-1.5V5a1.5 1.5 0 0 1 1.5-1.5z" stroke-linejoin="round" />
            <path d="M13.5 3.5V8H18" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
          <span class="file-kind-name">{{ kindText }}</span>
        </span>
      </div>
      <div class="file-body">
        <p class="file-name" :title="displayTitle">{{ displayTitle }}</p>
        <div class="file-meta">
          <PlatformBadge :platform="file.platform" />
          <span class="tnum">{{ formatBytes(file.size) }}</span>
          <span v-if="file.duration" class="tnum">{{ formatDuration(file.duration) }}</span>
        </div>
      </div>
    </button>
    <!-- 动作区：主行只留高频三个，次要动作收进「更多」——
         五个按钮横排会把最后一个挤出卡片边框（实测截图）。 -->
    <div class="file-actions">
      <RButton size="sm" variant="ghost" @click="emit('download', file)">下载</RButton>
      <RButton size="sm" variant="ghost" @click="emit('open', file)">详情</RButton>
      <RButton size="sm" variant="ghost" tone="danger" @click="emit('delete', file)">删除</RButton>
      <RMenu :items="menuItems" label="更多操作" />
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
  overflow: hidden;
}
/* 深色主题封面 1px 描边（docs/07 §2）：night/scarf/berry 的封面与卡片底色
   明度接近，没有边界时缩略图会「糊」进卡片里；snow（浅色）有明度差，不需要。 */
:global([data-theme='night']) .file-thumb,
:global([data-theme='scarf']) .file-thumb,
:global([data-theme='berry']) .file-thumb {
  box-shadow: inset 0 0 0 1px var(--border);
}
/* 非图片占位：图标 + 中文类型名，纵向居中。底色由 thumbTint 按平台品牌色注入，
   这里不再按 kind 写死渐变——平台色比类型色更能回答「这是哪家的文件」。 */
.file-kind {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 7px;
  color: var(--text-2);
}
.file-kind-icon { width: 30px; height: 30px; opacity: 0.9; }
.file-kind-name { font-size: var(--font-aux); letter-spacing: 0.02em; }
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
/* 动作区：允许换行 + 每个按钮不被压缩。
   原来 5 个按钮横排且容器不换行，最后一个「删除」被挤出卡片边框（实测截图）——
   现在主行只留 3 个高频动作 + 「更多」菜单，且仍保留换行兜底。 */
.file-actions {
  display: flex;
  gap: 6px;
  padding: 0 10px 10px;
  flex-wrap: wrap;
  align-items: center;
}
.file-actions :deep(.rbtn) { flex-shrink: 0; }

</style>
