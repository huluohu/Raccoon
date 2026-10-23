<script setup lang="ts">
// 历史页（docs/15 §2.3）：已结束任务的只读回看。
//
// 为什么只读：历史是「已经发生的事」。重试属于任务页、删除属于任务页 / 媒体库页
// （docs/14 Q5 主理人裁决），历史页上出现这些按钮只会让人以为能在历史里改结果。
// 所以这里连批量工具条、行内操作区、选择框都不渲染（TaskList / TaskCard / TaskRow 的 readonly）。
//
// 为什么不建 SSE：历史是快照，不是实时视图。挂载时对账一次即可；
// 也刻意不 disconnect——同一条连接是任务页在用的，离开历史页把它断掉，
// 用户回到任务页就看不到进度了。
import { computed, onMounted, ref } from 'vue'
import { api } from '@/api/client'
import type { Task, TaskEvent, TaskFile, TaskStatus } from '@/api/types'
import { useTasksStore } from '@/stores/tasks'
import { useToastStore } from '@/stores/toast'
import { useTaskTree, type FilterKey, type FilterOption } from '@/composables/useTaskTree'
import { formatBytes } from '@/utils/format'
import REmpty from '@/components/base/REmpty.vue'
import DetailDrawer from '@/components/base/DetailDrawer.vue'
import TaskList from '@/components/task/TaskList.vue'
import TaskDetail from '@/components/task/TaskDetail.vue'

const tasks = useTasksStore()
const toast = useToastStore()

/** 终态：只有这三种算「历史」。进行中的任务属于任务页，不该在这里出现。 */
const TERMINAL: TaskStatus[] = ['done', 'failed', 'cancelled']

// ---- 日期范围 ----
type RangeKey = 'today' | '7d' | '30d' | 'all'

/** 每个范围往回推几个自然日（0 = 只到今天 00:00）。 */
const RANGE_DAYS: Record<RangeKey, number> = { today: 0, '7d': 6, '30d': 29, all: 0 }

const RANGES: { key: RangeKey; label: string }[] = [
  { key: 'today', label: '今天' },
  { key: '7d', label: '近 7 天' },
  { key: '30d', label: '近 30 天' },
  { key: 'all', label: '全部' },
]

/** 默认「近 7 天」：历史页的常态是回看最近一周，全部记录通常又长又旧。 */
const range = ref<RangeKey>('7d')

/** 本地自然日边界（不是 24h 滚动窗）：用户说「今天」指的是日历上的今天。 */
function startOfDay(ms: number): number {
  const d = new Date(ms)
  d.setHours(0, 0, 0, 0)
  return d.getTime()
}

const rangeStart = computed(() => {
  if (range.value === 'all') return Number.NEGATIVE_INFINITY
  const d = new Date(startOfDay(Date.now()))
  // 用 setDate 而不是减毫秒：跨夏令时时减毫秒会偏一小时，自然日不会。
  d.setDate(d.getDate() - RANGE_DAYS[range.value])
  return d.getTime()
})

/** 任务落在哪个时间点：终态任务以完成时间为准，没有才退回创建时间。 */
function stamp(t: Task): number {
  return t.finished_at || t.created_at
}

const terminalTasks = computed(() => (tasks.items ?? []).filter((t) => TERMINAL.includes(t.status)))

/** 当前时间范围内、且已结束的任务（汇总条与列表都基于它）。 */
const inRange = computed(() => terminalTasks.value.filter((t) => stamp(t) >= rangeStart.value))

// ---- 列表（复用任务树，只为拿到父子计数与展开语义）----
const filter = ref<FilterKey>('all')

/** 历史页的筛选项就是三种终态；不给「进行中」——那类任务不属于历史。 */
const FILTERS: FilterOption[] = [
  { key: 'all', label: '全部' },
  { key: 'done', label: '已完成' },
  { key: 'failed', label: '失败' },
  { key: 'cancelled', label: '已取消' },
]

