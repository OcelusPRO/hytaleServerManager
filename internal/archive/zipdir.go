package archive

import (
	"archive/zip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// ExcludeFunc reports whether the slash-separated relative path should be
// left out of the archive.
type ExcludeFunc func(rel string, isDir bool) bool

// ZipDirectory writes the regular files below srcDir into w as a zip.
func ZipDirectory(srcDir string, w io.Writer, exclude ExcludeFunc) error {
	zw := zip.NewWriter(w)
	err := filepath.WalkDir(srcDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, p)
		if err != nil || rel == "." {
			return err
		}
		rel = filepath.ToSlash(rel)
		if exclude != nil && exclude(rel, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // directories are implied, links are skipped
		}
		return addFile(zw, p, rel, d)
	})
	if err != nil {
		zw.Close()
		return err
	}
	return zw.Close()
}

func addFile(zw *zip.Writer, path, rel string, d fs.DirEntry) error {
	info, err := d.Info()
	if err != nil {
		return err
	}
	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	hdr.Name = rel
	hdr.Method = zip.Deflate
	dst, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	_, err = io.Copy(dst, src)
	return err
}
