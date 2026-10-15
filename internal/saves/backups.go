package saves

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"hytale-server-manager/internal/archive"
)

const backupTimeLayout = "20060102-150405"

// autoSuffix marks the backups created by the autosave rotation.
const autoSuffix = "-auto"

var backupIDPattern = regexp.MustCompile(`^\d{8}-\d{6}-[a-f0-9]{4}(-auto)?$`)

// ErrInvalidBackupID is returned for malformed backup identifiers.
var ErrInvalidBackupID = errors.New("invalid backup identifier")

// Backup origins.
const (
	OriginManual   = "manual"   // created by the user
	OriginAutosave = "autosave" // periodic autosave rotation
	OriginRestore  = "restore"  // safety copy taken before a restore
	OriginUpdate   = "update"   // safety copy taken before a server update
)

const (
	maxBackupName = 80
	maxBackupNote = 500
)

// Backup describes an archived copy of a save.
type Backup struct {
	ID        string    `json:"id"`
	SaveID    string    `json:"saveId"`
	CreatedAt time.Time `json:"createdAt"`
	SizeBytes int64     `json:"sizeBytes"`
	// Auto is true for autosaves still subject to rotation. Naming an
	// autosave turns it into a manual backup (it is kept).
	Auto   bool   `json:"auto"`
	Name   string `json:"name"`
	Origin string `json:"origin"`
	// Detail is a machine-readable precision about the origin, e.g. the
	// backup being restored. Slot is the autosave slot (1..N), 0 otherwise.
	Detail string `json:"detail,omitempty"`
	Note   string `json:"note,omitempty"`
	Slot   int    `json:"slot,omitempty"`
}

// BackupOptions describes how a backup is labelled.
type BackupOptions struct {
	Name   string
	Origin string
	Detail string
	Note   string
}

// backupMeta is persisted next to the archive as <id>.json.
type backupMeta struct {
	Name   string `json:"name,omitempty"`
	Origin string `json:"origin,omitempty"`
	Detail string `json:"detail,omitempty"`
	Note   string `json:"note,omitempty"`
	Slot   int    `json:"slot,omitempty"`
	Pinned bool   `json:"pinned,omitempty"`
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
	return b.CreateNamed(saveID, BackupOptions{})
}

// CreateNamed archives a save with a name, an origin and optional details.
func (b *BackupStore) CreateNamed(saveID string, opts BackupOptions) (Backup, error) {
	name, err := normalizeBackupText(opts.Name, maxBackupName)
	if err != nil {
		return Backup{}, err
	}
	note, err := normalizeBackupText(opts.Note, maxBackupNote)
	if err != nil {
		return Backup{}, err
	}
	origin := opts.Origin
	if origin == "" || origin == OriginAutosave {
		origin = OriginManual
	}
	return b.create(saveID, false, 0, backupMeta{Name: name, Origin: origin, Detail: opts.Detail, Note: note})
}

// CreateAuto archives a save as an autosave, then deletes the oldest
// autosaves so that at most slots of them remain (manual backups are kept).
func (b *BackupStore) CreateAuto(saveID string, slots int) (Backup, error) {
	return b.create(saveID, true, max(slots, 1), backupMeta{Origin: OriginAutosave})
}

func (b *BackupStore) create(saveID string, auto bool, slots int, meta backupMeta) (Backup, error) {
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
		if meta.Slot, err = b.nextSlot(saveID, slots); err != nil {
			return Backup{}, err
		}
		meta.Name = fmt.Sprintf("Autosave %d", meta.Slot)
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
	if err := b.writeMeta(saveID, id, meta); err != nil {
		os.Remove(final)
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

// nextSlot returns the autosave slot the next autosave takes: the lowest slot
// not used by the newest slots-1 autosaves (the ones that survive rotation).
func (b *BackupStore) nextSlot(saveID string, slots int) (int, error) {
	backups, err := b.List(saveID) // newest first
	if err != nil {
		return 0, err
	}
	used := map[int]bool{}
	kept := 0
	for _, bk := range backups {
		if !bk.Auto {
			continue
		}
		if kept++; kept < slots {
			used[bk.Slot] = true
		}
	}
	slot := 1
	for used[slot] {
		slot++
	}
	return slot, nil
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
		if err := b.remove(saveID, bk.ID); err != nil {
			return err
		}
	}
	return nil
}

// remove deletes an archive and its metadata file.
func (b *BackupStore) remove(saveID, id string) error {
	os.Remove(filepath.Join(b.dir, saveID, id+".json"))
	if err := os.Remove(filepath.Join(b.dir, saveID, id+".zip")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Update renames a backup and edits its note. Naming an autosave turns it
// into a manual backup so that rotation no longer removes it.
func (b *BackupStore) Update(saveID, backupID, name, note string) (Backup, error) {
	if _, err := b.Path(saveID, backupID); err != nil {
		return Backup{}, err
	}
	name, err := normalizeBackupText(name, maxBackupName)
	if err != nil {
		return Backup{}, err
	}
	note, err = normalizeBackupText(note, maxBackupNote)
	if err != nil {
		return Backup{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	meta := b.readMeta(saveID, backupID)
	if name != "" && name != meta.Name && strings.HasSuffix(backupID, autoSuffix) {
		meta.Pinned = true
	}
	if name != "" {
		meta.Name = name
	}
	meta.Note = note
	if err := b.writeMeta(saveID, backupID, meta); err != nil {
		return Backup{}, err
	}
	return b.describe(saveID, backupID)
}

func normalizeBackupText(text string, limit int) (string, error) {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > limit {
		return "", ErrInvalidName
	}
	for _, r := range text {
		if !unicode.IsPrint(r) && r != '\n' {
			return "", ErrInvalidName
		}
	}
	return text, nil
}

func (b *BackupStore) readMeta(saveID, id string) backupMeta {
	var meta backupMeta
	if data, err := os.ReadFile(filepath.Join(b.dir, saveID, id+".json")); err == nil {
		_ = json.Unmarshal(data, &meta)
	}
	return meta
}

func (b *BackupStore) writeMeta(saveID, id string, meta backupMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(b.dir, saveID, id+".json"), data, 0o640)
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
	_ = path
	return b.remove(saveID, backupID)
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
	meta := b.readMeta(saveID, id)
	isAuto := strings.HasSuffix(id, autoSuffix)
	origin := meta.Origin
	if origin == "" {
		origin = OriginManual
		if isAuto {
			origin = OriginAutosave
		}
	}
	return Backup{
		ID: id, SaveID: saveID, CreatedAt: created, SizeBytes: info.Size(),
		Auto: isAuto && !meta.Pinned, Name: meta.Name, Origin: origin,
		Detail: meta.Detail, Note: meta.Note, Slot: meta.Slot,
	}, nil
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
