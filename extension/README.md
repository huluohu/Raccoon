# Raccoon 浏览器扩展

把网页链接一键提交到**你自己的** Raccoon 实例下载。扩展只做三件事：抓链接 → 调后端 API → 出通知；不下载、不解析、不存储媒体文件。

> 契约来源：本仓库 `docs/04-API详细设计.md`（HTTP API）与 `docs/01-需求规格与行为契约.md` §3.1 / §3.6（下载中心行为契约、多入口契约）。
> 扩展提交的链接进入**同一个任务系统、同一状态机**（与 Web GUI / Telegram / CLI 共用队列），不另起一套。

---

## 1. 安装（开发者模式加载已解压扩展）

扩展是纯原生 JS，**无构建步骤、无 npm 依赖**，下载/克隆后直接加载。

### 1.1 加载扩展

1. 打开 Chrome 或 Edge，地址栏输入 `chrome://extensions`（Edge 为 `edge://extensions`）。
2. 打开右上角（Edge 在左下角）的**「开发者模式」**开关。
3. 点击**「加载已解压的扩展程序」**（Edge 为「加载解压缩的扩展」）。
4. 选择本仓库的 `extension/` 目录（即包含 `manifest.json` 的那一层，不要选 `extension/src`）。

成功后扩展列表出现「Raccoon 下载助手」，建议点图钉固定到工具栏。

### 1.2 首次配置

1. 点击工具栏的浣熊图标打开弹窗。
2. 填写**实例地址**（例：`http://127.0.0.1:8530`；局域网内填 `http://192.168.1.20:8530`）。
3. 填写**API Token**：在 Raccoon Web 界面 **「设置 → API 令牌」**签发，权限范围选**读写**（scope 位掩码 `3` = read|write），点「签发」后**明文只显示这一次**，立刻复制。
4. 点**「保存」**。首次保存会弹出站点权限确认（扩展需要访问你的实例地址），点「允许」。
5. 点**「测试连接」**，出现「连接正常」即完成。

---

## 2. 配置项说明

| 配置项 | 对应 API 字段 | 留空时 | 说明 |
| --- | --- | --- | --- |
| 实例地址 | 请求基址 | —— | `http://host:port`，无 `/api/v1` 后缀。保存时自动规范化（去尾斜杠、补 scheme）。 |
| API Token | `Authorization: Bearer <token>` | —— | 只写入本机 `chrome.storage.local`。 |
| 默认质量 | `items[].quality` | 用服务端默认（`download.default_quality`，当前默认 `best`） | 例：`1080p`、`720p`。 |
| 默认保存目录 | `items[].save_path` | 用服务端命名规则 | 例：`/media/movies`。 |

配置存放于 `chrome.storage.local` 的 `raccoon.settings` 键。

---

## 3. 使用

### 3.1 右键菜单

| 菜单位置 | 标题 | 行为 |
| --- | --- | --- |
| 链接上右键 | 用 Raccoon 下载 | 提交该链接地址。 |
| 页面上右键 | 用 Raccoon 下载此页（含媒体直链） | 注入脚本抓 `<video>/<audio>/<source>`、`og:video`、`<link rel=canonical>` 等直链，一并提交；抓不到时退化为提交页面地址。 |
| 选中一段文本右键 | 用 Raccoon 下载选中的链接 | 从选中文本里提取第一条 `http(s)` 链接。 |

结果通过右上角系统通知（`chrome.notifications`）反馈：任务 ID、跳过的重复链接、失败原因。

### 3.2 弹窗

- **当前页面**：显示当前标签页地址，点「用 Raccoon 下载」提交（含媒体直链捕获）。
- **任务状态**：`GET /api/v1/tasks` 拉最近 20 条，可按状态过滤（等待中 / 解析中 / 下载中 / 后处理 / 已完成 / 失败 / 已取消）。
- **配置**：见 §2。

### 3.3 站点权限（host permissions）

实例地址是**你自己填的**，静态 `manifest.json` 无法穷举，因此：

- `manifest.json` 的 `host_permissions` 预置 `http://127.0.0.1/*`、`http://localhost/*`（本机默认场景，开箱可用）；
- 其余地址走 `optional_host_permissions`（`http://*/*`、`https://*/*`），在**保存配置时按需申请**，只授权你填的那一个源。

如果实例地址改了就重新点一次「保存」重新授权。

---

## 4. 调用的后端 API（与 `docs/04` 对照）

