// Package paths describes where every piece of persistent data lives.
package paths

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
)

// Layout resolves the on-disk locations below the data directory.
type Layout struct {
	root string
}

// New returns the layout rooted at dataDir.
func New(dataDir string) Layout {
	return Layout{root: filepath.Clean(dataDir)}
}

// ManagerDir holds the manager's own state (credentials, settings, keys).
func (l Layout) ManagerDir() string { return filepath.Join(l.root, "manager") }

// RuntimeDir holds downloaded tooling (the Java runtime).
func (l Layout) RuntimeDir() string { return filepath.Join(l.root, "runtime") }

// JavaDir holds installed Java runtimes.
func (l Layout) JavaDir() string { return filepath.Join(l.RuntimeDir(), "java") }

// GameDir holds the installed server binaries and assets.
func (l Layout) GameDir() string { return filepath.Join(l.root, "game") }

// SavesDir holds one working directory per world save.
func (l Layout) SavesDir() string { return filepath.Join(l.root, "saves") }

// BackupsDir holds archived saves.
func (l Layout) BackupsDir() string { return filepath.Join(l.root, "backups") }

// HomeDir is used as HOME for child processes.
func (l Layout) HomeDir() string { return filepath.Join(l.root, "home") }

// TmpDir is a scratch directory on the same filesystem (atomic renames).
func (l Layout) TmpDir() string { return filepath.Join(l.root, "tmp") }

// MachineIDFile is the machine identifier of the container: the image links
// /etc/machine-id to it, the game server reads it as its hardware id.
func (l Layout) MachineIDFile() string { return filepath.Join(l.root, "machine-id") }

// File returns a path inside the manager directory.
func (l Layout) File(name string) string { return filepath.Join(l.ManagerDir(), name) }

// EnsureAll creates every directory of the layout and empties TmpDir.
func (l Layout) EnsureAll() error {
	if err := os.RemoveAll(l.TmpDir()); err != nil {
		return fmt.Errorf("clean tmp dir: %w", err)
	}
	dirs := []string{
		l.ManagerDir(), l.JavaDir(), l.GameDir(),
		l.SavesDir(), l.BackupsDir(), l.HomeDir(), l.TmpDir(),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	// The manager directory contains secrets: keep it private.
	return os.Chmod(l.ManagerDir(), 0o700)
}

// staleTemp matches the files written next to their destination and renamed
// once complete: ".<name>.tmp-<n>" (fsutil.WriteFileAtomic), ".backup-<n>"
// (backups) and ".upload-<n>" (mods). One left behind means the manager was
// stopped while writing it.
var staleTemp = regexp.MustCompile(`^\.(.+\.tmp|backup|upload)-[0-9]+$`)

// CleanStale deletes what an interrupted operation may have left behind:
// half-written files, the build being replaced during an update, and the
// builds older versions kept for rollback. It must run before anything
// writes, and returns the paths it removed.
func (l Layout) CleanStale() ([]string, error) {
	var removed []string
	var errs []error
	for _, dir := range []string{
		filepath.Join(l.root, "game.old"),
		filepath.Join(l.root, "game-versions"), // rollback builds, no longer kept
	} {
		if _, err := os.Lstat(dir); err != nil {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			errs = append(errs, err)
			continue
		}
		removed = append(removed, dir)
	}
	for _, root := range []string{l.ManagerDir(), l.SavesDir(), l.BackupsDir()} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if !d.Type().IsRegular() || !staleTemp.MatchString(d.Name()) {
				return nil
			}
			if err := os.Remove(path); err != nil {
				errs = append(errs, err)
				return nil
			}
			removed = append(removed, path)
			return nil
		})
		if err != nil {
			errs = append(errs, err)
		}
	}
	return removed, errors.Join(errs...)
}
