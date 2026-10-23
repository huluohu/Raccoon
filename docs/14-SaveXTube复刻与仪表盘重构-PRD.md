# 14 · SaveXTube 复刻与仪表盘重构 · 增量 PRD

> 项目：Raccoon（浣熊）· 自托管多媒体下载中心 ｜ 作者：产品经理（software-product-manager）
> 性质：增量 PRD，只描述「复刻 + 重构变更部分」。复刻基准：SaveXTube 原版主页截图（clipboard-2026-10-23T05-06-46）。
> 用户原话：「我要的是功能复刻甚至更强，不是删减版；前端 UI 实在不知道你怎么就认可的，显然很烂」。
> 纪律：证据不足标「待确认」；验收标准可观测；不编造。后端地基均经亲自核实（见各节文件:行），未重新完整审计。
> 一致性声明：删除操作按 docs/13 已裁决 Q1（docs/01 §5.5 两个独立操作）；API 契约按已裁决 Q6（docs/04 与实现对齐，新端点同日补登路由总表，不再犯 docs/13 F4 的漏登问题）。
> 与 docs/07 的关系：docs/07 是前端宪法，本 PRD 的所有组件/Token/动效需求不得抛开 tokens.css 另起炉灶；涉及路由表/标签的 IA 调整随本轮同步修订 docs/07 §3（修订清单见 §3.6）。

---

## 1. 复刻映射总表（原版 18 块 × Raccoon 现状）

Raccoon 现有主页（docs/13 已核实）：Overview.vue = ActiveTasks + StatusStrip + 4 张指标卡（队列/媒体库/连接/系统）+ IssuesPanel。原版主页是「快速下载 + 16 格仪表盘 + 直播录制卡」。

| # | 原版信息块（截图逐个转录） | Raccoon 现状 | 差距判定 |
| --- | --- | --- | --- |
| 1 | 快速下载卡（输入框占位「粘贴视频/音乐/播放列表链接...」+ 下载按钮） | 已有全局顶栏 QuickSubmit（App.vue:190-194）+ /tasks 页解析预览入口 | **已覆盖**（形态差异：原版在主页内，Raccoon 在顶栏全局——全局入口更优，保留；主页补一张引导卡指向顶栏即可） |
| 2 | 运行时长（大数字 + 副行，截图两行同为「9小时」，语义待确认：推测「累计运行/本次启动」） | GET /system/info 有 uptime（Overview.vue:209 已在用） | **已覆盖**（副行语义待确认，见 Q7） |
| 3 | 总下载（累计完成任务数） | /system/queue 有 total_tasks（含各状态） | **部分覆盖**：需 done 口径的累计完成数（summary API 新增） |
| 4 | 今日下载 | 无任何日期维度统计（store/counts.go 仅全量计数） | **缺失**（日期聚合新建） |
| 5 | 成功率 | 无（counts 可前端除，但无权威口径） | **缺失**（success_rate 服务端算：done/(done+failed)） |
| 6 | 下载总量（字节，222.1 MB） | /system/queue 有 media_bytes（system_handler.go:84） | **已覆盖** |
| 7 | 图片（media kind=image 计数） | media_files.kind 列就绪（migration 0003/0004）但无按 kind 计数查询 | **部分覆盖**：新增 by-kind 聚合 |
| 8 | 视频（kind=video 计数） | 同上 | **部分覆盖** |
| 9 | 本周下载 | 无 | **缺失** |
| 10 | 订阅下载（source=subscription 完成数） | tasks.source 枚举就绪（state.go:24-32）无按 source 聚合 | **部分覆盖**：新增 by-source 聚合 |
| 11 | 收藏下载（B 站收藏夹订阅 vs UP 主订阅） | subscriptions.source_type 字段就绪（store/subscriptions.go:15，值 favorite/uploader/list） | **部分覆盖**：需 tasks.source_ref → subscriptions.source_type 关联统计 |
| 12 | CPU 使用率（环形图 + 已使用/空闲图例） | 完全没有 | **缺失**（sampler + API + 组件） |
| 13 | 内存使用（环形图 + 已用%/已用GB/空闲GB） | 完全没有 | **缺失** |
| 14 | 存储空间（饼图 + 已用%/空闲/总量） | /system/health 有 disk_used_pct（server.go:162）无字节数 | **部分覆盖**：补 free/total bytes |
| 15 | 网络带宽（上传/下载 Mbps 实时） | 完全没有 | **缺失** |
| 16 | 7 天下载趋势（折线，周一~周日） | 完全没有 | **缺失** |
| 17 | 30 天下载趋势（折线，按日期） | 完全没有 | **缺失** |
| 18 | 直播录制卡（主页底部，截图被截断） | /live-sources 有状态（offline/live/recording）+ RecordingList 组件就绪 | **已覆盖**（摘要有组件可复用） |

