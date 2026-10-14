package commandcatalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"hytale-server-manager/internal/fsutil"
	"hytale-server-manager/internal/gameserver"
)

const (
	cacheFile    = ".hsm-commands.json"
	bootedMarker = "Hytale Server Booted!"
	buildTimeout = 3 * time.Minute
	// catalogFormat changes whenever the parser does, invalidating caches.
	catalogFormat = "format-3"
)

// Catalog is the cached command list of a save.
type Catalog struct {
	// Key identifies the game version and mods the catalog was built with.
	Key     string    `json:"key"`
	BuiltAt time.Time `json:"builtAt"`
	Entries []Entry   `json:"entries"`
}

// Status is what the console page needs.
type Status struct {
	Building bool    `json:"building"`
	Entries  []Entry `json:"entries"`
}

// Deps lists the collaborators of the Service.
type Deps struct {
	Builder     *Builder
	Console     *gameserver.Console
	SaveDir     func(id string) (string, error)
	RunningSave func() string // "" when the server is not running
	GameVersion func() string
	Logger      *slog.Logger
}

// Service keeps one catalog per save, rebuilding it after a server boot
// whenever the game version or the mods of the save changed.
type Service struct {
	deps Deps

	mu       sync.Mutex
	building map[string]bool
}

// NewService creates the catalog service.
func NewService(deps Deps) *Service {
	return &Service{deps: deps, building: map[string]bool{}}
}

// Get returns the cached catalog of a save.
func (s *Service) Get(saveID string) Status {
	s.mu.Lock()
	building := s.building[saveID]
	s.mu.Unlock()
	st := Status{Building: building, Entries: []Entry{}}
	dir, err := s.deps.SaveDir(saveID)
	if err != nil {
		return st
	}
	var cat Catalog
	if found, err := fsutil.ReadJSON(filepath.Join(dir, cacheFile), &cat); found && err == nil && cat.Entries != nil {
		st.Entries = cat.Entries
	}
	return st
}

// Watch rebuilds catalogs when the server finishes booting.
func (s *Service) Watch(ctx context.Context) {
	lines, cancel := s.deps.Console.Subscribe()
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case l := <-lines:
			if strings.Contains(l.Text, bootedMarker) {
				go s.Refresh(ctx, false)
			}
		}
	}
}

// Refresh rebuilds the catalog of the running save if it is outdated (or
// unconditionally with force).
func (s *Service) Refresh(ctx context.Context, force bool) error {
	saveID := s.deps.RunningSave()
	if saveID == "" {
		return gameserver.ErrNotRunning
	}
	dir, err := s.deps.SaveDir(saveID)
	if err != nil {
		return err
	}
	key := s.key(dir)
	var cached Catalog
	if found, _ := fsutil.ReadJSON(filepath.Join(dir, cacheFile), &cached); found && cached.Key == key && !force {
		return nil
	}
	if !s.begin(saveID) {
		return nil // already building
	}
	defer s.end(saveID)

	ctx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()
	started := time.Now()
	entries, err := s.deps.Builder.Build(ctx, dir)
	if err != nil {
		s.deps.Logger.Warn("command catalog build failed", "error", err)
		return err
	}
	s.deps.Logger.Info("command catalog built", "commands", len(entries), "took", time.Since(started).Round(time.Millisecond))
	return fsutil.WriteJSONAtomic(filepath.Join(dir, cacheFile), Catalog{Key: key, BuiltAt: time.Now().UTC(), Entries: entries}, 0o640)
}

func (s *Service) begin(saveID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.building[saveID] {
		return false
	}
	s.building[saveID] = true
	return true
}

func (s *Service) end(saveID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.building, saveID)
}

// key fingerprints the game version and the enabled mods of a save.
func (s *Service) key(saveDir string) string {
	parts := []string{catalogFormat, s.deps.GameVersion()}
	if entries, err := os.ReadDir(filepath.Join(saveDir, "mods")); err == nil {
		for _, e := range entries {
			if info, err := e.Info(); err == nil && info.Mode().IsRegular() {
				parts = append(parts, fmt.Sprintf("%s:%d:%d", e.Name(), info.Size(), info.ModTime().Unix()))
			}
		}
	}
	slices.Sort(parts[2:])
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:8])
}
