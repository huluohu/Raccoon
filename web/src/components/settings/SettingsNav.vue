<script setup lang="ts">
// SettingsNav：设置页左侧分组导航（方案 §3「/settings → SettingsNav（左分组）」）。
//
// 为什么抽成组件：
// scrollspy 的判定逻辑（判定线 / 底部兜底 / 点击锁定）是一套有状态机的东西，
// 和「渲染一列锚点链接」是两件事。放在一起时，改导航样式很容易顺手改坏判定线，
// 改判定线又很容易碰掉「当前分组高亮」。拆开后各自有独立用例。
//
// 为什么不用 IntersectionObserver：
// 「相交就高亮」在这页是错的——分组卡片高 200–400px，视口里可能同时
// 没有任何分组相交（正卡在两张卡之间的空隙），此时高亮会突然消失；
// 用零高度哨兵也一样有边界问题（向上滚时多个哨兵同时相交，取谁都不对）。
//
// 用经典 scrollspy：判定线固定在视口顶部往下 140px，
// 高亮「判定线上方最后一个分组」。这样：
//   - 两组之间的空隙 → 保持上一个分组（不会突然没高亮）
//   - 向下滚 → 下一个分组越过判定线时切换
//   - 向上滚 → 上一个分组越过判定线时切换，方向天然正确
// 性能：scroll 事件里只排一次 rAF，一帧只读一次布局，不写样式，不触发 layout thrash。
//
// 光有判定线还不够，两个边界必须额外兜住（否则高亮会「卡住」）：
//
//   ① 页面底部：最后一个分组的 docTop 永远在「最大 scrollY + 判定线」之下，
//      判定线永远越不过它 → 滚到底部时高亮会停在上一个分组。
//      兜底：已经滚到底（scrollY + innerHeight >= scrollHeight）→ 直接高亮最后一个。
//   ② 锚点点击被底部截断：点击「API 令牌」时目标 docTop=5564，
//      但页面最大只能滚到 scrollHeight - innerHeight，目标被顶到视口 235px 处，
//      判定线（140px）同样选不中它 → 高亮停在「通道测试」。
//      兜底：点击时锁定到被点项（lockedId），被点项一旦滚出视口就自动解锁。
//      用户主动滚动（滚轮/触摸/方向键）同样解锁，避免锁定僵死。
//
// 还有一条首帧坑：内容还没撑开时 documentElement.scrollHeight 是 0，
// 「innerHeight + scrollY >= scrollHeight - 2」恒成立 → 会误高亮到最后一个分组。
// 必须加 docH > innerHeight + 2 前置条件。
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

export interface SettingsSection {
  /** 分组锚点 id（同时是内容区的 DOM id）。 */
  id: string
  /** 导航项显示文案。 */
  label: string
}

const props = defineProps<{
  /** 全部分组（顺序 = 导航项顺序 = 文档顺序）。 */
  sections: SettingsSection[]
}>()

const emit = defineEmits<{
  /** 用户点击了某个分组（视图据此做锚点跳转与锁定）。 */
  select: [id: string]
}>()

const activeSection = ref('')
/** rAF 句柄：保证一帧内最多算一次。 */
let scrollRaf = 0
/** 判定线：视口顶部往下多少像素算「当前分组」。 */
const SPY_LINE = 140
/** 点击导航后的锁定目标：锚点被页面底部截断时判定线选不中被点项，用它兜住。 */
let lockedId: string | null = null

