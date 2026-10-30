// Command rc 是 Raccoon 的命令行客户端（docs/04 §6）。
//
// 设计原则：CLI 是 HTTP API 的薄壳，不单独实现业务逻辑——保证「单一任务系统」
// 在入口层也不破（docs/01 §5.1）。
//
// 用法：
//
//	rc login --server http://127.0.0.1:8530 --token <t>
//	rc add <url> [--quality 1080p] [--kind qb|n115] [--path /media/xx] [--no-preview] [--json]
//	rc list [--status downloading] [--json]
//	rc status <task-id> [--follow] [--json]
//	rc cancel <task-id> | rc retry <task-id>
//	rc config get <key> | rc config set <key> <value>
//	rc diag export -o out.zip
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// version CLI 版本（构建注入）。
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "login":
		err = cmdLogin(args)
	case "add":
		err = cmdAdd(args)
	case "list":
		err = cmdList(args)
	case "status":
		err = cmdStatus(args)
	case "files":
		err = cmdFiles(args)
	case "cancel":
		err = cmdCancelRetry(args, "cancel")
	case "retry":
		err = cmdCancelRetry(args, "retry")
	case "config":
		err = cmdConfig(args)
	case "diag":
		err = cmdDiag(args)
	case "mcp":
		err = cmdMCP(args)
	case "help", "-h", "--help":
		usage()
		return
	case "version", "--version":
		fmt.Println("rc " + version)
		return
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误: "+err.Error())
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`rc — Raccoon 命令行客户端

用法:
  rc login --server <url> --token <token>     保存服务地址与 API Token
  rc add <url>... [选项]                      提交下载任务（多链接 = 批量）
       --quality <档位>   清晰度（best/2160p/1440p/1080p/720p/480p/360p/audio）
       --kind <通道>      下载通道：qb（qBittorrent）/ n115（115 网盘）/ download（本地下载）
       --path <目录>      保存目录（相对数据根）
       --no-preview       跳过解析预览直接入队
       --json             输出 JSON
       --jsonl            每行一个 JSON（批量结果流式输出）
  rc list [--status <状态>] [--platform <平台>] [--json] [--jsonl]
       --jsonl           每行一个 JSON 对象（流式消费/管道友好）
  rc status <task-id> [--follow] [--json]    详情；--follow 跟随 SSE 实时输出
  rc files <task-id> [--save <目录>]           列出/取回任务产物
  rc files <task-id> [--save <目录>] [--json]
       列出任务产物；--save 下载到本机目录（默认当前目录）
  rc cancel <task-id> | rc retry <task-id>
  rc config get <key> | rc config set <key> <value>
  rc diag export -o <file>
  rc mcp [--server <url>] [--token <token>]
       MCP 服务器（stdio）：把浣熊交给 AI 助手代管，工具面与 HTTP API 同源。
       MCP 客户端里把命令注册为 rc mcp 即可（详见 docs/16）。

配置:
  服务地址与 Token 存于 ~/.config/raccoon/credentials.json（权限 0600）。
`)
}

// ---- 配置存取 ----

type cliConfig struct {
	Server string `json:"server"`
	Token  string `json:"token"`
}

func configPath() string {
	dir := os.Getenv("RACCOON_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		dir = filepath.Join(home, ".config", "raccoon")
	}
	return filepath.Join(dir, "credentials.json")
}

func loadConfig() (*cliConfig, error) {
	raw, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &cliConfig{Server: "http://127.0.0.1:8530"}, nil
		}
		return nil, err
	}
	var c cliConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("配置文件损坏: %w", err)
	}
	return &c, nil
}

func saveConfig(c *cliConfig) error {
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

// ---- HTTP 封装 ----

// apiError 服务端错误信封。
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// call 发请求；Bearer Token 通道（免 CSRF）。
func call(method, path string, body any, out any) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	// mcp 子命令的显式覆盖（不落盘）。
	if mcpOverrideServer != "" {
		cfg.Server = mcpOverrideServer
	}
	if mcpOverrideToken != "" {
		cfg.Token = mcpOverrideToken
	}
	if cfg.Server == "" {
		return fmt.Errorf("未配置服务地址，请先执行: rc login --server <url> --token <token>")
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, strings.TrimRight(cfg.Server, "/")+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("连接服务失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		var env struct {
			Error apiError `json:"error"`
		}
		if json.Unmarshal(raw, &env) == nil && env.Error.Message != "" {
			return fmt.Errorf("%s: %s", env.Error.Code, env.Error.Message)
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("解析响应失败: %w", err)
		}
	}
	return nil
}

// ---- 命令实现 ----

