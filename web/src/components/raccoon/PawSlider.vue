<script setup lang="ts">
// PawSlider：进度条端点的爪印滑块（docs/07 §4「爪印滑块：极简冲突可退圆点」）。
//
// 为什么这么设计：
// 1) 爪印是浣熊的识别符号，把它钉在进度端点上，滚动的进度条就有了「谁在搬」的存在感。
//    爪印用 SVG 画（三趾 + 一个掌垫），不用 emoji 冒充图标（docs/07 §5）。
// 2) 爪印是装饰不是信息：尺寸小、或页面进入极简 / 减少动效模式时它会糊成一团，
//    此时退化成一个小圆点——保住「端点上有东西」的语义，丢掉花纹（docs/07 §4「极简冲突可退圆点」）。
// 3) 位置用百分比而不是 JS 量像素：容器宽度变了爪印自动跟随，省掉 resize 监听与首帧跳动。
// 4) 轨道两端留半个爪印的余量（padding），否则 percent=0 / 100 时爪印会被容器裁掉一半。
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    /** 进度百分比（0–100，超界会被夹住）。 */
    percent: number
    /** 爪印尺寸（像素）。 */
    size?: number
    /** 极简模式：爪印退化为圆点（与其他极简元素冲突时用）。 */
    minimal?: boolean
    /** 减少动效：爪印退化为圆点，且进度不再过渡（尊重 prefers-reduced-motion）。 */
    reduceMotion?: boolean
    /** 填充色语义（对齐 task/TaskProgress 的三段配色）。 */
    tone?: 'accent' | 'info' | 'success'
    /** 轨道粗细（像素）。 */
    thickness?: number
    /** 无障碍标签（读屏会念出来）。 */
    label?: string
    /**
     * 只出爪印滑块（不渲染轨道与填充）。
     * 供 TaskProgress 复用：它自己有三段配色轨道与不确定进度条纹，
     * 只需要把爪印钉在端点当滑块——各自职责不重叠（docs/07 §4）。
     */
    thumbOnly?: boolean
  }>(),
  {
    size: 18,
    minimal: false,
    reduceMotion: false,
    tone: 'accent',
    thickness: 6,
    label: '进度',
    thumbOnly: false,
  },
)

/** 进度夹在 0–100：脏数据（NaN / 越界）不能让进度条画到轨道外面去。 */
const pct = computed(() => {
  const v = props.percent
  if (typeof v !== 'number' || !Number.isFinite(v)) return 0
  return Math.max(0, Math.min(100, v))
})

/** 退化：极简或减少动效时爪印退化为圆点。 */
const degrade = computed(() => props.minimal || props.reduceMotion)
</script>

<template>
  <div
    class="paw"
    :class="{ 'paw--static': reduceMotion, 'paw--thumb-only': thumbOnly }"
    :style="{ '--paw-size': size + 'px', '--paw-thickness': thickness + 'px' }"
  >
    <div
      v-if="!thumbOnly"
      class="paw-track"
      role="progressbar"
      :aria-valuenow="Math.round(pct)"
      aria-valuemin="0"
      aria-valuemax="100"
      :aria-label="label"
    >
      <div class="paw-fill" :class="`paw-fill--${tone}`" :style="{ width: pct + '%' }" />
    </div>
    <!-- thumbOnly：爪印直接绝对定位在宿主轨道上，宿主负责 role/aria（避免重复朗读）。 -->
    <div class="paw-thumb" :style="{ left: pct + '%' }">
        <!-- 爪印：三趾 + 一个掌垫 -->
        <svg
          v-if="!degrade"
          class="paw-print"
          :width="size"
          :height="size"
          viewBox="0 0 24 24"
          aria-hidden="true"
        >
          <g fill="var(--accent)" stroke="var(--surface)" stroke-width="1">
            <ellipse cx="6.9" cy="8.8" rx="2.4" ry="3.1" transform="rotate(-20 6.9 8.8)" />
            <ellipse cx="12" cy="6.7" rx="2.4" ry="3.2" />
            <ellipse cx="17.1" cy="8.8" rx="2.4" ry="3.1" transform="rotate(20 17.1 8.8)" />
            <ellipse cx="12" cy="15.6" rx="5.2" ry="4.3" />
          </g>
        </svg>
        <!-- 退化：2px 圆点，保留端点语义、丢掉花纹 -->
        <svg v-else class="paw-dot" :width="size" :height="size" viewBox="0 0 24 24" aria-hidden="true">
          <circle cx="12" cy="12" r="2" fill="var(--accent)" stroke="var(--surface)" stroke-width="1" />
        </svg>
    </div>
  </div>
</template>
<style scoped>
.paw {
  width: 100%;
  /* 两端各留半个爪印：percent 到 0 / 100 时爪印不被容器裁掉。 */
  padding: 0 calc(var(--paw-size) / 2);
}
/* thumbOnly：铺满宿主并由宿主定位（宿主须为 position: relative）。 */
.paw--thumb-only {
  position: absolute;
  inset: 0;
  padding: 0;
  pointer-events: none;
}
.paw-track {
  position: relative;
  height: var(--paw-thickness);
  border-radius: var(--radius-pill);
  background: var(--surface-3);
}
.paw-fill {
  height: 100%;
  border-radius: var(--radius-pill);
  /* 进度数字 240ms linear（docs/07 §6）。 */
  transition: width 240ms linear;
}
.paw-fill--accent {
  background: var(--gold);
}
.paw-fill--info {
  background: var(--info);
}
.paw-fill--success {
  background: var(--success);
}
.paw-thumb {
  position: absolute;
  top: 50%;
  transform: translate(-50%, -50%);
  line-height: 0;
  transition: left 240ms linear;
}
.paw--static .paw-fill,
.paw--static .paw-thumb {
  transition: none;
}
@media (prefers-reduced-motion: reduce) {
  .paw-fill,
  .paw-thumb {
    transition: none;
  }
}
</style>
