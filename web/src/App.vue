<script setup lang="ts">
// 应用外壳：左侧导航（七个一级页面，方案 §8）+ 顶栏快速提交 + 通知中心。设计 Token 见 styles/tokens.css。
// 会话 gate：未初始化 → setup；未登录 → Login；就绪才渲染主界面（后端 428/401 语义的前端落点）。
//
// 响应式四档（docs/07 §6）：
//   ≥1280        侧导航常驻 + 双栏（默认）
//   1024–1279    侧导航收图标栏（仅图标，文字隐藏）
//   768–1023     抽屉+表格降级卡片（各视图自行处理）
//   <768         单栏+底部主操作栏，触控目标 ≥44px，内容留出底部安全区
//
// 视觉方向（docs/07 §2「结构色：树影蓝灰」的落地）：
//   侧导航是结构色的大面积载体——深树影蓝灰底撑起品牌骨架，内容区留白。
//   浅色模式 nav=#1B2430，深色模式 nav=#0B0E14（比内容区更暗，层次不翻转为浅）。
//   围巾金只落在最高频动作上：顶栏提交按钮 + 当前导航项，全屏 ≤3 处。
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useGlobalShortcuts } from '@/composables/useShortcuts'
import { useUiStore } from '@/stores/ui'
import { useAuthStore } from '@/stores/auth'
import { useTasksStore } from '@/stores/tasks'
import { useNotificationsStore } from '@/stores/notifications'
import Login from '@/views/Login.vue'
import Setup from '@/views/Setup.vue'
import NotifCenter from '@/components/shell/NotifCenter.vue'
import QuickSubmit from '@/components/shell/QuickSubmit.vue'
import RToast from '@/components/base/RToast.vue'
import RaccoonLogo from '@/components/brand/RaccoonLogo.vue'

const ui = useUiStore()
const auth = useAuthStore()
const tasks = useTasksStore()
const notif = useNotificationsStore()
const router = useRouter()
const route = useRoute()

// 全站快捷键（docs/07 §7）：`/` 聚焦提交框、G D / G S 跳转。
// 页面级行导航（↑↓ / Enter / R）由各页面 useShortcuts 注册，这里只保证
// 「任何页面都至少能用全局键」——否则快捷键只在下载页有效。
useGlobalShortcuts()

const quickUrl = ref('')
const quickErr = ref('')
/** 768–1023 抽屉态：侧导航收进抽屉时的开合。 */
const navOpen = ref(false)
/** <768 底部栏「更多」面板的开合（10 项塞不进 375px，5 高频 + 更多）。 */
const moreOpen = ref(false)

/**
 * 一级导航（docs/15 §1.2 裁决 5）：10 项 = 9 个复刻页 + Raccoon 自有「连接」。
 * `short` 是底部栏用的两字短标签（「媒体库」在 375px 下放不进 52px 的槽位）。
 * `tab` 标记底部栏 5 个高频入口（docs/15 §1.2 裁决 6：主页/任务/历史/文件/设置），
 * 其余 5 项收进「更多」面板。
 * 图标用线稿 SVG（docs/07 §4 禁 emoji 冒充图标）。
 */
