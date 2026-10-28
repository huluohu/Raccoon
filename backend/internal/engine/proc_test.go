package engine

// proc_test.go：RunCmd 的输出捕获与收尾行为。
//
// 两个曾经真实发生过的缺陷（2026-10-28 定位）：
// 1. cmd.Wait() 在读完管道之前调用 → Wait 会关闭管道，快退进程的输出被吞，
//    解析侧拿到空字符串 → "解析引擎输出失败: unexpected end of JSON input"。
//    表现就是「链接明明能下，却报解析失败」，且本地不一定复现（CI 机器慢，
//    Wait 更容易抢先）。
// 2. 输出通道从不关闭 → 消费者 goroutine 永不退出 → 每次调用都空等满 2 秒，
//    顺带每次泄漏一个 goroutine（长跑服务里越积越多）。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeFastExitBin 生成「打印一行就立刻退出」的脚本：
// 越快退出，越容易撞上 Wait 抢先关闭管道的竞态。
func writeFastExitBin(t *testing.T, dir, name, out string) string {
	t.Helper()
	bin := filepath.Join(dir, name)
	script := "#!/bin/sh\nprintf '%s\\n' \"" + out + "\"\nexit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("写脚本失败：%v", err)
	}
	return bin
}

// TestRunCmd_快退进程不丢输出 反复跑快退脚本，每次都必须拿到那一行输出。
func TestRunCmd_快退进程不丢输出(t *testing.T) {
	dir := t.TempDir()
	bin := writeFastExitBin(t, dir, "fast", "hello-raccoon")

	for i := 0; i < 50; i++ {
		var got strings.Builder
		if err := RunCmd(context.Background(), bin, []string{"--x"}, func(l string) {
			got.WriteString(l)
		}, ""); err != nil {
			t.Fatalf("第 %d 次调用报错：%v", i, err)
		}
		if !strings.Contains(got.String(), "hello-raccoon") {
			t.Fatalf("第 %d 次调用丢了输出：%q", i, got.String())
		}
	}
}

// TestRunCmd_不空等两秒 输出读完就该返回，不能每次都白等 2 秒。
func TestRunCmd_不空等两秒(t *testing.T) {
	dir := t.TempDir()
	bin := writeFastExitBin(t, dir, "fast2", "x")

	start := time.Now()
	var got strings.Builder
	if err := RunCmd(context.Background(), bin, nil, func(l string) { got.WriteString(l) }, ""); err != nil {
		t.Fatalf("调用报错：%v", err)
	}
	elapsed := time.Since(start)
	if elapsed > time.Second {
		t.Fatalf("调用耗时 %v，明显超过「读完即返回」的预期（旧的实现恒定空等 2 秒）", elapsed)
	}
}

// TestRunCmd_非零退出码透传 退出码要能拿到（分类与重试逻辑依赖它）。
func TestRunCmd_非零退出码透传(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fail")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho boom >&2\nexit 3\n"), 0o755); err != nil {
		t.Fatalf("写脚本失败：%v", err)
	}
	err := RunCmd(context.Background(), bin, nil, nil, "")
	if err == nil {
		t.Fatal("非零退出应返回错误")
	}
	var ee *ExitError
	if !asExitError(err, &ee) {
		t.Fatalf("应返回 ExitError，实际 %T: %v", err, err)
	}
	if ee.Code != 3 {
		t.Fatalf("退出码应为 3，实际 %d", ee.Code)
	}
}

// asExitError 是 errors.As 的薄封装（避免测试里多一个 import 名字冲突）。
func asExitError(err error, target **ExitError) bool {
	for err != nil {
		if ee, ok := err.(*ExitError); ok {
			*target = ee
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
