// Package secrets encrypts provider credentials at rest with AES-256-GCM.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

type Box struct {
	aead cipher.AEAD
}

// New parses a 32-byte key supplied as base64 or as raw 32 bytes.
func New(raw string) (*Box, error) {
	if raw == "" {
		return nil, errors.New("release master key is required")
	}
	key := []byte(raw)
	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil && len(decoded) == 32 {
		key = decoded
	}
	if len(key) != 32 {
		return nil, errors.New("release master key must be 32 bytes (raw or base64)")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

func (b *Box) Encrypt(plaintext string) ([]byte, []byte, error) {
	if plaintext == "" {
		return nil, nil, nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	return b.aead.Seal(nil, nonce, []byte(plaintext), nil), nonce, nil
}

func (b *Box) Decrypt(ciphertext, nonce []byte) (string, error) {
	if len(ciphertext) == 0 {
		return "", nil
	}
	if len(nonce) != b.aead.NonceSize() {
		return "", fmt.Errorf("invalid nonce size")
	}
	plain, err := b.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
