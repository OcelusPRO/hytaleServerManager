// Package hytaleauth keeps the server authenticated with Hytale: it runs the
// device code flow (method B) and issues game sessions that are handed to the
// server process through environment variables (method C).
package hytaleauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"hytale-server-manager/internal/fsutil"
	"hytale-server-manager/internal/secretbox"
)

// Credentials are the long-lived Hytale credentials of the server owner.
type Credentials struct {
	RefreshToken    string    `json:"refreshToken"`
	AccessToken     string    `json:"accessToken"`
	AccessExpiresAt time.Time `json:"accessExpiresAt"`
	RefreshedAt     time.Time `json:"refreshedAt"`
	ProfileUUID     string    `json:"profileUuid"`
	ProfileName     string    `json:"profileName"`
}

// TokenStore persists Credentials encrypted on disk.
type TokenStore struct {
	path string
	box  *secretbox.Box
}

// NewTokenStore returns a store writing to path, encrypted with box.
func NewTokenStore(path string, box *secretbox.Box) *TokenStore {
	return &TokenStore{path: path, box: box}
}

// Load returns the stored credentials, with found=false when none exist.
func (s *TokenStore) Load() (creds Credentials, found bool, err error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return Credentials{}, false, nil
	}
	if err != nil {
		return Credentials{}, false, err
	}
	plain, err := s.box.Open(data)
	if err != nil {
		return Credentials{}, false, fmt.Errorf("decrypt hytale credentials: %w", err)
	}
	if err := json.Unmarshal(plain, &creds); err != nil {
		return Credentials{}, false, fmt.Errorf("decode hytale credentials: %w", err)
	}
	return creds, true, nil
}

// Save encrypts and writes creds.
func (s *TokenStore) Save(creds Credentials) error {
	plain, err := json.Marshal(creds)
	if err != nil {
		return err
	}
	sealed, err := s.box.Seal(plain)
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(s.path, sealed, 0o600)
}

// Clear deletes the stored credentials.
func (s *TokenStore) Clear() error {
	err := os.Remove(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
