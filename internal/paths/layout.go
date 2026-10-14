// Package paths describes where every piece of persistent data lives.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
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

// GameVersionsDir keeps previous game builds so that one can be rolled back to.
func (l Layout) GameVersionsDir() string { return filepath.Join(l.root, "game-versions") }

// SavesDir holds one working directory per world save.
func (l Layout) SavesDir() string { return filepath.Join(l.root, "saves") }

// BackupsDir holds archived saves.
func (l Layout) BackupsDir() string { return filepath.Join(l.root, "backups") }

// HomeDir is used as HOME for child processes.
func (l Layout) HomeDir() string { return filepath.Join(l.root, "home") }

// TmpDir is a scratch directory on the same filesystem (atomic renames).
func (l Layout) TmpDir() string { return filepath.Join(l.root, "tmp") }

// File returns a path inside the manager directory.
func (l Layout) File(name string) string { return filepath.Join(l.ManagerDir(), name) }

// EnsureAll creates every directory of the layout and empties TmpDir.
func (l Layout) EnsureAll() error {
	if err := os.RemoveAll(l.TmpDir()); err != nil {
		return fmt.Errorf("clean tmp dir: %w", err)
	}
	dirs := []string{
		l.ManagerDir(), l.JavaDir(), l.GameDir(), l.GameVersionsDir(),
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