func cmdLogin(args []string) error {
	var server, token string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--server", "-s":
			i++
			if i >= len(args) {
				return fmt.Errorf("--server 缺参数")
			}
			server = args[i]
		case "--token", "-t":
			i++
			if i >= len(args) {
				return fmt.Errorf("--token 缺参数")
			}
			token = args[i]
		default:
			return fmt.Errorf("未知参数: %s", args[i])
		}
	}
	if server == "" || token == "" {
		return fmt.Errorf("用法: rc login --server <url> --token <token>")
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	cfg.Server = strings.TrimRight(server, "/")
	cfg.Token = token
	// 先落盘再验证：验证失败时用户仍可改配置重试。
	if err := saveConfig(cfg); err != nil {
		return err
	}
	// 连通性验证（真实请求，不假装成功）。
	if err := call(http.MethodGet, "/api/v1/tasks", nil, nil); err != nil {
		return fmt.Errorf("连通性验证失败（配置已保存到 %s）: %w", configPath(), err)
	}
	fmt.Printf("已保存连接信息：%s\n", cfg.Server)
	return nil
}

func cmdAdd(args []string) error {
	var urls []string
	quality, path, convert, kind := "", "", "", ""
	preview, asJSON, asJSONL := true, false, false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--jsonl":
			asJSONL = true
		case a == "--quality" || a == "-q":
			i++
			if i >= len(args) {
				return fmt.Errorf("--quality 缺参数")
			}
			quality = args[i]
		case a == "--kind" || a == "-k":
			i++
			if i >= len(args) {
				return fmt.Errorf("--kind 缺参数")
			}
			kind = strings.ToLower(args[i])
			if kind != "qb" && kind != "n115" && kind != "download" {
				return fmt.Errorf("--kind 仅支持 qb / n115 / download")
			}
		case a == "--convert":
			i++
			if i >= len(args) {
				return fmt.Errorf("--convert 缺参数")
			}
			convert = strings.ToLower(args[i])
			if convert != "mp4" && convert != "mp3" {
				return fmt.Errorf("--convert 仅支持 mp4 / mp3")
			}
		case a == "--path" || a == "-p":
			i++
			if i >= len(args) {
				return fmt.Errorf("--path 缺参数")
			}
			path = args[i]
		case a == "--no-preview":
			preview = false
		case a == "--json":
			asJSON = true
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("未知参数: %s", a)
		default:
			urls = append(urls, a)
		}
	}
	if len(urls) == 0 {
		return fmt.Errorf("用法: rc add <url> [选项]")
	}
	// 多链接拆分（docs/01 §3.1：换行与空格都视为分隔——用户引号里贴一串
	// 空格分隔的链接、或从文件粘多行，都应逐条入队而不是当成一个畸形 URL）。
	var items []map[string]string
	seen := map[string]bool{}
	for _, raw := range urls {
		for _, line := range strings.Fields(raw) {
			line = strings.TrimSpace(line)
			if line == "" || seen[line] {
				continue
			}
			seen[line] = true
			it := map[string]string{"url": line}
			if quality != "" {
				it["quality"] = quality
			}
			if path != "" {
				it["save_path"] = path
			}
			if convert != "" {
				it["convert_to"] = convert
			}
			if kind != "" {
				it["kind"] = kind
			}
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		return fmt.Errorf("没有有效链接")
	}
	if preview {
		// 先预览（首个链接），符合「预览与实际拉取结果一致」。
		var pv struct {
			Data struct {
				Title    string `json:"title"`
				Platform string `json:"platform"`
				Uploader string `json:"uploader"`
				Duration *int64 `json:"duration"`
				Formats  []struct {
					Quality string `json:"quality"`
					Size    *int64 `json:"size"`
				} `json:"formats"`
			} `json:"data"`
		}
		if err := call(http.MethodPost, "/api/v1/parse", map[string]any{"url": items[0]["url"]}, &pv); err != nil {
			return err
		}
		if !asJSON {
			fmt.Printf("预览：%s\n平台：%s  作者：%s\n", pv.Data.Title, pv.Data.Platform, pv.Data.Uploader)
			for _, f := range pv.Data.Formats {
				size := "大小未知"
				if f.Size != nil {
					size = humanSize(*f.Size)
				}
				fmt.Printf("  %-8s %s\n", f.Quality, size)
			}
			fmt.Println()
		}
	}
	var out struct {
		Data      []map[string]any `json:"data"`
		Conflicts []map[string]any `json:"conflicts"`
	}
	if err := call(http.MethodPost, "/api/v1/tasks", map[string]any{"items": items}, &out); err != nil {
		return err
	}
	if asJSON {
		return printJSON(out)
	}
	if asJSONL {
		items := make([]any, len(out.Data))
		for i, it := range out.Data {
			items[i] = it
		}
		return printJSONL(items)
	}
	for _, t := range out.Data {
		fmt.Printf("已入队 %v  %v\n", t["id"], t["url"])
	}
	if len(out.Conflicts) > 0 {
		fmt.Printf("重复提交 %d 条（队列中已存在）\n", len(out.Conflicts))
	}
	return nil
}