const { visibleRoots, childrenOf, shownChildren, isParent, statOf } = useTaskTree({
  allTasks: inRange,
  filter,
  // 历史页不连 SSE，拿不到聚合帧；子项计数只能靠已加载的子项算，
  // 这正好是「数得出来多少就说多少」的口径。
  childStats: computed(() => tasks.childStats),
})

/**
 * 只展示根任务：子项属于父卡，展开时才出现。
 * 这里显式按 parent_id 过滤，而不是用 rootTasks 的「父不在列表里就当根」兜底——
 * 历史页里父任务还在进行中时，它的子项不能冒充根任务冒出来。
 */
const roots = computed(() => visibleRoots.value.filter((t) => !t.parent_id))

const expanded = ref<Set<string>>(new Set())

function toggleExpand(id: string): void {
  const next = new Set(expanded.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  expanded.value = next
}

function countOf(key: FilterKey): number {
  if (key === 'all') return inRange.value.length
  return inRange.value.filter((t) => t.status === key).length
}

// ---- 顶部汇总（前端聚合，不改后端接口）----
const summary = computed(() => {
  const list = inRange.value
  const total = list.length
  const bytes = list.reduce((sum, t) => sum + (t.downloaded_size || 0), 0)
  const done = list.filter((t) => t.status === 'done').length
  // 分母为 0 时给 null：0% 会被读成「一条都没成功」，而事实是「还没有记录可比」。
  const rate = total === 0 ? null : Math.round((done / total) * 100)
  return { total, bytes, rate }
})

const rateText = computed(() => (summary.value.rate === null ? '—' : `${summary.value.rate}%`))

// ---- 详情抽屉（只读）----
const detail = ref<Task | null>(null)
const detailEvents = ref<TaskEvent[]>([])
const detailFiles = ref<TaskFile[]>([])

async function openDetail(t: Task): Promise<void> {
  detail.value = t
  detailEvents.value = []
  detailFiles.value = []
  try {
    const ev = await tasks.events(t.id)
    const res = await api.taskDetail(t.id)
    // 期间可能已切到别的任务，丢弃过期响应（否则详情会显示上一个任务的事件）。
    if (detail.value?.id !== t.id) return
    detailEvents.value = ev
    detailFiles.value = res.files ?? []
  } catch {
    // 详情拉不到不阻断列表：列表本身就是可用的快照
  }
}

/**
 * 详情面板里仍留着「重试这个任务」这类入口（TaskDetail 是任务页共用的组件，
 * 历史页不为它改结构）。点了不发写请求，但也不让它石沉大海——
 * 用一句提示把人指到真正能操作的地方。
 */
function hintReadonly(): void {
  toast.info('历史记录只读：重试请到「任务」页，删除请到「任务」页或「媒体库」页')
}

onMounted(async () => {
  // 只对账一次，不建 SSE（历史是快照）；也不在卸载时 disconnect，别断别人的连接。
  await tasks.refresh()
})
</script>

<template>
  <section class="page">
    <header class="page-head">
      <p class="page-sub">已完成 / 失败 / 已取消的记录，只读回看</p>
    </header>

    <!-- 日期范围：浏览行为，和任务页的状态筛选并列但互不替代 -->
    <div class="ranges" role="group" aria-label="时间范围">
      <button
        v-for="r in RANGES"
        :key="r.key"
        type="button"
        class="chip"
        :class="{ on: range === r.key }"
        :aria-pressed="range === r.key"
        @click="range = r.key"
      >
        {{ r.label }}
      </button>
    </div>

    <p v-if="tasks.error" class="err">{{ tasks.error }}</p>

    <!-- 汇总条：三块数字都是「当前范围」的口径，不跨范围混算 -->
    <section class="summary" aria-label="历史汇总">
      <p class="sm-item"><span class="sm-num tnum">{{ summary.total }}</span> 条记录</p>
      <p class="sm-item">合计 <span class="sm-num tnum">{{ formatBytes(summary.bytes) }}</span></p>
      <p class="sm-item">成功率 <span class="sm-num tnum">{{ rateText }}</span></p>
      <!-- 列表只加载了最近 100 条：汇总也是按这 100 条算的，
           不写这一句就等于把不完整的数据说成完整。 -->
      <p v-if="tasks.listTruncated" class="sm-note">仅统计已加载的最近 100 条</p>
    </section>

    <p v-if="tasks.loading && !tasks.items?.length" class="muted">加载中…</p>

    <TaskList
      v-else-if="roots.length"
      v-model:filter="filter"
      :roots="roots"
      :filters="FILTERS"
      :count-of="countOf"
      :expanded-ids="expanded"
      :stat-of="statOf"
      :children-of="childrenOf"
      :shown-children="shownChildren"
      :is-parent="isParent"
      readonly
      @open="openDetail"
      @toggle-expand="toggleExpand"
    />

    <!-- 空态：区分「这段时间真没有」和「筛选把它筛掉了」，两种空不是一回事 -->
    <REmpty
      v-else
      face="done"
      :title="inRange.length ? '这个筛选下没有记录' : '这段时间没有下载记录'"
      :hint="
        inRange.length
          ? '换个筛选看看，或者把时间范围放宽一点。'
          : '去「任务」页粘贴链接开始下载，完成的记录会留在这里。'
      "
    />

    <!-- 详情抽屉：只读展示，事件不接写操作 -->
    <DetailDrawer :open="detail !== null" title="任务详情" @close="detail = null">
      <TaskDetail
        v-if="detail"
        :task="detail"
        :events="detailEvents"
        :files="detailFiles"
        @retry="hintReadonly"
        @cancel="hintReadonly"
        @remove="hintReadonly"
        @delete-file="hintReadonly"
      >
        <template #actions>
          <p class="td-readonly">
            历史记录只读：重试请到「任务」页，删除任务或文件请到「任务」页 /「媒体库」页。
          </p>
        </template>
      </TaskDetail>
    </DetailDrawer>
  </section>
</template>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.page-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}
/* 页面副标题是正文档（16px）：它解释这页在干什么，不是脚注。 */
.page-sub {
  margin: 0;
  font-size: var(--font-body);
  color: var(--text-2);
}
.err {
  margin: 0;
  color: var(--danger);
  font-size: var(--font-body);
}
.muted {
  color: var(--text-3);
  font-size: var(--font-body);
}

