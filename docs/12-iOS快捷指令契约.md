# 12 · iOS 快捷指令契约

> 契约即文件：本文档描述的每个 HTTP 请求都严格对齐 `docs/04-API详细设计.md`，字段名逐字一致。
> 适用对象：在 iPhone / iPad 上用「快捷指令」App 把网页链接提交到自托管的 Raccoon 实例。
> 交互契约与 HTTP API、浏览器扩展同源（`docs/01` §3.6：浏览器扩展 / iOS 快捷指令 → 提交链接 / 查询状态）。
>
> ⚠️ **本契约基于 API 契约推导，未在真机实测。** 下文的动作名称、参数位置、JSON 组装方式均按 iOS 快捷指令的标准动作模型书写，但**没有**在真实 iOS 设备与真实 Raccoon 实例上跑通过。首次搭建时请按 §9 的验收清单逐条核对，遇到偏差以 `docs/04` 的 API 契约为准，并回来修订本文档。

---

## 1. 前提

### 1.1 需要准备的三样东西

| 项 | 值 | 来源 |
| --- | --- | --- |
| 实例地址 | 例：`http://192.168.1.20:8530` | 部署 Raccoon 时确定；同一局域网内的 iOS 设备必须能访问该地址（不能用 `127.0.0.1`） |
| API Token | 例：`rc_xxxxxxxx…` | Web 界面签发，见 §1.2 |
| 分享入口 | Safari 分享菜单 / URL Scheme | 见 §3 |

### 1.2 API Token 的获取路径

| 步骤 | 操作 | 说明 |
| --- | --- | --- |
| 1 | 用浏览器打开 Raccoon Web 界面并登录 | 走会话通道（`raccoon_session` Cookie） |
| 2 | 进入 **「设置」** 页 → 找到 **「API 令牌」** 区块 | 区块标题为「API 令牌」，说明文字是「给 CLI、扩展、快捷指令用」 |
| 3 | 填令牌名称，权限范围选 **「读写」** | 读写 = scope 位掩码 `3`（read=1 \| write=2）。**只读（1）无法提交任务** |
| 4 | 点「签发」 | 弹出明文 `rc_…`，**明文只显示这一次**，立刻复制 |
| 5 | 需要时回该页「撤销」 | 撤销后**立即失效**（`docs/04` §2.1、§4） |

> **注意「设置」与「连接」的区别**：「连接」页是**平台凭据中心**（B 站 Cookie、Bot Token、115 密钥等），不是 API Token 的签发处；API Token 只在「设置」页签发。

**为什么需要 write scope**：`docs/04` §1.1 规定 scope 位掩码 —— 读需 `read`，写需 `write`，管理域（配置/凭据/订阅/直播/令牌）需 `admin`。提交任务 `POST /api/v1/tasks` 属于写操作，只读令牌会返回 `403 FORBIDDEN`。

### 1.3 认证通道：为什么用 Bearer Token 而不是 Cookie

| 通道 | 机制 | 快捷指令能否使用 | 原因 |
| --- | --- | --- | --- |
| Web 会话 | `raccoon_session` HttpOnly Cookie + `X-CSRF-Token` 头 | ❌ 不可行 | 快捷指令的「获取 URL 内容」动作不带浏览器 Cookie；且 Cookie 会话通道的写操作必须附 `X-CSRF-Token`，快捷指令无法取得登录时下发的一次性内存 CSRF 值 |
| **API Token** | `Authorization: Bearer <token>` | ✅ 唯一可行 | 自带凭证、无 Cookie 上下文，**Token 通道豁免 CSRF**（`docs/04` §1.1 明文规定） |

结论：快捷指令只走 Token 通道，**不涉及 CSRF**。这也符合 `docs/01` §3.6「API Token 可撤销、有权限范围」。

---

## 2. 基线快捷指令：提交单个链接

> 这是其余所有变体的骨架。先把这一条跑通，再复制改造。

### 2.1 动作序列（照着点）

