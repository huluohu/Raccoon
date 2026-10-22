<script setup lang="ts">
// DetailDrawer：详情侧栏（桌面右侧 360–400px，移动全屏；docs/07 §6）。
import { onMounted, onUnmounted } from 'vue'

const props = withDefaults(defineProps<{ open: boolean; title?: string; width?: number }>(), {
  title: '',
  width: 380,
})
const emit = defineEmits<{ (e: 'close'): void }>()

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && props.open) emit('close')
}

onMounted(() => document.addEventListener('keydown', onKey))
onUnmounted(() => document.removeEventListener('keydown', onKey))
</script>

<template>
  <Teleport to="body">
    <Transition name="drawer">
      <div v-if="open" class="dd-root" role="dialog" aria-modal="true">
        <div class="dd-mask" @click="emit('close')" />
        <aside class="dd-panel" :style="{ width: width + 'px' }">
          <header class="dd-head">
            <h3 class="dd-title">{{ title }}</h3>
            <button class="dd-close" type="button" aria-label="关闭" @click="emit('close')">✕</button>
          </header>
          <div class="dd-body">
            <slot />
          </div>
        </aside>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.dd-root { position: fixed; inset: 0; z-index: 60; }
.dd-mask { position: absolute; inset: 0; background: var(--scrim); }
.dd-panel {
  position: absolute;
  top: 0;
  right: 0;
  bottom: 0;
  max-width: 100%;
  background: var(--surface);
  border-left: 1px solid var(--border);
  box-shadow: var(--shadow-3);
  display: flex;
  flex-direction: column;
}
.dd-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 16px;
  border-bottom: 1px solid var(--border);
}
.dd-title { font-size: var(--font-title-3); font-weight: var(--weight-strong); }
.dd-close {
  border: none;
  background: transparent;
  color: var(--text-3);
  cursor: pointer;
  font-size: var(--font-body);
  padding: 4px 8px;
  border-radius: var(--radius-ctrl);
}
.dd-close:hover { background: var(--surface-2); color: var(--text-1); }
.dd-body { flex: 1; overflow-y: auto; padding: 16px; }
.drawer-enter-active .dd-panel,
.drawer-leave-active .dd-panel { transition: transform 220ms cubic-bezier(0.2, 0.8, 0.2, 1); }
.drawer-enter-active .dd-mask,
.drawer-leave-active .dd-mask { transition: opacity 200ms ease-out; }
.drawer-enter-from .dd-panel,
.drawer-leave-to .dd-panel { transform: translateX(24px); }
.drawer-enter-from .dd-mask,
.drawer-leave-to .dd-mask { opacity: 0; }
@media (max-width: 768px) {
  .dd-panel { width: 100% !important; }
}
</style>
