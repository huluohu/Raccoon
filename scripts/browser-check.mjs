// Raccoon 前端实测：7 个页面 + 本轮关键交互。
// 用法：node scripts/browser-check.mjs [outDir]
import { chromium } from 'playwright'
import { mkdirSync } from 'node:fs'

const BASE = process.env.RC_BASE || 'http://127.0.0.1:8530'
const OUT = process.argv[2] || '/tmp/rc-shots'
const USER = process.env.RC_USER || 'admin'
const PASS = process.env.RC_PASS || 'Hlb27Xlh'
mkdirSync(OUT, { recursive: true })

const problems = []
const note = (m) => console.log('  ' + m)
const fail = (m) => { problems.push(m); console.log('  ✗ ' + m) }
const ok = (m) => console.log('  ✓ ' + m)

const browser = await chromium.launch()
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } })
const page = await ctx.newPage()

const consoleErrors = []
// 只采集登录之后的控制台错误：登录页应用会先探测会话，
// 未认证时返回 401 是正常流程，不是缺陷。
let afterLogin = false
page.on('console', (m) => { if (m.type() === 'error' && afterLogin) consoleErrors.push(m.text()) })
page.on('pageerror', (e) => { if (afterLogin) consoleErrors.push('pageerror: ' + e.message) })

// ---- 登录 ----
console.log('\n[1] 登录页')
await page.goto(BASE + '/login', { waitUntil: 'networkidle' })
await page.screenshot({ path: `${OUT}/01-login.png`, fullPage: true })
if (!(await page.locator('.login-card, form, .auth-card').first().isVisible().catch(() => false))) {
  fail('登录页主卡片不可见')
} else ok('登录页渲染')
await page.fill('input[type="text"], input[name="username"]', USER)
await page.fill('input[type="password"]', PASS)
await page.click('button[type="submit"], .rbtn--accent')
await page.waitForURL((u) => !u.pathname.includes('login'), { timeout: 15000 })
afterLogin = true
ok('登录成功 → ' + new URL(page.url()).pathname)

// ---- 各页面 ----
const pages = [
  ['/', 'overview', '总览'],
  ['/tasks', 'tasks', '任务'],
  ['/media', 'media', '媒体库'],
  ['/subscriptions', 'subs', '订阅'],
  ['/live', 'live', '直播'],
  ['/connections', 'connections', '连接'],
  ['/settings', 'settings', '设置'],
]

console.log('\n[2] 七个页面')
for (const [path, slug, label] of pages) {
  await page.goto(BASE + path, { waitUntil: 'networkidle' })
  await page.waitForTimeout(500)
  await page.screenshot({ path: `${OUT}/page-${slug}.png`, fullPage: true })
  const bodyText = await page.locator('body').innerText()
  const w = await page.evaluate(() => document.documentElement.scrollWidth)
  const vw = await page.evaluate(() => window.innerWidth)
  const hScroll = w > vw + 2
  if (hScroll) fail(`${label} 出现横向滚动 (scrollWidth=${w} > vw=${vw})`)
  else ok(`${label} 页渲染，无横向滚动 (scrollWidth=${w})`)
  // 主内容区不应缩在左上角
  const box = await page.locator('main, .app-main, .shell-main').first().boundingBox().catch(() => null)
  if (box && box.width < vw * 0.5) fail(`${label} 主内容区宽度只占 ${Math.round((box.width / vw) * 100)}%`)
  if (bodyText.trim().length < 20) fail(`${label} 页面内容过少`)
}

// ---- 设置页本轮关键交互 ----
console.log('\n[3] 设置页：分组导航 / 逐项保存反馈 / 诊断入口 / 账号')
await page.goto(BASE + '/settings', { waitUntil: 'networkidle' })
await page.waitForTimeout(600)

// 3.1 左侧分组导航存在
const navLinks = await page.locator('.side-nav .side-link').allInnerTexts()
if (navLinks.length >= 5) ok(`分组导航渲染 ${navLinks.length} 项：${navLinks.join('/')}`)
else fail(`分组导航项数异常：${navLinks.length}`)

// 3.2 点导航项能滚到对应分组
if (navLinks.length) {
  await page.locator('.side-nav .side-link').nth(1).click()
  await page.waitForTimeout(700)
  const active = await page.locator('.side-nav .side-link.on').count()
  if (active >= 1) ok('点击导航项后有激活态（scrollspy 生效）')
  else fail('点击导航项后没有激活态')
}

