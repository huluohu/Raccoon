# 🦝 Raccoon（浣熊）· 自托管多媒体下载中心

> 功能对齐 SaveXTube Server，独立实现：**能力完整、部署简单、界面好用**。不设付费激活、设备数量限制或功能付费墙。
> 正名 **浣熊 / Raccoon**；中文别体 **浣浣**，英文别体 **Rocky**，命令行 `rc`。

## 状态

**Wave 1–5 全部交付**（截至 2026-10-22，基线 commit `cc410ee`）：平台引擎（yt-dlp / gallery-dl /
NCM）、外部通道（qBittorrent / 115 离线 / Telegram Bot）、浏览器扩展、iOS 快捷指令契约、
批量父子任务、安全收口（SSRF/穿越/注入/越权/CSRF）、双架构 CI。
验收证据与未实测清单见 **[docs/10-Wave4-5交付与验收.md](docs/10-Wave4-5交付与验收.md)**。

质量门禁（CI 同款，均可新机复现）：

```bash
cd backend && go build -buildvcs=false ./... && go vet -buildvcs=false ./... && go test -buildvcs=false -count=1 ./...
cd web && npm ci && npm run build && npm test
docker build -t raccoon:verify . && docker run -d -p 8530:8530 raccoon:verify && curl -fsS http://127.0.0.1:8530/healthz
```

## 功能地图

| 模块 | 能力 |
| --- | --- |
| 下载引擎 | yt-dlp（视频/直播回放/播放列表）、gallery-dl（图片/图集站点）按 URL 形态自动路由 |
| 音乐 | 网易云 NCM 容器解密；Apple Music / YT Music 平台标识路由 |
| 外部通道 | qBittorrent 磁力/种子任务、115 云端离线、Telegram Bot 提交/查询/通知（与 Web 同队列同状态） |
| 任务系统 | 七态状态机、父子批量任务（父卡聚合进度 + 失败子项单独重试）、断点续传、取消/重试 |
| 媒体库 | 入库检索、直链/预览、批量管理 |
| 凭据中心 | AES-256-GCM 加密存储，按平台自动注入引擎 cookie 文件（0600 临时文件，任务结束即删） |
| 安全 | 登录/双通道鉴权（Cookie 会话 + Bearer Token）、CSRF、Token scope（read/write/admin）、SSRF 校验、路径穿越与软链逃逸防护、SQL 全参数化 |
| 部署 | 单容器单端口、非 root、SQLite 单文件、版本化迁移（checksum 防篡改） |

**已知限制（未实测项）**：115/qB/Telegram 按官方契约实现并有单测，但无真实账号/实例实战验证；
gallery-dl 需登录站点、网易云在线直链、iOS 快捷指令真机、浏览器扩展真实加载均未实测。
完整清单与影响见 docs/10 §6 —— **不冒充已验证**。

## 快速开始（开发态）

```bash
# 后端（Go 1.20+，无 cgo 依赖）
make backend-build          # 编译 backend/cmd/raccoon
make backend-run            # 数据目录 ./data，监听 :8530

# 前端（Node 20+）
make web-install            # 首次
make web-dev                # Vite dev server :5173（/api 代理到 8530）

# 一键验证
make verify                 # go build/vet + 前端 typecheck/build
```

## Docker 部署

```bash
docker build -t raccoon:0.1.0 .
docker compose up -d        # 单容器单端口 8530，数据目录 ./data，可选媒体库 ./media
```

容器内置 yt-dlp 2025.01.15 / gallery-dl 1.32.17 / FFmpeg；非 root（uid 1000）运行；
健康检查 `GET /healthz`（含 db 与磁盘占用）。CI 产出 `linux/amd64,linux/arm64`
双架构镜像推送 ghcr.io。升级/备份/回滚步骤见 docs/10 §5。

## 目录结构

```
Raccoon/
├── docs/                   # 详细设计（01 需求契约 / 02 架构 / 03 数据模型 / 04 API /
│                           #           05 任务系统 / 06 配置中心 / 07 前端落地 / 08 开发计划）
│                           #           09–10 交付与验收 / 12 iOS 快捷指令契约
├── extension/              # 浏览器扩展（Chrome/Edge MV3，零依赖）
├── backend/                # Go 服务端
│   ├── cmd/raccoon/        # 入口
│   └── internal/
│       ├── config/         # 配置中心：注册表 + env 锁定
│       ├── server/         # HTTP：router + 中间件 + 路由 + SSRF 校验
│       ├── store/          # SQLite 唯一数据出口 + 迁移 runner（migrations/*.sql）
│       ├── task/           # 任务模型与七态状态机 + 外部通道执行器 + 凭据桥接
│       ├── engine/         # 引擎集：yt-dlp / gallery-dl / NCM + URL 形态路由
│       ├── qb/             # qBittorrent WebUI API 客户端
│       ├── n115/           # 115 开放平台客户端（设备码授权 + 离线调度）
│       ├── tgbot/          # Telegram Bot 长轮询客户端
│       ├── credential/     # 凭据保管（AES-256-GCM）
│       ├── storage/        # SafeJoin / 目录管理
│       ├── httpx/          # JSON 响应 / 错误信封
│       ├── logx/           # 结构化日志 + 脱敏
│       └── web/            # 前端产物内嵌（dist 由 CI 拷贝）
└── web/                    # Vue 3 + TypeScript 管理界面
    └── src/
        ├── styles/         # tokens.css（全站唯一色值来源）+ base.css
        ├── api/            # API client + 类型（与 docs/04 同源）
        ├── stores/         # pinia
        ├── router/         # 七个一级页面
        └── views/          # 页面
```

## 核心约定

- **单一任务系统**：Web / Telegram / API / CLI 所有入口操作同一队列、同一状态机（七态：等待/解析/下载/后处理/完成/失败/取消）。
- **状态即真相**：失败不美化、大小未知不估算、不支持不说「能」。
- **配置五要素**：每项设置有说明/默认值/校验/敏感级/生效时机（即时/下个任务/需重启）。
- **数据单目录**：`/data` 一个挂载承载数据库、配置、凭据、日志与下载文件。

## 文档索引

| 文档 | 内容 |
| --- | --- |
| docs/01 | 需求规格与行为契约（六大模块 + 七态 + 验收口径） |
| docs/02 | 架构详细设计（ADR / 模块边界 / 安全 / 可观测） |
| docs/03 | 数据模型（全量 DDL，迁移同源） |
| docs/04 | HTTP API 契约（约 60 端点 + SSE + CLI 映射） |
| docs/05 | 任务系统（状态机 / 调度 / 恢复 / 父子任务） |
| docs/06 | 配置中心（五要素 / 三时机 / env 锁定） |
| docs/07 | 前端设计落地（浣熊 Token / 组件 / 页面 / 交互铁律） |
| docs/08 | 开发计划（Wave 1–5 + 验证门禁） |
| docs/09 | Wave 1–2 交付与验证 |
| docs/10 | **Wave 4–5 交付与验收**（质量门禁/异常矩阵/升级回滚/未实测清单） |
| docs/12 | iOS 快捷指令契约 |

基于《整体方案 v2.4》与 renlixing87/savextube 公开资料整理 · 2026-10-21
