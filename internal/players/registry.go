package players

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const maxPreviousNames = 20

// RegisteredPlayer is what the manager remembers about a player, keyed by
// UUID in the registry file.
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
}

// BanRecord is one ban of a player. UnbannedAt is the removal date as
// detected by the manager (or the expiry date of a temporary ban).
type BanRecord struct {
	Reason     string    `json:"reason,omitempty"`
	BannedAt   time.Time `json:"bannedAt"`
	ExpiresOn  time.Time `json:"expiresOn,omitzero"`
	UnbannedAt time.Time `json:"unbannedAt,omitzero"`
}

// Registry persists RegisteredPlayer entries in a JSON file so names and
// activity survive restarts (the game only stores names in player files).
type Registry struct {
	path string

	mu      sync.Mutex
	players map[string]RegisteredPlayer // lower-case uuid
}

// OpenRegistry loads the registry at path; a missing file starts it empty.
func OpenRegistry(path string) (*Registry, error) {
	r := &Registry{path: path, players: make(map[string]RegisteredPlayer)}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &r.players); err != nil {
		return nil, fmt.Errorf("%s is unreadable", filepath.Base(path))
	}
	if r.players == nil {
		r.players = make(map[string]RegisteredPlayer)
	}
	return r, nil
}

// Joined records a connection of uuid under name at the given instant.
func (r *Registry) Joined(uuid, name string, at time.Time) error {
	return r.update(uuid, func(p *RegisteredPlayer) {
		if p.FirstSeen.IsZero() {
			p.FirstSeen = at
		}
		if p.LastKnownName != "" && p.LastKnownName != name && !slices.Contains(p.PreviousNames, p.LastKnownName) {
			p.PreviousNames = append(p.PreviousNames, p.LastKnownName)
			if len(p.PreviousNames) > maxPreviousNames {
				p.PreviousNames = p.PreviousNames[len(p.PreviousNames)-maxPreviousNames:]
			}
		}
		p.LastKnownName = name
		p.LastSeen = at
		p.ConnectionCount++
	})
}

// Left records a disconnection; session is how long the player stayed.
func (r *Registry) Left(uuid string, at time.Time, session time.Duration) error {
	return r.update(uuid, func(p *RegisteredPlayer) {
		p.LastSeen = at
		if session > 0 {
			p.PlaytimeSeconds += int64(session.Seconds())
		}
	})
}

// SyncBans reconciles the ban history with the bans currently in bans.json:
// new bans open a record, bans that disappeared (or expired) close theirs.
// Bans made through the in-game console are caught the same way.
func (r *Registry) SyncBans(current []Ban, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	changed := false
	active := make(map[string]bool, len(current))

	for _, ban := range current {
		if !ValidUUID(ban.Player.UUID) {
			continue
		}
		key := strings.ToLower(ban.Player.UUID)
		active[key] = true
		p := r.players[key]
		if p.LastKnownName == "" && ban.Player.Name != "" {
			p.LastKnownName = ban.Player.Name
			changed = true
		}
		bannedAt := now
		if ban.Timestamp != nil {
			bannedAt = *ban.Timestamp
		}
		var expires time.Time
		if ban.ExpiresOn != nil {
			expires = *ban.ExpiresOn
		}
		if n := len(p.BanHistory); n > 0 && p.BanHistory[n-1].UnbannedAt.IsZero() {
			open := &p.BanHistory[n-1]
			if open.BannedAt.UnixMilli() == bannedAt.UnixMilli() || ban.Timestamp == nil {
				// Same ban: only a temporary ban's expiry can have ended it.
				if !expires.IsZero() && !expires.After(now) {
					open.UnbannedAt, open.ExpiresOn = expires, expires
					changed = true
				}
				r.players[key] = p
				continue
			}
			open.UnbannedAt = bannedAt // replaced by a newer ban
		}
		record := BanRecord{Reason: ban.Reason, BannedAt: bannedAt, ExpiresOn: expires}
		if !expires.IsZero() && !expires.After(now) {
			record.UnbannedAt = expires
		}
		p.BanHistory = append(p.BanHistory, record)
		r.players[key] = p
		changed = true
	}

	for key, p := range r.players {
		if n := len(p.BanHistory); n > 0 && p.BanHistory[n-1].UnbannedAt.IsZero() && !active[key] {
			p.BanHistory[n-1].UnbannedAt = now
			r.players[key] = p
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return r.saveLocked()
}

// Name returns the last known name of uuid, or "".
func (r *Registry) Name(uuid string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.players[strings.ToLower(uuid)].LastKnownName
}

// All returns a copy of every registered player keyed by lower-case uuid.
func (r *Registry) All() map[string]RegisteredPlayer {
	r.mu.Lock()
	defer r.mu.Unlock()
	return maps.Clone(r.players)
}

func (r *Registry) update(uuid string, mutate func(*RegisteredPlayer)) error {
	if !ValidUUID(uuid) {
		return ErrInvalidUUID
	}
	key := strings.ToLower(uuid)
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.players[key]
	mutate(&p)
	r.players[key] = p
	return r.saveLocked()
}

// saveLocked writes the file atomically (temp file + rename).
func (r *Registry) saveLocked() error {
	data, err := json.MarshalIndent(r.players, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}
