<script setup lang="ts">
// TaskProgress：三色进度条（解析/下载/后处理）；不确定进度用条纹 + 文字说明（禁估算）。
// 端点滑块用 PawSlider 的 thumbOnly 模式：三段配色与条纹归本组件，爪印归浣熊组件（docs/07 §4）。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { formatBytes, formatPercent, formatSpeed } from '@/utils/format'
import PawSlider from '@/components/raccoon/PawSlider.vue'
import type { Task } from '@/api/types'

const props = withDefaults(
  defineProps<{ task: Task; compact?: boolean }>(),
  { compact: false },
)

/** 系统级「减少动效」偏好（docs/07 §6）：爪印退化为圆点、进度不过渡。 */
const reduceMotion = ref(false)
function syncReduceMotion() {
  reduceMotion.value = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false
}
onMounted(() => {
  syncReduceMotion()
  window.matchMedia?.('(prefers-reduced-motion: reduce)').addEventListener('change', syncReduceMotion)
})
onBeforeUnmount(() => {
  window.matchMedia?.('(prefers-reduced-motion: reduce)').removeEventListener('change', syncReduceMotion)
})

/**
 * 进度阶段（决定条色）。
 *
 * 终态必须单独成支：之前 done/failed/cancelled 全落进 default → 'idle'，
 * 而 idle 用的是 --text-3（弱化文字色）——一条 100% 宽的灰蓝条看着像
 * 「什么都没开始」，用户完全读不出「已完成」。现在终态对齐全站的
 * STATUS_TONE 约定（utils/format.ts）：完成=success、失败=danger、取消=muted。
 */
const stage = computed(() => {
  switch (props.task.status) {
    case 'parsing':
      return 'parse'
    case 'downloading':
      return 'download'
    case 'post':
      return 'post'
    case 'done':
      return 'done'
    case 'failed':
      return 'failed'
    case 'cancelled':
      return 'cancelled'
    default:
      return 'idle'
  }
})

/** 是否不确定进度（解析中 / 后处理 / 下载大小未知且引擎未报百分比）。
 *  后处理（post）没有可信百分比：ffmpeg 合并/转码不吐进度，写个固定 100%
 *  或者按时间猜都是在骗人（docs/01 诚实原则）——统一走条纹 + 文字说明。
 *  引擎已吐出真实百分比（progress>0，含合集任务的全局折算）时即使大小未知
 *  也按确定进度显示——原先只看 expected_size，合集任务永远条纹满宽，
 *  视觉上像「进度满了」被用户当成进度错误（2026-10-24 实测踩过）。 */
const indeterminate = computed(
  () =>
    props.task.status === 'parsing' ||
    props.task.status === 'post' ||
    (props.task.expected_size === null && props.task.status === 'downloading' && !(props.task.progress > 0)),
)

/** 不确定进度的说明文案：说清「在干什么」，不假装知道还剩多少。 */
const indeterminateText = computed(() => {
  switch (props.task.status) {
    case 'parsing':
      return '解析中，大小未知'
    case 'post':
      return '后处理中，进度未知'
    default:
      return '下载中，大小未知'
  }
})

const pct = computed(() => {
  if (props.task.status === 'done') return 100
  return Math.max(0, Math.min(100, props.task.progress ?? 0))
})

/**
 * 爪印色与进度条同色系（parse=info / post=success / done=success / failed=danger）。
 * 爪印是进度条的端点标记，它说「到这儿了」——颜色必须跟状态一致，
 * 否则完成的进度条末端顶着一枚薄荷色爪印，又被读成「还在下载」。
 */
const pawTone = computed<'accent' | 'info' | 'success' | 'danger' | 'neutral'>(() => {
  switch (stage.value) {
    case 'parse':
      return 'info'
    case 'post':
      return 'success'
    case 'done':
      return 'success'
    case 'failed':
      return 'danger'
    case 'cancelled':
      return 'neutral'
    default:
      return 'accent'
  }
})

