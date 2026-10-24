<script setup lang="ts">
// 应用外壳：「浣熊夜勤」控制台（docs/07 §2）。
// 会话 gate：未初始化 → setup；未登录 → Login；就绪才渲染主界面（后端 428/401 语义的前端落点）。
//
// 响应式四档（docs/07 §6）：
//   ≥1280        侧导航常驻 + 双栏（默认）
//   1024–1279    侧导航收图标栏（仅图标，文字隐藏）
//   768–1023     抽屉+表格降级卡片（各视图自行处理）
//   <768         单栏+底部主操作栏，触控目标 ≥44px，内容留出底部安全区
//
// 视觉方向（docs/07 §2 四套浣熊主题）：
//   侧导航是结构色的大面积载体——深底控制台骨架，内容区留白；
//   页面标题归属视图层 PageHeader（眉题 + 大标题是页面构图的一部分），顶栏不重复。
//   强调色只落在：当前导航项 / 顶栏提交按钮 / 实时状态，全屏克制使用。
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useGlobalShortcuts } from '@/composables/useShortcuts'
import { useUiStore, type ThemeId } from '@/stores/ui'
import { useAuthStore } from '@/stores/auth'
import { useTasksStore } from '@/stores/tasks'
import { useNotificationsStore } from '@/stores/notifications'
import { useToastStore } from '@/stores/toast'
import Login from '@/views/Login.vue'
import Setup from '@/views/Setup.vue'
import NotifCenter from '@/components/shell/NotifCenter.vue'
import QuickSubmit from '@/components/shell/QuickSubmit.vue'
import AboutDialog from '@/components/shell/AboutDialog.vue'
import RToast from '@/components/base/RToast.vue'
import RaccoonLogo from '@/components/brand/RaccoonLogo.vue'
import { useNowTick } from '@/utils/now'

const ui = useUiStore()
const auth = useAuthStore()
const tasks = useTasksStore()
const notif = useNotificationsStore()
const toast = useToastStore()
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
const aboutRef = ref<InstanceType<typeof AboutDialog> | null>(null)
/** <768 底部栏「更多」面板的开合（10 项塞不进 375px，5 高频 + 更多）。 */
const moreOpen = ref(false)
/** 主题切换弹层的开合（侧栏底部）。 */
const themeOpen = ref(false)

/**
 * 一级导航（docs/15 §1.2 裁决 5）：10 项 = 9 个复刻页 + Raccoon 自有「连接」。
 * 分组双语（docs/07 §2 页头签名同源）：中文主标签 + 等宽英文副标签，
 * 按工作区/系统分两节——10 项平铺是一条无尽列表，分组后才像控制台。
 * `short` 是底部栏用的两字短标签；`tab` 标记底部栏 5 个高频入口（裁决 6）。
 * 图标用线稿 SVG（docs/07 §4 禁 emoji 冒充图标）。
 */
interface NavItem {
  to: string
  label: string
  en: string
  short: string
  tab: boolean
  icon: string
}

