// Package auth protects the management interface: password storage,
// sessions and brute-force mitigation.
package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

const (
	hashScheme     = "pbkdf2-sha256"
	hashIterations = 600_000 // OWASP 2023 recommendation for PBKDF2-HMAC-SHA256
	saltSize       = 16
	derivedKeySize = 32
)

// HashPassword derives a salted hash encoded as "scheme$iterations$salt$hash".
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, hashIterations, derivedKeySize)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return strings.Join([]string{
		hashScheme,
		strconv.Itoa(hashIterations),
		enc.EncodeToString(salt),
		enc.EncodeToString(key),
	}, "$"), nil
}

// VerifyPassword reports whether password matches the encoded hash.
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != hashScheme {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 1 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := enc.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// GeneratePassword returns a random password with 128 bits of entropy.
func GeneratePassword() string {
	return rand.Text()
}
