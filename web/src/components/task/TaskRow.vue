<script setup lang="ts">
// TaskRow：任务列表的紧凑行（docs/07 §4「状态徽章 + 迷你进度 + 速度/大小/平台徽章；父任务子计数」）。
//
// 为什么这么设计：
// 1) 行里只留「扫一眼就能判断」的信息：状态、标题、平台、进度、速度、大小。
//    取消/重试/删除这类动作交给 actions 插槽由上层决定显隐——行是数据，动作是次要信息，
//    常驻会把每行的注意力都拉走（与 base/RTable 的行内操作区同一取舍）。
// 2) 状态不自绘：复用 StatusDot（七态 = 色点 + 图标 + 文字三重编码，禁颜色唯一载体）。
//    自己再实现一套状态色，色板一改就会和别处漂移。
// 3) 父任务不显示速度：父没有自己的下载过程，它的 progress 是服务端下发的子项聚合值；
//    把它当成下载速度展示会让人以为父还在下载，所以父行改显示子项计数（docs/05 §5）。
// 4) 选中态由上层持有：多选是跨行的状态（Shift+Space / ↑↓ 维护选中集合），
//    组件只接收 selected 并把意图 emit 出去，不自己存选择，避免出现第二份真相。
import { computed } from 'vue'
import StatusDot from './StatusDot.vue'
import TaskProgress from './TaskProgress.vue'
import PlatformBadge from './PlatformBadge.vue'
import type { Task } from '@/api/types'
import { formatBytes, formatRelative, formatSpeed, waitReasonWithCountdown } from '@/utils/format'
import { useNowTick } from '@/utils/now'
import type { TaskChildStats } from './TaskCard.vue'

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
    /** 只读（历史页复用）：行内操作区与选择框都不渲染。
     *  只读页上出现的按钮都该能用，留着点了没反应的取消/重试/删除，
     *  比没有按钮更让人困惑——所以是「不渲染」，不是「置灰」。 */
    readonly?: boolean
  }>(),
  { selected: false, selectable: true, parent: false, readonly: false },
)

const emit = defineEmits<{
  /** 点选（上层负责 Shift 范围选择等策略）。 */
  (e: 'select', task: Task, ev: MouseEvent): void
  /** 打开详情。 */
  (e: 'open', task: Task): void
  /** 置顶/取消置顶（仅 waiting 行渲染按钮）。 */
  (e: 'pin', task: Task): void
}>()

