// Package logx 提供结构化 JSON 日志：一行一条，字段 ts/level/msg/module + 业务字段。
// 脱敏规则集中在本包（凭据/令牌类字段值替换为 ***），诊断与日志共用同一套脱敏器。
package logx

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	default:
		return "error"
	}
}

// sensitiveKeys 命中即脱敏（大小写不敏感、包含匹配）。
var sensitiveKeys = []string{"cookie", "token", "password", "passwd", "secret", "authorization", "session"}

// Redact 对字段值脱敏；日志、诊断包、配置导出共用。
func Redact(key, value string) string {
	lower := strings.ToLower(key)
	for _, k := range sensitiveKeys {
		if strings.Contains(lower, k) {
			return "***"
		}
	}
	return value
}

type Logger struct {
	mu     sync.Mutex
	out    io.Writer
	module string
	minLv  Level
}

func New(out io.Writer, module string, minLv Level) *Logger {
	return &Logger{out: out, module: module, minLv: minLv}
}

// Default 输出到 stdout，info 起步。
func Default(module string) *Logger {
	return New(os.Stdout, module, LevelInfo)
}

// FilePath 运行日志文件路径（与 /api/v1/logs 读取的是同一个文件，单一来源）。
// dataDir 为空时退化为进程工作目录下的 logs/。
func FilePath(dataDir string) string {
	if dataDir == "" {
		dataDir = "."
	}
	return filepath.Join(dataDir, "logs", "raccoon.log")
}

// maxLogFileBytes 单文件上限，超过则启动时轮转一次（raccoon.log → raccoon.log.1）。
// 只保留一份历史：单机自托管场景够用，真正的日志治理交给外部采集。
const maxLogFileBytes = 32 << 20

// OpenSink 打开运行日志文件（追加写）。目录不存在则创建；
// 文件超过 maxLogFileBytes 时先轮转为 .1（覆盖旧 .1），保证不无限增长。
// 调用方负责 Close。失败返回 error 由调用方决定是否降级为仅 stdout。
func OpenSink(path string) (io.WriteCloser, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if st, err := os.Stat(path); err == nil && st.Size() > maxLogFileBytes {
		_ = os.Rename(path, path+".1") // 覆盖旧 .1；失败不阻塞启动
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}

func (l *Logger) With(module string) *Logger {
	return &Logger{out: l.out, module: module, minLv: l.minLv}
}

func (l *Logger) log(lv Level, msg string, fields map[string]any) {
	if lv < l.minLv {
		return
	}
	rec := map[string]any{"ts": time.Now().UTC().Format(time.RFC3339Nano), "level": lv.String(), "module": l.module, "msg": msg}
	for k, v := range fields {
		if s, ok := v.(string); ok {
			rec[k] = Redact(k, s)
		} else {
			rec[k] = v
		}
	}
	buf, err := json.Marshal(rec)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.out.Write(append(buf, '\n'))
}

func (l *Logger) Debug(msg string, fields map[string]any) { l.log(LevelDebug, msg, fields) }
func (l *Logger) Info(msg string, fields map[string]any)  { l.log(LevelInfo, msg, fields) }
func (l *Logger) Warn(msg string, fields map[string]any)  { l.log(LevelWarn, msg, fields) }
func (l *Logger) Error(msg string, fields map[string]any) { l.log(LevelError, msg, fields) }
