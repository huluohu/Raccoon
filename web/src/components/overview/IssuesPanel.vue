<script setup lang="ts">
// IssuesPanel：总览页「有几单没成」（方案 §3 总览页关键组件之一）。
//
// 为什么抽成组件：
// 1) 失败任务是「需要用户动作」的信息（重试 / 去连接页修 Cookie），
//    和「进行中」是两种语气，混在一张卡里会互相削弱。
// 2) 失败原因必须带上「错误码 + 原文 + 下一步」三件套（§5 文案人格：
//    讲事实 + 给下一步）。这套结构在任务详情里也用得到，抽出来才不会两边不一致。
// 3) 表情按状态取用（§5）：失败一律用 failed（眼罩歪斜 + 平嘴）。
import RaccoonFace from '@/components/raccoon/RaccoonFace.vue'
import RButton from '@/components/base/RButton.vue'
import type { Task } from '@/api/types'

withDefaults(
  defineProps<{
    /** 失败任务列表（调用方已过滤）。 */
    tasks: Task[]
    /** 非任务类预警（E2：成功率低于阈值等）。与失败任务同处一块，语气一致。 */
    alerts?: { title: string; detail: string; to?: string }[]
  }>(),
  { tasks: () => [], alerts: () => [] },
)

const emit = defineEmits<{
  /** 重试某条失败任务。 */
  retry: [id: string]
  /** 「全部任务」→ 去下载页。 */
  more: []
  /** 预警条目点击 → 跳转（由父层负责路由）。 */
  open: [to: string]
}>()
</script>

<template>
  <article v-if="tasks.length > 0 || alerts.length > 0" class="card card--wide issue-panel">
    <header class="card-head">
      <h2>有几单没成</h2>
      <RButton variant="ghost" size="sm" @click="emit('more')">全部任务 →</RButton>
    </header>
    <!-- 预警条目（E2）：不是任务失败，但同样需要用户动作 -->
    <ul v-if="alerts.length" class="alert-list">
      <li v-for="a in alerts" :key="a.title" class="alert-row">
        <RaccoonFace face="failed" :size="32" />
        <div class="issue-main">
          <div class="issue-title">{{ a.title }}</div>
          <div class="issue-reason">{{ a.detail }}</div>
        </div>
        <RButton v-if="a.to" size="sm" @click="emit('open', a.to)">去看看</RButton>
      </li>
    </ul>
    <ul class="issue-list">
      <li v-for="t in tasks" :key="t.id" class="issue-row">
        <!-- 表情按状态取用（§5）：失败 = failed -->
        <RaccoonFace face="failed" :size="32" />
        <div class="issue-main">
          <div class="issue-title">{{ t.title || t.url }}</div>
          <div class="issue-reason">
            <code>{{ t.error_code }}</code>
            <span>{{ t.error_message }}</span>
          </div>
        </div>
        <RButton size="sm" @click="emit('retry', t.id)">重试</RButton>
      </li>
    </ul>
  </article>

  <!-- 无失败时的正面态。
       方案 §7「空态/加载/错误三分——不展示空白页」：这一块整块消失的话，
       总览页 = 空活卡 + 状态条 + 指标卡之后直接到底，首屏塌掉半屏。
       正面态不是空卡——它明确回答用户上总览要看的问题：「有没有失败任务」。
       表情按 §5 取 done（成功态），边框走成功色淡边，和失败卡一眼区分。 -->
  <article v-else class="card card--wide ok-panel">
    <RaccoonFace face="done" :size="32" />
    <div class="ok-main">
      <h2>没有失败的任务</h2>
      <p class="ok-hint">失败、缺凭据、引擎报错的任务会出现在这里，并带一键重试。</p>
    </div>
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
.card-head h2 {
  font-size: var(--font-title-3);
  font-weight: var(--weight-strong);
  color: var(--text-1);
}
/* 失败卡：边框带危险色，和普通卡片一眼区分开。 */
.issue-panel { border-color: color-mix(in srgb, var(--danger) 40%, var(--border)); }
/* 正面态卡：成功色淡边，横向排（表情 + 文案），占住失败卡的同一块版面位置。 */
.ok-panel {
  display: flex;
  align-items: center;
  gap: 14px;
  border-color: color-mix(in srgb, var(--success) 32%, var(--border));
}
.ok-main h2 {
  font-size: var(--font-title-3);
  font-weight: var(--weight-strong);
  color: var(--text-1);
  margin: 0 0 2px;
}
.ok-hint { margin: 0; font-size: var(--font-aux); color: var(--text-3); }
.issue-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
/* 预警区与失败任务区之间留一道间距：两类条目语气不同，糊在一起会读成同一件事。 */
.alert-list {
  list-style: none;
  margin: 0 0 12px;
  padding: 0 0 12px;
  border-bottom: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.alert-row { display: flex; align-items: center; gap: 14px; }
.issue-row { display: flex; align-items: center; gap: 14px; }
.issue-main { flex: 1; min-width: 0; }
.issue-title {
  font-size: var(--font-body);
  color: var(--text-1);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.issue-reason {
  display: flex;
  gap: 8px;
  font-size: var(--font-aux);
  color: var(--text-3);
  margin-top: 4px;
}
.issue-reason code {
  background: var(--surface-2);
  padding: 1px 5px;
  border-radius: 4px;
  color: var(--danger);
}
</style>
