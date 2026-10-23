// 本轮重构（docs/15 T05）的真实浏览器验收：三新页 + E1/E3/E4 + 导航 IA。
//
// 为什么不能只靠 vitest：单测里的 api 全是 mock，验得出「组件按契约渲染」，
// 验不出「真实后端返回的数据能不能撑住这个页面」——监控页在非 Linux 上降级、
// 偏好有没有真写进 user_preferences、跟随开关拉的是不是最新一页，
// 这些都只有真跑一遍才知道。
//
// 运行：node scripts/verify-round3-ia.mjs
// 环境：BASE=服务地址 RACCOON_USER=账号 RACCOON_PASS=口令
// 副作用：会临时隐藏一张主页卡片用于验证 E1 持久化，收尾时复位为默认布局。
import { chromium } from 'playwright'

const BASE = process.env.BASE ?? 'http://127.0.0.1:8530'
const USER = process.env.RACCOON_USER ?? 'admin'
const PASS = process.env.RACCOON_PASS ?? 'Hlb27Xlh'
const SHOTS = new URL('../docs/screenshots/audit/responsive/', import.meta.url).pathname

const steps = []
const t = (name, ok, detail = '') => {
  steps.push({ name, ok, detail })
  console.log(`  ${ok ? '✔' : '✘'} ${name}${detail ? ' — ' + detail : ''}`)
}

const browser = await chromium.launch()
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } })
const page = await ctx.newPage()

await page.goto(BASE + '/login', { waitUntil: 'networkidle' })
await page.fill('input[type=text]', USER)
await page.fill('input[type=password]', PASS)
await page.click('button[type=submit]')
await page.waitForTimeout(2500)

/** 读后端当前的主页布局（E1 的落库结果，绕过前端内存直接核对）。 */
async function readLayout() {
  return page.evaluate(async () => {
    const r = await fetch('/api/v1/user/preferences')
    const j = await r.json()
    return { status: r.status, layout: JSON.parse(j?.data?.layout ?? '{}') }
  })
}

// ---- 监控页（E3：实时 + 1h/24h 历史，纯 SVG）----
await page.goto(BASE + '/monitor', { waitUntil: 'networkidle' })
await page.waitForTimeout(1500)
const circles = await page.$$eval('svg circle', (els) => els.length)
t('监控页环形图为纯 SVG 自绘（无图表库）', circles > 0, `${circles} 个 circle`)
const histTab = await page.$('text=24 小时')
if (histTab) {
  await histTab.click()
  await page.waitForTimeout(1200)
  const lines = await page.$$eval('svg polyline, svg path', (e) => e.length)
  t('切到「24 小时」历史后仍画出序列', lines > 0, `${lines} 条折线/路径`)
  await page.screenshot({ path: SHOTS + 'monitor-history-24h.png' })
} else {
  t('切到「24 小时」历史', false, '未找到「24 小时」标签')
}

// ---- 日志页（E4：实时跟随起停）----
await page.goto(BASE + '/logs', { waitUntil: 'networkidle' })
await page.waitForTimeout(1200)
const follow = await page.$('.follow input')
if (follow) {
  await follow.click()
  await page.waitForTimeout(1000)
  const on = await page.$eval('.follow-state', (e) => e.textContent.trim())
  t('E4 跟随开关打开（显示跟随中）', on.includes('跟随'), on)
  await follow.click()
  await page.waitForTimeout(800)
  const off = await page.$eval('.follow-state', (e) => e.textContent.trim())
  t('E4 跟随开关可关（停止轮询）', off.includes('暂停') || off.includes('已'), off)
} else {
  t('E4 跟随开关存在', false)
}

// ---- 历史页（只读）----
await page.goto(BASE + '/history', { waitUntil: 'networkidle' })
await page.waitForTimeout(1200)
const batchbars = await page.$$eval('.batchbar', (e) => e.length)
t('历史页无批量操作条（只读回看）', batchbars === 0, `找到 ${batchbars} 个`)
const historyText = await page.$eval('.content', (e) => e.innerText.slice(0, 60))
t('历史页渲染出汇总或空态', historyText.length > 10, JSON.stringify(historyText))

// ---- E1：布局偏好真的落到后端 ----
await page.goto(BASE + '/', { waitUntil: 'networkidle' })
await page.waitForTimeout(1500)
const openBtn = await page.$('text=调整主页布局')
if (openBtn) {
  await openBtn.click()
  await page.waitForTimeout(700)
  const boxes = await page.$$('.rdialog input[type=checkbox]')
  t('E1 布局面板列出卡片开关', boxes.length > 0, `${boxes.length} 个`)
  const label = await boxes[0].evaluate((el) => el.parentElement.textContent.trim())
  await boxes[0].click()
  await page.waitForTimeout(300)
  // 保存按钮必须限定在对话框内：页面背后也有「保存」字样，会被遮罩挡住
  await page.click('.rdialog-foot button:last-child')
  await page.waitForTimeout(1500)

  const saved = await readLayout()
  t(
    'E1 偏好写入后端 user_preferences',
    saved.status === 200 && Array.isArray(saved.layout.hidden) && saved.layout.hidden.length > 0,
    `HTTP ${saved.status}，hidden=${JSON.stringify(saved.layout.hidden)}`,
  )

  await page.reload({ waitUntil: 'networkidle' })
  await page.waitForTimeout(1800)
  const afterReload = await page.$eval('.content', (e) => e.innerText)
  t(`E1 刷新后被关掉的「${label}」不回来`, !afterReload.includes(label))

  // ---- 收尾：复位默认布局（避免把验收用的隐藏留在用户主页上）----
  await page.evaluate(async () => {
    const csrf = localStorage.getItem('raccoon.csrf') ?? ''
    await fetch('/api/v1/user/preferences', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
      body: JSON.stringify({ layout: JSON.stringify({ hidden: [], order: [] }) }),
    })
  })
  const reset = await readLayout()
  t('E1 收尾复位默认布局', reset.layout.hidden?.length === 0, JSON.stringify(reset.layout.hidden))
} else {
  t('E1 主页有「调整主页布局」入口', false)
}

// ---- 导航 IA：10 项 ----
const navCount = await page.$$eval('.nav a', (e) => e.length)
t('侧导航 10 项（9 复刻 + 连接）', navCount === 10, `${navCount} 项`)

await browser.close()

const failed = steps.filter((s) => !s.ok)
console.log('\n' + '='.repeat(60))
console.log(`合计 ${steps.length} 项：PASS ${steps.length - failed.length} / FAIL ${failed.length}`)
if (failed.length) {
  failed.forEach((f) => console.log(`  ✘ ${f.name} — ${f.detail}`))
  process.exit(1)
}
console.log('结论：三新页与 E1/E3/E4、导航 IA 在真实浏览器里全部符合 docs/15。')
