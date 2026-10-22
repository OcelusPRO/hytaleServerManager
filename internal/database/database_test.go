package database

import (
	"path/filepath"
	"testing"
)

func TestOpenMigratesOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manager.db")
	for range 2 { // reopening keeps the schema
		db, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var version int
		if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != len(migrations) {
			t.Fatalf("version %d, %v", version, err)
		}
		var fk int
		if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
			t.Fatalf("foreign keys %d, %v", fk, err)
		}
		if _, err := db.Exec(`INSERT OR IGNORE INTO players (uuid, name) VALUES ('u', 'n')`); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}