func cmdList(args []string) error {
	status, platform := "", ""
	asJSON, asJSONL := false, false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--status", "-s":
			i++
			if i >= len(args) {
				return fmt.Errorf("--status 缺参数")
			}
			status = args[i]
		case "--platform":
			i++
			if i >= len(args) {
				return fmt.Errorf("--platform 缺参数")
			}
			platform = args[i]
		case "--json":
			asJSON = true
		case "--jsonl":
			asJSONL = true
		default:
			return fmt.Errorf("未知参数: %s", args[i])
		}
	}
	q := "/api/v1/tasks?limit=50"
	if status != "" {
		q += "&status=" + status
	}
	if platform != "" {
		q += "&platform=" + platform
	}
	var out struct {
		Items  []map[string]any `json:"items"`
		Counts map[string]int   `json:"counts"`
	}
	if err := call(http.MethodGet, q, nil, &out); err != nil {
		return err
	}
	if asJSON {
		return printJSON(out)
	}
	if asJSONL {
		items := make([]any, len(out.Items))
		for i, it := range out.Items {
			items[i] = it
		}
		return printJSONL(items)
	}
	if len(out.Items) == 0 {
		fmt.Println("队列是空的。")
		return nil
	}
	fmt.Printf("%-36s %-12s %-8s %-10s %s\n", "ID", "状态", "进度", "大小", "标题")
	for _, t := range out.Items {
		fmt.Printf("%-36v %-12v %-8s %-10s %v\n",
			t["id"], t["status"], fmt.Sprintf("%.0f%%", toFloat(t["progress"])),
			humanSize(int64(toFloat(t["downloaded_size"]))), t["title"])
	}
	return nil
}

func cmdStatus(args []string) error {
	var id string
	follow, asJSON := false, false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--follow", "-f":
			follow = true
		case "--json":
			asJSON = true
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("未知参数: %s", args[i])
			}
			id = args[i]
		}
	}
	if id == "" {
		return fmt.Errorf("用法: rc status <task-id> [--follow]")
	}
	if follow {
		return followTask(id)
	}
	var out struct {
		Data map[string]any `json:"data"`
	}
	if err := call(http.MethodGet, "/api/v1/tasks/"+id, nil, &out); err != nil {
		return err
	}
	if asJSON {
		return printJSON(out)
	}
	d := out.Data
	fmt.Printf("ID:      %v\n", d["id"])
	fmt.Printf("标题:    %v\n", d["title"])
	fmt.Printf("平台:    %v\n", d["platform"])
	fmt.Printf("状态:    %v\n", d["status"])
	fmt.Printf("进度:    %.1f%%\n", toFloat(d["progress"]))
	fmt.Printf("已下载:  %s\n", humanSize(int64(toFloat(d["downloaded_size"]))))
	fmt.Printf("速度:    %s/s\n", humanSize(int64(toFloat(d["speed_bps"]))))
	if v, ok := d["error_message"]; ok && v != "" {
		fmt.Printf("失败原因: %v\n", v)
	}
	return nil
}

// followTask 跟随 SSE 实时输出（Ctrl-C 退出）。
func followTask(id string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(cfg.Server, "/")+"/api/v1/events", nil)
	if err != nil {
		return err
	}
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}
	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("SSE 连接失败 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	// Ctrl-C 退出。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sigCh; resp.Body.Close() }()

	fmt.Printf("跟随任务 %s（Ctrl-C 退出）\n", id)
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev map[string]any
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev) != nil {
			continue
		}
		if tid, ok := ev["task_id"].(string); ok && tid != id {
			continue
		}
		switch {
		case ev["status"] != nil:
			fmt.Printf("[%s] 状态 → %v\n", nowStr(), ev["status"])
			if st, _ := ev["status"].(string); st == "done" || st == "failed" || st == "cancelled" {
				return nil
			}
		case ev["progress"] != nil:
			fmt.Printf("[%s] 进度 %.1f%%  %s/s\n", nowStr(), toFloat(ev["progress"]),
				humanSize(int64(toFloat(ev["speed_bps"]))))
		}
	}
	return nil
}

func cmdCancelRetry(args []string, action string) error {
	if len(args) < 1 {
		return fmt.Errorf("用法: rc %s <task-id>", action)
	}
	id := args[0]
	if err := call(http.MethodPost, "/api/v1/tasks/"+id+"/"+action, nil, nil); err != nil {
		return err
	}
	fmt.Printf("已%s：%s\n", map[string]string{"cancel": "取消", "retry": "重试"}[action], id)
	return nil
}

