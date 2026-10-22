package engine

// netease_test.go：.ncm 容器解密器的往返测试。
//
// 为什么值得单独测：ncm 的解密链有三处最容易出错的「魔数级」细节 ——
//  1. 密钥区前 17 字节前缀（"neteasecloudmusic"）必须先剥掉才是真密钥；
//  2. 元数据区是「XOR 0x63 → base64 → AES-ECB → 剥 "music:" 前缀」三段式；
//  3. 音频区用的不是标准 RC4，KSA 之后还要做一次 keyBox 映射。
// 少任何一步，输出都是「有长度但全是噪声」——用眼看长度根本发现不了，
// 所以这里做字节级往返断言。

import (
	"bytes"
	"crypto/aes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// aesEcbEncrypt 测试侧的对称加密（AES-128-ECB + PKCS#7 填充）。
//
// 生产代码只提供解密，测试要造容器就得能加密；这里独立实现而不是复用生产
// 代码，是为了让「加密-解密」互为独立实现（同一份算法自己加密自己解密，
// 掩不出实现错误）。
func aesEcbEncrypt(t *testing.T, key, plain []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("建 AES 失败：%v", err)
	}
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append(append([]byte{}, plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	out := make([]byte, len(padded))
	for i := 0; i < len(padded); i += aes.BlockSize {
		block.Encrypt(out[i:i+aes.BlockSize], padded[i:i+aes.BlockSize])
	}
	return out
}

// xorBytes 逐字节异或（密钥区用 0x64、元数据区用 0x63）。
func xorBytes(b []byte, k byte) []byte {
	out := make([]byte, len(b))
	for i := range b {
		out[i] = b[i] ^ k
	}
	return out
}

// ncmBuilder 按 netease.go 的解析规则拼一个 .ncm 容器。
//
// 布局（与 DecryptNCM 的读取顺序一一对应）：
//
//	magic(8) | 版本(2) | 填充(2) | 密钥区长度(4) | 密钥区
//	          | 元数据区长度(4) | 元数据区 | 封面区 | 音频区
//
// 封面区按实现分两种：无封面时是 4 字节 0 + 1 字节标记(0) + 4 字节长度 +
// 4 字节占位；有封面时是 4 字节 0 + 1 字节标记(1) + 4 字节长度 + 封面数据。
func ncmBuilder(t *testing.T, realKey []byte, metaJSON string, cover []byte, audio []byte) []byte {
	t.Helper()

	// ---- 密钥区：明文 = "neteasecloudmusic" + 真密钥 ----
	corePlain := append([]byte("neteasecloudmusic"), realKey...)
	coreCipher := aesEcbEncrypt(t, ncmCoreKey, corePlain)
	keyArea := xorBytes(coreCipher, 0x64)

	// ---- 元数据区：明文 = "163 key(Don't modify):" + base64(AES("music:"+JSON)) ----
	metaPlain := "music:" + metaJSON
	metaCipher := aesEcbEncrypt(t, ncmModifyKey, []byte(metaPlain))
	metaArea := xorBytes([]byte(ncmMetaPrefix+base64.StdEncoding.EncodeToString(metaCipher)), 0x63)

	// ---- 音频区：RC4 变种（与解密同一函数，XOR 对称） ----
	audioArea := ncmDecryptAudio(realKey, audio)

	var buf bytes.Buffer
	buf.WriteString(ncmMagic)
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1)) // 版本
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0)) // 填充
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(keyArea)))
	buf.Write(keyArea)
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(metaArea)))
	buf.Write(metaArea)

	// 封面区
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0)) // 4 字节 0
	if len(cover) > 0 {
		buf.WriteByte(1)
		_ = binary.Write(&buf, binary.LittleEndian, uint32(len(cover)))
		buf.Write(cover)
	} else {
		buf.WriteByte(0)
		_ = binary.Write(&buf, binary.LittleEndian, uint32(0))
		buf.Write([]byte{0, 0, 0, 0}) // 4 字节占位
	}

	buf.Write(audioArea)
	return buf.Bytes()
}

// writeFile 写临时文件并注册清理。
func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatalf("写临时文件失败：%v", err)
	}
	return p
}

