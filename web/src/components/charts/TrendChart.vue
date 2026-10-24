<script setup lang="ts">
// TrendChart：折线趋势图（docs/14 §3.4 选型：纯 SVG，不引图表库）。
//
// 为什么手写 SVG：
// 1) 单静态二进制（embed.FS）不能被图表库撑大；
// 2) 数据量只有 7/30/60 个点，polyline 足够；
// 3) jsdom 里 SVG 是真 DOM，`points` 属性可断言——canvas 方案测不了。
//
// 诚实原则：
// - 没有数据（空数组或全零）就写「暂无数据」，不画一条贴底的假线；
// - 对比序列（compare）是可选的独立刻度虚线，只用于「同窗口另一维度看走势」，
//   图例必须写清楚它的含义（调用方传 compareLabel），不与主线共用刻度冒充同量纲。
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    /** 数据点（升序）。 */
    points: { date: string; value: number }[]
    /** 对比序列（与 points 等长，可为空）。独立归一化，虚线绘制。 */
    compare?: number[]
    /** 对比序列图例（如「下载量（相对峰值）」）；传了 compare 才有意义。 */
    compareLabel?: string
    /** 图高（px）。 */
    height?: number
    /** 无障碍描述（屏幕阅读器读这个，而不是读折线）。 */
    label?: string
    /** 值格式化（tooltip / 峰值标注用）。 */
    format?: (v: number) => string
    /** 是否显示 x 轴刻度（30 点时只标首/中/末，避免挤成一团）。 */
    showAxis?: boolean
  }>(),
  { height: 140, label: '趋势图', showAxis: true, format: undefined, compare: () => [], compareLabel: '' },
)

/** 画布几何（viewBox 固定 0..320 × 0..height，外层用 CSS 拉满宽度）。 */
const W = 320
const PAD_X = 6
const PAD_TOP = 10
const PAD_BOTTOM = 18

const H = computed(() => props.height)
const innerH = computed(() => Math.max(1, H.value - PAD_TOP - PAD_BOTTOM))

/** 有数据 = 有点且不全为 0：全零窗口画出来是一条贴底假线，视觉等于「坏了」。 */
const hasData = computed(
  () => props.points.length > 0 && props.points.some((p) => p.value > 0),
)
const maxValue = computed(() => Math.max(1, ...props.points.map((p) => p.value)))

/** 每个点的坐标（x 均匀分布；只有一个点时放中间）。 */
const coords = computed(() => {
  const n = props.points.length
  if (!n) return [] as { x: number; y: number; p: { date: string; value: number } }[]
  const span = W - PAD_X * 2
  return props.points.map((p, i) => {
    const x = n === 1 ? W / 2 : PAD_X + (span * i) / (n - 1)
    const ratio = p.value / maxValue.value
    const y = PAD_TOP + innerH.value * (1 - ratio)
    return { x, y, p }
  })
})

/** polyline 的 points 属性（测试直接断言它）。 */
const polyline = computed(() =>
  coords.value.map((c) => `${c.x.toFixed(1)},${c.y.toFixed(1)}`).join(' '),
)