| # | 动作 | 参数设置 |
| --- | --- | --- |
| 1 | **共享表单**（Share Sheet） | 勾选「网址 URL」类型输入；不勾「文本」也可以（取决于 §3 选定的路径） |
| 2 | **设置变量** | 变量名 `raccoonURL`，值 = 动作 1 的「共享表单」输出 |
| 3 | **字典** | 新建字典，键值对：<br>· `items` → **数组**，元素为字典<br>· 元素字典键值对：`url` → 变量 `raccoonURL`；`quality` → 文本（空串）；`save_path` → 文本（空串） |
| 4 | **获取 URL 内容** | URL = `http://<你的实例地址>/api/v1/tasks`（例：`http://192.168.1.20:8530/api/v1/tasks`）<br>方法 = **POST**<br>请求头 = 加两行：`Authorization` = `Bearer rc_xxxxxxxx…`；`Content-Type` = `application/json`<br>请求体 = **JSON**，值 = 动作 3 的字典 |
| 5 | **获取字典值** | 字典 = 动作 4 的「获取 URL 内容」输出，键 = `data` |
| 6 | **显示结果** | 显示动作 5 的结果（或改用 **朗读文本**） |

### 2.2 请求体 JSON 模板（逐字对应 `docs/04` §2.2）

动作 3 组装出的字典，序列化后必须等价于下面这段：

```json
{
  "items": [
    {
      "url": "https://www.bilibili.com/video/BV1xx411c7mD",
      "quality": "",
      "save_path": ""
    }
  ]
}
```

| 字段 | 类型 | 必填 | 语义 | 留空行为 |
| --- | --- | --- | --- | --- |
| `items` | 数组 | ✅ | 待提交的任务条目列表 | 空数组会被服务端拒绝（`VALIDATION`：「items 不能为空」） |
| `items[].url` | 字符串 | ✅ | 要下载的链接 | 仅支持 `http://` / `https://`；内网地址被 SSRF 防护拒绝（`docs/04` §5） |
| `items[].quality` | 字符串 | ❌ | 下载质量，如 `1080p` | 空串 = 用服务端默认 `download.default_quality`（当前默认 `best`） |
| `items[].save_path` | 字符串 | ❌ | 保存目录，如 `/media/movies` | 空串 = 用服务端命名规则 |

> **单批上限 50 条**（`docs/04` §2.2，服务端同样校验，超出返回 `400 VALIDATION`）。

### 2.3 成功时读什么

`POST /api/v1/tasks` 成功返回 **HTTP 201**，body 形如：

```json
{
  "data": [
    {
      "id": "task_01J…",
      "kind": "download",
      "source": "web",
      "platform": "bilibili",
      "url": "https://www.bilibili.com/video/BV1xx411c7mD",
      "title": "…",
      "status": "waiting",
      "quality": "best",
      "save_path": "",
      "progress": 0,
      "created_at": 1730000000000
    }
  ],
  "conflicts": []
}
```

| 字段 | 含义 |
| --- | --- |
| `data` | 新建的任务数组（`store.Task` 结构，字段见 `docs/03` §4） |
| `conflicts[]` | 本批中被判定重复的条目，每项含 `existing_task_id`（已存在的任务 ID） |
| `data[].id` | 任务 ID，后续用它查状态 / 取消 / 重试 |
| `data[].status` | 任务七态之一：`waiting` / `parsing` / `downloading` / `post` / `done` / `failed` / `cancelled`（`docs/01` §4） |

**部分重复时**：HTTP 仍是 **201**，但 `conflicts` 非空——即「成功的进队列，重复的跳过」，**不是错误**。想让快捷指令把 `conflicts` 也念出来，在动作 5 之后追加一个 **获取字典值**（键 `conflicts`）。

---

## 3. 从 Safari 分享 → Raccoon

两条路径都能把「当前网页地址」喂给快捷指令。**推荐先做路径 A**（步骤更少、不依赖第三方 App）。

### 3.1 路径 A：共享表单 / Share Extension（推荐）

原理：iOS 的**共享表单（Share Extension）** 出现在 App 的分享菜单里。快捷指令把自己暴露为一个分享动作，Safari 分享时即可直接调用。

