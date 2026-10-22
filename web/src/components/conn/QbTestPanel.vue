<script setup lang="ts">
// QbTestPanel：可主动探测的外部通道面板（方案 §3「/connections → QbTestPanel」）。
//
// 为什么单独成组件：
// qB / 115 这类通道有「测试连接」按钮 + 测试结果 + 版本号，是一组独立交互；
// Telegram 没有测试端点（可用性由 status 给出），两者行为不同，
// 放在同一个组件里就得用 if 分支处理「有没有测试按钮」。
//
// 四态必须如实区分（不能退化）：
//   未配置 → 已配置但后端没实探 → 可用 → 不可用
// 「已配置但没实探结果」绝不能显示成「可用」——那是假装可用。
import { computed } from 'vue'
import RButton from '@/components/base/RButton.vue'

export type ChannelState = 'unconfigured' | 'unknown' | 'ok' | 'bad'

const props = defineProps<{
  /** 通道名。 */
  name?: string
  /** 通道说明。 */
  desc?: string
  /** 后端上报的通道状态。 */
  state?: ChannelState
  /** 状态说明文案。 */
  detail?: string
  /** 正在测试。 */
  testing?: boolean
  /** 最近一次手动测试结果（null = 没测过）。 */
  result?: { ok: boolean; text: string } | null
}>()

const emit = defineEmits<{
  /** 发起测试。 */
  test: []
}>()

/**
 * 实际生效的状态：手动测试结果优先于后端上报状态。
 * 用户刚点完「测试连接」，看到的应该是这次测试的结果，
 * 而不是几秒前那次轮询拉到的旧状态。
 */
const effective = computed<ChannelState>(() => {
  if (props.result) return props.result.ok ? 'ok' : 'bad'
  return props.state ?? 'unknown'
})

/** 状态文字。 */
const stateText = computed(() => {
  switch (effective.value) {
    case 'ok':
      return '可用'
    case 'bad':
      return '不可用'
    case 'unconfigured':
      return '未配置'
    default:
      return '状态不明'
  }
})

/** 状态符号（形状 + 颜色双重编码，不靠颜色单独承载信息）。 */
const glyph = computed(() => {
  switch (effective.value) {
    case 'ok':
      return '✓'
    case 'bad':
      return '✕'
    case 'unconfigured':
      return '–'
    default:
      return '?'
  }
})
</script>

<template>
  <li class="int-card" :data-state="effective">
    <header class="int-card-head">
      <span class="int-name">{{ name ?? 'qBittorrent' }}</span>
      <span class="int-state">
        <span class="int-glyph" aria-hidden="true">{{ glyph }}</span>
        {{ stateText }}
      </span>
    </header>
    <p class="int-desc">{{ desc ?? '把磁力/离线下载交给 qB 客户端执行' }}</p>
    <p v-if="detail" class="int-detail">{{ detail }}</p>
    <p v-if="result" class="int-test" :class="result.ok ? 'ok' : 'bad'">
      {{ result.ok ? '✓' : '✕' }} {{ result.text }}
    </p>
    <footer class="int-foot">
      <RButton size="sm" :disabled="testing" @click="emit('test')">
        {{ testing ? '测试中…' : '测试连接' }}
      </RButton>
      <RouterLink class="int-link" to="/settings">去设置页填写参数 →</RouterLink>
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
.int-card[data-state='bad'] { border-color: color-mix(in srgb, var(--danger) 40%, var(--border)); }
.int-card-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.int-name { font-size: var(--font-title-3); font-weight: var(--weight-medium); }
/* 通道状态：辅助档 13.5px（§2「12px 仅表头/徽章」；这是状态文字不是徽章）。 */
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
.int-card[data-state='bad'] .int-state { color: var(--danger); }
.int-card[data-state='bad'] .int-glyph { background: var(--danger); }
.int-card[data-state='unknown'] .int-state { color: var(--warning); }
.int-card[data-state='unknown'] .int-glyph { background: var(--warning); }
.int-desc { margin: 0; font-size: var(--font-body); color: var(--text-2); line-height: 1.7; }
.int-detail { margin: 0; font-size: var(--font-aux); color: var(--text-3); word-break: break-word; }
.int-test { margin: 0; font-size: var(--font-aux); }
.int-test.ok { color: var(--success); }
.int-test.bad { color: var(--danger); }
.int-foot {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-top: auto;
  padding-top: 8px;
  border-top: 1px solid var(--border);
}
.int-link { color: var(--text-3); font-size: var(--font-aux); text-decoration: none; }
.int-link:hover { color: var(--accent); }
</style>
