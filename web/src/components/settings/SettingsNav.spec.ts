import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import SettingsNav from '@/components/settings/SettingsNav.vue'
import type { SettingsSection } from '@/components/settings/SettingsNav.vue'

/**
 * SettingsNav 用例（docs/07 §3「SettingsNav（左分组）」）。
 *
 * 关键验收点（§8.1 ④ 明确要求这三条都要有用例）：
 * 1) 判定线：滚动时高亮「判定线上方最后一个分组」，方向天然正确。
 * 2) 页面底部兜底：最后一个分组永远越不过判定线，滚到底要手动兜底。
 * 3) 锚点点击锁定：锚点被页面底部截断时判定线选不中被点项，靠锁定兜住。
 *
 * 外加首帧坑：内容还没撑开（scrollHeight=0）时不能误高亮到最后一个分组。
 */
const sections: SettingsSection[] = [
  { id: 'account', label: '账号' },
  { id: 'storage', label: '存储' },
  { id: 'download', label: '下载' },
  { id: 'conn', label: '通道测试' },
  { id: 'tokens', label: 'API 令牌' },
  { id: 'history', label: '配置历史' },
]

/** jsdom 不做布局，getBoundingClientRect 全是 0，这里按 id 打桩。 */
function stubTops(tops: Record<string, number>): void {
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
    const id = this.id
    const top = tops[id] ?? 9999
    return { top, bottom: top + 200, left: 0, right: 0, width: 0, height: 200, x: 0, y: top, toJSON: () => ({}) } as DOMRect
  })
}

/**
 * 挂载导航并造出对应的分组 DOM 元素。
 *
 * 为什么必须造真实元素：scrollspy 靠 document.getElementById 判断分组是否已渲染，
 * jsdom 里只有元素真的在 document 里，导航才能看见它。
 */
function mountNav(sectionsToUse: SettingsSection[] = sections) {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const wrapper = mount(SettingsNav, {
    props: { sections: sectionsToUse },
    attachTo: host,
  })
  return wrapper
}

/** 给分组 id 造一个真实 DOM 元素（scrollspy 的判定对象）。 */
function ensureElements(ids: string[]): void {
  for (const id of ids) {
    if (!document.getElementById(id)) {
      const el = document.createElement('section')
      el.id = id
      document.body.appendChild(el)
    }
  }
}

function activeLabel(w: ReturnType<typeof mountNav>): string {
  return w.find('.side-link.on').text().trim()
}

/** 等一帧，让 rAF 里的判定逻辑跑完。 */
function nextFrame(): Promise<void> {
  return new Promise((r) => setTimeout(r, 40))
}

beforeEach(() => {
  // 默认：不滚到底部（否则会命中「底部兜底」分支）
  Object.defineProperty(document.documentElement, 'scrollHeight', { value: 10000, configurable: true })
  Object.defineProperty(window, 'innerHeight', { value: 900, configurable: true })
  Object.defineProperty(window, 'scrollY', { value: 0, configurable: true })
  window.scrollTo = vi.fn() as any
})

afterEach(() => {
  vi.restoreAllMocks()
  document.body.innerHTML = ''
})

