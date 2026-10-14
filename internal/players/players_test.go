package players

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestRegistryTracksPlayers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registered_players.json")
	reg, err := OpenRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	names := NewNameDirectory().WithRegistry(reg)
	tr := NewTracker(names).WithRegistry(reg)
	tr.Observe("[Universe|P] Adding player 'Ocelus (" + aliceUUID + ")")
	tr.Observe("[Universe|P] Removing player 'Ocelus' (" + aliceUUID + ")")
	tr.Observe("[Universe|P] Adding player 'Ocelus2 (" + aliceUUID + ")")
	tr.Reset() // server stop closes the open session

	reopened, err := OpenRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	p := reopened.All()[aliceUUID]
	if p.LastKnownName != "Ocelus2" || len(p.PreviousNames) != 1 || p.PreviousNames[0] != "Ocelus" {
		t.Errorf("names not tracked: %+v", p)
	}
	if p.ConnectionCount != 2 || p.FirstSeen.IsZero() || p.LastSeen.Before(p.FirstSeen) {
		t.Errorf("activity not tracked: %+v", p)
	}
	// A fresh directory resolves the name from the registry alone.
	if ref := NewNameDirectory().WithRegistry(reopened).Resolver(t.TempDir())(aliceUUID); ref.Name != "Ocelus2" {
		t.Errorf("registry fallback failed: %+v", ref)
	}
}

func TestRegistrySyncBansHistory(t *testing.T) {
	reg, err := OpenRegistry(filepath.Join(t.TempDir(), "registered_players.json"))
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	ban := Ban{Player: PlayerRef{UUID: bobUUID, Name: "Bob"}, Reason: "griefing", Timestamp: &t0}
	for i := 0; i < 2; i++ { // idempotent
		if err := reg.SyncBans([]Ban{ban}, t0.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	h := reg.All()[bobUUID].BanHistory
	if len(h) != 1 || h[0].Reason != "griefing" || !h[0].UnbannedAt.IsZero() {
		t.Fatalf("ban not recorded: %+v", h)
	}
	unban := t0.Add(48 * time.Hour)
	_ = reg.SyncBans(nil, unban)
	t1 := t0.Add(72 * time.Hour)
	ban2 := Ban{Player: ban.Player, Reason: "again", Timestamp: &t1}
	_ = reg.SyncBans([]Ban{ban2}, t1)
	h = reg.All()[bobUUID].BanHistory
	if len(h) != 2 || !h[0].UnbannedAt.Equal(unban) || h[1].Reason != "again" || !h[1].UnbannedAt.IsZero() {
		t.Fatalf("history wrong: %+v", h)
	}
}
