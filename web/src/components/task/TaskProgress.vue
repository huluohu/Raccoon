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

/** 进度阶段（决定条色）。 */
const stage = computed(() => {
  switch (props.task.status) {
    case 'parsing':
      return 'parse'
    case 'downloading':
      return 'download'
    case 'post':
      return 'post'
    default:
      return 'idle'
  }
})

/** 是否不确定进度（解析中/大小未知）。 */
const indeterminate = computed(
  () => props.task.status === 'parsing' || (props.task.expected_size === null && props.task.status === 'downloading'),
)

const pct = computed(() => {
  if (props.task.status === 'done') return 100
  return Math.max(0, Math.min(100, props.task.progress ?? 0))
})

/** 爪印色与进度条同色系（parse=info / post=success / 其余=gold）。 */
const pawTone = computed<'gold' | 'info' | 'success'>(() => {
  if (stage.value === 'parse') return 'info'
  if (stage.value === 'post') return 'success'
  return 'gold'
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
      <div class="tprog-fill" :style="{ width: indeterminate ? '100%' : pct + '%' }" />
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
      <span class="tnum">{{ indeterminate ? (task.status === 'parsing' ? '解析中，大小未知' : '大小未知') : formatPercent(pct) }}</span>
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
  background: var(--gold);
}
.tprog--parse .tprog-fill { background: var(--info); }
.tprog--post .tprog-fill { background: var(--success); }
.tprog--idle .tprog-fill { background: var(--text-3); }
.tprog-bar--indeterminate .tprog-fill {
  background-image: repeating-linear-gradient(
    115deg,
    var(--info) 0 10px,
    color-mix(in srgb, var(--info) 45%, transparent) 10px 20px
  );
  background-size: 28px 100%;
  animation: tprog-stripe 1.1s linear infinite;
}
@keyframes tprog-stripe {
  from { background-position: 0 0; }
  to { background-position: 28px 0; }
}
@media (prefers-reduced-motion: reduce) {
  .tprog-bar--indeterminate .tprog-fill { animation: none; }
}
.tprog-meta {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: var(--font-caption);
  color: var(--text-3);
}
.tprog-size { color: var(--text-2); }
.tprog-speed { margin-left: auto; }
</style>
