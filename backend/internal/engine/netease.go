package engine

// netease.go：网易云音乐平台支持（docs/08 W4.3）。
//
// 能力边界（诚实声明，勿夸大）：
//   - 网易云的歌曲直链需要登录态（Cookie）与账号权限，Raccoon 不内置账号体系；
//   - 本文件实现的是「.ncm 容器解密」纯 Go 能力（ncmdump 同源算法），
//     用于把用户自行获取的 .ncm 文件还原为标准音频；
//   - 网易云在线直链抓取需要真实 Cookie 与平台风控配合，本环境无账号，
//     因此**在线直链通道标注为未实测**，接口契约单独放在 netease_api.go。
//
// Apple Music / YT Music：yt-dlp 的 extractor 已覆盖这两个站，本文件只负责
// 平台标识路由（见 routing.go MusicPlatform），不需要独立引擎。

import (
	"crypto/aes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ncm 容器固定常量（与网易云客户端一致，公开逆向结论）。
var (
	// ncmCoreKey 用于解密「密钥信息」区。
	ncmCoreKey = []byte{
		0x68, 0x7A, 0x48, 0x52, 0x41, 0x6D, 0x73, 0x6F,
		0x35, 0x6B, 0x49, 0x6E, 0x62, 0x61, 0x78, 0x57,
	}
	// ncmModifyKey 用于解密「元数据」区。
	ncmModifyKey = []byte{
		0x23, 0x31, 0x34, 0x6C, 0x6A, 0x6B, 0x5F, 0x21,
		0x5C, 0x5D, 0x26, 0x30, 0x55, 0x3C, 0x27, 0x28,
	}
	// ncmMagic 文件魔数。
	ncmMagic = "CTENFDAM"
	// ncmMetaPrefix 解密元数据后需剥离的前缀。
	ncmMetaPrefix = "163 key(Don't modify):"
	// ncmMusicPrefix 解密元数据后需剥离的第二层前缀。
	ncmMusicPrefix = "music:"
)

// ncmKeyOffset 密钥区解密后需剥离的前缀长度（"neteasecloudmusic"）。
const ncmKeyOffset = 17

// NCMMetadata 是 .ncm 文件内的歌曲元信息（解密后的 JSON 子集）。
type NCMMetadata struct {
	MusicName string `json:"musicName"`
	Artist    []struct {
		Name string `json:"name"`
	} `json:"artist"`
	Album    string `json:"album"`
	AlbumPic string `json:"albumPic"`
	Format   string `json:"format"`
	Bitrate  int    `json:"bitrate"`
}

// ErrNotNCM 输入不是合法的 .ncm 文件。
var ErrNotNCM = errors.New("不是合法的 ncm 文件")

// DecryptNCM 解密 .ncm 文件，输出标准音频到 outPath。
//
// 返回值是解密出的元信息（歌曲名/歌手/专辑）。算法与 ncmdump 一致：
//  1. 密钥区：每字节 XOR 0x64 → AES-128-ECB(coreKey) → 剥离前 17 字节 → 真正的密钥；
//  2. 元数据区：每字节 XOR 0x63 → 剥离 "163 key(Don't modify):" → Base64 → AES-128-ECB(modifyKey) → 剥离 "music:"；
//  3. 音频区：RC4 变种（KSA 后再做一次映射，缺失这一步解出来是噪声）。
func DecryptNCM(inPath, outPath string) (*NCMMetadata, error) {
	raw, err := os.ReadFile(inPath)
	if err != nil {
		return nil, err
	}
	if len(raw) < 16 || string(raw[:8]) != ncmMagic {
		return nil, ErrNotNCM
	}
	off := 8
	// 2 字节版本 + 2 字节填充。
	off += 2 + 2

	// ---- 密钥区 ----
	if off+4 > len(raw) {
		return nil, ErrNotNCM
	}
	keyLen := int(binary.LittleEndian.Uint32(raw[off : off+4]))
	off += 4
	if keyLen < 0 || off+keyLen > len(raw) {
		return nil, ErrNotNCM
	}
	keyData := make([]byte, keyLen)
	for i := 0; i < keyLen; i++ {
		keyData[i] = raw[off+i] ^ 0x64
	}
	off += keyLen
	corePlain, err := aesEcbDecrypt(ncmCoreKey, keyData)
	if err != nil {
		return nil, fmt.Errorf("解密密钥区失败: %w", err)
	}
	if len(corePlain) <= ncmKeyOffset {
		return nil, ErrNotNCM
	}
	realKey := corePlain[ncmKeyOffset:]

	// ---- 元数据区 ----
	if off+4 > len(raw) {
		return nil, ErrNotNCM
	}
	metaLen := int(binary.LittleEndian.Uint32(raw[off : off+4]))
	off += 4
	if metaLen < 0 || off+metaLen > len(raw) {
		return nil, ErrNotNCM
	}
	metaRaw := make([]byte, metaLen)
	for i := 0; i < metaLen; i++ {
		metaRaw[i] = raw[off+i] ^ 0x63
	}
	off += metaLen
	meta, err := decodeNCMMeta(metaRaw)
	if err != nil {
		return nil, err
	}

	// ---- 封面区（可选）----
	// 结构：4 字节 0x00000000 + 1 字节标记 + 4 字节封面长度 + 封面数据。
	// 标记为 0 时后面是 4 字节占位；音频起点随之偏移。
	audioStart, cover, err := skipNCMCover(raw, off)
	if err != nil {
		return nil, err
	}

	// ---- 音频区 ----
	decrypted := ncmDecryptAudio(realKey, raw[audioStart:])
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(outPath, decrypted, 0o644); err != nil {
		return nil, err
	}
	// 封面单独落盘，便于媒体库索引。
	if len(cover) > 0 {
		_ = os.WriteFile(strings.TrimSuffix(outPath, filepath.Ext(outPath))+".cover.jpg", cover, 0o644)
	}
	return meta, nil
}

// decodeNCMMeta 解码元数据区。
func decodeNCMMeta(metaRaw []byte) (*NCMMetadata, error) {
	s := string(metaRaw)
	if !strings.HasPrefix(s, ncmMetaPrefix) {
		return nil, ErrNotNCM
	}
	s = strings.TrimPrefix(s, ncmMetaPrefix)
	dec, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("元数据 base64 解码失败: %w", err)
	}
	plain, err := aesEcbDecrypt(ncmModifyKey, dec)
	if err != nil {
		return nil, fmt.Errorf("解密元数据失败: %w", err)
	}
	js := string(plain)
	if !strings.HasPrefix(js, ncmMusicPrefix) {
		return nil, ErrNotNCM
	}
	js = strings.TrimPrefix(js, ncmMusicPrefix)
	var meta NCMMetadata
	if err := json.Unmarshal([]byte(js), &meta); err != nil {
		return nil, fmt.Errorf("解析元数据 JSON 失败: %w", err)
	}
	return &meta, nil
}

