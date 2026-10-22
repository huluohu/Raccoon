<script lang="ts">
// 父任务子项计数的形状。TaskRow 与 TaskCard 共用这一份定义：
// 两边各写一份，字段名一旦漂移，上层传子计数就会出现「传了但没生效」的静默失配。
export interface TaskChildStats {
  /** 子项总数：服务端权威值（task.child_total / SSE 聚合帧 child_total）。 */
  total: number
  /** 失败子项数：服务端权威值（task.child_failed）。 */
  failed: number
  /** 已完成子项数：后端不下发，只能由上层从已加载子项统计；
   *  缺省时组件只说「共 N 项」，不谎报「已完成 0/N」。 */
  done?: number
}
</script>

<script setup lang="ts">
// TaskCard：任务的卡片形态（docs/07 §4「状态徽章 + 迷你进度 + 速度/大小/平台徽章；父任务子计数」）。
//
// 为什么这么设计：
// 1) 卡片是「宽屏降级 / 单栏」形态（docs/07 §6：768–1023 表格降级卡片），
//    所以它是 TaskRow 的信息超集：行里有的卡片都有，卡片再补缩略图与子项计数。
// 2) 父任务在卡片上要一眼看出「还剩多少、错了几个」——只给个百分比不够，
//    所以显示「已完成 3/7，失败 1」；数字全部来自服务端与已加载子项，前端不推算。
// 3) 「重试失败项」是父任务专属动作，且要一次只重试失败的那些子任务（docs/05 §5），
//    组件不自己发请求（禁在组件里 fetch），只留插槽 + 上下文给上层。
import { computed } from 'vue'
import StatusDot from './StatusDot.vue'
import TaskProgress from './TaskProgress.vue'
import PlatformBadge from './PlatformBadge.vue'
import type { Task } from '@/api/types'
import { formatBytes, formatRelative, formatSpeed, WAIT_REASON_TEXT } from '@/utils/format'

const props = withDefaults(
  defineProps<{
    /** 任务本体（字段定义见 api/types.ts）。 */
    task: Task
    /** 多选选中态（由上层维护，组件不自持）。 */
    selected?: boolean
    /** 是否显示选择框：纯浏览场景关掉，多选场景打开。 */
    selectable?: boolean
    /** 父任务子项计数；不传时回退到 task.child_total / task.child_failed。 */
    childStats?: TaskChildStats
    /** 显式声明是父任务（后端建父任务时 platform 固定为 batch，也可由 childStats 推断）。 */
    parent?: boolean
  }>(),
  { selected: false, selectable: true, parent: false },
)

const emit = defineEmits<{
  /** 点选（上层负责 Shift 范围选择等策略）。 */
  (e: 'select', task: Task, ev: MouseEvent): void
  /** 打开详情。 */
  (e: 'open', task: Task): void
}>()

defineSlots<{
  /** 卡片操作区（取消 / 重试 / 删除等，由上层按任务状态给）。 */
  actions(props: { task: Task }): unknown
  /** 父任务「重试失败项」插槽：仅在父任务且存在失败子项时渲染，插槽参数带失败数。 */
  'retry-failed'(props: { task: Task; failed: number }): unknown
  /** 附加元信息（放在标题右侧）。 */
  meta(props: { task: Task }): unknown
}>()

const title = computed(() => props.task.title || props.task.url)

/** 是否父任务：显式声明、有子项计数、或服务端带了 child_total，任一成立即是。 */
const isParent = computed(
  () => props.parent || props.childStats !== undefined || props.task.child_total !== undefined,
)

/**
 * 子项计数：优先用上层给的（能算出「已完成」，后端不下发这个数），
 * 否则回退服务端上报的 child_total / child_failed。
 * done 缺省时只说「共 N 项」，不谎报「已完成 0/N」——后端没有这个字段，编一个就是骗人。
 */
const child = computed<TaskChildStats | null>(() => {
  if (props.childStats) return props.childStats
  if (props.task.child_total === undefined) return null
  return { total: props.task.child_total, failed: props.task.child_failed ?? 0 }
})

/** 等待原因（StatusDot 只表达状态，等待原因要单独讲清楚）。 */
const waitReason = computed(() => {
  if (props.task.status !== 'waiting' || !props.task.wait_reason) return ''
  return WAIT_REASON_TEXT[props.task.wait_reason] ?? ''
})
</script>