const NAV_ITEMS = [
  { to: '/', label: '总览', short: '主页', tab: true, icon: 'M2.6 7.3 8 2.9l5.4 4.4v5.4a.9.9 0 0 1-.9.9H3.5a.9.9 0 0 1-.9-.9z M6.2 13.6V9.4h3.6v4.2' },
  { to: '/tasks', label: '下载', short: '任务', tab: true, icon: 'M8 2.2v7.2m0 0 2.8-2.8M8 9.4 5.2 6.6M2.6 12.6h10.8' },
  { to: '/history', label: '历史', short: '历史', tab: true, icon: 'M8 3a5 5 0 1 0 0 10A5 5 0 0 0 8 3zM8 5.5V8l2 1.4' },
  { to: '/media', label: '媒体库', short: '文件', tab: true, icon: 'M2.4 4.4h3.9l1.2 1.6h6.1v6.9a1 1 0 0 1-1 1H3.4a1 1 0 0 1-1-1z M6.6 9.9l1.5 1.7 1.1-1.2 1.6 1.8' },
  { to: '/subscriptions', label: '订阅', short: '订阅', tab: false, icon: 'M3.2 6.6a5.4 5.4 0 0 1 5.4 5.4M3.2 3.4a8.6 8.6 0 0 1 8.6 8.6M4.4 12a.9.9 0 1 0 0-.1' },
  { to: '/live', label: '直播', short: '直播', tab: false, icon: 'M8 9.1a1.5 1.5 0 1 0 0-3 1.5 1.5 0 0 0 0 3zM5.1 5.1a4.6 4.6 0 0 0 0 5.8m5.8 0a4.6 4.6 0 0 0 0-5.8M2.7 2.7a7.8 7.8 0 0 0 0 10.6m10.6 0a7.8 7.8 0 0 0 0-10.6' },
  { to: '/logs', label: '日志', short: '日志', tab: false, icon: 'M3.4 3.6h9.2M3.4 6.6h9.2M3.4 9.6h5.6M3.4 12.4h3.2' },
  { to: '/monitor', label: '监控', short: '监控', tab: false, icon: 'M2.2 8h2.3l1.5-3.8 2.5 7.6 1.5-3.8h3.8' },
  { to: '/connections', label: '连接', short: '连接', tab: false, icon: 'M6.4 9.6 9.6 6.4M5.4 7.2 3.9 8.7a2.4 2.4 0 0 0 3.4 3.4l1.5-1.5M10.6 8.8l1.5-1.5a2.4 2.4 0 0 0-3.4-3.4L7.2 5.4' },
  { to: '/settings', label: '设置', short: '设置', tab: true, icon: 'M8 5.6a2.4 2.4 0 1 0 0 4.8 2.4 2.4 0 0 0 0-4.8zM13 8c0-.6-.1-1.1-.2-1.6l1.3-1-1.3-2.2-1.5.6c-.5-.4-1.1-.8-1.7-1l-.2-1.6h-2.6l-.2 1.6c-.6.2-1.2.6-1.7 1l-1.5-.6-1.3 2.2 1.3 1c-.1.5-.2 1-.2 1.6s.1 1.1.2 1.6l-1.3 1 1.3 2.2 1.5-.6c.5.4 1.1.8 1.7 1l.2 1.6h2.6l.2-1.6c.6-.2 1.2-.6 1.7-1l1.5.6 1.3-2.2-1.3-1c.1-.5.2-1 .2-1.6z' },
]

/** 底部栏 5 个高频入口。 */
const TAB_ITEMS = computed(() => NAV_ITEMS.filter((i) => i.tab))
/** 「更多」面板里的其余入口。 */
const MORE_ITEMS = computed(() => NAV_ITEMS.filter((i) => !i.tab))

/**
 * 顶栏页面标题：路由 meta.title 是唯一来源。
 * 原来每个视图自己在内容区顶一个 <h1> + 金色左边条（Word 文档风），
 * 七页七种顶部留白；标题归属统一到顶栏后，「你在哪」由导航高亮 + 顶栏标题
 * 双重锚定，内容区直接进入数据——这是布局比例重构的核心一步。
 */
const pageTitle = computed(() => (route.meta.title as string) ?? '')

/** 768–1023 抽屉态 / <768「更多」面板：Esc 关闭（焦点即使跑出面板也能兜住）。 */
function onNavKey(ev: KeyboardEvent) {
  if (ev.key !== 'Escape') return
  if (moreOpen.value) moreOpen.value = false
  if (navOpen.value) navOpen.value = false
}

// 路由变化必须收起「更多」面板：面板是覆盖层，跳转后还挂着会挡住新页面内容。
watch(
  () => route.fullPath,
  () => {
    moreOpen.value = false
    navOpen.value = false
  },
)