// TestDecryptNCM_往返 构造合法容器 → 解密 → 音频字节与元信息都应还原。
func TestDecryptNCM_往返(t *testing.T) {
	dir := t.TempDir()

	realKey := []byte("0123456789abcdef")
	audio := []byte("this-is-the-original-audio-payload-0123456789")
	metaJSON := `{"musicName":"测试歌曲","artist":[{"name":"歌手甲"},{"name":"歌手乙"}],"album":"测试专辑","format":"mp3","bitrate":320000}`

	raw := ncmBuilder(t, realKey, metaJSON, nil, audio)
	in := writeFile(t, dir, "song.ncm", raw)
	out := filepath.Join(dir, "out", "song.mp3")

	meta, err := DecryptNCM(in, out)
	if err != nil {
		t.Fatalf("解密失败：%v", err)
	}
	if meta == nil {
		t.Fatal("应返回元信息")
	}
	if meta.MusicName != "测试歌曲" {
		t.Errorf("MusicName = %q，期望 %q", meta.MusicName, "测试歌曲")
	}
	if len(meta.Artist) != 2 {
		t.Fatalf("Artist 应有 2 条，实际 %d 条", len(meta.Artist))
	}
	if meta.Artist[0].Name != "歌手甲" {
		t.Errorf("Artist[0].Name = %q，期望 %q", meta.Artist[0].Name, "歌手甲")
	}
	if meta.Album != "测试专辑" {
		t.Errorf("Album = %q，期望 %q", meta.Album, "测试专辑")
	}
	if meta.Bitrate != 320000 {
		t.Errorf("Bitrate = %d，期望 320000", meta.Bitrate)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("读解密产物失败：%v", err)
	}
	if !bytes.Equal(got, audio) {
		t.Errorf("解密后的音频与原文不一致：\n got %q\nwant %q", got, audio)
	}
}

// TestDecryptNCM_有封面 封面应被单独落盘（媒体库索引用）。
func TestDecryptNCM_有封面(t *testing.T) {
	dir := t.TempDir()

	realKey := []byte("cover-key-16bytes")
	audio := []byte("audio-with-cover")
	cover := []byte("\xff\xd8\xff\xe0 fake jpeg payload")
	raw := ncmBuilder(t, realKey, `{"musicName":"带封面","artist":[]}`, cover, audio)
	in := writeFile(t, dir, "cover.ncm", raw)
	out := filepath.Join(dir, "cover.mp3")

	if _, err := DecryptNCM(in, out); err != nil {
		t.Fatalf("解密失败：%v", err)
	}
	gotCover, err := os.ReadFile(filepath.Join(dir, "cover.cover.jpg"))
	if err != nil {
		t.Fatalf("读封面文件失败：%v", err)
	}
	if !bytes.Equal(gotCover, cover) {
		t.Errorf("封面内容不一致：\n got %q\nwant %q", gotCover, cover)
	}
}

// TestDecryptNCM_非法魔数 魔数不对必须报 ErrNotNCM（而不是解出噪声）。
func TestDecryptNCM_非法魔数(t *testing.T) {
	dir := t.TempDir()
	in := writeFile(t, dir, "bad.ncm", []byte("NOTNCM__some-random-bytes-here"))
	out := filepath.Join(dir, "bad.mp3")

	_, err := DecryptNCM(in, out)
	if !errors.Is(err, ErrNotNCM) {
		t.Fatalf("非法魔数应返回 ErrNotNCM，实际 %v", err)
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Error("非法文件不应产出解密文件")
	}
}

// TestDecryptNCM_截断文件 截断文件必须返回错误且不 panic。
//
// 早期这类解析器最常见的崩法就是切片越界 panic —— 恶意/残缺文件能让
// 整个进程挂掉。这里逐个区域截断，断言「返回 error 且不 panic」。
func TestDecryptNCM_截断文件(t *testing.T) {
	dir := t.TempDir()

	realKey := []byte("truncate-key-16byte")
	audio := []byte("0123456789abcdefghijklmnopqrstuvwxyz")
	raw := ncmBuilder(t, realKey, `{"musicName":"截断","artist":[]}`, nil, audio)

	// 只在「头部区域」截断：音频区被截断仍可能解出（长度变短），
	// 那不是解析错误，不在本次断言范围。
	cuts := []int{0, 4, 8, 12, 16, 24, 40, 64, len(raw) / 2}
	for _, n := range cuts {
		if n >= len(raw) {
			continue
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("截断到 %d 字节时 panic：%v", n, r)
				}
			}()
			in := writeFile(t, dir, "trunc.ncm", raw[:n])
			out := filepath.Join(dir, "trunc.mp3")
			if _, err := DecryptNCM(in, out); err == nil {
				t.Errorf("截断到 %d 字节应返回错误，实际无错误", n)
			}
		}()
	}
}

// TestNCMDecryptAudio_往返 RC4 变种是对称的：加密再解密回到原文。
func TestNCMDecryptAudio_往返(t *testing.T) {
	key := []byte("rc4-variant-key")
	plain := []byte("hello ncm audio payload, longer than 256 bytes to exercise keystream wrap around .................")

	enc := ncmDecryptAudio(key, plain)
	if bytes.Equal(enc, plain) {
		t.Fatal("加密结果不应与原文相同")
	}
	dec := ncmDecryptAudio(key, enc)
	if !bytes.Equal(dec, plain) {
		t.Errorf("往返后不一致：\n got %q\nwant %q", dec, plain)
	}
}

