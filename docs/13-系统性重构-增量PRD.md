# 13 · 系统性重构 · 增量 PRD

> 项目：Raccoon（浣熊）· 自托管多媒体下载中心 ｜ 作者：产品经理（software-product-manager）
> 性质：增量 PRD，只描述「重构变更部分」，不重写整体需求。输入：docs/01–10 + 主理人前置审计疑点清单。
> 纪律：所有结论均以亲自读代码 / 跑命令为准；证据不足处标「待确认」，不编造。
> 日期：2026-10-23 ｜ 代码基线：commit 0cec068（HEAD）

---

## 1. 审计结论摘要

### 1.1 质量门禁复跑结果（本人亲自执行，非引用）

| 门禁 | 命令 | 结果 |
| --- | --- | --- |
| 设计符合度闸门 | `python3 scripts/check-design-conformance.py` | **228/228 PASS，exit 0** |
| 前端构建 | `cd web && npm run build` | **exit 0**（built in 3.54s） |
| 前端单测 | `cd web && npx vitest run` | **389/389 通过（41 个文件），exit 0** |
| 后端构建/静态检查/单测 | `cd backend && go build -buildvcs=false ./... && go vet ... && go test -count=1 ./...` | **exit 0，9/9 包 ok** |

注：本环境 `go` 不在默认 PATH，位于 `/usr/local/go/bin/go`（主理人复跑时注意）。

### 1.2 现状达标面（有证据，简要）

- **docs/07 前端宪法大面积兑现**：9 条路由齐备（router/index.ts:9-17）；22 个视图级组件真实落地且被视图引用（闸门 + 抽查 QuickSubmit/StatusStrip/SubForm/SettingItem 等）；**浣熊六表情齐备且按状态真实取用**（RaccoonFace.vue:14-29 六态；REmpty 空态、NotifCenter 通知映射、ActiveTasks「正在搬的活/活都搬完了」、TaskDetail 失败/取消、ItemsTable surprise 追更）；动效四档 + prefers-reduced-motion + 禁用项落实（motion.css:16-75）；Token 唯一色值来源、无硬编码色值/字号（闸门 228 项）；响应式四档含 CSS 层叠顺序修正与 tabbar 文字标签（App.vue:337-440）。
- **docs/01 行为契约大部分兑现**（逐条找证据）：① 任意入口同队列（tgbot → task.Manager，backend/internal/tgbot/handler.go:45-48）；② 失败真实原因 + 修复建议 + Cookie 过期一键跳连接页（TaskDetail.vue:82-87,327-329）；③ 大小未知显「未知」、无假暂停按钮（format.ts:5；全仓无任务暂停按钮）；④ 父卡整体进度 + 失败项一键重试（TaskCard.vue:158-163 + Tasks.vue retryFailedChildren）；⑥ 设置项生效时机徽章 + 脏圆点 + 保存反馈（SettingItem.vue:76-87）；⑦ env 锁定只读且标注来源变量名（SettingItem.vue:77-84,91-103）；⑧ qB/115 不可用仅相关任务失败（external_test.go:309 TestRunQB_不影响其他任务）；⑩ 375px 关键按钮不遮挡（App.vue:394-440 安全区 + 截图）；⑪ 重启恢复（docs/09 §2.4 kill -9 实测 + cmd 恢复用例）。订阅先预览后创建（SubForm.vue:92 canSave 必须 preview≠null）；直播平台实测清单 422（live_handler.go:67-69 + RecordPlanForm 禁用标注「未实测，不支持」）；连接页四态诚实表达「不假装可用」（Connections.vue:84-114）；Toast 四级/4s/错误常驻/同屏≤3（toast.ts:23-31）；RDialog 默认焦点「取消」（RDialog.vue:125-129）；保存结果按 effective_summary 展示（Settings.vue:190 + config_handler.go:157）。

### 1.3 差距面（一屏读完）