导航复刻（9 项）：主页/任务/历史/文件/订阅/直播/日志/监控/设置。Raccoon 现有 7 项（总览/下载/媒体库/订阅/直播/连接/设置）→ **缺历史、日志、监控三个一级页**；「连接」为 Raccoon 自有能力（原版无独立连接页，凭据在设置里），保留为一级页并作为差异点说明（路由不删，docs/07 §3 同步修订而非推翻）。标签对齐建议：总览→主页、下载→任务、媒体库→文件（见 Q6）。

---

## 2. 新增后端 API 需求（对齐 docs/04 契约格式）

通用规则：全部挂在 `/api/v1` 下、会话认证（requireAuth）；SSE 不扩展（监控/统计走轮询，理由见 §3.4）；**每个新端点必须同日落进 docs/04 §2 路由总表**（docs/13 F4 教训）；日期口径统一用服务端本地时区的自然日/自然周。

### 2.1 GET /system/stats/summary —— 主页指标聚合（新建）

| 项 | 内容 |
| --- | --- |
| 方法/路径 | GET /api/v1/system/stats/summary |
| 入参 | 无 |
| 出参草案 | `{ "uptime_sec": 32400, "total_downloads": 2, "today_downloads": 2, "week_downloads": 2, "success_rate": 100.0, "media_bytes": 232883456, "media_by_kind": {"video":2,"audio":0,"image":0,"subtitle":0,"other":0}, "sub_downloads": 0, "favorite_downloads": 0 }` |
| 复用/新建 | store 层新建 `internal/store/stats.go`：CountTasksByStatus/CountTasks（counts.go:6-13）保留；新增 ByDateRange（今日/本周 done 计数）、MediaCountByKind（migration 0003 kind 列）、CountBySource（state.go:24-32 枚举）、CountFavoriteDownloads（tasks.source_ref JOIN subscriptions.source_type='favorite'，订阅删除后历史任务仍计入——快照口径，删订阅不影响历史统计） |
| 口径说明 | 今日/本周下载 = status='done' 且 finished_at 落在自然日/周（周一为首日）；success_rate = done/(done+failed)，分母为 0 时返回 null（前端显示「—」，不显示 0% 或 100% 造假） |
| 验收 | 单测：构造跨日/跨周任务与多 kind 媒体、多 source 任务、含已删订阅的 source_ref，逐字段断言；success_rate 分母为 0 返回 null |

### 2.2 GET /system/stats/trend —— 下载趋势（新建）

| 项 | 内容 |
| --- | --- |
| 方法/路径 | GET /api/v1/system/stats/trend?days=7（days ∈ {7,30}，默认 7） |
| 入参 | days：7 或 30（其他值 400 VALIDATION） |
| 出参草案 | `{ "days": 7, "points": [{"date":"2026-10-17","count":2,"bytes":232883456}, ...] }`（按日升序，无数据日补 0，长度恒等于 days） |
| 复用/新建 | store/stats.go 新建 DailyDoneStats(from,to)：GROUP BY date(finished_at)，status='done' |
| 验收 | 单测：跨 7/30 天边界、空窗补 0、时区一致性（与 summary 同一本地时区函数） |

### 2.3 GET /system/monitor —— 系统监控实时（新建，含后台 sampler）