async function quickSubmit(target: string) {
  if (!target) return
  quickErr.value = ''
  try {
    await tasks.submit(target)
    quickUrl.value = ''
  } catch (e) {
    quickErr.value = e instanceof Error ? e.message : '提交失败'
  }
}

/**
 * QuickSubmit 组件抛来的提交意图。
 * 组件已经做过格式校验 + 分享文案提取（value 是规范化后的纯链接），
 * 直接用它提交，不要再读 quickUrl.value——那是原始输入，可能带着一整段分享文案。
 */
async function onQuickSubmit(value: string) {
  await quickSubmit(value)
}

// 会话进入 ready 是「开始拉数据 + 建 SSE」的唯一起点。
// 不能只在 onMounted 里判断一次：首屏若落在登录页，bootstrap() 返回 login，
// 用户手动登录后 phase 才变 ready，那时的 onMounted 早已跑完，SSE 永远连不上。
watch(
  () => auth.phase,
  async (phase, prev) => {
    if (phase !== 'ready') {
      if (prev === 'ready') {
        tasks.disconnect()
        notif.stop()
      }
      return
    }
    // 带着有效 cookie 直接开在 /login、/setup：bootstrap 结束后会话 gate 会放行外壳，
    // 而 router-view 还会把登录页渲染一遍（同屏重复）。这里一并跳回总览。
    const name = router.currentRoute.value.name
    if (name === 'login' || name === 'setup') router.replace('/')
    await tasks.refresh()
    tasks.connect()
    notif.start()
  },
)

onMounted(async () => {
  // 抽屉态 Esc 兜底：遮罩可点，但键盘用户也要有一条零成本退出路径（§6/§7 一致原则）。
  document.addEventListener('keydown', onNavKey)
  await auth.bootstrap()
  // bootstrap 可能已经把 phase 置为 ready（cookie 仍有效），watch 不会为「同值」触发，这里补一次。
  if (auth.phase === 'ready') {
    await tasks.refresh()
    tasks.connect()
    notif.start()
  }
})

onUnmounted(() => {
  document.removeEventListener('keydown', onNavKey)
  notif.stop()
})
</script>

