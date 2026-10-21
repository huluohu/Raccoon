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
	// 符号链接逃逸检查：若目标已存在，取其真实路径再校验。
	if resolved, err := filepath.EvalSymlinks(joined); err == nil {
		realRoot, rerr := filepath.EvalSymlinks(absRoot)
		if rerr != nil {
			realRoot = absRoot
		}
		if resolved != realRoot && !strings.HasPrefix(resolved, realRoot+string(os.PathSeparator)) {
			return "", fmt.Errorf("%w: 符号链接指向根目录外", ErrPathEscape)
		}
	}
	return joined, nil
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
