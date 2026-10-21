package task

import (
	"os"

	"github.com/huluohu/raccoon/internal/logx"
)

// testLogger 返回一个写 stderr 的测试 logger（避免污染标准输出）。
func testLogger() *logx.Logger {
	return logx.New(os.Stderr, "test", logx.LevelWarn)
}
