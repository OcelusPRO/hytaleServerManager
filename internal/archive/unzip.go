package archive

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ExtractZip extracts the zip file at src into dest. Symlinks are skipped
// and every entry is confined to dest.
func ExtractZip(src, dest string, limits Limits) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	b := &budget{limits: limits}
	for _, f := range r.File {
		if err := extractZipEntry(f, dest, b); err != nil {
			return err
		}
	}
	return nil
}

func extractZipEntry(f *zip.File, dest string, b *budget) error {
	target, err := SafeJoin(dest, f.Name)
	if err != nil {
		return err
	}
	mode := f.Mode()
	switch {
	case mode.IsDir():
		return os.MkdirAll(target, 0o755)
	case mode&os.ModeSymlink != 0 || !mode.IsRegular():
		return nil // never materialise links or special files
	}
	if err := b.addFile(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("open %s: %w", f.Name, err)
	}
	defer rc.Close()
	return writeLimited(target, rc, filePerm(mode), b)
}

// writeLimited copies r into a new file, enforcing the remaining budget
// regardless of what the archive headers claim.
func writeLimited(target string, r io.Reader, perm os.FileMode, b *budget) error {
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	remaining := b.remaining()
	n, copyErr := io.Copy(out, io.LimitReader(r, remaining+1))
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if err := b.consume(n); err != nil {
		return err
	}
	return closeErr
}

func filePerm(mode os.FileMode) os.FileMode {
	if mode&0o111 != 0 {
		return 0o755
	}
	return 0o644
}
