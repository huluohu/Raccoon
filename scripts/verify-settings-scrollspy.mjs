// 设置页左分组导航 scrollspy 验收（真实浏览器 + 真实后端）。
//
// 为什么要有这个脚本：设置页 6000px+、14 个分组，scrollspy 有三个必踩的坑，
// 单测里 jsdom 没布局、只能打桩补几何，真实浏览器才能验到端到端行为：
//   ① 判定线越不过最后一个分组 → 滚到底部高亮卡住
//   ② 锚点被页面底部截断 → 点「API 令牌」高亮停在「通道测试」
//   ③ 首帧 scrollHeight=0 → 误高亮到最后一个分组
//
// 运行：node scripts/verify-settings-scrollspy.mjs
// 环境：BASE=服务地址 RACCOON_USER=账号 RACCOON_PASS=口令
import { chromium } from 'playwright'

const BASE = process.env.BASE ?? 'http://127.0.0.1:8530'
const USER = process.env.RACCOON_USER ?? 'admin'
const PASS = process.env.RACCOON_PASS ?? 'Hlb27Xlh'

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

await page.goto(BASE + '/login', { waitUntil: 'networkidle' })
await page.fill('input[type=text]', USER)
await page.fill('input[type=password]', PASS)
await page.click('button[type=submit]')
await page.waitForTimeout(2000)
await page.goto(BASE + '/settings', { waitUntil: 'networkidle' })
await page.waitForTimeout(1500)

const active = () =>
  page.evaluate(() => document.querySelector('.side-link.on')?.textContent?.trim() ?? '(none)')

// ---- ① 首屏必须是第一个分组（不是最后一个）----
console.log('\n【scrollspy 首屏】')
;(await active()) === '账号'
  ? ok('首屏高亮第一个分组', await active())
  : fail('首屏高亮第一个分组', `实际 ${await active()}`)

// ---- ② 向下滚动：逐档切换，不许卡住 ----
console.log('\n【向下滚动】')
const DOWN = [
  { y: 400, expect: '存储' },
  { y: 1400, expect: '下载' },
  { y: 1900, expect: '订阅' },
  { y: 2200, expect: '直播' },
  { y: 3400, expect: '服务' },
]
for (const { y, expect } of DOWN) {
  await page.evaluate((yy) => window.scrollTo(0, yy), y)
  await page.waitForTimeout(350)
  const got = await active()
  got === expect
    ? ok(`scrollY=${y} 高亮「${expect}」`)
    : fail(`scrollY=${y} 高亮「${expect}」`, `实际 ${got}`)
}

// ---- ③ 滚到底部：兜底高亮最后一个 ----
console.log('\n【页面底部兜底】')
const maxY = await page.evaluate(
  () => document.documentElement.scrollHeight - window.innerHeight,
)
await page.evaluate((yy) => window.scrollTo(0, yy), maxY)
await page.waitForTimeout(400)
const bottom = await active()
bottom === '配置历史'
  ? ok('滚到底部高亮最后一个分组', bottom)
  : fail('滚到底部高亮最后一个分组', `实际 ${bottom}`)

// ---- ④ 向上滚动：方向天然正确 ----
console.log('\n【向上滚动】')
for (const { y, expect } of [
  { y: 3000, expect: '日志' },
  { y: 2000, expect: '订阅' },
  { y: 1000, expect: '下载' },
  { y: 0, expect: '账号' },
]) {
  await page.evaluate((yy) => window.scrollTo(0, yy), y)
  await page.waitForTimeout(350)
  const got = await active()
  got === expect
    ? ok(`scrollY=${y} 高亮「${expect}」`)
    : fail(`scrollY=${y} 高亮「${expect}」`, `实际 ${got}`)
}

// ---- ⑤ 点击锚点：目标被页面底部截断时也要高亮准确 ----
console.log('\n【点击导航项】')
for (const label of ['API 令牌', '配置历史', '账号']) {
  await page.evaluate((lbl) => {
    const a = Array.from(document.querySelectorAll('.side-link')).find((x) =>
      x.textContent.includes(lbl),
    )
    a.click()
  }, label)
  await page.waitForTimeout(700)
  const got = await active()
  got === label
    ? ok(`点「${label}」高亮「${label}」`)
    : fail(`点「${label}」高亮「${label}」`, `实际 ${got}`)
}

// ---- ⑥ 点击锁定不能僵死：用户主动滚动后交回 scrollspy ----
console.log('\n【点击锁定可解除】')
await page.evaluate(() => {
  const a = Array.from(document.querySelectorAll('.side-link')).find((x) =>
    x.textContent.includes('API 令牌'),
  )
  a.click()
})
await page.waitForTimeout(600)
;(await active()) === 'API 令牌'
  ? ok('点击后锁定在被点项', await active())
  : fail('点击后锁定在被点项', `实际 ${await active()}`)

// 用户主动滚轮 → 解除锁定，scrollspy 重新接管。
// 注意：wheel 会真的改变 scrollY，预期值必须按滚动后的真实几何算，
// 不能拿滚动前的分组硬套（这坑我踩过一次：以为该是「下载」，实际是「存储」）。
await page.evaluate(() => window.scrollTo(0, 1000))
await page.waitForTimeout(300)
await page.mouse.wheel(0, -200)
await page.waitForTimeout(500)
const afterWheel = await active()
const expected = await page.evaluate(() => {
  // 判定线上方最后一个分组 = 复核 scrollspy 判定线逻辑（140px）
  const ids = [...document.querySelectorAll('.side-link')].map((a) =>
    a.getAttribute('href').slice(1),
  )
  let cur = null
  for (const id of ids) {
    const el = document.getElementById(id)
    if (!el) continue
    if (el.getBoundingClientRect().top <= 140) cur = el
    else break
  }
  return cur?.id ?? '(none)'
})
const expectedLabel = await page.evaluate(
  (id) => document.querySelector(`.side-link[href="#${id}"]`)?.textContent?.trim() ?? '(none)',
  expected,
)
afterWheel === expectedLabel
  ? ok('用户滚动后解锁，高亮回到 scrollspy', afterWheel)
  : fail(
      '用户滚动后解锁，高亮回到 scrollspy',
      `实际 ${afterWheel}，按判定线应为 ${expectedLabel}`,
    )

await browser.close()

const failed = steps.filter((s) => !s.pass)
console.log('\n' + '='.repeat(60))
console.log(`合计 ${steps.length} 项：PASS ${steps.length - failed.length} / FAIL ${failed.length}`)
if (failed.length) {
  failed.forEach((f) => console.log(`  ✘ ${f.name} — ${f.detail}`))
  process.exit(1)
}
console.log('结论：设置页左分组导航 scrollspy 全部符合预期。')
