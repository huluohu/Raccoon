<script setup lang="ts">
// PageHeader：指挥中心式页头（docs/07 §2「页头签名」）。
//
// 十个一级视图的统一入口：等宽眉题 + 句点大标题 + 副行 + 右侧动作区。
// 为什么归属视图而不是顶栏：眉题 + 大标题是页面构图的一部分（控制台方向裁决），
// 顶栏只承载全局动作（提交框 / 时钟 / 通知）——两层职责不再混。
//
// 副行支持默认文本或 #sub 插槽（要放实时状态点、计数等富内容时用插槽）。
//
// sticky：页头吸顶（设置页用）。
// 为什么需要：设置页有几十个配置项，改完要滚很久才找得到「保存改动」，
// 按钮跟着页头一起滚走等于让用户改一屏就找一次保存。
// 实现上整块页头吸顶（而不是只让动作区吸顶）——动作区是页头的子元素，
// 单独 sticky 会被页头自身的高度框住，滚不出几像素就跟着走了。
withDefaults(
  defineProps<{
    /** 等宽眉题：大写英文短词（如 TASKS / LIVE OPERATIONS），前置短横线由组件给。 */
    eyebrow: string
    /** 页面大标题（中文）。句点由组件补在末尾（强调色），调用方不带。 */
    title: string
    /** 副行文本（简单场景）；富内容用 #sub 插槽。 */
    sub?: string
    /** 页头吸顶（长页面 + 需要常驻动作区时启用）。 */
    sticky?: boolean
  }>(),
  { sub: '', sticky: false },
)
</script>

<template>
  <header class="phead" :class="{ 'phead--sticky': sticky }">
    <div class="phead-main">
      <p class="phead-eyebrow eyebrow">
        <span class="phead-dash" aria-hidden="true" />{{ eyebrow }}
      </p>
      <h1 class="phead-title">{{ title }}<span class="phead-dot" aria-hidden="true">.</span></h1>
      <p v-if="sub || $slots.sub" class="phead-sub">
        <slot name="sub">{{ sub }}</slot>
      </p>
    </div>
    <div v-if="$slots.actions" class="phead-actions">
      <slot name="actions" />
    </div>
  </header>
</template>

<style scoped>
.phead {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px;
  margin: 2px 0 20px;
}
.phead-eyebrow { display: flex; align-items: center; margin-bottom: 8px; }
/* 眉题前置短横线：强调色，是「这一屏开始了」的信号灯。 */
.phead-dash {
  width: 18px;
  height: 2px;
  border-radius: 1px;
  background: var(--accent);
  margin-right: 10px;
  flex-shrink: 0;
}
.phead-title {
  font-size: var(--font-display);
  font-weight: var(--weight-display);
  line-height: var(--line-height-display);
  color: var(--text-1);
  letter-spacing: 0.01em;
}
/* 句点：强调色收尾——页头的签名笔画，克制的一处品牌落点。 */
.phead-dot { color: var(--accent); margin-left: 2px; }
.phead-sub {
  margin-top: 6px;
  font-size: var(--font-body);
  color: var(--text-2);
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  flex-wrap: wrap;
}
.phead-actions { display: flex; align-items: center; gap: 10px; flex-shrink: 0; }

/* 吸顶页头：整块吸住，动作区（「保存改动」）因此常驻可见。
   间距用 padding-bottom 而不是 margin-bottom——margin 在框外，
   内容会从缝里透出来；padding 在框内，背景能盖住。 */
.phead--sticky {
  position: sticky;
  top: 0;
  z-index: 5;
  background: var(--bg);
  margin-bottom: 0;
  padding-bottom: 20px;
}

@media (max-width: 767px) {
  .phead { margin: 0 0 16px; }
  .phead-actions { width: 100%; }
  .phead { flex-wrap: wrap; }
  .phead--sticky { margin-bottom: 0; padding-bottom: 16px; }
}
</style>