- **P0 × 2**：① 删除契约破灭——「删除文件」操作前端零入口，且删除任务对话框虚假承诺「及其本地文件」（实际只删记录）；② 存储策略路由（本地/115 优先/失败转 qB）整体未实现，「任务详情显示走了哪条路」缺失，而 docs/10 却将 4.7 标记为已交付。
- **P1 × 2**：③ 快捷键仅 /tasks 一页注册，docs/07 §3 全局条款「`/` 聚焦 QuickSubmit」在其余六页落空（闸门存在性断言查不到，属 docs/07 §8.1 ① 警告的盲区）；④ 验收截图早于三个前端提交（含视觉方向级重构 e46f1cb），当前界面无留证。
- **P2 × 6**：API 契约三处不一致（auth/status 方法、counts 来源、parent 端点）+ docs/04 六个已实现端点未登记；post 阶段无独立进度；暂停无界面说明；深色模式封面/缩略图缺 1px 边框（docs/07 §2 明示规则）；文档笔误（docs/07 §2 `--surface-2:#EEEF2` 非法 hex）。

---

## 2. 差距清单（按 功能 / UI / 交互 / 视觉 分组）

严重度定义：P0 = 违背 docs/01 验收口径/行为契约；P1 = 设计落地打折或验证缺口；P2 = 打磨项。

### 2.1 功能类

#### F1【P0】删除文件操作无入口 + 删除对话框虚假承诺（违背 docs/01 §5.5、§3.5；docs/04 §2.2）

- **契约**：docs/01 §5.5「删除任务记录与删除文件是两个操作，均二次确认」；§3.5 删除行「两个独立操作，均二次确认；明示影响范围（§9.5）」；docs/04 §2.2 `DELETE /tasks/{id}/files/{fileId}`「删除实际文件 + 索引（独立危险操作）」。
- **证据**：
  - 后端两端点都在：`DELETE /tasks/{id}`（backend/internal/server/task_handler.go:54）、`DELETE /tasks/{id}/files/{fileId}`（task_handler.go:61）。
  - 前端只有删记录：web/src/api/client.ts:147 仅 `deleteTask`，无 `deleteTaskFile` 封装（全仓 grep 零命中）；web/src/views/Media.vue 无任何删除入口（grep「删除/delete」零命中）。
  - 虚假承诺：web/src/views/Tasks.vue:664 对话框文案「将删除『xxx』**及其本地文件**，此操作不可撤销」；批量删除同样文案（Tasks.vue:652）。但 confirmDelete → tasks.remove（stores/tasks.ts:279-282）→ `DELETE /tasks/{id}`，后端 handleTaskDelete 只做 SoftDelete（task_handler.go:207-217），**不删文件、也不识别 `?delete_files=1`**。用户确认后文件仍在磁盘，与承诺相反。
- **建议动作**：见需求池 R1/R2（含待确认 Q1）。

#### F2【P0】存储策略路由整体缺失，「走了哪条路」不可见（违背 docs/01 §3.4；docs/08 §4.7 未交付却标记已交付）

- **契约**：docs/01 §3.4 存储策略行「本地下载 / 115 优先 / 失败转 qB｜按策略路由｜策略链路全失败 → 任务失败并保留上下文｜**策略可配置、可解释（任务详情显示走了哪条路）**」；docs/08 §4.7「115 离线调度（提交-查询闭环 + 策略路由）」。
- **证据**：
  - 配置中心无策略项：backend/internal/config/config.go:31-65 全量 Registry 无 `storage.strategy` 或类似键。
  - 路由只有一条硬规则：backend/internal/task/manager.go:341-358「显式 kind 或 magnet → qb，其余 → download；n115 只能显式指定」，无「本地/115 优先/失败转 qB」三级策略，无失败转移逻辑。
  - 任务详情无策略展示：web/src/components/task/TaskDetail.vue 概览区仅有 save_path（:258-261），无「走了哪条路」字段。
  - 交付记录不实：docs/10 §1 将 4.7 标注「✅ 已交付（开放平台未实测）」，未提策略路由缺失。
- **建议动作**：见需求池 R3/R4（含待确认 Q2：本轮实施 or 显式降级并修订文档）。

#### F3【P2】API 契约与实现三处不一致（docs/04 §2.1 / §2.2）

