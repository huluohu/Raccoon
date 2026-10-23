// Package monitor 实现系统资源监控：后台采样器（2s tick）+ 60 点内存环 +
// 1min 聚合落 SQLite（保留 7 天）。
//
// 诚实原则（docs/01）：非 Linux 平台整条链路 ok=false 降级，不返回假数据——
// 采集器只有 Linux 实现（读 /proc），其他平台由 sampler_other.go 的桩兜底。
//
// 数据流：readSample()（平台相关）→ Sampler.tick 派生指标（CPU 两次采样差）→
// 环形缓冲（供 /system/monitor 的 series）→ 每 30 tick 聚合落 monitor_sample 表
// （供 /system/monitor/history 的 1h/24h 视图）。
package monitor

import (
	"context"
	"sync"
	"time"

	"github.com/huluohu/raccoon/internal/store"
)

// Sample 一次原始采样（linux 实现填充；CPU 使用率由 Sampler 按两次采样差计算）。
type Sample struct {
	CPUTotalJiffies, CPUIdleJiffies uint64
	MemUsedBytes, MemTotalBytes     uint64
	DiskUsedPct                     float64
	DiskFreeBytes, DiskTotalBytes   uint64
	NetRxBytes, NetTxBytes          uint64
	Ts                              int64 // Unix 秒
}

// Point 环形缓冲中的一个派生点（percent 或 bps；null=该时刻采集失败）。
type Point struct {
	Ts    int64   `json:"ts"`
	Value float64 `json:"value"`
}

// Metric 百分比型指标。
type Metric struct {
	UsedPct float64 `json:"used_pct"`
}

// MemMetric 内存指标（含字节数，界面图例要显示「已用 GB / 空闲 GB」）。
type MemMetric struct {
	UsedPct    float64 `json:"used_pct"`
	UsedBytes  uint64  `json:"used_bytes"`
	FreeBytes  uint64  `json:"free_bytes"`
	TotalBytes uint64  `json:"total_bytes"`
}

// DiskMetric 磁盘指标。
type DiskMetric struct {
	UsedPct    float64 `json:"used_pct"`
	FreeBytes  uint64  `json:"free_bytes"`
	TotalBytes uint64  `json:"total_bytes"`
}

// NetMetric 网络带宽（字节/秒，两次采样差）。
type NetMetric struct {
	UpBps   float64 `json:"up_bps"`
	DownBps float64 `json:"down_bps"`
}

// Series 实时序列（60 点；点值 null=该时刻采集失败，前端断线渲染，不插值造假）。
type Series struct {
	CPU     []*float64 `json:"cpu"`
	Memory  []*float64 `json:"memory"`
	NetUp   []*float64 `json:"net_up"`
	NetDown []*float64 `json:"net_down"`
}

// Snapshot /system/monitor 的读模型（handler 直接序列化）。
type Snapshot struct {
	Ok        bool        `json:"ok"`
	Message   string      `json:"message,omitempty"`
	SampledAt int64       `json:"sampled_at"`
	CPU       *Metric     `json:"cpu,omitempty"`
	Memory    *MemMetric  `json:"memory,omitempty"`
	Disk      *DiskMetric `json:"disk,omitempty"`
	Net       *NetMetric  `json:"net,omitempty"`
	Series    *Series     `json:"series,omitempty"`
}

// unsupportedSnapshot 非 Linux 平台的降级读模型。
func unsupportedSnapshot() Snapshot {
	return Snapshot{Ok: false, Message: "当前平台不支持资源监控（仅 Linux 采集 /proc）"}
}

// ring 定长环形缓冲（float64 点；nil=该时刻采集失败）。
type ring struct {
	buf  []*float64
	ts   []int64
	next int
	n    int
}

func newRing(cap int) *ring {
	return &ring{buf: make([]*float64, cap), ts: make([]int64, cap)}
}

// push 写入一个点（覆写最旧）。
func (r *ring) push(ts int64, v *float64) {
	r.buf[r.next] = v
	r.ts[r.next] = ts
	r.next = (r.next + 1) % len(r.buf)
	if r.n < len(r.buf) {
		r.n++
	}
}