const NAV_ITEMS: NavItem[] = [
  { to: '/', label: '总览', en: 'OVERVIEW', short: '主页', tab: true, icon: 'M2.6 7.3 8 2.9l5.4 4.4v5.4a.9.9 0 0 1-.9.9H3.5a.9.9 0 0 1-.9-.9z M6.2 13.6V9.4h3.6v4.2' },
  { to: '/tasks', label: '下载', en: 'TASKS', short: '任务', tab: true, icon: 'M8 2.2v7.2m0 0 2.8-2.8M8 9.4 5.2 6.6M2.6 12.6h10.8' },
  { to: '/history', label: '历史', en: 'HISTORY', short: '历史', tab: true, icon: 'M8 3a5 5 0 1 0 0 10A5 5 0 0 0 8 3zM8 5.5V8l2 1.4' },
  { to: '/media', label: '媒体库', en: 'MEDIA', short: '文件', tab: true, icon: 'M2.4 4.4h3.9l1.2 1.6h6.1v6.9a1 1 0 0 1-1 1H3.4a1 1 0 0 1-1-1z M6.6 9.9l1.5 1.7 1.1-1.2 1.6 1.8' },
  { to: '/subscriptions', label: '订阅', en: 'SUBS', short: '订阅', tab: false, icon: 'M3.2 6.6a5.4 5.4 0 0 1 5.4 5.4M3.2 3.4a8.6 8.6 0 0 1 8.6 8.6M4.4 12a.9.9 0 1 0 0-.1' },
  { to: '/live', label: '直播', en: 'LIVE', short: '直播', tab: false, icon: 'M8 9.1a1.5 1.5 0 1 0 0-3 1.5 1.5 0 0 0 0 3zM5.1 5.1a4.6 4.6 0 0 0 0 5.8m5.8 0a4.6 4.6 0 0 0 0-5.8M2.7 2.7a7.8 7.8 0 0 0 0 10.6m10.6 0a7.8 7.8 0 0 0 0-10.6' },
  // 平台账号：内容源登录态（与「连接」的系统集成区分，2026-10-24 裁决）。
  { to: '/accounts', label: '平台账号', en: 'ACCOUNTS', short: '账号', tab: false, icon: 'M8 7.2a2.2 2.2 0 1 0 0-4.4 2.2 2.2 0 0 0 0 4.4zM3.4 13.4c.4-2.4 2.3-3.8 4.6-3.8s4.2 1.4 4.6 3.8M12.6 4.2v3.6M14.4 6h-3.6' },
  { to: '/connections', label: '连接', en: 'CHANNELS', short: '连接', tab: false, icon: 'M6.4 9.6 9.6 6.4M5.4 7.2 3.9 8.7a2.4 2.4 0 0 0 3.4 3.4l1.5-1.5M10.6 8.8l1.5-1.5a2.4 2.4 0 0 0-3.4-3.4L7.2 5.4' },
  { to: '/logs', label: '日志', en: 'LOGS', short: '日志', tab: false, icon: 'M3.4 3.6h9.2M3.4 6.6h9.2M3.4 9.6h5.6M3.4 12.4h3.2' },
  { to: '/monitor', label: '监控', en: 'MONITOR', short: '监控', tab: false, icon: 'M2.2 8h2.3l1.5-3.8 2.5 7.6 1.5-3.8h3.8' },
  { to: '/settings', label: '设置', en: 'SETTINGS', short: '设置', tab: true, icon: 'M8 5.6a2.4 2.4 0 1 0 0 4.8 2.4 2.4 0 0 0 0-4.8zM13 8c0-.6-.1-1.1-.2-1.6l1.3-1-1.3-2.2-1.5.6c-.5-.4-1.1-.8-1.7-1l-.2-1.6h-2.6l-.2 1.6c-.6.2-1.2.6-1.7 1l-1.5-.6-1.3 2.2 1.3 1c-.1.5-.2 1-.2 1.6s.1 1.1.2 1.6l-1.3 1 1.3 2.2 1.5-.6c.5.4 1.1.8 1.7 1l.2 1.6h2.6l.2-1.6c.6-.2 1.2-.6 1.7-1l1.5.6 1.3-2.2-1.3-1c.1-.5.2-1 .2-1.6z' },
]

/** 侧导航分组：工作区（下载域）+ 系统（运维域），小节标签用等宽英文。 */
const NAV_SECTIONS: { label: string; en: string; items: NavItem[] }[] = [
  // 工作区含平台账号（内容源账号服务下载域）；连接是系统集成，归系统组——
  // 恰好紧贴在平台账号之后（2026-10-24 裁决：连接排在平台账号后面）。
  { label: '工作区', en: 'WORKSPACE', items: NAV_ITEMS.slice(0, 7) },
  { label: '系统', en: 'SYSTEM', items: NAV_ITEMS.slice(7) },
]

/** 底部栏 5 个高频入口。 */
const TAB_ITEMS = computed(() => NAV_ITEMS.filter((i) => i.tab))
/** 「更多」面板里的其余入口。 */
const MORE_ITEMS = computed(() => NAV_ITEMS.filter((i) => !i.tab))

/** 当前主题元数据（切换器按钮与移动端面板显示用）。 */
const currentTheme = computed(() => ui.themes.find((t) => t.id === ui.theme) ?? ui.themes[0])

function pickTheme(id: ThemeId) {
  ui.setTheme(id)
  themeOpen.value = false
}

/** 顶栏等宽时钟：控制台的「系统活着」心跳（文档/05 诚实原则——只显示本地时间）。 */
const nowTick = useNowTick()
const clock = computed(() => {
  const d = new Date(nowTick.value)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
})

