package metricsstream

import (
	"sync"
	"time"

	"hytale-server-manager/internal/hmsp"
)

const (
	// maxEvents bounds the gameplay events kept in memory.
	maxEvents = 1000
	// staleAfter drops a world or player that stopped being reported.
	staleAfter = 30 * time.Second
)

// Event is a gameplay event with a sequence number, so that the interface
// can ask only for what is new.
type Event struct {
	ID uint64 `json:"id"`
	hmsp.Record
}

type timedRecord struct {
	data map[string]any
	seen time.Time
}

// DumpInfo describes the last RAW_DUMP received.
type DumpInfo struct {
	TimeMs int64 `json:"timeMs"`
}

// Live is the latest state reported by the plugin.
type Live struct {
	Hello   map[string]any   `json:"hello,omitempty"`
	Server  map[string]any   `json:"server,omitempty"`
	JVM     map[string]any   `json:"jvm,omitempty"`
	Plugins []any            `json:"plugins,omitempty"`
	Worlds  []map[string]any `json:"worlds"`
	Players []map[string]any `json:"players"`
	Dump    *DumpInfo        `json:"dump,omitempty"`
}

// Store keeps the latest snapshots and a ring of recent events.
type Store struct {
	mu sync.RWMutex

	hello, server, jvm map[string]any
	plugins            []any
	worlds             map[string]timedRecord
	players            map[string]timedRecord
	dump               map[string]any
	dumpTimeMs         int64

	events []Event
	nextID uint64

	records  uint64
	lastSeen time.Time
	now      func() time.Time
}

// NewStore returns an empty store.
func NewStore() *Store {
	s := &Store{now: time.Now}
	s.reset()
	return s
}

func (s *Store) reset() {
	s.hello, s.server, s.jvm, s.plugins, s.dump = nil, nil, nil, nil, nil
	s.dumpTimeMs = 0
	s.worlds = map[string]timedRecord{}
	s.players = map[string]timedRecord{}
	s.events = nil
	s.nextID = 1
	s.records = 0
	s.lastSeen = time.Time{}
}

// Reset forgets everything (a new server process starts a new stream).
func (s *Store) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reset()
}

// Add records the content of a decoded message.
func (s *Store) Add(records []hmsp.Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.lastSeen = now
	for _, r := range records {
		s.records++
		switch r.Kind {
		case hmsp.KindSnapshot:
			s.addSnapshot(r, now)
		case hmsp.KindEvent:
			s.events = append(s.events, Event{ID: s.nextID, Record: r})
			s.nextID++
			if len(s.events) > maxEvents {
				s.events = append(s.events[:0], s.events[len(s.events)-maxEvents:]...)
			}
		}
	}
}

func (s *Store) addSnapshot(r hmsp.Record, now time.Time) {
	switch r.Type {
	case "HELLO":
		s.hello = r.Data
	case "SERVER":
		s.server = r.Data
	case "JVM":
		s.jvm = r.Data
	case "PLUGINS":
		s.plugins, _ = r.Data["plugins"].([]any)
	case "WORLD":
		if name, _ := r.Data["name"].(string); name != "" {
			s.worlds[name] = timedRecord{r.Data, now}
		}
	case "PLAYER":
		if id, _ := r.Data["uuid"].(string); id != "" {
			s.players[id] = timedRecord{r.Data, now}
		}
	case "RAW_DUMP":
		if doc, ok := r.Data["dump"].(map[string]any); ok {
			s.dump, s.dumpTimeMs = doc, r.TimeMs
		}
	}
}

// Live returns the current state, leaving out worlds and players that are no
// longer reported.
func (s *Store) Live() Live {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	live := Live{Hello: s.hello, Server: s.server, JVM: s.jvm, Plugins: s.plugins,
		Worlds: []map[string]any{}, Players: []map[string]any{}}
	for k, v := range s.worlds {
		if now.Sub(v.seen) > staleAfter {
			delete(s.worlds, k)
			continue
		}
		live.Worlds = append(live.Worlds, v.data)
	}
	for k, v := range s.players {
		if now.Sub(v.seen) > staleAfter {
			delete(s.players, k)
			continue
		}
		live.Players = append(live.Players, v.data)
	}
	sortByName(live.Worlds, "name")
	sortByName(live.Players, "name")
	if s.dump != nil {
		live.Dump = &DumpInfo{TimeMs: s.dumpTimeMs}
	}
	return live
}

// Dump returns the last RAW_DUMP document.
func (s *Store) Dump() (doc map[string]any, timeMs int64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dump, s.dumpTimeMs
}

// EventsAfter returns up to limit events newer than id, oldest first, and
// the id to ask for next.
func (s *Store) EventsAfter(id uint64, limit int) (events []Event, next uint64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	next = id
	for _, e := range s.events {
		if e.ID <= id {
			continue
		}
		events = append(events, e)
		next = e.ID
		if limit > 0 && len(events) >= limit {
			break
		}
	}
	if events == nil {
		events = []Event{}
	}
	return events, next
}

// Activity reports how many records were stored and when the last one came.
func (s *Store) Activity() (records uint64, last time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.records, s.lastSeen
}