/** 对比序列：独立归一化（除以自身峰值），虚线；长度必须与 points 对齐才画。 */
const compareUsable = computed(
  () => props.compare.length === props.points.length && props.compare.some((v) => v > 0),
)
const compareMax = computed(() => Math.max(1, ...props.compare))
const comparePolyline = computed(() => {
  if (!compareUsable.value) return ''
  const n = props.points.length
  const span = W - PAD_X * 2
  return props.compare
    .map((v, i) => {
      const x = n === 1 ? W / 2 : PAD_X + (span * i) / (n - 1)
      const y = PAD_TOP + innerH.value * (1 - v / compareMax.value)
      return `${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')
})

/** 面积填充：折线 + 回到基线闭合。 */
const areaPath = computed(() => {
  const cs = coords.value
  if (!cs.length) return ''
  const baseY = PAD_TOP + innerH.value
  const head = `M ${cs[0].x.toFixed(1)} ${baseY.toFixed(1)}`
  const line = cs.map((c) => `L ${c.x.toFixed(1)} ${c.y.toFixed(1)}`).join(' ')
  const tail = `L ${cs[cs.length - 1].x.toFixed(1)} ${baseY.toFixed(1)} Z`
  return `${head} ${line} ${tail}`
})

/** 末端点（最新一天）。 */
const last = computed(() => coords.value[coords.value.length - 1])

/** x 轴刻度：首 / 中 / 末三个，避免 30 点时日期挤成黑条。 */
const axisTicks = computed(() => {
  const cs = coords.value
  if (cs.length <= 1) return cs
  const mid = Math.floor((cs.length - 1) / 2)
  const idx = cs.length <= 3 ? cs.map((_, i) => i) : [0, mid, cs.length - 1]
  return idx.map((i) => cs[i])
})

/** 日期 → MM-DD（年份对 7/30 天窗口没有信息量）。 */
function shortDate(d: string): string {
  return d.length >= 10 ? d.slice(5) : d
}

const fmt = computed(() => props.format ?? ((v: number) => String(v)))
</script>

<template>
  <div class="tchart">
    <svg
      v-if="hasData"
      class="tchart-svg"
      :viewBox="`0 0 ${W} ${H}`"
      preserveAspectRatio="none"
      role="img"
      :aria-label="label"
    >
      <!-- 基线：没有它，低值折线会飘在半空 -->
      <line
        class="tchart-base"
        :x1="PAD_X"
        :x2="W - PAD_X"
        :y1="PAD_TOP + innerH"
        :y2="PAD_TOP + innerH"
      />
      <path class="tchart-area" :d="areaPath" />
      <polyline v-if="comparePolyline" class="tchart-compare" :points="comparePolyline" />
      <polyline class="tchart-line" :points="polyline" />
      <circle v-if="last" class="tchart-dot" :cx="last.x" :cy="last.y" r="2.6" />
    </svg>

    <!-- 空态：一句「暂无数据」，不画假线（诚实原则；全零窗口同样算无数据） -->
    <p v-else class="tchart-empty">暂无数据</p>

    <div v-if="hasData && showAxis" class="tchart-axis" aria-hidden="true">
      <span v-for="t in axisTicks" :key="t.p.date" class="tchart-tick tnum">{{ shortDate(t.p.date) }}</span>
    </div>

    <!-- 峰值与末值：图表之外给一句人话，读者不必自己数格子 -->
    <p v-if="hasData" class="tchart-sum">
      峰值 <span class="tnum">{{ fmt(maxValue) }}</span>
      <span class="tchart-sep">·</span>
      最新 <span class="tnum">{{ fmt(points[points.length - 1].value) }}</span>
      <template v-if="comparePolyline && compareLabel">
        <span class="tchart-sep">·</span>
        <span class="tchart-legend"><i class="tchart-legend-swatch" aria-hidden="true" />{{ compareLabel }}</span>
      </template>
    </p>
  </div>
</template>

<style scoped>
.tchart { display: flex; flex-direction: column; gap: 6px; }
.tchart-svg { width: 100%; height: auto; display: block; overflow: visible; }
.tchart-base { stroke: var(--border); stroke-width: 1; }
/* 面积：单色低透明（不是装饰性渐变，docs/07 §6 允许图表功能填充） */
.tchart-area { fill: var(--accent); fill-opacity: 0.1; stroke: none; }
.tchart-line { fill: none; stroke: var(--accent); stroke-width: 1.8; stroke-linejoin: round; stroke-linecap: round; }
.tchart-dot { fill: var(--accent); stroke: var(--surface); stroke-width: 1.2; }
/* 对比序列：中性色虚线（不与主线抢强调色），独立刻度由图例说明。 */
.tchart-compare {
  fill: none;
  stroke: var(--text-3);
  stroke-width: 1.4;
  stroke-dasharray: 4 4;
  stroke-linejoin: round;
  stroke-linecap: round;
  opacity: 0.8;
}
.tchart-axis { display: flex; justify-content: space-between; font-size: var(--font-aux); color: var(--text-3); }
.tchart-sum { margin: 0; font-size: var(--font-aux); color: var(--text-3); display: flex; align-items: center; flex-wrap: wrap; }
.tchart-sep { margin: 0 6px; }
.tchart-legend { display: inline-flex; align-items: center; gap: 5px; }
.tchart-legend-swatch {
  width: 14px;
  height: 0;
  border-top: 2px dashed var(--text-3);
  display: inline-block;
}
.tchart-empty {
  margin: 0;
  padding: 24px 0;
  text-align: center;
  font-size: var(--font-aux);
  color: var(--text-3);
}
</style>
