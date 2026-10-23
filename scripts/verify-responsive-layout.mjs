// 前端布局验收（真实浏览器 + 真实后端，全程不 mock）。
//
// 为什么要有这个脚本：design-conformance 闸门查的是「代码里规则写没写对」，
// 查不出「CSS 层叠后规则实际生效没有」。实测翻车案例：
//   .nav-toggle{display:none} 写在媒体查询之后 → 768–1023 档汉堡按钮不显示，
//   900px 下导航彻底打不开，闸门 100 项却全绿。
//
// 本脚本用 getComputedStyle 验「实际生效的 display / 尺寸」，四档逐个断言。
//
// 运行：node scripts/verify-responsive-layout.mjs
// 环境：BASE=服务地址 RACCOON_USER=账号 RACCOON_PASS=口令
import { chromium } from 'playwright'
import { mkdirSync } from 'node:fs'

const BASE = process.env.BASE ?? 'http://127.0.0.1:8530'
const USER = process.env.RACCOON_USER ?? 'admin'
const PASS = process.env.RACCOON_PASS ?? 'Hlb27Xlh'
const SHOTS = new URL('../docs/screenshots/audit/responsive/', import.meta.url).pathname
mkdirSync(SHOTS, { recursive: true })

const steps = []
const ok = (name, detail = '') => {
  steps.push({ name, pass: true, detail })
  console.log(`  ✔ ${name}${detail ? ' — ' + detail : ''}`)
}
const fail = (name, detail = '') => {
  steps.push({ name, pass: false, detail })
  console.log(`  ✘ ${name}${detail ? ' — ' + detail : ''}`)
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

const browser = await chromium.launch()
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } })
const page = await ctx.newPage()

// ---- 登录 ----
await page.goto(BASE + '/login', { waitUntil: 'networkidle' })
await page.fill('input[type=text]', USER)
await page.fill('input[type=password]', PASS)
await page.click('button[type=submit]')
await page.waitForTimeout(2000)

/** 读某元素的实际生效样式。 */
async function probe(sel) {
  return page.evaluate((s) => {
    const el = document.querySelector(s)
    if (!el) return null
    const cs = getComputedStyle(el)
    const r = el.getBoundingClientRect()
    return {
      display: cs.display,
      width: Math.round(r.width),
      height: Math.round(r.height),
      left: Math.round(r.left),
      visible: r.width > 0 && r.height > 0,
    }
  }, sel)
}

// ---- 四档断言（方案 §6）----
const TIERS = [
  { w: 1440, h: 900, name: '≥1280 常驻侧栏', nav: 'block', toggle: 'none', tabbar: 'none' },
  { w: 1100, h: 800, name: '1024–1279 图标栏', nav: 'block', toggle: 'none', tabbar: 'none' },
  { w: 900, h: 800, name: '768–1023 抽屉+汉堡', nav: 'block', toggle: 'flex', tabbar: 'none' },
  { w: 375, h: 780, name: '<768 底部主操作栏', nav: 'none', toggle: 'none', tabbar: 'flex' },
]

console.log('\n【方案 §6 响应式四档】')
for (const t of TIERS) {
  await page.setViewportSize({ width: t.w, height: t.h })
  await page.goto(BASE + '/tasks', { waitUntil: 'networkidle' })
  await page.waitForTimeout(700)

  const nav = await probe('.nav')
  const toggle = await probe('.nav-toggle')
  const tabbar = await probe('.tabbar')

  // 侧导航：≥1024 常驻；<768 隐藏（改用底部栏）；768–1023 是抽屉（display 仍为 flex，靠 transform 收起）
  const navShown = nav && nav.display !== 'none'
  navShown === (t.nav === 'block')
    ? ok(`${t.w}px ${t.name}：侧导航显隐`, `display=${nav.display} w=${nav.width}`)
    : fail(`${t.w}px ${t.name}：侧导航显隐`, `display=${nav?.display}，期望 ${t.nav}`)

  const toggleShown = toggle && toggle.display !== 'none'
  toggleShown === (t.toggle === 'flex')
    ? ok(`${t.w}px ${t.name}：汉堡按钮`, `display=${toggle?.display}`)
    : fail(`${t.w}px ${t.name}：汉堡按钮`, `display=${toggle?.display}，期望 ${t.toggle}`)

  const tabShown = tabbar && tabbar.display !== 'none'
  tabShown === (t.tabbar === 'flex')
    ? ok(`${t.w}px ${t.name}：底部主操作栏`, `display=${tabbar?.display}`)
    : fail(`${t.w}px ${t.name}：底部主操作栏`, `display=${tabbar?.display}，期望 ${t.tabbar}`)

  // 横向溢出：宽屏下「不成比例」最直观的表现
  const ox = await page.evaluate(
    () => document.documentElement.scrollWidth - window.innerWidth,
  )
  ox === 0
    ? ok(`${t.w}px ${t.name}：无横向溢出`)
    : fail(`${t.w}px ${t.name}：无横向溢出`, `溢出 ${ox}px`)
}

// ---- 抽屉开合（768–1023 档必须有真能用的入口）----
console.log('\n【768–1023 抽屉交互】')
await page.setViewportSize({ width: 900, height: 800 })
await page.goto(BASE + '/tasks', { waitUntil: 'networkidle' })
await page.waitForTimeout(700)
const navLeft0 = (await probe('.nav')).left
navLeft0 < 0
  ? ok('抽屉初始收起', `left=${navLeft0}`)
  : fail('抽屉初始收起', `left=${navLeft0}，应为负数`)