| 场景 | 方法 | 路径 | 请求体 / 查询参数 | 文档出处 |
| --- | --- | --- | --- | --- |
| 提交任务 | `POST` | `/api/v1/tasks` | body `{"items":[{"url":"…","quality":"…","save_path":"…"}]}` | docs/04 §2.2 |
| 查询任务列表 | `GET` | `/api/v1/tasks` | query `status` / `platform` / `q` / `kind` / `limit` / `cursor` | docs/04 §1.3、§2.2 |
| 查询任务详情 | `GET` | `/api/v1/tasks/{id}` | —— | docs/04 §2.2 |

约定：

- **基础路径** `/api/v1`（docs/04 §1）。
- **认证**：`Authorization: Bearer <token>`，Token 通道（docs/04 §1.1）。请求用 `credentials: 'omit'`，不带 Cookie，因此**天然豁免 CSRF**（CSRF 只作用于会话通道的写操作）。
- **空字段省略**：`quality` / `save_path` 传空串表示「用服务端默认」（docs/04 §2.2 中二者为可选字段），字段名始终存在。
- **单批上限**：一次最多 50 个任务（docs/04 §2.2，服务端同样校验）。
- **错误处理**：按 docs/04 §1.2 的唯一错误格式解析 `{"error":{"code","message","request_id"}}`，并对 `401/403/409/429/503` 给出可读提示；`409 CONFLICT` 表示该链接已在队列中（docs/04 §4 幂等与防重）。

---

## 5. 隐私说明

| 数据 | 存哪儿 | 是否外传 |
| --- | --- | --- |
| API Token | `chrome.storage.local`（键 `raccoon.settings`） | **否**。只作为 `Authorization` 头发往你配置的实例地址。 |
| 实例地址、默认质量、默认保存目录 | `chrome.storage.local` | 否。 |
| 页面链接 / 媒体直链 | 不落盘 | 作为 `items[].url` 发往你的实例（这正是提交下载所必需的）。 |
| 任务列表 | 仅内存（弹窗打开期间） | 否。 |

- 扩展**不使用** `chrome.storage.sync`，Token 不会跟随浏览器账号同步到其他设备。
- 扩展**没有**任何第三方统计、埋点、远程配置上报代码。
- 扩展**不上传**页面内容、Cookie 或浏览历史。
- 除你的 Raccoon 实例地址外，扩展不与任何第三方通信；网页媒体直链只发给该实例。
- 撤销 Token 的方式：在 Web 界面「设置 → API 令牌」点「撤销」，**立即失效**（docs/04 §2.1、§4）。

---

## 6. 权限说明

| 权限 | 用途 |
| --- | --- |
| `storage` | 存实例地址与 API Token（`chrome.storage.local`）。 |
| `contextMenus` | 右键菜单「用 Raccoon 下载」。 |
| `activeTab` | 用户主动点击扩展/菜单后，读取当前标签页地址。 |
| `scripting` | 注入只读脚本抓取页面媒体直链（不修改页面）。 |
| `notifications` | 提交结果系统通知。 |
| `host_permissions` / `optional_host_permissions` | 访问你配置的 Raccoon 实例地址。 |

---

## 7. 故障排查

| 现象 | 原因与处理 |
| --- | --- |
| 「无法连接到 Raccoon 实例」 | 实例地址错、实例没起、或未授予该地址的站点权限 → 重新点「保存」授权。 |
| `401 UNAUTHORIZED` | Token 无效 / 已撤销 / 已过期 → 在 Web 界面重新签发。 |
| `403 FORBIDDEN` | Token scope 不含 `write` → 重新签发时选「读写」。 |
| `409 CONFLICT` | 同一链接已在队列中（防重），不是错误。 |
| `429 RATE_LIMITED` | 触发限流（API Token 120 次/分钟，docs/04 §5）→ 稍后再试。 |
| `503 ENGINE_UNAVAILABLE` | 服务端 yt-dlp 引擎缺失（docs/04 §1.2）→ 检查实例部署。 |
| 页面右键提交了整页而不是单个视频 | 该页没有暴露媒体直链，退化为提交页面地址属预期行为。 |

---

## 8. 目录结构

```
extension/
├── manifest.json          # MV3 清单
├── README.md              # 本文件
├── icons/                 # 16/32/48/128 图标
└── src/
    ├── api.js             # API 客户端（Bearer Token 通道；globalThis.RaccoonAPI）
    ├── settings.js        # 配置存取（chrome.storage.local；globalThis.RaccoonSettings）
    ├── background.js      # service worker：右键菜单 / 媒体链接捕获 / 提交 / 通知
    ├── popup.html         # 弹窗结构
    ├── popup.js           # 弹窗逻辑
    └── popup.css          # 弹窗样式
```

`background.js` 是**经典脚本**（`importScripts` 加载 `api.js` / `settings.js`），`popup.html` 用普通 `<script>` 顺序加载同一批文件——两端共用同一份 API 客户端，不会出现实现分叉。
