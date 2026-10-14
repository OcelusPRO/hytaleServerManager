package auth

import (
	"errors"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"hytale-server-manager/internal/fsutil"
)

// MinPasswordLength is the minimum length accepted for a new password.
const MinPasswordLength = 12

// ErrWrongPassword is returned when the current password does not match.
var ErrWrongPassword = errors.New("wrong password")

// ErrWeakPassword is returned when a new password is too short.
var ErrWeakPassword = fmt.Errorf("password must contain at least %d characters", MinPasswordLength)

type storedCredentials struct {
	PasswordHash string    `json:"passwordHash"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// CredentialStore persists the hash of the panel password.
type CredentialStore struct {
	path string

	mu   sync.RWMutex
	hash string
}

// OpenCredentialStore loads the stored credentials, if any.
func OpenCredentialStore(path string) (*CredentialStore, error) {
	var stored storedCredentials
	if _, err := fsutil.ReadJSON(path, &stored); err != nil {
		return nil, err
	}
	return &CredentialStore{path: path, hash: stored.PasswordHash}, nil
}

// Initialized reports whether a password has been configured.
func (s *CredentialStore) Initialized() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hash != ""
}

// Verify checks a password against the stored hash.
func (s *CredentialStore) Verify(password string) bool {
	s.mu.RLock()
	hash := s.hash
	s.mu.RUnlock()
	return hash != "" && VerifyPassword(password, hash)
}

// Set replaces the password unconditionally (used for bootstrap/reset).
func (s *CredentialStore) Set(password string) error {
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := storedCredentials{PasswordHash: hash, UpdatedAt: time.Now().UTC()}
	if err := fsutil.WriteJSONAtomic(s.path, stored, 0o600); err != nil {
		return err
	}
	s.hash = hash
	return nil
}

// Change replaces the password after checking the current one.
func (s *CredentialStore) Change(current, next string) error {
	if !s.Verify(current) {
		return ErrWrongPassword
	}
	if utf8.RuneCountInString(next) < MinPasswordLength {
		return ErrWeakPassword
	}
	return s.Set(next)
}
