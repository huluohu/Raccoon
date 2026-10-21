//go:build darwin

package main

import (
	"strings"

	"golang.org/x/sys/unix"
)

// processName 取进程可执行名（darwin 开发路径）。
//
// macOS 没有 /proc，纯 Go 里 syscall.SysctlKinfoProc 已在 Go 1.20 移除，
// 但 golang.org/x/sys/unix 保留了它（项目已间接依赖 x/sys）。
// 直接问内核拿 KERN_PROC_PID，比 exec `ps` 可靠得多：`ps` 在受限容器/沙箱里
// 可能 fork/exec 失败（实测 operation not permitted），而 sysctl 走的是内核接口，
// 不受执行权限影响。
//
// 仍保留 ps 作为最后兜底（极老内核或未来 x/sys 结构变动时）。
func processName(pid int) (string, bool) {
	if kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid); err == nil {
		if name := strings.TrimSpace(unix.ByteSliceToString(kp.Proc.P_comm[:])); name != "" {
			return name, true
		}
	}
	out, err := execCommand("ps", "-p", itoa(pid), "-o", "comm=")
	if err != nil {
		return "", false
	}
	name := strings.TrimSpace(out)
	return name, name != ""
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}