package auth

import (
	"sync"
	"time"
)

type attempts struct {
	failures    int
	windowStart time.Time
	lockedUntil time.Time
}

// LoginLimiter locks a client out after too many failed logins.
type LoginLimiter struct {
	maxFailures int
	window      time.Duration
	lockout     time.Duration
	now         func() time.Time

	mu      sync.Mutex
	entries map[string]*attempts
}

// NewLoginLimiter allows maxFailures failures per window before locking the
// client out for the lockout duration.
func NewLoginLimiter(maxFailures int, window, lockout time.Duration) *LoginLimiter {
	return &LoginLimiter{
		maxFailures: maxFailures,
		window:      window,
		lockout:     lockout,
		now:         time.Now,
		entries:     make(map[string]*attempts),
	}
}

// Allow reports whether key may attempt a login, and otherwise for how long
// it must wait.
func (l *LoginLimiter) Allow(key string) (bool, time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.entries[key]
	if !ok {
		return true, 0
	}
	if now.Before(a.lockedUntil) {
		return false, a.lockedUntil.Sub(now)
	}
	return true, 0
}

// RecordFailure registers a failed attempt for key.
func (l *LoginLimiter) RecordFailure(key string) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.purgeLocked(now)

	a, ok := l.entries[key]
	if !ok || now.Sub(a.windowStart) > l.window {
		a = &attempts{windowStart: now}
		l.entries[key] = a
	}
	a.failures++
	if a.failures >= l.maxFailures {
		a.lockedUntil = now.Add(l.lockout)
		a.failures = 0
		a.windowStart = now
	}
}

// RecordSuccess forgets previous failures of key.
func (l *LoginLimiter) RecordSuccess(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

func (l *LoginLimiter) purgeLocked(now time.Time) {
	for key, a := range l.entries {
		if now.After(a.lockedUntil) && now.Sub(a.windowStart) > l.window {
			delete(l.entries, key)
		}
	}
}
