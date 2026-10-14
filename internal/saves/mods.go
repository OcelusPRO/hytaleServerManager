package saves

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"hytale-server-manager/internal/fsutil"
)

const (
	enabledModsDir  = "mods"
	disabledModsDir = "mods-disabled" // outside mods/ so the server ignores it
)

// Mod errors.
var (
	ErrInvalidModName = errors.New("nom de mod invalide (fichier .jar ou .zip)")
	ErrNotAnArchive   = errors.New("le fichier n'est pas une archive .jar/.zip valide")
	ErrModExists      = errors.New("un mod portant ce nom existe déjà")
	ErrModTooLarge    = errors.New("le fichier dépasse la taille autorisée")
)

var (
	modNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._()+\-\[\]]{0,150}\.(jar|zip)$`)
	zipMagic       = []byte("PK\x03\x04")
)

// Mod describes an installed mod file.
type Mod struct {
	FileName   string    `json:"fileName"`
	Enabled    bool      `json:"enabled"`
	SizeBytes  int64     `json:"sizeBytes"`
	ModifiedAt time.Time `json:"modifiedAt"`
	// CurseForge is set when the file was installed from CurseForge.
	CurseForge *CurseForgeLink `json:"curseforge,omitempty"`
}

// ModStore manages the mods of each save.
type ModStore struct {
	saves *Store

	linksMu sync.Mutex
}

// NewModStore returns a mod manager backed by saves.
func NewModStore(saves *Store) *ModStore {
	return &ModStore{saves: saves}
}

// List returns enabled and disabled mods of a save.
func (m *ModStore) List(saveID string) ([]Mod, error) {
	dir, err := m.saves.Dir(saveID)
	if err != nil {
		return nil, err
	}
	links, err := m.Links(saveID)
	if err != nil {
		return nil, err
	}
	mods := []Mod{}
	for _, sub := range []string{enabledModsDir, disabledModsDir} {
		entries, err := os.ReadDir(filepath.Join(dir, sub))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.Type().IsRegular() || !modNamePattern.MatchString(e.Name()) {
				continue
			}
			info, err := e.Info()
			if err != nil {
				return nil, err
			}
			mod := Mod{
				FileName:   e.Name(),
				Enabled:    sub == enabledModsDir,
				SizeBytes:  info.Size(),
				ModifiedAt: info.ModTime().UTC(),
			}
			if link, ok := links[e.Name()]; ok {
				mod.CurseForge = &link
			}
			mods = append(mods, mod)
		}
	}
	slices.SortFunc(mods, func(a, b Mod) int { return strings.Compare(strings.ToLower(a.FileName), strings.ToLower(b.FileName)) })
	return mods, nil
}

// Add stores an uploaded mod in the enabled mods directory.
func (m *ModStore) Add(saveID, fileName string, r io.Reader, maxBytes int64) (Mod, error) {
	if !modNamePattern.MatchString(fileName) {
		return Mod{}, ErrInvalidModName
	}
	dir, err := m.saves.Dir(saveID)
	if err != nil {
		return Mod{}, err
	}
	if _, _, found := m.locate(dir, fileName); found {
		return Mod{}, ErrModExists
	}
	return m.write(dir, fileName, enabledModsDir, r, maxBytes)
}

// Put stores a mod, replacing any file of the same name while keeping
// it in the directory (enabled or disabled) given by enabled.
func (m *ModStore) Put(saveID, fileName string, enabled bool, r io.Reader, maxBytes int64) (Mod, error) {
	if !modNamePattern.MatchString(fileName) {
		return Mod{}, ErrInvalidModName
	}
	dir, err := m.saves.Dir(saveID)
	if err != nil {
		return Mod{}, err
	}
	sub, other := enabledModsDir, disabledModsDir
	if !enabled {
		sub, other = other, sub
	}
	mod, err := m.write(dir, fileName, sub, r, maxBytes)
	if err != nil {
		return Mod{}, err
	}
	// Never leave the same file in both directories.
	if err := os.Remove(filepath.Join(dir, other, fileName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Mod{}, err
	}
	mod.Enabled = enabled
	return mod, nil
}

// State reports whether fileName is installed and enabled.
func (m *ModStore) State(saveID, fileName string) (enabled, found bool, err error) {
	if !modNamePattern.MatchString(fileName) {
		return false, false, ErrInvalidModName
	}
	dir, err := m.saves.Dir(saveID)
	if err != nil {
		return false, false, err
	}
	_, enabled, found = m.locate(dir, fileName)
	return enabled, found, nil
}

func (m *ModStore) write(saveDir, fileName, sub string, r io.Reader, maxBytes int64) (Mod, error) {
	modsDir := filepath.Join(saveDir, sub)
	if err := os.MkdirAll(modsDir, 0o750); err != nil {
		return Mod{}, err
	}
	tmp, err := os.CreateTemp(modsDir, ".upload-*")
	if err != nil {
		return Mod{}, err
	}
	defer os.Remove(tmp.Name())
	size, err := copyMod(tmp, r, maxBytes)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return Mod{}, err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(modsDir, fileName)); err != nil {
		return Mod{}, err
	}
	return Mod{FileName: fileName, Enabled: sub == enabledModsDir, SizeBytes: size, ModifiedAt: time.Now().UTC()}, nil
}

// Remove deletes a mod file.
func (m *ModStore) Remove(saveID, fileName string) error {
	path, err := m.path(saveID, fileName)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return m.Unlink(saveID, fileName)
}

// SetEnabled moves a mod between the enabled and disabled directories.
func (m *ModStore) SetEnabled(saveID, fileName string, enabled bool) error {
	path, err := m.path(saveID, fileName)
	if err != nil {
		return err
	}
	dir, _ := m.saves.Dir(saveID)
	targetDir := filepath.Join(dir, disabledModsDir)
	if enabled {
		targetDir = filepath.Join(dir, enabledModsDir)
	}
	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		return err
	}
	return os.Rename(path, filepath.Join(targetDir, fileName))
}

func (m *ModStore) path(saveID, fileName string) (string, error) {
	if !modNamePattern.MatchString(fileName) {
		return "", ErrInvalidModName
	}
	dir, err := m.saves.Dir(saveID)
	if err != nil {
		return "", err
	}
	path, _, found := m.locate(dir, fileName)
	if !found {
		return "", ErrNotFound
	}
	return path, nil
}

func (m *ModStore) locate(saveDir, fileName string) (path string, enabled bool, found bool) {
	for _, sub := range []string{enabledModsDir, disabledModsDir} {
		p := filepath.Join(saveDir, sub, fileName)
		if fsutil.Exists(p) {
			return p, sub == enabledModsDir, true
		}
	}
	return "", false, false
}

// copyMod copies at most maxBytes and checks the zip signature.
func copyMod(dst io.Writer, src io.Reader, maxBytes int64) (int64, error) {
	head := make([]byte, len(zipMagic))
	if _, err := io.ReadFull(src, head); err != nil || !bytes.Equal(head, zipMagic) {
		return 0, ErrNotAnArchive
	}
	if _, err := dst.Write(head); err != nil {
		return 0, err
	}
	n, err := io.Copy(dst, io.LimitReader(src, maxBytes-int64(len(head))+1))
	if err != nil {
		return 0, fmt.Errorf("write mod: %w", err)
	}
	total := n + int64(len(head))
	if total > maxBytes {
		return 0, ErrModTooLarge
	}
	return total, nil
}