| # | 步骤 | 说明 |
| --- | --- | --- |
| 1 | 打开 **快捷指令** App → 右上角 `+` 新建快捷指令 | 命名为「Raccoon 下载」 |
| 2 | 点「添加操作」→ 搜索 **共享表单** | 选中「共享表单」动作；它决定这个快捷指令会出现在哪些 App 的分享菜单里 |
| 3 | 勾选 **网址 URL**（Websites / URL） | 勾掉不需要的类型（图片、文本等），勾得越少越准 |
| 4 | 添加后续动作：设置变量 → 字典 → 获取 URL 内容 → 显示结果 | 完全照抄 §2.1 的第 2–6 步 |
| 5 | 完成 → 回到 **Safari** 打开任意视频页 → 点分享按钮 | 分享菜单里出现「Raccoon 下载」（可能需要在「更多」里勾选并把它固定到顶部） |
| 6 | 点「Raccoon 下载」 | 快捷指令运行，任务入队，右上角弹出「Raccoon 下载」完成提示 |

**要点**：共享表单动作放在**第一个**，它的输出就是「用户刚分享的网页地址」。若把动作 2（设置变量）放在共享表单之前，拿到的会是空值。

### 3.2 路径 B：URL Scheme

原理：用 `shortcuts://run-shortcut` 这个系统 URL Scheme 唤起指定快捷指令并传入文本参数。适合「从别的 App / 网页 / 二维码跳进来」的场景。

| # | 步骤 | 说明 |
| --- | --- | --- |
| 1 | 快捷指令的第一个动作改成 **共享表单** 或 **文本**（接收输入），并照 §2.1 接好后续动作 | 本路径不依赖分享菜单，改由 URL 参数喂入 |
| 2 | 在 Safari 地址栏或任意可输入 URL 的地方拼出：<br>`shortcuts://run-shortcut?name=Raccoon%20%E4%B8%8B%E8%BD%BD&input=text&text=<URL 编码后的目标地址>` | `name` 是快捷指令名（空格用 `%20`），`input=text` 指定以文本形式传入，`text` 是要提交的目标地址（**必须百分号编码**） |
| 3 | Safari 打开该地址 → 系统唤起「快捷指令」App 并自动运行对应快捷指令 | 首次会提示「是否允许打开快捷指令」，同意即可 |
| 4 | 快捷指令内部若要从 Scheme 拿值：在第一个动作加 **获取 URL 内容** 或从 **共享表单 / 文本** 的输入取 `text` 参数 | 具体取值动作取决于 §3.1 第 1 步选用哪种输入动作 |

**URL Scheme 路径的两个已知限制**：

- Safari 的**分享菜单不能直接打开任意 URL Scheme**，必须借道地址栏、书签或中间 App；所以「Safari 分享 → 一步提交」这件事本身仍以路径 A 为准。
- `name` 必须与快捷指令名**完全一致**（含空格、大小写），否则系统找不到该快捷指令。

**为什么用 `shortcuts://` 而不是自建 `raccoon://`**：Raccoon 是服务端项目，不含 iOS 原生 App，无法注册自定义 URL Scheme；`shortcuts://` 是 iOS 系统自带能力，零依赖。若将来做了原生壳 App，可再换成 `raccoon://download?url=…`，届时只需改第 2 步的 Scheme 前缀，HTTP 部分完全不变。

### 3.3 两条路径对比

| 维度 | 路径 A（共享表单） | 路径 B（URL Scheme） |
| --- | --- | --- |
| 入口 | Safari 分享菜单，一步到位 | 地址栏 / 书签 / 其他 App 跳转 |
| 对 iOS 版本的要求 | 较新（共享表单在 iOS 15/16 逐步开放，Shortcuts App 内可直接选） | 较低（`shortcuts://` 长期可用） |
| 依赖第三方 App | 否 | 否 |
| 适合场景 | 「看到就下」的高频场景 | 自动化脚本、外部入口 |

---

## 4. 变体快捷指令

以下三条都从 §2 的骨架复制改造。**只读类变体需要 `read` scope（只读令牌即可）**。

### 4.1 变体一：查询任务状态

**用途**：拿到上一步提交的 `task_id`，查它现在什么状态。

| # | 动作 | 参数 |
| --- | --- | --- |
| 1 | **文本** | 输入任务 ID（可手工填，或从 §2 的 `data[].id` 接力传过来） |
| 2 | **获取 URL 内容** | URL = `http://<实例地址>/api/v1/tasks/<任务ID>`<br>方法 = **GET**<br>请求头 = `Authorization: Bearer rc_…` |
| 3 | **获取字典值** | 键 = `data` → 得到任务字典 |
| 4 | **获取字典值** | 键 = `status` → 状态码（`waiting` / `parsing` / `downloading` / `post` / `done` / `failed` / `cancelled`） |
| 5 | **获取字典值** | 键 = `progress` → 进度百分比（0–100 的数字） |
| 6 | **显示结果** 或 **朗读文本** | 建议拼成「任务 <状态>，进度 <n>%」再朗读 |

