# 04 · HTTP API 详细设计

> 契约即文件：本文档与 `internal/server/routes.go` 的路由表、handler 的入参出参同源维护。
> 基础路径 `/api/v1`；前后端分离开发期前端走 Vite 代理 `/api` → `127.0.0.1:8530`。

---

## 1. 通用规范

### 1.1 认证双通道

| 通道 | 机制 | 用途 |
| --- | --- | --- |
| Web 会话 | 登录后种 `raccoon_session` HttpOnly Cookie（SameSite=Lax）+ SQLite 持久化会话表（auth_sessions，重启不掉登录态） | Web GUI |
| API Token | `Authorization: Bearer <token>`（api_tokens 表，scope 位掩码） | HTTP API / CLI / 扩展 / 快捷指令 |

- 会话认证的状态变更请求（POST/PUT/DELETE）需 `X-CSRF-Token` 头（登录时下发，存内存）。
- 会话有效期（滑动续期）：登录页勾选「30 天内免登录」（`remember:true`）→ 30 天 + 持久化 Cookie；未勾选 → 24h + 浏览器会话 Cookie（关浏览器即登出）。会话落 `auth_sessions` 表，应用重启不清登录态。
- Token 通道豁免 CSRF（无 Cookie 上下文）。

### 1.2 统一响应

成功：HTTP 200/201，body 为资源 JSON 或 `{"data": ...}`；列表统一：

```json
{ "items": [ ... ], "next_cursor": "1730000000000:task_ab12", "total": 128 }
```

失败（唯一错误格式）：

```json
{ "error": { "code": "TASK_NOT_FOUND", "message": "任务不存在或已删除", "request_id": "req_01H..." } }
```

| HTTP | code | 含义 |
| --- | --- | --- |
| 400 | VALIDATION | 参数校验失败（details 逐字段） |
| 401 | UNAUTHORIZED | 未登录 / Token 无效 |
| 403 | FORBIDDEN | Token scope 不足 |
| 404 | NOT_FOUND | 资源不存在 |
| 409 | CONFLICT | 状态冲突（如重复提交、并发修改） |
| 428 | SETUP_REQUIRED | 未初始化管理账号（管理 API 全部拦此） |
| 429 | RATE_LIMITED | 超频（Retry-After 头） |
| 500 | INTERNAL | 未预期错误（message 不泄露内部细节） |
| 503 | ENGINE_UNAVAILABLE | 引擎二进制缺失 / 外部服务未配置 |

### 1.3 分页与过滤

- 游标分页：`?limit=50&cursor=<next_cursor>`，limit 上限 100，默认 50。
- 过滤按资源定义（如 tasks：`status=waiting,downloading` 逗号分隔多选；`q=` 标题搜索；`platform=`）。
- 排序固定 `created_at DESC, id DESC`（任务/通知），时间正序用于事件流。

---

## 2. 路由总表（约 60 端点）

### 2.1 认证与初始化 `auth`

| 方法 | 路径 | 说明 | 认证 |
| --- | --- | --- | --- |
| POST | /auth/status | `{setup_required: bool}` 决定前端进 /setup 还是 /login | 无 |
| POST | /auth/setup | 创建管理员（仅 users 空时可用）：username/password → 种会话 | 无 |
| POST | /auth/login | username/password → 会话 Cookie + csrf_token | 无 |
| POST | /auth/logout | 销毁会话 | 会话 |
| GET | /auth/me | 当前用户信息 | 任一 |
| PUT | /auth/password | 修改密码（旧密码 + 新密码） | 会话 |
| PUT | /auth/username | 修改用户名（新用户名 + 当前口令） | 会话 |

