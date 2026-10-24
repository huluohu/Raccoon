// 全局测试准备：保证每个用例结束后组件被卸载干净 + Web Storage 兜底。
//
// 为什么必须走 setupFiles：hook 要按「测试文件」注册，而 mount.ts 是源码模块，
// 每个文件重新求值一次（node_modules 里的 @vue/test-utils 不会），
// 把 afterEach 写在 mount.ts 里只会在第一个 import 它的文件生效。
// setupFiles 由 vitest 对每个测试文件各跑一遍，注册才是稳的。
import { afterEach } from 'vitest'
import { unmountAllTracked } from '@/test/mount'

// ---- Web Storage 兜底（Node ≥22 的实验性 localStorage 全局）----
//
// Node 22+ 往全局注入了自己的实验性 localStorage（需 --localstorage-file 才可用），
// 未提供时该键存在但值是 undefined，把 jsdom 本应挂上的实现整个遮蔽掉——
// 于是 vitest（jsdom）里 typeof localStorage === 'undefined'，凡用到它的用例全炸
// （本机 Node 26 实测；CI 的 Node 20 没有这个全局，所以此前没暴露）。
// 这里在缺失时补一个 Map 后备实现：测试只依赖 set/get/remove/clear 的进程内语义。
function installStorageShim(): void {
  const g = globalThis as Record<string, unknown>
  const make = (): Storage => {
    const m = new Map<string, string>()
    return {
      get length() {
        return m.size
      },
      key: (i: number) => [...m.keys()][i] ?? null,
      getItem: (k: string) => (m.has(k) ? m.get(k)! : null),
      setItem: (k: string, v: string) => void m.set(String(k), String(v)),
      removeItem: (k: string) => void m.delete(k),
      clear: () => void m.clear(),
    }
  }
  if (typeof g.localStorage === 'undefined') g.localStorage = make()
  if (typeof g.sessionStorage === 'undefined') g.sessionStorage = make()
}
installStorageShim()

afterEach(() => {
  unmountAllTracked()
})