<template>
  <div class="app" :data-theme="ui.theme">
    <!-- 会话 gate（后端 428/401 语义的前端落点）：
         未初始化 → 初始化向导（方案 §3 /setup「向导表单」）；
         未登录 → 登录页；就绪才渲染主界面。 -->
    <Setup v-if="auth.phase === 'setup'" />
    <Login v-else-if="auth.phase !== 'ready'" />
    <template v-else>
      <!-- 768–1023：侧导航收进抽屉，这里只放触发按钮（≥1024 隐藏） -->
      <button
        class="nav-toggle"
        type="button"
        aria-label="打开导航"
        :aria-expanded="navOpen ? 'true' : 'false'"
        @click="navOpen = true"
      >
        <svg viewBox="0 0 16 16" width="18" height="18" aria-hidden="true">
          <path d="M2.4 4.2h11.2M2.4 8h11.2M2.4 11.8h11.2" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
        </svg>
      </button>

      <aside class="nav" :class="{ open: navOpen }">
        <!-- 品牌区：RaccoonLogo 静态徽标（不是会变表情的 RaccoonFace——表情是状态角色，
             徽标是识别符号）。深底上 --logo-fur 提亮一档，否则头会糊进导航底色。 -->
        <div class="brand">
          <RaccoonLogo :size="34" />
        </div>
        <nav @click="navOpen = false">
          <router-link v-for="item in NAV_ITEMS" :key="item.to" :to="item.to" :title="item.label">
            <svg class="nav-icon" viewBox="0 0 16 16" width="17" height="17" aria-hidden="true">
              <path :d="item.icon" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
            <span class="nav-link-text">{{ item.label }}</span>
          </router-link>
        </nav>
        <div class="nav-foot">
          <!-- 用户名 + 连接状态一行：用户区自带分隔线，一眼是「账户区」而不是导航列表的尾巴。 -->
          <div class="user-row">
            <span class="user" :title="auth.user?.username">{{ auth.user?.username }}</span>
            <span class="conn" :class="tasks.connected ? 'on' : 'off'" :title="tasks.connected ? '实时连接正常' : '实时连接断开，正在重连'">
              {{ tasks.connected ? '实时' : '重连中' }}
            </span>
          </div>
          <button class="foot-btn" type="button" @click="ui.toggleTheme">
            <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
              <path v-if="ui.theme === 'dark'" d="M13 9.8A5.6 5.6 0 0 1 6.2 3a5.6 5.6 0 1 0 6.8 6.8z" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linejoin="round" />
              <path v-else d="M8 3.2v1.6m0 6.4v1.6M3.2 8h1.6m6.4 0h1.6M4.8 4.8l1.1 1.1m4.2 4.2 1.1 1.1m0-6.4-1.1 1.1M5.9 10.1l-1.1 1.1M8 5.6a2.4 2.4 0 1 0 0 4.8 2.4 2.4 0 0 0 0-4.8z" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" />
            </svg>
            <span class="btn-text">{{ ui.theme === 'dark' ? '浅色模式' : '深色模式' }}</span>
          </button>
          <button class="foot-btn" type="button" @click="auth.logout()">
            <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
              <path d="M6.4 3.6H3.8a1 1 0 0 0-1 1v6.8a1 1 0 0 0 1 1h2.6M10.4 5.6 13 8l-2.6 2.4M13 8H6.2" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
            <span class="btn-text">退出登录</span>
          </button>
        </div>
      </aside>
      <!-- 抽屉态遮罩：点它即关，Esc 也关（键盘兜底见下方 onKeydown） -->
      <div v-if="navOpen" class="nav-mask" @click="navOpen = false" />

      <div class="main">
        <header class="topbar">
          <!-- 页面标题：路由 meta.title，全站唯一的「你在哪」文字锚点 -->
          <h1 class="topbar-title">{{ pageTitle }}</h1>
          <QuickSubmit
            v-model="quickUrl"
            :error="quickErr"
            @submit="onQuickSubmit"
          />
          <div class="topbar-right">
            <span v-if="tasks.warning" class="topbar-warn" :title="tasks.warning">{{ tasks.warning }}</span>
            <NotifCenter />
          </div>
        </header>
        <main class="content">
          <!-- 内容宽度约束：超宽屏下不把为 ~1200px 设计的版面硬拉满。
               1440 是阅读/表格的舒适上限，居中后两侧留白对称，布局比例不随窗口漂移。 -->
          <div class="content-inner">
            <router-view v-slot="{ Component }">
              <component :is="Component" />
            </router-view>
          </div>
        </main>
      </div>

      <!-- <768：底部主操作栏（触控目标 ≥44px；内容区已留出底部安全区，关键按钮不被挡）
           10 项导航在 375px 下每项只有 37px，放不下 → 5 高频 + 「更多」面板
           （docs/15 §1.2 裁决 6；方案 a 不引入横向滚动的隐藏可及性问题）。 -->
      <nav class="tabbar" aria-label="主导航">
        <router-link v-for="item in TAB_ITEMS" :key="item.to" :to="item.to" :title="item.label">
          <svg viewBox="0 0 16 16" width="20" height="20" aria-hidden="true">
            <path :d="item.icon" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
          <span class="tabbar-label">{{ item.short }}</span>
        </router-link>
        <button
          class="tabbar-more"
          type="button"
          aria-controls="tabbar-more-sheet"
          :aria-expanded="moreOpen ? 'true' : 'false'"
          @click="moreOpen = !moreOpen"
        >
          <svg viewBox="0 0 16 16" width="20" height="20" aria-hidden="true">
            <circle cx="3.6" cy="8" r="1.2" fill="currentColor" />
            <circle cx="8" cy="8" r="1.2" fill="currentColor" />
            <circle cx="12.4" cy="8" r="1.2" fill="currentColor" />
          </svg>
          <span class="tabbar-label">更多</span>
        </button>
      </nav>

      <!-- 「更多」面板：点遮罩、点任一项、Esc 都关（零成本退出路径） -->
      <div v-if="moreOpen" class="sheet-mask" @click="moreOpen = false" />
      <div v-if="moreOpen" id="tabbar-more-sheet" class="more-sheet" role="dialog" aria-label="更多导航">
        <router-link
          v-for="item in MORE_ITEMS"
          :key="item.to"
          :to="item.to"
          class="more-item"
          @click="moreOpen = false"
        >
          <svg viewBox="0 0 16 16" width="22" height="22" aria-hidden="true">
            <path :d="item.icon" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
          <span>{{ item.label }}</span>
        </router-link>
      </div>
    </template>
    <RToast />
  </div>