const sizeText = computed(() => {
  const dl = props.task.downloaded_size ?? 0
  const exp = props.task.expected_size
  if (!exp) return formatBytes(dl)
  return `${formatBytes(dl)} / ${formatBytes(exp)}`
})
</script>

<template>
  <div class="tprog" :class="[`tprog--${stage}`, { 'tprog--compact': compact }]">
    <div class="tprog-bar" :class="{ 'tprog-bar--indeterminate': indeterminate }">
      <!-- 不确定进度的宽度由 CSS（28% 条纹块往复）控制，不设内联 width，
           否则内联样式会盖住条纹块的移动宽度、退化为满格假进度。 -->
      <div class="tprog-fill" :style="indeterminate ? undefined : { width: pct + '%' }" />
      <!-- 端点爪印滑块：不确定进度（条纹）没有端点语义，此时不画爪印。 -->
      <PawSlider
        v-if="!indeterminate"
        class="tprog-paw"
        :percent="pct"
        thumb-only
        :size="compact ? 14 : 18"
        :tone="pawTone"
        :reduce-motion="reduceMotion"
        label="任务进度"
      />
    </div>
    <div v-if="!compact" class="tprog-meta">
      <span class="tnum">{{ indeterminate ? indeterminateText : formatPercent(pct) }}</span>
      <span class="tnum tprog-size">{{ sizeText }}</span>
      <span v-if="task.status === 'downloading'" class="tnum tprog-speed">{{ formatSpeed(task.speed_bps) }}</span>
    </div>
  </div>
</template>

<style scoped>
.tprog { display: flex; flex-direction: column; gap: 4px; width: 100%; }
.tprog-bar {
  position: relative; /* 端点爪印滑块（PawSlider thumbOnly）绝对定位的基准 */
  height: 6px;
  border-radius: 999px;
  background: var(--surface-3);
  /* 不加 overflow:hidden：爪印比轨道高，会被裁掉；fill 自带圆角无需父级裁剪。 */
}
.tprog--compact .tprog-bar { height: 4px; }
.tprog-fill {
  height: 100%;
  border-radius: 999px;
  transition: width 240ms linear;
  background: var(--accent);
}
.tprog--parse .tprog-fill { background: var(--info); }
.tprog--post .tprog-fill { background: var(--success); }
/* 终态：完成=成功绿、失败=危险红、取消=弱化灰。
   与全站 STATUS_TONE（utils/format.ts）一致，进度条不再自建一套语义色。 */
.tprog--done .tprog-fill { background: var(--success); }
.tprog--failed .tprog-fill { background: var(--danger); }
.tprog--cancelled .tprog-fill { background: var(--text-3); }
.tprog--idle .tprog-fill { background: var(--text-3); }
/* 条纹底色跟随阶段：解析=信息蓝、后处理=成功绿。写死 --info 会让后处理
   显示成一条「蓝色条纹的绿色任务」，阶段色与条纹色自相矛盾。 */
.tprog--parse { --stripe: var(--info); }
.tprog--post { --stripe: var(--success); }
/* 不确定进度（条纹块移动）：用一段 28% 宽的条纹在轨道内往复移动，
   绝不能用 100% 宽的全条条纹——那在视觉上和「进度快满了」无法区分，
   用户会把「大小未知」误读成「快下完了」（2026-10-24 实测踩过）。 */
.tprog-bar--indeterminate .tprog-fill {
  width: 28%;
  background: var(--stripe, var(--info));
  background-image: repeating-linear-gradient(
    115deg,
    var(--stripe, var(--info)) 0 10px,
    color-mix(in srgb, var(--stripe, var(--info)) 45%, transparent) 10px 20px
  );
  background-size: 28px 100%;
  animation: tprog-slide 1.6s ease-in-out infinite alternate;
}
@keyframes tprog-slide {
  from { margin-left: 0; }
  to { margin-left: 72%; }
}
@media (prefers-reduced-motion: reduce) {
  .tprog-bar--indeterminate .tprog-fill { animation: none; }
}
.tprog-meta {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: var(--font-aux);
  color: var(--text-3);
}
.tprog-size { color: var(--text-2); }
.tprog-speed { margin-left: auto; }
</style>
