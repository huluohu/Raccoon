<script setup lang="ts">
// RaccoonLogo：品牌徽标（docs/07 §5 浣熊识别特征：连体眼罩 / 环尾 / 围巾）。
//
// 为什么不能拿 RaccoonFace 当 Logo：
// RaccoonFace 是「状态表情」——给空态、失败态、通知用的拟人角色，
// 六套五官随时切换，缩到 22px 就是一团糊。品牌徽标是静态、对称、
// 缩任何尺寸都清晰的识别符号：正面浣熊头 + 连体眼罩 + 环纹尾 + 围巾飘带。
// 两者共用一套视觉语言（四主题换皮的浣熊形象变量），但职责不同。
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
      <!-- 环尾（浣熊第二识别物，先画压在头后）：
           基线粗描边 = 尾身，虚线描边同路径 = 环纹（dasharray 沿路径分段成环）。
           早期版本没有尾巴、眼罩是两条细斜带——缩到 22px 就是一只哈士奇；
           连体眼罩 + 环尾才把「浣熊」钉死。 -->
      <path
        d="M 44 51 Q 60 46 58.5 27"
        stroke="var(--logo-fur)"
        stroke-width="9"
        fill="none"
        stroke-linecap="round"
      />
      <path
        d="M 44 51 Q 60 46 58.5 27"
        stroke="var(--logo-mask)"
        stroke-width="9"
        fill="none"
        stroke-dasharray="4 4.5"
      />
      <!-- 耳朵：圆角三角（外毛内耳），不是圆——圆耳是熊 -->
      <path d="M 12.5 22 Q 9.5 8.5 21 10.5 Q 26.5 11.8 28 18 Z" fill="var(--logo-fur)" />
      <path d="M 51.5 22 Q 54.5 8.5 43 10.5 Q 37.5 11.8 36 18 Z" fill="var(--logo-fur)" />
      <path d="M 16 18.5 Q 15 11.5 21.5 13 Q 24.5 14 25.5 17.5 Z" fill="var(--logo-ear)" />
      <path d="M 48 18.5 Q 49 11.5 42.5 13 Q 39.5 14 38.5 17.5 Z" fill="var(--logo-ear)" />
      <!-- 头：偏左给尾巴让位 -->
      <ellipse cx="30" cy="35" rx="20" ry="19" fill="var(--logo-fur)" />
      <!-- 面部浅色区（浣熊面纹） -->
      <ellipse cx="30" cy="40" rx="15" ry="12" fill="var(--logo-face)" />
      <!-- 眼罩：整块连体（鼻梁处微收），浣熊的眼罩是一体的，分开的两条是哈士奇 -->
      <path
        d="M 10.5 28.5 Q 20 22.5 30 27 Q 40 22.5 49.5 28.5 Q 50.5 35.5 42.5 37 Q 36 38 31.5 33.5 Q 30.5 32.5 29.5 33.5 Q 25 38 18.5 37 Q 9.5 35.5 10.5 28.5 Z"
        fill="var(--logo-mask)"
      />
      <!-- 眼睛：眼罩上的两个浅点 -->
      <circle cx="21.5" cy="30" r="2.7" fill="var(--logo-face)" />
      <circle cx="40.5" cy="30" r="2.7" fill="var(--logo-face)" />
      <!-- 鼻与微笑 -->
      <path
        d="M 27.5 41.5 L 32.5 41.5 Q 33.5 41.5 32.9 42.5 L 31 45.2 Q 30 46.3 29 45.2 L 27.1 42.5 Q 26.5 41.5 27.5 41.5 Z"
        fill="var(--logo-mask)"
      />
      <path
        d="M 30 46.5 L 30 48 M 30 48 Q 27.5 50 25.5 48.3 M 30 48 Q 32.5 50 34.5 48.3"
        stroke="var(--logo-mask)"
        stroke-width="1.6"
        fill="none"
        stroke-linecap="round"
      />
      <!-- 围巾（随主题换皮：night=薄荷 / scarf=金 / snow=墨青 / berry=珊瑚） -->
      <path d="M 12.5 52.5 Q 30 59 47.5 52.5 L 47.5 56.5 Q 30 63 12.5 56.5 Z" fill="var(--logo-scarf)" />
      <path d="M 43.5 55.5 L 52 60.5 L 48 62.5 L 41.5 58 Z" fill="var(--logo-scarf-strong)" />
    </svg>

    <span v-if="variant === 'full'" class="rlogo-word">
      <span class="rlogo-name" :style="{ fontSize: nameSize + 'px' }">浣熊</span>
      <span class="rlogo-en" :style="{ fontSize: Math.max(10, Math.round(props.size * 0.3)) + 'px' }">Raccoon</span>
      <span v-if="tagline" class="rlogo-tagline">{{ tagline }}</span>
    </span>
  </span>
</template>

<style scoped>
/* 徽标配色全部走 --logo-* 换皮变量（docs/07 §2 四主题）：
   颜色由 tokens.css 按主题定义，组件本身不猜环境、不设本地默认——
   换主题时这只浣熊整体换皮（毛色/眼罩/围巾/耳色）。 */
.rlogo {
  display: inline-flex;
  align-items: center;
  gap: 10px;
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
