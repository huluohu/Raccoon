package server

// log_handler.go：运行日志查询端点（docs/04 §2.10-e / docs/15 §3.1）。
//
// 契约要点：
//   - 入参 level（逗号多选）/ module / q（≤256，超长截断不报错）/ from / to（Unix 秒）/ limit / cursor；
//   - cursor 是**行号游标**（1 起），空=从文件头；next_cursor 为下一段起始行号，
//     无更多时回传当前 cursor（前端据此停「加载更多」，不靠「比 limit 少」猜）；
//   - 出口统一走 redactLine（与诊断包同一套脱敏），**不造第二套**。
//
// 读文件：日志按行追加，整文件读入后按行号切片；当前单机日志量级（几 MB）可接受，
// 后续如加轮转再改流式扫描（契约不变）。

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/huluohu/raccoon/internal/httpx"
	"github.com/huluohu/raccoon/internal/logx"
)

// setupLogs 注册日志路由。
func (s *Server) setupLogs() {
	s.router.GET("/api/v1/logs", s.requireAuth(s.handleLogs))
}

// maxLogQueryLen q 参数上限（超长截断，不报错——docs/15 §3.1）。
const maxLogQueryLen = 256

// logItem /logs 单条。
type logItem struct {
	Ts     int64  `json:"ts"`
	Level  string `json:"level"`
	Module string `json:"module"`
	Msg    string `json:"msg"`
}

// handleLogs 日志查询（过滤 + 行号游标分页 + 脱敏）。
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()

	// level：逗号多选，默认 info,warn,error（debug 需显式要）。
	levels := map[string]bool{}
	for _, raw := range strings.Split(q.Get("level"), ",") {
		v := strings.ToLower(strings.TrimSpace(raw))
		if v == "" {
			continue
		}
		switch v {
		case "debug", "info", "warn", "error":
			levels[v] = true
		default:
			return httpx.Validation("level 仅支持 debug/info/warn/error（逗号多选）")
		}
	}
	if len(levels) == 0 {
		levels = map[string]bool{"info": true, "warn": true, "error": true}
	}

	limit := parseBoundedInt(q.Get("limit"), 100, 1, 500)
	cursor := 0
	if raw := strings.TrimSpace(q.Get("cursor")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			return httpx.Validation("cursor 必须是非负整数行号")
		}
		cursor = v
	}
	module := strings.TrimSpace(q.Get("module"))
	keyword := q.Get("q")
	if len(keyword) > maxLogQueryLen {
		keyword = keyword[:maxLogQueryLen] // 截断，不报错
	}
	keyword = strings.ToLower(keyword)
	from, _ := strconv.ParseInt(strings.TrimSpace(q.Get("from")), 10, 64)
	to, _ := strconv.ParseInt(strings.TrimSpace(q.Get("to")), 10, 64)

	lines := s.readLogLines()
	items := make([]logItem, 0, limit)
	next := cursor
	scanned := cursor // 已扫描到的行号（1 起）
	for i := cursor; i < len(lines); i++ {
		scanned = i + 1
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		var rec struct {
			Ts     string `json:"ts"`
			Level  string `json:"level"`
			Module string `json:"module"`
			Msg    string `json:"msg"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue // 非 JSON 行（启动横幅等）跳过，不让整页 500
		}
		if !levels[strings.ToLower(rec.Level)] {
			continue
		}
		if module != "" && rec.Module != module {
			continue
		}
		// ts 是 RFC3339Nano（UTC）；转 Unix 秒供前端比较。
		ts, perr := time.Parse(time.RFC3339Nano, rec.Ts)
		var tsUnix int64
		if perr == nil {
			tsUnix = ts.Unix()
		}
		if from > 0 && tsUnix < from {
			continue
		}
		if to > 0 && tsUnix > to {
			continue
		}
		if keyword != "" {
			hay := strings.ToLower(rec.Msg + " " + rec.Module + " " + rec.Level)
			if !strings.Contains(hay, keyword) {
				continue
			}
		}
		items = append(items, logItem{
			Ts:     tsUnix,
			Level:  strings.ToLower(rec.Level),
			Module: rec.Module,
			Msg:    redactLine(rec.Msg), // 出口唯一脱敏点
		})
		if len(items) >= limit {
			next = scanned
			break
		}
	}
	// 扫到底没有更多：next_cursor 回传当前 cursor（前端据此停「加载更多」）。
	if len(items) < limit {
		next = cursor
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"items":       items,
		"next_cursor": strconv.Itoa(next),
	})
	return nil
}

// readLogLines 读日志文件全部行（不存在返回空）。
// 路径与进程写入口同源（logx.FilePath），避免「写这里、读那里」的错位。
func (s *Server) readLogLines() []string {
	raw, err := os.ReadFile(logx.FilePath(s.cfg.GetString("storage.data_dir")))
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
}
