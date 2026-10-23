<script setup lang="ts">
// ActiveTasks：总览页「正在搬的活」（方案 §3 总览页关键组件之一）。
//
// 为什么抽成组件：
// 1) 「进行中的任务」是独立的一块信息（状态点 + 平台徽章 + 进度条），
//    塞在 Overview.vue 里会让总览页变成一个 300 行的巨型模板，
//    而且这块内容在「下载」页也会用到——列表展示逻辑（行样式、进度条）
//    复制一份就会漂移。
// 2) 组件只吃 props、不发请求：状态来源唯一是 SSE + /api 对账（§7 铁律）。
//    组件自己拉数据就成了第二份真相。
// 3) 空态表情按状态取用（§5）：有活干完过 → done（眨眼笑），
//    纯粹在等 → waiting（眼罩半搭，等着接单）。这是 §5「按状态取用」的直接落点。
import StatusDot from '@/components/task/StatusDot.vue'
import PlatformBadge from '@/components/task/PlatformBadge.vue'
import TaskProgress from '@/components/task/TaskProgress.vue'
import RaccoonFace from '@/components/raccoon/RaccoonFace.vue'
import RButton from '@/components/base/RButton.vue'
import type { Task } from '@/api/types'

const props = withDefaults(
  defineProps<{
    /** 进行中的任务（调用方已过滤好，这里不再二次过滤）。 */
    tasks: Task[]
    /** 加载中：显示骨架屏而不是空态（§7 空态/加载/错误三分）。 */
    loading?: boolean
    /** 是否有成功完成记录：决定空态用 done 还是 waiting 表情。 */
    hasDone?: boolean
  }>(),
  { loading: false, hasDone: false },
)

const emit = defineEmits<{
  /** 「查看全部」→ 去下载页。 */
  more: []
}>()

/** 空态文案随「有没有干完过」变化——这是 §5 的文案人格：讲事实 + 给下一步。 */
function emptyTitle(): string {
  return props.hasDone ? '活都搬完了，眼罩可以摘' : '眼罩搁下了，眼下没活'
}
function emptyHint(): string {
  return props.hasDone
    ? '干完的都在媒体库里躺着，新活随时可以开工。'
    : '粘贴链接到顶栏就能开工，或者去订阅页挂个自动追更。'
}
</script>

<template>
  <article class="card card--wide">
    <header class="card-head">
      <h2>
        <!-- working 表情（§5「眼罩推上头顶 + 叼金牌箭头」）：有任务在跑时才出现，
             「正在搬的活」不只是文字——搬着的小浣熊就蹲在标题旁。
             没有任务时不显示（空态有自己的表情位，每界面至多一处拟人）。 -->
        <RaccoonFace v-if="tasks.length > 0" face="working" :size="22" />
        正在搬的活
      </h2>
      <RButton variant="ghost" size="sm" @click="emit('more')">查看全部 →</RButton>
    </header>

    <!-- 加载中：骨架屏（§7 加载态），不是空态 -->
    <div v-if="loading" class="skeleton-rows">
      <div v-for="n in 3" :key="n" class="skeleton-row" />
    </div>

    <!-- 空态：引导空态（§7），表情按状态取用（§5） -->
    <div v-else-if="tasks.length === 0" class="inline-empty">
      <RaccoonFace :face="hasDone ? 'done' : 'waiting'" :size="52" />
      <div>
        <p class="inline-empty-title">{{ emptyTitle() }}</p>
        <p class="inline-empty-hint">{{ emptyHint() }}</p>
      </div>
    </div>

    <ul v-else class="task-list">
      <li v-for="t in tasks" :key="t.id" class="task-row">
        <div class="task-row-main">
          <div class="task-row-head">
            <StatusDot :status="t.status" :wait-reason="t.wait_reason" />
            <PlatformBadge :platform="t.platform" />
            <span class="task-title">{{ t.title || t.url }}</span>
          </div>
          <TaskProgress :task="t" />
        </div>
      </li>
    </ul>
  </article>
</template>

<style scoped>
.card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  padding: 20px 24px;
  box-shadow: var(--shadow-1);
}
.card--wide { margin-bottom: 18px; }
.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
}
/* 卡片标题：18px（--font-title-3），与正文拉开层级。 */
.card-head h2 {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: var(--font-title-3);
  font-weight: var(--weight-strong);
  color: var(--text-1);
}
.task-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.task-row-main { display: flex; flex-direction: column; gap: 8px; }
.task-row-head { display: flex; align-items: center; gap: 8px; }
/* 任务标题：正文档 16px。 */
.task-title {
  font-size: var(--font-body);
  color: var(--text-1);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.inline-empty { display: flex; align-items: center; gap: 16px; padding: 16px 0; }
.inline-empty-title {
  font-size: var(--font-title-3);
  color: var(--text-1);
  font-weight: var(--weight-strong);
}
.inline-empty-hint { font-size: var(--font-aux); color: var(--text-3); margin-top: 4px; }
.skeleton-rows { display: flex; flex-direction: column; gap: 12px; }
.skeleton-row { height: 40px; border-radius: var(--radius-ctrl); background: var(--surface-2); }
</style>
