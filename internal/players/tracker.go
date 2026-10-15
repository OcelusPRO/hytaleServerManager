package players

import (
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Universe log lines of the Hytale server announcing connections:
//
//	[Universe|P] Adding player 'Name (uuid)
//	[Universe|P] Removing player 'Name' (uuid)
//
// World transfers log "Removing player 'Name' from world", which is not a
// disconnection and is deliberately not matched.
var (
	joinPattern  = regexp.MustCompile(`Adding player '([A-Za-z0-9_]{1,32}) \(([0-9a-fA-F-]{36})\)`)
	leavePattern = regexp.MustCompile(`Removing player '([A-Za-z0-9_]{1,32})' \(([0-9a-fA-F-]{36})`)
)

// OnlinePlayer is a player currently connected.
type OnlinePlayer struct {
	UUID     string    `json:"uuid"`
	Name     string    `json:"name"`
	JoinedAt time.Time `json:"joinedAt"`
}

// Tracker derives the list of online players from console output and
// teaches the name directory the names it sees.
type Tracker struct {
	names    *NameDirectory
	registry *Registry // optional

	mu     sync.Mutex
	online map[string]OnlinePlayer // keyed by lower-case uuid
}

// NewTracker returns an empty tracker feeding names.
func NewTracker(names *NameDirectory) *Tracker {
	return &Tracker{names: names, online: make(map[string]OnlinePlayer)}
}

// WithRegistry makes the tracker record connections in r.
func (t *Tracker) WithRegistry(r *Registry) *Tracker {
	t.registry = r
	return t
}

// Observe updates presence from one console line.
func (t *Tracker) Observe(line string) {
	if m := leavePattern.FindStringSubmatch(line); m != nil {
		t.Leave(m[2])
		return
	}
	if m := joinPattern.FindStringSubmatch(line); m != nil {
		t.Join(m[2], m[1])
	}
}

// Join marks a player as connected. It is idempotent: presence is also
// reported by the MetricsStream plugin, and whichever source tells first
// starts the session.
func (t *Tracker) Join(uuid, name string) {
	key := strings.ToLower(uuid)
	t.names.Learn(uuid, name)
	now := time.Now().UTC()
	t.mu.Lock()
	if _, already := t.online[key]; already {
		t.mu.Unlock()
		return
	}
	t.online[key] = OnlinePlayer{UUID: uuid, Name: name, JoinedAt: now}
	t.mu.Unlock()
	if t.registry != nil {
		_ = t.registry.Joined(uuid, name, now) // best effort: presence must not depend on disk
	}
}

// Leave marks a player as disconnected (idempotent).
func (t *Tracker) Leave(uuid string) {
	key := strings.ToLower(uuid)
	t.mu.Lock()
	p, was := t.online[key]
	delete(t.online, key)
	t.mu.Unlock()
	if was {
		t.recordLeave(p)
	}
}

// Reset clears presence information (server stopped), closing the sessions
// of players still connected.
func (t *Tracker) Reset() {
	t.mu.Lock()
	left := make([]OnlinePlayer, 0, len(t.online))
	for _, p := range t.online {
		left = append(left, p)
	}
	clear(t.online)
	t.mu.Unlock()
	for _, p := range left {
		t.recordLeave(p)
	}
}

func (t *Tracker) recordLeave(p OnlinePlayer) {
	if t.registry == nil {
		return
	}
	now := time.Now().UTC()
	_ = t.registry.Left(p.UUID, now, now.Sub(p.JoinedAt))
}

// Online lists the players currently connected.
func (t *Tracker) Online() []OnlinePlayer {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]OnlinePlayer, 0, len(t.online))
	for _, p := range t.online {
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b OnlinePlayer) int { return a.JoinedAt.Compare(b.JoinedAt) })
	return out
}

// Count returns the number of connected players.
func (t *Tracker) Count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.online)
}
