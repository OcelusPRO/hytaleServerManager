package auth

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("correct horse battery", hash) {
		t.Error("valid password rejected")
	}
	if VerifyPassword("wrong", hash) {
		t.Error("invalid password accepted")
	}
	if VerifyPassword("x", "garbage") {
		t.Error("malformed hash accepted")
	}
}

func TestCredentialStoreChange(t *testing.T) {
	store, err := OpenCredentialStore(filepath.Join(t.TempDir(), "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	if store.Initialized() {
		t.Fatal("fresh store should not be initialized")
	}
	if err := store.Set("initial-password"); err != nil {
		t.Fatal(err)
	}
	if err := store.Change("bad", "a-much-longer-password"); err != ErrWrongPassword {
		t.Errorf("wrong current password: got %v", err)
	}
	if err := store.Change("initial-password", "short"); err != ErrWeakPassword {
		t.Errorf("weak password: got %v", err)
	}
	if err := store.Change("initial-password", "a-much-longer-password"); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenCredentialStore(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.Verify("a-much-longer-password") {
		t.Error("password not persisted")
	}
}

func TestSessionExpiry(t *testing.T) {
	m := NewSessionManager(time.Minute, time.Hour)
	now := time.Now()
	m.now = func() time.Time { return now }
	token, err := m.Create()
	if err != nil {
		t.Fatal(err)
	}
	if !m.Validate(token) {
		t.Fatal("fresh session rejected")
	}
	now = now.Add(2 * time.Minute)
	if m.Validate(token) {
		t.Error("idle session accepted")
	}
	if m.Validate("") || m.Validate("forged") {
		t.Error("unknown token accepted")
	}
}

func TestLoginLimiterLocksOut(t *testing.T) {
	l := NewLoginLimiter(3, time.Minute, 5*time.Minute)
	now := time.Now()
	l.now = func() time.Time { return now }
	for range 3 {
		l.RecordFailure("1.2.3.4")
	}
	if ok, _ := l.Allow("1.2.3.4"); ok {
		t.Fatal("client should be locked out")
	}
	if ok, _ := l.Allow("5.6.7.8"); !ok {
		t.Error("other clients must not be affected")
	}
	now = now.Add(6 * time.Minute)
	if ok, _ := l.Allow("1.2.3.4"); !ok {
		t.Error("lockout should expire")
	}
}