defineSlots<{
  /** 行尾操作区（取消 / 重试 / 删除等，由上层按任务状态给）。 */
  actions(props: { task: Task }): unknown
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

/** 等待原因（StatusDot 只表达状态，等待原因要单独讲清楚；auto_retry 带秒级倒计时）。 */
const nowTick = useNowTick()
const waitReason = computed(() => waitReasonWithCountdown(props.task, nowTick.value))
</script>

<template>
  <li class="trow" :class="{ 'is-selected': selected }">
    <!-- 选择框：独立按钮，点了只改选中不改当前详情，避免误触丢焦点。
         只读模式下一并隐去——多选只为批量操作服务，只读页没有批量操作。 -->
    <button
      v-if="selectable && !readonly"
      class="trow-pick"
      type="button"
      role="checkbox"
      :aria-checked="selected"
      :aria-label="`选择 ${title}`"
      @click.stop="emit('select', task, $event)"
    >
      <span class="trow-pick-mark" aria-hidden="true">✓</span>
    </button>

    <img
      v-if="task.thumbnail"
      class="trow-thumb"
      :src="task.thumbnail"
      :alt="title"
      loading="lazy"
      referrerpolicy="no-referrer"
    />
    <div v-else class="trow-thumb trow-thumb--ph" aria-hidden="true" />

    <div
      class="trow-main"
      role="button"
      tabindex="0"
      @click="emit('open', task)"
      @keydown.enter="emit('open', task)"
    >
      <div class="trow-head">
        <StatusDot :status="task.status" />
        <span class="trow-title" :title="title">{{ title }}</span>
        <PlatformBadge :platform="task.platform" />
        <slot name="meta" :task="task" />
      </div>

      <p class="trow-meta">
        <span class="tnum">{{ task.quality }}</span>
        <span class="tnum trow-time">{{ formatRelative(task.created_at) }}</span>
        <span v-if="task.attempt > 0" class="tnum">第 {{ task.attempt + 1 }}/{{ task.max_attempts }} 次</span>
        <span v-if="waitReason" class="trow-wait">{{ waitReason }}</span>
      </p>

      <!-- 迷你进度：父任务画的是服务端聚合百分比，不是前端自己算的（docs/05 §5）。 -->
      <TaskProgress :task="task" compact />

      <p v-if="task.error_message" class="trow-err">
        <span class="tnum">{{ task.error_code }}</span>：{{ task.error_message }}
      </p>
    </div>

    <div class="trow-side">
      <!-- 父任务：子项计数（已完成 X/Y、失败 N）代替速度/大小。 -->
      <p v-if="isParent && child" class="trow-child tnum">
        <span v-if="child.done !== undefined">已完成 {{ child.done }}/{{ child.total }}</span>
        <span v-else>共 {{ child.total }} 项</span>
        <span v-if="child.failed > 0" class="trow-child-failed">失败 {{ child.failed }}</span>
      </p>
      <template v-else>
        <span class="tnum trow-speed">{{ formatSpeed(task.speed_bps) }}</span>
        <span class="tnum trow-size">{{ formatBytes(task.downloaded_size) }}</span>
      </template>

      <!-- 行内操作区：横排一行，不和数据叠成竖列。
           竖排时「速度 / 大小 / 取消 / 重试 / 删除」挤成一摞，右侧栏宽被撑到
           120px，主内容区反而被挤窄——一行任务的视觉重心全跑到了右边。
           这里给操作区单独一行横排：数据在上，动作在下，各自成行。
           只读模式下整个容器不渲染：父级传了 #actions 也不给位置。 -->
      <div v-if="!readonly && $slots.actions" class="trow-ops">
        <slot name="actions" :task="task" />
      </div>
    </div>
  </li>
</template>

<style scoped>
.trow {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  box-shadow: var(--shadow-1);
  /* 微反馈 120ms ease-out（docs/07 §6）：hover / 选中都是即时反馈，不该有拖影。 */
  transition: background 120ms ease-out, border-color 120ms ease-out;
}
.trow:hover {
  background: var(--surface-2);
}
.trow.is-selected {
  background: var(--accent-soft);
  border-color: var(--accent);
  /* 左侧金条：选中态在深浅底色下都靠形状识别，不只靠颜色（禁颜色唯一载体）。 */
  box-shadow: inset 3px 0 0 var(--accent);
}

/* 选择框：几何勾号，非 emoji（docs/07 §4 禁 emoji 冒充图标）。 */
.trow-pick {
  flex-shrink: 0;
  width: 18px;
  height: 18px;
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
.trow-pick:hover {
  border-color: var(--accent);
}
.trow-pick-mark {
  font-size: var(--font-caption);
  line-height: 1;
  color: var(--surface);
  opacity: 0;
}
.trow.is-selected .trow-pick {
  background: var(--accent);
  border-color: var(--accent);
}
.trow.is-selected .trow-pick-mark {
  opacity: 1;
}

.trow-thumb {
  width: 84px;
  height: 48px;
  flex-shrink: 0;
  object-fit: cover;
  border-radius: var(--radius-ctrl);
  background: var(--surface-3);
}
.trow-thumb--ph {
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-3);
  font-size: var(--font-caption);
}

.trow-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
  cursor: pointer;
}
.trow-head {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.trow-title {
  /* 任务标题是列表行的主角：正文档 16px。
     原来用 aux(13.5px)，整列任务标题跟元信息大不了多少，
     扫一眼分不清「标题」和「补充说明」——列表就没有主次。 */
  font-size: var(--font-body);
  font-weight: var(--weight-semibold);
  color: var(--text-1);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
/* 元信息（清晰度 / 时间 / 重试次数）：辅助档 13.5px。
   它是给标题做补充的，不该跟标题抢视线。 */
.trow-meta {
  margin: 0;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  font-size: var(--font-aux);
  color: var(--text-3);
}
.trow-wait {
  color: var(--warning);
}
.trow-err {
  margin: 0;
  font-size: var(--font-aux);
  color: var(--danger);
}

/* 右侧列：数据（速度/大小/子项计数）在上，操作区在下，各占一行。
   align-items: flex-end 让数字右对齐成一条竖线。 */
.trow-side {
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 6px;
  font-size: var(--font-aux);
  color: var(--text-2);
}
/* 行内操作区：横排。三个动作挤成一竖列会把右侧栏撑到 120px，
   主内容区被挤窄，整行的视觉重心跑到右边去了。 */
.trow-pin {
  border: 1px solid var(--border);
  border-radius: var(--radius-pill);
  background: var(--surface);
  color: var(--text-3);
  font-size: var(--font-caption);
  padding: 1px 8px;
  cursor: pointer;
  flex-shrink: 0;
  transition: color 120ms ease-out, border-color 120ms ease-out;
}
.trow-pin:hover { color: var(--accent); border-color: var(--accent); }
.trow-pin.on {
  color: var(--accent);
  border-color: var(--accent);
  background: var(--accent-soft);
}
.trow-ops {
  display: flex;
  flex-direction: row;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
  flex-wrap: wrap;
}
.trow-child {
  margin: 0;
  display: flex;
  gap: 8px;
  color: var(--text-1);
  font-weight: var(--weight-semibold);
}
.trow-child-failed {
  color: var(--danger);
}
</style>