// 3.3 诊断分组 + 按钮
const diagSec = await page.locator('#diag').count()
if (diagSec === 1) ok('诊断分组存在')
else fail('诊断分组 #diag 不存在')
const diagBtn = await page.locator('#diag button', { hasText: /导出|诊断/ }).count()
if (diagBtn >= 1) ok('诊断区有操作入口')
else fail('诊断区没有操作入口')

// 3.4 账号分组
const accSec = await page.locator('#account').count()
if (accSec === 1) ok('账号分组存在')
else fail('账号分组 #account 不存在')

// 3.5 逐项保存反馈：改一个开关型配置 → 点页头「保存改动」→ 看该项是否出现「已保存 · …」
console.log('\n[4] 设置页：逐项保存反馈')
const toggles = page.locator('.setting input[type="checkbox"]')
const tCount = await toggles.count()
if (tCount > 0) {
  const before = await page.locator('.saved-badge').count()
  // 逐个 setting 块找第一个带 checkbox 的，记下它的 key
  const rows = page.locator('.setting')
  const n = await rows.count()
  let rowIdx = -1
  for (let i = 0; i < n; i++) {
    if ((await rows.nth(i).locator('input[type="checkbox"]').count()) > 0) { rowIdx = i; break }
  }
  if (rowIdx < 0) { note('没有带 checkbox 的配置项，跳过'); }
  const firstRow = rows.nth(rowIdx)
  const key = (await firstRow.locator('.setting-key').innerText()).trim()
  await toggles.first().click()
  await page.waitForTimeout(300)

  // 改动后应出现页头「未保存」标记
  const dirtyMark = await page.locator('.dirty-dot').count()
  if (dirtyMark >= 1) ok('改动后页头出现「未保存」标记')
  else fail('改动后没有「未保存」标记')
  // 改动后该项应出现脏圆点
  if ((await firstRow.locator('.dirty-pip').count()) >= 1) ok('改动后该项出现脏圆点')
  else fail('改动后该项没有脏圆点')

  // 点页头「保存改动」
  const saveBtn = page.locator('button', { hasText: /保存改动/ }).first()
  if ((await saveBtn.count()) === 0) fail('找不到「保存改动」按钮')
  else {
    await saveBtn.click()
    await page.waitForTimeout(1800)
    const after = await page.locator('.saved-badge').count()
    const badgeTexts = await page.locator('.saved-badge').allInnerTexts()
    if (after > before) ok(`保存后出现逐项保存反馈徽章（${before} → ${after}）：${badgeTexts.join(' / ')}`)
    else fail('保存后没有出现逐项保存反馈徽章')
    // 反馈应挂在被改的那一项上
    let hit = 0
    for (let i = 0; i < n; i++) {
      const k = (await rows.nth(i).locator('.setting-key').innerText()).trim()
      if (k === key) { hit = await rows.nth(i).locator('.saved-badge').count(); break }
    }
    if (hit >= 1) ok(`保存反馈挂在了被改的项「${key}」上`)
    else fail(`保存反馈没有挂在被改的项「${key}」上`)
    await page.screenshot({ path: `${OUT}/03-settings-saved.png`, fullPage: true })
  }
} else {
  note('没有找到开关型配置项，跳过保存反馈检查')
}

// 3.6 生效时机徽章
const badges = await page.locator('.eff-badge').allInnerTexts()
if (badges.length) ok(`生效时机徽章渲染 ${badges.length} 个：${[...new Set(badges)].join('/')}`)
else fail('没有生效时机徽章')

// ---- 提交前的链接格式闸（本轮新增）----
console.log('\n[5] 链接格式闸')
await page.goto(BASE + '/tasks', { waitUntil: 'networkidle' })
await page.waitForTimeout(500)
const urlInput = page.locator('#task-url-input')
if ((await urlInput.count()) === 0) fail('找不到下载页的 URL 输入框')
else {
  // 5.1 缺协议头 → 就地提示，不发请求
  await urlInput.fill('www.bilibili.com/video/BV1xx')
  await page.waitForTimeout(300)
  const errText = await page.locator('.qsubmit-err').innerText().catch(() => '')
  if (errText.includes('http://')) ok(`缺协议头就地提示：「${errText.trim()}」`)
  else fail(`缺协议头没有提示（实际显示：${errText || '无'}）`)

  // 5.2 分享文案 → 能提取出链接并正常提交（不卡在格式闸）
  await urlInput.fill('【测试视频】https://b23.tv/abc123 复制打开bilibili')
  await page.waitForTimeout(300)
  const errText2 = await page.locator('.qsubmit-err').innerText().catch(() => '')
  if (!errText2.includes('http://')) ok('分享文案不误报格式错误')
  else fail(`分享文案被误判：${errText2}`)

  // 5.3 提交分享文案 → 请求里带的是提取后的干净链接
  const reqs = []
  const onReq = (r) => { if (r.url().includes('/api/v1/parse')) reqs.push(r.postData() || '') }
  page.on('request', onReq)
  await page.locator('button', { hasText: '解析预览' }).first().click()
  await page.waitForTimeout(2500)
  page.off('request', onReq)
  if (reqs.length && reqs[0].includes('https://b23.tv/abc123') && !reqs[0].includes('复制打开')) {
    ok('提交的是提取后的干净链接（分享文案没混进请求）')
  } else {
    fail('分享文案的提交内容不对：' + (reqs[0] || '（没有发出请求）').slice(0, 160))
  }
}

