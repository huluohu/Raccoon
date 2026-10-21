<script setup lang="ts">
// 下载页占位（Wave 3 落地：列表 + 详情侧栏 + 批量工具条，docs/07 §3）。
const title = '直播'
</script>

<template>
  <section class="page">
    <h1 class="page-title">{{ title }}</h1>
    <div class="empty">
      <p>直播源、录制计划、当前录制与历史文件；随 Wave 4 落地。</p>
      <p class="muted">后端任务系统（docs/05）已在 Wave 2 实现后在此呈现。</p>
    </div>
  </section>
</template>

<style scoped>
.page-title { font-size: 24px; font-weight: 750; margin-bottom: 16px; border-left: 4px solid var(--gold); padding-left: 12px; }
.empty { background: var(--surface); border: 1px dashed var(--border); border-radius: var(--radius-card); padding: 32px 24px; text-align: center; color: var(--text-2); font-size: 14px; }
.muted { color: var(--text-3); margin-top: 4px; }
</style>