| 项 | 内容 |
| --- | --- |
| 方法/路径 | GET /api/v1/system/monitor |
| 入参 | 无 |
| 出参草案 | `{ "ok": true, "sampled_at": 1730000000, "cpu": {"used_pct": 0.0}, "memory": {"used_pct": 10.5, "used_bytes": 788000000, "free_bytes": 6900000000, "total_bytes": 7700000000}, "disk": {"used_pct": 97.4, "free_bytes": 12700000000, "total_bytes": 128800000000}, "net": {"up_bps": 0, "down_bps": 0}, "series": {"cpu":[60个点], "memory":[60], "net_up":[60], "net_down":[60]} }`；非 Linux 平台 `{"ok": false, "message": "当前平台不支持资源监控（仅 Linux 采集 /proc）"}` |
| 复用/新建 | 新建 `internal/monitor` 包：启动时起 sampler goroutine（建议 2s 间隔，环形缓冲 60 点 = 2 分钟实时窗口，频率待确认 Q3）；CPU 读 /proc/stat 两次采样差、内存读 /proc/meminfo、磁盘复用现有 disk 统计逻辑（healthz 同源）、网络读 /proc/net/dev 差量；非 Linux（dev macOS）ok=false 如实降级，不返回假数据（docs/01 诚实原则） |
| 验收 | 单测：/proc 内容用 fixture 文本注入解析器（used_pct 计算、两次采样差、环形缓冲淘汰）；非 Linux 构建下 ok=false；handler 单测覆盖 ok 两态 |

### 2.4 GET /logs —— 日志查询（新建，复用脱敏器）

| 项 | 内容 |
| --- | --- |
| 方法/路径 | GET /api/v1/logs?level=warn&module=task&q=关键词&from=&to=&limit=100&cursor= |
| 入参 | level（debug/info/warn/error，可多选逗号分隔）、module、q（消息关键词，≤256 字符，对齐 docs/10 §3.7 截断教训）、from/to（Unix 秒）、limit（默认 100 上限 500）、cursor（行号游标） |
| 出参草案 | `{ "items": [{"ts":1730000000,"level":"info","module":"task","msg":"..."}], "next_cursor": "1200" }`（统一 camelCase 由前端隔离层转换，对齐 docs/07 §8） |
| 复用/新建 | 复用 system_handler.go:153-178 的 readLogTail + redactLine 脱敏器（cookie/token/password/key → ***，日志页与诊断包同一套脱敏，不造第二套）；扩展为按行解析 JSON（logx 结构化格式 ts/level/msg/module）+ 过滤 + 行号游标；日志路径 data_dir/logs/raccoon.log |
| 验收 | 单测：脱敏 fixture（含 cookie= 行必出 ***）、level/module/q/from/to 过滤、cursor 翻页不重不漏、q 超 256 截断；安全用例：日志行含 token 明文时接口输出必为 *** |

### 2.5（P2）GET /system/monitor/history —— 监控历史曲线

| 项 | 内容 |
| --- | --- |
| 方法/路径 | GET /api/v1/system/monitor/history?range=24h |
| 入参 | range：1h / 24h（其他 400） |
| 出参草案 | `{ "range": "24h", "granularity_sec": 60, "points": [{"ts":..., "cpu":..,"memory":..,"net_up":..,"net_down":..}, ...] }` |
| 复用/新建 | monitor 包内 1min 聚合环形缓冲（24h = 1440 点，内存占用可忽略）；重启清零，界面如实标注「重启后历史从零开始」 |
| 验收 | 单测：聚合均值计算、容量淘汰、range 校验 |

### 2.6 路由总表补登清单（随本轮修订 docs/04 §2.9）

/system/stats/summary、/system/stats/trend、/system/monitor、/logs、（P2）/system/monitor/history——共 4-5 个新端点，与 docs/13 F3/F4 的修订（auth/status 改 POST、counts 来源、tasks/parent、tokens×3、integrations×4、system/queue）合并为一次 docs/04 路由总表校准，校准后以 router 注册清单逐条核对（方法 + 路径）出具记录。

---

## 3. 前端重构需求

### 3.1 导航 IA 调整（9 复刻项 + 1 差异项 = 10 个一级路由）

