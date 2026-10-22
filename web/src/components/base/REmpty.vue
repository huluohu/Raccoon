<script setup lang="ts">
// REmpty：引导空态（线稿 + 一句说明 + 一个主按钮）；不与骨架屏混用（docs/07 §7）。
import RButton from './RButton.vue'
import RaccoonFace from '@/components/raccoon/RaccoonFace.vue'

withDefaults(
  defineProps<{
    face?: 'waiting' | 'working' | 'done' | 'failed' | 'cancelled' | 'surprise'
    title: string
    hint?: string
    actionLabel?: string
  }>(),
  { face: 'waiting', hint: '', actionLabel: '' },
)

const emit = defineEmits<{ (e: 'action'): void }>()
</script>

<template>
  <div class="rempty">
    <RaccoonFace :face="face" :size="72" />
    <p class="rempty-title">{{ title }}</p>
    <p v-if="hint" class="rempty-hint">{{ hint }}</p>
    <RButton v-if="actionLabel" variant="accent" size="sm" @click="emit('action')">{{ actionLabel }}</RButton>
  </div>
</template>

<style scoped>
.rempty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  padding: 48px 20px;
  text-align: center;
}
.rempty-title { font-size: var(--font-title-3); font-weight: var(--weight-semibold); color: var(--text-1); }
.rempty-hint { font-size: var(--font-aux); color: var(--text-3); max-width: 420px; }
</style>