// skipNCMCover 跳过封面区，返回音频起始偏移与封面字节。
func skipNCMCover(raw []byte, off int) (int, []byte, error) {
	// 4 字节 0x00000000。
	if off+4 > len(raw) {
		return 0, nil, ErrNotNCM
	}
	off += 4
	if off >= len(raw) {
		return 0, nil, ErrNotNCM
	}
	hasCover := raw[off]
	off++
	if off+4 > len(raw) {
		return 0, nil, ErrNotNCM
	}
	coverLen := int(binary.LittleEndian.Uint32(raw[off : off+4]))
	off += 4
	if hasCover == 0 {
		// 无封面：4 字节占位，音频紧跟其后。
		if off+4 <= len(raw) {
			off += 4
		}
		return off, nil, nil
	}
	if coverLen < 0 || off+coverLen > len(raw) {
		return 0, nil, ErrNotNCM
	}
	cover := raw[off : off+coverLen]
	off += coverLen
	return off, cover, nil
}

// ncmDecryptAudio 用 RC4 变种解密音频数据。
//
// 与标准 RC4 的差别在于 KSA 之后多做一次映射：
//
//	K[i] = S[S[i] + S[(S[i] + i) & 0xFF] & 0xFF]
//
// 少了这一步，解出来的全是噪声 —— 这是最容易踩的坑。
func ncmDecryptAudio(key, data []byte) []byte {
	n := len(key)
	if n == 0 {
		return data
	}
	s := make([]int, 256)
	for i := 0; i < 256; i++ {
		s[i] = i
	}
	c := 0
	for i := 0; i < 256; i++ {
		swap := s[i]
		c = (c + swap + int(key[i%n])) & 0xFF
		s[i] = s[c]
		s[c] = swap
	}
	keyBox := make([]byte, 256)
	for i := 0; i < 256; i++ {
		keyBox[i] = byte(s[(s[i]+s[(s[i]+i)&0xFF])&0xFF])
	}
	out := make([]byte, len(data))
	for i := range data {
		out[i] = data[i] ^ keyBox[i&0xFF]
	}
	return out
}

// aesEcbDecrypt AES-128-ECB 解密（去 PKCS#7 填充）。
func aesEcbDecrypt(key, data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if len(data)%aes.BlockSize != 0 {
		return nil, errors.New("密文长度不是 AES 块大小整数倍")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(data))
	buf := make([]byte, aes.BlockSize)
	for i := 0; i < len(data); i += aes.BlockSize {
		block.Decrypt(buf, data[i:i+aes.BlockSize])
		copy(out[i:], buf)
	}
	// 去 PKCS#7 填充（填充值非法时按原样返回，ncmdump 同样宽松处理）。
	if pad := int(out[len(out)-1]); pad > 0 && pad <= aes.BlockSize && pad <= len(out) {
		valid := true
		for i := len(out) - pad; i < len(out); i++ {
			if int(out[i]) != pad {
				valid = false
				break
			}
		}
		if valid {
			out = out[:len(out)-pad]
		}
	}
	return out, nil
}
