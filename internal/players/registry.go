package players

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"hytale-server-manager/internal/database"
)

const (
	// historyLimit bounds the sessions and actions returned for a player.
	historyLimit = 200
	// panelEcho is how long after a panel action the same change found in
	// the world's files is taken as its effect rather than a new action.
	panelEcho = 5 * time.Minute
)

// Origins of a moderation action.
const (
	SourcePanel  = "panel"  // made from this interface
	SourceServer = "server" // found in the world's files (game console)
)

// RegisteredPlayer is what the manager remembers about a player.
type RegisteredPlayer struct {
	LastKnownName   string    `json:"lastKnownName"`
	PreviousNames   []string  `json:"previousNames,omitempty"`
	FirstSeen       time.Time `json:"firstSeen,omitzero"`
	LastSeen        time.Time `json:"lastSeen,omitzero"`
	ConnectionCount int       `json:"connectionCount"`
	// PlaytimeSeconds only sums sessions the manager watched end.
	PlaytimeSeconds int64 `json:"playtimeSeconds"`
	// BanHistory lists every ban, oldest first; the last one is current
	// while its UnbannedAt is unset.
	BanHistory []BanRecord `json:"banHistory,omitempty"`
	// Names are the names the player was seen with, oldest first.
	Names []NameRecord `json:"names,omitempty"`
	// Sessions are the latest connections, newest first.
	Sessions []SessionRecord `json:"sessions,omitempty"`
	// Actions are the latest moderation actions, newest first.
	Actions []ActionRecord `json:"actions,omitempty"`
}

// BanRecord is one ban of a player. UnbannedAt is the removal date as
// detected by the manager (or the expiry date of a temporary ban).
type BanRecord struct {
	Reason     string    `json:"reason,omitempty"`
	SaveID     string    `json:"saveId,omitempty"`
	BannedAt   time.Time `json:"bannedAt"`
	ExpiresOn  time.Time `json:"expiresOn,omitzero"`
	UnbannedAt time.Time `json:"unbannedAt,omitzero"`
}

// NameRecord is a name a player used. Dates are unknown for names
// recorded before the history existed.
type NameRecord struct {
	Name      string    `json:"name"`
	FirstSeen time.Time `json:"firstSeen,omitzero"`
	LastSeen  time.Time `json:"lastSeen,omitzero"`
}

// SessionRecord is one connection to the server.
type SessionRecord struct {
	Name     string    `json:"name"`
	SaveID   string    `json:"saveId,omitempty"`
	JoinedAt time.Time `json:"joinedAt"`
	LeftAt   time.Time `json:"leftAt,omitzero"`
	// Interrupted sessions were open when the manager stopped: their end is
	// unknown.
	Interrupted bool `json:"interrupted,omitempty"`
}

// ActionRecord is a moderation action targeting a player.
type ActionRecord struct {
	UUID   string    `json:"uuid,omitempty"`
	Name   string    `json:"name,omitempty"`
	Action Action    `json:"action"`
	Reason string    `json:"reason,omitempty"`
	SaveID string    `json:"saveId,omitempty"`
	Source string    `json:"source"`
	At     time.Time `json:"at"`
}

// Registry keeps the players and their history in the manager database,
// so names and activity survive restarts (the game only stores names in
// player files).
type Registry struct {
	db *sql.DB
}

// NewRegistry uses the player tables of db.
func NewRegistry(db *sql.DB) *Registry {
	return &Registry{db: db}
}

func (r *Registry) inTx(fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// CloseInterrupted marks the sessions still open, left by a previous run of
// the manager, as interrupted. Call it at startup.
func (r *Registry) CloseInterrupted() error {
	_, err := r.db.Exec(`UPDATE player_sessions SET interrupted = 1 WHERE left_at IS NULL AND interrupted = 0`)
	return err
}

// ensurePlayer creates the row of key, learning its name when unknown.
func ensurePlayer(tx *sql.Tx, key, name string) error {
	_, err := tx.Exec(`
INSERT INTO players (uuid, name) VALUES (?, ?)
ON CONFLICT (uuid) DO UPDATE SET name = CASE WHEN name = '' THEN excluded.name ELSE name END`, key, name)
	return err
}

// Joined records a connection of uuid under name to the save, at the given
// instant.
func (r *Registry) Joined(uuid, name, saveID string, at time.Time) error {
	if !ValidUUID(uuid) {
		return ErrInvalidUUID
	}
	key, ms := strings.ToLower(uuid), database.Millis(at)
	return r.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`
INSERT INTO players (uuid, name, first_seen, last_seen, connection_count) VALUES (?, ?, ?, ?, 1)
ON CONFLICT (uuid) DO UPDATE SET
	name = excluded.name,
	first_seen = COALESCE(first_seen, excluded.first_seen),
	last_seen = excluded.last_seen,
	connection_count = connection_count + 1`, key, name, ms, ms); err != nil {
			return err
		}
		if _, err := tx.Exec(`
INSERT INTO player_names (uuid, name, first_seen, last_seen) VALUES (?, ?, ?, ?)
ON CONFLICT (uuid, name) DO UPDATE SET
	first_seen = COALESCE(first_seen, excluded.first_seen),
	last_seen = excluded.last_seen`, key, name, ms, ms); err != nil {
			return err
		}
		// A session still open never saw its end.
		if _, err := tx.Exec(`UPDATE player_sessions SET interrupted = 1 WHERE uuid = ? AND left_at IS NULL`, key); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO player_sessions (uuid, name, save_id, joined_at) VALUES (?, ?, ?, ?)`, key, name, saveID, ms)
		return err
	})
}

// Left records a disconnection; session is how long the player stayed.
func (r *Registry) Left(uuid string, at time.Time, session time.Duration) error {
	if !ValidUUID(uuid) {
		return ErrInvalidUUID
	}
	key, ms := strings.ToLower(uuid), database.Millis(at)
	return r.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE players SET last_seen = ?, playtime_seconds = playtime_seconds + ? WHERE uuid = ?`,
			ms, max(0, int64(session.Seconds())), key); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE player_names SET last_seen = ? WHERE uuid = ? AND name = (SELECT name FROM players WHERE uuid = ?)`,
			ms, key, key); err != nil {
			return err
		}
		_, err := tx.Exec(`