- **证据**：
  - `GET /auth/status`（docs/04 §2.1）vs 实现 `POST /api/v1/auth/status`（backend/internal/server/auth_handler.go:36），前端也按 POST 调用（web/src/api/client.ts:92）——契约文档与实现不一致（主理人疑点 1 属实）。
  - docs/04 §2.2 称 `GET /tasks`「附 `counts` 各状态计数（总览页用）」，实现 handleTasksList 直接返回 ListTasks 结果、不含 counts（task_handler.go:72-91）；实际 counts 由**未登记端点** `GET /system/queue` 提供（system_handler.go:25；Overview.vue 用它）。
  - docs/04 §2.2 称 `POST /tasks` body 含 `parent_id?`/`options?`，实现 createTasksReq 无这两字段（task_handler.go:95-104）；父任务走**未登记端点** `POST /tasks/parent`（task_handler.go:59）。
- **建议动作**：见需求池 R8。修订方向见待确认 Q6（auth/status 改文档还是改实现）。

#### F4【P2】docs/04 §2 路由总表缺登记 6 个已实现端点

- **证据**（router 注册核对）：`POST /api/v1/tasks/parent`（task_handler.go:59）、`GET /api/v1/system/queue`（system_handler.go:25）、`GET/POST /api/v1/tokens` + `DELETE /api/v1/tokens/:id`（token_handler.go:21-23）、`GET /api/v1/integrations/status` + `POST /integrations/qb/test` + `POST /integrations/n115/test` + `GET /integrations/telegram/status`（integration_handler.go:45-48）——以上在 docs/04 §2 路由总表中均无条目。
- **建议动作**：R8 一并补登记（含方法、认证、用途），保持「契约即文件」。

#### F5【P2】post（后处理）阶段无独立进度事件

- **契约**：docs/01 §3.5「产出文件 + 后处理阶段进度」；§4 状态机「后处理 → 三色进度条映射」。
- **证据**：backend/internal/task/worker.go:154-161 在 downloading→post→done 迁移期间**没有广播 task.progress**（progress 广播只在 downloading 分支，worker.go:105-109）；前端 TaskProgress 在 stage=post 时 pct 取 task.progress 最后值（TaskProgress.vue:46-49,28-39）。三色映射与状态切换正确，仅进度数值停在下载完成值。
- **建议动作**：R9（后端补广播 vs 前端切条纹，二选一，见待确认 Q4）。

#### F6【P2】文档笔误与交付记录偏差

- **证据**：docs/07 §2 `--surface-2:#EEEF2` 为非法 5 位 hex，实现为 `#EEF0F7`（web/src/styles/tokens.css:22）；docs/10 §1 将 4.7「策略路由」随 115 交付一并标记已交付（实际未实现，见 F2）。
- **建议动作**：R12 修订笔误；R4 修订 docs/10 §1（与 F2 决策联动）。

### 2.2 UI 类

#### U1【P1】验收截图滞后于代码三个提交，当前视觉无留证

- **契约/背景**：docs/07 §8.1「断言要落在可观测结果上」；主理人审计输入称四档截图留档于 docs/screenshots/audit/。
- **证据**：截图文件时间为 2026-10-23 00:02；其后前端有三个提交：5c836dd（00:53 响应式整改）、9a2c6e8（10:22 构图修复）、e46f1cb（11:08 视觉方向级重构：深底侧导航/标题归属顶栏/金色主 CTA）。抽查 tasks-1440.png 仍是**旧版式**（浅色侧栏 + 内容区内 h1「下载」金条），与现行代码（顶栏 topbar-title、深底 nav）不符；tasks-900.png 中未见 768–1023 档汉堡按钮（旧截图早于响应式整改，不能作为现网证据）。
- **建议动作**：R6 按现行构建重截并附走查记录。

### 2.3 交互类

#### I1【P1】快捷键仅 /tasks 一页注册，全局 `/` 聚焦落空（违背 docs/07 §3 全局条款）

- **契约**：docs/07 §3「全局：顶部 QuickSubmit（`/` 快捷键聚焦）」；§7 快捷键清单（`/`、Esc、↑↓、Enter、Shift+Space、R、G D/G S）。
- **证据**：web/src/composables/useShortcuts.ts 全仓仅被 web/src/views/Tasks.vue:417 一处引用（grep 全仓仅此一处）；App.vue 外壳只注册了 Esc 关抽屉（App.vue:60-62 onNavKey）。在 /media、/subscriptions、/live、/connections、/settings、/ 六个页面按 `/` 无任何反应。闸门 check_shortcuts 只查 useShortcuts.ts 内部字符串存在（scripts/check-design-conformance.py:775-788），**查不到「仅一页注册」**——正是 docs/07 §8.1 ① 警告的「存在性 ≠ 组装成产品」盲区。
- **影响**：docs/01 §5.9「键盘可完成添加→详情→重试全流程」在 /tasks 内成立，但跨页键盘导航（G D/G S）与全局 `/` 承诺落空。
- **建议动作**：R5 + R7（闸门补断言防回归）。