**响应体**（`docs/04` §2.2）：

```json
{
  "data": {
    "id": "task_01J…",
    "status": "downloading",
    "progress": 43.2,
    "speed_bps": 12400000,
    "wait_reason": null,
    "error_code": "",
    "error_message": "",
    "created_at": 1730000000000
  },
  "files": []
}
```

| 字段 | 语义 |
| --- | --- |
| `data.status` | 任务七态之一（`docs/01` §4） |
| `data.progress` | 进度百分比；**不确定时不要自己估**，直接显示服务端给的值 |
| `data.wait_reason` | 等待原因（如「需登录，去连接页更新 Cookie」），非空说明任务卡在等待态而非失败态 |
| `data.error_code` / `data.error_message` | 失败原因；配 `fix_hint` 给修复建议（如 `goto_credentials`） |
| `files[]` | 任务产物列表；`done` 后可据此拿 `files[].id` 去下载直链 |

**列表查询**（想一次看多个任务时用）：`GET http://<实例地址>/api/v1/tasks?status=downloading&limit=20`，返回 `{"items":[…],"next_cursor":"…","total":128,"counts":{…}}`。可用过滤参数：`status`（逗号分隔多选，如 `waiting,downloading`）、`platform`、`q`（标题搜索）、`kind`；分页参数 `limit`（上限 100，默认 50）与 `cursor`（`docs/04` §1.3）。

**取消 / 重试**（可选追加）：`POST /api/v1/tasks/<id>/cancel`、`POST /api/v1/tasks/<id>/retry`，同样只需 `Authorization` 头。取消幂等（终态任务返回 200 当前状态），重试仅对 `failed` / `cancelled` 有效，否则 409。

### 4.2 变体二：批量提交多行链接

**用途**：一次性提交多条链接（多行文本 / 备忘录里的一堆链接）。

| # | 动作 | 参数 |
| --- | --- | --- |
| 1 | **共享表单** | 勾选 **文本**（如果链接来自分享菜单）；若手工输入，用 **文本** 动作 |
| 2 | **拆分文本** | 分隔符 = **新行**；作用在动作 1 的输出上 → 得到链接数组 |
| 3 | **重复操作**（Repeat with Each） | 遍历动作 2 的每一项：<br>· **文本** 动作做去空白（`Trim`）<br>· 若为空则**继续下一项**（用 **如果** 动作判断）<br>· **字典**：键 `url` = 当前项<br>· **添加到变量**（把该字典追加进一个数组变量 `raccoonItems`） |
| 4 | **字典** | 键 `items` = 变量 `raccoonItems`（数组） |
| 5 | **获取 URL 内容** | URL / 方法 / 请求头 / 请求体同 §2.1 第 4 步，但请求体的 `items` 用动作 4 的数组 |
| 6 | **显示结果** | 同 §2.1 第 5–6 步 |

**批量 body 模板**：

```json
{
  "items": [
    { "url": "https://example.com/a.mp4", "quality": "", "save_path": "" },
    { "url": "https://example.com/b.mp4", "quality": "", "save_path": "" },
    { "url": "https://example.com/c.mp4", "quality": "720p", "save_path": "/media/tmp" }
  ]
}
```

**批量注意事项**：

| 项 | 说明 |
| --- | --- |
| 条数上限 | **单批最多 50 条**（`docs/04` §2.2）。超了要么分批提交，要么服务端返回 `400 VALIDATION` |
| 去重 | 同一批里的重复链接由服务端防重（`docs/04` §4）：重复项进 `conflicts[]`，`data[]` 只含真正新建的任务 |
| 逐条报告 | 部分成功时仍是 **201**；`data[]` 与 `conflicts[]` 分别说明成功与跳过的条目 |
| 空行/空白行 | 必须自己过滤，空串会被服务端判为「URL 不能为空」 |

### 4.3 变体三：查看媒体库搜索

