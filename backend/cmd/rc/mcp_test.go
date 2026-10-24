package main

// mcp_test.go：rc mcp 的 stdio JSON-RPC 会话测试。
//
// 起一个假 Raccoon API（httptest）承接工具调用，从管道喂请求行、
// 断言响应行——覆盖 initialize / tools/list / tools/call（成功与失败）
// 与 parse error。不测真服务：这里测的是 MCP 协议面与参数投影。

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeRaccoonAPI(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items":[{"id":"tsk1","url":"https://x/v1","status":"waiting","progress":0}],"counts":{}}`))
			return
		}
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"tsk1","url":"https://x/v1"}],"conflicts":[]}`))
	})
	mux.HandleFunc("/api/v1/tasks/tsk1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"tsk1","title":"t","status":"downloading","progress":42.5}}`))
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// rpcRoundtrip 喂多行请求，收全部响应（按序）。
func rpcRoundtrip(t *testing.T, lines []string) []map[string]any {
	t.Helper()
	ts := fakeRaccoonAPI(t)
	oldS, oldT := mcpOverrideServer, mcpOverrideToken
	mcpOverrideServer, mcpOverrideToken = ts.URL, "rc-test-token"
	t.Cleanup(func() { mcpOverrideServer, mcpOverrideToken = oldS, oldT })

	in := strings.NewReader(strings.Join(lines, "\n") + "\n")
	var out strings.Builder
	if err := serveMCP(in, &out, io_discard()); err != nil {
		t.Fatalf("serveMCP 失败: %v", err)
	}
	var resps []map[string]any
	sc := bufio.NewScanner(strings.NewReader(out.String()))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("响应不是合法 JSON: %q（%v）", line, err)
		}
		resps = append(resps, m)
	}
	return resps
}

func io_discard() *strings.Builder { return &strings.Builder{} }

func TestMCPInitialize(t *testing.T) {
	resps := rpcRoundtrip(t, []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
	})
	if len(resps) != 1 {
		t.Fatalf("initialize 应回 1 条响应（notification 不回），实际 %d", len(resps))
	}
	res := resps[0]["result"].(map[string]any)
	info := res["serverInfo"].(map[string]any)
	if info["name"] != "raccoon" {
		t.Fatalf("serverInfo.name 应为 raccoon: %v", info)
	}
}

func TestMCPToolsList(t *testing.T) {
	resps := rpcRoundtrip(t, []string{
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	})
	tools := resps[0]["result"].(map[string]any)["tools"].([]any)
	if len(tools) < 6 {
		t.Fatalf("工具应 ≥6 个，实际 %d", len(tools))
	}
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"raccoon_add", "raccoon_list", "raccoon_status", "raccoon_files", "raccoon_retry", "raccoon_cancel"} {
		if !names[want] {
			t.Fatalf("工具 %s 缺失: %v", want, names)
		}
	}
}

func TestMCPCallAdd(t *testing.T) {
	resps := rpcRoundtrip(t, []string{
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"raccoon_add","arguments":{"url":"https://x/v1","convert_to":"mp4"}}}`,
	})
	res := resps[0]["result"].(map[string]any)
	if res["isError"] == true {
		t.Fatalf("add 调用不应失败: %v", res)
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "tsk1") {
		t.Fatalf("add 结果应包含任务 ID: %s", text)
	}
}

func TestMCPCallUnknownTool(t *testing.T) {
	resps := rpcRoundtrip(t, []string{
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"nope","arguments":{}}}`,
	})
	if resps[0]["error"] == nil {
		t.Fatalf("未知工具应回协议错误: %v", resps[0])
	}
}

func TestMCPParseError(t *testing.T) {
	resps := rpcRoundtrip(t, []string{`{not-json`})
	if resps[0]["error"] == nil {
		t.Fatalf("坏行应回 parse error: %v", resps[0])
	}
}
