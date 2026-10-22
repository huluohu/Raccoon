// 任务树与筛选（docs/07 §3 的 TaskList、§7 键盘导航共用同一份真相）。
//
// 为什么抽成 composable 而不是塞进 TaskList 组件：
// 键盘导航（↑↓ 移动、Enter 打开详情）需要知道「当前筛选下可见的行」，
// 而 TaskList 组件渲染的也是同一批行。两边各算一份必然漂移——
// 筛选变了、键盘光标还停在看不见的行上，Enter 打开的是用户看不见的任务。
// 所以：算一次，父子共享。
import { computed, type ComputedRef } from 'vue'
import type { Task, TaskStatus } from '@/api/types'

export type FilterKey = 'all' | 'active' | 'done' | 'failed' | 'cancelled'

export interface FilterOption {
  key: FilterKey
  label: string
}

export interface ChildStat {
  total: number
  failed: number
  done: number
}

/** 进行中的状态集合（waiting/parsing/downloading/post）。 */
export const ACTIVE_STATUSES: TaskStatus[] = ['waiting', 'parsing', 'downloading', 'post']

export function isActiveStatus(status: TaskStatus): boolean {
  return ACTIVE_STATUSES.includes(status)
}

export interface TaskTreeOptions {
  /** 全部任务（未筛选）。 */
  allTasks: ComputedRef<Task[]> | { value: Task[] }
  /** 当前筛选。 */
  filter: { value: FilterKey }
  /** 服务端上报的父子计数（SSE 聚合帧）：parentId → {total, failed}。 */
  childStats: { value: Record<string, { total: number; failed: number }> }
}

export function useTaskTree(opts: TaskTreeOptions) {
  const allTasks = computed<Task[]>(() => opts.allTasks.value ?? [])

  /** parent_id → 子任务列表。 */
  const childrenByParent = computed(() => {
    const m = new Map<string, Task[]>()
    for (const t of allTasks.value) {
      if (!t.parent_id) continue
      const arr = m.get(t.parent_id)
      if (arr) arr.push(t)
      else m.set(t.parent_id, [t])
    }
    return m
  })

  const knownIds = computed(() => new Set(allTasks.value.map((t) => t.id)))

  /** 顶层任务：无父，或父不在当前列表里（孤儿子项，按顶层展示，不隐藏）。 */
  const rootTasks = computed<Task[]>(
    () => allTasks.value.filter((t) => !t.parent_id || !knownIds.value.has(t.parent_id)),
  )

  function childrenOf(id: string): Task[] {
    return childrenByParent.value.get(id) ?? []
  }

  /** 父任务判定：后端建父任务时 platform 固定为 "batch"，
   *  子任务的 platform 是按 URL 推断的平台名。列表里出现子项时也认作父。 */
  function isParent(t: Task): boolean {
    return t.platform === 'batch' || childrenOf(t.id).length > 0
  }

  function matchesFilter(t: Task): boolean {
    switch (opts.filter.value) {
      case 'active':
        return isActiveStatus(t.status)
      case 'done':
        return t.status === 'done'
      case 'failed':
        return t.status === 'failed'
      case 'cancelled':
        return t.status === 'cancelled'
      default:
        return true
    }
  }

  /** 父任务展开后展示的子项：按当前筛选过滤（全部时展示全部）。 */
  function shownChildren(id: string): Task[] {
    const all = childrenOf(id)
    if (opts.filter.value === 'all') return all
    return all.filter(matchesFilter)
  }

  /** 顶层筛选：自身命中，或任一子项命中（否则「失败」筛选会漏掉父卡里的失败子项）。 */
  const visibleRoots = computed<Task[]>(
    () => rootTasks.value.filter((r) => matchesFilter(r) || childrenOf(r.id).some(matchesFilter)),
  )

  /**
   * 父卡聚合统计。
   *  - 总数/失败数：服务端上报的 child_total / child_failed（SSE 聚合帧 + 创建响应）优先；
   *  - 「已完成」数：只能从已加载的子项数出来（后端未下发 done 子项计数）；
   *  - 列表被截断时（子项没全加载到）如实标注，不假装数全了。
   */
  function parentStat(parent: Task, children: Task[]) {
    const meta = opts.childStats.value[parent.id]
    const loadedTotal = children.length
    const loadedDone = children.filter((c) => c.status === 'done').length
    const loadedFailed = children.filter((c) => c.status === 'failed').length
    const total = Math.max(meta?.total ?? 0, loadedTotal)
    const failed = Math.max(meta?.failed ?? 0, loadedFailed)
    const truncated = (meta?.total ?? 0) > loadedTotal || (meta?.failed ?? 0) > loadedFailed
    return { done: loadedDone, total, failed, loadedTotal, loadedDone, loadedFailed, truncated }
  }

  /** 父卡统计（模板里多处用，算一次即可）。 */
  const parentStats = computed(() => {
    const m = new Map<string, ReturnType<typeof parentStat>>()
    for (const r of rootTasks.value) {
      if (!isParent(r)) continue
      m.set(r.id, parentStat(r, childrenOf(r.id)))
    }
    return m
  })

  function statOf(id: string): ReturnType<typeof parentStat> {
    return (
      parentStats.value.get(id) ?? {
        done: 0,
        total: 0,
        failed: 0,
        loadedTotal: 0,
        loadedDone: 0,
        loadedFailed: 0,
        truncated: false,
      }
    )
  }

  return {
    rootTasks,
    visibleRoots,
    childrenOf,
    shownChildren,
    isParent,
    matchesFilter,
    statOf,
  }
}
