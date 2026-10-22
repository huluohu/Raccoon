import { createRouter, createWebHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'

// 路由表与 docs/07 §3 逐条对应：/setup、/login + 七个一级页面。
// setup/login 由 App.vue 的会话 gate 按 auth.phase 内联渲染；这里再注册同组件路由，
// 是为了让用户直接输 /login、/setup（或书签/反代重定向）时不落到空白页。
const routes: RouteRecordRaw[] = [
  { path: '/setup', name: 'setup', component: () => import('@/views/Login.vue'), meta: { title: '初始化' } },
  { path: '/login', name: 'login', component: () => import('@/views/Login.vue'), meta: { title: '登录' } },
  { path: '/', name: 'overview', component: () => import('@/views/Overview.vue'), meta: { title: '总览' } },
  { path: '/tasks', name: 'tasks', component: () => import('@/views/Tasks.vue'), meta: { title: '下载' } },
  { path: '/media', name: 'media', component: () => import('@/views/Media.vue'), meta: { title: '媒体库' } },
  { path: '/subscriptions', name: 'subscriptions', component: () => import('@/views/Subscriptions.vue'), meta: { title: '订阅' } },
  { path: '/live', name: 'live', component: () => import('@/views/Live.vue'), meta: { title: '直播' } },
  { path: '/connections', name: 'connections', component: () => import('@/views/Connections.vue'), meta: { title: '连接' } },
  { path: '/settings', name: 'settings', component: () => import('@/views/Settings.vue'), meta: { title: '设置' } },
]

export const router = createRouter({
  history: createWebHistory(),
  routes,
})

router.afterEach((to) => {
  document.title = to.meta.title ? `浣熊 · ${to.meta.title}` : '浣熊 · 自托管多媒体下载中心'
})