await page.click('.nav-toggle')
await page.waitForTimeout(600)
const navLeft1 = (await probe('.nav')).left
navLeft1 === 0
  ? ok('点汉堡抽屉滑出', `left=${navLeft1}`)
  : fail('点汉堡抽屉滑出', `left=${navLeft1}`)
await page.screenshot({ path: SHOTS + 'drawer-open-900.png' })
await page.click('.nav-mask', { position: { x: 700, y: 400 } })
await page.waitForTimeout(600)
const navLeft2 = (await probe('.nav')).left
navLeft2 < 0
  ? ok('点遮罩抽屉收回', `left=${navLeft2}`)
  : fail('点遮罩抽屉收回', `left=${navLeft2}`)

// ---- 底部栏：触控目标 ≥44px + 必须有文字标签 ----
console.log('\n【<768 底部主操作栏】')
await page.setViewportSize({ width: 375, height: 780 })
await page.goto(BASE + '/tasks', { waitUntil: 'networkidle' })
await page.waitForTimeout(700)
const tabs = await page.evaluate(() =>
  Array.from(document.querySelectorAll('.tabbar a')).map((a) => {
    const r = a.getBoundingClientRect()
    return {
      label: a.querySelector('.tabbar-label')?.textContent?.trim() ?? '',
      h: Math.round(r.height),
      w: Math.round(r.width),
    }
  }),
)
tabs.length >= 5
  ? ok('底部栏入口数（5 高频）', `${tabs.length} 项`)
  : fail('底部栏入口数（5 高频）', `${tabs.length} 项`)

// 「更多」面板：10 项导航在小屏不能只剩 5 个（docs/15 §2.3 IA）
await page.click('.tabbar-more')
await page.waitForTimeout(400)
const more = await page.evaluate(() => {
  const sheet = document.querySelector('.more-sheet')
  if (!sheet) return null
  const items = Array.from(sheet.querySelectorAll('.more-item'))
  return {
    n: items.length,
    minH: items.length ? Math.min(...items.map((el) => Math.round(el.getBoundingClientRect().height))) : 0,
  }
})
more && more.n >= 5
  ? ok('「更多」面板补齐低频入口', `${more.n} 项，最小高度 ${more.minH}px`)
  : fail('「更多」面板补齐低频入口', more ? `仅 ${more.n} 项` : '面板没渲染出来')
await page.screenshot({ path: SHOTS + 'more-sheet-375.png' })
await page.keyboard.press('Escape')
await page.waitForTimeout(400)
const moreClosed = await page.evaluate(() => !document.querySelector('.more-sheet'))
moreClosed ? ok('Esc 关闭「更多」面板') : fail('Esc 关闭「更多」面板')
const allLabeled = tabs.every((t) => t.label.length > 0)
allLabeled
  ? ok('底部栏每项都有文字标签', tabs.map((t) => t.label).join('/'))
  : fail('底部栏每项都有文字标签', '存在无标签项')
const allTouch = tabs.every((t) => t.h >= 44)
allTouch
  ? ok('底部栏触控目标 ≥44px', `最小 ${Math.min(...tabs.map((t) => t.h))}px`)
  : fail('底部栏触控目标 ≥44px', `最小 ${Math.min(...tabs.map((t) => t.h))}px`)

// ---- 各页四档截图 ----
console.log('\n【截图】')
const PAGES = [
  { path: '/', name: 'overview' },
  { path: '/tasks', name: 'tasks' },
  { path: '/media', name: 'media' },
  { path: '/subscriptions', name: 'subscriptions' },
  { path: '/live', name: 'live' },
  { path: '/connections', name: 'connections' },
  { path: '/settings', name: 'settings' },
  // 本轮新增三页（docs/15 T05）：历史只读 / 日志 / 监控
  { path: '/history', name: 'history' },
  { path: '/logs', name: 'logs' },
  { path: '/monitor', name: 'monitor' },
]
for (const { w, h } of TIERS) {
  await page.setViewportSize({ width: w, height: h })
  for (const { path, name } of PAGES) {
    await page.goto(BASE + path, { waitUntil: 'networkidle' })
    await page.waitForTimeout(800)
    // 页面不能是静默空白：至少要有可见文字内容
    const textLen = await page.evaluate(
      () => (document.querySelector('.content')?.innerText ?? '').trim().length,
    )
    textLen > 20
      ? ok(`${name}@${w}px 有内容`, `${textLen} 字符`)
      : fail(`${name}@${w}px 有内容`, `仅 ${textLen} 字符，疑似空白页`)
    await page.screenshot({ path: `${SHOTS}${name}-${w}x${h}.png` })
  }
}
ok('四档截图已生成', SHOTS)

await browser.close()

const failed = steps.filter((s) => !s.pass)
console.log('\n' + '='.repeat(60))
console.log(`合计 ${steps.length} 项：PASS ${steps.length - failed.length} / FAIL ${failed.length}`)
if (failed.length) {
  failed.forEach((f) => console.log(`  ✘ ${f.name} — ${f.detail}`))
  process.exit(1)
}
console.log('结论：响应式四档全部符合方案 §6。')