</template>

<style scoped>
.app { display: flex; min-height: 100vh; background: var(--bg); color: var(--text-1); }

/* ============ 侧导航：结构色大面积载体（树影蓝灰深底） ============ */
.nav {
  width: 240px;
  flex-shrink: 0;
  padding: 20px 14px 16px;
  background: var(--nav-bg);
  border-right: 1px solid var(--nav-border);
  display: flex;
  flex-direction: column;
  gap: 18px;
}
.brand { display: flex; align-items: center; padding: 0 6px; }
/* 深底上的徽标：毛色提亮一档（浅底用的是 --accent 深蓝灰，深底上会糊），
   面纹保持纸白、眼罩改近黑、字标反白。 */
.brand .rlogo {
  --logo-fur: var(--logo-fur-lift);
  --logo-face: var(--logo-face-lift);
  --logo-mask: var(--logo-mask-lift);
  --logo-name: var(--nav-text-strong);
  --logo-sub: var(--nav-text);
}
.nav nav { display: flex; flex-direction: column; gap: 2px; }
/* 导航条目：正文档 16px。导航是主操作路径，7 个入口都用 aux(13.5px) 会让
   「当前在哪、点哪去」这条主路径看起来比次要说明还轻。 */
.nav nav a {
  position: relative;
  padding: 10px 12px;
  border-radius: var(--radius-ctrl);
  color: var(--nav-text);
  text-decoration: none;
  font-size: var(--font-body);
  transition: background 120ms ease-out, color 120ms ease-out;
}
.nav nav a:hover { background: var(--nav-hover); color: var(--nav-text-strong); }
/* 当前项：左侧金条 + 金色文字 + 淡金底——围巾金落在「当前位置」上，
   和顶栏提交按钮两处，全屏金色 ≤3 处的额度刚好用完。 */
.nav nav a.router-link-active {
  background: var(--nav-active-bg);
  color: var(--nav-active-text);
  font-weight: var(--weight-semibold);
}
.nav nav a.router-link-active::before {
  content: '';
  position: absolute;
  left: 0;
  top: 50%;
  transform: translateY(-50%);
  width: 3px;
  height: 18px;
  border-radius: 2px;
  background: var(--nav-active-text);
}
/* 用户区与导航列表之间必须有分隔：否则 admin / 实时 / 深色模式 / 退出登录
   会和上面 15 个导航项连成一条无尽列表，观感就是「这页面没做完」。 */