### 2.2 任务 `tasks`

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | /tasks | 列表：status/platform/source/q/kind 过滤，游标分页；附 `counts` 各状态计数（总览页用） |
| POST | /tasks | 创建：`{items:[{url,quality?,save_path?,convert_to?,options?}], preview:false, parent_id?}`；`preview:true` 不落库只返回解析结果。重复提交 409 + `existing_task_id`。`convert_to`：产物目标格式，缺省=源格式，可选 `mp4`（post 阶段优先无损 remux，编解码不兼容才 H.264 转码）/ `mp3`（提取音轨）；转换失败保留源格式并记 warn 事件，不推翻下载成功 |
| GET | /tasks/{id} | 详情（含子任务计数、当前配置版本、尝试次数、失败修复提示） |
| GET | /tasks/{id}/events | 事件流（结构化 + 引擎日志 warn/error），游标分页 |
| POST | /tasks/{id}/cancel | 取消（幂等：终态任务返回当前状态 200） |
| POST | /tasks/{id}/priority | 置顶/取消置顶（仅 waiting；`{"priority":0|1..100}`，调度按 priority DESC + created_at 领取） |
| POST | /tasks/{id}/summary | 一键调用 AI 服务生成已完成任务的结构化总结 |
| POST | /tasks/{id}/retry | 重试。body 可选：缺省 = 普通重试（failed/cancelled；waiting+auto_retry 为立即重试，跳过剩余退避）；`{"kind":"n115"}` = 手动换通道重试（本地下载失败转 115 离线，failed/auto_retry 可用；115 未配置 409 如实拒绝）；`{"proxy":"..."}` 或 `{"use_proxy":true}` = 手工切换代理重试。保留原配置版本；failed 起步 attempt+1 |
| DELETE | /tasks/{id} | 删除任务记录（不删文件；二次确认在前端；`?delete_files=1` 走文件删除端点） |
| POST | /tasks/batch | 批量：`{action: cancel/retry/delete, ids: []}` → `{succeeded:[], failed:[{id,reason}]}` |
| GET | /tasks/{id}/files | 产物列表 |
| DELETE | /tasks/{id}/files/{fileId} | 删除实际文件 + 索引（独立危险操作，服务端二次校验路径在 data 根内） |

### 2.3 解析预览 `parse`

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | /parse | `{url, proxy?}` → 预览卡数据：title/thumbnail/uploader/duration/formats[{quality,size|null}]/kind/is_playlist/proxy_used/route_note/entries(preview)。直连网络异常时自动切换 net.proxy 重试。不落库、不入队 |

### 2.4 配置中心 `config`

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | /config | 全量设置注册表（每项含五要素 + 当前值 + 是否 env 锁定），敏感值打码 |
| PUT | /config | 批量保存：`{settings:{key:value,...}}` → 逐项校验结果；含 `effective` 汇总（哪些即时/下个任务/需重启） |
| PUT | /config/{key} | 单项保存 |
| POST | /config/test-connection | `{target: qbittorrent/n115/telegram/proxy, ...params}` → `{ok, message, latency_ms}` |
| GET | /config/export | 导出配置 JSON（敏感项不含明文，单独勾选才含且预览明示） |
| POST | /config/import | 导入（先校验，冲突项列出 diff 待确认） |
| GET | /config/history | 配置历史（key/version/时间/操作人） |
| POST | /config/rollback | 回滚到指定版本（凭据变更不回滚普通配置） |

### 2.5 凭据中心 `credentials`

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | /credentials | 列表（不含密文，含健康状态） |
| POST | /credentials | 新建（cookie 文件内容/账号/token），加密存储 |
| GET | /credentials/{id} | 元信息 |
| PUT | /credentials/{id} | 更新内容或标签 |
| DELETE | /credentials/{id} | 删除 |
| POST | /credentials/{id}/check | 健康检查（能连通平台接口则 active→expiring/expired） |
| POST | /credentials/bilibili/qrcode/start | B 站扫码登录：申请二维码 → `{url, qrcode_key, expires_in}`（前端离线渲染二维码，约 3 分钟有效） |
| POST | /credentials/bilibili/qrcode/poll | 轮询扫码：`{qrcode_key}` → `{status: waiting_scan/scanned/success/expired}`；success 时登录 Cookie 已加密入凭据中心（响应只回凭据元信息，永不回明文） |

### 2.6 订阅 `subscriptions`

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | /subscriptions | 列表（含 next_check_at、最后检查结果） |
| POST | /subscriptions | 创建：source_url → 先预览（preview 字段） |
| GET | /subscriptions/{id} | 详情 + 统计（已下载/跳过/失败） |
| PUT | /subscriptions/{id} | 修改规则/间隔/质量/启用 |
| DELETE | /subscriptions/{id} | 删除（不删已下载文件） |
| POST | /subscriptions/{id}/check-now | 立即检查一次（同步返回本次发现数，任务异步入队） |
| GET | /subscriptions/{id}/items | 已发现内容列表（item_id/title/status/task_id） |