/**
 * 主题属性必须挂在 <html> 上，不能只挂在 .app 上。
 *
 * RDialog / RToast / DetailDrawer 都是 <Teleport to="body">，浮层 DOM 是 .app 的
 * 兄弟节点；`[data-theme=…]` 挂在 .app 内部就匹配不到它们 —— 深色主题下
 * 对话框 / 抽屉 / Toast 会整套退回 :root 的 night token，白卡片浮在深色界面上。
 *
 * 挂到根元素后顺带修掉 body 底色：base.css 的 `body { background: var(--bg) }`
 * 取的是 :root 变量，只有根元素带属性主题块才会生效，
 * 否则滚动到顶/底的橡皮筋区域会露出默认主题的底。
 */
watch(
  () => ui.theme,
  (theme) => {
    document.documentElement.setAttribute('data-theme', theme)
  },
  { immediate: true },
)

/** 抽屉态 /「更多」面板 / 主题弹层：Esc 关闭（焦点即使跑出面板也能兜住）。 */
function onNavKey(ev: KeyboardEvent) {
  if (ev.key !== 'Escape') return
  if (moreOpen.value) moreOpen.value = false
  if (navOpen.value) navOpen.value = false
  if (themeOpen.value) themeOpen.value = false
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
    // 单条与批量同规格的回执：顶栏提交后用户可能停留在任意页面，
    // 任务列表不一定可见，没有 toast 就像「按了没反应」。
    toast.success('已加入下载队列')
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

/** 批量提交（多链接粘贴）：走 store 的批量接口，回执汇总一次 toast。 */
async function onQuickSubmitBatch(urls: string[]) {
  quickErr.value = ''
  try {
    const res = await tasks.submitBatch(urls.map((url) => ({ url })))
    quickUrl.value = ''
    const okN = res.data.length
    const dupN = res.conflicts.length
    toast.success(dupN > 0 ? `批量提交：${okN} 个入队，${dupN} 个已存在跳过` : `批量提交：${okN} 个任务已入队`)
  } catch (e) {
    quickErr.value = e instanceof Error ? e.message : '批量提交失败'
  }
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
  // 主题属性是本组件写在 <html> 上的，卸载时一并摘掉，别把状态留给别人。
  document.documentElement.removeAttribute('data-theme')
  notif.stop()
})
</script>

