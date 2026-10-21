// toast.ts：Toast 队列（四级；错误常驻；同屏 ≤3；docs/07 §4 RToast）。
import { defineStore } from 'pinia'
import { ref } from 'vue'

export type ToastLevel = 'info' | 'success' | 'warning' | 'error'

export interface ToastItem {
  id: number
  level: ToastLevel
  text: string
  /** 错误常驻（不自动消失）。 */
  sticky: boolean
}

let seq = 0

export const useToastStore = defineStore('toast', () => {
  const items = ref<ToastItem[]>([])

  function push(level: ToastLevel, text: string, sticky = false): number {
    const id = ++seq
    items.value.push({ id, level, text, sticky })
    // 同屏最多 3 条：超出丢最旧（错误常驻优先保留）。
    if (items.value.length > 3) {
      const idx = items.value.findIndex((t) => !t.sticky)
      items.value.splice(idx >= 0 ? idx : 0, 1)
    }
    if (!sticky) {
      window.setTimeout(() => dismiss(id), 4000)
    }
    return id
  }

  function dismiss(id: number): void {
    const idx = items.value.findIndex((t) => t.id === id)
    if (idx >= 0) items.value.splice(idx, 1)
  }

  return {
    items,
    push,
    dismiss,
    info: (t: string) => push('info', t),
    success: (t: string) => push('success', t),
    warning: (t: string) => push('warning', t),
    error: (t: string) => push('error', t, true),
  }
})
