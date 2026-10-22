package players

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hytale-server-manager/internal/database"
)

const (
	aliceUUID = "696f06b8-0d23-4901-aa53-103b9703584c"
	bobUUID   = "11111111-2222-3333-4444-555555555555"
)

func TestBuildCommand(t *testing.T) {
	cases := []struct {
		action  Action
		player  string
		reason  string
		want    string
		wantErr error
	}{
		{ActionKick, "Steve_01", "ignored", "/kick Steve_01", nil},
		{ActionBan, "Steve", "griefing", "/ban Steve griefing", nil},
		{ActionUnban, aliceUUID, "", "/unban " + aliceUUID, nil},
		{ActionOp, "Steve", "", "/op add Steve", nil},
		{ActionKick, "Steve\n/stop", "", "", ErrInvalidPlayer},
		{ActionBan, "Steve", "bye\n/stop", "", ErrInvalidReason},
		{Action("stop"), "Steve", "", "", ErrUnknownAction},
	}
	for _, c := range cases {
		got, err := BuildCommand(c.action, c.player, c.reason)
		if err != c.wantErr || got != c.want {
			t.Errorf("BuildCommand(%q, %q, %q) = %q, %v; want %q, %v", c.action, c.player, c.reason, got, err, c.want, c.wantErr)
		}
	}
}

