//go:build linux

package monitor

// sampler_linux.go：Linux 采集实现（/proc + statfs）。
//
// 解析函数全部是纯函数（文本进、数值出），测试用 fixture 文本注入，
// 不打真实 /proc 依赖（docs/15 §6.7）。

import (
	"os"
	"syscall"
	"time"
)

// readSample 采一次原始样本。磁盘统计失败不阻断（其余指标仍可用），
// 但 /proc 读取失败整体返回 ok=false（调用方入 null 点）。
func readSample() (Sample, bool) {
	var s Sample
	s.Ts = time.Now().Unix()

	if content, err := os.ReadFile("/proc/meminfo"); err == nil {
		used, total, perr := parseMeminfo(string(content))
		if perr != nil {
			return s, false
		}
		s.MemUsedBytes, s.MemTotalBytes = used, total
	} else {
		return s, false
	}

	if content, err := os.ReadFile("/proc/stat"); err == nil {
		total, idle, perr := parseCPUStat(string(content))
		if perr != nil {
			return s, false
		}
		s.CPUTotalJiffies, s.CPUIdleJiffies = total, idle
	} else {
		return s, false
	}

	if content, err := os.ReadFile("/proc/net/dev"); err == nil {
		rx, tx, perr := parseNetDev(string(content))
		if perr != nil {
			return s, false
		}
		s.NetRxBytes, s.NetTxBytes = rx, tx
	} else {
		return s, false
	}

	// 磁盘：statfs 失败不阻断（CPU/内存/网络仍可用）。
	var fs syscall.Statfs_t
	if err := syscall.Statfs(dataDirForStat(), &fs); err == nil {
		bsize := uint64(fs.Bsize)
		total := fs.Blocks * bsize
		free := fs.Bavail * bsize
		s.DiskTotalBytes = total
		s.DiskFreeBytes = free
		if total > 0 {
			s.DiskUsedPct = float64(total-free) * 100 / float64(total)
		}
	}
	return s, true
}

// dataDirForStat 磁盘统计目标：优先环境变量数据目录，回落当前目录。
func dataDirForStat() string {
	if dir := os.Getenv("RACCOON_DATA_DIR"); dir != "" {
		return dir
	}
	return "."
}
