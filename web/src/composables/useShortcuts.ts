// 快捷键（docs/07 §7，原方案 §9.10）：`/` 聚焦提交框、`Esc` 关闭/清除选择、
// `↑↓` 移动光标、`Enter` 看详情、`Shift+Space` 多选、`R` 重试、`G D`/`G S` 跳转。
//
// 关键约束：输入框内不触发（Esc 除外）——否则用户打字会被快捷键劫持。
// `G` 是一组和弦键（G 后 1s 内按 D/S 才生效），避免与单键冲突。
import { onBeforeUnmount, onMounted } from 'vue'
import { useRouter } from 'vue-router'

export interface ShortcutHandlers {
  /** `/`：聚焦快速提交框。 */
  focusSubmit: () => void
  /** 当前可见行（光标在此集合内移动）。 */
  rows: () => { id: string; status: string }[]
  /** 当前光标索引（可变 ref 的 getter/setter，避免 composable 依赖具体 ref 类型）。 */
  cursorIndex: { value: number }
  /** `Enter`：打开光标行详情。 */
  openRow: (id: string) => void
  /** `Shift+Space`：切换光标行选中。 */
  toggleSelect: (id: string) => void
  /** `Esc`：关闭详情（返回 true 表示已处理，交由调用方决定是否清除选择）。 */
  onEsc: () => boolean
  /** `R`：重试光标行（仅失败/取消态有意义）。 */
  retryRow: (id: string) => void
}

/** 判断事件目标是否是可输入元素（快捷键在这些元素内一律让行，Esc 除外）。 */
function isTypingTarget(el: EventTarget | null): boolean {
  if (!(el instanceof HTMLElement)) return false
  const tag = el.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || el.isContentEditable
}

export function useShortcuts(h: ShortcutHandlers) {
  const router = useRouter()
  // G 和弦键状态：按下 G 后进入等待期，1.5s 内未按到目标键即作废。
  let gPending = false
  let gTimer: number | undefined

  function clearG() {
    gPending = false
    if (gTimer) window.clearTimeout(gTimer)
    gTimer = undefined
  }

  function onKey(ev: KeyboardEvent) {
    // Esc 在输入框内也要放行：用户 Esc 清空/失散是常见预期。
    if (ev.key === 'Escape') {
      clearG()
      h.onEsc()
      return
    }
    // 其余快捷键一律避开输入场景。
    if (isTypingTarget(ev.target)) return
    if (ev.ctrlKey || ev.metaKey || ev.altKey) return

    // 和弦键 G：按下后进入等待期。
    if (!gPending && (ev.key === 'g' || ev.key === 'G')) {
      gPending = true
      gTimer = window.setTimeout(clearG, 1500)
      return
    }
    if (gPending) {
      clearG()
      if (ev.key === 'd' || ev.key === 'D') {
        void router.push('/tasks')
      } else if (ev.key === 's' || ev.key === 'S') {
        void router.push('/settings')
      }
      return
    }

    const rows = h.rows()
    switch (ev.key) {
      case '/':
        ev.preventDefault()
        h.focusSubmit()
        break
      case 'ArrowDown':
        ev.preventDefault()
        h.cursorIndex.value = Math.min(rows.length - 1, h.cursorIndex.value + 1)
        break
      case 'ArrowUp':
        ev.preventDefault()
        h.cursorIndex.value = Math.max(0, h.cursorIndex.value - 1)
        break
      case 'Enter': {
        const row = rows[h.cursorIndex.value]
        if (row) {
          ev.preventDefault()
          h.openRow(row.id)
        }
        break
      }
      case ' ':
        if (ev.shiftKey) {
          const row = rows[h.cursorIndex.value]
          if (row) {
            ev.preventDefault()
            h.toggleSelect(row.id)
          }
        }
        break
      case 'r':
      case 'R': {
        const row = rows[h.cursorIndex.value]
        // 只有失败/取消态才允许 R 重试，避免误触把排队中的任务重排。
        if (row && (row.status === 'failed' || row.status === 'cancelled')) {
          ev.preventDefault()
          h.retryRow(row.id)
        }
        break
      }
      default:
        break
    }
  }

  onMounted(() => document.addEventListener('keydown', onKey))
  onBeforeUnmount(() => document.removeEventListener('keydown', onKey))
}
