<script setup lang="ts">
// 应用外壳：左侧导航（七个一级页面，方案 §8）+ 顶栏快速提交。设计 Token 见 styles/tokens.css。
// 会话 gate：未初始化 → setup；未登录 → Login；就绪才渲染主界面（后端 428/401 语义的前端落点）。
import { onMounted, ref } from 'vue'
import { useUiStore } from '@/stores/ui'
import { useAuthStore } from '@/stores/auth'
import { useTasksStore } from '@/stores/tasks'
import Login from '@/views/Login.vue'

const ui = useUiStore()
const auth = useAuthStore()
const tasks = useTasksStore()

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

onMounted(async () => {
  await auth.bootstrap()
  if (auth.phase === 'ready') {
    await tasks.refresh()
    tasks.connect()
  }
})
</script>

<template>
  <div class="app" :data-theme="ui.theme">
    <Login v-if="auth.phase !== 'ready'" />
    <template v-else>
      <aside class="nav">
        <div class="brand">
          <span class="brand-logo" aria-hidden="true">🦝</span>
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
          <button class="theme-toggle" type="button" @click="ui.toggleTheme">
            {{ ui.theme === 'dark' ? '☀️ 浅色' : '🌙 深色' }}
          </button>
          <button class="logout" type="button" @click="auth.logout()">退出登录</button>
        </div>
      </aside>
      <div class="main">
        <header class="topbar">
          <input
            v-model="quickUrl"
            class="quick-submit"
            type="url"
            placeholder="粘贴链接下载（回车直接提交）"
            aria-label="粘贴链接下载"
            @keyup.enter="quickSubmit"
          />
          <button class="quick-btn" type="button" :disabled="!quickUrl.trim()" @click="quickSubmit">下载</button>
          <span v-if="quickErr" class="quick-err">{{ quickErr }}</span>
        </header>
        <main class="content">
          <router-view v-slot="{ Component }">
            <component :is="Component" />
          </router-view>
        </main>
      </div>
    </template>
  </div>
</template>

<style scoped>
.app { display: flex; min-height: 100vh; background: var(--bg); color: var(--text-1); }
.nav { width: 208px; flex-shrink: 0; padding: 16px 12px; background: var(--surface); border-right: 1px solid var(--border); display: flex; flex-direction: column; gap: 16px; }
.brand { display: flex; align-items: center; gap: 8px; font-size: 17px; font-weight: 700; }
.brand-logo { font-size: 20px; }
.brand-name em { font-style: normal; font-size: 12px; color: var(--text-3); margin-left: 2px; }
.nav nav { display: flex; flex-direction: column; gap: 2px; }
.nav nav a { padding: 8px 12px; border-radius: var(--radius-ctrl); color: var(--text-2); text-decoration: none; font-size: 14px; }
.nav nav a:hover { background: var(--surface-2); }
.nav nav a.router-link-active { background: var(--accent-soft); color: var(--accent); font-weight: 500; }
.nav-foot { margin-top: auto; display: flex; flex-direction: column; gap: 8px; }
.user { font-size: var(--font-caption); color: var(--text-3); padding-left: 12px; overflow: hidden; text-overflow: ellipsis; }
.theme-toggle, .logout { padding: 8px 12px; border: 1px solid var(--border); border-radius: var(--radius-ctrl); background: var(--surface); color: var(--text-2); cursor: pointer; font-size: var(--font-aux); text-align: left; }
.logout:hover { color: var(--danger); border-color: var(--danger); }
.main { flex: 1; display: flex; flex-direction: column; min-width: 0; }
.topbar { padding: 12px 24px; background: var(--surface); border-bottom: 1px solid var(--border); display: flex; align-items: center; gap: 8px; }
.quick-submit { flex: 1; max-width: 640px; padding: 8px 12px; border: 1px solid var(--border); border-radius: var(--radius-ctrl); background: var(--bg); color: var(--text-1); font-size: var(--font-body); }
.quick-submit:focus { outline: 2px solid var(--gold); outline-offset: 1px; }
.quick-btn { padding: 8px 16px; border: none; border-radius: var(--radius-ctrl); background: var(--accent); color: #fff; font-weight: 600; cursor: pointer; }
.quick-btn:disabled { opacity: 0.55; cursor: not-allowed; }
.quick-err { color: var(--danger); font-size: var(--font-caption); }
.content { padding: 24px; }
</style>