| 路由 | 页面 | 现状与动作 | 原版对应 |
| --- | --- | --- | --- |
| / | 主页（仪表盘） | Overview.vue 重构（§3.2） | 主页 |
| /tasks | 任务 | Tasks.vue 现有，标签「下载」→「任务」 | 任务 |
| /history | 历史 | **新建** History.vue：done/failed/cancelled 只读列表 + 日期筛选（§3.5） | 历史 |
| /media | 文件 | Media.vue 现有，标签「媒体库」→「文件」（Q6）；补删除文件入口（docs/13 Q1 裁决：独立操作 + RDialog 二次确认） | 文件 |
| /subscriptions | 订阅 | 现有 | 订阅 |
| /live | 直播 | 现有 | 直播 |
| /logs | 日志 | **新建** Logs.vue（§3.5） | 日志 |
| /monitor | 监控 | **新建** Monitor.vue（§3.5） | 监控 |
| /connections | 连接 | 现有（Raccoon 自有差异能力，保留一级页） | 原版无（凭据在设置里） |
| /settings | 设置 | 现有 | 设置 |

- App.vue NAV_ITEMS 扩到 10 项（图标线稿 SVG，禁 emoji，docs/07 §4）；router/index.ts 增 3 条路由（history/logs/monitor）。
- **移动端 tabbar 矛盾（待确认 Q2）**：docs/07 §6 <768 底部主操作栏现有 7 项满额（375px 每项 52px + 12px 两字标签，§8.1 ⑥），10 项放不下。建议方案 (a)：tabbar 固定 5 高频（主页/任务/文件/订阅/设置）+「更多」打开全量导航面板；方案 (b) 横向滚动（体验差，不推荐）。需用户决策。
- 顶栏保留 QuickSubmit 全局入口（docs/13 R5 全局化随本轮落地）。

### 3.2 主页仪表盘信息架构（分组与优先级）

**首屏必须可见（P0，一屏或一屏内滚动一条）**：
1. 快速下载引导卡（横跨）：一句引导 + 输入框（复用 QuickSubmit 组件，input-id 独立）+ 说明「顶栏随时可贴链接」
2. 指标行 1（4 卡）：运行时长 / 总下载 / 今日下载 / 成功率
3. 指标行 2（4 卡）：下载总量 / 图片 / 视频 / 本周下载
4. 指标行 3（4 卡）：订阅下载 / 收藏下载 / 直播录制中数 / 待处理问题数（IssuesPanel 摘要入口）
5. 直播录制卡（横跨或半宽）：录制中的源 + 分段信息 + 跳转 /live

**第二屏（P1）**：监控摘要带（CPU/内存/存储/带宽 4 张环形/数字卡，点击跳 /monitor）+ 7 天/30 天趋势（Tab 切换或并排）。

**保留不删**：ActiveTasks（正在搬的活，六表情 working）、StatusStrip（磁盘/数据库/实时/凭据）——这是 Raccoon 比原版强的部分，并入首屏指标行 3 之后或第二屏顶部。

- 轮询策略：summary 5s、trend 60s、monitor 2s（仅 /monitor 页与主页监控带；离开页面停轮询，对齐 stores/tasks.ts connect/disconnect 生命周期模式）。
- 加载/空/错误三分（docs/07 §7）：首屏指标卡骨架屏（skeleton.ts 已有）、接口失败错误卡 + 重试、success_rate 分母为 0 显示「—」。

### 3.3 指标卡组件规范（base/MetricCard.vue）

- 结构：彩色圆角图标块（40px，圆角 --radius-ctrl）+ 大数字 + 标签（+ 可选副行/跳转）。
- 色板**全部从 tokens.css 语义色取，禁新硬编码**（闸门 §2 断言覆盖，违规即 FAIL）：图标块底 = `color-mix(in srgb, var(--语义色) 16%, transparent)`，图标 = `var(--语义色)`——与 SubList.vue .cnt 徽章同手法（已有先例）。分配：运行时长 --neutral｜总下载 --info｜今日下载 --success｜成功率 --warning（<100% 时 --danger，前端按值切 tone）｜下载总量 --info｜图片 --neutral｜视频 --info｜本周下载 --success｜订阅下载 --warning｜收藏下载 --gold（gold-soft 底 + gold-strong 图标）｜CPU --info｜内存 --warning｜存储 --danger（≥90%）/ --warning｜带宽 --success。
- 字阶：大数字 --font-display（30px）+ font-variant-numeric:tabular-nums（§2 铁律）；标签 --font-aux；禁止 --font-caption 上正文（闸门 §2 语义断言覆盖）。
- 动效：数字刷新 --motion-progress 240ms linear（§6 进度数字档）。
- 网格：`repeat(auto-fit, minmax(260px, 1fr))`——与 Overview.vue .cards 现值一致（1440 主档四卡齐行，闸门 §6 网格断言覆盖）。
- 组件配 MetricCard.spec.ts（渲染 + tone 切换 + tabular-nums 类名断言）。

