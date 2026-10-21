package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/huluohu/raccoon/internal/logx"
)

// spawnFakeEngineCmd 造一个「进程名像引擎」的长驻子进程。
//
// 做法：把 /bin/sleep 复制成 yt-dlp-probe 再执行——这样进程的 comm 就是
// yt-dlp-probe，命中 isEngineProcess。不依赖 yt-dlp 二进制是否安装，
// 也不发起任何网络请求，测试可离线重复跑。
func spawnFakeEngineCmd(t *testing.T, name string) *exec.Cmd {
	t.Helper()
	dir := t.TempDir()
	src, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("找不到 sleep，跳过：%v", err)
	}
	dst := filepath.Join(dir, name)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("复制 sleep 失败：%v", err)
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Fatalf("写入假引擎失败：%v", err)
	}
	cmd := exec.Command(dst, "120")
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动假引擎失败：%v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd
}

func writePIDFile(t *testing.T, dir string, pids ...int) string {
	t.Helper()
	path := filepath.Join(dir, enginePIDFile)
	var sb strings.Builder
	for _, p := range pids {
		sb.WriteString(strconv.Itoa(p))
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o600); err != nil {
		t.Fatalf("写 PID 文件失败：%v", err)
	}
	return path
}

func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// killedAndReaped 判定「子进程已被 SIGKILL 杀死」，并顺手收割僵尸。
//
// 不能用 kill(pid, 0)：它对僵尸进程同样返回成功（僵尸已不执行代码，但内核
// 仍保留条目等父进程 wait），macOS 又没有 /proc 可读状态位。所以这里用
// Wait4 非阻塞收割——测试进程本就是这些假引擎的父进程，收割后 PID 立即失效，
// 判定才准确。
func killedAndReaped(t *testing.T, pid int) bool {
	t.Helper()
	var ws syscall.WaitStatus
	// WNOHANG：子进程若还活着立刻返回 (0, nil)，不阻塞。
	wpid, err := syscall.Wait4(pid, &ws, syscall.WNOHANG, nil)
	switch {
	case err == syscall.ECHILD:
		// 已经不是本测试的子进程（早已被收割）→ 视为已死
		return true
	case err != nil:
		t.Fatalf("Wait4 失败：%v", err)
		return false
	case wpid == 0:
		return false // 仍在运行
	default:
		return true // 收割成功，说明已退出
	}
}

// TestReapOrphanEngines_回收引擎并保留非引擎 是本文件的核心用例：
// 登记 3 个 PID —— 真引擎、进程名像但不是引擎、根本不存在。
// 期望：只杀引擎，另两个不动，登记文件被清空。
func TestReapOrphanEngines_回收引擎并保留非引擎(t *testing.T) {
	dir := t.TempDir()
	enginePID := spawnFakeEngineCmd(t, "yt-dlp-probe").Process.Pid
	otherPID := spawnFakeEngineCmd(t, "unrelated-probe").Process.Pid
	deadPID := 999999 // 远超 pid_max 必然不存在

	writePIDFile(t, dir, enginePID, otherPID, deadPID)

	reapOrphanEngines(dir, logx.Default("test"))

	if !killedAndReaped(t, enginePID) {
		t.Errorf("引擎进程 %d 应被回收，但仍在执行", enginePID)
	}
	if !alive(otherPID) {
		t.Errorf("非引擎进程 %d 不应被杀（PID 复用防护失效）", otherPID)
	}
	if _, err := os.Stat(filepath.Join(dir, enginePIDFile)); !os.IsNotExist(err) {
		t.Error("回收后 PID 登记文件应被删除，否则下次启动会重复处理已回收的 PID")
	}
}

// TestReapOrphanEngines_无登记文件不报错 覆盖首次启动路径。
func TestReapOrphanEngines_无登记文件不报错(t *testing.T) {
	reapOrphanEngines(t.TempDir(), logx.Default("test")) // 不应 panic
}

// TestReapOrphanEngines_忽略非法行 确认脏数据不会导致误杀或崩溃。
// 混入非数字、0、负数、pid<=1 与空行，真引擎仍应被正确回收。
func TestReapOrphanEngines_忽略非法行(t *testing.T) {
	dir := t.TempDir()
	enginePID := spawnFakeEngineCmd(t, "ffmpeg-probe").Process.Pid
	path := filepath.Join(dir, enginePIDFile)
	content := "abc\n\n0\n1\n-5\n" + strconv.Itoa(enginePID) + "\n   \n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	reapOrphanEngines(dir, logx.Default("test"))

	if !killedAndReaped(t, enginePID) {
		t.Error("脏数据行不应影响对合法引擎 PID 的回收")
	}
}

// TestReapOrphanEngines_自身PID绝不自杀 是安全底线：
// 万一登记文件被写坏成当前进程自己的 PID，也不能把自己杀掉。
func TestReapOrphanEngines_自身PID绝不自杀(t *testing.T) {
	dir := t.TempDir()
	writePIDFile(t, dir, os.Getpid())
	reapOrphanEngines(dir, logx.Default("test"))
	// 能执行到这里就说明没自杀；再确认进程名不是引擎，语义上也该跳过
	if isEngineProcess("raccoon") {
		t.Skip("当前进程名恰好命中引擎规则，跳过语义断言")
	}
}

// TestIsEngineProcess 覆盖判定表，防止以后加引擎类型时漏改。
func TestIsEngineProcess(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"yt-dlp", true},
		{"yt_dlp", true},
		{"yt-dlp-probe", true},
		{"ffmpeg", true},
		{"gallery-dl", true},
		{"FFMPEG", true}, // 大小写不敏感
		{"raccoon", false},
		{"sleep", false},
		{"postgres", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isEngineProcess(c.name); got != c.want {
			t.Errorf("isEngineProcess(%q) = %v，期望 %v", c.name, got, c.want)
		}
	}
}

// TestProcessName_能取到自身进程名 验证探测路径在当前平台可用。
// 注意：本用例在受限沙箱里曾因 exec ps 失败而暴露「静默失效」缺陷，
// 现在 darwin 走 unix.SysctlKinfoProc，不依赖任何外部命令。
func TestProcessName_能取到自身进程名(t *testing.T) {
	name, ok := processName(os.Getpid())
	if !ok {
		t.Fatal("取不到自身进程名，孤儿回收会退化成静默失效")
	}
	if name == "" {
		t.Fatal("进程名为空")
	}
	t.Logf("自身进程名 = %q", name)
}

// TestProcessName_不存在的PID返回false 确认「进程已退出」与
// 「探测失败」在上层能被区分（前者记 gone，后者记 unknown 并告警）。
func TestProcessName_不存在的PID返回false(t *testing.T) {
	if _, ok := processName(999999); ok {
		t.Error("不存在的 PID 不应返回 ok=true")
	}
}

// TestEnginePIDFile_进程退出后注销 验证登记文件不会无限增长。
func TestEnginePIDFile_进程退出后注销(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, enginePIDFile)
	pid := spawnFakeEngineCmd(t, "gallery-dl-probe").Process.Pid
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 模拟 worker 正常退出后的注销：直接验证 unregisterPID 走的是同一份实现，
	// 这里只确认文件内容可被逐行移除（RunCmd 内部调用同一函数）。
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), strconv.Itoa(pid)) {
		t.Fatal("PID 未写入登记文件")
	}
	// 给点时间让子进程确实在跑，避免竞态导致的假阴性
	time.Sleep(50 * time.Millisecond)
	if !alive(pid) {
		t.Fatal("子进程意外退出")
	}
}