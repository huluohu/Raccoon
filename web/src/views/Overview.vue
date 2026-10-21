<script setup lang="ts">
// 总览（方案 §8）：现在什么情况、我要做什么。Wave 1 先接系统状态，任务/磁盘/连接卡片 Wave 3 补齐。
import { onMounted, ref } from 'vue'
import { api } from '@/api/client'
import type { HealthCheck, SystemInfo } from '@/api/types'

const info = ref<SystemInfo | null>(null)
const health = ref<HealthCheck | null>(null)
const errorMsg = ref('')

onMounted(async () => {
  try {
    info.value = await api.systemInfo()
    health.value = await api.health()
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : '加载失败'
  }
})

function formatUptime(sec: number): string {
  if (sec < 60) return `${sec} 秒`
  if (sec < 3600) return `${Math.floor(sec / 60)} 分钟`
  return `${Math.floor(sec / 3600)} 小时 ${Math.floor((sec % 3600) / 60)} 分`
}

function reload() {
  window.location.reload()
}
</script>

<template>
  <section class="overview">
    <h1 class="page-title">总览</h1>

    <div v-if="errorMsg" class="error-card" role="alert">
      <p>页面加载失败：{{ errorMsg }}</p>
      <button type="button" @click="reload">重试</button>
    </div>

    <template v-else>
      <div class="cards">
        <article class="card">
          <h2>系统</h2>
          <dl v-if="info">
            <dt>版本</dt><dd class="tnum">{{ info.version }}</dd>
            <dt>运行</dt><dd class="tnum">{{ formatUptime(info.uptime) }}</dd>
            <dt>架构</dt><dd class="tnum">{{ info.arch }}</dd>
          </dl>
          <p v-else class="muted">加载中…</p>
        </article>

        <article class="card">
          <h2>健康</h2>
          <p v-if="health">
            <span class="dot" :class="health.status ? 'ok' : 'bad'" aria-hidden="true" />
            数据库 {{ health.checks.db === 'ok' ? '正常' : health.checks.db }}
            <template v-if="health.checks.disk_used_pct !== undefined">
              · 磁盘已用 <span class="tnum">{{ health.checks.disk_used_pct }}%</span>
            </template>
          </p>
          <p v-else class="muted">加载中…</p>
        </article>

        <article class="card placeholder">
          <h2>活跃任务</h2>
          <p class="muted">暂无任务。粘贴链接，或按 <kbd>/</kbd> 快速聚焦输入框。</p>
        </article>
      </div>
    </template>
  </section>
</template>

<style scoped>
.page-title { font-size: 24px; font-weight: 750; margin-bottom: 16px; border-left: 4px solid var(--gold); padding-left: 12px; }
.cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 16px; }
.card { background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-card); padding: 16px 20px; box-shadow: var(--shadow-1); }
.card h2 { font-size: 14px; font-weight: 500; color: var(--text-3); margin-bottom: 8px; }
.card dl { display: grid; grid-template-columns: auto 1fr; gap: 4px 12px; margin: 0; font-size: 14px; }
.card dt { color: var(--text-3); }
.card dd { margin: 0; color: var(--text-1); }
.muted { color: var(--text-3); font-size: 14px; }
.error-card { background: var(--surface); border: 1px solid var(--danger); border-radius: var(--radius-card); padding: 16px 20px; color: var(--danger); }
.error-card button { margin-top: 8px; padding: 6px 16px; border: 1px solid var(--danger); border-radius: var(--radius-ctrl); background: transparent; color: var(--danger); cursor: pointer; }
.dot { display: inline-block; width: 8px; height: 8px; border-radius: var(--radius-pill); margin-right: 6px; }
.dot.ok { background: var(--success); }
.dot.bad { background: var(--danger); }
kbd { font-family: inherit; border: 1px solid var(--border); border-radius: 4px; padding: 0 4px; font-size: 12px; }
</style>