UPDATE player_sessions SET left_at = ?, interrupted = 0
WHERE id = (SELECT id FROM player_sessions WHERE uuid = ? AND left_at IS NULL ORDER BY id DESC LIMIT 1)`, ms, key)
		return err
	})
}

// storedAccess returns the members of a list recorded by the last sync, and
// whether the list was synced before.
func storedAccess(tx *sql.Tx, saveID string, list List) (map[string]bool, bool, error) {
	rows, err := tx.Query(`SELECT uuid FROM player_access WHERE save_id = ? AND list = ?`, saveID, list)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	stored, known := map[string]bool{}, false
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, false, err
		}
		if key == "" {
			known = true
		} else {
			stored[key] = true
		}
	}
	return stored, known, rows.Err()
}

func insertAction(tx *sql.Tx, a ActionRecord) error {
	var key sql.NullString
	if a.UUID != "" {
		key = sql.NullString{String: strings.ToLower(a.UUID), Valid: true}
	}
	if a.Name == "" && key.Valid {
		_ = tx.QueryRow(`SELECT name FROM players WHERE uuid = ?`, key.String).Scan(&a.Name)
	}
	_, err := tx.Exec(`INSERT INTO player_actions (uuid, name, action, reason, save_id, source, at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		key, a.Name, a.Action, a.Reason, a.SaveID, a.Source, database.Millis(a.At))
	return err
}

// RecordAction logs a moderation action.
func (r *Registry) RecordAction(a ActionRecord) error {
	return r.inTx(func(tx *sql.Tx) error { return insertAction(tx, a) })
}

// UUIDByName returns the player last known under name (case-insensitive),
// or "".
func (r *Registry) UUIDByName(name string) string {
	var key string
	_ = r.db.QueryRow(`SELECT uuid FROM players WHERE name = ? COLLATE NOCASE ORDER BY last_seen DESC LIMIT 1`, name).Scan(&key)
	return key
}

// Name returns the last known name of uuid, or "".
func (r *Registry) Name(uuid string) string {
	var name string
	_ = r.db.QueryRow(`SELECT name FROM players WHERE uuid = ?`, strings.ToLower(uuid)).Scan(&name)
	return name
}

// KnownPlayer summarises a player the manager has seen, connected or not.
type KnownPlayer struct {
	UUID            string    `json:"uuid"`
	Name            string    `json:"name"`
	FirstSeen       time.Time `json:"firstSeen,omitzero"`
	LastSeen        time.Time `json:"lastSeen,omitzero"`
	ConnectionCount int       `json:"connectionCount"`
	PlaytimeSeconds int64     `json:"playtimeSeconds"`
}

