<script setup lang="ts">
// TelegramPanel：Telegram 通道面板（方案 §3「/connections → TelegramPanel」）。
//
// 为什么单独成组件：
// Telegram 与 qB/115 的行为**根本不同**——它没有测试端点，
// 可用性来自 /integrations/telegram/status 的 enabled + 白名单人数。
// 混在同一个组件里就得靠 if 分支区分「有没有测试按钮」，
// 而这两条通道的语义本来就不同：一个是「主动探测」，一个是「读取配置状态」。
import RButton from '@/components/base/RButton.vue'

defineProps<{
  /** 是否已启用（token + 白名单都齐）。 */
  enabled?: boolean
  /** 白名单人数。 */
  allowlist?: number
  /** 正在刷新。 */
  loading?: boolean
}>()

const emit = defineEmits<{
  /** 刷新状态。 */
  refresh: []
  /** 打开通道参数表单（参数在「连接」页就地填写，不再跳设置页）。 */
  params: []
}>()
</script>

<template>
  <li class="int-card" :data-state="enabled ? 'ok' : 'unconfigured'">
    <header class="int-card-head">
      <span class="int-name">
        <span class="int-mark" aria-hidden="true">T</span>
        Telegram
      </span>
      <span class="int-state">
        <span class="int-glyph" aria-hidden="true">{{ enabled ? '✓' : '–' }}</span>
        {{ enabled ? '可用' : '未启用' }}
      </span>
    </header>
    <p class="int-desc">通过 Telegram 机器人提交任务到同一队列</p>
    <p class="int-detail">
      {{
        enabled
          ? `已启用 · 白名单 ${allowlist ?? 0} 人`
          : '需配置 bot token 且白名单非空'
      }}
    </p>
    <footer class="int-foot">
      <!-- Telegram 没有独立测试端点：可用性只能靠刷新状态确认。 -->
      <RButton size="sm" :disabled="loading" @click="emit('refresh')">
        {{ loading ? '刷新中…' : '刷新状态' }}
      </RButton>
      <RButton size="sm" variant="ghost" @click="emit('params')">填写参数</RButton>
    </footer>
  </li>
</template>

<style scoped>
.int-card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  padding: 20px 22px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  box-shadow: var(--shadow-1);
}
.int-card[data-state='ok'] { border-color: color-mix(in srgb, var(--success) 40%, var(--border)); }
.int-card-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.int-name { font-size: var(--font-title-3); font-weight: var(--weight-medium); display: inline-flex; align-items: center; gap: 9px; min-width: 0; }
/* 品牌字标：与 QbTestPanel 同一套（三个通道卡并列时对齐）。 */
.int-mark {
  width: 24px;
  height: 24px;
  border-radius: 7px;
  background: var(--accent);
  color: var(--on-accent);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: var(--font-caption);
  font-weight: var(--weight-strong);
  flex-shrink: 0;
}
.int-state {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: var(--font-aux);
  font-weight: var(--weight-semibold);
  color: var(--text-2);
}
.int-glyph {
  width: 18px;
  height: 18px;
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: var(--font-caption);
  font-weight: var(--weight-strong);
  color: var(--on-neutral);
  background: var(--neutral);
}
.int-card[data-state='ok'] .int-state { color: var(--success); }
.int-card[data-state='ok'] .int-glyph { background: var(--success); }
.int-desc { margin: 0; font-size: var(--font-body); color: var(--text-2); line-height: 1.7; }
.int-detail { margin: 0; font-size: var(--font-aux); color: var(--text-3); word-break: break-word; }
.int-foot {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-top: auto;
  padding-top: 8px;
  border-top: 1px solid var(--border);
}
</style>
