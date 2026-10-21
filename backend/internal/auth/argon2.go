package auth

// argon2.go：Argon2id KDF 封装（golang.org/x/crypto 实现，纯 Go 无 CGO）。

import "golang.org/x/crypto/argon2"

func argon2IDKey(password, salt []byte, time, memory uint32, threads uint8, keyLen uint32) []byte {
	return argon2.IDKey(password, salt, time, memory, threads, keyLen)
}
