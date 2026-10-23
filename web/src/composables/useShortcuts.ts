// 快捷键（docs/07 §7）：`/` 聚焦提交框、`Esc` 关闭/清除选择、`↑↓` 移动光标、
// `Enter` 看详情、`Shift+Space` 多选、`R` 重试、`G D`/`G S` 跳转。
//
// 关键约束：
// 1) 输入框内不触发（Esc 除外）——否则用户打字会被快捷键劫持；
// 2) `G` 是一组和弦键（G 后 1.5s 内按 D/S 才生效），避免与单键冲突；
// 3) **全局化**：G D / G S 与 `/` 在任何页面都要能用（App.vue 装 useGlobalShortcuts），
//    行级快捷键（↑↓ / Enter / Shift+Space / R）只有页面注册了上下文才生效。
//
// 两个监听、各管一段（不用模块级单例）：
// - 页面监听（useShortcuts）随组件挂载/卸载，处理 Esc 与行级键；
// - 全局监听（useGlobalShortcuts）只处理 G 和弦与 `/` 兜底。
// 同一个键不会被执行两次：页面监听先注册先执行，处理过就 preventDefault，
// 全局监听看到 defaultPrevented 直接让行。
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

/** 页面级键盘处理：Esc / `/` / 行导航。 */
function handlePageKey(ev: KeyboardEvent, h: ShortcutHandlers) {
  // Esc 在输入框内也要放行：用户 Esc 清空/失焦是常见预期。
  if (ev.key === 'Escape') {
    h.onEsc()
    return
  }
  // 其余快捷键一律避开输入场景。
  if (isTypingTarget(ev.target)) return
  if (ev.ctrlKey || ev.metaKey || ev.altKey) return

  if (ev.key === '/') {
    ev.preventDefault()
    h.focusSubmit()
    return
  }

  const rows = h.rows()
  switch (ev.key) {
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

/** 页面级快捷键：注册行级上下文，随组件卸载自动注销。 */
export function useShortcuts(h: ShortcutHandlers) {
  const onKey = (ev: KeyboardEvent) => handlePageKey(ev, h)
  onMounted(() => document.addEventListener('keydown', onKey))
  onBeforeUnmount(() => document.removeEventListener('keydown', onKey))
}

/**
 * 全局快捷键：在 App.vue 装一次。
 *
 * 没有它，`/` 与 G D / G S 只在「下载」页有效——用户在媒体库/订阅页想跳下载页
 * 得先摸鼠标，快捷键等于半残。docs/07 §7 要求的是全站可用。
 */
export function useGlobalShortcuts() {
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
    // 页面级监听已经处理过（它会 preventDefault），这里不再插手。
    if (ev.defaultPrevented) return
    // Esc 归页面管（关抽屉/清选择），全局层不抢。
    if (ev.key === 'Escape') {
      clearG()
      return
    }
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
      if (ev.key === 'd' || ev.key === 'D') void router.push('/tasks')
      else if (ev.key === 's' || ev.key === 'S') void router.push('/settings')
      return
    }

    // `/` 兜底：当前页没有注册行级上下文时，聚焦顶栏快速提交框。
    if (ev.key === '/') {
      const el = document.querySelector<HTMLElement>('#quick-submit')
      if (el) {
        ev.preventDefault()
        el.focus()
      }
    }
  }

  onMounted(() => document.addEventListener('keydown', onKey))
  onBeforeUnmount(() => document.removeEventListener('keydown', onKey))
}
