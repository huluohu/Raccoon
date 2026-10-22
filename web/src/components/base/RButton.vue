<script setup lang="ts">
// RButton：三级样式（accent 实色 / 描边 / 文字）+ danger 二次确认（docs/07 §4）。
import { computed, ref } from 'vue'

const props = withDefaults(
  defineProps<{
    variant?: 'accent' | 'outline' | 'ghost'
    tone?: 'default' | 'danger'
    size?: 'sm' | 'md'
    disabled?: boolean
    confirm?: string
    type?: 'button' | 'submit'
    block?: boolean
  }>(),
  { variant: 'outline', tone: 'default', size: 'md', disabled: false, confirm: '', type: 'button', block: false },
)

const emit = defineEmits<{ (e: 'click', ev: MouseEvent): void }>()

const confirming = ref(false)
let timer: number | undefined

const cls = computed(() => [
  'rbtn',
  `rbtn--${props.variant}`,
  `rbtn--${props.tone}`,
  `rbtn--${props.size}`,
  { 'rbtn--block': props.block, 'rbtn--disabled': props.disabled },
])

function onClick(ev: MouseEvent) {
  if (props.disabled) return
  if (props.confirm && !confirming.value) {
    confirming.value = true
    timer = window.setTimeout(() => (confirming.value = false), 4000)
    return
  }
  if (timer) window.clearTimeout(timer)
  confirming.value = false
  emit('click', ev)
}
</script>

<template>
  <button :class="cls" :type="type" :disabled="disabled" @click="onClick">
    <slot />
  </button>
</template>

<style scoped>
.rbtn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  border-radius: var(--radius-ctrl);
  border: 1px solid transparent;
  cursor: pointer;
  font-weight: 500;
  transition: background 120ms ease-out, border-color 120ms ease-out, color 120ms ease-out;
  white-space: nowrap;
}
.rbtn--md { padding: 8px 16px; font-size: var(--font-aux); }
.rbtn--sm { padding: 4px 10px; font-size: var(--font-caption); }
.rbtn--accent { background: var(--accent); color: #fff; }
.rbtn--accent:hover:not(.rbtn--disabled) { background: var(--accent-strong); }
.rbtn--outline { background: var(--surface); color: var(--text-1); border-color: var(--border); }
.rbtn--outline:hover:not(.rbtn--disabled) { border-color: var(--accent); color: var(--accent); }
.rbtn--ghost { background: transparent; color: var(--text-2); }
.rbtn--ghost:hover:not(.rbtn--disabled) { background: var(--surface-2); color: var(--text-1); }
.rbtn--danger.rbtn--accent { background: var(--danger); color: #fff; }
.rbtn--danger.rbtn--outline { color: var(--danger); border-color: var(--border); }
.rbtn--danger.rbtn--outline:hover:not(.rbtn--disabled) { border-color: var(--danger); }
.rbtn--block { width: 100%; }
.rbtn--disabled { opacity: 0.5; cursor: not-allowed; }
/* <768 触控目标 ≥44px（docs/07 §6）：小屏上 sm 尺寸的 4px 内边距根本点不准。 */
@media (max-width: 767px) {
  .rbtn--sm { min-height: 36px; padding: 6px 12px; }
  .rbtn--md { min-height: 44px; }
}
</style>
