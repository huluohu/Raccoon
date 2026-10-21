<script setup lang="ts">
// 应用外壳：左侧导航（七个一级页面，方案 §8）+ 顶栏快速提交占位。设计 Token 见 styles/tokens.css。
import { useUiStore } from '@/stores/ui'

const ui = useUiStore()
</script>

<template>
  <div class="app" :data-theme="ui.theme">
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
      <button class="theme-toggle" type="button" @click="ui.toggleTheme">
        {{ ui.theme === 'dark' ? '☀️ 浅色' : '🌙 深色' }}
      </button>
    </aside>
    <div class="main">
      <header class="topbar">
        <input class="quick-submit" type="text" placeholder="粘贴链接下载（按 / 快速聚焦）" aria-label="粘贴链接下载" />
      </header>
      <main class="content">
        <router-view v-slot="{ Component }">
          <component :is="Component" />
        </router-view>
      </main>
    </div>
  </div>
</template>

<style scoped>
.app { display: flex; min-height: 100vh; background: var(--bg); color: var(--text-1); }
.nav { width: 208px; flex-shrink: 0; padding: 16px 12px; background: var(--surface); border-right: 1px solid var(--border); display: flex; flex-direction: column; gap: 16px; }
.brand { display: flex; align-items: center; gap: 8px; font-size: var(--font-title-3); font-weight: 700; }
.brand-logo { font-size: 20px; }
.brand-name em { font-style: normal; font-size: 12px; color: var(--text-3); margin-left: 2px; }
.nav nav { display: flex; flex-direction: column; gap: 2px; }
.nav nav a { padding: 8px 12px; border-radius: var(--radius-ctrl); color: var(--text-2); text-decoration: none; font-size: 14px; }
.nav nav a:hover { background: var(--surface-2); }
.nav nav a.router-link-active { background: var(--accent-soft); color: var(--accent); font-weight: 500; }
.theme-toggle { margin-top: auto; padding: 8px 12px; border: 1px solid var(--border); border-radius: var(--radius-ctrl); background: var(--surface); color: var(--text-2); cursor: pointer; }
.main { flex: 1; display: flex; flex-direction: column; min-width: 0; }
.topbar { padding: 12px 24px; background: var(--surface); border-bottom: 1px solid var(--border); }
.quick-submit { width: 100%; max-width: 640px; padding: 8px 12px; border: 1px solid var(--border); border-radius: var(--radius-ctrl); background: var(--bg); color: var(--text-1); font-size: var(--font-body); }
.quick-submit:focus { outline: 2px solid var(--gold); outline-offset: 1px; }
.content { padding: 24px; }
</style>