### 3.4 趋势图与监控图组件（选型建议：纯 SVG，不引图表库）

**建议：TrendChart.vue / DonutChart.vue 全部手写 SVG，不引入 ECharts 等图表库。理由**：
1. docs/07 §8 工程约定 CI 只有 `npm ci && vue-tsc && vite build && vitest run`，当前依赖仅 vue/pinia/vue-router（package.json:14-18 已核实）——引库即扩供应链与 embed.FS 二进制体积，与「单静态二进制」技术约束（docs/01 §1）相悖；
2. 数据量极小（7/30 个点、60 点序列），SVG polyline + circle 足够，无性能问题；
3. jsdom 下 SVG 是真实 DOM 可断言（vitest 可测），canvas 方案不可测——违背 docs/07 §8「每视图配 spec」；
4. 全站先例：RaccoonFace/PawSlider/全部图标均为手写 SVG（§8.1 闸门对 emoji/线稿的断言同源），风格一致。

- TrendChart：`<svg viewBox>` + polyline 折线 + 末端数据点圆 + 单色 12% 透明面积填充（注意：docs/07 §6 禁的是**装饰性大渐变**，图表功能填充用单色低透明，不违规；禁用 LinearGradient 装饰）；x 轴刻度 --font-caption（表头/时间戳白名单内，闸门语义断言放行）；hover 竖线 + 数值气泡（P1，SVG line + text）；reduced-motion 只保留透明度（motion.css 全局兜底已覆盖）。
- DonutChart：SVG circle + stroke-dasharray/stroke-dashoffset 画环；图例 = 色点 + 文字（色点用同一语义色变量）；环端点 stroke-linecap:round；中间大数字（--font-display + tabular-nums）。存储饼图用双环（已用/空闲分段）或 donut + 图例，二选一由架构师定，PRD 不锁死。
- 网络带宽：双数字（上传/下载 Mbps）+ 迷你 sparkline（TrendChart 复用，无轴）。
- 每组件配 spec.ts：给定 points 断言 polyline points 属性、dasharray 比例、空数据降级（显示「暂无数据」空态，不画崩）。

### 3.5 三个新页面组件规范

- History.vue：TaskList 组件复用（status=done,failed,cancelled 过滤 + 日期范围选择器 RSelect/RInput）；只读——不给取消/重试/删除按钮（重试在任务页做，删除按 docs/13 Q1 两个独立操作）；顶部汇总条（时间范围内完成数/失败数/成功率，summary API 扩展或前端聚合二选一由架构师定）。空态 REmpty（face=done，「还没有完成记录」）。
- Logs.vue：过滤器行（级别多选 chips + module 下拉 + 关键词输入 + 时间范围）+ 虚拟滚动外的简单分页（cursor）+ 实时跟随开关（P2，3s 轮询 tail 模式）；日志行渲染：ts（--font-caption 时间戳白名单）+ level 徽章（warn/error 用语义色）+ msg（等宽字体 mono，对齐 TaskDetail .td-engine-detail 手法）；脱敏由服务端保证，前端不得尝试还原。
- Monitor.vue：4 张监控卡（CPU/内存 donut、存储 donut、带宽双数字 + sparkline）+ 实时序列图（60 点，TrendChart 复用）+（P2）历史曲线 Tab（1h/24h）。非 Linux 降级：整页显示 REmpty + 「当前平台不支持资源监控」，不渲染假图表（诚实原则）。轮询 2s，页面不可见时停（visibilitychange）。

### 3.6 需同步修订 docs/07 的清单（宪法随 IA 调整校准，不推翻）

1. §3 路由表：新增 /history、/logs、/monitor 三条；标签「总览→主页、下载→任务、媒体库→文件」；关键组件列补 History/Logs/Monitor 三页组件。
2. §4 组件清单：补 MetricCard / TrendChart / DonutChart / HistoryList / LogViewer / MonitorCard 六个组件及验收点。
3. §6 响应式：tabbar 从「7 项」改为「5 高频 + 更多」（若 Q2 选方案 a）。
4. §8 每视图配 spec：三个新视图 + 六个新组件都要有 *.spec.ts。