#### I2【P2】暂停/续传「界面说明原因」未兑现

- **契约**：docs/01 §3.1「暂停/续传：仅引擎支持时提供；不支持 → 界面说明原因，不显示暂停按钮；如实标注，无假按钮」。
- **证据**：全前端无任务暂停按钮（grep「暂停/pause」仅命中订阅暂停 SubList.vue:108，与任务暂停无关）；后端无 pause 端点（router 注册清单无 /pause）。「无假按钮」达标；但「界面说明原因」未兑现——用户看不到为什么没有暂停，只能猜。
- **建议动作**：R10。

### 2.4 视觉类

#### V1【P2】深色模式封面/缩略图缺 1px 边框（违背 docs/07 §2 明示规则）

- **契约**：docs/07 §2 规则「深色模式封面/缩略图加 1px 边框防『浮空』」。
- **证据**：缩略图元素均无深色模式边框——TaskCard .tcard-thumb（TaskCard.vue:233-239）、TaskRow .trow-thumb（TaskRow.vue:209-216）、FileCard .file-thumb-img img（FileCard.vue:155-156）、PreviewDrawer img/video（PreviewDrawer.vue:78-81）；tokens.css 深色模式块（:100-149）无针对缩略图的边框规则。深色模式下深色封面贴在 --surface-2（#1D2231）上无沿，边缘「浮空」。闸门 228 项未覆盖此规则。
- **建议动作**：R11。

#### V2 视觉层其余部分

在闸门 228 项（Token/字阶语义/禁硬编码/网格塌陷/emoji/动效/响应式/构图/视觉方向）覆盖下**未发现其他可验证差距**；主理人疑点 2（六表情）、3（动效四档）、5（路由/组件映射）复查均属实落地。唯一系统性风险是 U1（截图滞后）导致视觉层缺当前构建的留证。

---

## 3. 重构需求池（P0/P1/P2，验收标准全部可观测）

> 对齐 docs/07 §8.1 教训：不写「已优化」这类不可验收条目；每条落到可见结果 / 可执行断言 / 命令退出码。

### P0（违背契约，必修）

**R1 删除文件操作落地**（对应 F1）
- 范围：前端入口（媒体库 FileCard 操作区 + 任务详情产物区各一处）+ api 层补 `deleteTaskFile`（`DELETE /tasks/{id}/files/{fileId}`，后端已就绪，无需改后端）。
- 验收标准：
  1. 媒体库文件卡与任务详情产物列表均出现「删除文件」按钮；
  2. 点击弹出 RDialog（默认焦点「取消」），展示文件名 + 大小 + 完整路径，明示「只删这个文件，任务记录保留」；
  3. 确认后文件从磁盘与媒体库索引消失，列表/计数即时刷新；取消则不发出任何请求；
  4. vitest 新增用例覆盖「打开对话框 / 确认删除 / 取消」三路径（@vue/test-utils + fetch mock）。

**R2 删除任务记录对话框文案修正**（对应 F1）
- 范围：web/src/views/Tasks.vue:652,664 单条与批量删除文案。
- 验收标准：两处文案不再出现「及其本地文件」；改为「仅删除任务记录，已下载文件保留在媒体库（可在媒体库单独删除文件）」；Tasks.spec.ts 断言文案包含「仅删除任务记录」。
- 备注：若待确认 Q1 选择「支持连同文件一起删」，则本项改为：对话框提供「同时删除本地文件」勾选 + 前端显式传 `?delete_files=1` + 后端实现该参数，文案与行为一致。

