<script setup lang="ts">
// DonutChart：环形占比图（docs/14 §3.4：纯 SVG，不引图表库）。
//
// 用 stroke-dasharray/dashoffset 画环，中间放主数字。图例独立成列（色点 + 文案），
// 色点与环用同一个语义色变量——颜色不写死，闸门 §2 会拦。
//
// 诚实原则：percent 为 null 表示「无法计算」（如成功率为空），
// 环画成满格灰 + 中间显示「—」，不假装 0%。
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    /** 占比 0..100；null = 无法计算（显示「—」，环不填充）。 */
    percent: number | null
    /** 中心主数字下方的小标签（如「已用」）。 */
    centerLabel?: string
    /** 中心主数字（默认用 percent 取整 + %）。 */
    centerValue?: string
    /** 环色语义。 */
    tone?: 'info' | 'success' | 'warning' | 'danger' | 'gold' | 'neutral'
    /** 图例条目。 */
    legend?: { label: string; value: string }[]
    /** 无障碍描述。 */
    label?: string
    /** 尺寸（px，正方形）。 */
    size?: number
  }>(),
  {
    centerLabel: '',
    centerValue: '',
    tone: 'info',
    legend: () => [],
    label: '占比图',
    size: 120,
  },
)

/** 半径/周长：viewBox 固定 0..40，线宽 4，留 2 的余量给圆头端点。 */
const R = 16
const CIRC = 2 * Math.PI * R

/** 有效占比（null → 0 填充，但文案仍显示「—」）。 */
const ratio = computed(() => {
  if (props.percent === null) return 0
  return Math.max(0, Math.min(100, props.percent)) / 100
})

/** dasharray 的第二段（空白长度）。测试直接断言这个比例。 */
const dashOffset = computed(() => CIRC * (1 - ratio.value))

const toneVar = computed(() => `var(--${props.tone})`)

const display = computed(() => {
  if (props.centerValue) return props.centerValue
  if (props.percent === null) return '—'
  return `${Math.round(props.percent)}%`
})
</script>

<template>
  <div class="donut" :style="{ width: size + 'px' }">
    <svg :viewBox="`0 0 40 40`" role="img" :aria-label="`${label}：${display}`">
      <!-- 轨道 -->
      <circle class="donut-track" cx="20" cy="20" :r="R" />
      <!-- 数据环：从 12 点方向起画，圆头端点 -->
      <circle
        v-if="percent !== null"
        class="donut-ring"
        cx="20"
        cy="20"
        :r="R"
        :stroke="toneVar"
        :stroke-dasharray="`${CIRC} ${CIRC}`"
        :stroke-dashoffset="dashOffset"
        transform="rotate(-90 20 20)"
      />
    </svg>
    <div class="donut-center">
      <span class="donut-value tnum">{{ display }}</span>
      <span v-if="centerLabel" class="donut-label">{{ centerLabel }}</span>
    </div>
    <ul v-if="legend.length" class="donut-legend">
      <li v-for="item in legend" :key="item.label">
        <span class="donut-dot" :style="{ background: toneVar }" aria-hidden="true" />
        <span class="donut-legend-label">{{ item.label }}</span>
        <span class="donut-legend-value tnum">{{ item.value }}</span>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.donut { position: relative; display: flex; flex-direction: column; align-items: center; gap: 8px; }
.donut svg { width: 100%; height: auto; display: block; }
.donut-track { fill: none; stroke: var(--surface-3); stroke-width: 4; }
.donut-ring { fill: none; stroke-width: 4; stroke-linecap: round; transition: stroke-dashoffset 240ms linear; }
.donut-center {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  /* 中心文字压在环内：环占满正方形，中心区高度 = 宽度 */
  aspect-ratio: 1;
  pointer-events: none;
}
.donut-value { font-size: var(--font-title-2); font-weight: var(--weight-title); color: var(--text-1); line-height: 1.1; }
.donut-label { font-size: var(--font-aux); color: var(--text-3); }
.donut-legend { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 4px; width: 100%; }
.donut-legend li { display: flex; align-items: center; gap: 6px; font-size: var(--font-aux); color: var(--text-2); }
.donut-dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
.donut-legend-label { flex: 1; }
.donut-legend-value { color: var(--text-1); }
</style>
