<script setup lang="ts">
// DeleteFileDialog：删文件二次确认（docs/13 Q1 裁决：删记录 / 删文件是两个独立操作）。
//
// 为什么单独封装而不是各处写一段 RDialog 文案：
// 1) 删文件是不可撤销的破坏性操作，文案里必须同时说清三件事——删哪个文件、
//    多大、什么会保留（任务记录）。四处各写一遍，迟早有一处漏掉「什么会保留」；
// 2) 媒体库与任务详情都要用，共用一份才不会出现两种语气。
//
// 组件只负责「问」，不发请求：确认后 emit('confirm')，请求与刷新由调用方做
// （请求失败时调用方要能保留对话框/弹 Toast，组件里发请求就做不到了）。
import { computed } from 'vue'
import RDialog from '@/components/base/RDialog.vue'
import { formatBytes } from '@/utils/format'

const props = withDefaults(
  defineProps<{
    /** 是否打开。 */
    open: boolean
    /** 文件名（展示给人看，不是路径）。 */
    fileName: string
    /** 字节数。 */
    size?: number
    /** 完整路径（可选，展示在描述里帮助确认删的是哪个）。 */
    path?: string
    /** 是否保留任务记录（媒体库删文件 = 保留；任务详情删产物 = 保留任务）。 */
    keepTask?: boolean
  }>(),
  { size: 0, path: '', keepTask: true },
)

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void
  (e: 'confirm'): void
}>()

const description = computed(() => {
  const parts = [`将从磁盘删除「${props.fileName}」`]
  if (props.size > 0) parts.push(`（${formatBytes(props.size)}）`)
  let text = parts.join('')
  if (props.path) text += `，路径 ${props.path}`
  text += props.keepTask ? '。任务记录会保留，只是文件不再占空间。此操作不可撤销。' : '。此操作不可撤销。'
  return text
})
</script>

<template>
  <RDialog
    :open="open"
    title="删除这个文件？"
    danger
    confirm-text="删除文件"
    :description="description"
    @update:open="(v: boolean) => emit('update:open', v)"
    @confirm="emit('confirm')"
    @cancel="emit('update:open', false)"
  />
</template>
