package autoupdate

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

type harness struct {
	now      time.Time
	pending  time.Time
	hasPend  bool
	running  bool
	players  int
	backupEr error
	events   []string
	said     []string
	updater  *Updater
}

func newHarness() *harness {
	h := &harness{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), running: true}
	h.pending, h.hasPend = h.now, true
	h.updater = New(Deps{
		Config: func() Config {
			return Config{Enabled: true, IdleAfter: 10 * time.Minute, ForceAfter: 6 * time.Hour}
		},
		Running:      func() bool { return h.running },
		PlayerCount:  func() int { return h.players },
		PendingSince: func() (time.Time, bool) { return h.pending, h.hasPend },
		Stop:         func() error { h.running = false; h.events = append(h.events, "stop"); return nil },
		Start:        func(context.Context) error { h.running = true; h.events = append(h.events, "start"); return nil },
		Send: func(c string) error {
			h.said = append(h.said, c)
			return nil
		},
		Backup: func() error { h.events = append(h.events, "backup"); return h.backupEr },
		RequestUpdate: func(after func()) error {
			h.events = append(h.events, "update")
			after()
			return nil
		},
		Now:  func() time.Time { return h.now },
		Wait: func(time.Duration) {},
	})
	return h
}

func (h *harness) advance(d time.Duration, step time.Duration) {
	for end := h.now.Add(d); h.now.Before(end); {
		h.now = h.now.Add(step)
		h.updater.tick(context.Background())
	}
}

func TestIdleServerUpdatesAfterTenMinutes(t *testing.T) {
	h := newHarness()
	h.advance(9*time.Minute, time.Second)
	if len(h.events) != 0 {
		t.Fatalf("updated too early: %v", h.events)
	}
	h.advance(2*time.Minute, time.Second)
	want := []string{"stop", "backup", "update", "start"}
	if !slices.Equal(h.events, want) {
		t.Fatalf("events = %v, want %v", h.events, want)
	}
}

func TestPlayerActivityResetsIdleTimer(t *testing.T) {
	h := newHarness()
	h.advance(8*time.Minute, time.Second)
	h.players = 1
	h.advance(time.Minute, time.Second)
	h.players = 0
	h.advance(9*time.Minute, time.Second)
	if len(h.events) != 0 {
		t.Fatalf("idle timer was not reset: %v", h.events)
	}
	h.advance(2*time.Minute, time.Second)
	if len(h.events) == 0 {
		t.Fatal("update never applied")
	}
}

func TestNoUpdateWithoutPendingOrWhenDisabled(t *testing.T) {
	h := newHarness()
	h.hasPend = false
	h.advance(time.Hour, time.Second)
	if len(h.events) != 0 {
		t.Fatalf("updated without pending update: %v", h.events)
	}
}

func TestForcedUpdateCountsDown(t *testing.T) {
	h := newHarness()
	h.players = 2
	h.advance(6*time.Hour-time.Minute, 10*time.Second)
	if len(h.events) != 0 || len(h.said) != 0 {
		t.Fatalf("acted before the force delay: %v %v", h.events, h.said)
	}
	h.advance(32*time.Minute, time.Second)

	if want := []string{"stop", "backup", "update", "start"}; !slices.Equal(h.events, want) {
		t.Fatalf("events = %v, want %v", h.events, want)
	}
	for _, expected := range []string{"30 minutes", "15 minutes", "10 minutes", "5 minutes", "1 minute.", "30 secondes", "10 secondes"} {
		if !slices.ContainsFunc(h.said, func(s string) bool { return strings.HasPrefix(s, "/notify") && strings.Contains(s, "dans "+expected) }) {
			t.Errorf("missing /notify %q in %v", expected, h.said)
		}
	}
	titles, saves := 0, 0
	for _, s := range h.said {
		if strings.HasPrefix(s, "/eventtitle --title") {
			titles++
		}
		if s == "/world save" {
			saves++
		}
	}
	if titles != 8 || saves != 1 { // seven warnings + final notice
		t.Errorf("got %d titles and %d native saves, want 8 and 1: %v", titles, saves, h.said)
	}
}

func TestCountdownEndsEarlyWhenPlayersLeave(t *testing.T) {
	h := newHarness()
	h.players = 1
	h.advance(6*time.Hour+5*time.Minute, time.Second)
	h.players = 0
	h.advance(11*time.Minute, time.Second)
	if want := []string{"stop", "backup", "update", "start"}; !slices.Equal(h.events, want) {
		t.Fatalf("events = %v, want %v", h.events, want)
	}
}

func TestBackupFailureAbortsUpdateAndRestartsServer(t *testing.T) {
	h := newHarness()
	h.backupEr = errors.New("disque plein")
	h.advance(11*time.Minute, time.Second)
	if want := []string{"stop", "backup", "start"}; !slices.Equal(h.events, want) {
		t.Fatalf("events = %v, want %v", h.events, want)
	}
	// No retry loop right after a failure.
	h.advance(30*time.Minute, time.Second)
	if len(h.events) != 3 {
		t.Fatalf("retried too soon: %v", h.events)
	}
}
