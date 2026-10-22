<script setup lang="ts">
// StatusStrip：系统状态条（方案 §3 总览页关键组件「StatusStrip（磁盘/连接）」）。
//
// 为什么单独抽成组件：
// 1) 这些指标是「一眼扫过」型信息——磁盘用了多少、数据库活不活、SSE 连没连、
//    凭据有没有过期。它们不属于任何一张卡片，卡在卡片里会显得每张卡都半斤八两。
// 2) 状态必须三重编码（色点 + 文字 + 数值），不能只靠颜色——这是 docs/07 §4
//    对 StatusDot 的通用要求，状态条同理：磁盘 90% 用红色点，
//    色弱用户看不到区别，但「已用 90%」这行字所有人都能读。
// 3) 组件自己不发请求，只吃 props：状态来源唯一是 SSE + /api 对账（§7 铁律），
//    组件自算就成了第二份真相。
import { computed } from 'vue'

export interface StatusStripItem {
  /** 指标名，如「磁盘」「数据库」。 */
  label: string
  /** 当前值（文本），如「已用 62%」「正常」。 */
  value: string
  /** 严重度：决定色点颜色。缺省视为中性。 */
  tone?: 'ok' | 'warn' | 'bad' | 'neutral'
  /** 补充说明（可选），放在值后面。 */
  hint?: string
}

const props = withDefaults(
  defineProps<{
    items: StatusStripItem[]
  }>(),
  { items: () => [] },
)

/** 有内容才渲染：空数组不占位，避免页面上出现一条空白的横杠。 */
const visible = computed(() => props.items.filter((i) => i.label && i.value))
</script>

<template>
  <div v-if="visible.length > 0" class="sstrip" role="status" aria-label="系统状态">
    <div v-for="it in visible" :key="it.label" class="sstrip-item">
      <!-- 色点：形状 + 颜色双重编码（§4 禁颜色唯一载体），点本身 aria-hidden，
           真正的信息在同行的文字里。 -->
      <span class="sstrip-dot" :class="`sstrip-dot--${it.tone ?? 'neutral'}`" aria-hidden="true" />
      <span class="sstrip-label">{{ it.label }}</span>
      <span class="sstrip-value tnum">{{ it.value }}</span>
      <span v-if="it.hint" class="sstrip-hint">{{ it.hint }}</span>
    </div>
  </div>
</template>

<style scoped>
.sstrip {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px 28px;
  padding: 14px 20px;
  margin-bottom: 18px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  box-shadow: var(--shadow-1);
}
.sstrip-item {
  display: flex;
  align-items: baseline;
  gap: 8px;
}
/* 色点：8px 实心圆 + 外圈，色弱用户靠位置和旁边的文字也能分辨。 */
.sstrip-dot {
  width: 9px;
  height: 9px;
  border-radius: 50%;
  flex-shrink: 0;
  background: var(--neutral);
  align-self: center;
}
.sstrip-dot--ok { background: var(--success); }
.sstrip-dot--warn { background: var(--warning); }
.sstrip-dot--bad { background: var(--danger); }
.sstrip-dot--neutral { background: var(--text-3); }
/* 指标名：辅助档 13.5px。 */
.sstrip-label { font-size: var(--font-aux); color: var(--text-3); }
/* 指标值：正文档 16px，tabular-nums 保证数字刷新时不抖动。 */
.sstrip-value { font-size: var(--font-body); font-weight: var(--weight-semibold); color: var(--text-1); }
.sstrip-hint { font-size: var(--font-aux); color: var(--text-3); }
</style>
