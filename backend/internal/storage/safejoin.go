// Package storage 提供安全路径解析与落盘目录管理（docs/02 §5：防路径穿越）。
//
// 铁律：所有用户输入路径经 SafeJoin；清洗后必须仍在 root 前缀内，
// 不允许 .. / 绝对路径 / 符号链接逃逸。
package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrPathEscape 路径越界（逃出根目录）。
var ErrPathEscape = errors.New("路径超出允许范围")

// ErrEmptyPath 空路径。
var ErrEmptyPath = errors.New("路径不能为空")

// SafeJoin 把用户相对路径拼到 root 下并校验不越界。
//
// 规则：
//   - 拒绝绝对路径与含 ".." 的路径；
//   - 拼好后取绝对路径 + EvalSymlinks（若存在），必须仍以 root 前缀开头；
//   - root 本身允许（返回 root）。
func SafeJoin(root, rel string) (string, error) {
	if strings.TrimSpace(rel) == "" {
		return "", ErrEmptyPath
	}
	// Windows 盘符/UNC 一律拒绝（服务跑在 Linux 容器，但输入不可信）。
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "\\") {
		return "", fmt.Errorf("%w: 绝对路径不允许", ErrPathEscape)
	}
	if strings.Contains(rel, "\x00") {
		return "", fmt.Errorf("%w: 路径含非法字符", ErrPathEscape)
	}
	// 逐段检查 ..（防止 a/../../b 这类组合在清洗后仍越界）。
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if seg == ".." {
			return "", fmt.Errorf("%w: 父目录引用不允许", ErrPathEscape)
		}
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	joined := filepath.Clean(filepath.Join(absRoot, rel))
	// 二次保险：清洗后仍在 root 内。
	if joined != absRoot && !strings.HasPrefix(joined, absRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: %s", ErrPathEscape, rel)
	}
	// 符号链接逃逸检查。
	//
	// 为什么不能只 EvalSymlinks(joined)：SafeJoin 的主用途是计算「尚未存在」的
	// 产物输出路径，而 EvalSymlinks 对不存在的路径返回 ENOENT —— 早年直接
	// 跳过检查，等于给预置软链（root/escape -> /outside）开了逃逸通道
	// （渗透用例 TestSafeJoin_符号链接逃逸被拒 实测复现）。
	// 正确做法：从 joined 逐级上退到第一个真实存在的祖先做解析，
	// 再把不存在的尾部成分逐个 Lstat，软链一律解析校验。
	if err := checkSymlinkEscape(absRoot, joined); err != nil {
		return "", err
	}
	return joined, nil
}

// checkSymlinkEscape 校验 joined（含其全部成分）不经符号链接逃逸出 root。
func checkSymlinkEscape(absRoot, joined string) error {
	realRoot, rerr := filepath.EvalSymlinks(absRoot)
	if rerr != nil {
		realRoot = absRoot
	}
	within := func(p string) bool {
		return p == realRoot || strings.HasPrefix(p, realRoot+string(os.PathSeparator))
	}
	// 逐级上退找第一个真实存在的祖先。
	probe := joined
	var tail []string // 不存在的尾部成分（自底向上收集）
	for {
		if resolved, err := filepath.EvalSymlinks(probe); err == nil {
			if !within(resolved) {
				return fmt.Errorf("%w: 符号链接指向根目录外", ErrPathEscape)
			}
			break
		}
		tail = append(tail, filepath.Base(probe))
		parent := filepath.Dir(probe)
		if parent == probe {
			break // 已到文件系统根
		}
		probe = parent
	}
	// 尾部成分（joined 里尚不存在）逐个检查：是软链就解析并校验目标。
	for _, name := range tail {
		candidate := filepath.Join(probe, name)
		if fi, err := os.Lstat(candidate); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(candidate)
			if err != nil {
				// 悬空软链：EvalSymlinks 会因目标不存在而失败，改读链接本身。
				target, lerr := os.Readlink(candidate)
				if lerr != nil {
					return fmt.Errorf("%w: 无法解析符号链接", ErrPathEscape)
				}
				if filepath.IsAbs(target) {
					resolved = filepath.Clean(target)
				} else {
					resolved = filepath.Clean(filepath.Join(filepath.Dir(candidate), target))
				}
			}
			if !within(resolved) {
				return fmt.Errorf("%w: 符号链接指向根目录外", ErrPathEscape)
			}
		}
		probe = candidate
	}
	return nil
}

// WithinRoot 判断绝对路径是否位于 root 内（媒体库直链/预览用）。
func WithinRoot(root, abs string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	target, err := filepath.Abs(abs)
	if err != nil {
		return false
	}
	if target == absRoot {
		return true
	}
	return strings.HasPrefix(target, absRoot+string(os.PathSeparator))
}

// WithinAnyRoot 判断绝对路径是否位于任一允许根内。
//
// 引入原因：下载目录支持独立于数据根（如容器内 /downloads）后，
// 媒体库预览/删除、任务产物删除的路径校验只认数据根就会把独立下载根里的
// 文件误判越界（403）。产物文件的合法根集合 = 数据根 ∪ 下载根（绝对时）。
func WithinAnyRoot(roots []string, abs string) bool {
	for _, root := range roots {
		if WithinRoot(root, abs) {
			return true
		}
	}
	return false
}

// EnsureDir 确保目录存在（0750）。
func EnsureDir(dir string) error {
	return os.MkdirAll(dir, 0o750)
}

// SanitizeName 清洗单个文件名成分（去分隔符与控制字符，防目录注入）。
func SanitizeName(name string) string {
	name = strings.TrimSpace(name)
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_", "?", "_",
		"\"", "_", "<", "_", ">", "_", "|", "_", "\n", " ", "\r", " ", "\t", " ",
	)
	name = replacer.Replace(name)
	// 去掉首尾点（防 ".." 与隐藏文件）
	name = strings.Trim(name, ".")
	if name == "" {
		return "untitled"
	}
	if len([]rune(name)) > 120 {
		name = string([]rune(name)[:120])
	}
	return name
}