describe('SettingsNav 组件', () => {
  it('按传入顺序渲染分组导航项', () => {
    ensureElements(sections.map((s) => s.id))
    const w = mountNav()
    const labels = w.findAll('.side-link').map((a) => a.text().trim())
    expect(labels).toEqual(['账号', '存储', '下载', '通道测试', 'API 令牌', '配置历史'])
  })

  it('每个导航项都是指向对应分组 id 的锚点', () => {
    ensureElements(sections.map((s) => s.id))
    const w = mountNav()
    const hrefs = w.findAll('.side-link').map((a) => a.attributes('href'))
    expect(hrefs).toEqual(['#account', '#storage', '#download', '#conn', '#tokens', '#history'])
  })

  it('默认高亮第一个分组（账号）', async () => {
    ensureElements(sections.map((s) => s.id))
    stubTops({ account: 100, storage: 500, download: 1100, history: 5800 })
    const w = mountNav()
    await nextFrame()
    expect(activeLabel(w)).toBe('账号')
  })

  it('首帧内容还没撑开（scrollHeight=0）时不能误高亮到最后一个分组', async () => {
    // 实测坑：首帧 documentElement.scrollHeight 还是 0，
    // 「innerHeight + scrollY >= scrollHeight - 2」恒成立 → 误高亮「配置历史」。
    Object.defineProperty(document.documentElement, 'scrollHeight', { value: 0, configurable: true })
    ensureElements(sections.map((s) => s.id))
    stubTops({ account: 100, storage: 500, download: 1100, conn: 5000, tokens: 5200, history: 5800 })
    const w = mountNav()
    await nextFrame()
    expect(activeLabel(w)).toBe('账号')
  })

  it('滚到存储分组时，高亮跟着切到「存储」', async () => {
    ensureElements(sections.map((s) => s.id))
    stubTops({ account: -800, storage: 100, download: 1100, history: 5800 })
    const w = mountNav()
    await nextFrame()
    window.dispatchEvent(new Event('scroll'))
    await nextFrame()
    expect(activeLabel(w)).toBe('存储')
  })

  it('两组之间的空隙保持上一个分组（不会突然没高亮）', async () => {
    ensureElements(sections.map((s) => s.id))
    // 判定线 140px 上方是 storage，下一个分组 download 还在 1100px（远在下方）
    stubTops({ account: -800, storage: 100, download: 1100, history: 5800 })
    const w = mountNav()
    await nextFrame()
    window.dispatchEvent(new Event('scroll'))
    await nextFrame()
    expect(activeLabel(w)).toBe('存储')
  })

  it('滚到底部时兜底高亮最后一个分组（配置历史）', async () => {
    ensureElements(sections.map((s) => s.id))
    stubTops({ account: -5600, storage: -5400, download: -4600, history: 484 })
    // 滚到底：scrollY + innerHeight >= scrollHeight
    Object.defineProperty(window, 'scrollY', { value: 9100, configurable: true })
    const w = mountNav()
    await nextFrame()
    window.dispatchEvent(new Event('scroll'))
    await nextFrame()
    expect(activeLabel(w)).toBe('配置历史')
  })

  it('点击导航项后即使锚点被页面底部截断，高亮也锁在被点项上', async () => {
    // 模拟实测场景：tokens docTop=5564，但页面最大只能滚到 5329，
    // 目标被顶到视口 235px 处，判定线（140px）选不中它 → 必须靠点击锁定兜住。
    ensureElements(sections.map((s) => s.id))
    stubTops({ account: -5174, storage: -4820, download: -4206, conn: 60, tokens: 235, history: 484 })
    Object.defineProperty(window, 'scrollY', { value: 5329, configurable: true })
    const w = mountNav()
    await nextFrame()

    const tokensLink = w.findAll('.side-link').find((a) => a.text().includes('API 令牌'))!
    await tokensLink.trigger('click')
    await nextFrame()

    expect(activeLabel(w)).toBe('API 令牌')
  })

  it('点击时上抛 select 事件（供视图做锚点跳转）', async () => {
    ensureElements(sections.map((s) => s.id))
    const w = mountNav()
    await nextFrame()
    await w.findAll('.side-link')[1].trigger('click')
    expect(w.emitted('select')![0]).toEqual(['storage'])
  })

  it('用户主动滚动后解除点击锁定，高亮重新交给 scrollspy', async () => {
    ensureElements(sections.map((s) => s.id))
    stubTops({ account: -800, storage: 100, download: 1100, conn: 3000, tokens: 3200, history: 3400 })
    const w = mountNav()
    await nextFrame()

    const tokensLink = w.findAll('.side-link').find((a) => a.text().includes('API 令牌'))!
    await tokensLink.trigger('click')
    await nextFrame()
    expect(activeLabel(w)).toBe('API 令牌')

    // 用户滚轮滚动 → 解锁 + scrollspy 接管（此时 storage 在判定线上方）
    window.dispatchEvent(new Event('wheel'))
    await nextFrame()
    expect(activeLabel(w)).toBe('存储')
  })

  it('方向键也算用户主动滚动（同样解锁）', async () => {
    ensureElements(sections.map((s) => s.id))
    stubTops({ account: -800, storage: 100, download: 1100, conn: 3000, tokens: 3200, history: 3400 })
    const w = mountNav()
    await nextFrame()

    const tokensLink = w.findAll('.side-link').find((a) => a.text().includes('API 令牌'))!
    await tokensLink.trigger('click')
    await nextFrame()
    expect(activeLabel(w)).toBe('API 令牌')

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown' }))
    await nextFrame()
    expect(activeLabel(w)).toBe('存储')
  })

  it('点击锁定不会让被点项永久卡住：被点项滚出视口后自动解锁', async () => {
    ensureElements(sections.map((s) => s.id))
    stubTops({ account: -800, storage: 100, download: 1100, conn: 3000, tokens: 3200, history: 3400 })
    const w = mountNav()
    await nextFrame()

    const tokensLink = w.findAll('.side-link').find((a) => a.text().includes('API 令牌'))!
    await tokensLink.trigger('click')
    await nextFrame()
    expect(activeLabel(w)).toBe('API 令牌')

    // tokens 已经滚出视口（top 远小于 0），判定线选中最新的 storage
    stubTops({ account: -900, storage: 60, download: 1100, conn: 3000, tokens: -200, history: 3400 })
    window.dispatchEvent(new Event('scroll'))
    await nextFrame()
    expect(activeLabel(w)).toBe('存储')
  })

  it('分组列表变化后重算高亮（异步加载完成的场景）', async () => {
    ensureElements(sections.map((s) => s.id))
    stubTops({ account: 100, storage: 500, download: 1100, history: 5800 })
    const w = mountNav([{ id: 'account', label: '账号' }])
    await nextFrame()
    expect(activeLabel(w)).toBe('账号')

    // 分组异步加载完 → 列表变化 → 重算
    await w.setProps({ sections })
    await nextFrame()
    expect(w.findAll('.side-link').length).toBe(6)
    expect(activeLabel(w)).toBe('账号')
  })

  it('分组还没渲染到 DOM 时不高亮到看不见的分组', async () => {
    // 只造 account 元素，其余分组尚未渲染（真实场景：配置接口还没回来）
    ensureElements(['account'])
    stubTops({ account: 100, storage: 500, download: 1100, history: 5800 })
    const w = mountNav()
    await nextFrame()
    expect(activeLabel(w)).toBe('账号')
  })
})
