package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"sync"
	"time"
)

type session struct {
	createdAt time.Time
	lastSeen  time.Time
}

// SessionManager issues and validates opaque session tokens kept in memory.
// Only a hash of each token is stored.
type SessionManager struct {
	idleTTL     time.Duration
	absoluteTTL time.Duration
	now         func() time.Time

	mu       sync.Mutex
	sessions map[[sha256.Size]byte]*session
}

// NewSessionManager creates a manager with the given expirations.
func NewSessionManager(idleTTL, absoluteTTL time.Duration) *SessionManager {
	return &SessionManager{
		idleTTL:     idleTTL,
		absoluteTTL: absoluteTTL,
		now:         time.Now,
		sessions:    make(map[[sha256.Size]byte]*session),
	}
}

// Create starts a new session and returns its token.
func (m *SessionManager) Create() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	now := m.now()

	m.mu.Lock()
	defer m.mu.Unlock()
	m.purgeExpiredLocked(now)
	m.sessions[sha256.Sum256([]byte(token))] = &session{createdAt: now, lastSeen: now}
	return token, nil
}

// Validate reports whether token belongs to a live session and refreshes it.
func (m *SessionManager) Validate(token string) bool {
	if token == "" {
		return false
	}
	key := sha256.Sum256([]byte(token))
	now := m.now()

	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[key]
	if !ok {
		return false
	}
	if m.expired(s, now) {
		delete(m.sessions, key)
		return false
	}
	s.lastSeen = now
	return true
}

// Revoke ends the session identified by token.
func (m *SessionManager) Revoke(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sha256.Sum256([]byte(token)))
}

// RevokeAll ends every session (e.g. after a password change).
func (m *SessionManager) RevokeAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	clear(m.sessions)
}

// IdleTTL is the inactivity timeout, used for the cookie lifetime.
func (m *SessionManager) IdleTTL() time.Duration { return m.idleTTL }

func (m *SessionManager) expired(s *session, now time.Time) bool {
	return now.Sub(s.lastSeen) > m.idleTTL || now.Sub(s.createdAt) > m.absoluteTTL
}

func (m *SessionManager) purgeExpiredLocked(now time.Time) {
	for key, s := range m.sessions {
		if m.expired(s, now) {
			delete(m.sessions, key)
		}
	}
}
