<script setup lang="ts">
// RToast：全局 Toast 出口（四级；错误常驻；docs/07 §4）。
import { computed } from 'vue'
import { useToastStore } from '@/stores/toast'

const toast = useToastStore()

/** 屏上是否有错误级：有则整个容器切到 assertive（错误应立即播报）。 */
const hasError = computed(() => toast.items.some((t) => t.level === 'error'))
</script>

<template>
  <Teleport to="body">
    <!-- 错误级用 assertive（role="alert"）：错误应立即打断读屏，
         不该排在 polite 队列里等空闲才播报。 -->
    <div
      class="rtoasts"
      :role="hasError ? 'alert' : 'status'"
      :aria-live="hasError ? 'assertive' : 'polite'"
    >
      <div
        v-for="t in toast.items"
        :key="t.id"
        class="rtoast"
        :class="`rtoast--${t.level}`"
        @click="toast.dismiss(t.id)"
      >
        <span class="rtoast-glyph" aria-hidden="true">
          {{ t.level === 'success' ? '✓' : t.level === 'error' ? '✕' : t.level === 'warning' ? '!' : 'i' }}
        </span>
        <span class="rtoast-text">{{ t.text }}</span>
        <button class="rtoast-close" type="button" aria-label="关闭">✕</button>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.rtoasts {
  position: fixed;
  right: 16px;
  bottom: 16px;
  z-index: 90;
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-width: min(380px, calc(100vw - 32px));
}
.rtoast {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 10px 12px;
  border-radius: var(--radius-ctrl);
  background: var(--surface);
  border: 1px solid var(--border);
  box-shadow: var(--shadow-2);
  font-size: var(--font-aux);
  color: var(--text-1);
  cursor: pointer;
}
.rtoast-glyph {
  width: 18px;
  height: 18px;
  flex-shrink: 0;
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: var(--font-caption);
  font-weight: var(--weight-strong);
  color: var(--on-info);
  background: var(--info);
}
.rtoast--success .rtoast-glyph { background: var(--success); color: var(--on-success); }
.rtoast--warning .rtoast-glyph { background: var(--warning); color: var(--on-warning); }
.rtoast--error .rtoast-glyph { background: var(--danger); color: var(--on-danger); }
.rtoast-text { flex: 1; line-height: 1.5; word-break: break-word; }
.rtoast-close { border: none; background: transparent; color: var(--text-3); cursor: pointer; font-size: var(--font-caption); padding: 0 2px; }
</style>
