package monitor

// monitor_test.go：fixture 注入 /proc 解析 + 环形缓冲 + 聚合落库（docs/15 §5 T04 验收）。
//
// 解析全部走 fixture 文本注入（不打真实 /proc 依赖），在任意 OS 可跑。

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/huluohu/raccoon/internal/store"
)

const meminfoFixture = `MemTotal:       16384000 kB
MemFree:          512000 kB
MemAvailable:    4096000 kB
Buffers:          256000 kB
Cached:          8192000 kB
`

const cpuStatFixture = `cpu  1000 200 300 8000 50 10 20 30 0 0
cpu0 500 100 150 4000 25 5 10 15 0 0
cpu1 500 100 150 4000 25 5 10 15 0 0
intr 123456
`

const netDevFixture = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1000 10 0 0 0 0 0 0 2000 20 0 0 0 0 0 0
  eth0: 5000000 5000 0 0 0 0 0 0 1000000 2000 0 0 0 0 0 0
 wlan0: 1000000 1000 0 0 0 0 0 0 500000 500 0 0 0 0 0 0
`

func TestParseMeminfo_used等于Total减Available(t *testing.T) {
	used, total, err := parseMeminfo(meminfoFixture)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if total != 16384000*1024 {
		t.Fatalf("MemTotal 应为 %d，实际 %d", 16384000*1024, total)
	}
	want := uint64(16384000-4096000) * 1024
	if used != want {
		t.Fatalf("used 应为 %d，实际 %d", want, used)
	}
}

func TestParseMeminfo_无Available回落Free(t *testing.T) {
	used, total, err := parseMeminfo("MemTotal: 1000 kB\nMemFree: 400 kB\n")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if total != 1000*1024 || used != 600*1024 {
		t.Fatalf("回落 MemFree 计算错误: used=%d total=%d", used, total)
	}
}

func TestParseMeminfo_缺MemTotal报错(t *testing.T) {
	if _, _, err := parseMeminfo("MemFree: 400 kB\n"); err != errNoMemTotal {
		t.Fatalf("缺 MemTotal 应返回 errNoMemTotal，实际 %v", err)
	}
}

func TestParseCPUStat_聚合行且跳过cpu0(t *testing.T) {
	total, idle, err := parseCPUStat(cpuStatFixture)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	// 聚合行：1000+200+300+8000+50+10+20+30 = 9610；idle+iowait = 8000+50 = 8050。
	if total != 9610 {
		t.Fatalf("total 应为 9610，实际 %d", total)
	}
	if idle != 8050 {
		t.Fatalf("idle 应为 8050，实际 %d", idle)
	}
}

func TestParseCPUStat_坏行报错(t *testing.T) {
	if _, _, err := parseCPUStat("cpu 1 2\n"); err != errBadCPUStat {
		t.Fatalf("字段不足应返回 errBadCPUStat，实际 %v", err)
	}
	if _, _, err := parseCPUStat("cpu0 1 2 3 4\n"); err != errBadCPUStat {
		t.Fatalf("只有 cpu0 行应返回 errBadCPUStat，实际 %v", err)
	}
}

func TestParseNetDev_聚合非lo网卡(t *testing.T) {
	rx, tx, err := parseNetDev(netDevFixture)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if rx != 5000000+1000000 {
		t.Fatalf("rx 应为 eth0+wlan0=6000000，实际 %d", rx)
	}
	if tx != 1000000+500000 {
		t.Fatalf("tx 应为 1500000，实际 %d", tx)
	}
}

func TestCPUUsagePct_已知值与零推进(t *testing.T) {
	// total 推进 100，idle 推进 25 → 75%。
	if p := cpuUsagePct(1000, 400, 1100, 425); p != 75 {
		t.Fatalf("CPU 使用率应为 75，实际 %v", p)
	}
	// total 未推进（采样间隔内无变化）→ 0，不除零。
	if p := cpuUsagePct(1000, 400, 1000, 400); p != 0 {
		t.Fatalf("total 未推进应返回 0，实际 %v", p)
	}
}

func TestRing_超出容量淘汰最旧(t *testing.T) {
	r := newRing(3)
	one := 1.0
	two := 2.0
	three := 3.0
	four := 4.0
	r.push(1, &one)
	r.push(2, &two)
	r.push(3, &three)
	r.push(4, &four) // 淘汰 ts=1
	ts, vals := r.snapshot()
	if len(ts) != 3 || ts[0] != 2 || ts[2] != 4 {
		t.Fatalf("淘汰后顺序错误: %v", ts)
	}
	if *vals[0] != 2 || *vals[2] != 4 {
		t.Fatalf("淘汰后值错误: %v %v", *vals[0], *vals[2])
	}
}

func TestRing_null点保留断线语义(t *testing.T) {
	r := newRing(2)
	v := 1.0
	r.push(1, &v)
	r.push(2, nil) // 采集失败 → null，不插值
	_, vals := r.snapshot()
	if vals[1] != nil {
		t.Fatalf("断线点应保留 null，实际 %v", *vals[1])
	}
}

func TestAggTicksPerBucket_整分对齐(t *testing.T) {
	if n := aggTicksPerBucket(2 * time.Second); n != 30 {
		t.Fatalf("2s tick 每桶应 30 tick，实际 %d", n)
	}
	if n := aggTicksPerBucket(90 * time.Second); n != 1 {
		t.Fatalf("90s tick 每桶至少 1 tick，实际 %d", n)
	}
}

func TestFlushAgg_均值落库且可查回(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "monitor.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	m := New(st, 2*time.Second, 60)
	// 手工灌两个样本进累加器（10/20 → 均值 15）。
	m.agg.samples = 2
	m.agg.cpu = 10 + 20
	m.agg.mem = 30 + 50
	m.agg.disk = 40 + 40
	m.agg.netRx = 100 + 300
	m.agg.netTx = 5 + 7
	// 桶时间必须落在保留期（7 天）内：flushAgg 会顺手清理过期行，
	// 用写死的 2023 时间戳会被当场删掉，测出来是「落了 0 行」的假失败。
	now := time.Now().Unix()
	bucketTs := now - now%60 // 对齐整分
	m.agg.bucketTs = bucketTs
	m.flushAgg()

	rows, err := st.MonitorHistory(bucketTs-1, bucketTs+1)
	if err != nil {
		t.Fatalf("查历史失败: %v", err)
	}
	// flush 后累加器应清零（下次落桶不串味）。
	m.mu.Lock()
	samples := m.agg.samples
	m.mu.Unlock()
	if samples != 0 {
		t.Fatalf("flush 后累加器应清零，实际 %d", samples)
	}
	if len(rows) != 1 {
		t.Fatalf("应落 1 行聚合，实际 %d 行", len(rows))
	}
	got := rows[0]
	if got.CPU != 15 || got.Memory != 40 || got.Disk != 40 || got.NetRx != 200 || got.NetTx != 6 {
		t.Fatalf("聚合均值不符: %+v", got)
	}
}

func TestFlushAgg_零样本不落空桶(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "monitor.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	m := New(st, 2*time.Second, 60)
	m.agg.bucketTs = 1700000000
	m.flushAgg() // samples=0：全程采集失败，不造空桶
	rows, err := st.MonitorHistory(0, 1700000100)
	if err != nil {
		t.Fatalf("查历史失败: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("零样本不应落桶，实际 %d 行", len(rows))
	}
}

func TestSnapshot_未采到样本返回降级读模型(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "monitor.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	m := New(st, 2*time.Second, 60)
	snap := m.Snapshot()
	if snap.Ok {
		t.Fatal("未采到样本时应返回 ok=false 降级读模型")
	}
	if snap.Message == "" {
		t.Fatal("降级读模型应带说明文案")
	}
}