<template>
  <article class="tcard" :class="{ 'is-selected': selected }">
    <div class="tcard-top">
      <!-- 选择框：独立按钮，点了只改选中不改当前详情，避免误触丢焦点。 -->
      <button
        v-if="selectable"
        class="tcard-pick"
        type="button"
        role="checkbox"
        :aria-checked="selected"
        :aria-label="`选择 ${title}`"
        @click.stop="emit('select', task, $event)"
      >
        <span class="tcard-pick-mark" aria-hidden="true">✓</span>
      </button>

      <img
        v-if="task.thumbnail"
        class="tcard-thumb"
        :src="task.thumbnail"
        :alt="title"
        loading="lazy"
        referrerpolicy="no-referrer"
      />
      <div v-else class="tcard-thumb tcard-thumb--ph" aria-hidden="true" />

      <div
        class="tcard-main"
        role="button"
        tabindex="0"
        @click="emit('open', task)"
        @keydown.enter="emit('open', task)"
      >
        <div class="tcard-head">
          <StatusDot :status="task.status" />
          <PlatformBadge :platform="task.platform" />
          <span class="tcard-title" :title="title">{{ title }}</span>
          <slot name="meta" :task="task" />
        </div>

        <p class="tcard-meta">
          <span class="tnum">{{ task.quality }}</span>
          <span class="tnum tcard-time">{{ formatRelative(task.created_at) }}</span>
          <span v-if="task.attempt > 0" class="tnum">第 {{ task.attempt + 1 }}/{{ task.max_attempts }} 次</span>
          <span v-if="waitReason" class="tcard-wait">{{ waitReason }}</span>
        </p>

        <!-- 迷你进度：父任务画的是服务端聚合百分比，不是前端自己算的（docs/05 §5）。 -->
        <TaskProgress :task="task" compact />

        <p v-if="task.error_message" class="tcard-err">
          <span class="tnum">{{ task.error_code }}</span>：{{ task.error_message }}
        </p>
      </div>
    </div>

    <footer class="tcard-foot">
      <!-- 父任务：子项计数（已完成 X/Y、失败 N）代替速度/大小。 -->
      <p v-if="isParent && child" class="tcard-child tnum">
        <span v-if="child.done !== undefined">已完成 {{ child.done }}/{{ child.total }}</span>
        <span v-else>共 {{ child.total }} 项</span>
        <span v-if="child.failed > 0" class="tcard-child-failed">失败 {{ child.failed }}</span>
      </p>
      <p v-else class="tcard-stat tnum">
        <span>{{ formatSpeed(task.speed_bps) }}</span>
        <span>{{ formatBytes(task.downloaded_size) }}</span>
      </p>

      <div class="tcard-ops">
        <!-- 「重试失败项」是父任务专属：只有父任务且确有失败子项时才给位置。 -->
        <slot
          v-if="isParent && child && child.failed > 0"
          name="retry-failed"
          :task="task"
          :failed="child.failed"
        />
        <slot name="actions" :task="task" />
      </div>
    </footer>
  </article>
</template>

<style scoped>
.tcard {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px 14px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  box-shadow: var(--shadow-1);
  /* 微反馈 120ms ease-out（docs/07 §6）。 */
  transition: background 120ms ease-out, border-color 120ms ease-out;
}
.tcard:hover {
  background: var(--surface-2);
}
.tcard.is-selected {
  background: var(--gold-soft);
  border-color: var(--gold);
  /* 左侧金条：选中态在深浅底色下都靠形状识别，不只靠颜色（禁颜色唯一载体）。 */
  box-shadow: inset 3px 0 0 var(--gold);
}

.tcard-top {
  display: flex;
  align-items: flex-start;
  gap: 10px;
}

/* 选择框：几何勾号，非 emoji（docs/07 §4 禁 emoji 冒充图标）。 */
.tcard-pick {
  flex-shrink: 0;
  width: 18px;
  height: 18px;
  margin-top: 2px;
  padding: 0;
  border: 1px solid var(--border);
  border-radius: var(--radius-ctrl);
  background: var(--surface);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  transition: background 120ms ease-out, border-color 120ms ease-out;
}
.tcard-pick:hover {
  border-color: var(--accent);
}
.tcard-pick-mark {
  font-size: var(--font-caption);
  line-height: 1;
  color: var(--surface);
  opacity: 0;
}
.tcard.is-selected .tcard-pick {
  background: var(--accent);
  border-color: var(--accent);
}
.tcard.is-selected .tcard-pick-mark {
  opacity: 1;
}

.tcard-thumb {
  width: 120px;
  height: 68px;
  flex-shrink: 0;
  object-fit: cover;
  border-radius: var(--radius-ctrl);
  background: var(--surface-3);
}
.tcard-thumb--ph {
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-3);
  font-size: var(--font-caption);
}

.tcard-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
  cursor: pointer;
}
.tcard-head {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.tcard-title {
  /* 与 TaskRow 同口径：标题是主角，正文档 16px。 */
  font-size: var(--font-body);
  font-weight: var(--weight-semibold);
  color: var(--text-1);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tcard-meta {
  margin: 0;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  font-size: var(--font-aux);
  color: var(--text-3);
}
.tcard-wait {
  color: var(--warning);
}
.tcard-err {
  margin: 0;
  font-size: var(--font-aux);
  color: var(--danger);
}

.tcard-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
  padding-top: 8px;
  border-top: 1px solid var(--border);
}
.tcard-child {
  margin: 0;
  display: flex;
  gap: 8px;
  font-size: var(--font-aux);
  font-weight: var(--weight-semibold);
  color: var(--text-1);
}
.tcard-child-failed {
  color: var(--danger);
}
.tcard-stat {
  margin: 0;
  display: flex;
  gap: 10px;
  font-size: var(--font-aux);
  color: var(--text-2);
}
.tcard-ops {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}
</style>
