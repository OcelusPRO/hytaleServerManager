package archive

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSafeJoinRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"../evil", "a/../../evil", "/etc/passwd", "C:/windows", `..\evil`, ""} {
		if _, err := SafeJoin(root, name); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("SafeJoin(%q) = %v, want ErrUnsafePath", name, err)
		}
	}
	got, err := SafeJoin(root, "universe/worlds/default/config.json")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "universe", "worlds", "default", "config.json"); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func writeZip(t *testing.T, files map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "a.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExtractZipBlocksZipSlip(t *testing.T) {
	src := writeZip(t, map[string]string{"../escape.txt": "x"})
	dest := t.TempDir()
	if err := ExtractZip(src, dest, Limits{}); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("expected ErrUnsafePath, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "escape.txt")); err == nil {
		t.Fatal("file escaped the destination")
	}
}

func TestExtractZipEnforcesLimits(t *testing.T) {
	src := writeZip(t, map[string]string{"a.txt": "0123456789", "b.txt": "0123456789"})
	if err := ExtractZip(src, t.TempDir(), Limits{MaxTotalBytes: 15}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("size limit: got %v", err)
	}
	if err := ExtractZip(src, t.TempDir(), Limits{MaxFiles: 1}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("file limit: got %v", err)
	}
	if err := ExtractZip(src, t.TempDir(), Limits{MaxFiles: 2, MaxTotalBytes: 20}); err != nil {
		t.Fatalf("within limits: %v", err)
	}
}

func TestZipDirectoryRoundTrip(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "universe", "worlds"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(src, "universe", "worlds", "w.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(src, "logs", "latest.log"), []byte("log"), 0o644)

	zipPath := filepath.Join(t.TempDir(), "out.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	err = ZipDirectory(src, f, func(rel string, isDir bool) bool { return isDir && rel == "logs" })
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	if err := ExtractZip(zipPath, dest, Limits{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "universe", "worlds", "w.json")); err != nil {
		t.Errorf("missing world file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "logs")); err == nil {
		t.Error("excluded directory was archived")
	}
}
