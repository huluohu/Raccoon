<script setup lang="ts">
// 应用外壳：左侧导航（七个一级页面，方案 §8）+ 顶栏快速提交 + 通知中心。设计 Token 见 styles/tokens.css。
// 会话 gate：未初始化 → setup；未登录 → Login；就绪才渲染主界面（后端 428/401 语义的前端落点）。
//
// 响应式四档（docs/07 §6）：
//   ≥1280        侧导航常驻 + 双栏（默认）
//   1024–1279    侧导航收图标栏（仅图标，文字隐藏）
//   768–1023     侧导航收进抽屉，顶栏出汉堡按钮；表格降级卡片（各视图自行处理）
//   <768         单栏 + 底部主操作栏，触控目标 ≥44px，内容留出底部安全区
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useUiStore } from '@/stores/ui'
import { useAuthStore } from '@/stores/auth'
import { useTasksStore } from '@/stores/tasks'
import { useNotificationsStore } from '@/stores/notifications'
import Login from '@/views/Login.vue'
import Setup from '@/views/Setup.vue'
import NotifCenter from '@/components/shell/NotifCenter.vue'
import QuickSubmit from '@/components/shell/QuickSubmit.vue'
import RToast from '@/components/base/RToast.vue'
import RaccoonFace from '@/components/raccoon/RaccoonFace.vue'

const ui = useUiStore()
const auth = useAuthStore()
const tasks = useTasksStore()
const notif = useNotificationsStore()
const router = useRouter()

const quickUrl = ref('')
const quickErr = ref('')
/** 768–1023 抽屉态：侧导航收进抽屉时的开合。 */
const navOpen = ref(false)

/** 一级导航：图标用线稿 SVG（docs/07 §4 禁 emoji 冒充图标）。 */
const NAV_ITEMS = [
  { to: '/', label: '总览', icon: 'M2.6 7.3 8 2.9l5.4 4.4v5.4a.9.9 0 0 1-.9.9H3.5a.9.9 0 0 1-.9-.9z M6.2 13.6V9.4h3.6v4.2' },
  { to: '/tasks', label: '下载', icon: 'M8 2.2v7.2m0 0 2.8-2.8M8 9.4 5.2 6.6M2.6 12.6h10.8' },
  { to: '/media', label: '媒体库', icon: 'M2.4 4.4h3.9l1.2 1.6h6.1v6.9a1 1 0 0 1-1 1H3.4a1 1 0 0 1-1-1z M6.6 9.9l1.5 1.7 1.1-1.2 1.6 1.8' },
  { to: '/subscriptions', label: '订阅', icon: 'M3.2 6.6a5.4 5.4 0 0 1 5.4 5.4M3.2 3.4a8.6 8.6 0 0 1 8.6 8.6M4.4 12a.9.9 0 1 0 0-.1' },
  { to: '/live', label: '直播', icon: 'M8 9.1a1.5 1.5 0 1 0 0-3 1.5 1.5 0 0 0 0 3zM5.1 5.1a4.6 4.6 0 0 0 0 5.8m5.8 0a4.6 4.6 0 0 0 0-5.8M2.7 2.7a7.8 7.8 0 0 0 0 10.6m10.6 0a7.8 7.8 0 0 0 0-10.6' },
  { to: '/connections', label: '连接', icon: 'M6.4 9.6 9.6 6.4M5.4 7.2 3.9 8.7a2.4 2.4 0 0 0 3.4 3.4l1.5-1.5M10.6 8.8l1.5-1.5a2.4 2.4 0 0 0-3.4-3.4L7.2 5.4' },
  { to: '/settings', label: '设置', icon: 'M8 5.6a2.4 2.4 0 1 0 0 4.8 2.4 2.4 0 0 0 0-4.8zM13 8c0-.6-.1-1.1-.2-1.6l1.3-1-1.3-2.2-1.5.6c-.5-.4-1.1-.8-1.7-1l-.2-1.6h-2.6l-.2 1.6c-.6.2-1.2.6-1.7 1l-1.5-.6-1.3 2.2 1.3 1c-.1.5-.2 1-.2 1.6s.1 1.1.2 1.6l-1.3 1 1.3 2.2 1.5-.6c.5.4 1.1.8 1.7 1l.2 1.6h2.6l.2-1.6c.6-.2 1.2-.6 1.7-1l1.5.6 1.3-2.2-1.3-1c.1-.5.2-1 .2-1.6z' },
]

