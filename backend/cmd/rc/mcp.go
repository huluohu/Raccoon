// mcp.go：rc mcp —— MCP（Model Context Protocol）服务器子命令。
//
// 把浣熊的下载能力交给 AI 助手（Claude Desktop / 其他 MCP 客户端）代管：
// stdio 传输、JSON-RPC 2.0，工具面就是 HTTP API 的投影（单一任务系统不变，
// docs/01 §5.1）。配置方式与 docs/16 相同：先 rc login，再在 MCP 客户端里
// 把 `rc mcp` 注册为命令。
//
// 协议实现是手写的最小面：initialize / notifications/* / ping / tools/list /
// tools/call 五个方法即覆盖主流客户端的接入需求，不引 SDK 依赖。
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// mcpOverrideServer/mcpOverrideToken --server/--token 覆盖（不落盘）。
var (
	mcpOverrideServer string
	mcpOverrideToken  string
)

// JSON-RPC 信封。
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func cmdMCP(args []string) error {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--server", "-s":
			i++
			if i >= len(args) {
				return fmt.Errorf("--server 缺参数")
			}
			mcpOverrideServer = strings.TrimRight(args[i], "/")
		case "--token", "-t":
			i++
			if i >= len(args) {
				return fmt.Errorf("--token 缺参数")
			}
			mcpOverrideToken = args[i]
		default:
			return fmt.Errorf("未知参数: %s（用法: rc mcp [--server <url>] [--token <token>]）", args[i])
		}
	}
	// MCP stdio 传输是协议通道：任何日志只能走 stderr，stdout 必须只有 JSON-RPC。
	return serveMCP(os.Stdin, os.Stdout, os.Stderr)
}

// serveMCP 主循环（in/out 显式传入便于测试）。
func serveMCP(in io.Reader, out, errW io.Writer) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	enc := json.NewEncoder(out)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			// 坏行按 JSON-RPC 规范回 parse error（id=null）。
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0", "id": nil,
				"error": map[string]any{"code": -32700, "message": "parse error"},
			})
			continue
		}
		if msg.ID == nil {
			continue // notification：无需响应
		}
		result, rpcErr := handleMCP(msg)
		resp := map[string]any{"jsonrpc": "2.0", "id": msg.ID}
		if rpcErr != nil {
			resp["error"] = rpcErr
		} else {
			resp["result"] = result
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

func handleMCP(msg rpcMessage) (any, map[string]any) {
	switch msg.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "raccoon", "version": version},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": mcpTools()}, nil
	case "tools/call":
		return mcpToolsCall(msg.Params)
	}
	return nil, map[string]any{"code": -32601, "message": "method not found: " + msg.Method}
}

// mcpToolDesc 工具描述（JSON Schema 的最小面）。
type mcpToolDesc struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func mcpTools() []mcpToolDesc {
	str := func(desc string) map[string]any {
		return map[string]any{"type": "string", "description": desc}
	}
	obj := func(props map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "properties": props, "required": required}
	}
	return []mcpToolDesc{
		{
			Name:        "raccoon_add",
			Description: "提交下载任务（支持 B站/YouTube/抖音/X 等站点的链接；会员内容需已在「连接」页配置凭据）",
			InputSchema: obj(map[string]any{
				"url":        str("要下载的链接"),
				"quality":    str("清晰度：best/1080p/720p/480p/360p/audio，留空用默认"),
				"convert_to": str("产物格式：mp4 / mp3，留空保持源格式"),
				"no_preview": map[string]any{"type": "boolean", "description": "跳过解析预览直接入队（默认 false）"},
			}, "url"),
		},
		{
			Name:        "raccoon_list",
			Description: "列出最近任务（状态/进度/错误）",
			InputSchema: obj(map[string]any{
				"status": str("按状态过滤：waiting/parsing/downloading/post/done/failed/cancelled"),
				"limit":  map[string]any{"type": "integer", "description": "返回条数（默认 20，上限 100）"},
			}),
		},
		{
			Name:        "raccoon_status",
			Description: "查询单个任务详情（含速度/剩余时间/错误与修复建议）",
			InputSchema: obj(map[string]any{"task_id": str("任务 ID")}, "task_id"),
		},
		{
			Name:        "raccoon_files",
			Description: "列出任务产物文件（路径/大小/类型）",
			InputSchema: obj(map[string]any{"task_id": str("任务 ID")}, "task_id"),
		},
		{
			Name:        "raccoon_retry",
			Description: "重试失败/取消的任务",
			InputSchema: obj(map[string]any{"task_id": str("任务 ID")}, "task_id"),
		},
		{
			Name:        "raccoon_cancel",
			Description: "取消进行中的任务",
			InputSchema: obj(map[string]any{"task_id": str("任务 ID")}, "task_id"),
		},
	}
}