// List returns every known player, the most recently seen first (players
// only known from a ban last).
func (r *Registry) List() ([]KnownPlayer, error) {
	rows, err := r.db.Query(`
SELECT uuid, name, first_seen, last_seen, connection_count, playtime_seconds FROM players
ORDER BY last_seen IS NULL, last_seen DESC, name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []KnownPlayer{}
	for rows.Next() {
		var p KnownPlayer
		var first, last sql.NullInt64
		if err := rows.Scan(&p.UUID, &p.Name, &first, &last, &p.ConnectionCount, &p.PlaytimeSeconds); err != nil {
			return nil, err
		}
		p.FirstSeen, p.LastSeen = database.NullTime(first), database.NullTime(last)
		list = append(list, p)
	}
	return list, rows.Err()
}

// Get returns everything stored about uuid.
func (r *Registry) Get(uuid string) (RegisteredPlayer, bool, error) {
	key := strings.ToLower(uuid)
	var p RegisteredPlayer
	var first, last sql.NullInt64
	err := r.db.QueryRow(`SELECT name, first_seen, last_seen, connection_count, playtime_seconds FROM players WHERE uuid = ?`, key).
		Scan(&p.LastKnownName, &first, &last, &p.ConnectionCount, &p.PlaytimeSeconds)
	if errors.Is(err, sql.ErrNoRows) {
		return RegisteredPlayer{}, false, nil
	}
	if err != nil {
		return RegisteredPlayer{}, false, err
	}
	p.FirstSeen, p.LastSeen = database.NullTime(first), database.NullTime(last)

	err = r.each(`SELECT name, first_seen, last_seen FROM player_names WHERE uuid = ? ORDER BY COALESCE(first_seen, 0), rowid`, key, func(rows *sql.Rows) error {
		var n NameRecord
		var f, l sql.NullInt64
		if err := rows.Scan(&n.Name, &f, &l); err != nil {
			return err
		}
		n.FirstSeen, n.LastSeen = database.NullTime(f), database.NullTime(l)
		p.Names = append(p.Names, n)
		if n.Name != p.LastKnownName {
			p.PreviousNames = append(p.PreviousNames, n.Name)
		}
		return nil
	})
	if err == nil {
		err = r.each(`SELECT reason, save_id, banned_at, expires_on, unbanned_at FROM player_bans WHERE uuid = ? ORDER BY banned_at, id`, key, func(rows *sql.Rows) error {
			var b BanRecord
			var at int64
			var expires, unbanned sql.NullInt64
			if err := rows.Scan(&b.Reason, &b.SaveID, &at, &expires, &unbanned); err != nil {
				return err
			}
			b.BannedAt, b.ExpiresOn, b.UnbannedAt = database.Time(at), database.NullTime(expires), database.NullTime(unbanned)
			p.BanHistory = append(p.BanHistory, b)
			return nil
		})
	}
	if err == nil {
		err = r.each(fmt.Sprintf(`SELECT name, save_id, joined_at, left_at, interrupted FROM player_sessions WHERE uuid = ? ORDER BY joined_at DESC, id DESC LIMIT %d`, historyLimit), key, func(rows *sql.Rows) error {
			var s SessionRecord
			var joined int64
			var left sql.NullInt64
			if err := rows.Scan(&s.Name, &s.SaveID, &joined, &left, &s.Interrupted); err != nil {
				return err
			}
			s.JoinedAt, s.LeftAt = database.Time(joined), database.NullTime(left)
			p.Sessions = append(p.Sessions, s)
			return nil
		})
	}
	if err == nil {
		err = r.each(fmt.Sprintf(`SELECT name, action, reason, save_id, source, at FROM player_actions WHERE uuid = ? ORDER BY at DESC, id DESC LIMIT %d`, historyLimit), key, func(rows *sql.Rows) error {
			a := ActionRecord{UUID: key}
			var at int64
			if err := rows.Scan(&a.Name, &a.Action, &a.Reason, &a.SaveID, &a.Source, &at); err != nil {
				return err
			}
			a.At = database.Time(at)
			p.Actions = append(p.Actions, a)
			return nil
		})
	}
	if err != nil {
		return RegisteredPlayer{}, false, err
	}
	return p, true, nil
}

func (r *Registry) each(query, key string, scan func(*sql.Rows) error) error {
	rows, err := r.db.Query(query, key)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// ImportJSON moves the players of registered_players.json, used before the
// database, into it, then renames the file to <path>.migrated. Their bans
// are attributed to saveID (the active world). A missing file is not an
// error.
func (r *Registry) ImportJSON(path, saveID string) (int, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var legacy map[string]RegisteredPlayer
	if err := json.Unmarshal(data, &legacy); err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	imported := 0
	err = r.inTx(func(tx *sql.Tx) error {
		for uuid, p := range legacy {
			if !ValidUUID(uuid) {
				continue
			}
			key := strings.ToLower(uuid)
			res, err := tx.Exec(`INSERT OR IGNORE INTO players (uuid, name, first_seen, last_seen, connection_count, playtime_seconds) VALUES (?, ?, ?, ?, ?, ?)`,
				key, p.LastKnownName, database.NullMillis(p.FirstSeen), database.NullMillis(p.LastSeen), p.ConnectionCount, p.PlaytimeSeconds)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				continue // already imported
			}
			for _, name := range p.PreviousNames { // dates unknown
				if _, err := tx.Exec(`INSERT OR IGNORE INTO player_names (uuid, name) VALUES (?, ?)`, key, name); err != nil {
					return err
				}
			}
			if p.LastKnownName != "" {
				if _, err := tx.Exec(`INSERT OR IGNORE INTO player_names (uuid, name, first_seen, last_seen) VALUES (?, ?, ?, ?)`,
					key, p.LastKnownName, database.NullMillis(p.FirstSeen), database.NullMillis(p.LastSeen)); err != nil {
					return err
				}
			}
			for _, b := range p.BanHistory {
				if _, err := tx.Exec(`INSERT INTO player_bans (uuid, save_id, reason, banned_at, expires_on, unbanned_at) VALUES (?, ?, ?, ?, ?, ?)`,
					key, saveID, b.Reason, database.Millis(b.BannedAt), database.NullMillis(b.ExpiresOn), database.NullMillis(b.UnbannedAt)); err != nil {
					return err
				}
			}
			imported++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return imported, os.Rename(path, path+".migrated")
}