**用途**：查已经落到实例上的文件（不是查任务）。

| # | 动作 | 参数 |
| --- | --- | --- |
| 1 | **询问输入**（可选） | 提示「搜索关键词」；也可用共享表单的文本 |
| 2 | **获取 URL 内容** | URL = `http://<实例地址>/api/v1/files?q=<URL 编码后的关键词>&limit=20`<br>方法 = **GET**<br>请求头 = `Authorization: Bearer rc_…` |
| 3 | **获取字典值** | 键 = `items` → 文件数组 |
| 4 | **重复操作** | 遍历每个文件：取 `name`、`size`、`platform`、`kind`，拼成一行 |
| 5 | **显示结果** | 或用 **朗读文本** 逐条播报 |

**响应体**（`docs/04` §2.8）：

```json
{
  "items": [
    {
      "id": "file_01J…",
      "task_id": "task_01J…",
      "path": "/media/movies/xxx.mp4",
      "name": "xxx.mp4",
      "size": 123456789,
      "mime": "video/mp4",
      "platform": "bilibili",
      "uploader": "…",
      "kind": "video",
      "previewable": true,
      "created_at": 1730000000000
    }
  ],
  "total": 128,
  "platforms": ["bilibili", "youtube"]
}
```

| 过滤参数 | 说明 |
| --- | --- |
| `q` | 文件名模糊匹配 |
| `platform` | 平台 |
| `uploader` | 上传者 |
| `kind` | `video` / `audio` / `image` / `other` |
| `limit` | 返回条数 |
| `offset` | 偏移分页（注意：媒体库列表用 `offset`，任务列表用 `cursor` 游标） |

**拿到文件后下载直链**：`GET http://<实例地址>/api/v1/files/<文件ID>/download`（响应带 `Content-Disposition`，路径经服务端校验必须在 data 根内）。因为这个端点需要 `Authorization` 头，**不能**直接用 Safari 打开裸 URL——请用「获取 URL 内容」动作取回，或在请求头带上 Token 后另存文件。

---

## 5. 错误处理

### 5.1 统一错误格式与判定方式

所有失败响应都是同一个信封（`docs/04` §1.2）：

```json
{
  "error": {
    "code": "UNAUTHORIZED",
    "message": "API Token 无效",
    "request_id": "req_01H…"
  }
}
```

**在快捷指令里判定状态码**：动作 4「获取 URL 内容」的输出对象里有 **响应状态码** 字段。用 **如果** 动作按状态码分支：

| 状态码 | code | 含义 | 快捷指令该怎么响应 |
| --- | --- | --- | --- |
| 201 / 200 | —— | 成功 | 读 `data`（必要时读 `conflicts`），显示任务 ID |
| 400 | `VALIDATION` | 参数校验失败 | 提示「请求被拒：字段有问题」，并把 `message` 念出来（消息里含具体字段） |
| 401 | `UNAUTHORIZED` | Token 无效 / 已撤销 / 已过期 | 见 §5.2 |
| 403 | `FORBIDDEN` | Token scope 不足 | 提示「令牌权限不足，需要「读写」范围」，去 §1.2 重签 |
| 404 | `NOT_FOUND` | 任务不存在或已删除 | 提示「任务不存在或已删除」 |
| 409 | `CONFLICT` | 重复提交 / 状态冲突 | 见 §5.3 |
| 428 | `SETUP_REQUIRED` | 实例未初始化管理员账号 | 提示「实例还没初始化，请先在 Web 界面完成初始化」 |
| 429 | `RATE_LIMITED` | 触发限流（Token 通道 120 次/分钟） | 提示「请求太频繁，稍后再试」；响应带 `Retry-After` 头 |
| 500 | `INTERNAL` | 服务内部错误 | 提示「服务端错误」，附 `request_id` 便于查日志 |
| 503 | `ENGINE_UNAVAILABLE` | yt-dlp 引擎缺失 / 外部服务未配置 | 提示「下载引擎不可用，实例侧需要修复」 |

### 5.2 Token 失效（401）

