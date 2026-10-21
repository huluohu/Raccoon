# 🦝 Raccoon（浣熊）· 自托管多媒体下载中心

> 功能对齐 SaveXTube Server，独立实现：**能力完整、部署简单、界面好用**。不设付费激活、设备数量限制或功能付费墙。
> 正名 **浣熊 / Raccoon**；中文别体 **浣浣**，英文别体 **Rocky**，命令行 `rc`。

## 状态

W0（规格与骨架）已交付：`docs/01–08` 详细设计 + 可编译双端骨架。开发按 `docs/08-开发计划.md` 的 Wave 推进。

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

容器内置 yt-dlp / FFmpeg；非 root 运行；健康检查 `GET /healthz`。

## 目录结构

```
Raccoon/
├── docs/                   # 详细设计（01 需求契约 / 02 架构 / 03 数据模型 / 04 API /
│                           #           05 任务系统 / 06 配置中心 / 07 前端落地 / 08 开发计划）
├── backend/                # Go 服务端
│   ├── cmd/raccoon/        # 入口
│   └── internal/
│       ├── config/         # 配置中心：注册表 + env 锁定
│       ├── server/         # HTTP：router + 中间件 + 路由
│       ├── store/          # SQLite 唯一数据出口 + 迁移 runner（migrations/*.sql）
│       ├── task/           # 任务模型与七态状态机（纯函数）
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

基于《整体方案 v2.4》与 renlixing87/savextube 公开资料整理 · 2026-10-21