.nav-foot { margin-top: auto; display: flex; flex-direction: column; gap: 8px; border-top: 1px solid var(--nav-border); padding-top: 14px; }
.user-row { display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 0 12px; min-width: 0; }
/* 用户名是「当前身份」，正文档 + 反白，不当辅助信息。 */
.user { font-size: var(--font-body); font-weight: var(--weight-strong); color: var(--nav-text-strong); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
/* 实时连接状态：辅助档 13.5px（§2「12px 仅表头/徽章」，这是状态提示不是徽章）。 */
.conn {
  font-size: var(--font-aux);
  flex-shrink: 0;
  color: var(--nav-text);
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.conn::before { content: ''; width: 6px; height: 6px; border-radius: 50%; background: var(--neutral); }
.conn.on::before { background: var(--success); }
.conn.off::before { background: var(--warning); }
/* 深底上的次级按钮：半透明描边 + 浅字，hover 出浅色蒙层。 */
.foot-btn {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 9px 12px;
  border: 1px solid var(--nav-btn-border);
  border-radius: var(--radius-ctrl);
  background: transparent;
  color: var(--nav-text);
  cursor: pointer;
  font-size: var(--font-body);
  text-align: left;
  transition: background 120ms ease-out, color 120ms ease-out;
}
.foot-btn:hover { background: var(--nav-hover); color: var(--nav-text-strong); }
.foot-btn:last-child:hover { color: var(--danger); }

.main { flex: 1; display: flex; flex-direction: column; min-width: 0; }
.topbar { padding: 0 24px; height: 64px; flex-shrink: 0; background: var(--surface); border-bottom: 1px solid var(--border); display: flex; align-items: center; gap: 20px; }
/* 顶栏标题：title-1 24px/750。内容区各视图不再自己顶标题——
   标题归属统一在这里，七页共用一条稳定的「你在哪」锚线。 */
.topbar-title { font-size: var(--font-title-1); font-weight: var(--weight-title); color: var(--text-1); white-space: nowrap; }
.topbar-right { margin-left: auto; display: flex; align-items: center; gap: 10px; }
/* 顶栏告警条：辅助档 13.5px（这是要读的一句话，不是表头）。 */
.topbar-warn {
  max-width: 280px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--font-aux);
  color: var(--warning);
}
.content { padding: 24px; flex: 1; min-width: 0; }
/* 超宽屏内容区上限：桌面阅读与六列表格的舒适宽度，居中留白对称。 */
.content-inner { width: 100%; max-width: 1440px; margin: 0 auto; }

/* ============ §6 响应式四档 ============ */

/* 默认隐藏态必须写在媒体查询【之前】。
   CSS 同特异性下后写的赢：这两条若放在媒体查询后面，会把媒体查询里的
   display:inline-flex / display:flex 覆盖掉，导致
   768–1023 档汉堡按钮不显示（导航彻底打不开）、<768 档底部主操作栏不渲染。
   实测踩过：四档截图里 .nav-toggle / .tabbar 全是 display:none。 */
.nav-toggle {
  display: none;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  flex-shrink: 0;
  border: 1px solid var(--border);
  border-radius: var(--radius-ctrl);
  background: var(--surface);
  color: var(--text-2);
  cursor: pointer;
}

.tabbar { display: none; }
/* 「更多」面板与它的遮罩默认隐藏，只有 <768 才启用。
   默认隐藏态写在这里（媒体查询之前）——同特异性下后写的赢，
   写在后面会把媒体查询里的 display 覆盖掉（docs/07 §8.1 ⑤ 的教训）。 */
.sheet-mask,
.more-sheet { display: none; }

/* ≥1280：侧导航常驻 + 双栏 —— 即上方默认样式，无需覆盖 */

/* 1024–1279：侧导航收图标栏（图标居中，文字与尾部文字按钮隐藏） */
@media (max-width: 1279px) and (min-width: 1024px) {
  .nav { width: 64px; padding: 20px 8px 16px; align-items: center; }
  .brand { justify-content: center; padding: 0; }
  .nav nav { width: 100%; }
  .nav nav a { justify-content: center; padding: 11px 0; }
  /* 图标栏态的金条改成圆点：竖条在 64px 窄栏里会戳出边界 */
  .nav nav a.router-link-active::before { width: 18px; height: 3px; top: auto; bottom: 3px; transform: none; }
  .nav-link-text { display: none; }
  .nav-foot { align-items: center; }
  .nav-foot .user,
  .nav-foot .conn { display: none; }
  .nav-foot .btn-text { display: none; }
  .foot-btn { padding: 10px; justify-content: center; }
}

/* 768–1023：侧导航收进抽屉 + 顶栏汉堡；表格降级卡片由各视图自行处理 */
@media (max-width: 1023px) and (min-width: 768px) {
  .nav-toggle { display: inline-flex; }
  .nav {
    position: fixed;
    inset: 0 auto 0 0;
    z-index: 60;
    width: 248px;
    transform: translateX(-100%);
    transition: transform var(--motion-panel) var(--motion-panel-ease);
    box-shadow: var(--shadow-3);
  }
  .nav.open { transform: translateX(0); }
  .nav-mask { position: fixed; inset: 0; z-index: 50; background: var(--scrim); }
  .topbar { padding: 0 12px; gap: 12px; }
}

/* <768：单栏 + 底部主操作栏；触控目标 ≥44px；内容留出底部安全区（375px 关键按钮不被挡） */
@media (max-width: 767px) {
  .nav,
  .nav-toggle,
  .nav-mask { display: none; }
  /* 顶栏标题降档：375px 下 24px 标题 + 输入框 + 铃铛同行会挤爆，
     标题让位给提交框（本页最高频动作），页面归属由底部 tabbar 高亮承担。 */
  .topbar { padding: 0 12px; gap: 10px; height: 56px; }
  .topbar-title { display: none; }
  .tabbar {
    position: fixed;
    inset: auto 0 0 0;
    z-index: 60;
    display: flex;
    justify-content: space-around;
    align-items: stretch;
    /* 底部安全区：iPhone Home 指示条 / 手势区不遮按钮 */
    padding: 4px 4px calc(4px + env(safe-area-inset-bottom, 0px));
    background: var(--surface);
    border-top: 1px solid var(--border);
  }
  .tabbar a {
    flex: 1;
    min-width: 0;
    min-height: 44px; /* §6 触控目标 */
    display: inline-flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 2px;
    color: var(--text-2);
    text-decoration: none;
    border-radius: var(--radius-ctrl);
  }
  /* 底部栏文字：纯图标在移动端认不出是哪个入口，7 项挤 375px 放得下 2 字标签 */
  .tabbar-label {
    font-size: var(--font-caption); /* 12px：底部栏标签是紧凑的图标注解 */
    line-height: 1;
    color: inherit;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 100%;
  }
  .tabbar a.router-link-active { color: var(--accent); background: var(--accent-soft); }
  /* 「更多」按钮与普通 tab 同构：同样的 flex 槽位、同样的 44px 触控目标。
     它是 button 不是 a，所以选中态由 aria-expanded 驱动（面板开着时点亮）。 */
  .tabbar-more {
    flex: 1;
    min-width: 0;
    min-height: 44px;
    display: inline-flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 2px;
    border: none;
    background: none;
    padding: 0;
    color: var(--text-2);
    border-radius: var(--radius-ctrl);
    cursor: pointer;
    font-family: inherit;
  }
  .tabbar-more[aria-expanded='true'] { color: var(--accent); background: var(--accent-soft); }

  /* 「更多」面板：贴着底部栏上沿弹出，遮罩挡住内容区（避免误点下面的列表）。 */
  .sheet-mask { display: block; position: fixed; inset: 0; z-index: 70; background: var(--scrim); }
  .more-sheet {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 8px;
    position: fixed;
    left: 0;
    right: 0;
    bottom: calc(52px + env(safe-area-inset-bottom, 0px));
    z-index: 71;
    padding: 12px 12px 16px;
    background: var(--surface);
    border-top: 1px solid var(--border);
    border-radius: var(--radius-card) var(--radius-card) 0 0;
    box-shadow: var(--shadow-3);
  }
  .more-item {
    min-height: 56px;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 4px;
    padding: 6px 4px;
    color: var(--text-2);
    text-decoration: none;
    /* 面板项是「要点的入口」而不是注解，用辅助档 13.5px，不用 12px 徽章档 */
    font-size: var(--font-aux);
    border-radius: var(--radius-ctrl);
  }
  .more-item.router-link-active { color: var(--accent); background: var(--accent-soft); }
  /* 内容底部留出 tabbar 高度 + 间距，长页面最后一个按钮不会被挡住 */
  .content { padding: 16px 14px calc(16px + 52px + env(safe-area-inset-bottom, 0px)); }
}

.nav-icon { flex-shrink: 0; }

</style>