// TestNCMDecryptAudio_空密钥 空密钥时原样返回（不崩溃）。
func TestNCMDecryptAudio_空密钥(t *testing.T) {
	data := []byte("payload")
	got := ncmDecryptAudio(nil, data)
	if !bytes.Equal(got, data) {
		t.Errorf("空密钥应原样返回，实际 %q", got)
	}
}

// TestAesEcbDecrypt_往返 AES-128-ECB 解密应还原原文。
func TestAesEcbDecrypt_往返(t *testing.T) {
	key := []byte("0123456789abcdef")
	plain := []byte("aes-ecb roundtrip payload")

	enc := aesEcbEncrypt(t, key, plain)
	got, err := aesEcbDecrypt(key, enc)
	if err != nil {
		t.Fatalf("解密失败：%v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("往返后不一致：\n got %q\nwant %q", got, plain)
	}
}

// TestAesEcbDecrypt_密文长度非法 长度不是块大小整数倍必须报错
// （否则会读越界或静默解出垃圾）。
func TestAesEcbDecrypt_密文长度非法(t *testing.T) {
	key := []byte("0123456789abcdef")
	for _, n := range []int{1, 5, 15, 17, 33} {
		data := make([]byte, n)
		if _, err := aesEcbDecrypt(key, data); err == nil {
			t.Errorf("密文长度 %d 不是 16 的整数倍，应返回错误", n)
		}
	}
}

// TestAesEcbDecrypt_非法填充宽松处理 填充字节不合法时按原样返回
// （与 ncmdump 同样的宽松策略，不能因此 panic 或丢数据）。
func TestAesEcbDecrypt_非法填充宽松处理(t *testing.T) {
	key := []byte("0123456789abcdef")
	// 末字节声明填充 0x05，但前 4 字节不是 0x05 → 填充非法。
	data := make([]byte, 16)
	data[15] = 0x05
	got, err := aesEcbDecrypt(key, data)
	if err != nil {
		t.Fatalf("非法填充不应报错：%v", err)
	}
	if len(got) != 16 {
		t.Errorf("填充非法时应原样返回 16 字节，实际 %d 字节", len(got))
	}
}

// TestDecodeNCMMeta_元数据解析 元数据区解析后应得到结构化元信息。
func TestDecodeNCMMeta_元数据解析(t *testing.T) {
	metaJSON := `{"musicName":"解析测试","artist":[{"name":"甲"}]}`
	enc := aesEcbEncrypt(t, ncmModifyKey, []byte("music:"+metaJSON))
	raw := []byte(ncmMetaPrefix + base64.StdEncoding.EncodeToString(enc))

	meta, err := decodeNCMMeta(raw)
	if err != nil {
		t.Fatalf("解析元数据失败：%v", err)
	}
	if meta.MusicName != "解析测试" {
		t.Errorf("MusicName = %q，期望 %q", meta.MusicName, "解析测试")
	}
	if len(meta.Artist) != 1 || meta.Artist[0].Name != "甲" {
		t.Errorf("Artist 解析错误：%+v", meta.Artist)
	}
}

// TestDecodeNCMMeta_非法元数据 缺前缀/坏 base64/缺 music: 前缀都要报错。
func TestDecodeNCMMeta_非法元数据(t *testing.T) {
	bad := []string{
		"",
		"163 key(Don't modify):!!!not-base64!!!",
		"wrong prefix: " + base64.StdEncoding.EncodeToString([]byte("0123456789abcdef")),
	}
	for _, s := range bad {
		if _, err := decodeNCMMeta([]byte(s)); err == nil {
			t.Errorf("非法元数据 %q 应返回错误", s)
		}
	}
}

// TestNCMMetadata_JSON字段名 元信息 JSON 字段名与契约一致（改名会让
// 老文件解出空标题）。
func TestNCMMetadata_JSON字段名(t *testing.T) {
	raw := []byte(`{"musicName":"m","artist":[{"name":"a"}],"album":"al","albumPic":"p","format":"flac","bitrate":999}`)
	var m NCMMetadata
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if m.MusicName != "m" || m.Album != "al" || m.AlbumPic != "p" || m.Format != "flac" || m.Bitrate != 999 {
		t.Errorf("字段解析错误：%+v", m)
	}
	if len(m.Artist) != 1 || m.Artist[0].Name != "a" {
		t.Errorf("Artist 解析错误：%+v", m.Artist)
	}
}