**R3 存储策略可配置 + 可解释**（对应 F2，范围依赖待确认 Q2）
- 范围（若本轮实施）：配置中心新增 `storage.strategy`（三值：local / n115_first / qb_fallback，五要素齐全：key/desc/default/effective/sensitive）；任务提交按策略路由，115 优先失败可转 qB（或按所选策略）；任务详情概览区新增「走了哪条路」行（如「115 优先 → 115 提交成功」/「115 失败 → 转 qB」）；策略链路全失败 → 任务 failed 且事件保留上下文。
- 验收标准：
  1. `GET /config` 返回含 `storage.strategy` 项且五要素齐全；env 锁定行为与其他项一致；
  2. 磁力/离线任务按所选策略路由（单测覆盖三值 × 成功/失败转移矩阵）；
  3. 任务详情渲染「走了哪条路」，文案与任务实际执行路径一致（TaskDetail.spec.ts 断言）；
  4. 设置页可改该项并有生效时机徽章。

**R4 修订 docs/10 §1 交付记录**（对应 F2/F6）
- 验收标准：docs/10 §1 的 4.7 行补注「策略路由未交付（见 docs/13）」；或在 R3 实施后更新为已交付并附证据章节号。二选一，不留模糊表述。

### P1（设计落地打折 / 验证缺口）

**R5 快捷键全局化**（对应 I1）
- 范围：把 `/` 聚焦与 `G D`/`G S` 跳转注册上移到 App.vue 外壳（useShortcuts 增加「仅全局键」用法或拆一个 useGlobalShortcuts）；Tasks 页保留行操作类快捷键（↑↓/Enter/Shift+Space/R/Esc），`/` 优先聚焦本页输入框的逻辑不变。
- 验收标准：
  1. 在 /、/media、/subscriptions、/live、/connections、/settings 六页按 `/`，`document.activeElement.id === 'quick-submit'`（App 级 vitest 用例 ×6 或参数化 ×1）；
  2. 六页按 `G D`/`G S` 均跳转 /tasks、/settings；
  3. 输入框内（除 Esc）快捷键不触发的既有行为不回归（现有用例保持绿）。

**R6 四档截图重截 + 走查记录**（对应 U1）
- 范围：按现行构建重截七页 × 四档（375/900/1100/1440）+ 深色模式，存档 docs/screenshots/audit/，附走查记录。
- 验收标准：截图文件 mtime 晚于最后前端提交；走查记录逐条记：375px 关键按钮不被 tabbar 遮挡、900px 汉堡按钮可见且点击能开抽屉、1100/1440 无破版、深色模式封面有边框（R11 后）。

**R7 闸门补两条断言**（对应 I1/F1 防回归）
- 范围：scripts/check-design-conformance.py。
- 验收标准：
  1. 新增断言「useShortcuts 被 App.vue 引用」（全局注册，防再次退化成单页）；
  2. 新增断言「client.ts 存在 deleteTaskFile 且被 Media.vue 或 TaskDetail.vue 引用」；
  3. 闸门总数 228 → 230+，重跑 exit 0。

### P2（打磨项）

**R8 API 契约对齐**（对应 F3/F4）
- 范围：docs/04 §2 修订 +（若 Q6 决定）auth_handler.go 方法调整。
- 验收标准：
  1. docs/04 §2.1 `auth/status` 的方法与 router 注册逐字一致（auth_handler.go:36）；
  2. docs/04 §2 路由总表补齐 6 个已实现端点（tasks/parent、system/queue、tokens ×3、integrations ×4 中的实际集合），每条含方法/路径/认证/用途；
  3. counts 的权威来源在 docs/04 中写明（`GET /system/queue` 或改为随 `/tasks` 返回，二选一并同步实现/文档）；
  4. 修订后跑一遍「docs/04 §2 vs router 注册」人工核对清单，零偏差（建议固化为脚本，见 R7 扩展）。

**R9 post 阶段进度形态**（对应 F5，方案依赖 Q4）
- 验收标准（二选一，写入 TaskProgress.spec.ts）：a) 后端在 post 阶段广播一次 100% 进度帧，前端显示 100% + success 色；b) 前端在 post 态显示不确定条纹 + 「后处理中」文案（与解析态一致）。

**R10 暂停/续传界面说明**（对应 I2）
- 范围：TaskDetail 操作区。
- 验收标准：操作区出现一句「当前引擎不支持暂停/续传；需要中断请取消，重新入队会保留原配置版本」；TaskDetail.spec.ts 断言该文案存在。

