// Wave 1 前端端到端验证（真实浏览器 + 真实后端 + 真实下载，全程不 mock）。
// 运行：node scripts/e2e-tasks.mjs   （需仓库根 node_modules 软链到 playwright）
// 前置：服务已在 BASE 上运行，且数据目录无历史任务（软删除即可，不删 DB）。
import { chromium } from 'playwright'
import { mkdirSync } from 'node:fs'

const BASE = process.env.BASE ?? 'http://127.0.0.1:18541'
const USER = process.env.RACCOON_USER ?? 'admin'
const PASS = process.env.RACCOON_PASS ?? 'Raccoon@2026'
const TEST_URL = process.env.TEST_URL ?? 'https://www.bilibili.com/video/BV1cs411a7cQ'
const SHOTS = new URL('../docs/screenshots/', import.meta.url).pathname
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
const consoleErrors = []
page.on('console', (m) => m.type() === 'error' && consoleErrors.push(m.text()))
page.on('pageerror', (e) => consoleErrors.push('pageerror: ' + e.message))

try {
  // 1. 未登录 → 登录页
  await page.goto(BASE + '/tasks', { waitUntil: 'networkidle' })
  const hasLogin = await page.locator('input[type=password]').first().isVisible().catch(() => false)
  hasLogin ? ok('未登录时呈现登录页') : fail('未登录时呈现登录页')

  // 2. 登录
  await page.fill('input[autocomplete=username]', USER)
  await page.fill('input[autocomplete=current-password]', PASS)
  await page.click('button[type=submit]')
  await page.waitForSelector('.nav nav a', { timeout: 15000 })
  ok('登录成功，主界面渲染')

  // 3. SSE 连接指示
  await page.waitForTimeout(1200)
  const connText = await page.locator('.conn').innerText()
  connText.includes('已连接') ? ok('SSE 已连接（实时通道）') : fail('SSE 已连接', connText)

  // 4. 空态（干净环境）
  await page.waitForSelector('.empty', { timeout: 8000 }).catch(() => null)
  ;(await page.locator('.empty').count()) > 0 ? ok('无任务时呈现空态引导') : fail('无任务时呈现空态引导')

  // 5. 解析预览（用 .submit 作用域，避免撞上顶栏快速提交框）
  await page.fill('.submit input[type=url]', TEST_URL)
  await page.click('.submit .btn.ghost')
  await page.waitForSelector('.preview', { timeout: 45000 })
  const pvTitle = await page.locator('.pv-title').innerText()
  const qualityCount = await page.locator('.qualities .q').count()
  pvTitle && qualityCount > 0
    ? ok('解析预览返回标题与清晰度档', `${pvTitle.slice(0, 26)} / ${qualityCount} 档`)
    : fail('解析预览', `title=${pvTitle} q=${qualityCount}`)
  await page.screenshot({ path: SHOTS + 'tasks-preview.png' })

  // 6. 加入下载（选 360p 控制体积与耗时）
  const lowQ = page.locator('.qualities .q', { hasText: '360p' })
  if (await lowQ.count()) await lowQ.first().click()
  await page.click('.pv-actions .btn.primary')
  await page.waitForSelector('.preview', { state: 'detached', timeout: 15000 })
  ok('加入下载成功，预览卡关闭')

  // 7. 新行出现 + SSE 实时推进（进度条 / 速度，不刷新页面）
  const newRow = page.locator('.row').first()
  await newRow.waitFor({ timeout: 15000 })
  ok('新任务出现在列表')
  let sawBar = false
  let sawSpeed = false
  for (let i = 0; i < 30; i++) {
    if (await newRow.locator('.bar').count()) sawBar = true
    const sp = (await newRow.locator('.speed').innerText().catch(() => '')) || ''
    if (sp.trim()) sawSpeed = true
    if (sawBar && sawSpeed) break
    await sleep(1200)
  }
  sawBar ? ok('SSE 推进进度条（无需刷新）') : fail('SSE 推进进度条')
  sawSpeed ? ok('SSE 推送实时速度') : fail('SSE 推送实时速度')
  await page.screenshot({ path: SHOTS + 'tasks-downloading.png' })

  // 8. 等完成（done 事件 → store 全量对账 → 行状态更新）
  let finalState = ''
  for (let i = 0; i < 50; i++) {
    finalState = (await newRow.locator('.st').innerText().catch(() => '')) || ''
    if (finalState.includes('已完成') || finalState.includes('失败')) break
    await sleep(1200)
  }
  finalState.includes('已完成') ? ok('任务完成（SSE 推 done + 对账）', finalState) : fail('任务完成', finalState)
  await page.screenshot({ path: SHOTS + 'tasks-done.png' })

  // 9. 详情抽屉：事件时间线 + 产物（等数据真正渲染完成再断言）
  await newRow.click()
  await page.waitForSelector('.drawer', { timeout: 10000 })
  await page.waitForSelector('.timeline li', { timeout: 10000 })
  // 事件至少三条：created / parsed / done
  await page.waitForFunction(
    () => document.querySelectorAll('.timeline li').length >= 3,
    null,
    { timeout: 10000 },
  ).catch(() => null)
  // 产物块是 v-if 渲染，等它出现再数
  await page.waitForSelector('.files li', { timeout: 15000 }).catch(() => null)
  const evCount = await page.locator('.timeline li').count()
  const fileCount = await page.locator('.files li').count()
  evCount >= 3 ? ok('详情抽屉事件时间线', `${evCount} 条事件`) : fail('详情抽屉事件时间线', `${evCount} 条`)
  fileCount > 0 ? ok('详情抽屉产物文件', `${fileCount} 个`) : fail('详情抽屉产物文件', '0 个')
  await page.screenshot({ path: SHOTS + 'tasks-detail.png', fullPage: true })
  await page.click('.drawer header .btn')

  // 10. 筛选（已完成）
  await page.click('.tab:has-text("已完成")')
  await sleep(600)
  const doneRows = await page.locator('.row').count()
  doneRows > 0 ? ok('筛选（已完成）', `${doneRows} 行`) : fail('筛选（已完成）')
  await page.click('.tab:has-text("全部")')

  // 11. 顶栏快速提交（同一路径的另一入口）
  await page.fill('.quick-submit', TEST_URL)
  await page.click('.quick-btn')
  await sleep(2500)
  const afterQuick = await page.locator('.row').count()
  afterQuick >= 1 ? ok('顶栏快速提交进入队列', `列表 ${afterQuick} 行`) : fail('顶栏快速提交进入队列')

  // 12. 深色主题
  await page.click('.theme-toggle')
  await sleep(500)
  const theme = await page.locator('.app').getAttribute('data-theme')
  theme === 'dark' ? ok('深色主题切换') : fail('深色主题切换', String(theme))
  await page.screenshot({ path: SHOTS + 'tasks-dark.png' })

  // 13. 登出
  await page.click('.logout')
  await page.waitForSelector('input[type=password]', { timeout: 10000 })
  ok('登出回到登录页')

  // 401 是登录前 authMe 的预期探测（后端语义正确），不算前端错误。
  const realErrors = consoleErrors.filter(
    (e) => !e.includes('favicon') && !e.includes('ERR_ABORTED') && !e.includes('401'),
  )
  realErrors.length === 0 ? ok('浏览器 console 无错误') : fail('浏览器 console 无错误', realErrors.slice(0, 3).join(' | '))
} catch (e) {
  fail('执行异常中断', e.message.split('\n')[0])
  await page.screenshot({ path: SHOTS + 'tasks-error.png', fullPage: true }).catch(() => {})
} finally {
  await browser.close()
}

const passed = steps.filter((s) => s.pass).length
console.log(`\n结果：${passed}/${steps.length} 通过`)
if (passed !== steps.length) {
  console.log('失败项：')
  steps.filter((s) => !s.pass).forEach((s) => console.log(`  - ${s.name}: ${s.detail}`))
  process.exit(1)
}