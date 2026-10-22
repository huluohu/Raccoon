//go:build linux

package main

import (
	"os"
	"strconv"
	"strings"
)

// processName 取进程可执行名（Linux 生产路径）。
//
// 读 /proc/<pid>/comm —— 容器内始终可用且无需外部命令。
// 早先的实现是「读 /proc 失败就 exec ps」，在受限环境（ps 不可执行）里
// ps 探测会静默失败，被上层误判成「进程已退出」，导致孤儿引擎永远回收不掉。
// 现在把探测下沉到各平台实现，Linux 路径不再有任何外部依赖。
func processName(pid int) (string, bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
	if err != nil {
		return "", false // 进程已退出，或权限不足
	}
	name := strings.TrimSpace(string(data))
	return name, name != ""
}