### 2.7 直播 `live-sources`

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | /live-sources | 列表（含当前状态 offline/live/recording） |
| POST | /live-sources | 新增（platform 必须在实测清单内，否则 422 + 说明） |
| PUT | /live-sources/{id} | 修改计划/质量/启用 |
| DELETE | /live-sources/{id} | 删除 |
| GET | /live-sources/{id}/recordings | 录制历史 |
| POST | /live-sources/{id}/record | 手动开始录制（创建 record 任务） |

### 2.8 媒体库 `files`

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | /files | 文件视角列表：platform/uploader/kind/q 过滤；`previewable` 标记 |
| GET | /files/{id}/download | 直链下载（Content-Disposition；路径必须在 data 根内） |
| GET | /files/{id}/preview | 浏览器可播编码返回 Range 播放，否则 415 + 提示 |
| POST | /files/{id}/convert | **二次格式转换 / 音频提取**：`{target: "mp3"|"mp4"}`，按需提取 MP3 或转码 MP4 并入库 |
| GET | /files/{id}/summary | 读取已缓存的 AI 结构化总结 |
| POST | /files/{id}/summary | 调用 AI 服务生成媒体文件结构化总结并持久化 |
| DELETE | /files/{id} | **删文件**（磁盘 + 索引；路径从 DB 取并二次校验在 data 根内）。与「删任务记录」是两个动作 |

### 2.9 通知 `notifications` ｜ 事件流 `events` ｜ 系统 `system`

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | /notifications | 列表（未读徽章计数） |
| POST | /notifications/read | `{ids:[]}` 或 `{all:true}` |
| GET | /events | **SSE**：`text/event-stream`，事件类型见 §3；断线 3s 重连（Last-Event-ID 补发） |
| GET | /system/health | 健康检查（进程/DB 可写/磁盘余量/引擎自检）；根路径 `/healthz` 为其别名，供容器 HEALTHCHECK |
| GET | /system/info | 版本、运行时长、引擎版本、架构 |
| POST | /system/restart | 优雅重启应用服务（非宿主容器）：停收新任务 → 当前任务让出 → 恢复队列 |
| GET | /system/diag/export | 诊断包（脱敏日志+配置快照+任务统计；导出前预览接口同路径带 `?preview=1`） |
| GET | /system/queue | 队列快照：各状态计数 + 运行中任务（总览页用） |

### 2.10 统计 / 监控 / 日志（仪表盘数据源，docs/14 §2、docs/15 §3.1）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | /system/stats/summary | 主页指标一次聚合：`{uptime_sec,total_downloads,today_downloads,week_downloads,success_rate,media_bytes,media_by_kind,sub_downloads,favorite_downloads,live_recording}` |
| GET | /system/stats/trend?days=7\|30 | 按日趋势 + 失败拆分：`{days,points:[{date,count,bytes}],failures:{by_platform,by_reason}}`；`points` 长度恒等于 days，末元素为本地今日；其它 days 值 400 |
| GET | /system/monitor | 实时资源：`{ok,sampled_at,cpu,memory,disk,net,series}`（2s×60 点；`null` = 该时刻采集失败，前端断线不插值）；非 Linux 返回 `{ok:false,message}` |
| GET | /system/monitor/history?range=1h\|24h | 历史聚合：`{range,granularity_sec,points:[{ts,cpu,memory,disk,net_rx,net_tx}]}`；1h=60s 粒度、24h=1440s 均值；空窗返回 `[]` |
| GET | /logs | 运行日志：`level`（逗号多选）/`module`/`q`（≤256 截断）/`from`/`to`/`limit`（≤500）/`cursor`（行号）→ `{items:[{ts,level,module,msg}],next_cursor}`；`msg` 出口统一脱敏（cookie/token/password → `***`） |

口径（与实现同源）：

- `today/week` = `status='done'` 且 `finished_at` 落在**服务端本地时区**自然日/自然周（周一为首日）；
- `success_rate` = done/(done+failed)，**分母 0 时返回 `null`**（前端显示「—」，不造 0%/100%）；
- `favorite_downloads` 为**快照口径**：订阅删除后历史任务仍计入（`tasks.sub_favorite` 标记）。

