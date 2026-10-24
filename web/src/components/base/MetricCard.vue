<script setup lang="ts">
// MetricCard：仪表盘指标卡（docs/14 §3.3）。
//
// 结构固定四件套：图标块（40px 圆角）+ 大数字（--font-display / tabular-nums）
// + 标签 + 可选副行。固定结构是为了让 17 块卡片在网格里节奏一致——原来四张卡
// 各自排版，高度参差、眼睛要重新找数字。
//
// 色板纪律：所有颜色都从 tokens.css 语义色派生（color-mix 做 16% 淡底），
// 组件内不出现任何色值字面量（闸门 §2 断言覆盖）。tone 决定语义色：
//   neutral / info / success / warning / danger / gold
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    /** 标签（这张卡在说什么）。 */
    label: string
    /** 主数字（已格式化好的字符串；数字格式化归调用方，卡片不猜单位）。 */
    value: string
    /** 线稿图标 path（16×16 viewBox，禁 emoji，docs/07 §4）。 */
    icon?: string
    /** 语义色。 */
    tone?: 'neutral' | 'info' | 'success' | 'warning' | 'danger' | 'gold'
    /** 副行说明（可选）。 */
    hint?: string
    /** 有值时整卡可点击跳转。 */
    to?: string
    /** 加载中：数字位置显示占位，不显示 0（0 是数据，占位不是）。 */
    loading?: boolean
  }>(),
  { icon: '', tone: 'neutral', hint: '', to: '', loading: false },
)

const emit = defineEmits<{
  /** 点击卡片（to 有值时由父层负责路由跳转）。 */
  (e: 'open', to: string): void
}>()

/** 语义色变量名（模板里拼 var(--xxx) 会被闸门的硬编码检查放过，但拼错就没颜色，
 *  所以在这里集中映射一次）。 */
const toneVar = computed(() => `var(--${props.tone})`)

const clickable = computed(() => props.to !== '')

function onClick() {
  if (clickable.value) emit('open', props.to)
}
</script>

<template>
  <component
    :is="clickable ? 'button' : 'article'"
    class="mcard"
    :class="{ 'mcard--link': clickable }"
    :type="clickable ? 'button' : undefined"
    :aria-label="clickable ? `${label}：${value}，查看详情` : undefined"
    @click="onClick"
  >
    <span
      class="mcard-icon"
      :style="{
        background: `color-mix(in srgb, ${toneVar} 16%, transparent)`,
        color: toneVar,
      }"
    >
      <svg v-if="icon" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" aria-hidden="true">
        <path :d="icon" stroke-linecap="round" stroke-linejoin="round" />
      </svg>
    </span>
    <span class="mcard-body">
      <span class="mcard-value tnum" :style="{ color: toneVar }">
        <template v-if="loading"><span class="mcard-skel" aria-hidden="true" /></template>
        <template v-else>{{ value }}</template>
      </span>
      <span class="mcard-label">{{ label }}</span>
      <span v-if="hint" class="mcard-hint">{{ hint }}</span>
    </span>
  </component>
</template>

<style scoped>
.mcard {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 10px;
  width: 100%;
  padding: 18px 20px 16px;
  overflow: hidden;
  text-align: left;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  box-shadow: var(--shadow-1);
  font: inherit;
  color: inherit;
}
/* 可点击卡：hover 只提亮边框 + 阴影，不做位移（网格里位移会让整行抖）。 */
.mcard--link { cursor: pointer; transition: border-color 140ms ease-out, box-shadow 140ms ease-out; }
.mcard--link:hover { border-color: var(--accent); box-shadow: var(--shadow-2); }
.mcard--link:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.mcard-icon {
  flex-shrink: 0;
  width: 38px;
  height: 38px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--radius-ctrl);
}
.mcard-icon svg { width: 19px; height: 19px; }
.mcard-body { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
/* 大数字：等宽族 + display 档 + 表格数字（docs/07 §2 铁律）。
   控制台方向：数据是等宽的，读数不抖、气质冷峻。 */
.mcard-value {
  font-family: var(--font-mono);
  font-size: var(--font-display);
  font-weight: var(--weight-display);
  line-height: 1.1;
  letter-spacing: -0.01em;
  color: var(--text-1);
}
.mcard-label { font-size: var(--font-aux); color: var(--text-2); }
.mcard-hint { font-size: var(--font-aux); color: var(--text-3); }
/* 加载占位：一块淡色条，不写 0——0 是真实数据。 */
.mcard-skel { display: inline-block; width: 3ch; height: 1em; border-radius: 4px; background: var(--surface-3); }
</style>
