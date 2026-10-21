<script setup lang="ts">
// RaccoonFace：浣熊六表情（docs/07 §5）。
//
// 全部用 SVG 画，不用 emoji 冒充；眼罩/围巾/爪印是浣熊的识别特征。
// 状态：waiting / working / done / failed / cancelled / surprise。
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
    <!-- 耳朵 -->
    <circle cx="14" cy="14" r="8" fill="var(--accent)" />
    <circle cx="50" cy="14" r="8" fill="var(--accent)" />
    <circle cx="14" cy="14" r="4" fill="var(--gold-soft)" />
    <circle cx="50" cy="14" r="4" fill="var(--gold-soft)" />

    <!-- 头 -->
    <ellipse cx="32" cy="32" rx="24" ry="22" fill="var(--accent)" />
    <!-- 面部浅色区（浣熊面纹） -->
    <ellipse cx="32" cy="38" rx="17" ry="14" fill="var(--surface)" />

    <!-- 眼罩 -->
    <path
      v-if="mood.mask !== 'up'"
      d="M14 26 Q32 18 50 26 L50 32 Q32 26 14 32 Z"
      fill="var(--accent-strong)"
      opacity="0.85"
    />
    <path
      v-else
      d="M12 22 Q32 14 52 22 L52 26 Q32 20 12 26 Z"
      fill="var(--accent-strong)"
      opacity="0.7"
    />
    <path
      v-if="mood.mask === 'half'"
      d="M14 30 Q32 24 50 30 L50 34 Q32 30 14 34 Z"
      fill="var(--accent-strong)"
      opacity="0.6"
    />
    <path
      v-if="mood.mask === 'tilt'"
      d="M14 27 Q30 20 48 32 L47 37 Q30 27 15 32 Z"
      fill="var(--accent-strong)"
      opacity="0.85"
    />

    <!-- 眼睛 -->
    <g v-if="mood.eyes === 'focused'">
      <circle cx="24" cy="31" r="2.6" fill="var(--text-1)" />
      <circle cx="40" cy="31" r="2.6" fill="var(--text-1)" />
    </g>
    <g v-else-if="mood.eyes === 'wink'">
      <path d="M20 31 Q24 27 28 31" stroke="var(--text-1)" stroke-width="2" fill="none" stroke-linecap="round" />
      <circle cx="40" cy="31" r="2.6" fill="var(--text-1)" />
    </g>
    <g v-else-if="mood.eyes === 'flat'">
      <path d="M20 31 L28 31" stroke="var(--text-1)" stroke-width="2" stroke-linecap="round" />
      <path d="M36 31 L44 31" stroke="var(--text-1)" stroke-width="2" stroke-linecap="round" />
    </g>
    <g v-else-if="mood.eyes === 'calm'">
      <path d="M20 32 Q24 30 28 32" stroke="var(--text-1)" stroke-width="1.8" fill="none" stroke-linecap="round" />
      <path d="M36 32 Q40 30 44 32" stroke="var(--text-1)" stroke-width="1.8" fill="none" stroke-linecap="round" />
    </g>
    <g v-else-if="mood.eyes === 'sparkle'">
      <circle cx="24" cy="31" r="3.2" fill="var(--text-1)" />
      <circle cx="40" cy="31" r="3.2" fill="var(--text-1)" />
      <circle cx="25.2" cy="29.8" r="1" fill="var(--surface)" />
      <circle cx="41.2" cy="29.8" r="1" fill="var(--surface)" />
    </g>
    <g v-else>
      <path d="M20 30 Q24 28 28 30" stroke="var(--text-1)" stroke-width="1.8" fill="none" stroke-linecap="round" />
      <path d="M36 30 Q40 28 44 30" stroke="var(--text-1)" stroke-width="1.8" fill="none" stroke-linecap="round" />
    </g>

    <!-- 嘴 -->
    <path
      v-if="mood.mouth === 'smile'"
      d="M25 44 Q32 50 39 44"
      stroke="var(--text-1)"
      stroke-width="2"
      fill="none"
      stroke-linecap="round"
    />
    <path
      v-else-if="mood.mouth === 'flat'"
      d="M26 45 L38 45"
      stroke="var(--text-1)"
      stroke-width="2"
      stroke-linecap="round"
    />
    <path
      v-else-if="mood.mouth === 'dots'"
      d="M27 45 L27 45.5 M32 45 L32 45.5 M37 45 L37 45.5"
      stroke="var(--text-1)"
      stroke-width="2.4"
      stroke-linecap="round"
    />
    <path
      v-else-if="mood.mouth === 'arrow'"
      d="M27 44 L35 44 M32 41 L35 44 L32 47"
      stroke="var(--gold-strong)"
      stroke-width="2"
      fill="none"
      stroke-linecap="round"
      stroke-linejoin="round"
    />
    <g v-else-if="mood.mouth === 'grin'">
      <path d="M24 42 Q32 52 40 42 Z" fill="var(--text-1)" />
      <path d="M27 42.5 L37 42.5" stroke="var(--surface)" stroke-width="1.4" />
    </g>
    <g v-else>
      <ellipse cx="32" cy="45" rx="4" ry="3" fill="var(--text-1)" />
    </g>

    <!-- 腮红 -->
    <ellipse v-if="mood.blush" cx="18" cy="38" rx="3" ry="2" fill="var(--gold)" opacity="0.35" />
    <ellipse v-if="mood.blush" cx="46" cy="38" rx="3" ry="2" fill="var(--gold)" opacity="0.35" />

    <!-- 围巾（金） -->
    <path d="M14 50 Q32 58 50 50 L50 54 Q32 62 14 54 Z" fill="var(--gold)" />
    <path d="M46 53 L54 58 L50 60 L44 55 Z" fill="var(--gold-strong)" />
  </svg>
</template>

<style scoped>
.rface { display: block; }
</style>
