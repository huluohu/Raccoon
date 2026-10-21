<script setup lang="ts">
// PlatformBadge：平台品牌色小图标 + 文字（禁 emoji 冒充，docs/07 §4）。
import { computed } from 'vue'
import { platformText, platformTone } from '@/utils/format'

const props = defineProps<{ platform?: string | null; compact?: boolean }>()

const tone = computed(() => platformTone(props.platform))
const text = computed(() => platformText(props.platform))
/** 平台首字（图标位）。 */
const mark = computed(() => {
  const t = text.value
  return t ? t.slice(0, 1) : '?'
})
</script>

<template>
  <span class="pbadge" :title="text">
    <span class="pbadge-mark" :style="{ background: tone }" aria-hidden="true">{{ mark }}</span>
    <span v-if="!compact" class="pbadge-text">{{ text }}</span>
  </span>
</template>

<style scoped>
.pbadge {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: var(--font-caption);
  color: var(--text-2);
  white-space: nowrap;
}
.pbadge-mark {
  width: 16px;
  height: 16px;
  border-radius: 4px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-size: 10px;
  font-weight: 700;
}
.pbadge-text { max-width: 120px; overflow: hidden; text-overflow: ellipsis; }
</style>
