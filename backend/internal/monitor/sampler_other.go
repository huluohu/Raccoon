//go:build !linux

package monitor

import "time"

// sampler_other.go：非 Linux 平台的采集桩。
//
// 诚实原则（docs/01）：监控能力依赖 /proc，macOS/Windows 没有等价物。
// 这里不返回假数据——readSample 恒 ok=false，Snapshot 走降级读模型，
// 前端 Monitor 页整页显示「当前平台不支持资源监控」。

func readSample() (Sample, bool) {
	return Sample{Ts: time.Now().Unix()}, false
}
