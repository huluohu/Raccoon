# 🦝 Raccoon（浣熊）· 自托管多媒体下载中心

[![Docker Version](https://img.shields.io/docker/v/fooololo/raccoon-server?sort=semver&label=Docker%20Hub)](https://hub.docker.com/r/fooololo/raccoon-server)
[![Platforms](https://img.shields.io/badge/arch-amd64%20%7C%20arm64-blue)](https://hub.docker.com/r/fooololo/raccoon-server)
[![License](https://img.shields.io/badge/license-TBD-lightgrey)](#许可证)

自托管的一站式下载服务：粘贴链接（B站 / YouTube / 抖音 / X / 小红书等 yt-dlp、gallery-dl 支持的站点），
自动解析 → 入队 → 下载 → 后处理（格式转换 / 封面 / 元数据），Web 界面实时看进度，
Telegram Bot / 微信推送 / iOS 快捷指令 / CLI / MCP 多端触达，全部操作同一个任务队列。

> 设计立场：**能力完整、部署简单、界面好用**。不设付费激活、设备数量限制或功能墙；
> 数据全部落在本机 `/data`，凭据 AES-256-GCM 加密存储，无云端依赖。

## 功能特性

| 模块 | 能力 |
| --- | --- |
| 下载引擎 | yt-dlp（视频/直播回放/播放列表）、gallery-dl（图片/图集）按 URL 形态自动路由 |
| 凭据中心 | 平台 Cookie 加密托管、按任务自动注入；**B 站扫码登录**（手机扫码即用，免手抄 Cookie） |
| 任务系统 | 七态状态机、父子批量任务、失败自动重试（指数退避）、取消/优先级/换通道重试 |
| 后处理 | 任务级**格式转换**（MP4 优先无损 remux，不兼容才 H.264 转码；MP3 音轨提取）、封面图落盘、元数据内嵌（均可配置开关） |
| 外部通道 | qBittorrent 磁力/种子、115 云端离线、直播开播自动录制 |
| 多端入口 | Web / `rc` CLI / MCP（AI 助手）/ Telegram Bot / iOS 快捷指令 / 浏览器扩展 |
| 通知 | 站内通知中心、Webhook、**Server酱微信推送**（任务完成/失败/磁盘预警） |
| 订阅 | UP 主/收藏夹周期检查，新内容自动入队（防重复跨重启） |
| 媒体库 | 入库检索、直链/预览、批量管理 |
| 安全 | 登录 + 双通道鉴权（Cookie 会话 / Bearer Token + scope）、CSRF、SSRF 校验、路径穿越防护、SQL 全参数化、非 root 容器 |

## 快速开始

```bash
docker run -d --name raccoon --restart unless-stopped \
  -p 8530:8530 -v /opt/raccoon/data:/data -e TZ=Asia/Shanghai \
  fooololo/raccoon-server:1.0.0
```

浏览器打开 `http://<主机IP>:8530`：

1. 首次访问进入初始化向导，创建管理员账号；
2. 粘贴任意视频链接 → 解析预览 → 选清晰度/格式 → 加入队列；
3. 需要会员内容：连接页「B 站扫码登录」扫一下即可。

健康检查：`curl http://127.0.0.1:8530/healthz`。

完整部署（compose、NAS、反向代理与 HTTPS、数据备份、升级与回滚、排障）见
**[docs/17-部署指南.md](docs/17-部署指南.md)**。

### 能力边界（诚实声明）

- 会员/付费内容需要账号本身的权限：扫码登录解决的是「凭据怎么进来」，不是「绕过付费墙」；
- DRM 加密内容（Netflix、优酷/腾讯/爱奇艺大部分 VIP 剧）任何凭据都无法下载（不破解 DRM）；
- 各平台接口随时可能改版导致解析失败，届时升级镜像内引擎版本即可。

## 使用入口

| 入口 | 一句话 | 文档 |
| --- | --- | --- |
| Web 界面 | 全功能管理台 | docs/07 |
| `rc` CLI | `rc login` 一次，`rc add <url>` 提交、`rc list/status/files` 管理 | docs/16 |
| MCP | `rc mcp` 注册进 Claude 等 MCP 客户端，AI 助手代管下载 | docs/16 |
| Telegram Bot | 发链接即入队，进度/完成主动推送 | docs/04 |
| iOS 快捷指令 | Safari 分享 → 一键提交到家里实例（API 令牌通道） | docs/12 |
| 浏览器扩展 | Chrome/Edge 一键发送当前页 | `extension/` |

## 文档索引

| 文档 | 内容 |
| --- | --- |
| [docs/01](docs/01-需求规格与行为契约.md) | 需求规格与行为契约（六大模块 + 七态状态机 + 验收口径） |
| [docs/02](docs/02-架构详细设计.md) | 架构设计（ADR / 模块边界 / 安全 / 可观测） |
| [docs/03](docs/03-数据模型设计.md) | 数据模型（全量 DDL，与迁移同源） |
| [docs/04](docs/04-API详细设计.md) | HTTP API 契约（端点 + SSE + 错误码） |
| [docs/05](docs/05-任务系统详细设计.md) | 任务系统（调度 / 恢复 / 父子任务 / 后处理） |
| [docs/06](docs/06-配置中心详细设计.md) | 配置中心（五要素 / 三时机 / 环境变量锁定） |
| [docs/07](docs/07-前端设计落地.md) | 前端设计（设计 Token / 组件 / 交互铁律） |
| [docs/08](docs/08-开发计划.md) | 开发计划与验证门禁 |
| [docs/09](docs/09-Wave1-2交付与验证.md) / [docs/10](docs/10-Wave4-5交付与验收.md) | 分波交付与验收记录（含未实测清单） |
| [docs/12](docs/12-iOS快捷指令契约.md) | iOS 快捷指令契约（服务端侧已回归验证） |
| [docs/13](docs/13-系统性重构-增量PRD.md) – [docs/15](docs/15-系统性重构-增量设计.md) | 系统性重构 PRD 与设计 |
| [docs/16](docs/16-AI助手接入-Agent-Skill指引.md) | CLI / MCP / AI 助手接入指引 |
| [docs/17](docs/17-部署指南.md) | 部署指南（docker run / compose / 升级 / 备份 / 反代 / 排障） |

## 开发

环境要求：Go 1.20+、Node 20+、Docker（构建镜像）。

```bash
make verify          # 一键验证：gofmt / go vet / 后端编译 / 前端 typecheck+build / 设计符合度对账
make backend-test    # 后端全量测试（14 包）
make web-test        # 前端全量测试（vitest）
make web-dev         # 前端开发服务器 :5173（/api 代理到 8530）
make docker-build    # 本地构建镜像（raccoon-server:<VERSION>）
make docker-release  # 双架构构建并推 Docker Hub（需 docker login）
```

本地运行推荐 compose（`docker compose up -d --build`，端口 8530，数据落 `./data`）；
质量门禁详情见 docs/10。

## 目录结构

```
Raccoon/
├── docs/                   # 全量设计文档（01 需求契约 … 17 部署指南）
├── extension/              # 浏览器扩展（Chrome/Edge MV3，零依赖）
├── backend/                # Go 服务端
│   ├── cmd/raccoon/        # 服务入口；cmd/rc/ 为 CLI + MCP 服务器
│   └── internal/
│       ├── server/         # HTTP：路由 + 中间件 + SSRF/CSRF 校验
│       ├── task/           # 七态状态机 + 调度 + 凭据桥接
│       ├── engine/         # 引擎集：yt-dlp / gallery-dl + 格式转换
│       ├── bilibili/       # B 站扫码登录（passport 协议客户端）
│       ├── store/          # SQLite 唯一数据出口 + 版本化迁移
│       ├── credential/     # 凭据保管（AES-256-GCM）
│       ├── qb/ n115/ tgbot/ live/   # 外部通道与直播守护
│       └── config/ httpx/ logx/ ...
└── web/                    # Vue 3 + TypeScript 管理界面（pinia / vitest）
```

## 核心约定

- **单一任务系统**：所有入口操作同一队列、同一状态机，没有第二套任务系统；
- **状态即真相**：失败不美化、大小未知不估算、不支持的能力不说「能」；
- **配置五要素**：每项设置带说明/默认值/校验/敏感级/生效时机；
- **数据单目录**：`/data` 承载数据库、配置、凭据、日志与下载文件，备份恢复只认这一个目录。

## 已知限制

- 115 / qBittorrent / Telegram 按官方契约实现且有单测，但无真实账号的长期实战验证；
- iOS 快捷指令的服务端契约已由集成测试锁定，快捷指令 App 内的动作组装未真机实测；
- 会员内容的边界见上文「能力边界」。完整未实测清单见 docs/10 §6。

## 许可证

尚未选择开源许可证（License: TBD）。仓库公开仅供学习参考，在许可证确定前，
默认保留所有权利——如需引用或二次开发请先联系作者。
