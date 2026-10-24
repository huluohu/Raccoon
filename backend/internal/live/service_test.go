package live

// service_test.go：直播守护（docs/07 计划录制闭环的后端部分）。
//
// 锁死的不变式：
//  1. record_plan=manual 的源绝不被自动录制（manual 是用户的明确意愿）；
//  2. always 计划开播即录：提交任务 + 建录制记录 + 源进 recording 态；
//  3. scheduled 计划只命中时段才录；时段缺失/不可解析不自动录（不猜）；
//  4. 探测失败如实落 error 态，不假装离线；
//  5. 录制中的源跳过探测（收尾交给任务终态，不重复提交）。

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/huluohu/raccoon/internal/config"
	"github.com/huluohu/raccoon/internal/engine"
	"github.com/huluohu/raccoon/internal/logx"
	"github.com/huluohu/raccoon/internal/sse"
	"github.com/huluohu/raccoon/internal/store"
	"github.com/huluohu/raccoon/internal/task"
)

func newLiveTestEnv(t *testing.T) (*Service, *store.Store, *task.Manager) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试库失败：%v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg := config.New()
	hub := sse.New(logx.New(nil, "test", logx.LevelError))
	mgr := task.NewManager(st, cfg, &engine.Set{}, hub, testLogger())
	svc := New(st, cfg, mgr, testLogger())
	return svc, st, mgr
}

func testLogger() *logx.Logger {
	return logx.New(nil, "test", logx.LevelError)
}

func createSource(t *testing.T, st *store.Store, plan, schedule string, intervalSec int) *store.LiveSource {
	t.Helper()
	ls := &store.LiveSource{
		Platform: "bilibili", RoomID: "23058", RoomURL: "https://live.bilibili.com/23058",
		Title: "测试直播间", RecordPlan: plan, ScheduleJSON: schedule,
		CheckIntervalSec: intervalSec, Enabled: true,
	}
	if err := st.CreateLiveSource(ls); err != nil {
		t.Fatalf("建直播源失败：%v", err)
	}
	return ls
}