### 2.11 API Token `tokens`

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | /tokens | 列表（不含明文，只有前缀） |
| POST | /tokens | 创建：`{name,scope,expires_in_hours}` → 明文只在此次响应出现一次 |
| DELETE | /tokens/{id} | 撤销（幂等） |

### 2.12 外部集成 `integrations`

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | /integrations/status | qB / 115 / Telegram 配置态与连通性汇总 |
| POST | /integrations/qb/test | qB 连通性测试 |
| POST | /integrations/n115/test | 115 连通性测试 |
| GET | /integrations/telegram/status | Telegram bot 状态 |

### 2.13 用户偏好 `user/preferences`

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | /user/preferences | `{"data":{"layout":"{\"hidden\":[],\"order\":[]}"}}`——`layout` 是 JSON **字符串**；无记录时为空串（前端回落默认布局） |
| PUT | /user/preferences | body `{"layout":"..."}`；校验合法 JSON + ≤64KB，非法 400；走 CSRF |

---

## 3. SSE 事件契约（进度即真相）

```
event: task.status    data: {"task_id":"...","status":"downloading","wait_reason":null,"ts":...}
event: task.progress  data: {"task_id":"...","progress":43.2,"speed_bps":12400000,"eta_sec":82,"downloaded_size":...}
event: task.done      data: {"task_id":"...","status":"done","files":[{"path":"...","size":...}]}
event: task.failed    data: {"task_id":"...","status":"failed","error_code":"COOKIE_EXPIRED","fix_hint":{"action":"goto_credentials","label":"去连接页更新"}}
event: notif.new      data: {"id":"...","kind":"task_done","severity":"success","title":"...","body":"..."}
event: system.warn    data: {"code":"DISK_LOW","message":"剩余 8%，将暂停新下载","free_bytes":...}
event: config.changed data: {"keys":["download.quality"],"effective":"next_task"}
```

规范：

- 所有状态以事件为准，前端不本地推算；断连期间用 `GET /tasks` 全量对账。
- 事件即任务事件的广播投影（同一数据源，无第二状态）。
- 心跳 25s 一条注释帧，防中间层断链。

## 4. 幂等与防重

| 操作 | 幂等键 | 行为 |
| --- | --- | --- |
| POST /tasks | dedupe_key（DB 唯一部分索引兜底） | 已存在活跃同 key → 409 + existing_task_id |
| POST /tasks/batch | 逐个执行报告 | 单条失败不影响其余 |
| POST /subscriptions/{id}/check-now | 运行中检查锁（单订阅单飞） | 已在检查 → 200 返回「检查进行中」 |
| 取消/重试 | 终态检查 | 重复请求返回当前态 200 |
| n115 提交 | info_hash | 提交-查询闭环，避免两边重复下载 |

## 5. 安全实现要点（对齐方案 §11 与 docs/02 §5）

- SSRF 防护在 `POST /parse` 与任务创建入口双重生效：协议白名单、私网/环回/链路本地地址拒绝、重定向逐跳复查 ≤5 跳；用户配置的 HTTP 代理地址单独白名单。
- `DELETE /tasks/{id}/files/{fileId}`：服务端重新解析文件真实路径并校验 `storage.SafeJoin` 前缀，不信前端传路径。
- 导出/诊断包：脱敏器强制扫描（cookie/token/password/key 字段名 → `***`），预览接口先行。
- 限流：登录 5 次/分钟/IP；API Token 120 次/分钟；解析端点 20 次/分钟（引擎子进程昂贵）。
- 全部外部输入走结构体校验（`encoding/json` + 手写 validate），错误信息定位到字段。

## 6. CLI 映射（rc）

```
rc login --server http://127.0.0.1:8530 --token <t>
rc add <url> [--quality 1080p] [--path /media/xx] [--no-preview]
rc list [--status downloading] [--json]
rc status <task-id> [--follow]          # --follow 走 SSE
rc cancel <task-id> | rc retry <task-id>
rc config get <key> | rc config set <key> <value>
rc diag export -o out.zip
```

CLI 是 HTTP API 的薄壳（同契约同错误码），不单独实现业务逻辑——保证「单一任务系统」在入口层也不破。