| 现象 | `message` 会出现 | 处理 |
| --- | --- | --- |
| Token 写错 / 复制时带空格 | 「API Token 无效」 | 重新复制；注意不要带首尾空格与换行 |
| Token 被撤销 | 「API Token 已被撤销」 | 回 Web 界面「设置 → API 令牌」看列表，签发新 Token 并替换快捷指令里的值 |
| Token 过期 | 「API Token 已过期」 | 重新签发（创建时可设有效期） |
| 改用其他用户的 Token | 「Token 所属用户不存在」 | 用实例管理员账号签发的 Token |

**快捷指令里的 Token 存放**：直接写进动作参数即可（存在快捷指令的配置文件里）。若不希望明文留档，可用 **「从剪贴板」** 动作读取 Token，或用 iOS 钥匙串 + **「文本」** 动作引用——但注意这两种做法在无人值守的自动化里不稳定。

**Token 轮换**：把 Token 放在一个固定的**文本动作**里（或一个单独存放 Token 的快捷指令，通过「运行快捷指令」传入），换 Token 时只改这一处。

### 5.3 任务重复提交（409）

| 场景 | 服务端行为 | 快捷指令该怎么响应 |
| --- | --- | --- |
| 整批都是重复链接 | **409 CONFLICT**，message「相同链接的任务已在进行中或队列中」 | 这是**防重**而非故障（`docs/04` §4）。文案建议：「该链接已在队列中，无需重复提交」 |
| 批内部分重复 | **201**，重复项落在 `conflicts[]`（含 `existing_task_id`），成功项落在 `data[]` | 读 `conflicts` 念出「已存在任务 <existing_task_id>，已跳过」 |
| 取消已终态的任务 | 200，返回当前状态 | 幂等，无需特殊处理 |
| 重试一个还在跑的任务 | 409，「仅失败或已取消的任务可以重试」 | 提示「只有失败或已取消的任务能重试」 |

> `docs/04` §2.2/§4 把 409 描述为「409 + `existing_task_id`」。**实操以返回体为准**：当前服务端在 201 响应的 `conflicts[]` 里给 `existing_task_id`；若将来 409 错误体也带上该字段，可在快捷指令里同时从 `error.existing_task_id` 与 `conflicts[].existing_task_id` 两处取。

### 5.4 CSRF：为什么快捷指令用 Bearer Token 就没事

| 环节 | 会话通道（Cookie） | Token 通道（快捷指令用这个） |
| --- | --- | --- |
| 凭证载体 | 浏览器自动带 `raccoon_session` Cookie | 动作里显式写 `Authorization: Bearer …` |
| 写操作额外校验 | **需要** `X-CSRF-Token` 头（登录时下发，存服务端内存） | **不需要** |
| 原因 | Cookie 会被浏览器自动附加，第三方站点可诱导浏览器发起带 Cookie 的写请求，故需额外 token 阻断（`docs/04` §1.1） | 请求**不携带任何 Cookie**，没有「被浏览器自动附加的凭证」这回事，跨站伪造无从成立 → `docs/04` §1.1 明文「Token 通道豁免 CSRF（无 Cookie 上下文）」 |
| 快捷指令可行性 | ❌ 快捷指令不发 Cookie，也拿不到内存里的 CSRF 值 | ✅ 只需一个静态字符串 Token |

**顺带说明**：Token 通道的写操作**同样受 scope 约束**——豁免的是 CSRF，不是鉴权。无 `write` scope 的 Token 调 `POST /api/v1/tasks` 依然 403。

---

## 6. 边界与注意事项

### 6.1 长度限制

| 限制 | 数值 | 说明与规避 |
| --- | --- | --- |
| 快捷指令文本 / URL 参数长度 | **保守按 4096 字符设计** | Apple 未对快捷指令各类输入框公布正式的字符上限，工程上按 4096 字符为安全上界来设计批量逻辑。**长链接（带大量 query 的网盘链接、播放列表）可能逼近或超过该上限** |
| 批量提交单批条数 | **50 条**（服务端硬校验） | 与字符上限无关的另一道闸；批量超过 50 条必须分批（见 §4.2） |
| 服务端请求体大小 | 1 MB（`httpx.DecodeJSON` 限流读取） | 50 条长链接仍可能顶到；遇到 `400 VALIDATION` 时先怀疑体长 |
| URL 本身长度 | 取决于目标站点 | 目标站不支持超长 URL 时，扩展/快捷指令都无法绕过；这是平台约束，不是 Raccoon 的 |

