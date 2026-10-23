import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { defineComponent, h, nextTick } from 'vue'

import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { mount } from '@/test/mount'

/**
 * 全站快捷键用例（docs/07 §7）。
 *
 * 断言的是「用户能观测到的结果」：路由跳没跳、焦点落没落到提交框、
 * 页面上下文注册前后行为差异——不测监听器数量这类实现细节。
 *
 * 用真 router（memory history）而不是 vi.mock('vue-router')：mock 会写进
 * 模块缓存，在单 fork 跑全量时污染其它 spec 的 useRouter。
 */
function makeRouter(): Router {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/tasks', component: { template: '<div />' } },
      { path: '/settings', component: { template: '<div />' } },
    ],
  })
}

async function tick() {
  await nextTick()
  await new Promise((r) => setTimeout(r, 0))
}

function key(k: string, init: KeyboardEventInit = {}) {
  document.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, ...init }))
}

describe('useShortcuts / useGlobalShortcuts', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
  })

  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('G D 在任何页面都能跳到下载页（全局键不依赖页面上下文）', async () => {
    const { useGlobalShortcuts } = await import('@/composables/useShortcuts')
    const router = makeRouter()
    const Comp = defineComponent({
      setup() {
        useGlobalShortcuts()
        return () => h('div')
      },
    })
    const w = mount(Comp, { global: { plugins: [router] } })
    await tick()

    key('g')
    key('d')
    await tick()
    expect(router.currentRoute.value.path).toBe('/tasks')
    w.unmount()
  })

  it('G S 跳到设置页', async () => {
    const { useGlobalShortcuts } = await import('@/composables/useShortcuts')
    const router = makeRouter()
    const Comp = defineComponent({
      setup() {
        useGlobalShortcuts()
        return () => h('div')
      },
    })
    const w = mount(Comp, { global: { plugins: [router] } })
    await tick()

    key('g')
    key('s')
    await tick()
    expect(router.currentRoute.value.path).toBe('/settings')
    w.unmount()
  })

  it('没有页面上下文时 `/` 聚焦顶栏快速提交框', async () => {
    const input = document.createElement('input')
    input.id = 'quick-submit'
    document.body.appendChild(input)

    const { useGlobalShortcuts } = await import('@/composables/useShortcuts')
    const router = makeRouter()
    const Comp = defineComponent({
      setup() {
        useGlobalShortcuts()
        return () => h('div')
      },
    })
    const w = mount(Comp, { global: { plugins: [router] } })
    await tick()

    key('/')
    await tick()
    expect(document.activeElement).toBe(input)
    w.unmount()
  })

  it('页面注册上下文后，`/` 走页面自己的提交框、Enter 打开光标行', async () => {
    const { useShortcuts } = await import('@/composables/useShortcuts')
    const focusSubmit = vi.fn()
    const openRow = vi.fn()
    const Comp = defineComponent({
      setup() {
        useShortcuts({
          focusSubmit,
          rows: () => [{ id: 't1', status: 'done' }],
          cursorIndex: { value: 0 },
          openRow,
          toggleSelect: vi.fn(),
          onEsc: () => false,
          retryRow: vi.fn(),
        })
        return () => h('div')
      },
    })
    const w = mount(Comp)
    await tick()

    key('/')
    expect(focusSubmit).toHaveBeenCalled()
    key('Enter')
    expect(openRow).toHaveBeenCalledWith('t1')

    // 卸载后监听一并注销：再按 Enter 不该有反应
    w.unmount()
    await tick()
    openRow.mockClear()
    key('Enter')
    expect(openRow).not.toHaveBeenCalled()
  })

  it('输入框内不劫持快捷键（除 Esc）', async () => {
    const { useShortcuts } = await import('@/composables/useShortcuts')
    const focusSubmit = vi.fn()
    const Comp = defineComponent({
      setup() {
        useShortcuts({
          focusSubmit,
          rows: () => [],
          cursorIndex: { value: 0 },
          openRow: vi.fn(),
          toggleSelect: vi.fn(),
          onEsc: () => false,
          retryRow: vi.fn(),
        })
        return () => h('div')
      },
    })
    const w = mount(Comp, { attachTo: document.body })
    await tick()

    const input = document.createElement('input')
    document.body.appendChild(input)
    input.dispatchEvent(new KeyboardEvent('keydown', { key: '/', bubbles: true }))
    expect(focusSubmit).not.toHaveBeenCalled()
    w.unmount()
  })
})
