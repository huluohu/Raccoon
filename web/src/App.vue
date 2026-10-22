<script setup lang="ts">
// 应用外壳：左侧导航（七个一级页面，方案 §8）+ 顶栏快速提交 + 通知中心。设计 Token 见 styles/tokens.css。
// 会话 gate：未初始化 → setup；未登录 → Login；就绪才渲染主界面（后端 428/401 语义的前端落点）。
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { useUiStore } from '@/stores/ui'
import { useAuthStore } from '@/stores/auth'
import { useTasksStore } from '@/stores/tasks'
import { useNotificationsStore } from '@/stores/notifications'
import Login from '@/views/Login.vue'
import NotifCenter from '@/components/shell/NotifCenter.vue'
import RToast from '@/components/base/RToast.vue'
import RaccoonFace from '@/components/raccoon/RaccoonFace.vue'

const ui = useUiStore()
const auth = useAuthStore()
const tasks = useTasksStore()
const notif = useNotificationsStore()

const quickUrl = ref('')
const quickErr = ref('')

async function quickSubmit() {
  const target = quickUrl.value.trim()
  if (!target) return
  quickErr.value = ''
  try {
    await tasks.submit(target)
    quickUrl.value = ''
  } catch (e) {
    quickErr.value = e instanceof Error ? e.message : '提交失败'
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
    await tasks.refresh()
    tasks.connect()
    notif.start()
  },
)

onMounted(async () => {
  await auth.bootstrap()
  // bootstrap 可能已经把 phase 置为 ready（cookie 仍有效），watch 不会为「同值」触发，这里补一次。
  if (auth.phase === 'ready') {
    await tasks.refresh()
    tasks.connect()
    notif.start()
  }
})

onUnmounted(() => notif.stop())
</script>

<template>
  <div class="app" :data-theme="ui.theme">
    <Login v-if="auth.phase !== 'ready'" />
    <template v-else>
      <aside class="nav">
        <div class="brand">
          <RaccoonFace face="working" :size="22" />
          <span class="brand-name">浣熊 <em>Raccoon</em></span>
        </div>
        <nav>
          <router-link to="/">总览</router-link>
          <router-link to="/tasks">下载</router-link>
          <router-link to="/media">媒体库</router-link>
          <router-link to="/subscriptions">订阅</router-link>
          <router-link to="/live">直播</router-link>
          <router-link to="/connections">连接</router-link>
          <router-link to="/settings">设置</router-link>
        </nav>
        <div class="nav-foot">
          <span class="user" :title="auth.user?.username">{{ auth.user?.username }}</span>
          <span class="conn" :class="tasks.connected ? 'on' : 'off'" :title="tasks.connected ? '实时连接正常' : '实时连接断开，正在重连'">
            {{ tasks.connected ? '实时' : '重连中' }}
          </span>
          <button class="theme-toggle" type="button" @click="ui.toggleTheme">
            {{ ui.theme === 'dark' ? '浅色模式' : '深色模式' }}
          </button>
          <button class="logout" type="button" @click="auth.logout()">退出登录</button>
        </div>
      </aside>
      <div class="main">
        <header class="topbar">
          <input
            v-model="quickUrl"
            id="quick-submit"
            class="quick-submit"
            type="url"
            placeholder="粘贴链接下载（回车直接提交）"
            aria-label="粘贴链接下载"
            @keyup.enter="quickSubmit"
          />
          <button class="quick-btn" type="button" :disabled="!quickUrl.trim()" @click="quickSubmit">下载</button>
          <span v-if="quickErr" class="quick-err">{{ quickErr }}</span>
          <div class="topbar-right">
            <span v-if="tasks.warning" class="topbar-warn" :title="tasks.warning">{{ tasks.warning }}</span>
            <NotifCenter />
          </div>
        </header>
        <main class="content">
          <router-view v-slot="{ Component }">
            <component :is="Component" />
          </router-view>
        </main>
      </div>
    </template>
    <RToast />
  </div>
</template>

<style scoped>
.app { display: flex; min-height: 100vh; background: var(--bg); color: var(--text-1); }
.nav { width: 208px; flex-shrink: 0; padding: 16px 12px; background: var(--surface); border-right: 1px solid var(--border); display: flex; flex-direction: column; gap: 16px; }
.brand { display: flex; align-items: center; gap: 8px; font-size: 17px; font-weight: 700; }
.brand-name em { font-style: normal; font-size: 12px; color: var(--text-3); margin-left: 2px; }
.nav nav { display: flex; flex-direction: column; gap: 2px; }
.nav nav a { padding: 8px 12px; border-radius: var(--radius-ctrl); color: var(--text-2); text-decoration: none; font-size: 14px; }
.nav nav a:hover { background: var(--surface-2); }
.nav nav a.router-link-active { background: var(--accent-soft); color: var(--accent); font-weight: 500; }
.nav-foot { margin-top: auto; display: flex; flex-direction: column; gap: 8px; }
.user { font-size: var(--font-caption); color: var(--text-3); padding-left: 12px; overflow: hidden; text-overflow: ellipsis; }
.conn {
  font-size: 10px;
  margin-left: 12px;
  color: var(--text-3);
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.conn::before { content: ''; width: 6px; height: 6px; border-radius: 50%; background: var(--neutral); }
.conn.on::before { background: var(--success); }
.conn.off::before { background: var(--warning); }
.theme-toggle, .logout { padding: 8px 12px; border: 1px solid var(--border); border-radius: var(--radius-ctrl); background: var(--surface); color: var(--text-2); cursor: pointer; font-size: var(--font-aux); text-align: left; }
.logout:hover { color: var(--danger); border-color: var(--danger); }
.main { flex: 1; display: flex; flex-direction: column; min-width: 0; }
.topbar { padding: 12px 24px; background: var(--surface); border-bottom: 1px solid var(--border); display: flex; align-items: center; gap: 8px; }
.quick-submit { flex: 1; max-width: 640px; padding: 8px 12px; border: 1px solid var(--border); border-radius: var(--radius-ctrl); background: var(--bg); color: var(--text-1); font-size: var(--font-body); }
.quick-submit:focus { outline: 2px solid var(--gold); outline-offset: 1px; }
.quick-btn { padding: 8px 16px; border: none; border-radius: var(--radius-ctrl); background: var(--accent); color: #fff; font-weight: 600; cursor: pointer; }
.quick-btn:disabled { opacity: 0.55; cursor: not-allowed; }
.quick-err { color: var(--danger); font-size: var(--font-caption); }
.topbar-right { margin-left: auto; display: flex; align-items: center; gap: 10px; }
.topbar-warn {
  max-width: 260px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--font-caption);
  color: var(--warning);
}
.content { padding: 24px; flex: 1; min-width: 0; }
</style>