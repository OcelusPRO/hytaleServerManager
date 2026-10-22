package players

import (
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"

	"hytale-server-manager/internal/database"
)

// The whitelist, the bans and the operators are kept by the manager and
// shared by every world.
//
// The server keeps its own copy in the files of each world (bans.json,
// permissions.json, config.json) and must be the one writing them: its
// formats are not documented. So the manager:
//
//   - takes the changes found in the files of the world in use (made from
//     the game console, or by the server applying a panel command) into the
//     shared lists, by comparing them with what it saw there last time;
//   - when a world boots, sends the console commands that bring it in line
//     with the shared lists (Plan);
//   - while the server is stopped, records additions in the shared lists
//     only (applied at the next boot) and applies removals to the files
//     directly, which only means deleting entries.

// Keys of access_flags.
const (
	flagInitialized      = "initialized"
	flagWhitelistEnabled = "whitelist_enabled"
	sharedScope          = ""
)

// SharedEntry is an entry of a shared list.
type SharedEntry struct {
	Player    PlayerRef
	Reason    string
	AddedAt   time.Time
	ExpiresOn time.Time
}

// Shared is the content of the shared lists.
type Shared struct {
	WhitelistEnabled bool
	Lists            map[List][]SharedEntry
}

// Change is a console command that brings a world in line with the shared
// lists.
type Change struct {
	Action Action
	Player PlayerRef
	Reason string
}

// SharedLists stores the shared lists in the manager database.
type SharedLists struct {
	db *sql.DB
}

// NewSharedLists uses the access tables of db.
func NewSharedLists(db *sql.DB) *SharedLists {
	return &SharedLists{db: db}
}

func (l *SharedLists) inTx(fn func(*sql.Tx) error) error {
	return (&Registry{db: l.db}).inTx(fn)
}

// sharedListActions maps each list to its add and remove actions.
var sharedListActions = map[List][2]Action{
	ListBans:      {ActionBan, ActionUnban},
	ListOperators: {ActionOp, ActionDeop},
	ListWhitelist: {ActionWhitelistAdd, ActionWhitelistRemove},
}

var sharedListOrder = []List{ListBans, ListOperators, ListWhitelist}

// listFor returns the list an action changes and whether it adds to it.
func listFor(action Action) (List, bool, bool) {
	for list, actions := range sharedListActions {
		switch action {
		case actions[0]:
			return list, true, true
		case actions[1]:
			return list, false, true
		}
	}
	return "", false, false
}