<template>
  <!-- 主题属性由 watch 写到 <html> 上（见上方 ui.theme 的 watch），
       这里不再重复挂 :data-theme——浮层 Teleport 到 body，挂在这层管不到它们。 -->
  <div class="app">
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
             徽标是识别符号）。配色走 --logo-* 主题换皮变量。 -->
        <div class="brand">
          <RaccoonLogo :size="34" />
        </div>
        <div class="nav-scroll">
          <div v-for="sec in NAV_SECTIONS" :key="sec.en" class="nav-sec">
            <div class="nav-sec-label">
              <span class="nav-sec-en eyebrow">{{ sec.en }}</span>
              <span class="nav-sec-zh">{{ sec.label }}</span>
            </div>
            <nav :aria-label="sec.label">
              <router-link v-for="item in sec.items" :key="item.to" :to="item.to" :title="item.label">
                <svg class="nav-icon" viewBox="0 0 16 16" width="17" height="17" aria-hidden="true">
                  <path :d="item.icon" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round" />
                </svg>
                <span class="nav-txt">
                  <span class="nav-link-text">{{ item.label }}</span>
                  <span class="nav-en">{{ item.en }}</span>
                </span>
              </router-link>
            </nav>
          </div>
        </div>
        <div class="nav-foot">
          <!-- 用户名 + 连接状态一行：用户区自带分隔线，一眼是「账户区」而不是导航列表的尾巴。 -->
          <div class="user-row">
            <span class="user" :title="auth.user?.username">{{ auth.user?.username }}</span>
            <span class="conn" :class="tasks.connected ? 'on' : 'off'" :title="tasks.connected ? '实时连接正常' : '实时连接断开，正在重连'">
              {{ tasks.connected ? '实时' : '重连中' }}
            </span>
            <!-- 退出并入用户行（2026-10-24 用户设计诉求）：用户名/状态在左，退出图标在右 -->
            <button
              class="logout-inline"
              type="button"
              title="退出登录"
              aria-label="退出登录"
              @click="auth.logout()"
            >
              <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
                <path d="M6.4 3.6H3.8a1 1 0 0 0-1 1v6.8a1 1 0 0 0 1 1h2.6M10.4 5.6 13 8l-2.6 2.4M13 8H6.2" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round" />
              </svg>
            </button>
          </div>
          <button class="foot-btn" type="button" @click="aboutRef?.openDialog()">
            <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
              <path d="M8 7.2v4.2M8 4.6a.7.7 0 1 0 0-1.4.7.7 0 0 0 0 1.4zM8 14.6A6.6 6.6 0 1 0 8 1.4a6.6 6.6 0 0 0 0 13.2z" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
            <span class="btn-text">关于</span>
          </button>

        </div>
      </aside>
      <!-- 抽屉态遮罩：点它即关，Esc 也关（键盘兜底见下方 onKeydown） -->
      <div v-if="navOpen" class="nav-mask" @click="navOpen = false" />

      <div class="main">
        <header class="topbar">
          <!-- 与 .content-inner 同款居中容器：顶栏首元素与内容区左缘对齐
               （超宽屏下 content-inner 居中收缩，顶栏若不受同样约束，
               搜索框和下面卡片的左边界会错开——实测踩过）。 -->
          <div class="topbar-inner">
            <QuickSubmit
              v-model="quickUrl"
              :error="quickErr"
              @submit="onQuickSubmit"
              @submit-batch="onQuickSubmitBatch"
            />
            <div class="topbar-right">
              <span v-if="tasks.warning" class="topbar-warn" :title="tasks.warning">{{ tasks.warning }}</span>
              <span class="conn topbar-conn" :class="tasks.connected ? 'on' : 'off'" :title="tasks.connected ? '实时连接正常' : '实时连接断开，正在重连'">
                {{ tasks.connected ? '实时' : '重连中' }}
              </span>
              <span class="topbar-clock tnum">{{ clock }}</span>
              <!-- 主题切换器（2026-10-24 从侧栏底部挪来：用户区只留关于/退出） -->
              <button
                class="theme-topbar-btn"
                type="button"
                aria-haspopup="listbox"
                :aria-expanded="themeOpen ? 'true' : 'false'"
                title="切换主题"
                @click="themeOpen = !themeOpen"
              >
                <span class="foot-swatch" :style="{ background: currentTheme.swatch }" aria-hidden="true" />
                <span class="theme-topbar-label">{{ currentTheme.label }}</span>
              </button>
              <NotifCenter />
              <div v-if="themeOpen" class="pop-mask" @click="themeOpen = false" />
              <div v-if="themeOpen" class="theme-pop theme-pop--topbar" role="listbox" aria-label="选择主题">
                <button
                  v-for="t in ui.themes"
                  :key="t.id"
                  class="theme-item"
                  type="button"
                  role="option"
                  :aria-selected="t.id === ui.theme ? 'true' : 'false'"
                  @click="pickTheme(t.id)"
                >
                  <span class="foot-swatch" :style="{ background: t.swatch }" aria-hidden="true" />
                  <span class="theme-item-txt">
                    <span class="theme-item-label">{{ t.label }}</span>
                    <span class="theme-item-en">{{ t.en }}</span>
                  </span>
                  <svg v-if="t.id === ui.theme" viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
                    <path d="M3.4 8.4 6.6 11.6 12.6 5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
                  </svg>
                </button>
              </div>
            </div>
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

      <!-- 「更多」面板：点遮罩、点任一项、Esc 都关（零成本退出路径）。
           面板尾部带主题行：移动端侧导航不可达，主题切换在这里兜底。 -->
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
        <div class="sheet-themes" role="group" aria-label="选择主题">
          <button
            v-for="t in ui.themes"
            :key="t.id"
            class="sheet-theme"
            type="button"
            :aria-pressed="t.id === ui.theme ? 'true' : 'false'"
            @click="ui.setTheme(t.id)"
          >
            <span class="foot-swatch" :style="{ background: t.swatch }" aria-hidden="true" />
            <span>{{ t.label }}</span>
          </button>
        </div>
      </div>
    </template>
    <RToast />
    <AboutDialog ref="aboutRef" />
  </div>
</template>

<style scoped>
.app {
  display: flex;
  height: 100vh;
  height: 100dvh; /* 移动端地址栏收展不影响壳高 */
  background: var(--bg);
  color: var(--text-1);
  overflow: hidden; /* 控制台壳：滚动只发生在内容区，侧栏与顶栏钉死 */
}

