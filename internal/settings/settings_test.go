package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDisableSentryIsMigrated(t *testing.T) {
	for legacy, want := range map[string]string{
		`{"disableSentry":true}`:                            ErrorReportingOff,
		`{"disableSentry":false}`:                           ErrorReportingHypixel,
		`{"errorReporting":"manager","disableSentry":true}`: ErrorReportingManager,
	} {
		path := filepath.Join(t.TempDir(), "settings.json")
		if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
			t.Fatal(err)
		}
		store, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := store.Get().ErrorReporting; got != want {
			t.Errorf("%s: got %q, want %q", legacy, got, want)
		}
	}
}

func TestForwardDSNIsValidated(t *testing.T) {
	s := Defaults()
	s.SentryForwardDSN = "not a dsn"
	if s.Validate() == nil {
		t.Error("invalid DSN accepted")
	}
	s.SentryForwardDSN = "https://key@o1.ingest.sentry.io/123"
	if err := s.Validate(); err != nil {
		t.Error(err)
	}
}