**R11 深色模式封面 1px 边框**（对应 V1）
- 范围：tokens.css 深色模式块或各缩略图类补 `border: 1px solid var(--border)`（.tcard-thumb / .trow-thumb / .file-thumb-img img / PreviewDrawer img,video）。
- 验收标准：深色模式下缩略图有 1px 边框（源码断言）；闸门新增对应断言（可并入 R7）。

**R12 文档笔误修订**（对应 F6）
- 验收标准：docs/07 §2 `--surface-2:#EEEF2` 改为 `#EEF0F7`（与 tokens.css:22 一致）；全仓 grep 无其他非法 hex token。

---

## 4. 不在本轮范围的事项（明确排除）

| # | 事项 | 排除理由 |
| --- | --- | --- |
| 1 | 115/qB/Telegram/网易云直链/iOS 快捷指令**真实联调** | 无账号/实例/真机，属外部资源依赖；docs/10 §6 已如实标注，界面已按「未配置/状态不明/可用/不可用」四态诚实表达（Connections.vue:84-114）、直播平台清单已禁用未实测平台（RecordPlanForm.vue:109）——「不假装可用」已达标 |
| 2 | 微信入口 | docs/01 §2.2 明确 out of scope（协议核实前不实现） |
| 3 | engine/task/store/server 集成测试补强（覆盖率 21–27%） | 测试专项而非功能/UI/交互/视觉重构；建议单独立项（核心不变量已被单测覆盖） |
| 4 | 升级→回滚实机演练（docs/10 §6 #11） | 运维演练；机制（checksum + 只增迁移）已实现并有文档 |
| 5 | arm64 镜像本机构建 | CI QEMU+buildx 双平台已覆盖 |
| 6 | DNS Rebinding 防护 | 设计取舍已声明（ssrf.go 注释），自托管由网络边界兜底 |
| 7 | 同名产物撞名（docs/09 §4） | 保存策略缺陷，需产品决策命名维度，见待确认 Q3 |
| 8 | 大文件平台限速 | 平台侧问题，非本软件缺陷 |

---

## 5. 待确认问题（需用户/主理人决策，产品经理不自行编答案）

- **Q1 删除任务时是否提供「连同文件一起删除」？** docs/01 §5.5 要求两个操作分离；当前对话框却承诺删文件。选项：(a) 严格按契约拆成两个操作（推荐，R1+R2 默认方案）；(b) 增加「同时删除文件」勾选 + 后端 `delete_files=1` 支持。
- **Q2 存储策略路由本轮是否实施？** docs/01 §3.4 与 docs/08 §4.7 要求「本地/115 优先/失败转 qB」可配置可解释，但属功能新建而非重构打磨，且 115 通道未实测。选项：(a) 本轮实施（R3）；(b) 显式降级为后续版本，同步修订 docs/01 §3.4、docs/08 §4.7、docs/10 §1。
- **Q3 同名产物撞名**（不同清晰度同文件名 → NO_OUTPUT，docs/09 §4）：保存策略是否带清晰度维度（如 `downloads/<platform>/<清晰度>/`）？影响 storage.naming_rule 默认值与 UI 提示。
- **Q4 post 阶段进度形态**：后端广播 100% 帧（R9-a）还是前端不确定条纹（R9-b）？涉及后端改动成本，需架构师意见。
- **Q5 docs/01 §5.12「升级前备份数据库」是否要求应用内备份入口**（如一键导出 data.zip）？当前仅 docs/10 §5 手动 `cp -r data` 步骤，无应用内机制。
- **Q6 `/auth/status` 契约修订方向**：改 docs/04 为 POST（对齐实现），还是改实现为 GET（对齐文档）？注意 iOS 快捷指令/浏览器扩展等外部消费者以 docs/04 为准（docs/12 已声明以 docs/04 为契约）。

---

## 附：本 PRD 结论的复跑命令

```bash
# 设计符合度闸门（228 项）
python3 scripts/check-design-conformance.py

# 前端门禁
cd web && npm run build && npx vitest run

# 后端门禁（注意 go 路径：/usr/local/go/bin/go）
cd backend && /usr/local/go/bin/go build -buildvcs=false ./... \
  && /usr/local/go/bin/go vet -buildvcs=false ./... \
  && /usr/local/go/bin/go test -buildvcs=false -count=1 ./...
```
