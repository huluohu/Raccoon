<script setup lang="ts">
// WeChatPanel：微信公众号 / 远程下载集成面板。
import RButton from '@/components/base/RButton.vue'

defineProps<{
  /** 是否已配置 Webhook Token。 */
  enabled?: boolean
  /** Webhook 回调地址。 */
  webhookUrl?: string
  /** 正在刷新。 */
  loading?: boolean
}>()

const emit = defineEmits<{
  refresh: []
  params: []
}>()
</script>

<template>
  <li class="int-card" :data-state="enabled ? 'ok' : 'unconfigured'">
    <header class="int-card-head">
      <span class="int-name">
        <span class="int-mark int-mark--wx" aria-hidden="true">W</span>
        微信远程下载
      </span>
      <span class="int-state-badge">
        <span class="int-glyph" aria-hidden="true">{{ enabled ? '✓' : '–' }}</span>
        {{ enabled ? '就绪' : '未启用' }}
      </span>
    </header>
    <p class="int-desc">向微信公众号直接发送链接，自动入队并下载</p>
    <p class="int-detail-hint">
      {{
        enabled
          ? `已配置 Token · Webhook: ${webhookUrl || '/api/v1/integrations/wechat/webhook'}`
          : '需配置 wechat.token 作为公众号服务器校验'
      }}
    </p>
    <footer class="int-foot">
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
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.int-card[data-state='ok'] {
  border-color: color-mix(in srgb, var(--success) 35%, var(--border));
}
.int-card-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.int-name {
  font-size: var(--font-body);
  font-weight: var(--weight-semibold);
  color: var(--text-1);
  display: flex;
  align-items: center;
  gap: 8px;
}
.int-mark {
  width: 20px;
  height: 20px;
  border-radius: var(--radius-sm);
  background: var(--surface-2);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: var(--font-caption);
  font-weight: var(--weight-bold);
  color: var(--text-2);
}
.int-mark--wx {
  background: color-mix(in srgb, var(--success) 20%, transparent);
  color: var(--success);
}
.int-state-badge {
  font-size: var(--font-caption);
  color: var(--text-3);
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.int-card[data-state='ok'] .int-state-badge {
  color: var(--success);
}
.int-desc {
  font-size: var(--font-aux);
  color: var(--text-2);
  margin: 0;
  line-height: 1.5;
}
.int-detail-hint {
  font-size: var(--font-caption);
  color: var(--text-3);
  margin: 0;
  word-break: break-all;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
.int-foot {
  display: flex;
  gap: 8px;
  margin-top: auto;
  padding-top: 8px;
}
</style>
