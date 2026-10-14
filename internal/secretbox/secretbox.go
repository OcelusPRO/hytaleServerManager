// Package secretbox encrypts small secrets at rest with AES-256-GCM.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"hytale-server-manager/internal/fsutil"
)

const keySize = 32

// Box seals and opens secrets with a key persisted on disk.
type Box struct {
	aead cipher.AEAD
}

// LoadOrCreate loads the key at keyPath, generating it on first use.
func LoadOrCreate(keyPath string) (*Box, error) {
	key, err := os.ReadFile(keyPath)
	if errors.Is(err, fs.ErrNotExist) {
		key = make([]byte, keySize)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generate key: %w", err)
		}
		if err := fsutil.WriteFileAtomic(keyPath, key, 0o600); err != nil {
			return nil, fmt.Errorf("store key: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("read key: %w", err)
	}
	if len(key) != keySize {
		return nil, fmt.Errorf("invalid key size in %s", keyPath)
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

// Seal encrypts plaintext; the random nonce is prepended to the output.
func (b *Box) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Open decrypts data produced by Seal.
func (b *Box) Open(data []byte) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(data) < n {
		return nil, errors.New("ciphertext too short")
	}
	return b.aead.Open(nil, data[:n], data[n:], nil)
}
