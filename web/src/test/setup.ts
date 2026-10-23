// 全局测试准备：只做一件事——保证每个用例结束后组件被卸载干净。
//
// 为什么必须走 setupFiles：hook 要按「测试文件」注册，而 mount.ts 是源码模块，
// 每个文件重新求值一次（node_modules 里的 @vue/test-utils 不会），
// 把 afterEach 写在 mount.ts 里只会在第一个 import 它的文件生效。
// setupFiles 由 vitest 对每个测试文件各跑一遍，注册才是稳的。
import { afterEach } from 'vitest'
import { unmountAllTracked } from '@/test/mount'

afterEach(() => {
  unmountAllTracked()
})
