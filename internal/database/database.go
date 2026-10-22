// Package database opens the manager's SQLite database (manager.db).
//
// It holds the data that grows over time and is queried by period: error
// reports and the history of players. Settings and secrets stay in small
// JSON files, and what describes a world or a backup stays next to it.
//
// The driver is modernc.org/sqlite, written in Go: the image is built
// without cgo. Times are stored as Unix milliseconds (UTC).
package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"time"

	_ "modernc.org/sqlite"
)

// Open opens (or creates) the database at path and brings its schema up
// to date. OpenWithBackup also keeps and uses a verified copy.
func Open(path string) (*sql.DB, error) {
	db, err := openFile(path)
	if err != nil {
		return nil, err
	}
	if err := migrateOrClose(db, path); err != nil {
		return nil, err
	}
	return db, nil
}

func migrateOrClose(db *sql.DB, path string) error {
	if err := migrate(context.Background(), db); err != nil {
		db.Close()
		return fmt.Errorf("database %s: %w", path, err)
	}
	return nil
}

// openFile opens the database file at path, creating it if needed.
func openFile(path string) (*sql.DB, error) {
	// Private like the other files of the manager. SQLite gives its -wal
	// and -shm files the mode of the database file.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	f.Close()
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, err
	}
	pragmas := url.Values{}
	for _, p := range []string{"busy_timeout(5000)", "journal_mode(WAL)", "synchronous(NORMAL)", "foreign_keys(1)"} {
		pragmas.Add("_pragma", p)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?"+pragmas.Encode())
	if err != nil {
		return nil, err
	}
	// One connection serialises the writes, so SQLite never reports a busy
	// database; the load is far below what this limits.
	db.SetMaxOpenConns(1)
	return db, nil
}

// migrate applies the migrations newer than the database's user_version,
// each in its own transaction.
func migrate(ctx context.Context, db *sql.DB) error {
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > len(migrations) {
		return fmt.Errorf("schema version %d is newer than this manager (%d): downgrade not supported", version, len(migrations))
	}
	for i := version; i < len(migrations); i++ {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Millis converts a time to the stored form.
func Millis(t time.Time) int64 { return t.UnixMilli() }

// NullMillis stores a zero time as NULL.
func NullMillis(t time.Time) sql.NullInt64 {
	if t.IsZero() {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: t.UnixMilli(), Valid: true}
}

// Time converts a stored time back.
func Time(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

// NullTime converts a nullable stored time back (zero when NULL).
func NullTime(v sql.NullInt64) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return Time(v.Int64)
}