**4096 字符上限的工程建议**：

- 批量逻辑里**先切分再提交**：把链接数组按「累计长度 < 3000 字符」或「每批 ≤ 20 条」两个条件**同时**满足来分批（留出余量给 JSON 结构与转义开销）。
- 分批时给每批加序号提示（「第 1/3 批已提交 12 条」），避免用户以为漏提。
- 单条超长链接无法靠分批解决——只能换更短的分享链接。

### 6.2 URL 编码注意事项

编码分两个位置，规则**相反**，搞混就会提交出坏链接：

| 位置 | 要不要编码 | 怎么编码 | 原因 |
| --- | --- | --- | --- |
| **URL Scheme 传参**（`shortcuts://run-shortcut?…&text=<目标地址>`） | **必须**百分号编码 | 用快捷指令的 **URL 编码** 动作，或用 **URL 组件** 动作取值 | Query string 里 `&` `?` `=` `#` 会截断参数；`%` 需转义成 `%25` |
| **HTTP 请求头**（`Authorization`） | **不要**编码 | 原样写 `Bearer rc_…` | 头值不是 Query 参数，百分号编码会破坏凭证 |
| **JSON body 里的 `url` 字段值** | **只做 JSON 字符串转义**，**不要**做百分号编码 | JSON 层面只需转义 `"` `\` 和控制字符 | 百分号编码后，服务端拿到的是 `%3A%2F%2F…` 这样的字符串，不是真实地址，会被当成非法 URL 拒绝 |
| **媒体库搜索的 `q` 参数** | **必须**百分号编码 | 用 **URL 编码** 动作 | 它是 Query 参数值 |
| **实例地址本身** | 不要编码 | 直接写 `http://192.168.1.20:8530` | 编码后主机名解析会失败 |

**JSON 转义 vs 百分号编码的分工**（最容易踩的坑）：

```
原链接：https://example.com/watch?v=1&t=2

✅ 正确（JSON body 内）：
{ "items": [ { "url": "https://example.com/watch?v=1&t=2" } ] }

❌ 错误（对 body 内的 URL 做了百分号编码）：
{ "items": [ { "url": "https%3A%2F%2Fexample.com%2Fwatch%3Fv%3D1%26t%3D2" } ] }

✅ 正确（URL Scheme 传参，要编码）：
shortcuts://run-shortcut?name=Raccoon&input=text&text=https%3A%2F%2Fexample.com%2Fwatch%3Fv%3D1%26t%3D2
```

**快捷指令里做 JSON 的稳妥姿势**：用 **字典** 动作搭结构 → 用 **获取字典值** 动作转成 JSON 文本，而不是手写字符串再拼。**字典** 动作会自动做 JSON 转义，不会出现「链接里带引号把 JSON 撑破」的问题。**URL 编码** 动作只用在 Query 参数和 Scheme 传参上。

### 6.3 其他边界

| 边界 | 说明 |
| --- | --- |
| 内网地址被拒 | 服务端对任务入口做 SSRF 防护：仅 `http`/`https`、拒绝私网/环回/链路本地地址字面量（`docs/04` §5）。**这意味着从快捷指令提交 `http://192.168.x.x/...` 会被拒**——但把**目标媒体地址**指向内网是不允许的，实例地址本身当然可以是内网 |
| 限流 | API Token 120 次/分钟；解析端点 20 次/分钟（`docs/04` §5）。批量提交时别把请求打太密 |
| 明文 Token | Token 等同于实例的写权限，**别把快捷指令导出分享**给别人（导出的快捷指令含明文参数） |
| 首次授权提示 | iOS 会提示快捷指令访问网络，属于系统行为，正常放行即可 |
| 任务不可撤销 | 取消任务用 `POST /api/v1/tasks/{id}/cancel`；删任务记录与删文件是两个独立操作（`docs/01` §3.1、§3.5） |

---

## 7. 契约对照表（快捷指令动作 ↔ `docs/04`）

