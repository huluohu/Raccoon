<script setup lang="ts">
// RecordingList：录制历史列表（方案 §3「/live → RecordingList」）。
//
// 为什么抽成组件：
// 1) 表头 caption(12px) / 表体 body(16px) 的字号规则必须和表结构绑在一起——
//    之前整张表都是 12px，密密麻麻一坨，看不出是数据表。
// 2) 状态列的文字 + 颜色成对（§4：禁颜色唯一载体）。
import { formatBytes, formatDuration } from '@/utils/format'
import type { LiveRecording } from '@/api/types'

defineProps<{
  /** 录制记录。 */
  recordings: LiveRecording[]
  /** 分段总数。 */
  totalSegments?: number
  /** 总体积（字节）。 */
  totalSize?: number
}>()

/** 录制状态 → 中文。 */
const REC_STATUS: Record<string, string> = {
  recording: '录制中',
  completed: '已完成',
  failed: '失败',
}

/** 录制状态 → 语义色。 */
function statusTone(status: string): string {
  if (status === 'completed') return 'var(--success)'
  if (status === 'failed') return 'var(--danger)'
  return 'var(--info)'
}
</script>

<template>
  <div>
    <p class="hist-sum">
      共 {{ recordings.length }} 次录制 · {{ totalSegments ?? 0 }} 个分段 ·
      {{ formatBytes(totalSize ?? 0) }}
    </p>
    <table v-if="recordings.length" class="tbl">
      <thead>
        <tr><th>开始</th><th>时长</th><th>分段</th><th>大小</th><th>状态</th></tr>
      </thead>
      <tbody>
        <tr v-for="r in recordings" :key="r.id">
          <td class="tnum">{{ new Date(r.started_at).toLocaleString('zh-CN') }}</td>
          <td class="tnum">
            {{ r.ended_at ? formatDuration((r.ended_at - r.started_at) / 1000) : '进行中' }}
          </td>
          <td class="tnum">{{ r.segments }}</td>
          <td class="tnum">{{ formatBytes(r.size) }}</td>
          <td :style="{ color: statusTone(r.status) }">{{ REC_STATUS[r.status] || r.status }}</td>
        </tr>
      </tbody>
    </table>
    <p v-else class="field-hint">还没有录制记录。</p>
  </div>
</template>

<style scoped>
.hist-sum { font-size: var(--font-body); color: var(--text-2); margin-bottom: 12px; }
.field-hint { font-size: var(--font-aux); color: var(--text-3); }
/* 表格：表头 caption(12px)，表体 body(16px)。 */
.tbl { width: 100%; border-collapse: collapse; font-size: var(--font-body); }
.tbl th {
  text-align: left;
  color: var(--text-3);
  font-weight: var(--weight-caption);
  font-size: var(--font-caption);
  padding: 8px 10px;
  border-bottom: 1px solid var(--border);
}
.tbl td { padding: 9px 10px; border-bottom: 1px solid var(--border); color: var(--text-2); }
.tbl .tnum { white-space: nowrap; }
</style>
