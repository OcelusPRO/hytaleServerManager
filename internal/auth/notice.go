package auth

import (
	"errors"
	"io/fs"
	"os"

	"hytale-server-manager/internal/fsutil"
)

// InitialPasswordNotice keeps the generated password readable on the volume
// until the administrator replaces it with one of their own.
type InitialPasswordNotice struct {
	path string
}

// NewInitialPasswordNotice returns a notice stored at path.
func NewInitialPasswordNotice(path string) *InitialPasswordNotice {
	return &InitialPasswordNotice{path: path}
}

// Write stores the generated password with owner-only permissions.
func (n *InitialPasswordNotice) Write(password string) error {
	return fsutil.WriteFileAtomic(n.path, []byte(password+"\n"), 0o600)
}

// Clear removes the notice once the password has been changed.
func (n *InitialPasswordNotice) Clear() error {
	err := os.Remove(n.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// Path returns where the notice is stored.
func (n *InitialPasswordNotice) Path() string { return n.path }
