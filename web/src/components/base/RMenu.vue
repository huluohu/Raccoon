<script setup lang="ts">
// RMenu：卡片次要动作的下拉菜单（docs/07 §4）。
//
// 为什么需要：媒体库卡片原本横排 5 个按钮（提音频/转MP4/下载/详情/删除），
// 卡片宽度装不下，最后一个「删除」被挤出卡片边框（实测截图）。
// 把次要动作收进「更多」后，主行只留高频动作，宽度恒定。
//
// 交互约定：
//   - 点击触发器开合菜单；
//   - 点击菜单项后自动关闭；
//   - 点击外部 / 按 Esc 关闭；
//   - 用原生 button + role="menu"，键盘可达（↑↓ 移动、Enter 触发、Esc 关闭）。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

export interface RMenuItem {
  /** 菜单项文案。 */
  label: string
  /** 点击回调。 */
  onSelect: () => void
  /** 危险动作（danger 色）。 */
  danger?: boolean
  /** 禁用。 */
  disabled?: boolean
  /** 副标题（次要说明，如「把音轨提取成 MP3」）。 */
  hint?: string
}

const props = withDefaults(
  defineProps<{
    /** 菜单项。 */
    items: RMenuItem[]
    /** 触发器文案（无障碍名）。 */
    label?: string
    /** 触发器尺寸。 */
    size?: 'sm' | 'md'
  }>(),
  { label: '更多操作', size: 'sm' },
)

const open = ref(false)
const rootEl = ref<HTMLElement | null>(null)
const activeIdx = ref(-1)
/** 向下弹出：默认向上弹（菜单挂在卡片动作区下方），
 *  但触发器贴近视口顶部时向上会顶出屏幕——那时改向下弹。 */
const dropDown = ref(false)

const usableItems = computed(() => props.items.filter((i) => !i.disabled))

/**
 * 当前键盘高亮落在哪个菜单项上。
 *
 * 用**对象身份**比对而不是下标：`usableItems` 是过滤掉禁用项后的数组，
 * 它的下标和 `items` 的下标不是一回事（有禁用项时整体错位）。
 * 之前按下标比会把高亮打在禁用项上——「键盘选中的那一项」和「看到的选中项」不是同一个。
 */
function isActive(item: RMenuItem) {
  return activeIdx.value >= 0 && usableItems.value[activeIdx.value] === item
}

function toggle() {
  open.value = !open.value
  if (!open.value) {
    activeIdx.value = -1
    return
  }
  activeIdx.value = -1
  const el = rootEl.value
  if (el) {
    // 菜单高度按「每项约 44px」估算（含内边距），够用且不依赖渲染后的测量。
    const need = props.items.length * 44 + 12
    dropDown.value = el.getBoundingClientRect().top < need
  }
}

function close() {
  open.value = false
  activeIdx.value = -1
}

function select(item: RMenuItem) {
  if (item.disabled) return
  close()
  item.onSelect()
}

function onDocPointer(e: MouseEvent) {
  if (!open.value) return
  if (rootEl.value && !rootEl.value.contains(e.target as Node)) close()
}

function onKeydown(e: KeyboardEvent) {
  if (!open.value) {
    if (e.key === 'Escape') close()
    return
  }
  switch (e.key) {
    case 'Escape':
      e.preventDefault()
      close()
      break
    case 'ArrowDown':
      e.preventDefault()
      activeIdx.value = (activeIdx.value + 1) % Math.max(1, usableItems.value.length)
      break
    case 'ArrowUp':
      e.preventDefault()
      activeIdx.value = (activeIdx.value - 1 + usableItems.value.length) % Math.max(1, usableItems.value.length)
      break
    case 'Enter': {
      e.preventDefault()
      const item = usableItems.value[activeIdx.value]
      if (item) select(item)
      break
    }
  }
}

onMounted(() => {
  document.addEventListener('pointerdown', onDocPointer, true)
  document.addEventListener('keydown', onKeydown)
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocPointer, true)
  document.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <div ref="rootEl" class="rmenu">
    <button
      type="button"
      class="rmenu-trigger-badge"
      :class="`rmenu-trigger-badge--${size}`"
      :aria-haspopup="'menu'"
      :aria-expanded="open ? 'true' : 'false'"
      :aria-label="label"
      :title="label"
      @click="toggle"
    >
      <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
        <circle cx="3.6" cy="8" r="1.3" fill="currentColor" />
        <circle cx="8" cy="8" r="1.3" fill="currentColor" />
        <circle cx="12.4" cy="8" r="1.3" fill="currentColor" />
      </svg>
    </button>

    <div v-if="open" class="rmenu-pop" :class="{ 'rmenu-pop--down': dropDown }" role="menu" :aria-label="label">
      <button
        v-for="item in items"
        :key="item.label"
        type="button"
        class="rmenu-item"
        :class="{ 'is-danger': item.danger, 'is-active': isActive(item) }"
        role="menuitem"
        :disabled="item.disabled"
        @click="select(item)"
      >
        <span class="rmenu-item-label">{{ item.label }}</span>
        <span v-if="item.hint" class="rmenu-item-hint">{{ item.hint }}</span>
      </button>
    </div>
  </div>
</template>

<style scoped>
.rmenu { position: relative; display: inline-flex; }
.rmenu-trigger-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  padding: 4px 8px;
  border: 1px solid transparent;
  border-radius: var(--radius-ctrl);
  background: transparent;
  color: var(--text-2);
  cursor: pointer;
  font-size: var(--font-caption);
  min-height: 26px;
}
.rmenu-trigger-badge:hover { background: var(--surface-2); color: var(--text-1); }
.rmenu-trigger-badge--md { min-height: 34px; font-size: var(--font-aux); }

.rmenu-pop {
  position: absolute;
  bottom: calc(100% + 6px);
  right: 0;
  min-width: 180px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-ctrl);
  box-shadow: var(--shadow-2);
  padding: 4px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  z-index: 40;
}
/* 触发器贴近视口顶部时改为向下弹，否则菜单会被顶出屏幕看不见。 */
.rmenu-pop--down {
  bottom: auto;
  top: calc(100% + 6px);
}
.rmenu-item {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 1px;
  padding: 7px 10px;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-1);
  font-size: var(--font-aux);
  cursor: pointer;
  text-align: left;
  width: 100%;
}
.rmenu-item:hover:not(:disabled),
.rmenu-item.is-active { background: var(--surface-2); }
.rmenu-item:disabled { color: var(--text-3); cursor: not-allowed; }
.rmenu-item.is-danger { color: var(--danger); }
.rmenu-item-hint {
  font-size: var(--font-caption);
  color: var(--text-3);
  line-height: 1.4;
}
</style>
