package qb

// magnet.go：磁力链接解析。
//
// 独立成文件是因为它有纯函数语义（不碰网络、不依赖 Client），上层在提交
// 种子前就能先用它拿到 info_hash 去做去重与入库，不必等 qB 回包。

import (
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

// ParseInfoHash 从磁力链接里取出 info_hash（归一成小写十六进制）。
//
// 磁力链接的 info_hash 有两种写法，磁力生态里都常见：
//   - 32 位 base32：urn:btih:VL2MMHO4YXUKFWV63YHTWSBM3GXKSQ2N
//   - 40 位十六进制：urn:btih:aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d
//
// 两种都归一成 40 位小写十六进制返回（与 qB 内部及 /torrents/info 的 hash
// 字段一致），上层可以放心拿它当主键。
func ParseInfoHash(magnet string) (string, error) {
	raw := strings.TrimSpace(magnet)
	if raw == "" {
		return "", fmt.Errorf("%w: 链接为空", ErrInvalidMagnet)
	}
	// 只认 magnet: 协议头：info_hash 只能从磁力链接里取，不接受裸 hash
	// 字符串——那样调用方很容易把普通下载链接误传进来。
	if !strings.HasPrefix(strings.ToLower(raw), "magnet:") {
		return "", fmt.Errorf("%w: 不是 magnet 链接", ErrInvalidMagnet)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: 链接无法解析: %v", ErrInvalidMagnet, err)
	}
	// xt 可能出现多次（v1 哈希 + 多哈希混合链接），只取第一个能解出的
	// urn:btih:。
	for _, xt := range u.Query()["xt"] {
		lower := strings.ToLower(xt)
		if !strings.HasPrefix(lower, "urn:btih:") {
			continue
		}
		hash, err := decodeInfoHash(strings.TrimSpace(xt[len("urn:btih:"):]))
		if err != nil {
			continue
		}
		return hash, nil
	}
	return "", fmt.Errorf("%w: 链接中没有 urn:btih: 哈希", ErrInvalidMagnet)
}

// decodeInfoHash 解析并归一 info_hash。
//
// base32 与 hex 各 20 字节（SHA-1），归一后都是 40 位小写 hex。
func decodeInfoHash(s string) (string, error) {
	s = strings.TrimSpace(s)
	switch len(s) {
	case 40:
		// 十六进制：直接解码验证合法性，避免把非法字符当成哈希用。
		b, err := hex.DecodeString(s)
		if err != nil {
			return "", fmt.Errorf("%w: 十六进制哈希非法", ErrInvalidMagnet)
		}
		return hex.EncodeToString(b), nil
	case 32:
		// base32：磁力生态里的传统写法，字符集是 RFC4648 大写 A-Z2-7。
		// magnet 链接里大小写混用很常见，所以先统一成大写再解。
		b, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(s))
		if err != nil {
			return "", fmt.Errorf("%w: base32 哈希非法", ErrInvalidMagnet)
		}
		return hex.EncodeToString(b), nil
	default:
		return "", fmt.Errorf("%w: 哈希长度 %d 非法（应为 32 或 40）", ErrInvalidMagnet, len(s))
	}
}