---

## 4. 「甚至更强」清单（超越原版截图的能力）

> 纪律：只列可在现有架构内闭环的增强；每条标 P1/P2，P0 一律留给复刻本身。

### E1（P1）仪表盘卡片可配置：显示/隐藏 + 排序

- 原版主页 17 块全量铺开，信息密度高但无个性化。
- 增强：主页右上角「自定义」入口（RDialog），MetricCard 级开关 + 拖拽排序（HTML5 draggable 即可，不引库）；配置持久化到后端 `user_preferences` 表（或现有配置表加 JSON 字段，由架构师定），跨设备生效。
- 验收：关闭「图片」卡后刷新仍在关闭态；排序调整后多端一致；默认配置 = 复刻全量 17 块（保证首次体验与原版对齐）。

### E2（P1）成功率趋势预警：低于阈值自动浮 IssuesPanel + 通知

- 原版成功率只是一个数字，无下文。
- 增强：成功率 < 80%（阈值可配，配置中心加 `alert.success_rate_threshold`）时，MetricCard tone 切 --danger；Overview 顶部浮出 IssuesPanel（复用现有 IssuesPanel 组件，插入「近 7 天成功率 72%，低于阈值」条目，点击跳监控页）；配置开启通知时走现有通知通道。
- 验收：mock trend 数据使成功率 72% → IssuesPanel 出现且链接正确；阈值改为 60% 后预警消失。

### E3（P2）监控历史曲线：24h / 1min 聚合

- 原版监控只有瞬时值。
- 增强：Monitor.vue 加历史 Tab（1h 实时 60 点 / 24h 聚合 60 点，每点 24min 均值），数据来自 §2.5 `GET /system/monitor/history`（服务端环形缓冲落 SQLite，表 monitor_sample(ts, cpu, mem, disk, net_rx, net_tx)，保留 7 天）。
- 验收：24h Tab 渲染 60 点曲线；环形缓冲淘汰后查询不报错；非 Linux 平台整 Tab 隐藏（不显示不可用入口）。

### E4（P2）日志实时跟随模式

- 原版日志页静态查询。
- 增强：Logs.vue 「实时跟随」开关（默认关），开启后 3s 轮询 cursor 增量（服务端 cursor 分页已支持），新行带 --motion-progress 淡入；到达底部才自动滚动（用户上翻则停跟，显示「回到最新」浮钮）。
- 验收：模拟持续写日志，开启跟随后 3s 内出现新行且视口钉底；用户上翻后新行不抢滚动。

### E5（P2）平台失败率拆分：失败按平台/原因聚合作图

- 原版只有总数。
- 增强：Monitor 或 Logs 页（由架构师定）加「失败分析」区块：trend API 扩展 by_platform 维度（yt-dlp/gallery-dl/FFmpeg 等），前端横向条形图（纯 SVG div 宽度比例即可），点击条形跳 Logs 预过滤该平台。
- 验收：mock 三平台失败数，条形宽度比例正确；点击跳转后 Logs 页 module 过滤值正确。

---

## 5. 不在本轮范围事项

1. **i18n / 多语言**：原版与 Raccoon 均为中文界面，复刻保持中文；文案全部走现有 i18n 或常量（由架构师定），不在本轮引入语言框架。
2. **引入图表库（ECharts/Chart.js/D3）**：见 §3.4 四条理由，明确排除；若后续出现必须库才能满足的图表需求，走增量评审，不在本轮。
3. **非 Linux 平台的资源监控**：监控 API 在非 Linux 返回 ok=false 降级（诚实原则），本轮不为 macOS/Windows 补实现，只保证降级文案与空态正确。
4. **日志系统改造（ELK/Loki 等外接）**：Logs 页只消费现有 readLogTail/脱敏器管道，不外接日志基础设施。
5. **原版「独立粘贴链接按钮」**：SaveXTube 截图中的独立粘贴入口不单独复刻，Raccoon 保留顶栏 QuickSubmit（App.vue:190-194 已核实）作为统一提交入口，交互等价且更集中。
6. **多用户/权限体系**：复刻不涉及多租户；E1 的 user_preferences 按单用户假设设计（表结构预留 user_id 由架构师定，不实现权限）。
7. **监控历史 24h 之外更长周期**（7 天/30 天曲线）：§2.5 只保留 7 天样本且只做 1h/24h 两个视图，更长期不做。
8. **主页 17 块之外的原版其他页面细节**：本轮只对齐导航 9 项 + 主页 17 块，原版其他页面若有截图差异，证据不足，标待确认不硬复刻。