/** 算当前分组：判定线上方最后一个分组；页面到底时兜底到最后一个。 */
function updateActiveSection(): void {
  scrollRaf = 0
  const ids = props.sections.map((s) => s.id)
  if (!ids.length) return

  // 只认「已经在 DOM 里」的分组：分组列表是异步加载的，
  // 拿还没渲染的 id 当兜底目标会高亮到一个看不见的分组上。
  const present = ids.filter((id) => document.getElementById(id) !== null)
  if (!present.length) return

  // ① 点击锁定：被点项只要还在视口里，就以它为准（锚点被底部截断时判定线选不中它）
  if (lockedId && present.includes(lockedId)) {
    const el = document.getElementById(lockedId)!
    const r = el.getBoundingClientRect()
    if (r.top < window.innerHeight && r.bottom > 0) {
      activeSection.value = lockedId
      return
    }
    lockedId = null
  }

  // ② 页面底部兜底：最后一个分组永远越不过判定线，直接高亮最后一个
  //    注意要排除「内容还没撑开」的情况：首帧 scrollHeight 可能还是 0，
  //    此时 innerHeight + 0 >= 0 - 2 恒成立 → 会误高亮到最后一个分组。
  const docH = document.documentElement.scrollHeight
  const atBottom =
    docH > window.innerHeight + 2 && window.innerHeight + window.scrollY >= docH - 2
  if (atBottom) {
    activeSection.value = present[present.length - 1]
    return
  }

  // ③ 判定线：取「判定线上方最后一个分组」
  let current = present[0]
  for (const id of present) {
    const el = document.getElementById(id)!
    if (el.getBoundingClientRect().top <= SPY_LINE) {
      current = id
    } else {
      // 文档顺序，越过判定线的第一个就是「线上方最后一个」；
      // 后面的分组只会更靠下，不用再算。
      break
    }
  }
  if (current) activeSection.value = current
}

/** scroll 回调：只排 rAF，不直接算。 */
function onScroll(): void {
  if (scrollRaf) return
  scrollRaf = window.requestAnimationFrame(updateActiveSection)
}

/** 用户主动滚动（滚轮/触摸/方向键）→ 解除点击锁定，让 scrollspy 重新接管。 */
function releaseLock(): void {
  if (!lockedId) return
  lockedId = null
  updateActiveSection()
}

function onKeydown(e: KeyboardEvent): void {
  if (['ArrowDown', 'ArrowUp', 'PageDown', 'PageUp', 'Home', 'End', ' '].includes(e.key)) {
    releaseLock()
  }
}

/** 点击导航项：锁定到被点项，并让浏览器执行锚点跳转。 */
function pickSection(id: string): void {
  lockedId = id
  activeSection.value = id
  emit('select', id)
}

watch(
  () => props.sections,
  () => nextTick(updateActiveSection),
  { immediate: true, deep: true },
)

onMounted(() => {
  window.addEventListener('scroll', onScroll, { passive: true })
  window.addEventListener('resize', onScroll)
  window.addEventListener('wheel', releaseLock, { passive: true })
  window.addEventListener('touchmove', releaseLock, { passive: true })
  window.addEventListener('keydown', onKeydown)
  updateActiveSection()
})

onBeforeUnmount(() => {
  window.removeEventListener('scroll', onScroll)
  window.removeEventListener('resize', onScroll)
  window.removeEventListener('wheel', releaseLock)
  window.removeEventListener('touchmove', releaseLock)
  window.removeEventListener('keydown', onKeydown)
  if (scrollRaf) window.cancelAnimationFrame(scrollRaf)
})
</script>

<template>
  <nav class="side-nav" aria-label="设置分组">
    <a
      v-for="s in sections"
      :key="s.id"
      :href="'#' + s.id"
      class="side-link"
      :class="{ on: activeSection === s.id }"
      @click="pickSection(s.id)"
    >{{ s.label }}</a>
  </nav>
</template>

<style scoped>
/* 左分组导航：方案 §3「SettingsNav（左分组）」。列宽 180px，
   条目字号 16px——原来 150px/13.5px，条目又窄又小，一眼看不出是导航。 */
.side-nav {
  position: sticky; top: 16px; display: flex; flex-direction: column; gap: 2px;
  max-height: calc(100vh - 32px); overflow-y: auto;
}
.side-link {
  padding: 8px 12px; border-radius: var(--radius-ctrl); color: var(--text-2);
  text-decoration: none; font-size: var(--font-body); line-height: 1.4;
  border-left: 3px solid transparent; transition: background 120ms ease-out, color 120ms ease-out;
}
.side-link:hover { background: var(--surface-2); color: var(--text-1); }
.side-link.on { background: var(--accent-soft); color: var(--accent); font-weight: var(--weight-semibold); border-left-color: var(--accent); }
@media (max-width: 1024px) {
  .side-nav { position: static; flex-direction: row; flex-wrap: wrap; }
}
</style>
