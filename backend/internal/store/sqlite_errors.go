package store

import (
	"errors"
	"fmt"
)

// DescribeSQLiteOpenError formats a SQLite open/migration error using the driver's
// numeric result code rather than its message text. In modernc.org/sqlite v1.28.0,
// error text can say "out of memory" while Code() is 14 (SQLITE_CANTOPEN).
func DescribeSQLiteOpenError(err error) string {
	if err == nil {
		return ""
	}
	var coded interface{ Code() int }
	if !errors.As(err, &coded) {
		return err.Error()
	}

	// SQLite extended result codes encode the primary result in the low byte.
	code := coded.Code()
	primary := code & 0xff
	switch primary {
	case 14: // SQLITE_CANTOPEN
		// 某些 modernc.org/sqlite 版本返回的文本可能含误导性的 "out of memory"；
		// 已确认 code=14 是 CANTOPEN，所以不要再把该原文转述给用户。
		return fmt.Sprintf("SQLite 无法打开数据库（SQLITE_CANTOPEN，code=%d）；请检查数据库路径、文件类型、挂载读写权限及 ACL。", code)
	case 7: // SQLITE_NOMEM
		return fmt.Sprintf("SQLite 内存分配失败（SQLITE_NOMEM，code=%d）；请检查系统内存与进程限制。原始错误：%v", code, err)
	default:
		return fmt.Sprintf("SQLite 错误（code=%d）：%v", code, err)
	}
}
