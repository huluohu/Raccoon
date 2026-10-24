# 16 · AI 助手接入（Agent Skill 指引）

> 把浣熊交给 AI 助手（Claude / 其他 Agent）代管：提交下载、盯进度、取回文件。
> 全部能力经 `rc` CLI（HTTP API 薄壳）完成——单一任务系统在 Agent 入口同样成立，
> Bot 不需要知道浣熊内部长什么样。

## 前置

1. 服务已运行（默认 `http://127.0.0.1:8530`）；
2. 在「设置 → API 令牌」签发一枚 scope=3（读写）令牌；
3. 一次性登录：

```bash
rc login --server http://127.0.0.1:8530 --token <令牌>
```

## 核心命令（Agent 最常用的五条）

```bash
# 提交（支持批量：多个链接一次入队；--no-preview 跳过预览直接下）
rc add "https://www.bilibili.com/video/BV..." --quality 1080p --no-preview

# 盯进度（--jsonl 每行一个 JSON，适合逐行解析）
rc list --jsonl
rc status <task-id>          # 单任务详情（含速度/剩余时间/错误与建议）

# 取回产物到本机目录
rc files <task-id> --save ./downloads

# 出错处理：先看错误与建议，再决定重试或换通道
rc status <task-id>          # error_code / fix_hint 字段给出下一步
# 换 115 离线通道重试（媒体流直连不通时的出路）
curl -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"kind":"n115"}' http://127.0.0.1:8530/api/v1/tasks/<id>/retry
```

## Agent 行为约定（建议写进 Skill 的规则）

- **提交前不猜测可下性**：把链接原样交给 `rc add`，失败原因由错误码翻译
  （`NET_TIMEOUT`=网络类可重试、`AUTH_REQUIRED`=缺凭据、`FORMAT_UNAVAILABLE`=换清晰度）。
- **轮询节奏**：`rc list --jsonl` 每 30s 一次足够；`--follow` 只用于用户明确「盯着看」。
- **重试纪律**：`NET_TIMEOUT` 类失败等自动重试耗尽（默认 3 次、退避 30s/2m/10m）再介入；
  介入优先级：立即重试 → 配代理 → 换 115 通道。
- **诚实报告**：`success_rate=null` 显示「—」，不要替用户编 0% 或 100%。

## JSON 契约速查

| 命令 | 字段要点 |
| --- | --- |
| `rc list --jsonl` | 每行一个任务对象：`id/status/progress/speed_bps/title` |
| `rc status <id> --json` | 含 `error_code/error_message/fix_hint/route_path/attempt/max_attempts` |
| `rc files <id> --json` | 产物数组：`id/path/size/kind`（`--save` 前先看尺寸） |

## 安全边界

- 令牌 scope 最小化（只读场景用 scope=1）；
- Agent 不应触碰 `config set`（服务行为变更留给人工）；
- webhook（`notify.webhook_url`）可以把任务终态推给 Agent 的接收端，替代轮询。

## MCP 接入（rc mcp）

> MCP（Model Context Protocol）是 Claude Desktop 等 MCP 客户端的标准接入协议。
> `rc mcp` 把上面这套 CLI 能力包装成 MCP 服务器：stdio 传输、JSON-RPC 2.0，
> 工具面与 HTTP API 同源（单一任务系统不变，docs/01 §5.1）。

### Claude Desktop 配置（`claude_desktop_config.json`）

```json
{
  "mcpServers": {
    "raccoon": {
      "command": "/path/to/rc",
      "args": ["mcp"],
      "env": {}
    }
  }
}
```

连接信息复用 `rc login` 的落盘配置（`~/.config/raccoon/credentials.json`）；
也可在 args 里显式指定：`rc mcp --server http://192.168.1.20:8530 --token rc_xxx`。

### 工具面（6 个）

| 工具 | 参数 | 说明 |
| --- | --- | --- |
| `raccoon_add` | `url`（必填）、`quality?`、`convert_to?`（mp4/mp3） | 提交下载任务 |
| `raccoon_list` | `status?`、`limit?`（默认 20） | 最近任务列表 |
| `raccoon_status` | `task_id` | 单任务详情（含错误与修复建议） |
| `raccoon_files` | `task_id` | 产物文件列表 |
| `raccoon_retry` / `raccoon_cancel` | `task_id` | 重试 / 取消 |

行为约定与上面的 Agent 纪律一致：不猜测可下性、轮询 ≥30s、重试先看错误码。
