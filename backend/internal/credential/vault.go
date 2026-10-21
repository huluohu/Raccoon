// Package credential 实现凭据中心：主密钥管理 + AES-256-GCM 加密 + 密文存储。
//
// 主密钥在 data/keys/master.key 首次初始化时生成（32 字节随机，权限 0600）。
// 换机迁移必须带上该文件，否则已有凭据无法解密——这一点在部署文档里明说。
package credential

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrNoKey 主密钥不存在（数据目录迁移时漏带）。
var ErrNoKey = errors.New("master key not found")

// ErrDecrypt 解密失败（密钥不匹配或密文损坏）。
var ErrDecrypt = errors.New("decrypt failed")

// Vault 主密钥保管 + 加解密。
type Vault struct {
	keyPath string
	aead    cipher.AEAD
}

// OpenVault 打开或创建主密钥（首次生成）。
func OpenVault(dataDir string) (*Vault, error) {
	keyPath := filepath.Join(dataDir, "keys", "master.key")
	key, err := loadOrCreateKey(keyPath)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("初始化 AES: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("初始化 GCM: %w", err)
	}
	return &Vault{keyPath: keyPath, aead: aead}, nil
}

// loadOrCreateKey 读主密钥；不存在则生成并以 0600 写入。
func loadOrCreateKey(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err == nil {
		key, derr := base64.StdEncoding.DecodeString(string(raw))
		if derr != nil || len(key) != 32 {
			return nil, fmt.Errorf("主密钥文件损坏: %s", path)
		}
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	enc := base64.StdEncoding.EncodeToString(key)
	// 先以 0600 写临时文件再改名，避免半截密钥文件。
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(enc), 0o600); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return nil, err
	}
	return key, nil
}

// Encrypt 加密明文（返回带 nonce 的密文）。
func (v *Vault) Encrypt(plain []byte) ([]byte, error) {
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return v.aead.Seal(nonce, nonce, plain, nil), nil
}

// Decrypt 解密密文。
func (v *Vault) Decrypt(cipherText []byte) ([]byte, error) {
	ns := v.aead.NonceSize()
	if len(cipherText) < ns {
		return nil, ErrDecrypt
	}
	nonce, ct := cipherText[:ns], cipherText[ns:]
	plain, err := v.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plain, nil
}

// KeyPath 主密钥文件路径（诊断包导出时提示）。
func (v *Vault) KeyPath() string { return v.keyPath }

// Mask 生成打码预览（前端展示用；只露首尾各 2 字符）。
func Mask(plain string) string {
	if plain == "" {
		return ""
	}
	r := []rune(plain)
	if len(r) <= 4 {
		return "****"
	}
	return string(r[:2]) + "****" + string(r[len(r)-2:])
}