// snapshot 按时间正序拷贝（不在锁内序列化，锁外组装）。
func (r *ring) snapshot() ([]int64, []*float64) {
	out := make([]*float64, 0, r.n)
	ts := make([]int64, 0, r.n)
	for i := 0; i < r.n; i++ {
		idx := (r.next - r.n + i + len(r.buf)*2) % len(r.buf)
		out = append(out, r.buf[idx])
		ts = append(ts, r.ts[idx])
	}
	return ts, out
}

// aggAccum 聚合累加器（1min 一桶：均值）。
type aggAccum struct {
	samples  int
	cpu, mem float64
	disk     float64
	netRx    float64 // 下行 bps 累加
	netTx    float64 // 上行 bps 累加
	bucketTs int64   // 桶起点（Unix 秒，对齐整分）
}

// Sampler 后台采样器：interval tick、ringCap 点内存环、每 30 tick 聚合落库。
type Sampler struct {
	mu       sync.Mutex
	interval time.Duration
	ringCap  int
	st       *store.Store

	prev     *Sample
	rings    map[string]*ring // cpu/memory/net_up/net_down
	agg      aggAccum
	aggEvery int // 每 N tick 落一次（N = 60s / interval）

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// New 构造采样器（不启动；Start 起 goroutine）。
func New(st *store.Store, interval time.Duration, ringCap int) *Sampler {
	return &Sampler{
		interval: interval,
		ringCap:  ringCap,
		st:       st,
		rings: map[string]*ring{
			"cpu": newRing(ringCap), "memory": newRing(ringCap),
			"net_up": newRing(ringCap), "net_down": newRing(ringCap),
		},
		aggEvery: aggTicksPerBucket(interval),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// aggTicksPerBucket 每个 60s 聚合桶需要多少 tick（至少 1）。
func aggTicksPerBucket(interval time.Duration) int {
	n := int((60 * time.Second) / interval)
	if n < 1 {
		n = 1
	}
	return n
}

// Start 起后台采样 goroutine（ctx 取消即停）。
func (m *Sampler) Start(ctx context.Context) {
	go func() {
		defer close(m.doneCh)
		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()
		tickN := 0
		for {
			select {
			case <-ctx.Done():
				m.flushAgg()
				return
			case <-m.stopCh:
				m.flushAgg()
				return
			case <-ticker.C:
			}
			m.tick()
			tickN++
			if tickN%m.aggEvery == 0 {
				m.flushAgg()
			}
		}
	}()
}

// Stop 停止采样并 flush 未满 1min 的部分聚合（≥1 样本才落，不造空桶）。
func (m *Sampler) Stop() {
	m.stopOnce.Do(func() { close(m.stopCh) })
	<-m.doneCh
}

// tick 一次采样 + 派生 + 入环 + 累加。
func (m *Sampler) tick() {
	s, ok := readSample()
	if !ok {
		// 采集失败：入 null 点（断线渲染），不更新 prev/聚合——假数据比断点更糟。
		now := time.Now().Unix()
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, r := range m.rings {
			r.push(now, nil)
		}
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	var cpuPct *float64
	if m.prev != nil && s.CPUTotalJiffies > m.prev.CPUTotalJiffies {
		p := cpuUsagePct(m.prev.CPUTotalJiffies, m.prev.CPUIdleJiffies,
			s.CPUTotalJiffies, s.CPUIdleJiffies)
		cpuPct = &p
	}
	var upBps, downBps *float64
	if m.prev != nil {
		dt := float64(s.Ts - m.prev.Ts)
		if dt > 0 {
			up := float64(s.NetTxBytes-m.prev.NetTxBytes) / dt
			down := float64(s.NetRxBytes-m.prev.NetRxBytes) / dt
			upBps, downBps = &up, &down
		}
	}
	var memPct *float64
	if s.MemTotalBytes > 0 {
		p := float64(s.MemUsedBytes) * 100 / float64(s.MemTotalBytes)
		memPct = &p
	}
	m.rings["cpu"].push(s.Ts, cpuPct)
	m.rings["memory"].push(s.Ts, memPct)
	m.rings["net_up"].push(s.Ts, upBps)
	m.rings["net_down"].push(s.Ts, downBps)

	// 聚合累加（均值）。
	m.agg.samples++
	if cpuPct != nil {
		m.agg.cpu += *cpuPct
	}
	if memPct != nil {
		m.agg.mem += *memPct
	}
	m.agg.disk += s.DiskUsedPct
	if upBps != nil {
		m.agg.netTx += *upBps
	}
	if downBps != nil {
		m.agg.netRx += *downBps
	}
	if m.agg.bucketTs == 0 {
		m.agg.bucketTs = s.Ts - s.Ts%60 // 对齐整分
	}
	prev := s
	m.prev = &prev
}

// flushAgg 把累加器按均值落一桶 monitor_sample，并清空累加器。
// 样本为 0（全程采集失败）不落——不造空桶假数据。
func (m *Sampler) flushAgg() {
	m.mu.Lock()
	n := m.agg.samples
	bucketTs := m.agg.bucketTs
	cpu, mem, disk, netRx, netTx := m.agg.cpu, m.agg.mem, m.agg.disk, m.agg.netRx, m.agg.netTx
	m.agg = aggAccum{}
	m.mu.Unlock()

	if n == 0 || m.st == nil {
		return
	}
	_ = m.st.InsertMonitorSample(bucketTs, cpu/float64(n), mem/float64(n),
		disk/float64(n), netRx/float64(n), netTx/float64(n))
	_ = m.st.DeleteMonitorSamplesBefore(time.Now().Unix() - 7*24*3600)
}

// Snapshot 当前读模型（加锁拷贝环，锁外组装；不在锁内做 JSON 序列化）。
func (m *Sampler) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.prev == nil {
		// 还没采到样本（启动后第一个 interval 之前 / 全程采集失败）。
		return unsupportedSnapshot()
	}
	s := m.prev
	snap := Snapshot{
		Ok:        true,
		SampledAt: s.Ts,
		CPU:       &Metric{},
		Memory:    &MemMetric{},
		Disk:      &DiskMetric{},
		Net:       &NetMetric{},
		Series:    &Series{},
	}
	if s.MemTotalBytes > 0 {
		snap.CPU.UsedPct = 0
		snap.Memory.UsedPct = float64(s.MemUsedBytes) * 100 / float64(s.MemTotalBytes)
		snap.Memory.UsedBytes = s.MemUsedBytes
		snap.Memory.FreeBytes = s.MemTotalBytes - s.MemUsedBytes
		snap.Memory.TotalBytes = s.MemTotalBytes
	}
	snap.Disk.UsedPct = s.DiskUsedPct
	snap.Disk.FreeBytes = s.DiskFreeBytes
	snap.Disk.TotalBytes = s.DiskTotalBytes

	// 瞬时值取环末点（CPU/带宽是派生量，末点即最新）。
	if ts, vals := m.rings["cpu"].snapshot(); len(ts) > 0 && vals[len(vals)-1] != nil {
		snap.CPU.UsedPct = *vals[len(vals)-1]
	}
	if _, vals := m.rings["net_up"].snapshot(); len(vals) > 0 && vals[len(vals)-1] != nil {
		snap.Net.UpBps = *vals[len(vals)-1]
	}
	if _, vals := m.rings["net_down"].snapshot(); len(vals) > 0 && vals[len(vals)-1] != nil {
		snap.Net.DownBps = *vals[len(vals)-1]
	}
	// series 断线渲染：null 点保留 null，前端画断线不插值。
	for key, dst := range map[string]*[]*float64{
		"cpu": &snap.Series.CPU, "memory": &snap.Series.Memory,
		"net_up": &snap.Series.NetUp, "net_down": &snap.Series.NetDown,
	} {
		_, vals := m.rings[key].snapshot()
		*dst = vals
	}
	return snap
}
