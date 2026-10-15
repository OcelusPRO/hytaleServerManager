package saves

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"hytale-server-manager/internal/archive"
)

const backupTimeLayout = "20060102-150405"

// autoSuffix marks the backups created by the autosave rotation.
const autoSuffix = "-auto"

var backupIDPattern = regexp.MustCompile(`^\d{8}-\d{6}-[a-f0-9]{4}(-auto)?$`)

// ErrInvalidBackupID is returned for malformed backup identifiers.
var ErrInvalidBackupID = errors.New("invalid backup identifier")

// Backup describes an archived copy of a save.
type Backup struct {
	ID        string    `json:"id"`
	SaveID    string    `json:"saveId"`
	CreatedAt time.Time `json:"createdAt"`
	SizeBytes int64     `json:"sizeBytes"`
	Auto      bool      `json:"auto"`
}

// BackupStore creates, lists and restores zip backups of saves.
type BackupStore struct {
	dir     string
	tmpDir  string
	saves   *Store
	extract archive.Limits

	mu sync.Mutex // one backup/restore at a time
}

// NewBackupStore stores backups below dir.
func NewBackupStore(dir, tmpDir string, saves *Store, extract archive.Limits) *BackupStore {
	return &BackupStore{dir: dir, tmpDir: tmpDir, saves: saves, extract: extract}
}

// Create archives the current content of a save.
func (b *BackupStore) Create(saveID string) (Backup, error) {
	return b.create(saveID, false, 0)
}

// CreateAuto archives a save as an autosave, then deletes the oldest
// autosaves so that at most slots of them remain (manual backups are kept).
func (b *BackupStore) CreateAuto(saveID string, slots int) (Backup, error) {
	return b.create(saveID, true, max(slots, 1))
}

func (b *BackupStore) create(saveID string, auto bool, slots int) (Backup, error) {
	saveDir, err := b.saves.Dir(saveID)
	if err != nil {
		return Backup{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	id, err := newBackupID(time.Now().UTC())
	if err != nil {
		return Backup{}, err
	}
	if auto {
		id += autoSuffix
	}
	dir := filepath.Join(b.dir, saveID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return Backup{}, err
	}
	tmp, err := os.CreateTemp(dir, ".backup-*")
	if err != nil {
		return Backup{}, err
	}
	defer os.Remove(tmp.Name())
	err = archive.ZipDirectory(saveDir, tmp, excludeFromBackup)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return Backup{}, fmt.Errorf("create backup: %w", err)
	}
	final := filepath.Join(dir, id+".zip")
	if err := os.Rename(tmp.Name(), final); err != nil {
		return Backup{}, err
	}
	backup, err := b.describe(saveID, id)
	if err != nil {
		return Backup{}, err
	}
	if auto {
		if err := b.pruneAuto(saveID, slots); err != nil {
			return backup, fmt.Errorf("rotate autosaves: %w", err)
		}
	}
	return backup, nil
}

// pruneAuto removes the oldest autosaves beyond keep. Callers hold b.mu.
func (b *BackupStore) pruneAuto(saveID string, keep int) error {
	backups, err := b.List(saveID) // newest first
	if err != nil {
		return err
	}
	kept := 0
	for _, bk := range backups {
		if !bk.Auto {
			continue
		}
		if kept++; kept <= keep {
			continue
		}
		if err := os.Remove(filepath.Join(b.dir, saveID, bk.ID+".zip")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// List returns the backups of a save, newest first.
func (b *BackupStore) List(saveID string) ([]Backup, error) {
	if !validID(saveID) {
		return nil, ErrInvalidID
	}
	entries, err := os.ReadDir(filepath.Join(b.dir, saveID))
	if errors.Is(err, os.ErrNotExist) {
		return []Backup{}, nil
	}
	if err != nil {
		return nil, err
	}
	backups := []Backup{}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".zip")
		if !ok || !backupIDPattern.MatchString(id) {
			continue
		}
		backup, err := b.describe(saveID, id)
		if err != nil {
			return nil, err
		}
		backups = append(backups, backup)
	}
	slices.SortFunc(backups, func(x, y Backup) int { return y.CreatedAt.Compare(x.CreatedAt) })
	return backups, nil
}

// Path returns the archive file of a backup.
func (b *BackupStore) Path(saveID, backupID string) (string, error) {
	if !validID(saveID) {
		return "", ErrInvalidID
	}
	if !backupIDPattern.MatchString(backupID) {
		return "", ErrInvalidBackupID
	}
	path := filepath.Join(b.dir, saveID, backupID+".zip")
	if _, err := os.Stat(path); err != nil {
		return "", ErrNotFound
	}
	return path, nil
}

// Delete removes a backup.
func (b *BackupStore) Delete(saveID, backupID string) error {
	path, err := b.Path(saveID, backupID)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

// DeleteAll removes every backup of a save.
func (b *BackupStore) DeleteAll(saveID string) error {
	if !validID(saveID) {
		return ErrInvalidID
	}
	return os.RemoveAll(filepath.Join(b.dir, saveID))
}

// Restore replaces the content of the save with the backup.
func (b *BackupStore) Restore(saveID, backupID string) error {
	path, err := b.Path(saveID, backupID)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	staging, err := os.MkdirTemp(b.tmpDir, "restore-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := archive.ExtractZip(path, staging, b.extract); err != nil {
		return fmt.Errorf("extract backup: %w", err)
	}
	// Keep the server's native backups, which are excluded from archives.
	if saveDir, err := b.saves.Dir(saveID); err == nil {
		native := filepath.Join(saveDir, "backups")
		if _, err := os.Stat(native); err == nil {
			if err := os.RemoveAll(filepath.Join(staging, "backups")); err != nil {
				return err
			}
			if err := os.Rename(native, filepath.Join(staging, "backups")); err != nil {
				return err
			}
		}
	}
	return b.saves.ReplaceContent(saveID, staging)
}

func (b *BackupStore) describe(saveID, id string) (Backup, error) {
	info, err := os.Stat(filepath.Join(b.dir, saveID, id+".zip"))
	if err != nil {
		return Backup{}, err
	}
	created, err := time.Parse(backupTimeLayout, id[:len(backupTimeLayout)])
	if err != nil {
		return Backup{}, err
	}
	return Backup{ID: id, SaveID: saveID, CreatedAt: created, SizeBytes: info.Size(), Auto: strings.HasSuffix(id, autoSuffix)}, nil
}

func newBackupID(now time.Time) (string, error) {
	suffix := make([]byte, 2)
	if _, err := rand.Read(suffix); err != nil {
		return "", err
	}
	return now.Format(backupTimeLayout) + "-" + hex.EncodeToString(suffix), nil
}

// excludeFromBackup skips regenerable or redundant data.
func excludeFromBackup(rel string, isDir bool) bool {
	if !isDir {
		return false
	}
	switch rel {
	case "logs", ".cache", "backups":
		return true
	}
	return false
}
