<script setup lang="ts">
// DownloadBtn：媒体库下载按钮（方案 §3「/media → DownloadBtn」）。
//
// 为什么不用普通 RButton：
// 下载是「导航到资源 URL」，语义上应该是 <a href download>，不是 <button>。
// 用了 <a> 才有：浏览器原生下载、右键「另存为」、以及读屏软件把它念成链接。
// RButton 已支持传 href 时渲染成 <a>，这里只加「下载中」这一层交互反馈。
//
// 为什么单独抽成组件：
// 表格行、卡片、详情抽屉三处都要下载，URL 生成 + 提示语 +
// 「下载中」态放一份，行为不会三处不一致。
import { computed, onBeforeUnmount, ref } from 'vue'
import RButton from '@/components/base/RButton.vue'

const props = withDefaults(
  defineProps<{
    /** 下载 URL。 */
    href: string
    /** 按钮文案。 */
    label?: string
    /** 尺寸。 */
    size?: 'sm' | 'md'
    /** 是否占满宽度。 */
    block?: boolean
    /** 是否为主要动作（accent 实色）。 */
    variant?: 'accent' | 'outline' | 'ghost'
  }>(),
  { label: '下载', size: 'sm', block: false, variant: 'ghost' },
)

/** 下载中态：点了之后 1.5s 内显示「下载中…」，防止用户以为没点上而反复点。 */
const downloading = ref(false)
let timer: number | undefined

function onClick() {
  downloading.value = true
  if (timer) window.clearTimeout(timer)
  timer = window.setTimeout(() => (downloading.value = false), 1500)
}

onBeforeUnmount(() => {
  if (timer) window.clearTimeout(timer)
})

const text = computed(() => (downloading.value ? '下载中…' : props.label))
</script>

<template>
  <RButton
    :href="href"
    download
    :variant="variant"
    :size="size"
    :block="block"
    :disabled="downloading"
    :aria-label="`下载 ${label}`"
    @click="onClick"
  >
    {{ text }}
  </RButton>
</template>
