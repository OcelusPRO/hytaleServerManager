package players

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"hytale-server-manager/internal/database"
)

func newTestShared(t *testing.T) (*SharedLists, *Registry) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "manager.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewSharedLists(db), NewRegistry(db)
}

func uuids(entries []SharedEntry) []string {
	out := []string{}
	for _, e := range entries {
		out = append(out, e.Player.UUID)
	}
	slices.Sort(out)
	return out
}

func mustShared(t *testing.T, l *SharedLists) Shared {
	t.Helper()
	s, err := l.Get()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func roster(bans []Ban, ops []PlayerRef, whitelist []PlayerRef, enabled bool) Roster {
	if bans == nil {
		bans = []Ban{}
	}
	return Roster{Bans: bans, Operators: ops, Whitelist: Whitelist{Enabled: enabled, Players: whitelist}}
}

var (
	alice = PlayerRef{UUID: aliceUUID, Name: "Ocelus"}
	bob   = PlayerRef{UUID: bobUUID, Name: "Bob"}
)

func TestSharedListsFollowEveryWorld(t *testing.T) {
	l, reg := newTestShared(t)
	t0 := time.Date(2026, 10, 22, 14, 0, 0, 0, time.UTC)

	// The first sync fills the shared lists from the world in use.
	w1 := roster(nil, []PlayerRef{alice}, []PlayerRef{bob}, true)
	if err := l.Sync("w1", w1, t0); err != nil {
		t.Fatal(err)
	}
	s := mustShared(t, l)
	if got := uuids(s.Lists[ListOperators]); !slices.Equal(got, []string{aliceUUID}) || !s.WhitelistEnabled || len(s.Lists[ListWhitelist]) != 1 {
		t.Fatalf("seeded %+v", s)
	}

	// Another world, seen for the first time, does not change them...
	w2 := roster(nil, nil, nil, false)
	if err := l.Sync("w2", w2, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if s := mustShared(t, l); len(s.Lists[ListOperators]) != 1 || !s.WhitelistEnabled {
		t.Fatalf("a new world changed the shared lists: %+v", s)
	}
	// ...but must be brought in line when it boots.
	changes, whitelist, err := l.Plan(w2, t0)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || changes[0].Action != ActionOp || changes[1].Action != ActionWhitelistAdd || whitelist == nil || !*whitelist {
		t.Fatalf("plan %+v, whitelist %v", changes, whitelist)
	}

	// A ban made from the game console in w2 reaches the shared lists.
	t1 := t0.Add(2 * time.Minute)
	w2 = roster([]Ban{{Player: bob, Reason: "grief", Timestamp: &t1}}, []PlayerRef{alice}, []PlayerRef{bob}, true)
	if err := l.Sync("w2", w2, t1); err != nil {
		t.Fatal(err)
	}
	s = mustShared(t, l)
	if got := uuids(s.Lists[ListBans]); !slices.Equal(got, []string{bobUUID}) || s.Lists[ListBans][0].Reason != "grief" {
		t.Fatalf("ban not shared: %+v", s.Lists[ListBans])
	}
	p, _, _ := reg.Get(bobUUID)
	if len(p.BanHistory) != 1 || p.BanHistory[0].SaveID != "w2" || len(p.Actions) != 1 || p.Actions[0].Source != SourceServer {
		t.Fatalf("history %+v / %+v", p.BanHistory, p.Actions)
	}
	// w1 receives it at its next boot.
	if changes, _, _ := l.Plan(w1, t1); len(changes) != 1 || changes[0].Action != ActionBan || changes[0].Reason != "grief" {
		t.Fatalf("plan for w1 %+v", changes)
	}

	// Removing the operator from the console of w1 removes it everywhere.
	w1 = roster(nil, nil, []PlayerRef{bob}, true)
	if err := l.Sync("w1", w1, t1.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if s := mustShared(t, l); len(s.Lists[ListOperators]) != 0 {
		t.Fatalf("deop not shared: %+v", s.Lists[ListOperators])
	}
	if changes, _, _ := l.Plan(w2, t1); len(changes) != 1 || changes[0].Action != ActionDeop {
		t.Fatalf("plan for w2 %+v", changes)
	}
}

func TestSharedListsPanelWhileStopped(t *testing.T) {
	l, reg := newTestShared(t)
	t0 := time.Date(2026, 10, 22, 14, 0, 0, 0, time.UTC)
	world := roster(nil, nil, nil, false)
	l.Sync("w", world, t0)

	if err := l.Add(ListBans, alice, "spam", "w", t0); err != nil {
		t.Fatal(err)
	}
	if err := l.Add(ListOperators, bob, "", "w", t0); err != nil {
		t.Fatal(err)
	}
	// Still missing from the world's files: pending, then applied at boot.
	pending := pendingEntries(mustShared(t, l), world)
	if !slices.Equal(pending[ListBans], []string{aliceUUID}) || !slices.Equal(pending[ListOperators], []string{bobUUID}) {
		t.Fatalf("pending %+v", pending)
	}
	// A sync in the meantime keeps them (the files never had them).
	if err := l.Sync("w", world, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if s := mustShared(t, l); len(s.Lists[ListBans]) != 1 || len(s.Lists[ListOperators]) != 1 {
		t.Fatalf("lost by a sync: %+v", s.Lists)
	}
	// Once the server applied them, nothing is logged twice.
	t1 := t0.Add(2 * time.Minute)
	applied := roster([]Ban{{Player: alice, Reason: "spam", Timestamp: &t1}}, []PlayerRef{bob}, nil, false)
	if err := l.Sync("w", applied, t1); err != nil {
		t.Fatal(err)
	}
	if p, _, _ := reg.Get(aliceUUID); len(p.Actions) != 1 || p.Actions[0].Source != SourcePanel || len(p.BanHistory) != 1 {
		t.Fatalf("alice %+v / %+v", p.Actions, p.BanHistory)
	}

	if err := l.Remove(ListBans, aliceUUID, "w", t1.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if p, _, _ := reg.Get(aliceUUID); len(p.BanHistory) != 1 || p.BanHistory[0].UnbannedAt.IsZero() {
		t.Fatalf("unban not recorded: %+v", p.BanHistory)
	}
	if err := l.SetWhitelistEnabled(true, "w"); err != nil || !mustShared(t, l).WhitelistEnabled {
		t.Fatalf("whitelist switch: %v", err)
	}
}

func TestSharedTemporaryBansExpire(t *testing.T) {
	l, reg := newTestShared(t)
	t0 := time.Date(2026, 10, 22, 14, 0, 0, 0, time.UTC)
	end := t0.Add(time.Hour)
	l.Sync("w", roster([]Ban{{Player: bob, Timestamp: &t0, ExpiresOn: &end}}, nil, nil, false), t0)
	if s := mustShared(t, l); len(s.Lists[ListBans]) != 1 {
		t.Fatalf("ban not seeded: %+v", s.Lists)
	}
	// Temporary bans are not sent to other worlds (the console bans for good).
	if changes, _, _ := l.Plan(roster(nil, nil, nil, false), t0); len(changes) != 0 {
		t.Fatalf("plan %+v", changes)
	}
	l.Sync("w", roster(nil, nil, nil, false), t0.Add(2*time.Hour))
	if s := mustShared(t, l); len(s.Lists[ListBans]) != 0 {
		t.Fatalf("expired ban kept: %+v", s.Lists)
	}
	if p, _, _ := reg.Get(bobUUID); len(p.BanHistory) != 1 || !p.BanHistory[0].UnbannedAt.Equal(end) {
		t.Fatalf("history %+v", p.BanHistory)
	}
}

type fakeConsole struct {
	live bool
	sent []string
}

func (c *fakeConsole) UsesSave(string) bool { return c.live }
func (c *fakeConsole) Send(cmd string) error {
	c.sent = append(c.sent, cmd)
	return nil
}

func TestServiceOfflineActionsAndBoot(t *testing.T) {
	l, reg := newTestShared(t)
	dir := t.TempDir()
	console := &fakeConsole{}
	svc := NewService(console, NewNameDirectory().WithRegistry(reg), func() (string, string, error) { return "w", dir, nil }).WithRegistry(reg, l)
	reg.Joined(aliceUUID, "Ocelus", "w", time.Now())
	if _, _, err := svc.Roster(); err != nil { // first sync
		t.Fatal(err)
	}

	// Stopped: a known player can be banned and made operator...
	if err := svc.Act(ActionBan, "Ocelus", "spam"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Act(ActionOp, aliceUUID, ""); err != nil {
		t.Fatal(err)
	}
	// ...an unknown one cannot, and a kick needs the server.
	if err := svc.Act(ActionBan, "Nobody", ""); err != ErrUnknownPlayer {
		t.Fatalf("unknown player: %v", err)
	}
	if err := svc.Act(ActionKick, "Ocelus", ""); err != ErrServerStopped {
		t.Fatalf("kick: %v", err)
	}
	roster, pending, err := svc.Roster()
	if err != nil || len(roster.Bans) != 1 || roster.Bans[0].Reason != "spam" || len(pending.Bans) != 1 || len(pending.Operators) != 1 {
		t.Fatalf("roster %+v, pending %+v, %v", roster, pending, err)
	}
	if len(console.sent) != 0 {
		t.Fatalf("commands sent while stopped: %v", console.sent)
	}
	// Removing what was never applied to the world works too.
	if err := svc.Act(ActionDeop, "Ocelus", ""); err != nil {
		t.Fatal(err)
	}

	// At boot, the world receives the shared lists.
	console.live = true
	n, err := svc.Reconcile()
	if err != nil || n != 1 || !slices.Equal(console.sent, []string{"/ban Ocelus spam"}) {
		t.Fatalf("reconcile %d %v %v", n, console.sent, err)
	}
}