func getFlag(q interface {
	QueryRow(string, ...any) *sql.Row
}, scope, key string) (string, bool, error) {
	var v string
	err := q.QueryRow(`SELECT value FROM access_flags WHERE scope = ? AND key = ?`, scope, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

func setFlag(tx *sql.Tx, scope, key, value string) error {
	_, err := tx.Exec(`INSERT INTO access_flags (scope, key, value) VALUES (?, ?, ?)
ON CONFLICT (scope, key) DO UPDATE SET value = excluded.value`, scope, key, value)
	return err
}

func boolFlag(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// Get returns the shared lists, each sorted by name.
func (l *SharedLists) Get() (Shared, error) {
	out := Shared{Lists: map[List][]SharedEntry{}}
	for _, list := range sharedListOrder {
		out.Lists[list] = []SharedEntry{}
	}
	v, _, err := getFlag(l.db, sharedScope, flagWhitelistEnabled)
	if err != nil {
		return out, err
	}
	out.WhitelistEnabled = v == "1"
	rows, err := l.db.Query(`SELECT list, uuid, name, reason, added_at, expires_on FROM access_entries ORDER BY name COLLATE NOCASE, uuid`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var list List
		var e SharedEntry
		var added int64
		var expires sql.NullInt64
		if err := rows.Scan(&list, &e.Player.UUID, &e.Player.Name, &e.Reason, &added, &expires); err != nil {
			return out, err
		}
		e.AddedAt, e.ExpiresOn = database.Time(added), database.NullTime(expires)
		out.Lists[list] = append(out.Lists[list], e)
	}
	return out, rows.Err()
}

// rosterEntries returns the members of each list in a world's files.
func rosterEntries(roster Roster) map[List]map[string]SharedEntry {
	out := map[List]map[string]SharedEntry{ListBans: {}, ListOperators: {}, ListWhitelist: {}}
	add := func(list List, e SharedEntry) {
		if ValidUUID(e.Player.UUID) {
			e.Player.UUID = strings.ToLower(e.Player.UUID)
			out[list][e.Player.UUID] = e
		}
	}
	for _, b := range roster.Bans {
		e := SharedEntry{Player: b.Player, Reason: b.Reason}
		if b.Timestamp != nil {
			e.AddedAt = *b.Timestamp
		}
		if b.ExpiresOn != nil {
			e.ExpiresOn = *b.ExpiresOn
		}
		add(ListBans, e)
	}
	for _, p := range roster.Operators {
		add(ListOperators, SharedEntry{Player: p})
	}
	for _, p := range roster.Whitelist.Players {
		add(ListWhitelist, SharedEntry{Player: p})
	}
	return out
}

// Sync takes the changes found in the files of a world since the last
// call into the shared lists. The first call ever fills the shared lists
// from that world; the first call for a world only takes note of its
// content. Expired bans are removed.
func (l *SharedLists) Sync(saveID string, roster Roster, now time.Time) error {
	files := rosterEntries(roster)
	return l.inTx(func(tx *sql.Tx) error {
		if err := expireBans(tx, now); err != nil {
			return err
		}
		_, initialized, err := getFlag(tx, sharedScope, flagInitialized)
		if err != nil {
			return err
		}
		if !initialized { // the shared lists start as this world's
			for _, list := range sharedListOrder {
				for _, e := range files[list] {
					if err := addShared(tx, list, e, saveID, SourceServer, now, true); err != nil {
						return err
					}
				}
			}
		}
		for _, list := range sharedListOrder {
			stored, known, err := storedAccess(tx, saveID, list)
			if err != nil {
				return err
			}
			for key, e := range files[list] {
				if stored[key] {
					continue
				}
				if known {
					if err := addShared(tx, list, e, saveID, SourceServer, now, false); err != nil {
						return err
					}
				}
				if _, err := tx.Exec(`INSERT INTO player_access (save_id, list, uuid) VALUES (?, ?, ?)`, saveID, list, key); err != nil {
					return err
				}
			}
			for key := range stored {
				if _, ok := files[list][key]; ok {
					continue
				}
				if known {
					if err := removeShared(tx, list, key, saveID, SourceServer, now); err != nil {
						return err
					}
				}
				if _, err := tx.Exec(`DELETE FROM player_access WHERE save_id = ? AND list = ? AND uuid = ?`, saveID, list, key); err != nil {
					return err
				}
			}
			if !known {
				if _, err := tx.Exec(`INSERT INTO player_access (save_id, list, uuid) VALUES (?, ?, '')`, saveID, list); err != nil {
					return err
				}
			}
		}

		enabled := boolFlag(roster.Whitelist.Enabled)
		last, seen, err := getFlag(tx, saveID, flagWhitelistEnabled)
		if err != nil {
			return err
		}
		if !initialized || (seen && last != enabled) {
			if err := setFlag(tx, sharedScope, flagWhitelistEnabled, enabled); err != nil {
				return err
			}
		}
		if err := setFlag(tx, saveID, flagWhitelistEnabled, enabled); err != nil {
			return err
		}
		if !initialized {
			return setFlag(tx, sharedScope, flagInitialized, "1")
		}
		return nil
	})
}

// addShared adds e to a shared list unless it is there already. The action
// is logged (from a world, unless it echoes a recent panel action) except
// while seeding.
func addShared(tx *sql.Tx, list List, e SharedEntry, saveID, source string, now time.Time, seeding bool) error {
	key := strings.ToLower(e.Player.UUID)
	added := e.AddedAt
	if added.IsZero() {
		added = now
	}
	res, err := tx.Exec(`INSERT OR IGNORE INTO access_entries (list, uuid, name, reason, added_at, expires_on) VALUES (?, ?, ?, ?, ?, ?)`,
		list, key, e.Player.Name, e.Reason, database.Millis(added), database.NullMillis(e.ExpiresOn))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}
	if err := ensurePlayer(tx, key, e.Player.Name); err != nil {
		return err
	}
	if list == ListBans {
		if err := openBan(tx, key, saveID, e.Reason, added, e.ExpiresOn); err != nil {
			return err
		}
	}
	if seeding {
		return nil
	}
	return logAction(tx, ActionRecord{UUID: key, Name: e.Player.Name, Action: sharedListActions[list][0], Reason: e.Reason, SaveID: saveID, Source: source, At: now})
}

// removeShared removes uuid from a shared list if it is there.
func removeShared(tx *sql.Tx, list List, key, saveID, source string, now time.Time) error {
	res, err := tx.Exec(`DELETE FROM access_entries WHERE list = ? AND uuid = ?`, list, key)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}
	if list == ListBans {
		if err := closeBan(tx, key, now); err != nil {
			return err
		}
	}
	return logAction(tx, ActionRecord{UUID: key, Action: sharedListActions[list][1], SaveID: saveID, Source: source, At: now})
}

// logAction records an action, except a change found in a world's files
// that is the effect of a recent panel action.
func logAction(tx *sql.Tx, a ActionRecord) error {
	if a.Source == SourceServer {
		var echoed bool
		if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM player_actions WHERE uuid = ? AND action = ? AND source = ? AND at >= ?)`,
			a.UUID, a.Action, SourcePanel, database.Millis(a.At.Add(-panelEcho))).Scan(&echoed); err != nil || echoed {
			return err
		}
	}
	return insertAction(tx, a)
}

func openBan(tx *sql.Tx, key, saveID, reason string, at, expires time.Time) error {
	var open bool
	if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM player_bans WHERE uuid = ? AND unbanned_at IS NULL)`, key).Scan(&open); err != nil || open {
		return err
	}
	_, err := tx.Exec(`INSERT INTO player_bans (uuid, save_id, reason, banned_at, expires_on) VALUES (?, ?, ?, ?, ?)`,
		key, saveID, reason, database.Millis(at), database.NullMillis(expires))
	return err
}