func cmdConfig(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("用法: rc config get <key> | rc config set <key> <value>")
	}
	switch args[0] {
	case "get":
		var out struct {
			Items []struct {
				Key   string `json:"key"`
				Value any    `json:"value"`
			} `json:"items"`
		}
		if err := call(http.MethodGet, "/api/v1/config", nil, &out); err != nil {
			return err
		}
		for _, it := range out.Items {
			if it.Key == args[1] {
				fmt.Printf("%s = %v\n", it.Key, it.Value)
				return nil
			}
		}
		return fmt.Errorf("未找到配置项: %s", args[1])
	case "set":
		if len(args) < 3 {
			return fmt.Errorf("用法: rc config set <key> <value>")
		}
		var out map[string]any
		if err := call(http.MethodPut, "/api/v1/config", map[string]any{
			"settings": map[string]any{args[1]: parseValue(args[2])},
		}, &out); err != nil {
			return err
		}
		return printJSON(out)
	}
	return fmt.Errorf("未知子命令: %s", args[0])
}

func cmdDiag(args []string) error {
	if len(args) < 2 || args[0] != "export" {
		return fmt.Errorf("用法: rc diag export -o <file>")
	}
	out := "raccoon-diag.zip"
	for i := 2; i < len(args); i++ {
		if args[i] == "-o" || args[i] == "--output" {
			i++
			if i >= len(args) {
				return fmt.Errorf("-o 缺参数")
			}
			out = args[i]
		}
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(cfg.Server, "/")+"/api/v1/system/diag/export", nil)
	if err != nil {
		return err
	}
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("导出失败 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, raw, 0o600); err != nil {
		return err
	}
	fmt.Printf("诊断包已保存：%s\n", out)
	return nil
}

// ---- files：列出/取回任务产物 ----
//
// 「取回文件」是 CLI 缺的最后一块（功能墙对齐项）：远端任务完成的产物
// 通过 GET /files/{id}/download 拉回本机。列出不下载时只走 /tasks/{id}/files。

func cmdFiles(args []string) error {
	var id, saveDir string
	asJSON := false
	positional := []string{}
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--save" || args[i] == "-o":
			i++
			if i >= len(args) {
				return fmt.Errorf("--save 缺参数")
			}
			saveDir = args[i]
		case args[i] == "--json":
			asJSON = true
		default:
			positional = append(positional, args[i])
		}
	}
	if len(positional) == 0 {
		return fmt.Errorf("用法: rc files <task-id> [--save <目录>] [--json]")
	}
	id = positional[0]

	var out struct {
		Items []struct {
			ID   string `json:"id"`
			Path string `json:"path"`
			Size int64  `json:"size"`
			Kind string `json:"kind"`
		} `json:"items"`
	}
	if err := call(http.MethodGet, "/api/v1/tasks/"+id+"/files", nil, &out); err != nil {
		return err
	}
	if len(out.Items) == 0 {
		fmt.Println("该任务没有产物文件。")
		return nil
	}
	if asJSON {
		return printJSON(out.Items)
	}
	fmt.Printf("%-38s %-10s %-12s %s\n", "文件ID", "类型", "大小", "路径")
	for _, f := range out.Items {
		fmt.Printf("%-38s %-10s %-12s %s\n", f.ID, f.Kind, humanSize(f.Size), f.Path)
	}
	if saveDir == "" {
		return nil
	}
	if err := os.MkdirAll(saveDir, 0o755); err != nil {
		return err
	}
	for _, f := range out.Items {
		name := filepath.Base(f.Path)
		dest := filepath.Join(saveDir, name)
		if err := downloadFile("/api/v1/files/"+f.ID+"/download", dest); err != nil {
			return fmt.Errorf("下载 %s 失败: %w", name, err)
		}
		fmt.Printf("已取回：%s（%s）\n", dest, humanSize(f.Size))
	}
	return nil
}

// downloadFile 流式下载 API 路径到本机文件。
func downloadFile(apiPath, dest string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(cfg.Server, "/")+apiPath, nil)
	if err != nil {
		return err
	}
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

// ---- 小工具 ----

// printJSONL 每行一个 JSON 对象（--jsonl：给 jq/脚本管道用，无缩进无包裹）。
func printJSONL(items []any) error {
	for _, it := range items {
		raw, err := json.Marshal(it)
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
	}
	return nil
}

func printJSON(v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(raw))
	return nil
}

func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	}
	return 0
}

// parseValue 值解析：数字/布尔/字符串。
func parseValue(s string) any {
	if b, err := strconv.ParseBool(s); err == nil && (s == "true" || s == "false") {
		return b
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return s
}

// humanSize 字节 → 可读大小。
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// nowStr 当前时间戳（HH:MM:SS）。
func nowStr() string { return time.Now().Format("15:04:05") }
