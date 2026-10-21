import { createRouter, createWebHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'

// 七个一级页面（docs/07 §3）。Wave 1 先落 Overview；其余页面在 Wave 3 逐批落地。
const routes: RouteRecordRaw[] = [
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