func closeBan(tx *sql.Tx, key string, at time.Time) error {
	_, err := tx.Exec(`UPDATE player_bans SET unbanned_at = ? WHERE uuid = ? AND unbanned_at IS NULL`, database.Millis(at), key)
	return err
}

// expireBans removes the temporary bans that ended.
func expireBans(tx *sql.Tx, now time.Time) error {
	rows, err := tx.Query(`SELECT uuid, expires_on FROM access_entries WHERE list = ? AND expires_on IS NOT NULL AND expires_on <= ?`,
		ListBans, database.Millis(now))
	if err != nil {
		return err
	}
	type ended struct {
		key string
		at  int64
	}
	var expired []ended
	for rows.Next() {
		var e ended
		if err := rows.Scan(&e.key, &e.at); err != nil {
			rows.Close()
			return err
		}
		expired = append(expired, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, e := range expired {
		if _, err := tx.Exec(`DELETE FROM access_entries WHERE list = ? AND uuid = ?`, ListBans, e.key); err != nil {
			return err
		}
		if err := closeBan(tx, e.key, database.Time(e.at)); err != nil {
			return err
		}
	}
	return nil
}

// Add puts a player in a shared list from the panel while the server is
// stopped: the world receives it at its next boot.
func (l *SharedLists) Add(list List, player PlayerRef, reason, saveID string, now time.Time) error {
	if !ValidUUID(player.UUID) {
		return ErrInvalidUUID
	}
	return l.inTx(func(tx *sql.Tx) error {
		return addShared(tx, list, SharedEntry{Player: player, Reason: reason, AddedAt: now}, saveID, SourcePanel, now, false)
	})
}

// Remove takes a player out of a shared list from the panel while the
// server is stopped; the caller removes it from the world's files.
func (l *SharedLists) Remove(list List, uuid, saveID string, now time.Time) error {
	if !ValidUUID(uuid) {
		return ErrInvalidUUID
	}
	key := strings.ToLower(uuid)
	return l.inTx(func(tx *sql.Tx) error {
		if err := removeShared(tx, list, key, saveID, SourcePanel, now); err != nil {
			return err
		}
		// The world's files no longer hold it either.
		_, err := tx.Exec(`DELETE FROM player_access WHERE save_id = ? AND list = ? AND uuid = ?`, saveID, list, key)
		return err
	})
}

// SetWhitelistEnabled changes the shared whitelist switch from the panel
// while the server is stopped; the caller changes the world's config.
func (l *SharedLists) SetWhitelistEnabled(enabled bool, saveID string) error {
	return l.inTx(func(tx *sql.Tx) error {
		if err := setFlag(tx, sharedScope, flagWhitelistEnabled, boolFlag(enabled)); err != nil {
			return err
		}
		return setFlag(tx, saveID, flagWhitelistEnabled, boolFlag(enabled))
	})
}

// Plan returns the console commands that bring a world's files in line
// with the shared lists, and whether its whitelist switch must change.
// Temporary bans are left out: the console only bans permanently.
func (l *SharedLists) Plan(roster Roster, now time.Time) (changes []Change, whitelist *bool, err error) {
	shared, err := l.Get()
	if err != nil {
		return nil, nil, err
	}
	files := rosterEntries(roster)
	for _, list := range sharedListOrder {
		want := map[string]bool{}
		for _, e := range shared.Lists[list] {
			key := strings.ToLower(e.Player.UUID)
			want[key] = true
			if _, ok := files[list][key]; ok {
				continue
			}
			if !e.ExpiresOn.IsZero() {
				continue
			}
			changes = append(changes, Change{Action: sharedListActions[list][0], Player: e.Player, Reason: e.Reason})
		}
		var extra []SharedEntry
		for key, e := range files[list] {
			if !want[key] && (e.ExpiresOn.IsZero() || e.ExpiresOn.After(now)) {
				extra = append(extra, e)
			}
		}
		slices.SortFunc(extra, func(a, b SharedEntry) int { return strings.Compare(a.Player.UUID, b.Player.UUID) })
		for _, e := range extra {
			changes = append(changes, Change{Action: sharedListActions[list][1], Player: e.Player})
		}
	}
	if roster.Whitelist.Enabled != shared.WhitelistEnabled {
		whitelist = &shared.WhitelistEnabled
	}
	return changes, whitelist, nil
}

// pendingEntries returns, for each list, the players of the shared lists
// missing from a world's files: they are applied at its next boot.
func pendingEntries(shared Shared, roster Roster) map[List][]string {
	files := rosterEntries(roster)
	out := map[List][]string{}
	for _, list := range sharedListOrder {
		out[list] = []string{}
		for _, e := range shared.Lists[list] {
			if _, ok := files[list][strings.ToLower(e.Player.UUID)]; !ok {
				out[list] = append(out[list], e.Player.UUID)
			}
		}
	}
	return out
}
