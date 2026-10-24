<script setup lang="ts">
// RaccoonFace：浣熊六表情（docs/07 §5）。
//
// 全部用 SVG 画，不用 emoji 冒充；眼罩/围巾/爪印是浣熊的识别特征。
// 状态：waiting / working / done / failed / cancelled / surprise。
//
// 配色走 --logo-* 换皮变量（docs/07 §2 四主题）：毛色/脸底/眼罩/围巾随主题走，
// 禁用 --accent/--surface 当毛色——那是页面语义，不是浣熊的皮。
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{ face?: 'waiting' | 'working' | 'done' | 'failed' | 'cancelled' | 'surprise'; size?: number }>(),
  { face: 'waiting', size: 48 },
)

/** 各表情的眼部与嘴部差异（用同一套骨架，只换细节）。 */
const mood = computed(() => {
  switch (props.face) {
    case 'working':
      return { mask: 'up', eyes: 'focused', mouth: 'arrow', blush: false }
    case 'done':
      return { mask: 'up', eyes: 'wink', mouth: 'smile', blush: true }
    case 'failed':
      return { mask: 'tilt', eyes: 'flat', mouth: 'flat', blush: false }
    case 'cancelled':
      return { mask: 'up', eyes: 'calm', mouth: 'dots', blush: false }
    case 'surprise':
      return { mask: 'up', eyes: 'sparkle', mouth: 'grin', blush: true }
    default:
      return { mask: 'half', eyes: 'half', mouth: 'yawn', blush: false }
  }
})

/** 眼色随眼罩位置：罩在眼上→浅色眼点；推上额头→眼睛露在白脸上用深墨。 */
const eyeColor = computed(() =>
  mood.value.mask === 'up' ? 'var(--logo-mask)' : 'var(--logo-face)',
)
</script>

