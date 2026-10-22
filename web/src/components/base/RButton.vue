<script setup lang="ts">
// RButton：三级样式（accent 实色 / 描边 / 文字）+ danger 二次确认（docs/07 §4）。
//
// 支持渲染成 <a>（传 href 时）：下载、打开外链这类「导航到资源」的动作，
// 语义上就该是链接而不是按钮——<a> 才有浏览器原生下载、右键「另存为」、
// 以及读屏软件念作「链接」。用 <button>+window.open 全都拿不到。
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
    /** 传了则渲染成 <a href>（下载 / 外链场景）。 */
    href?: string
    /** <a> 是否带 download 属性（强制下载而非导航）。 */
    download?: boolean
  }>(),
  {
    variant: 'outline',
    tone: 'default',
    size: 'md',
    disabled: false,
    confirm: '',
    type: 'button',
    block: false,
    href: '',
    download: false,
  },
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
  <!--
    有 href → 渲染成 <a>（下载 / 外链）；否则是普通 <button>。
    必须用动态标签 <component :is> 而不是 v-if/v-else 两个根：
    v-if/v-else 在 Vue 3 里编译成 Fragment，组件就变成「多根」——
    fallthrough 属性（aria-label 等）无处安放，测试里 element 也是注释节点。
    动态标签保持单根，attrs 正常继承。
  -->
  <component
    :is="href ? 'a' : 'button'"
    :class="cls"
    :href="href && !disabled ? href : undefined"
    :download="href && download ? '' : undefined"
    :type="href ? undefined : type"
    :disabled="href ? undefined : disabled"
    :aria-disabled="href && disabled ? 'true' : undefined"
    :role="href && disabled ? 'link' : undefined"
    @click="onClick"
  >
    <slot />
  </component>
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
  font-weight: var(--weight-caption);
  transition: background 120ms ease-out, border-color 120ms ease-out, color 120ms ease-out;
  white-space: nowrap;
}
.rbtn--md { padding: 8px 16px; font-size: var(--font-aux); }
.rbtn--sm { padding: 4px 10px; font-size: var(--font-caption); }
.rbtn--accent { background: var(--accent); color: var(--on-accent); }
.rbtn--accent:hover:not(.rbtn--disabled) { background: var(--accent-strong); }
.rbtn--outline { background: var(--surface); color: var(--text-1); border-color: var(--border); }
.rbtn--outline:hover:not(.rbtn--disabled) { border-color: var(--accent); color: var(--accent); }
.rbtn--ghost { background: transparent; color: var(--text-2); }
.rbtn--ghost:hover:not(.rbtn--disabled) { background: var(--surface-2); color: var(--text-1); }
.rbtn--danger.rbtn--accent { background: var(--danger); color: var(--on-danger); }
.rbtn--danger.rbtn--outline { color: var(--danger); border-color: var(--border); }
.rbtn--danger.rbtn--outline:hover:not(.rbtn--disabled) { border-color: var(--danger); }
.rbtn--block { width: 100%; }
.rbtn--disabled { opacity: 0.5; cursor: not-allowed; }
/* <a> 没有原生 disabled：靠 pointer-events 阻断点击 + aria-disabled 告知读屏。 */
a.rbtn--disabled { pointer-events: none; }
/* <768 触控目标 ≥44px（docs/07 §6）：小屏上 sm 尺寸的 4px 内边距根本点不准。 */
@media (max-width: 767px) {
  .rbtn--sm { min-height: 36px; padding: 6px 12px; }
  .rbtn--md { min-height: 44px; }
}
</style>
