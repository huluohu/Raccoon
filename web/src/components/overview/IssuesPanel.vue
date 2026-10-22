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
  }>(),
  { tasks: () => [] },
)

const emit = defineEmits<{
  /** 重试某条失败任务。 */
  retry: [id: string]
  /** 「全部任务」→ 去下载页。 */
  more: []
}>()
</script>

<template>
  <article v-if="tasks.length > 0" class="card card--wide issue-panel">
    <header class="card-head">
      <h2>有几单没成</h2>
      <RButton variant="ghost" size="sm" @click="emit('more')">全部任务 →</RButton>
    </header>
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
.issue-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
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