/** 768–1023 抽屉态：Esc 关闭（焦点即使跑出抽屉也能兜住）。 */
function onNavKey(ev: KeyboardEvent) {
  if (ev.key === 'Escape' && navOpen.value) navOpen.value = false
}

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
        <div class="brand">
          <RaccoonFace face="working" :size="22" />
          <span class="brand-name">浣熊 <em>Raccoon</em></span>
        </div>
        <nav @click="navOpen = false">
          <router-link v-for="item in NAV_ITEMS" :key="item.to" :to="item.to" :title="item.label">
            <svg class="nav-icon" viewBox="0 0 16 16" width="16" height="16" aria-hidden="true">
              <path :d="item.icon" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
            <span class="nav-link-text">{{ item.label }}</span>
          </router-link>
        </nav>
        <div class="nav-foot">
          <!-- 用户名 + 连接状态一行：原来分两行刷下来，和上面 15 个导航项
               连成一条长列表，像没写完。现在用户区自带分隔线，一眼是「账户区」。 -->
          <div class="user-row">
            <span class="user" :title="auth.user?.username">{{ auth.user?.username }}</span>
            <span class="conn" :class="tasks.connected ? 'on' : 'off'" :title="tasks.connected ? '实时连接正常' : '实时连接断开，正在重连'">
              {{ tasks.connected ? '实时' : '重连中' }}
            </span>
          </div>
          <button class="theme-toggle" type="button" @click="ui.toggleTheme">
            <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
              <path v-if="ui.theme === 'dark'" d="M13 9.8A5.6 5.6 0 0 1 6.2 3a5.6 5.6 0 1 0 6.8 6.8z" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linejoin="round" />
              <path v-else d="M8 3.2v1.6m0 6.4v1.6M3.2 8h1.6m6.4 0h1.6M4.8 4.8l1.1 1.1m4.2 4.2 1.1 1.1m0-6.4-1.1 1.1M5.9 10.1l-1.1 1.1M8 5.6a2.4 2.4 0 1 0 0 4.8 2.4 2.4 0 0 0 0-4.8z" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" />
            </svg>
            <span class="btn-text">{{ ui.theme === 'dark' ? '浅色模式' : '深色模式' }}</span>
          </button>
          <button class="logout" type="button" @click="auth.logout()">
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

      <!-- <768：底部主操作栏（触控目标 ≥44px；内容区已留出底部安全区，关键按钮不被挡） -->
      <nav class="tabbar" aria-label="主导航">
        <router-link v-for="item in NAV_ITEMS" :key="item.to" :to="item.to" :title="item.label">
          <svg viewBox="0 0 16 16" width="20" height="20" aria-hidden="true">
            <path :d="item.icon" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
          <span class="tabbar-label">{{ item.label }}</span>
        </router-link>
      </nav>
    </template>
    <RToast />
  </div>
</template>

<style scoped>
.app { display: flex; min-height: 100vh; background: var(--bg); color: var(--text-1); }
.nav { width: 208px; flex-shrink: 0; padding: 16px 12px; background: var(--surface); border-right: 1px solid var(--border); display: flex; flex-direction: column; gap: 16px; }
.brand { display: flex; align-items: center; gap: 8px; font-size: var(--font-title-3); font-weight: var(--weight-strong); }
.brand-name em { font-style: normal; font-size: var(--font-aux); color: var(--text-3); margin-left: 2px; }
.nav nav { display: flex; flex-direction: column; gap: 2px; }
/* 导航条目：正文档 16px。导航是主操作路径，7 个入口都用 aux(13.5px) 会让
   「当前在哪、点哪去」这条主路径看起来比次要说明还轻。 */
