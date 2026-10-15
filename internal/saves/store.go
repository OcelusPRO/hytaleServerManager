package saves

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"hytale-server-manager/internal/archive"
	"hytale-server-manager/internal/fsutil"
)

// metaFile stores the save metadata inside its directory.
const metaFile = ".hsm-save.json"

// Save describes one world save.
type Save struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	SizeBytes int64     `json:"sizeBytes"`
}

// Store manages the save directories.
type Store struct {
	dir    string
	tmpDir string

	mu sync.Mutex
}

// NewStore manages saves located in dir; tmpDir must be on the same
// filesystem.
func NewStore(dir, tmpDir string) *Store {
	return &Store{dir: dir, tmpDir: tmpDir}
}

// Dir returns the working directory of save id.
func (s *Store) Dir(id string) (string, error) {
	if !validID(id) {
		return "", ErrInvalidID
	}
	dir := filepath.Join(s.dir, id)
	if !fsutil.Exists(filepath.Join(dir, metaFile)) {
		return "", ErrNotFound
	}
	return dir, nil
}

// List returns all saves sorted by creation date.
func (s *Store) List() ([]Save, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	saves := []Save{}
	for _, e := range entries {
		if !e.IsDir() || !validID(e.Name()) {
			continue
		}
		save, err := s.Get(e.Name())
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		saves = append(saves, save)
	}
	slices.SortFunc(saves, func(a, b Save) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return saves, nil
}

// Get returns the save id with its current size.
func (s *Store) Get(id string) (Save, error) {
	dir, err := s.Dir(id)
	if err != nil {
		return Save{}, err
	}
	var save Save
	if _, err := fsutil.ReadJSON(filepath.Join(dir, metaFile), &save); err != nil {
		return Save{}, err
	}
	save.ID = id
	save.SizeBytes, _ = fsutil.DirSize(dir)
	return save, nil
}

// Create makes a new empty save; the server generates the world on start.
func (s *Store) Create(name string) (Save, error) {
	name, err := normalizeName(name)
	if err != nil {
		return Save{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	id, err := newID()
	if err != nil {
		return Save{}, err
	}
	dir := filepath.Join(s.dir, id)
	if err := os.MkdirAll(filepath.Join(dir, "mods"), 0o750); err != nil {
		return Save{}, err
	}
	save := Save{ID: id, Name: name, CreatedAt: time.Now().UTC()}
	if err := s.writeMeta(dir, save); err != nil {
		os.RemoveAll(dir)
		return Save{}, err
	}
	return save, nil
}

// Import creates a save from a zip archive of a server directory.
func (s *Store) Import(name, zipPath string, limits archive.Limits) (Save, error) {
	name, err := normalizeName(name)
	if err != nil {
		return Save{}, err
	}
	staging, err := os.MkdirTemp(s.tmpDir, "import-*")
	if err != nil {
		return Save{}, err
	}
	defer os.RemoveAll(staging)
	if err := archive.ExtractZip(zipPath, staging, limits); err != nil {
		return Save{}, fmt.Errorf("invalid archive: %w", err)
	}
	root := findSaveRoot(staging)

	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := newID()
	if err != nil {
		return Save{}, err
	}
	save := Save{ID: id, Name: name, CreatedAt: time.Now().UTC()}
	if err := s.writeMeta(root, save); err != nil {
		return Save{}, err
	}
	if err := os.MkdirAll(filepath.Join(root, "mods"), 0o750); err != nil {
		return Save{}, err
	}
	if err := os.Rename(root, filepath.Join(s.dir, id)); err != nil {
		return Save{}, err
	}
	return save, nil
}

// Rename changes the display name of a save.
func (s *Store) Rename(id, name string) (Save, error) {
	name, err := normalizeName(name)
	if err != nil {
		return Save{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	save, err := s.Get(id)
	if err != nil {
		return Save{}, err
	}
	save.Name = name
	dir, _ := s.Dir(id)
	return save, s.writeMeta(dir, save)
}

// Delete removes a save and all its content.
func (s *Store) Delete(id string) error {
	dir, err := s.Dir(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	trash := filepath.Join(s.tmpDir, "deleted-"+id)
	if err := os.Rename(dir, trash); err != nil {
		return err
	}
	return os.RemoveAll(trash)
}

// ReplaceContent swaps the content of save id with the directory newRoot,
// keeping the save metadata. newRoot must be on the same filesystem.
func (s *Store) ReplaceContent(id, newRoot string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	save, err := s.Get(id)
	if err != nil {
		return err
	}
	dir, _ := s.Dir(id)
	if err := s.writeMeta(newRoot, save); err != nil {
		return err
	}
	trash := filepath.Join(s.tmpDir, "replaced-"+id)
	if err := os.RemoveAll(trash); err != nil {
		return err
	}
	if err := os.Rename(dir, trash); err != nil {
		return err
	}
	if err := os.Rename(newRoot, dir); err != nil {
		_ = os.Rename(trash, dir)
		return err
	}
	return os.RemoveAll(trash)
}

func (s *Store) writeMeta(dir string, save Save) error {
	save.SizeBytes = 0
	return fsutil.WriteJSONAtomic(filepath.Join(dir, metaFile), save, 0o640)
}

// findSaveRoot unwraps archives whose content sits in a single folder.
func findSaveRoot(dir string) string {
	for {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 || !entries[0].IsDir() || isServerDirName(entries[0].Name()) {
			return dir
		}
		dir = filepath.Join(dir, entries[0].Name())
	}
}

func isServerDirName(name string) bool {
	switch name {
	case "universe", "mods", "logs":
		return true
	}
	return false
}

// IsNotFound reports whether err means the save does not exist.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, fs.ErrNotExist)
}