/* ============ 侧导航：深底控制台骨架 ============ */
.nav {
  position: relative;
  width: 248px;
  flex-shrink: 0;
  padding: 20px 14px 16px;
  background: var(--nav-bg);
  border-right: 1px solid var(--nav-border);
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.brand { display: flex; align-items: center; padding: 0 6px 4px; }
/* 深底上的字标：随主题反白（nav-text-strong），副标签用 nav-text。 */
.brand .rlogo {
  --logo-name: var(--nav-text-strong);
  --logo-sub: var(--nav-text);
}
/* 分组导航可滚动：小屏高度不够时导航自己滚，账户区钉在底部。 */
.nav-scroll { flex: 1; min-height: 0; overflow-y: auto; display: flex; flex-direction: column; gap: 16px; margin: 0 -6px; padding: 0 6px; }
.nav-sec { display: flex; flex-direction: column; gap: 4px; }
/* 小节标签：等宽英文 + 中文说明，控制台的「分区标牌」。 */
.nav-sec-label { display: flex; align-items: baseline; gap: 8px; padding: 6px 12px 2px; }
.nav-sec-zh { font-size: var(--font-caption); color: var(--nav-text); opacity: 0.72; }
.nav-sec .nav-sec-en { color: var(--nav-text); opacity: 0.55; }
.nav nav { display: flex; flex-direction: column; gap: 2px; }
/* 双行导航条目：图标 + (中文主标签 / 等宽英文副标签)。 */
.nav nav a {
  position: relative;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 12px;
  border-radius: var(--radius-ctrl);
  color: var(--nav-text);
  text-decoration: none;
  transition: background 120ms ease-out, color 120ms ease-out;
}
.nav-txt { display: flex; flex-direction: column; line-height: 1.25; min-width: 0; }
.nav-link-text { font-size: var(--font-body); font-weight: var(--weight-medium); }
.nav-en {
  font-family: var(--font-mono);
  font-size: var(--font-caption);
  letter-spacing: 0.08em;
  opacity: 0.6;
}
.nav nav a:hover { background: var(--nav-hover); color: var(--nav-text-strong); }
/* 当前项：左侧强调色条 + 强调色文字 + 淡底——「你在哪」的控制台锚点。 */
.nav nav a.router-link-active {
  background: var(--nav-active-bg);
  color: var(--nav-active-text);
}
.nav nav a.router-link-active .nav-link-text { font-weight: var(--weight-semibold); }
.nav nav a.router-link-active .nav-en { opacity: 0.85; }
.nav nav a.router-link-active::before {
  content: '';
  position: absolute;
  left: 0;
  top: 50%;
  transform: translateY(-50%);
  width: 3px;
  height: 20px;
  border-radius: 2px;
  background: var(--nav-active-text);
}
/* 用户区与导航列表之间必须有分隔：否则账户区会和上面的导航项连成一条无尽列表。 */
.nav-foot { position: relative; margin-top: auto; display: flex; flex-direction: column; gap: 8px; border-top: 1px solid var(--nav-border); padding-top: 14px; }
.user-row { display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 0 12px; min-width: 0; }
/* 行内退出按钮（并入用户行，2026-10-24）：常态低调，悬停转危险色示退出意图。 */
.logout-inline {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  width: 26px;
  height: 26px;
  padding: 0;
  border: none;
  border-radius: var(--radius-ctrl);
  background: transparent;
  color: var(--text-3);
  cursor: pointer;
}
.logout-inline:hover { color: var(--danger); background: var(--surface-2); }
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
/* 主题色样本：10px 圆点，颜色来自 THEMES 元数据（当前主题的强调色样本）。 */
.foot-swatch { width: 10px; height: 10px; border-radius: 50%; flex-shrink: 0; box-shadow: 0 0 0 1px color-mix(in srgb, var(--text-1) 18%, transparent); }

/* 主题弹层：贴着按钮上沿弹出。 */
.pop-mask { position: fixed; inset: 0; z-index: 80; }
.theme-pop {
  position: absolute;
  bottom: calc(100% + 8px);
  left: 0;
  right: 0;
  z-index: 81;
  padding: 6px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  box-shadow: var(--shadow-3);
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.theme-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border: none;
  border-radius: var(--radius-ctrl);
  background: transparent;
  color: var(--text-2);
  cursor: pointer;
  font-size: var(--font-body);
  text-align: left;
}
.theme-item:hover { background: var(--accent-softer); color: var(--text-1); }
.theme-item[aria-selected='true'] { color: var(--accent); background: var(--accent-soft); }
.theme-item-txt { display: flex; flex-direction: column; line-height: 1.25; flex: 1; min-width: 0; }
.theme-item-en { font-family: var(--font-mono); font-size: var(--font-caption); letter-spacing: 0.08em; opacity: 0.65; }

.main { flex: 1; display: flex; flex-direction: column; min-width: 0; min-height: 0; }
/* 指挥顶栏：提交框为主角，右侧是系统心跳（连接状态 + 等宽时钟 + 通知）。 */
.topbar { padding: 0 24px; height: 60px; flex-shrink: 0; background: var(--surface); border-bottom: 1px solid var(--border); display: flex; align-items: center; }
/* 居中容器：与 .content-inner 完全同款（1440 上限 + 居中），顶栏与内容区左右缘对齐。 */
.topbar-inner { width: 100%; max-width: 1440px; margin: 0 auto; display: flex; align-items: center; gap: 20px; }
.topbar-inner > :first-child { flex: 1; max-width: 620px; }
.topbar-right { margin-left: auto; display: flex; align-items: center; gap: 14px; }
/* 顶栏主题切换（2026-10-24 从侧栏挪入）：色板 + 当前主题名，紧凑一行。 */
.theme-topbar-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-ctrl);
  background: transparent;
  color: var(--text-2);
  font-size: var(--font-aux);
  cursor: pointer;
}
.theme-topbar-btn:hover { color: var(--text-1); border-color: var(--text-3); }
/* 顶栏弹层：right:0 对齐按钮右缘、自按钮下方展开（覆盖侧栏版的 bottom 定位）。 */
.theme-pop--topbar {
  bottom: auto;
  top: calc(100% + 8px);
  left: auto;
  right: 0;
  width: 200px;
}
.topbar-right { position: relative; }
/* 顶栏连接状态与告警：辅助档 13.5px（这是要读的一句话，不是表头）。 */
.topbar-conn { color: var(--text-3); }
.topbar-warn {
  max-width: 260px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--font-aux);
  color: var(--warning);
}
/* 等宽时钟：系统心跳（tnum 保数字不抖）。 */
.topbar-clock {
  font-family: var(--font-mono);
  font-size: var(--font-caption);
  letter-spacing: 0.06em;
  color: var(--text-3);
}
.content { padding: 24px; flex: 1; min-width: 0; min-height: 0; overflow-y: auto; }
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