.nav nav a { padding: 9px 12px; border-radius: var(--radius-ctrl); color: var(--text-2); text-decoration: none; font-size: var(--font-body); }
.nav nav a:hover { background: var(--surface-2); }
.nav nav a.router-link-active { background: var(--accent-soft); color: var(--accent); font-weight: var(--weight-semibold); }
/* 用户区与导航列表之间必须有分隔：否则 admin / 实时 / 深色模式 / 退出登录
   会和上面 15 个导航项连成一条无尽列表，观感就是「这页面没做完」。 */
.nav-foot { margin-top: auto; display: flex; flex-direction: column; gap: 8px; border-top: 1px solid var(--border); padding-top: 14px; }
.user-row { display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 0 12px; min-width: 0; }
/* 用户名是「当前身份」，正文档 + 主文字色，不当辅助信息。 */
.user { font-size: var(--font-body); font-weight: var(--weight-strong); color: var(--text-2); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
/* 实时连接状态：辅助档 13.5px（§2「12px 仅表头/徽章」，这是状态提示不是徽章）。 */
.conn {
  font-size: var(--font-aux);
  margin-left: 0;
  flex-shrink: 0;
  color: var(--text-3);
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.conn::before { content: ''; width: 6px; height: 6px; border-radius: 50%; background: var(--neutral); }
.conn.on::before { background: var(--success); }
.conn.off::before { background: var(--warning); }
.theme-toggle, .logout { padding: 9px 12px; border: 1px solid var(--border); border-radius: var(--radius-ctrl); background: var(--surface); color: var(--text-2); cursor: pointer; font-size: var(--font-body); text-align: left; }
.logout:hover { color: var(--danger); border-color: var(--danger); }
.main { flex: 1; display: flex; flex-direction: column; min-width: 0; }
.topbar { padding: 12px 24px; background: var(--surface); border-bottom: 1px solid var(--border); display: flex; align-items: flex-start; gap: 8px; }
.topbar-right { margin-left: auto; display: flex; align-items: center; gap: 10px; padding-top: 1px; }
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

/* ≥1280：侧导航常驻 + 双栏 —— 即上方默认样式，无需覆盖 */

/* 1024–1279：侧导航收图标栏（图标居中，文字与尾部文字按钮隐藏） */
@media (max-width: 1279px) and (min-width: 1024px) {
  .nav { width: 60px; padding: 16px 6px; align-items: center; }
  .brand { justify-content: center; }
  .brand-name { display: none; }
  .nav nav { width: 100%; }
  .nav nav a { justify-content: center; padding: 10px 0; }
  .nav-link-text { display: none; }
  .nav-foot { align-items: center; }
  .nav-foot .user,
  .nav-foot .conn { display: none; }
  .nav-foot .btn-text { display: none; }
  .theme-toggle,
  .logout { padding: 10px; justify-content: center; }
}

/* 768–1023：侧导航收进抽屉 + 顶栏汉堡；表格降级卡片由各视图自行处理 */
@media (max-width: 1023px) and (min-width: 768px) {
  .nav-toggle { display: inline-flex; }
  .nav {
    position: fixed;
    inset: 0 auto 0 0;
    z-index: 60;
    width: 232px;
    transform: translateX(-100%);
    transition: transform var(--motion-panel) var(--motion-panel-ease);
    box-shadow: var(--shadow-3);
  }
  .nav.open { transform: translateX(0); }
  .nav-mask { position: fixed; inset: 0; z-index: 50; background: var(--scrim); }
  .topbar { padding-left: 12px; }
}

/* <768：单栏 + 底部主操作栏；触控目标 ≥44px；内容留出底部安全区（375px 关键按钮不被挡） */
@media (max-width: 767px) {
  .nav,
  .nav-toggle,
  .nav-mask { display: none; }
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
  /* 内容底部留出 tabbar 高度 + 间距，长页面最后一个按钮不会被挡住 */
  .content { padding: 16px 14px calc(16px + 52px + env(safe-area-inset-bottom, 0px)); }
  .topbar { padding: 10px 14px; }
}

.nav-icon { flex-shrink: 0; }

</style>