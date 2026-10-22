// safejoin_test.go：task 包 SafeJoin 用例集（manager.go:477，worker.go:338 调用）。
//
// 为什么测这个：任务提交接口的 save_path 是用户可控字段，最终经
// Worker.saveDirFor → SafeJoin 决定下载产物落盘位置。放行 "../" 意味着
// 用户可让服务把文件写到数据根之外（覆盖任意路径，取决于进程权限）。
//
// 与 storage.SafeJoin 的实现差异（本测试如实记录，供评审判断，不改生产代码）：
//  1. 签名是 (string, bool)：调用方拿不到拒绝原因，只能拿到「不行」；
//  2. 只做「字符串含 .. + 前缀」两级检查：不单独拒绝对路径/反斜杠开头/NUL，
//     也不解析符号链接——铁律退化为「放行时字面路径在 root 内」。
//
// 因此断言策略：穿越串必须拒；其余输入只锁「不逃逸」不变式。
package task

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// assertWithinRoot 铁律断言：got 必须等于 root 或严格位于 root 之下。
func assertWithinRoot(t *testing.T, root, got string) {
	t.Helper()
	cleanRoot := filepath.Clean(root)
	if got == cleanRoot {
		return
	}
	rel, err := filepath.Rel(cleanRoot, got)
	if err != nil {
		t.Fatalf("无法计算相对路径（root=%q got=%q）: %v", cleanRoot, got, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("结果 %q 逃出 root %q（rel=%q）", got, cleanRoot, rel)
	}
	if !strings.HasPrefix(got, cleanRoot+string(filepath.Separator)) {
		t.Fatalf("结果 %q 不在 root %q 前缀之内", got, cleanRoot)
	}
}

// TestSafeJoin_必须拒绝的穿越串 只要串里出现 ".." 就拒（比 storage 版更粗的判据）。
func TestSafeJoin_必须拒绝的穿越串(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name string
		rel  string
	}{
		{"经典上级目录逃逸", "../etc/passwd"},
		{"先下钻再逃逸", "a/../../b"},
		{"单纯一个上级目录", ".."},
		{"多级上级目录", "../../.."},
		{"下载目录穿出去", "downloads/../../etc"},
		{"Windows风格上级目录", `..\..\x`},
		{"夹在路径中段的上级目录", "downloads/../../etc/passwd"},
		{"藏在文件名里的双点", "a/..b/../c"},
	}
	for _, c := range cases {
		got, ok := SafeJoin(root, c.rel)
		if ok {
			t.Errorf("%s（%q）应被拒绝，实际放行: %q", c.name, c.rel, got)
			continue
		}
		if got != "" {
			t.Errorf("%s 拒绝时应返回空路径，实际返回 %q", c.name, got)
		}
	}
}

// TestSafeJoin_边界串不得逃逸 不断言拒/放，只锁「放行时结果在 root 内」。
// 这些用例同时记录了与 storage.SafeJoin 的行为差异（见文件头注释）。
func TestSafeJoin_边界串不得逃逸(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name string
		rel  string
	}{
		// 本实现不拒绝对路径：Join(root, "/etc/passwd") 落在 root 之下，不逃逸但语义别扭
		{"Linux绝对路径", "/etc/passwd"},
		// NUL 原样拼入（Linux 下是合法文件名字节）；不逃逸
		{"含NUL字节", "a\x00b"},
		{"超长路径", strings.Repeat("a/", 2000) + "final.mp4"},
		{"三点目录名", "..."},
		// 含双点的合法文件名被误拒（".." 子串判据过度拒绝）——记录在案，
		// 无论拒还是放都不得逃逸
		{"含双点的合法文件名", "v1..2.mp4"},
		{"空串（Join 后等于 root）", ""},
		{"当前目录", "."},
	}
	for _, c := range cases {
		got, ok := SafeJoin(root, c.rel)
		if !ok {
			continue // 拒绝同样是可接受结果
		}
		assertWithinRoot(t, root, got)
	}
}

// TestSafeJoin_放行合法相对路径 save_path 的正常用法必须放行（误拒 = 下载落错目录）。
func TestSafeJoin_放行合法相对路径(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name string
		rel  string
	}{
		{"普通下载目录", "downloads/bilibili/uploader"},
		{"多层目录", "a/b/c"},
		{"中文目录与文件名", "哔哩哔哩/视频 文件（高清）"},
		{"带冗余点段", "./downloads/x"},
	}
	for _, c := range cases {
		got, ok := SafeJoin(root, c.rel)
		if !ok {
			t.Errorf("%s（%q）应放行，实际拒绝", c.name, c.rel)
			continue
		}
		if want := filepath.Join(root, c.rel); got != want {
			t.Errorf("%s 应解析为 %q，实际 %q", c.name, want, got)
		}
		assertWithinRoot(t, root, got)
	}
}

// TestSafeJoin_不解析符号链接（差异记录） task 版只看字符串前缀，不做
// EvalSymlinks：root 下若存在指向外部的软链，SafeJoin 会放行。
// 当前调用方传入的 rel 来自任务 save_path（用户可控），若数据根内可被
// 制造软链（例如经其他漏洞），此差距会被放大——如实记录，不在测试里锁死。
func TestSafeJoin_不解析符号链接(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("当前环境不支持创建符号链接，跳过: %v", err)
	}
	got, ok := SafeJoin(root, "escape/secret.txt")
	if !ok {
		return // 若将来补上软链解析，此用例自然通过
	}
	// 放行时至少保证字面路径在 root 内（不解析链接的前提下）
	assertWithinRoot(t, root, got)
	t.Log("task.SafeJoin 未解析符号链接：escape/secret.txt 经软链可指向 root 外（storage 版会拒绝）")
}