<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 64 64"
    class="rface"
    role="img"
    :aria-label="`浣熊表情：${face}`"
  >
    <!-- 耳朵：外毛内耳（耳色随主题） -->
    <circle cx="14" cy="14" r="8" fill="var(--logo-fur)" />
    <circle cx="50" cy="14" r="8" fill="var(--logo-fur)" />
    <circle cx="14" cy="14" r="4" fill="var(--logo-ear)" />
    <circle cx="50" cy="14" r="4" fill="var(--logo-ear)" />

    <!-- 头 -->
    <ellipse cx="32" cy="32" rx="24" ry="22" fill="var(--logo-fur)" />
    <!-- 面部浅色区（浣熊面纹） -->
    <ellipse cx="32" cy="38" rx="17" ry="14" fill="var(--logo-face)" />

    <!-- 眼罩：整块连体（与品牌徽标同一形状语言）。
         half=罩在眼上 / up=推上额头 / tilt=歪斜（没精打采）。 -->
    <path
      v-if="mood.mask === 'half'"
      d="M 12 28 Q 21 21.5 32 26.5 Q 43 21.5 52 28 Q 53 35.5 44.5 37 Q 37.5 38 32 33.5 Q 26.5 38 19.5 37 Q 11 35.5 12 28 Z"
      fill="var(--logo-mask)"
    />
    <path
      v-else-if="mood.mask === 'up'"
      d="M 14 17.5 Q 23 12 32 16 Q 41 12 50 17.5 Q 50.5 23.5 43 25 Q 37 26 32 22 Q 27 26 21 25 Q 13.5 23.5 14 17.5 Z"
      fill="var(--logo-mask)"
      opacity="0.9"
    />
    <path
      v-else-if="mood.mask === 'tilt'"
      d="M 12.5 24.5 Q 22 18.5 32.5 24 Q 43.5 18.5 51.5 26.5 Q 53 34 43.5 37 Q 36.5 39 31.5 33 Q 27 38.5 19.5 36.5 Q 11 33.5 12.5 24.5 Z"
      fill="var(--logo-mask)"
    />

    <!-- 眼睛（眼罩上的深墨五官） -->
    <g v-if="mood.eyes === 'focused'">
      <circle cx="24" cy="31" r="2.6" :fill="eyeColor" />
      <circle cx="40" cy="31" r="2.6" :fill="eyeColor" />
    </g>
    <g v-else-if="mood.eyes === 'wink'">
      <path d="M20 31 Q24 27 28 31" :stroke="eyeColor" stroke-width="2" fill="none" stroke-linecap="round" />
      <circle cx="40" cy="31" r="2.6" :fill="eyeColor" />
    </g>
    <g v-else-if="mood.eyes === 'flat'">
      <path d="M20 31 L28 31" :stroke="eyeColor" stroke-width="2" stroke-linecap="round" />
      <path d="M36 31 L44 31" :stroke="eyeColor" stroke-width="2" stroke-linecap="round" />
    </g>
    <g v-else-if="mood.eyes === 'calm'">
      <path d="M20 32 Q24 30 28 32" :stroke="eyeColor" stroke-width="1.8" fill="none" stroke-linecap="round" />
      <path d="M36 32 Q40 30 44 32" :stroke="eyeColor" stroke-width="1.8" fill="none" stroke-linecap="round" />
    </g>
    <g v-else-if="mood.eyes === 'sparkle'">
      <circle cx="24" cy="31" r="3.2" :fill="eyeColor" />
      <circle cx="40" cy="31" r="3.2" :fill="eyeColor" />
      <circle cx="25.2" cy="29.8" r="1" fill="var(--logo-face)" />
      <circle cx="41.2" cy="29.8" r="1" fill="var(--logo-face)" />
    </g>
    <g v-else>
      <path d="M20 30 Q24 28 28 30" :stroke="eyeColor" stroke-width="1.8" fill="none" stroke-linecap="round" />
      <path d="M36 30 Q40 28 44 30" :stroke="eyeColor" stroke-width="1.8" fill="none" stroke-linecap="round" />
    </g>

    <!-- 嘴 -->
    <path
      v-if="mood.mouth === 'smile'"
      d="M25 44 Q32 50 39 44"
      stroke="var(--logo-mask)"
      stroke-width="2"
      fill="none"
      stroke-linecap="round"
    />
    <path
      v-else-if="mood.mouth === 'flat'"
      d="M26 45 L38 45"
      stroke="var(--logo-mask)"
      stroke-width="2"
      stroke-linecap="round"
    />
    <path
      v-else-if="mood.mouth === 'dots'"
      d="M27 45 L27 45.5 M32 45 L32 45.5 M37 45 L37 45.5"
      stroke="var(--logo-mask)"
      stroke-width="2.4"
      stroke-linecap="round"
    />
    <path
      v-else-if="mood.mouth === 'arrow'"
      d="M27 44 L35 44 M32 41 L35 44 L32 47"
      stroke="var(--logo-scarf-strong)"
      stroke-width="2"
      fill="none"
      stroke-linecap="round"
      stroke-linejoin="round"
    />
    <g v-else-if="mood.mouth === 'grin'">
      <path d="M24 42 Q32 52 40 42 Z" fill="var(--logo-mask)" />
      <path d="M27 42.5 L37 42.5" stroke="var(--logo-face)" stroke-width="1.4" />
    </g>
    <g v-else>
      <ellipse cx="32" cy="45" rx="4" ry="3" fill="var(--logo-mask)" />
    </g>

    <!-- 腮红（围巾色淡染） -->
    <ellipse v-if="mood.blush" cx="18" cy="38" rx="3" ry="2" fill="var(--logo-scarf)" opacity="0.35" />
    <ellipse v-if="mood.blush" cx="46" cy="38" rx="3" ry="2" fill="var(--logo-scarf)" opacity="0.35" />

    <!-- 围巾（随主题换皮） -->
    <path d="M14 50 Q32 58 50 50 L50 54 Q32 62 14 54 Z" fill="var(--logo-scarf)" />
    <path d="M46 53 L54 58 L50 60 L44 55 Z" fill="var(--logo-scarf-strong)" />
  </svg>
</template>

<style scoped>
.rface { display: block; }
</style>
