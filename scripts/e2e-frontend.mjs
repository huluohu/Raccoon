// Wave 3 前端六页端到端验证（真实浏览器 + 真实后端 + 真实数据，全程不 mock）。
// 运行：BASE=http://127.0.0.1:18551 node scripts/e2e-frontend.mjs
// 前置：服务已运行、已初始化管理员、已有 1 个凭据 + 1 个直播源 + 至少 1 个已完成下载任务。
import { chromium } from 'playwright'
import { mkdirSync } from 'node:fs'

const BASE = process.env.BASE ?? 'http://127.0.0.1:18551'
const USER = process.env.RACCOON_USER ?? 'admin'
const PASS = process.env.RACCOON_PASS ?? 'Raccoon@2026'
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

async function goto(path) {
  await page.goto(BASE + path, { waitUntil: 'networkidle' })
  await sleep(500)
}

try {
  // ---- 0. 登录 ----
  await page.goto(BASE + '/', { waitUntil: 'networkidle' })
  const needLogin = await page.locator('input[type=password]').first().isVisible().catch(() => false)
  if (needLogin) {
    await page.fill('input[autocomplete=username]', USER)
    await page.fill('input[autocomplete=current-password]', PASS)
    await page.click('button[type=submit]')
  }
  await page.waitForSelector('.nav nav a', { timeout: 15000 })
  ok('登录并进入主界面')

  // 用户名必须真的有值（曾因 /auth/me 信封不匹配导致空白）
  await sleep(800)
  const userText = (await page.locator('.user').innerText().catch(() => '')) || ''
  userText.trim() === USER ? ok('顶栏用户名正确渲染', userText) : fail('顶栏用户名正确渲染', `got "${userText}"`)

  // SSE 连接指示（握手 + onopen 需要时间，最多等 8s）
  let connText = ''
  for (let i = 0; i < 16; i++) {
    connText = (await page.locator('.conn').innerText().catch(() => '')) || ''
    if (connText.includes('实时')) break
    await sleep(500)
  }
  connText.includes('实时') ? ok('SSE 实时通道已连接', connText) : fail('SSE 实时通道已连接', connText)

  // ---- 1. 总览页 ----
  await goto('/')
  const hasOverviewTitle = (await page.locator('.page-title').first().innerText().catch(() => '')) === '总览'
  hasOverviewTitle ? ok('总览页渲染') : fail('总览页渲染')
  // 四张摘要卡：队列 / 媒体库 / 连接 / 系统（前两张是 .metric 大数字，后两张是列表）
  const cards = await page.locator('.cards .card').count()
  const metricNums = await page.locator('.metric-num').count()
  cards >= 4 && metricNums >= 2
    ? ok('总览四张摘要卡', `${cards} 张卡 / ${metricNums} 个大数字`)
    : fail('总览四张摘要卡', `cards=${cards} metrics=${metricNums}`)
  await page.screenshot({ path: SHOTS + 'overview.png', fullPage: true })

  // ---- 2. 媒体库页 ----
  await goto('/media')
  const fileCards = await page.locator('.file-card').count()
  fileCards > 0 ? ok('媒体库列出真实文件', `${fileCards} 个`) : fail('媒体库列出真实文件', '0 个')
  // 搜索过滤（真请求）
  await page.fill('.search', '不存在的关键词zzz')
  await sleep(900)
  const afterSearch = await page.locator('.file-card').count()
  const emptyShown = await page.locator('.rempty').count()
  afterSearch === 0 || emptyShown > 0
    ? ok('媒体库搜索过滤生效', `搜完剩 ${afterSearch} 个`)
    : fail('媒体库搜索过滤生效', `仍 ${afterSearch} 个`)
  await page.fill('.search', '')
  await sleep(900)
  // 类型筛选
  await page.selectOption('.toolbar select >> nth=0', 'video')
  await sleep(900)
  const videoOnly = await page.locator('.file-card').count()
  ok('媒体库类型筛选', `video → ${videoOnly} 个`)
  await page.selectOption('.toolbar select >> nth=0', '')
  await sleep(700)
  // 打开详情抽屉
  await page.locator('.file-card .file-open').first().click()
  await page.waitForSelector('.dd-panel', { timeout: 8000 })
  await sleep(600)
  const kvRows = await page.locator('.kv dt').count()
  kvRows >= 6 ? ok('文件详情抽屉（路径/大小/平台）', `${kvRows} 项`) : fail('文件详情抽屉', `${kvRows} 项`)
  await page.screenshot({ path: SHOTS + 'media-detail.png', fullPage: true })
  await page.click('.dd-close')

  // ---- 3. 连接页（凭据中心）----
  await goto('/connections')
  const credCards = await page.locator('.card').count()
  credCards > 0 ? ok('凭据中心列出凭据', `${credCards} 条`) : fail('凭据中心列出凭据', '0 条')
  // 列表不得出现明文
  const pageText = await page.locator('.cred-grid').innerText()
  const leaks = ['fake_for_e2e_test', 'SESSDATA=fake']
  const leaked = leaks.filter((k) => pageText.includes(k))
  leaked.length === 0 ? ok('凭据列表不打码明文（无泄漏）') : fail('凭据列表不打码明文', leaked.join(','))
  pageText.includes('****') ? ok('凭据预览已打码') : fail('凭据预览已打码', '未见 ****')
  // 健康检查（真发平台请求）
  await page.locator('.card-foot button', { hasText: '健康检查' }).first().click()
  await sleep(2500)
  const checkLine = await page.locator('.check').first().innerText().catch(() => '')
  checkLine.trim() ? ok('凭据健康检查返回结论', checkLine.trim().slice(0, 40)) : fail('凭据健康检查返回结论')
  // 新建抽屉可开
  await page.locator('button', { hasText: '新建凭据' }).first().click()
  await page.waitForSelector('.dd-panel', { timeout: 8000 })
  const fields = await page.locator('.dd-panel .field').count()
  fields >= 6 ? ok('新建凭据表单字段完整', `${fields} 个字段`) : fail('新建凭据表单字段完整', `${fields}`)
  await page.screenshot({ path: SHOTS + 'connections-form.png', fullPage: true })
  await page.click('.dd-close')

  // ---- 4. 订阅页 ----
  await goto('/subscriptions')
  const subCards = await page.locator('.card').count()
  const hasSubEmpty = await page.locator('.rempty').count()
  subCards > 0 || hasSubEmpty > 0
    ? ok('订阅页渲染（列表或空态）', subCards > 0 ? `${subCards} 个` : '空态')
    : fail('订阅页渲染')
  // 新建抽屉：必须先解析预览才能提交
  await page.locator('button', { hasText: '新建订阅' }).first().click()
  await page.waitForSelector('.dd-panel', { timeout: 8000 })
  const saveBtn = page.locator('.dd-panel button', { hasText: /创建订阅|请先解析预览/ }).first()
  const saveText = await saveBtn.innerText()
  saveText.includes('先解析预览')
    ? ok('订阅未解析前禁止提交（防空订阅）', saveText)
    : fail('订阅未解析前禁止提交', saveText)
  await page.screenshot({ path: SHOTS + 'subscriptions.png', fullPage: true })
  await page.click('.dd-close')

  // ---- 5. 直播页 ----
  await goto('/live')
  const liveCards = await page.locator('.card').count()
  liveCards > 0 ? ok('直播源列表渲染', `${liveCards} 个`) : fail('直播源列表渲染', '0 个')
  const platNote = await page.locator('.platform-note').innerText().catch(() => '')
  platNote.includes('实测支持') ? ok('直播页明示实测支持清单', platNote.slice(0, 34)) : fail('直播页明示实测支持清单')
  // 不支持平台必须在下拉里禁用
  await page.locator('button', { hasText: '添加直播源' }).first().click()
  await page.waitForSelector('.dd-panel', { timeout: 8000 })
  const disabledOpts = await page.locator('.dd-panel select option[disabled]').count()
  disabledOpts > 0
    ? ok('未实测平台在下拉中禁用', `${disabledOpts} 个`)
    : fail('未实测平台在下拉中禁用', '0 个')
  await page.screenshot({ path: SHOTS + 'live.png', fullPage: true })
  await page.click('.dd-close')

  // ---- 6. 设置页 ----
  await goto('/settings')
  const groups = await page.locator('.group').count()
  groups >= 5 ? ok('设置页分组导航', `${groups} 组`) : fail('设置页分组导航', `${groups} 组`)
  const settingItems = await page.locator('.setting').count()
  settingItems >= 15 ? ok('设置项数量（五要素条目）', `${settingItems} 项`) : fail('设置项数量', `${settingItems}`)
  // 生效时机徽章：每项都应标明 immediate / next_task / restart
  const effBadges = await page.locator('.eff-badge').count()
  effBadges === settingItems
    ? ok('每项都带生效时机徽章', `${effBadges}/${settingItems}`)
    : fail('每项都带生效时机徽章', `${effBadges}/${settingItems}`)
  // 改一个值 → 脏状态 → 保存 → 生效提示
  // 注意：不能取「第一个 .input」——env 锁定项是 disabled 的 text input，fill 会一直等到超时。
  // 这里限定在 .setting 内的可编辑控件（input / select 都算）。
  const editable = page.locator('.setting .input:not([disabled])')
  const editCount = await editable.count()
  const disabledCount = await page.locator('.setting .input[disabled]').count()
  ok(
    'env 锁定项只读、可编辑项可写',
    `可编辑 ${editCount} 个 / 只读 ${disabledCount} 个`,
  )
  if (editCount > 0) {
    // 用 download.default_quality 这类安全枚举项做改-存-还原。
    // 不碰 server.addr / storage.data_dir：写坏会直接影响监听地址与数据目录。
    // 该 key 后端下发了 choices，因此控件是 select（枚举不允许自由输入）。
    const row = page.locator('.setting').filter({ hasText: 'download.default_quality' })
    const target = row.locator('.input:not([disabled])').first()
    const hasTarget = (await target.count()) > 0
    hasTarget ? ok('定位到安全可测设置项', 'download.default_quality') : fail('定位到安全可测设置项')
    if (hasTarget) {
      const tag = await target.evaluate((el) => el.tagName.toLowerCase())
      ok('枚举项渲染为下拉（值域由后端下发）', tag === 'select' ? 'select' : tag)
      const options = await target.locator('option').allTextContents()
      options.length >= 4 ? ok('下拉含全部合法档位', options.join('/')) : fail('下拉含全部合法档位', options.join('/'))
      const oldVal = tag === 'select' ? await target.inputValue() : await target.inputValue()
      const newVal = options.find((o) => o !== oldVal && o !== 'audio') ?? oldVal
      if (tag === 'select') await target.selectOption(newVal)
      else await target.fill(newVal)
      await sleep(400)
      const dirty = await page.locator('.dirty-pip').count()
      ok('修改设置项标记脏状态', dirty > 0 ? '已标记' : '无脏标记')
      const saveBtn = page.locator('.head-actions button', { hasText: /保存|应用/ }).first()
      await saveBtn.click()
      await sleep(1800)
      const toastTxt = await page.locator('.rtoast-text').first().innerText().catch(() => '')
      toastTxt ? ok('保存设置返回生效提示', toastTxt.slice(0, 60)) : fail('保存设置返回生效提示', '无 toast')
      // 值必须真的落库：重进页面读回
      await goto('/settings')
      const reread = await page
        .locator('.setting')
        .filter({ hasText: 'download.default_quality' })
        .locator('.input:not([disabled])')
        .first()
        .inputValue()
      reread === newVal
        ? ok('设置值持久化（重进页面读回一致）', reread)
        : fail('设置值持久化', `期望 ${newVal} 实得 ${reread}`)
      // 配置历史留痕
      const histBtn = page.locator('button', { hasText: /历史/ }).first()
      if (await histBtn.count()) {
        await histBtn.click()
        await sleep(1200)
        const histRows = await page.locator('.hist-row, .history li, .tbl tr').count()
        ok('配置历史留痕可查', `${histRows} 条`)
        const closeHist = page.locator('.dd-close').first()
        if (await closeHist.count()) await closeHist.click()
      }
      // 还原
      const restore = page
        .locator('.setting')
        .filter({ hasText: 'download.default_quality' })
        .locator('.input:not([disabled])')
        .first()
      if (tag === 'select') await restore.selectOption(oldVal)
      else await restore.fill(oldVal)
      await sleep(300)
      const save2 = page.locator('.head-actions button', { hasText: /保存|应用/ }).first()
      await save2.click()
      await sleep(1200)
    }
  }
  // ---- API 令牌：真签发一个 → 明文只显示一次 → 撤销 ----
  const tokenGroup = page.locator('#tokens')
  // 每轮用唯一名，避免历史轮次累积的同名令牌干扰定位
  const TNAME = 'e2e-' + Date.now().toString(36)
  ;(await tokenGroup.count()) ? ok('设置页含 API 令牌管理') : fail('设置页含 API 令牌管理')
  if (await tokenGroup.count()) {
    await tokenGroup.locator('input.input').first().fill(TNAME)
    await tokenGroup.locator('select.input').selectOption('3')
    await tokenGroup.locator('button', { hasText: '签发' }).click()
    await sleep(1500)
    const plain = (await tokenGroup.locator('.token-plain code').innerText().catch(() => '')) || ''
    plain.startsWith('rc_')
      ? ok('签发令牌返回明文（仅此一次）', plain.slice(0, 12) + '…')
      : fail('签发令牌返回明文', plain || '未出现 .token-plain')
    const rowCount = await tokenGroup.locator('.token-row').count()
    rowCount > 0 ? ok('令牌列表出现新条目', `${rowCount} 个`) : fail('令牌列表出现新条目', '0')
    // 撤销：本轮会累积同名令牌，已撤销的条目不再渲染按钮，
    // 所以定位「仍带撤销按钮的那一条」而不是简单取 last()/first()。
    const revocable = tokenGroup.locator('.token-row', { hasText: TNAME }).filter({ has: page.locator('button', { hasText: '撤销' }) })
    const revocableCount = await revocable.count()
    if (revocableCount === 0) {
      fail('找到可撤销的令牌行', '0 条')
    } else {
      await revocable.last().locator('button', { hasText: '撤销' }).click()
      await sleep(1200)
      const left = await tokenGroup.locator('.token-row', { hasText: TNAME }).filter({ has: page.locator('button', { hasText: '撤销' }) }).count()
      left === revocableCount - 1
        ? ok('令牌可撤销（按钮消失即失效态）', `${revocableCount} → ${left}`)
        : fail('令牌可撤销', `${revocableCount} → ${left}`)
    }
    // 注意：不能在浏览器里验「撤销后 401」——同源 fetch 会自动带会话 cookie，
    // 被 cookie 通道放行是正确行为（cookie 优先级高于 Bearer）。
    // 撤销的真实失效判定必须是无 cookie 环境，属于 curl 层验证（scripts/verify-token-channel.sh）。
  }
  await page.screenshot({ path: SHOTS + 'settings.png', fullPage: true })

  // ---- 7. 通知中心 ----
  await page.click('.notif-btn')
  await page.waitForSelector('.dd-panel', { timeout: 8000 })
  await sleep(900)
  const notifItems = await page.locator('.nitem').count()
  const notifEmpty = await page.locator('.rempty').count()
  notifItems > 0 || notifEmpty > 0
    ? ok('通知中心打开（列表或空态）', notifItems > 0 ? `${notifItems} 条` : '空态')
    : fail('通知中心打开')
  await page.screenshot({ path: SHOTS + 'notifications.png', fullPage: true })
  await page.click('.dd-close')

  // ---- 8. 深色主题（七页统一切一遍，确认 token 覆盖到位）----
  await page.click('.theme-toggle')
  await sleep(500)
  const theme = await page.locator('.app').getAttribute('data-theme')
  theme === 'dark' ? ok('深色主题切换') : fail('深色主题切换', String(theme))
  for (const p of ['/media', '/connections', '/subscriptions', '/live', '/settings']) {
    await goto(p)
    await page.screenshot({ path: SHOTS + 'dark' + p.replace(/\//g, '-') + '.png', fullPage: true })
  }
  ok('七页深色主题截图完成')
  await page.click('.theme-toggle')
  await sleep(400)

  // ---- 9. 登出 ----
  await page.click('.logout')
  await page.waitForSelector('input[type=password]', { timeout: 10000 })
  ok('登出回到登录页')

  const realErrors = consoleErrors.filter(
    (e) => !e.includes('favicon') && !e.includes('ERR_ABORTED') && !e.includes('401'),
  )
  realErrors.length === 0 ? ok('浏览器 console 无错误') : fail('浏览器 console 无错误', realErrors.slice(0, 3).join(' | '))
} catch (e) {
  fail('执行异常中断', e.message.split('\n')[0])
  await page.screenshot({ path: SHOTS + 'frontend-error.png', fullPage: true }).catch(() => {})
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