func TestService_always开播即录(t *testing.T) {
	svc, st, _ := newLiveTestEnv(t)
	probeCalls := 0
	svc.setProbe(func(ctx context.Context, platform, roomID string) (bool, error) {
		probeCalls++
		if platform != "bilibili" || roomID != "23058" {
			t.Errorf("探测参数不对：platform=%s roomID=%s", platform, roomID)
		}
		return true, nil
	})
	ls := createSource(t, st, "always", "", 60)

	if err := svc.ProbeOne(context.Background(), ls); err != nil {
		t.Fatalf("ProbeOne 失败：%v", err)
	}
	if probeCalls != 1 {
		t.Fatalf("应探测一次，实际 %d", probeCalls)
	}
	cur, err := st.GetLiveSource(ls.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cur.Status != "recording" {
		t.Fatalf("开播后源应进 recording 态，得到 %s", cur.Status)
	}
	recs, err := st.ListLiveRecordings(ls.ID, 10)
	if err != nil || len(recs) != 1 {
		t.Fatalf("应建 1 条录制记录，得到 %d（err=%v）", len(recs), err)
	}
	if recs[0].TaskID == "" || recs[0].Status != "recording" {
		t.Fatalf("录制记录应带 task_id 且为 recording 态：%+v", recs[0])
	}
}

func TestService_manual永不被自动录制(t *testing.T) {
	svc, st, _ := newLiveTestEnv(t)
	// 故意注入「开播」——manual 也不许录（计划门槛在 ProbeOne 内）
	svc.setProbe(func(ctx context.Context, platform, roomID string) (bool, error) { return true, nil })

	ls := createSource(t, st, "manual", "", 60)
	if err := svc.ProbeOne(context.Background(), ls); err != nil {
		t.Fatalf("ProbeOne 失败：%v", err)
	}
	recs, _ := st.ListLiveRecordings(ls.ID, 10)
	if len(recs) != 0 {
		t.Fatalf("manual 源不得被自动录制，得到 %d 条记录", len(recs))
	}
}

func TestService_scheduled时段缺失不录(t *testing.T) {
	svc, st, _ := newLiveTestEnv(t)
	svc.setProbe(func(ctx context.Context, platform, roomID string) (bool, error) { return true, nil })
	ls := createSource(t, st, "scheduled", "", 60) // 没配 schedule_json

	if err := svc.ProbeOne(context.Background(), ls); err != nil {
		t.Fatalf("ProbeOne 失败：%v", err)
	}
	cur, _ := st.GetLiveSource(ls.ID)
	if cur.Status != "live" {
		t.Fatalf("开播态应如实回写 live，得到 %s", cur.Status)
	}
	recs, _ := st.ListLiveRecordings(ls.ID, 10)
	if len(recs) != 0 {
		t.Fatalf("scheduled 且时段缺失不得自动录制，得到 %d 条", len(recs))
	}
}

func TestService_scheduled命中时段即录(t *testing.T) {
	svc, st, _ := newLiveTestEnv(t)
	svc.setProbe(func(ctx context.Context, platform, roomID string) (bool, error) { return true, nil })

	now := time.Now()
	// 用当前时刻构造一个必中的窗口（覆盖跨午夜情形）
	start := now.Add(-time.Hour).Format("15:04")
	end := now.Add(time.Hour).Format("15:04")
	sched := `{"start":"` + start + `","end":"` + end + `"}`
	ls := createSource(t, st, "scheduled", sched, 60)

	if err := svc.ProbeOne(context.Background(), ls); err != nil {
		t.Fatalf("ProbeOne 失败：%v", err)
	}
	recs, _ := st.ListLiveRecordings(ls.ID, 10)
	if len(recs) != 1 {
		t.Fatalf("命中时段应自动录制，得到 %d 条", len(recs))
	}
}

func TestService_inWindow规则(t *testing.T) {
	cases := []struct {
		name string
		json string
		now  time.Time
		want bool
	}{
		{"空 JSON", "", time.Now(), false},
		{"坏 JSON", "{oops", time.Now(), false},
		{"缺时间字段", `{"days":[1]}`, time.Now(), false},
		{"同日窗口内", `{"start":"20:00","end":"23:00"}`, time.Date(2026, 10, 26, 21, 30, 0, 0, time.Local), true},
		{"同日窗口外", `{"start":"20:00","end":"23:00"}`, time.Date(2026, 10, 26, 19, 0, 0, 0, time.Local), false},
		{"跨午夜命中", `{"start":"22:00","end":"02:00"}`, time.Date(2026, 10, 26, 1, 0, 0, 0, time.Local), true},
		{"星期匹配", `{"days":[1],"start":"00:00","end":"23:59"}`, time.Date(2026, 10, 26, 12, 0, 0, 0, time.Local), true}, // 周一
		{"星期不匹配", `{"days":[2],"start":"00:00","end":"23:59"}`, time.Date(2026, 10, 26, 12, 0, 0, 0, time.Local), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := inWindow(c.json, c.now); got != c.want {
				t.Fatalf("inWindow(%q) = %v, want %v", c.json, got, c.want)
			}
		})
	}
}

func TestService_探测失败落error态(t *testing.T) {
	svc, st, _ := newLiveTestEnv(t)
	svc.setProbe(func(ctx context.Context, platform, roomID string) (bool, error) {
		return false, errors.New("网络不可达")
	})
	ls := createSource(t, st, "always", "", 60)

	if err := svc.ProbeOne(context.Background(), ls); err == nil {
		t.Fatal("探测失败应返回错误")
	}
	cur, _ := st.GetLiveSource(ls.ID)
	if cur.Status != "error" || cur.ErrorNote == "" {
		t.Fatalf("探测失败应如实落 error 态并带原因，得到 %s/%q", cur.Status, cur.ErrorNote)
	}
}

func TestService_录制中跳过scan(t *testing.T) {
	svc, st, _ := newLiveTestEnv(t)
	probeCalls := 0
	svc.setProbe(func(ctx context.Context, platform, roomID string) (bool, error) {
		probeCalls++
		return true, nil
	})
	ls := createSource(t, st, "always", "", 0)
	_ = st.MarkLiveSourceStatus(ls.ID, "recording", "", true)

	svc.scanDue(context.Background())
	time.Sleep(50 * time.Millisecond) // scanDue 里是 goroutine，等一下确认没有探测发生
	if probeCalls != 0 {
		t.Fatalf("录制中的源不应被探测，实际探测 %d 次", probeCalls)
	}
}
