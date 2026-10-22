import { afterEach } from 'vitest'
import { mount as vtuMount, type MountingOptions } from '@vue/test-utils'
import type { Component } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createWebHistory, type Router } from 'vue-router'

/**
 * 视图单测的统一挂载入口。
 *
 * 为什么要有它：视图在 setup 里普遍调用 useRouter()（跳转按钮）、useXxxStore()，
 * 裸 mount 会刷 "injection Symbol(router) not found"。这里统一装好 pinia + 空路由，
 * 各 spec 只关心业务断言，不用每份都铺一遍基础设施。
 *
 * 为什么必须 attachTo 真实 document：VTU 默认把组件挂在一棵脱离文档的节点树上，
 * 于是 document.getElementById / document.activeElement / el.focus() 全部失效。
 * docs/07 §7 的快捷键（`/` 聚焦提交框）和 RDialog 的「默认焦点在取消」都是真实
 * 焦点行为，只能挂到 document.body 上才验得到。
 *
 * @param comp   待挂载组件
 * @param routes 需要真实路由匹配时传入；默认空路由（足够 useRouter().push 不炸）
 */
const mountedNodes: HTMLElement[] = []

/** 建一棵挂到 document.body 上的宿主节点，并把 router 一起交回去（脏状态拦截要用）。 */
function mountWithRouter<P = unknown>(
  comp: Component,
  routes: { path: string; component: Component }[],
  options: MountingOptions<P> = {},
) {
  setActivePinia(createPinia())
  const router: Router = createRouter({ history: createWebHistory(), routes })
  // 注意合并顺序：options.global 是调用方传的 stubs/plugins，必须和 router 合并，
  // 不能整体覆盖 global（否则 useRouter() 会报 injection not found）。
  const { global, ...rest } = options
  const host = document.createElement('div')
  document.body.appendChild(host)
  mountedNodes.push(host)
  const wrapper = vtuMount(comp, {
    ...rest,
    attachTo: host,
    global: {
      plugins: [router],
      ...(global ?? {}),
    },
  })
  return { wrapper, router }
}

export function mountView<P = unknown>(
  comp: Component,
  routes: { path: string; component: Component }[] = [],
  options: MountingOptions<P> = {},
) {
  return mountWithRouter(comp, routes, options).wrapper
}

/** 需要断言路由行为的用例（如设置页离开拦截）用这个：能拿到真实 router。 */
export function mountViewWithRouter<P = unknown>(
  comp: Component,
  routes: { path: string; component: Component }[] = [],
  options: MountingOptions<P> = {},
) {
  return mountWithRouter(comp, routes, options)
}

// 每个用例结束后把挂载节点从文档里摘掉：否则下一个用例 document.getElementById
// 会撞上上一个用例的残留节点（两个 #task-url-input 同时存在，断言全乱）。
afterEach(() => {
  while (mountedNodes.length) mountedNodes.pop()?.remove()
})

export { createPinia, setActivePinia }
export type { Router }
