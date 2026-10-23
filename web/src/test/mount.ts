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
  tracked().push(wrapper as unknown as Unmountable)
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

/**
 * 每个用例结束后必须卸载：下面是为什么不能用 VTU 自带的 enableAutoUnmount。
 *
 * VTU 的 enableAutoUnmount 把 isEnabled / wrapperInstances 放在**模块作用域**里，
 * 而 @vue/test-utils 是 node_modules 外链依赖，整个 fork 进程只加载一次——
 * 于是「七个 spec 各注册一次」会抛 cannot be called more than once；
 * 只留一个文件注册，其余文件的组件又永远不会被卸载。
 * 两条路都会让上一个用例的 DOM 留在 document.body 上，
 * 下一个用例 document.querySelector / activeElement 读到的全是别人的节点。
 *
 * 这里改成自己记账：globalThis 上挂一个待卸载队列（跨文件共享无副作用，
 * 每个文件结束时由 setupFiles 注册的 afterEach 清空）。
 */
const TRACKED_KEY = '__raccoon_mounted_wrappers__'

type Unmountable = { unmount: () => void }

function tracked(): Unmountable[] {
  const g = globalThis as unknown as Record<string, Unmountable[] | undefined>
  if (!g[TRACKED_KEY]) g[TRACKED_KEY] = []
  return g[TRACKED_KEY] as Unmountable[]
}

/** 卸载所有已挂载实例并摘掉宿主节点。由 src/test/setup.ts 的 afterEach 调用。 */
export function unmountAllTracked(): void {
  const list = tracked()
  while (list.length) {
    try {
      list.pop()?.unmount()
    } catch {
      // 已经被上游 unmount 过：清干净即可，不该让用例挂在这种地方
    }
  }
  while (mountedNodes.length) mountedNodes.pop()?.remove()
}

/**
 * 等价于 VTU 的 mount，但会登记到卸载队列里。
 * spec 统一从这里取 mount，别再从 @vue/test-utils 直接 import。
 */
// 整体断言成 typeof vtuMount：保留 VTU 的全部重载与返回类型推导，
// 否则泛型组件的 slots/props 会在调用点报一串 TS2345。
export const mount = ((
  comp: unknown,
  options?: unknown,
): ReturnType<typeof vtuMount> => {
  const wrapper = vtuMount(
    comp as Component,
    (options ?? {}) as MountingOptions<never> & Record<string, unknown>,
  )
  tracked().push(wrapper as unknown as Unmountable)
  return wrapper
}) as typeof vtuMount

export { createPinia, setActivePinia }
export type { Router }
