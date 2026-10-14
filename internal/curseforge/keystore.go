package curseforge

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"sync"

	"hytale-server-manager/internal/fsutil"
	"hytale-server-manager/internal/secretbox"
)

// KeyStore keeps the CurseForge API key encrypted on disk.
type KeyStore struct {
	path string
	box  *secretbox.Box

	mu  sync.RWMutex
	key string
}

// OpenKeyStore loads the key stored at path, if any. An unreadable file
// (e.g. lost encryption key) is treated as "no key configured".
func OpenKeyStore(path string, box *secretbox.Box) *KeyStore {
	s := &KeyStore{path: path, box: box}
	if data, err := os.ReadFile(path); err == nil {
		if plain, err := box.Open(data); err == nil {
			s.key = string(plain)
		}
	}
	return s
}

// Get returns the API key, or "" when none is configured.
func (s *KeyStore) Get() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.key
}

// Set stores a new API key.
func (s *KeyStore) Set(key string) error {
	key = strings.TrimSpace(key)
	sealed, err := s.box.Seal([]byte(key))
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fsutil.WriteFileAtomic(s.path, sealed, 0o600); err != nil {
		return err
	}
	s.key = key
	return nil
}

// Clear removes the stored key.
func (s *KeyStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	s.key = ""
	return nil
}

// Hint returns a masked form of the key that is safe to display.
func Hint(key string) string {
	if len(key) < 8 {
		return ""
	}
	return "••••" + key[len(key)-4:]
}