---

## 6. 待确认问题（不准自编答案，请主理人/架构师裁决）

| # | 问题 | 选项/说明 | PRD 建议 |
|---|------|-----------|----------|
| Q1 | 导航最终项数：9 项复刻 + 「连接」共 10 项，还是「连接」并入设置？ | a) 10 项一级导航（复刻保真优先）b) 9 项 + 连接并入设置（降信息密度） | a：用户原话「功能复刻甚至更强」，且「连接」是 Raccoon 自有能力，合并会降低可达性 |
| Q2 | 移动端 tabbar：10 项无法全平铺，方案？ | a) 5 高频（主页/任务/历史/文件/设置）+「更多」展开 b) 横向滚动 c) 截断图标仅文字 | a：5+更多最稳，不引入滚动截断的隐藏可及性问题；若选 a 需同步修订 docs/07 §6 |
| Q3 | 监控采样频率确认：实时 2s × 60 点（1min 窗），历史 1min 聚合 × 60 点（24h 窗）？ | 2s 采样对 SQLite 写入量：1440 行/天 ×5 指标，量小可接受；若嫌写频高可降为 5s | 2s + 60 点；历史 1min 聚合 24h。请确认采样频率与保留 7 天 |
| Q4 | 趋势图/环形图选型纯 SVG（§3.4 已给 4 理由），是否认可？ | 若否决需给出可测/零依赖/embed.FS 体积三者同时满足的替代方案 | 认可纯 SVG；此为有建议权事项，默认按 §3.4 执行除非否决 |
| Q5 | 历史页只读边界确认：历史 = 完成/失败/取消记录只读展示；重试归任务页、删除归文件/任务页（docs/13 Q1 已裁决两个独立操作）？ | 若原版历史页有直接重试/删除按钮（截图证据不足，未在基准描述中），需补截图证据 | 按只读设计；如主理人有原版历史页截图证明可操作，改判并补 PRD |
| Q6 | 导航标签「媒体库 → 文件」是否按原版命名改？ | 原版第 4 项为「文件」；Raccoon 现「媒体库」语义更准（含图片/视频/音频分类） | 建议保留「媒体库」（media_files.kind 枚举支撑），映射表已注明差异；如坚持复刻则改 |
| Q7 | 「运行时长」副行语义：累计运行 vs 本次启动？ | 原版截图未明确；Raccoon 现有 health/uptime 语义需确认 | 建议「本次启动」+ tooltip 说明；若选「累计」需后端确认 uptime 字段来源 |

---

## 7. 建议实施顺序（供主理人排期参考）

1. **S1 后端地基**：§2 五个 API + 路由补登 + monitor_sample 表迁移（按 docs/04 格式补文档）——无前端依赖，可先行，配单测（docs/07 §8）。
2. **S2 组件层**：MetricCard / TrendChart / DonutChart + specs——主页重构的地基，零业务耦合。
3. **S3 主页仪表盘重构**：§3.2 信息架构 + §3.3 卡片规范落地，替换 Overview.vue 现有 cards（17 块全量，P0）。
4. **S4 三个新页面**：History → Logs → Monitor（按依赖从简到繁：History 复用 TaskList 最轻）。
5. **S5 增强项**：E1/E2（P1）→ E3/E4/E5（P2），随 Sprint 余量插入。
6. **S6 文档校准**：§3.6 docs/07 修订清单随各阶段同步，不留到最后一次性改。

**验收锚点**：四道门禁（check-design-conformance 228 项、npm build、vitest、go test）全程绿灯；§3.2 主页首屏五项与 docs/07 四档响应式逐档走查；E1-E5 验收标准逐条可观测。