// ---- 任务页本轮修复点 ----
console.log('\n[6] 任务页：筛选 / 批量工具条 / 空态')
await page.goto(BASE + '/tasks', { waitUntil: 'networkidle' })
await page.waitForTimeout(600)
await page.screenshot({ path: `${OUT}/04-tasks.png`, fullPage: true })
const tabs = await page.locator('.tasklist .tab').allInnerTexts()
if (tabs.length) ok(`筛选工具条渲染：${tabs.map((t) => t.replace(/\s+/g, ' ').trim()).join(' | ')}`)
else fail('筛选工具条没有渲染')
const batchbar = await page.locator('.tasklist .batchbar').count()
if (batchbar === 0) ok('未选中时不显示批量工具条（不占常态空间）')
else note('批量工具条已显示')

// ---- 任务详情：引擎原文 ----
console.log('\n[7] 任务详情：引擎原文')
const failedTab = page.locator('.tasklist .tab', { hasText: '失败' }).first()
if ((await failedTab.count()) > 0) {
  await failedTab.click()
  await page.waitForTimeout(600)
  const failedRows = page.locator('.tasklist .trow, .tasklist .tcard')
  const cnt = await failedRows.count()
  if (cnt > 0) {
    await failedRows.first().click()
    await page.waitForTimeout(1200)
    const drawer = page.locator('.tdetail')
    if ((await drawer.count()) > 0) {
      const hasDetail = await drawer.locator('.td-engine-detail').count()
      const detailText = hasDetail ? await drawer.locator('.td-engine-detail').innerText() : ''
      if (hasDetail > 0) ok(`错误区展示引擎原文：「${detailText.trim().slice(0, 80)}」`)
      else {
        note('该失败任务没有引擎原文事件（后端未写入）')
        const reason = await drawer.locator('.td-kv').innerText().catch(() => '')
        if (reason.includes('原因')) ok('错误区有「原因」行（分类文案）')
      }
      await page.screenshot({ path: `${OUT}/06-task-detail.png`, fullPage: true })
      await page.keyboard.press('Escape')
      await page.waitForTimeout(400)
    } else fail('任务详情抽屉没有打开')
  } else note('当前没有失败任务，跳过引擎原文实测（该验收点由 TaskDetail 单测覆盖）')
} else fail('没有「失败」筛选标签')

// ---- 响应式四档 ----
console.log('\n[8] 响应式四档')
const widths = [[1920, 'xl'], [1440, 'lg'], [1024, 'md'], [390, 'sm']]
for (const [w, tag] of widths) {
  await page.setViewportSize({ width: w, height: 900 })
  await page.goto(BASE + '/tasks', { waitUntil: 'networkidle' })
  await page.waitForTimeout(400)
  const sw = await page.evaluate(() => document.documentElement.scrollWidth)
  const vw = await page.evaluate(() => window.innerWidth)
  if (sw > vw + 2) fail(`${tag} ${w}px 横向滚动 (${sw} > ${vw})`)
  else ok(`${tag} ${w}px 无横向滚动`)
  await page.screenshot({ path: `${OUT}/05-responsive-${tag}-${w}.png`, fullPage: true })
}

console.log('\n[9] 控制台错误')
const realErrors = consoleErrors.filter((e) => !/favicon|ResizeObserver|DevTools/i.test(e))
if (realErrors.length) {
  realErrors.slice(0, 10).forEach((e) => fail('控制台错误: ' + e.slice(0, 200)))
} else ok('无控制台错误')

await browser.close()

console.log('\n========================================')
if (problems.length) {
  console.log(`发现 ${problems.length} 个问题：`)
  problems.forEach((p, i) => console.log(`${i + 1}. ${p}`))
  process.exit(1)
}
console.log('全部检查通过')
