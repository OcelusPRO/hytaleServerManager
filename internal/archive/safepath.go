// Package archive extracts and creates archives without ever writing
// outside of the target directory (zip-slip, symlink and bomb protection).
package archive

import (
	"errors"
	"fmt"
	"math"
	"path"
	"path/filepath"
	"strings"
)

// ErrUnsafePath is returned for entries escaping the destination directory.
var ErrUnsafePath = errors.New("archive entry escapes destination")

// ErrTooLarge is returned when an archive exceeds its Limits.
var ErrTooLarge = errors.New("archive exceeds allowed size")

// Limits bounds what an extraction may produce.
type Limits struct {
	MaxFiles      int
	MaxTotalBytes int64
}

// SafeJoin resolves the archive entry name below root, rejecting absolute
// paths, drive letters and ".." traversal.
func SafeJoin(root, name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
		return "", fmt.Errorf("%w: %q", ErrUnsafePath, name)
	}
	clean := path.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%w: %q", ErrUnsafePath, name)
	}
	if clean == "." {
		return filepath.Clean(root), nil
	}
	return filepath.Join(root, filepath.FromSlash(clean)), nil
}

type budget struct {
	limits Limits
	files  int
	bytes  int64
}

func (b *budget) addFile() error {
	b.files++
	if b.limits.MaxFiles > 0 && b.files > b.limits.MaxFiles {
		return fmt.Errorf("%w: too many files", ErrTooLarge)
	}
	return nil
}

// remaining is the number of bytes that may still be written. It stays
// below MaxInt64 so that callers can safely add one to it.
func (b *budget) remaining() int64 {
	if b.limits.MaxTotalBytes <= 0 {
		return math.MaxInt64 - 1
	}
	return b.limits.MaxTotalBytes - b.bytes
}

func (b *budget) consume(n int64) error {
	b.bytes += n
	if b.limits.MaxTotalBytes > 0 && b.bytes > b.limits.MaxTotalBytes {
		return ErrTooLarge
	}
	return nil
}
