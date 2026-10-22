package database

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func openSafe(t *testing.T, path string) {
	t.Helper()
	db, err := OpenWithBackup(path, quiet)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
}

// seed opens the database, adds a player and closes it.
func seed(t *testing.T, path, uuid string) {
	t.Helper()
	db, err := OpenWithBackup(path, quiet)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO players (uuid, name) VALUES (?, 'p')`, uuid); err != nil {
		t.Fatal(err)
	}
}

func players(t *testing.T, path string) []string {
	t.Helper()
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT uuid FROM players ORDER BY uuid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var u string
		rows.Scan(&u)
		out = append(out, u)
	}
	return out
}

func TestBackupIsMadeFromAWorkingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manager.db")
	seed(t, path, "a")
	seed(t, path, "b") // the copy made at this start holds "a"
	openSafe(t, path)  // and this one "a" and "b"
	if got := players(t, path+BackupSuffix); strings.Join(got, ",") != "a,b" {
		t.Fatalf("backup holds %v", got)
	}
	if problem := checkFile(path + BackupSuffix); problem != "" {
		t.Fatal(problem)
	}
	if info, _ := os.Stat(path + BackupSuffix); info.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Errorf("backup mode %v", info.Mode())
	}
}

func TestCorruptDatabaseIsRestored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manager.db")
	seed(t, path, "a")
	openSafe(t, path) // backup holds "a"
	backup, _ := os.ReadFile(path + BackupSuffix)

	// Damage the main file (and drop its WAL, as a crash could).
	os.Remove(path + "-wal")
	os.Remove(path + "-shm")
	if err := os.WriteFile(path, []byte("this is not a database at all, garbage garbage garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	openSafe(t, path)
	if got := players(t, path); strings.Join(got, ",") != "a" {
		t.Fatalf("restored database holds %v", got)
	}
	aside, _ := filepath.Glob(path + ".corrupt-*")
	if len(aside) == 0 {
		t.Fatal("the damaged file was not kept aside")
	}
	// The damaged database never replaced the copy.
	if after, _ := os.ReadFile(path + BackupSuffix); string(after) != string(backup) {
		t.Fatal("the backup was overwritten")
	}
}

func TestMissingDatabaseIsRestored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manager.db")
	seed(t, path, "a")
	openSafe(t, path)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		os.Remove(path + suffix)
	}
	openSafe(t, path)
	if got := players(t, path); strings.Join(got, ",") != "a" {
		t.Fatalf("restored database holds %v", got)
	}
	if got := players(t, path+BackupSuffix); strings.Join(got, ",") != "a" {
		t.Fatalf("an empty database replaced the backup: %v", got)
	}
}

func TestNoUsableBackupStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manager.db")
	os.WriteFile(path, []byte("garbage garbage garbage garbage garbage garbage garbage"), 0o600)
	os.WriteFile(path+BackupSuffix, []byte("garbage too"), 0o600)
	openSafe(t, path)
	if got := players(t, path); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}
