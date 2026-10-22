package database

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
)

// BackupSuffix names the copy kept next to the database.
const BackupSuffix = ".bak"

// OpenWithBackup opens the database like Open, protected by a copy kept at
// path+BackupSuffix:
//
//   - when the database passes an integrity check, it is copied (VACUUM
//     INTO) before anything uses it, and the copy replaces the previous one
//     only once it passes the check too: the copy always comes from a
//     working database;
//   - when it fails, it is moved aside (path.corrupt-<time>) and the copy
//     is restored, if it passes the check; otherwise the manager starts
//     with an empty database;
//   - when it is missing or empty, the copy is restored the same way, so an
//     empty database never overwrites a good copy.
//
// The copy is made before the migrations, so it also covers a failed
// schema upgrade.
func OpenWithBackup(path string, logger *slog.Logger) (*sql.DB, error) {
	if info, err := os.Stat(path); (err != nil || info.Size() == 0) && checkFile(path+BackupSuffix) == "" {
		logger.Warn("database: missing or empty, restoring the backup", "path", path)
		return openRestored(path, logger)
	}
	db, err := openFile(path)
	if err != nil {
		return nil, err
	}
	if problem := check(db); problem != "" {
		db.Close()
		logger.Error("database: integrity check failed", "path", path, "problem", problem)
		return openRestored(path, logger)
	}
	if err := backup(db, path+BackupSuffix); err != nil {
		// The database is fine: run without a fresh copy rather than not at all.
		logger.Warn("database: could not refresh the backup", "error", err)
	}
	if err := migrateOrClose(db, path); err != nil {
		return nil, err
	}
	return db, nil
}

// openRestored restores the backup in place of the database and opens it.
func openRestored(path string, logger *slog.Logger) (*sql.DB, error) {
	db, err := restore(path, logger)
	if err != nil {
		return nil, err
	}
	if err := migrateOrClose(db, path); err != nil {
		return nil, err
	}
	return db, nil
}

// check returns "" when db is a readable, consistent database, or the
// problem found.
func check(db *sql.DB) string {
	rows, err := db.Query("PRAGMA integrity_check")
	if err != nil {
		return err.Error()
	}
	defer rows.Close()
	var problems []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return err.Error()
		}
		if line != "ok" {
			problems = append(problems, line)
		}
	}
	if err := rows.Err(); err != nil {
		return err.Error()
	}
	return strings.Join(problems, "; ")
}

// checkFile checks the database file at path without changing it.
func checkFile(path string) string {
	if _, err := os.Stat(path); err != nil {
		return err.Error()
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return err.Error()
	}
	defer db.Close()
	return check(db)
}

// backup copies db to dest through a temporary file, replacing dest only
// when the copy passes the integrity check.
func backup(db *sql.DB, dest string) error {
	tmp := dest + ".tmp"
	_ = os.Remove(tmp) // VACUUM INTO refuses an existing file
	if _, err := db.Exec("VACUUM INTO ?", tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	if problem := checkFile(tmp); problem != "" {
		os.Remove(tmp)
		return fmt.Errorf("the copy failed its integrity check: %s", problem)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}

// restore moves the damaged database aside and puts the backup in its
// place, or starts an empty database when the backup cannot be used.
func restore(path string, logger *slog.Logger) (*sql.DB, error) {
	aside := fmt.Sprintf("%s.corrupt-%s", path, time.Now().UTC().Format("20060102-150405"))
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Rename(path+suffix, aside+suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("move the damaged database aside: %w", err)
		}
	}
	saved := path + BackupSuffix
	if problem := checkFile(saved); problem == "" {
		if err := copyFile(saved, path); err != nil {
			return nil, fmt.Errorf("restore the database backup: %w", err)
		}
		logger.Warn("database: restored from the backup made at the last start", "damaged", aside, "backup", saved)
	} else {
		logger.Error("database: no usable backup, starting with an empty database", "damaged", aside, "backup", problem)
	}
	return openFile(path)
}

// copyFile copies src to dst through a temporary file.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
