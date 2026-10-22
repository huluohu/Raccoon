// safejoin_test.go：SafeJoin 路径穿越用例集（docs/08 W5.4 安全用例）。
//
// 为什么测这个：SafeJoin 是「用户可控字符串」进入文件系统的唯一闸口
// （下载目录命名、产物删除端点都经它）。一旦放行 "../" 或绝对路径，
// 攻击者就能以服务进程权限读写数据目录之外的任意文件。
//
// 用例分三组：
//  1. 必须拒绝的穿越串（断言 err != nil 且返回空路径）；
//  2. 平台相关/边界串——不断言拒不拒，只锁死铁律「放行时结果必须落在 root 之下」
//     （例如 Windows 风格 `..\x` 在 Linux 只是普通文件名字符，拼出来仍在 root 内；
//     在 Windows 上则会被段检查拒绝——两种结果都安全，不变式唯一）；
//  3. 必须放行的合法相对路径（误拒即功能回归）。
package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// assertWithinRoot 铁律断言：got 必须等于 root 或严格位于 root 之下。
// 用 filepath.Rel 交叉验证，避免单纯字符串前缀被 "/dataroot" vs "/dataroot-evil" 蒙混。
func assertWithinRoot(t *testing.T, root, got string) {
	t.Helper()
	cleanRoot := filepath.Clean(root)
	if got == cleanRoot {
		return // root 本身允许（ SafeJoin 契约明示 ）
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

// TestSafeJoin_必须拒绝的穿越串 这些串在任何平台都必须被拒。
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
		{"Linux绝对路径", "/etc/passwd"},
		{"UNC风格反斜杠开头", `\\server\share`},
		{"含NUL字节（截断攻击老套路）", "a\x00/b"},
		{"空串", ""},
		{"纯空白", "   \t "},
	}
	for _, c := range cases {
		got, err := SafeJoin(root, c.rel)
		if err == nil {
			t.Errorf("%s（%q）应被拒绝，实际放行: %q", c.name, c.rel, got)
			continue
		}
		if got != "" {
			t.Errorf("%s 拒绝时应返回空路径，实际返回 %q", c.name, got)
		}
	}
	// 空串的错误语义要可辨识（调用方靠它区分「没传」和「传了非法的」）。
	if _, err := SafeJoin(root, ""); !errors.Is(err, ErrEmptyPath) {
		t.Errorf("空串应返回 ErrEmptyPath，实际 %v", err)
	}
	// 其余必须是越界类错误。
	if _, err := SafeJoin(root, "../etc/passwd"); !errors.Is(err, ErrPathEscape) {
		t.Errorf("上级目录应返回 ErrPathEscape，实际 %v", err)
	}
}

// TestSafeJoin_边界串不得逃逸 平台相关或语义模糊的输入：不锁死「拒/放」，
// 只锁死「放行时必须在 root 之内」。这是安全测试唯一稳健的不变式。
func TestSafeJoin_边界串不得逃逸(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name string
		rel  string
	}{
		// Linux 下反斜杠是合法文件名字符：拼出来是 root 下一个怪名字文件，仍在 root 内；
		// Windows 下 filepath.ToSlash 会把它变分段，".." 段被拒。两种结果都安全。
		{"Windows风格上级目录", `..\..\x`},
		// SafeJoin 不做 URL 解码：%2e 就是字面量，不会被解成 "."
		{"URL编码的上级目录", "%2e%2e/%2e%2e/etc/passwd"},
		// 超长路径不触犯任何系统调用（纯字符串拼接），不得因长度放行越界
		{"超长路径", strings.Repeat("a/", 2000) + "final.mp4"},
		// 三个点不是 ".."，是合法（虽古怪）的文件名
		{"三点目录名", "..."},
		{"隐藏文件样式", ".config/secrets"},
		// 单点解析为 root 本身（契约允许 root）
		{"当前目录", "."},
	}
	for _, c := range cases {
		got, err := SafeJoin(root, c.rel)
		if err != nil {
			continue // 拒绝同样是可接受结果
		}
		assertWithinRoot(t, root, got)
	}
}

// TestSafeJoin_放行合法相对路径 误拒合法路径 = 功能回归，和安全漏洞一样锁死。
func TestSafeJoin_放行合法相对路径(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name string
		rel  string
		want string // 期望返回（相对 root）
	}{
		{"普通下载产物", "downloads/x/y.mp4", "downloads/x/y.mp4"},
		{"多层目录", "a/b/c", "a/b/c"},
		{"中文目录与文件名", "哔哩哔哩/视频 文件（高清）.mp4", "哔哩哔哩/视频 文件（高清）.mp4"},
		{"带冗余点段", "./downloads/x", "downloads/x"},
		{"root本身", ".", "."},
	}
	for _, c := range cases {
		got, err := SafeJoin(root, c.rel)
		if err != nil {
			t.Errorf("%s（%q）应放行，实际拒绝: %v", c.name, c.rel, err)
			continue
		}
		if want := filepath.Join(root, c.want); got != want {
			t.Errorf("%s 应解析为 %q，实际 %q", c.name, want, got)
		}
		assertWithinRoot(t, root, got)
	}
}

// TestSafeJoin_符号链接逃逸被拒 EvalSymlinks 分支：root 内指向外部的软链
// 是字符串检查看不出来的逃逸通道，必须靠解析真实路径拦掉。
//
// 实测发现（本用例锁死期望行为）：软链目标「已存在」时能被拦，但目标
// 「不存在」时 EvalSymlinks 返回 ENOENT，检查被整体跳过——SafeJoin 放行
// 经软链写入新文件（escape/new.txt）。 SafeJoin 的典型用途正是计算
// 「还不存在」的产物输出路径，此差距即逃逸通道。包文档铁律明言
// 「不允许符号链接逃逸」，期望：两种情形都拒绝。
func TestSafeJoin_符号链接逃逸被拒(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("当前环境不支持创建符号链接，跳过: %v", err)
	}
	// 情形 A：软链目标已存在（现有实现可拦）。
	if err := os.WriteFile(filepath.Join(outside, "exists.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := SafeJoin(root, "escape/exists.txt"); err == nil {
		t.Errorf("经符号链接指向 root 外的已存在文件应被拒绝，实际放行: %q", got)
	}
	// 情形 B：软链目标不存在（写入新文件的典型场景）——期望同样拒绝。
	got, err := SafeJoin(root, "escape/new.txt")
	if err == nil {
		t.Errorf("经符号链接指向 root 外的新文件应被拒绝（EvalSymlinks 遇 ENOENT 跳过检查即逃逸），实际放行: %q", got)
	}
	// 软链指向 root 内则正常放行（不能误伤合法软链）。
	inner := filepath.Join(root, "real")
	if err := os.MkdirAll(inner, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(inner, filepath.Join(root, "inside")); err != nil {
		t.Skipf("当前环境不支持创建符号链接，跳过: %v", err)
	}
	got, err = SafeJoin(root, "inside/a.mp4")
	if err != nil {
		t.Errorf("指向 root 内的软链应放行，实际拒绝: %v", err)
		return
	}
	assertWithinRoot(t, root, got)
}
