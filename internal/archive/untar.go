package archive

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ExtractTarGz extracts a gzip-compressed tarball into dest. Relative
// symlinks are kept only when they resolve inside dest.
func ExtractTarGz(r io.Reader, dest string, limits Limits) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("open gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	b := &budget{limits: limits}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}
		if err := extractTarEntry(tr, hdr, dest, b); err != nil {
			return err
		}
	}
}

func extractTarEntry(tr *tar.Reader, hdr *tar.Header, dest string, b *budget) error {
	target, err := SafeJoin(dest, hdr.Name)
	if err != nil {
		return err
	}
	// Earlier symlinks could redirect this entry: check the real location.
	if err := ensureRealParentInside(dest, target); err != nil {
		return err
	}
	switch hdr.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(target, 0o755)
	case tar.TypeReg:
		if err := b.addFile(); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return writeLimited(target, tr, filePerm(os.FileMode(hdr.Mode)), b)
	case tar.TypeSymlink:
		return extractSymlink(dest, target, hdr.Linkname)
	default:
		return nil // hard links, devices, fifos are ignored
	}
}

// ensureRealParentInside resolves the existing part of target's parent
// directory and checks that it still lies within dest.
func ensureRealParentInside(dest, target string) error {
	realDest, err := filepath.EvalSymlinks(dest)
	if err != nil {
		return err
	}
	parent := filepath.Dir(target)
	for {
		real, err := filepath.EvalSymlinks(parent)
		if err == nil {
			if !isWithin(realDest, real) {
				return fmt.Errorf("%w: %s", ErrUnsafePath, target)
			}
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		next := filepath.Dir(parent)
		if next == parent {
			return nil
		}
		parent = next
	}
}

func isWithin(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func extractSymlink(dest, target, linkname string) error {
	if filepath.IsAbs(linkname) || strings.HasPrefix(linkname, "/") {
		return nil
	}
	resolved := filepath.Join(filepath.Dir(target), filepath.FromSlash(linkname))
	if !isWithin(dest, resolved) || strings.Contains(filepath.ToSlash(linkname), "..") {
		return nil // link could point outside dest: drop it
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.Symlink(linkname, target)
}
