<script setup lang="ts">
// StatusDot：七态 = 色点 + 图标 + 文字三重编码（禁颜色唯一载体，docs/07 §4）。
import { computed } from 'vue'
import { STATUS_TEXT, STATUS_TONE } from '@/utils/format'
import type { TaskStatus } from '@/api/types'

const props = defineProps<{ status: TaskStatus | string; waitReason?: string }>()

const tone = computed(() => STATUS_TONE[props.status] ?? 'neutral')
const text = computed(() => STATUS_TEXT[props.status] ?? props.status)
/** 状态图标（几何形状，色弱可辨）。 */
const glyph = computed(() => {
  switch (props.status) {
    case 'waiting':
      return '⋯'
    case 'parsing':
      return '◐'
    case 'downloading':
      return '↓'
    case 'post':
      return '⚙'
    case 'done':
      return '✓'
    case 'failed':
      return '✕'
    case 'cancelled':
      return '–'
    default:
      return '·'
  }
})
</script>

<template>
  <span class="sdot" :class="`sdot--${tone}`">
    <span class="sdot-glyph" aria-hidden="true">{{ glyph }}</span>
    <span class="sdot-text">{{ text }}</span>
  </span>
</template>

<style scoped>
.sdot {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: var(--font-caption);
  font-weight: var(--weight-caption);
  white-space: nowrap;
}
.sdot-glyph {
  width: 16px;
  height: 16px;
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: var(--font-caption);
  line-height: 1;
}
.sdot--neutral .sdot-glyph { background: var(--surface-3); color: var(--text-2); }
.sdot--info .sdot-glyph { background: var(--info); color: var(--on-info); }
.sdot--active .sdot-glyph { background: var(--accent); color: var(--on-accent); }
.sdot--success .sdot-glyph { background: var(--success); color: var(--on-success); }
.sdot--danger .sdot-glyph { background: var(--danger); color: var(--on-danger); }
.sdot--muted .sdot-glyph { background: var(--surface-3); color: var(--text-3); }
.sdot--neutral .sdot-text { color: var(--text-2); }
.sdot--info .sdot-text { color: var(--info); }
.sdot--active .sdot-text { color: var(--accent); }
.sdot--success .sdot-text { color: var(--success); }
.sdot--danger .sdot-text { color: var(--danger); }
.sdot--muted .sdot-text { color: var(--text-3); }
</style>