| 快捷指令动作 | 方法 | 路径 | 请求体 / 参数 | `docs/04` 出处 |
| --- | --- | --- | --- | --- |
| 获取 URL 内容（提交） | `POST` | `/api/v1/tasks` | body `{"items":[{"url","quality","save_path"}]}` | §2.2 |
| 获取 URL 内容（列任务） | `GET` | `/api/v1/tasks` | query `status` / `platform` / `q` / `kind` / `limit` / `cursor` | §1.3、§2.2 |
| 获取 URL 内容（任务详情） | `GET` | `/api/v1/tasks/{id}` | —— | §2.2 |
| 获取 URL 内容（取消） | `POST` | `/api/v1/tasks/{id}/cancel` | 无 body | §2.2 |
| 获取 URL 内容（重试） | `POST` | `/api/v1/tasks/{id}/retry` | 无 body | §2.2 |
| 获取 URL 内容（媒体库搜索） | `GET` | `/api/v1/files` | query `q` / `platform` / `uploader` / `kind` / `limit` / `offset` | §2.8 |
| 获取 URL 内容（文件直链） | `GET` | `/api/v1/files/{id}/download` | —— | §2.8 |
| 请求头（全部） | —— | —— | `Authorization: Bearer <token>` | §1.1 |
| 错误判定 | —— | —— | `{"error":{"code","message","request_id"}}` + HTTP 状态码 | §1.2 |
| 幂等语义 | —— | —— | 重复提交 → 409 或 `conflicts[]` | §4 |

**通用约定**：

| 项 | 值 |
| --- | --- |
| 基础路径 | `/api/v1` |
| 认证 | `Authorization: Bearer <token>`（Token 通道，豁免 CSRF） |
| 成功响应 | HTTP 200/201，body 为资源 JSON 或 `{"data": …}`；列表统一为 `{"items": […], "next_cursor": …, "total": …}` |
| 失败响应 | HTTP 4xx/5xx + 唯一错误信封 `{"error":{code,message,request_id}}` |
| 单批任务上限 | 50 |
| 任务状态机 | `waiting` / `parsing` / `downloading` / `post` / `done` / `failed` / `cancelled`（`docs/01` §4） |

---

## 8. 与浏览器扩展的关系

本契约与 `extension/` 目录下的浏览器扩展**共用同一套 API 契约**（同一份 `docs/04`），差别只在入口：

| 维度 | 浏览器扩展 | iOS 快捷指令 |
| --- | --- | --- |
| 认证 | `Authorization: Bearer <token>`，Token 存 `chrome.storage.local` | `Authorization: Bearer <token>`，Token 写在快捷指令动作里 |
| 提交入口 | 右键菜单 / 弹窗 | 共享表单 / URL Scheme |
| CSRF | 豁免（同为 Token 通道） | 豁免（同为 Token 通道） |
| 入队结果 | 同一队列、同一状态机 | 同一队列、同一状态机 |

> `docs/01` §3.6 的验收标准：「任意入口提交的任务出现在同一列表且状态一致」——本契约不引入任何独立逻辑，只是把同一份 HTTP 契约换个入口调用。

---

## 9. 首次搭建验收清单

搭完后逐条核对（**均为待验证项，本环境未实测**）：

| # | 检查 | 期望 |
| --- | --- | --- |
| 1 | 用只读令牌提交 | 收到 403 `FORBIDDEN`，消息提到 `write` |
| 2 | 用读写令牌提交一个真实链接 | 收到 201，`data[0].id` 非空，`status` = `waiting` |
| 3 | 立刻再提交同一个链接 | 收到 409，或 201 且 `conflicts[]` 里带 `existing_task_id` |
| 4 | 把 Token 改成一个错字符串 | 收到 401 `UNAUTHORIZED` |
| 5 | 用 `GET /api/v1/tasks` 查询 | 返回 `items` 数组，能看到刚才那个任务 |
| 6 | 提交 51 条链接 | 收到 400 `VALIDATION`（超过单批 50 条上限） |
| 7 | 提交一条 `http://192.168.1.20/xxx` 内网目标地址 | 被拒（SSRF 防护） |
| 8 | 提交一条带 `&`、`?`、`=` 的链接 | 任务 URL 与原始链接**逐字一致**（没有被百分号编码） |
| 9 | 媒体库搜索关键词含空格或中文 | 正确编码，命中预期文件 |
| 10 | 任务 `done` 后查详情 | `data.status` = `done`，`files[]` 非空 |

发现任何一条与期望不符：先核对 `docs/04` 的字段名与状态码，再回改本文档，**不要**在快捷指令侧绕过契约。