/* 日期 chip：与任务页筛选条同一套形态，避免同一功能长得不一样 */
.ranges {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}
.chip {
  padding: 8px 14px;
  border: 1px solid var(--border);
  border-radius: var(--radius-pill);
  background: var(--surface);
  color: var(--text-2);
  font-size: var(--font-body);
  cursor: pointer;
  transition: background var(--motion-micro) var(--motion-micro-ease),
    color var(--motion-micro) var(--motion-micro-ease),
    border-color var(--motion-micro) var(--motion-micro-ease);
}
.chip:hover {
  border-color: var(--accent);
  color: var(--text-1);
}
.chip.on {
  background: var(--accent);
  border-color: var(--accent);
  color: var(--on-accent);
}

/* 汇总条：三块数字等宽排列，不随窗口宽度跳来跳去 */
.summary {
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: 8px 24px;
  padding: 12px 16px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  box-shadow: var(--shadow-1);
}
.sm-item {
  margin: 0;
  font-size: var(--font-aux);
  color: var(--text-2);
}
/* 数字比标签重一档：这块就是给人看数的，数不该和单位一样弱 */
.sm-num {
  font-size: var(--font-title-3);
  font-weight: var(--weight-semibold);
  color: var(--text-1);
}
.sm-note {
  margin: 0;
  margin-left: auto;
  font-size: var(--font-aux);
  color: var(--text-3);
}

/* 详情只读说明：替换掉 TaskDetail 的操作按钮，用一句话讲清去哪做 */
.td-readonly {
  margin: 0;
  font-size: var(--font-aux);
  color: var(--text-3);
  line-height: var(--line-height-body);
}
</style>
