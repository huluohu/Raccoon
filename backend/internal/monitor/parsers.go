package monitor

// parsers.go：/proc 文本解析纯函数（平台无关——测试在任意 OS 用 fixture 注入，
// 不打真实 /proc 依赖；docs/15 §6.7）。

import (
	"bufio"
	"errors"
	"strconv"
	"strings"
)

// 采集解析错误（fixture 测试断言用）。
var (
	errNoMemTotal = errors.New("monitor: /proc/meminfo 缺少 MemTotal")
	errBadCPUStat = errors.New("monitor: /proc/stat CPU 行格式非法")
)

// parseMeminfo 解析 /proc/meminfo（kB 单位）。
// used = MemTotal - MemAvailable（无 MemAvailable 时回落 MemFree）。
func parseMeminfo(content string) (used, total uint64, err error) {
	var memTotal, memAvail, memFree uint64
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		line := sc.Text()
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		key := strings.TrimSpace(line[:colon])
		fields := strings.Fields(line[colon+1:])
		if len(fields) == 0 {
			continue
		}
		kb, perr := strconv.ParseUint(fields[0], 10, 64)
		if perr != nil {
			continue
		}
		switch key {
		case "MemTotal":
			memTotal = kb * 1024
		case "MemAvailable":
			memAvail = kb * 1024
		case "MemFree":
			memFree = kb * 1024
		}
	}
	if err = sc.Err(); err != nil {
		return 0, 0, err
	}
	if memTotal == 0 {
		return 0, 0, errNoMemTotal
	}
	avail := memAvail
	if avail == 0 {
		avail = memFree
	}
	return memTotal - avail, memTotal, nil
}

// parseCPUStat 解析 /proc/stat 首行 aggregate CPU（user+nice+system+idle+iowait+
// irq+softirq+steal 为 total；idle+iowait 为 idle）。
func parseCPUStat(content string) (total, idle uint64, err error) {
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "cpu") || strings.HasPrefix(line, "cpu0") {
			continue // 只要 aggregate 行（"cpu" 后是空格不是数字）
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return 0, 0, errBadCPUStat
		}
		var sum uint64
		vals := make([]uint64, 0, len(fields)-1)
		for _, f := range fields[1:] {
			v, perr := strconv.ParseUint(f, 10, 64)
			if perr != nil {
				return 0, 0, errBadCPUStat
			}
			vals = append(vals, v)
			sum += v
		}
		idleAll := vals[3] // idle
		if len(vals) > 4 {
			idleAll += vals[4] // iowait 也算空闲
		}
		return sum, idleAll, nil
	}
	return 0, 0, errBadCPUStat
}

// parseNetDev 解析 /proc/net/dev（跳过两行表头，聚合所有网卡，lo 除外）。
func parseNetDev(content string) (rx, tx uint64, err error) {
	sc := bufio.NewScanner(strings.NewReader(content))
	lineNo := 0
	for sc.Scan() {
		lineNo++
		if lineNo <= 2 {
			continue // 两行表头
		}
		line := sc.Text()
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		iface := strings.TrimSpace(line[:colon])
		if iface == "lo" {
			continue // 回环不计入带宽
		}
		fields := strings.Fields(line[colon+1:])
		if len(fields) < 10 {
			continue
		}
		rxB, perr := strconv.ParseUint(fields[0], 10, 64)
		if perr != nil {
			continue
		}
		txB, perr := strconv.ParseUint(fields[8], 10, 64)
		if perr != nil {
			continue
		}
		rx += rxB
		tx += txB
	}
	if err = sc.Err(); err != nil {
		return 0, 0, err
	}
	return rx, tx, nil
}

// cpuUsagePct 两次采样差算 CPU 使用率（total 未推进返回 0，不除零）。
func cpuUsagePct(prevTotal, prevIdle, curTotal, curIdle uint64) float64 {
	dTotal := curTotal - prevTotal
	dIdle := curIdle - prevIdle
	if dTotal == 0 {
		return 0
	}
	return float64(dTotal-dIdle) * 100 / float64(dTotal)
}