/* 1024–1279：侧导航收图标栏（图标居中，中英文字与小节标签隐藏） */
@media (max-width: 1279px) and (min-width: 1024px) {
  .nav { width: 64px; padding: 20px 8px 16px; align-items: center; }
  .brand { justify-content: center; padding: 0; }
  .nav-scroll { width: 100%; margin: 0; padding: 0; }
  .nav-sec { align-items: center; }
  .nav-sec-label { display: none; }
  .nav nav { width: 100%; }
  .nav nav a { justify-content: center; padding: 11px 0; }
  /* 图标栏态的强调条改成圆点：竖条在 64px 窄栏里会戳出边界 */
  .nav nav a.router-link-active::before { width: 18px; height: 3px; top: auto; bottom: 3px; transform: none; }
  .nav-txt { display: none; }
  .nav-foot { align-items: center; }
  .nav-foot .user,
  .nav-foot .conn { display: none; }
  .nav-foot .btn-text { display: none; }
  .foot-btn { padding: 10px; justify-content: center; }
  .theme-pop { left: auto; right: auto; width: 180px; }
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
  .topbar { padding: 0 12px; gap: 10px; height: 56px; }
  /* 小屏顶栏只留心跳的最小形态：连接点 + 铃铛，时钟让位给提交框 */
  .topbar-clock { display: none; }
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
  /* 主题行：占满一行的四枚色板（移动端唯一的主题切换入口）。 */
  .sheet-themes {
    grid-column: 1 / -1;
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 8px;
    padding-top: 8px;
    border-top: 1px solid var(--border);
  }
  .sheet-theme {
    min-height: 44px;
    display: inline-flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 4px;
    padding: 6px 4px;
    border: none;
    border-radius: var(--radius-ctrl);
    background: none;
    color: var(--text-2);
    font-size: var(--font-caption);
    cursor: pointer;
  }
  .sheet-theme[aria-pressed='true'] { color: var(--accent); background: var(--accent-soft); }
  /* 内容底部留出 tabbar 高度 + 间距，长页面最后一个按钮不会被挡住 */
  .content { padding: 16px 14px calc(16px + 52px + env(safe-area-inset-bottom, 0px)); }
}

.nav-icon { flex-shrink: 0; }

</style>
