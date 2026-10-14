package players

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

const maxPlayerFileBytes = 4 << 20

// ValidUUID reports whether s is a canonical UUID.
func ValidUUID(s string) bool { return uuidPattern.MatchString(s) }

// NameDirectory resolves player UUIDs to names, using names seen in the
// console and, failing that, the player save files of the world.
type NameDirectory struct {
	mu    sync.RWMutex
	known map[string]string // lower-case uuid -> name

	registry *Registry // optional persistent source of names
}

// WithRegistry makes the directory fall back on the persisted registry
// before reading player save files.
func (d *NameDirectory) WithRegistry(r *Registry) *NameDirectory {
	d.registry = r
	return d
}

// NewNameDirectory returns an empty directory.
func NewNameDirectory() *NameDirectory {
	return &NameDirectory{known: make(map[string]string)}
}

// Learn records a uuid/name pair observed at runtime.
func (d *NameDirectory) Learn(uuid, name string) {
	if !ValidUUID(uuid) || !playerPattern.MatchString(name) {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.known[strings.ToLower(uuid)] = name
}

// Resolver returns a function resolving UUIDs with the player files of
// saveDir as fallback.
func (d *NameDirectory) Resolver(saveDir string) func(uuid string) PlayerRef {
	playersDir := filepath.Join(saveDir, "universe", "players")
	return func(uuid string) PlayerRef {
		return PlayerRef{UUID: uuid, Name: d.name(playersDir, uuid)}
	}
}

func (d *NameDirectory) name(playersDir, uuid string) string {
	if !ValidUUID(uuid) {
		return ""
	}
	key := strings.ToLower(uuid)
	d.mu.RLock()
	name := d.known[key]
	d.mu.RUnlock()
	if name != "" {
		return name
	}
	if d.registry != nil {
		name = d.registry.Name(key)
	}
	if name == "" {
		name = readNameplate(filepath.Join(playersDir, key+".json"))
	}
	d.Learn(uuid, name)
	return name
}

// readNameplate extracts Components.Nameplate.Text from a player file.
func readNameplate(path string) string {
	info, err := os.Stat(path)
	if err != nil || info.Size() > maxPlayerFileBytes {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var doc struct {
		Components struct {
			Nameplate struct {
				Text string `json:"Text"`
			} `json:"Nameplate"`
		} `json:"Components"`
	}
	if json.Unmarshal(data, &doc) != nil || !playerPattern.MatchString(doc.Components.Nameplate.Text) {
		return ""
	}
	return doc.Components.Nameplate.Text
}