// Lines copied from a Hytale 0.7 server.
func TestTrackerFollowsRealLogLines(t *testing.T) {
	names := NewNameDirectory()
	tr := NewTracker(names)
	tr.Observe("[2026/10/12 23:31:56   INFO]              [Universe|P] Adding player 'Ocelus (" + aliceUUID + ")")
	tr.Observe("[2026/10/12 23:31:56   INFO]           [World|default] Adding player 'Ocelus' to world 'default' at location null (" + aliceUUID + ")")
	if online := tr.Online(); len(online) != 1 || online[0].Name != "Ocelus" || online[0].UUID != aliceUUID {
		t.Fatalf("unexpected online list %+v", online)
	}
	tr.Observe("[World|default] Removing player 'Ocelus' from world 'default' (" + aliceUUID + ")")
	if tr.Count() != 1 {
		t.Fatal("a world transfer is not a disconnection")
	}
	tr.Observe("[Universe|P] Removing player 'Ocelus' (" + aliceUUID + ")")
	if tr.Count() != 0 {
		t.Fatal("player should be gone")
	}
	if ref := names.Resolver(t.TempDir())(aliceUUID); ref.Name != "Ocelus" {
		t.Errorf("name not learned: %+v", ref)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixtureSave(t *testing.T) string {
	dir := t.TempDir()
	writeFile(t, dir, "permissions.json", `{"users":{"`+aliceUUID+`":{"groups":["hytale:Admin"],"permissions":["hytale.server.join"]},"`+bobUUID+`":{"groups":["hytale:Adventurer"],"permissions":["hytale.server.join"]}}}`)
	writeFile(t, dir, "bans.json", `{"Version":1,"Bans":[{"Target":"`+bobUUID+`","By":"`+aliceUUID+`","Timestamp":1760310000000,"Reason":"griefing"}]}`)
	writeFile(t, dir, "config.json", `{"Version":4,"MaxPlayers":100,"RequireJoinPermission":true}`)
	writeFile(t, dir, "universe/players/"+aliceUUID+".json", `{"Components":{"Nameplate":{"Text":"Ocelus"}}}`)
	return dir
}

func TestReadRoster(t *testing.T) {
	dir := fixtureSave(t)
	roster, err := ReadRoster(dir, NewNameDirectory().Resolver(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !roster.Whitelist.Enabled || len(roster.Whitelist.Players) != 2 {
		t.Errorf("whitelist: %+v", roster.Whitelist)
	}
	if len(roster.Operators) != 1 || roster.Operators[0].Name != "Ocelus" {
		t.Errorf("operators: %+v", roster.Operators)
	}
	if len(roster.Bans) != 1 {
		t.Fatalf("bans: %+v", roster.Bans)
	}
	ban := roster.Bans[0]
	if ban.Player.UUID != bobUUID || ban.Reason != "griefing" || ban.By == nil || ban.By.Name != "Ocelus" || ban.Timestamp == nil || ban.Timestamp.Year() != 2025 {
		t.Errorf("ban: %+v", ban)
	}
}

func TestOfflineEdits(t *testing.T) {
	dir := fixtureSave(t)
	if err := RemoveBan(dir, bobUUID); err != nil {
		t.Fatal(err)
	}
	if err := RemoveBan(dir, bobUUID); err != ErrNotInList {
		t.Errorf("second removal: %v", err)
	}
	if err := RemoveOperator(dir, aliceUUID); err != nil {
		t.Fatal(err)
	}
	if err := RemoveFromWhitelist(dir, bobUUID); err != nil {
		t.Fatal(err)
	}
	if err := SetWhitelistEnabled(dir, false); err != nil {
		t.Fatal(err)
	}
	roster, err := ReadRoster(dir, NewNameDirectory().Resolver(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(roster.Bans) != 0 || len(roster.Operators) != 0 || roster.Whitelist.Enabled || len(roster.Whitelist.Players) != 1 {
		t.Errorf("unexpected roster after edits: %+v", roster)
	}
	// Unrelated fields and exact numbers survive the rewrite.
	cfg, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	if !strings.Contains(string(cfg), `"MaxPlayers": 100`) || !strings.Contains(string(cfg), `"Version": 4`) {
		t.Errorf("config.json damaged: %s", cfg)
	}
}

func newTestRegistry(t *testing.T, path string) *Registry {
	t.Helper()
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewRegistry(db)
}

func mustPlayer(t *testing.T, r *Registry, uuid string) RegisteredPlayer {
	t.Helper()
	p, ok, err := r.Get(uuid)
	if err != nil || !ok {
		t.Fatalf("player %s: found %v, %v", uuid, ok, err)
	}
	return p
}

func TestRegistryTracksPlayers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manager.db")
	reg := newTestRegistry(t, path)
	names := NewNameDirectory().WithRegistry(reg)
	tr := NewTracker(names).WithRegistry(reg, func() string { return "world1" })
	tr.Observe("[Universe|P] Adding player 'Ocelus (" + aliceUUID + ")")
	tr.Observe("[Universe|P] Removing player 'Ocelus' (" + aliceUUID + ")")
	tr.Observe("[Universe|P] Adding player 'Ocelus2 (" + aliceUUID + ")")
	tr.Reset() // server stop closes the open session

	reopened := newTestRegistry(t, path)
	p := mustPlayer(t, reopened, aliceUUID)
	if p.LastKnownName != "Ocelus2" || len(p.PreviousNames) != 1 || p.PreviousNames[0] != "Ocelus" || len(p.Names) != 2 {
		t.Errorf("names not tracked: %+v", p)
	}
	if p.ConnectionCount != 2 || p.FirstSeen.IsZero() || p.LastSeen.Before(p.FirstSeen) {
		t.Errorf("activity not tracked: %+v", p)
	}
	if len(p.Sessions) != 2 || p.Sessions[0].Name != "Ocelus2" || p.Sessions[0].SaveID != "world1" || p.Sessions[0].LeftAt.IsZero() || p.Sessions[1].Interrupted {
		t.Errorf("sessions not tracked: %+v", p.Sessions)
	}
	// A fresh directory resolves the name from the registry alone.
	if ref := NewNameDirectory().WithRegistry(reopened).Resolver(t.TempDir())(aliceUUID); ref.Name != "Ocelus2" {
		t.Errorf("registry fallback failed: %+v", ref)
	}
	if list, err := reopened.List(); err != nil || len(list) != 1 || list[0].Name != "Ocelus2" || list[0].ConnectionCount != 2 {
		t.Errorf("List = %+v, %v", list, err)
	}
	if got := reopened.UUIDByName("ocelus2"); got != aliceUUID {
		t.Errorf("UUIDByName = %q", got)
	}
}

func TestRegistryMarksInterruptedSessions(t *testing.T) {
	reg := newTestRegistry(t, filepath.Join(t.TempDir(), "manager.db"))
	t0 := time.Date(2026, 10, 22, 14, 0, 0, 0, time.UTC)
	reg.Joined(aliceUUID, "Ocelus", "w", t0)
	if err := reg.CloseInterrupted(); err != nil { // the manager restarted
		t.Fatal(err)
	}
	reg.Joined(aliceUUID, "Ocelus", "w", t0.Add(time.Hour))
	reg.Left(aliceUUID, t0.Add(2*time.Hour), time.Hour)
	s := mustPlayer(t, reg, aliceUUID).Sessions
	if len(s) != 2 || !s[1].Interrupted || !s[1].LeftAt.IsZero() || s[0].Interrupted || !s[0].LeftAt.Equal(t0.Add(2*time.Hour)) {
		t.Fatalf("sessions %+v", s)
	}
}

func TestRegistryImportsLegacyJSON(t *testing.T) {
	reg := newTestRegistry(t, filepath.Join(t.TempDir(), "manager.db"))
	path := filepath.Join(t.TempDir(), "registered_players.json")
	os.WriteFile(path, []byte(`{"`+aliceUUID+`":{"lastKnownName":"Ocelus","previousNames":["Old"],"firstSeen":"2026-10-14T22:35:16Z",
		"lastSeen":"2026-10-22T13:59:00Z","connectionCount":7,"playtimeSeconds":2029,
		"banHistory":[{"reason":"x","bannedAt":"2026-10-15T10:00:00Z","unbannedAt":"2026-10-16T10:00:00Z"}]}}`), 0o644)

	n, err := reg.ImportJSON(path, "w1")
	if err != nil || n != 1 {
		t.Fatalf("imported %d, %v", n, err)
	}
	p := mustPlayer(t, reg, aliceUUID)
	if p.ConnectionCount != 7 || p.PlaytimeSeconds != 2029 || len(p.PreviousNames) != 1 || p.PreviousNames[0] != "Old" ||
		len(p.BanHistory) != 1 || p.BanHistory[0].SaveID != "w1" || p.BanHistory[0].UnbannedAt.IsZero() {
		t.Fatalf("imported player %+v", p)
	}
	if _, err := os.Stat(path + ".migrated"); err != nil {
		t.Error(err)
	}
}
