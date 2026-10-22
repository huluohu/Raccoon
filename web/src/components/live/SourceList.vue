<script setup lang="ts">
// SourceList：直播源列表（方案 §3「/live → SourceList」）。
//
// 为什么抽成组件：
// 每张直播源卡的信息结构固定（状态 + 计划徽章 → 标题 → URL → 六项元信息 →
// 四个动作）。抽出来后「状态色点 + 文字」的三重编码（§4 StatusDot 通用要求）
// 有明确归属，不会被后来的一次改动改成「只留个红点」。
import PlatformBadge from '@/components/task/PlatformBadge.vue'
import RButton from '@/components/base/RButton.vue'
import { formatRelative } from '@/utils/format'
import type { LiveSource } from '@/api/types'

defineProps<{
  /** 直播源列表。 */
  items: LiveSource[]
  /** 正在提交录制任务的源 ID。 */
  recordingId?: string
}>()

const emit = defineEmits<{
  /** 开始录制。 */
  record: [src: LiveSource]
  /** 查看录制历史。 */
  history: [src: LiveSource]
  /** 停止 / 恢复监控。 */
  toggle: [src: LiveSource]
  /** 删除。 */
  remove: [src: LiveSource]
}>()

/** 录制计划 → 中文。 */
const PLANS: Record<string, string> = {
  manual: '仅手动',
  always: '开播即录',
  scheduled: '定时录制',
}

/** 直播状态 → 中文。 */
const STATUS_TEXT: Record<string, string> = {
  offline: '未开播',
  live: '直播中',
  recording: '录制中',
  error: '异常',
}

/** 直播状态 → 语义色（色点 + 文字双重编码，不靠颜色单独承载信息）。 */
const STATUS_TONE: Record<string, string> = {
  offline: 'var(--neutral)',
  live: 'var(--success)',
  recording: 'var(--danger)',
  error: 'var(--warning)',
}

/** 上次检测时间 → 人话。 */
function lastCheckText(s: LiveSource): string {
  if (!s.last_checked_at) return '未检测过'
  return formatRelative(s.last_checked_at) + '检测'
}
</script>

<template>
  <div class="live-list">
    <article v-for="s in items" :key="s.id" class="card" :class="{ off: !s.enabled }">
      <header class="card-head">
        <PlatformBadge :platform="s.platform" />
        <span class="status" :style="{ color: STATUS_TONE[s.status] }">
          <span class="dot" :style="{ background: STATUS_TONE[s.status] }" aria-hidden="true" />
          {{ STATUS_TEXT[s.status] || s.status }}
        </span>
        <span class="plan">{{ PLANS[s.record_plan] || s.record_plan }}</span>
      </header>

      <h3 class="card-title">{{ s.title || s.room_id }}</h3>
      <p class="card-url mono" :title="s.room_url">{{ s.room_url }}</p>

      <dl class="meta">
        <div><dt>房间号</dt><dd>{{ s.room_id }}</dd></div>
        <div><dt>画质</dt><dd>{{ s.quality || 'best' }}</dd></div>
        <div><dt>分段</dt><dd>{{ s.segment_min || 0 }} 分钟</dd></div>
        <div><dt>检测</dt><dd>{{ lastCheckText(s) }} · 每 {{ s.check_interval_sec || 0 }}s</dd></div>
        <div><dt>录制次数</dt><dd>{{ s.recording_count ?? 0 }}</dd></div>
        <div v-if="s.last_live_at"><dt>上次开播</dt><dd>{{ formatRelative(s.last_live_at) }}</dd></div>
      </dl>

      <p v-if="s.error_note" class="err-note">{{ s.error_note }}</p>

      <footer class="card-foot">
        <RButton
          size="sm"
          variant="accent"
          :disabled="recordingId === s.id || s.status === 'recording'"
          @click="emit('record', s)"
        >
          {{ s.status === 'recording' ? '录制中' : recordingId === s.id ? '提交中…' : '开始录制' }}
        </RButton>
        <RButton size="sm" variant="ghost" @click="emit('history', s)">录制历史</RButton>
        <RButton size="sm" variant="ghost" @click="emit('toggle', s)">
          {{ s.enabled ? '停止监控' : '恢复监控' }}
        </RButton>
        <RButton
          size="sm"
          variant="ghost"
          tone="danger"
          confirm="再点一次确认删除"
          @click="emit('remove', s)"
        >
          删除
        </RButton>
      </footer>
    </article>
  </div>
</template>

<style scoped>
.live-list { display: flex; flex-direction: column; gap: 14px; }
.card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  padding: 18px 22px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  box-shadow: var(--shadow-1);
}
.card.off { opacity: 0.66; }
.card-head { display: flex; align-items: center; gap: 10px; }
/* 状态：色点 + 文字双重编码（不靠颜色单独承载信息）。 */
.status {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: var(--font-caption);
  font-weight: var(--weight-semibold);
}
.dot { width: 8px; height: 8px; border-radius: 50%; }
.plan {
  margin-left: auto;
  font-size: var(--font-caption);
  color: var(--text-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-pill);
  padding: 1px 8px;
}
.card-title { font-size: var(--font-title-3); font-weight: var(--weight-medium); }
.card-url {
  font-size: var(--font-aux);
  color: var(--text-3);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.meta { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 6px 20px; }
.meta > div { display: flex; gap: 10px; font-size: var(--font-aux); }
.meta dt { color: var(--text-3); width: 60px; flex-shrink: 0; }
.meta dd { color: var(--text-2); }
.err-note { font-size: var(--font-aux); color: var(--danger); }
.card-foot {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  padding-top: 10px;
  border-top: 1px solid var(--border);
}
</style>