// mcpToolsCall 工具调用分发：全部转 HTTP API（CLI 同源，不另起业务逻辑）。
func mcpToolsCall(params json.RawMessage) (any, map[string]any) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, map[string]any{"code": -32602, "message": "params 解析失败: " + err.Error()}
	}
	var args map[string]any
	if len(p.Arguments) > 0 {
		_ = json.Unmarshal(p.Arguments, &args)
	}
	argStr := func(k string) string { s, _ := args[k].(string); return s }

	var (
		method, path string
		body         any
		out          any
	)
	switch p.Name {
	case "raccoon_add":
		item := map[string]any{"url": argStr("url")}
		if q := argStr("quality"); q != "" {
			item["quality"] = q
		}
		if c := argStr("convert_to"); c != "" {
			item["convert_to"] = c
		}
		method, path, body = http.MethodPost, "/api/v1/tasks", map[string]any{
			"items": []any{item}, "preview": false,
		}
		out = map[string]any{}
	case "raccoon_list":
		limit := 20
		if n, ok := args["limit"].(float64); ok && n > 0 && n <= 100 {
			limit = int(n)
		}
		path = fmt.Sprintf("/api/v1/tasks?limit=%d", limit)
		if st := argStr("status"); st != "" {
			path += "&status=" + st
		}
		method = http.MethodGet
		out = map[string]any{}
	case "raccoon_status":
		if argStr("task_id") == "" {
			return nil, map[string]any{"code": -32602, "message": "task_id 不能为空"}
		}
		method, path = http.MethodGet, "/api/v1/tasks/"+argStr("task_id")
		out = map[string]any{}
	case "raccoon_files":
		if argStr("task_id") == "" {
			return nil, map[string]any{"code": -32602, "message": "task_id 不能为空"}
		}
		method, path = http.MethodGet, "/api/v1/tasks/"+argStr("task_id")+"/files"
		out = map[string]any{}
	case "raccoon_retry":
		method, path = http.MethodPost, "/api/v1/tasks/"+argStr("task_id")+"/retry"
	case "raccoon_cancel":
		method, path = http.MethodPost, "/api/v1/tasks/"+argStr("task_id")+"/cancel"
	default:
		return nil, map[string]any{"code": -32602, "message": "未知工具: " + p.Name}
	}
	if err := call(method, path, body, &out); err != nil {
		// 工具执行失败按 MCP 语义回 isError 内容（不是协议层错误）。
		return mcpText("调用失败: "+err.Error(), true), nil
	}
	return mcpJSON(out), nil
}

// mcpText 纯文本工具结果。
func mcpText(text string, isErr bool) map[string]any {
	return map[string]any{
		"content": []any{map[string]any{"type": "text", "text": text}},
		"isError": isErr,
	}
}

// mcpJSON 把结构化结果序列化成文本内容（MCP 工具结果统一文本通道；
// 客户端与模型都按 JSON 读）。
func mcpJSON(v any) map[string]any {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcpText(fmt.Sprintf("%v", v), false)
	}
	return mcpText(string(raw), false)
}
