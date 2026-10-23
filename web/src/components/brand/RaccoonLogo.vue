<script setup lang="ts">
// RaccoonLogo：品牌徽标（docs/07 §5 浣熊识别特征：眼罩 / 围巾 / 爪印）。
//
// 为什么不能拿 RaccoonFace 当 Logo：
// RaccoonFace 是「状态表情」——给空态、失败态、通知用的拟人角色，
// 六套五官随时切换，缩到 22px 就是一团糊。品牌徽标是静态、对称、
// 缩任何尺寸都清晰的识别符号：正面浣熊头 + 眼罩 + 围巾飘带。
// 两者共用一套视觉语言（树影蓝灰毛色 + 围巾金），但职责不同。
//
// 两个变体：
//   mark  仅徽标图形（favicon、空态角标、窄屏）
//   full  徽标 + 字标（侧导航品牌区、登录页）
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    /** 图形尺寸（px）。full 变体下徽标高与此同高。 */
    size?: number
    /** mark = 仅图形；full = 图形 + 字标。 */
    variant?: 'mark' | 'full'
    /** full 变体的副标题（如「自托管下载中心」），留空则不渲染。 */
    tagline?: string
  }>(),
  { size: 36, variant: 'full', tagline: '' },
)

/** 字标主字号跟随图形尺寸的 0.52 倍，保证大小两档 Logo 比例一致。 */
const nameSize = computed(() => Math.round(props.size * 0.52))
</script>

<template>
  <span class="rlogo" :class="`rlogo--${variant}`">
    <svg
      :width="size"
      :height="size"
      viewBox="0 0 64 64"
      class="rlogo-mark"
      role="img"
      aria-label="浣熊 Raccoon"
    >
      <!-- 耳朵：外深内金（金内耳是品牌点缀，和围巾呼应） -->
      <circle cx="13" cy="13" r="8.5" fill="var(--logo-fur)" />
      <circle cx="51" cy="13" r="8.5" fill="var(--logo-fur)" />
      <circle cx="13" cy="13" r="4.2" fill="var(--gold)" />
      <circle cx="51" cy="13" r="4.2" fill="var(--gold)" />
      <!-- 头 -->
      <ellipse cx="32" cy="33" rx="23" ry="21" fill="var(--logo-fur)" />
      <!-- 面部浅色区（浣熊面纹） -->
      <ellipse cx="32" cy="38" rx="16.5" ry="13.5" fill="var(--logo-face)" />
      <!-- 眼罩：浣熊的识别特征，两条深色斜带横过眼部 -->
      <path d="M11 27 Q22 22.5 30 29.5 L29 34.5 Q21 30 11 33.5 Z" fill="var(--logo-mask)" />
      <path d="M53 27 Q42 22.5 34 29.5 L35 34.5 Q43 30 53 33.5 Z" fill="var(--logo-mask)" />
      <!-- 眼睛：眼罩带上的两个浅点 -->
      <circle cx="21" cy="30" r="2.3" fill="var(--logo-face)" />
      <circle cx="43" cy="30" r="2.3" fill="var(--logo-face)" />
      <!-- 鼻与微笑 -->
      <ellipse cx="32" cy="41" rx="2.8" ry="1.9" fill="var(--logo-mask)" />
      <path d="M27 45 Q32 48.5 37 45" stroke="var(--logo-mask)" stroke-width="1.8" fill="none" stroke-linecap="round" />
      <!-- 围巾（品牌金）+ 飘带 -->
      <path d="M13 51.5 Q32 58.5 51 51.5 L51 55.5 Q32 62.5 13 55.5 Z" fill="var(--gold)" />
      <path d="M45 54.5 L53.5 59.5 L49.5 61.5 L43 57 Z" fill="var(--gold-strong)" />
    </svg>

    <span v-if="variant === 'full'" class="rlogo-word">
      <span class="rlogo-name" :style="{ fontSize: nameSize + 'px' }">浣熊</span>
      <span class="rlogo-en" :style="{ fontSize: Math.max(10, Math.round(props.size * 0.3)) + 'px' }">Raccoon</span>
      <span v-if="tagline" class="rlogo-tagline">{{ tagline }}</span>
    </span>
  </span>
</template>

<style scoped>
/* 徽标配色用独立变量而不是直接用 --accent：
   侧导航是深树影蓝灰底，徽标若也用 --accent 会糊进底色；
   登录页是浅底。--logo-* 由使用方按底色覆盖，组件本身不猜环境。 */
.rlogo {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  --logo-fur: var(--accent);
  --logo-face: var(--surface);
  --logo-mask: var(--accent-strong);
}
.rlogo-mark { display: block; flex-shrink: 0; }
.rlogo-word { display: flex; flex-direction: column; line-height: 1.15; min-width: 0; }
.rlogo-name { font-weight: var(--weight-title); color: var(--logo-name, var(--text-1)); }
.rlogo-en {
  color: var(--logo-sub, var(--text-3));
  font-weight: var(--weight-caption);
  letter-spacing: 0.06em;
}
.rlogo-tagline {
  margin-top: 2px;
  font-size: var(--font-aux);
  color: var(--logo-sub, var(--text-3));
}
</style>
