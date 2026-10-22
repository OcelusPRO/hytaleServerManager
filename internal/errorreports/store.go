package errorreports

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"hytale-server-manager/internal/database"
)

const (
	// maxIssues bounds the issues kept: the least recently seen go first.
	maxIssues = 500
	// maxOccurrences bounds the history kept for each issue.
	maxOccurrences = 1000
)

// Summary describes an issue: every occurrence of the same error.
type Summary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Culprit   string    `json:"culprit,omitempty"`
	Plugin    string    `json:"plugin,omitempty"`
	Level     string    `json:"level"`
	Count     int       `json:"count"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
	// Resolved issues are reopened by a new occurrence, which marks them as
	// regressed until they are resolved again.
	Resolved  bool `json:"resolved"`
	Regressed bool `json:"regressed,omitempty"`
	// ResolvedAt is when the issue was last marked as resolved.
	ResolvedAt *time.Time `json:"resolvedAt,omitempty"`
}

// Occurrence is one appearance of an issue, to relate it to what happened
// on the server at that time.
type Occurrence struct {
	// Time is when the error happened, as reported by the server.
	Time    time.Time `json:"time"`
	EventID string    `json:"eventId,omitempty"`
	// Message is the error of this occurrence: it may differ from the
	// issue's title (values inserted in the message).
	Message string `json:"message,omitempty"`
	Release string `json:"release,omitempty"`
	// Launch is when the server process that raised it was started.
	Launch *time.Time `json:"launch,omitempty"`
}

// Issue is an issue with its latest occurrence and its history, oldest
// first. The history only holds the latest maxOccurrences.
type Issue struct {
	Summary
	Last        Event        `json:"last"`
	Occurrences []Occurrence `json:"occurrences"`
}

// Store keeps the issues in the manager database.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// NewStore uses the error tables of db.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db, now: time.Now}
}

const summaryColumns = `id, title, culprit, plugin, level, count, first_seen, last_seen, resolved, regressed, resolved_at`

func scanSummary(row interface{ Scan(...any) error }, extra ...any) (Summary, error) {
	var s Summary
	var first, last int64
	var resolvedAt sql.NullInt64
	dest := append([]any{&s.ID, &s.Title, &s.Culprit, &s.Plugin, &s.Level, &s.Count, &first, &last, &s.Resolved, &s.Regressed, &resolvedAt}, extra...)
	if err := row.Scan(dest...); err != nil {
		return Summary{}, err
	}
	s.FirstSeen, s.LastSeen = database.Time(first), database.Time(last)
	if resolvedAt.Valid {
		t := database.Time(resolvedAt.Int64)
		s.ResolvedAt = &t
	}
	return s, nil
}

func nullTimePtr(t *time.Time) sql.NullInt64 {
	if t == nil {
		return sql.NullInt64{}
	}
	return database.NullMillis(*t)
}

// Add records an occurrence and returns its issue id.
func (s *Store) Add(ev Event) (string, error) {
	id := ev.Fingerprint()
	now := database.Millis(s.now())
	last, err := json.Marshal(ev)
	if err != nil {
		return "", err
	}
	occ := ev.occurrence()
	return id, s.inTx(func(tx *sql.Tx) error {
		// Column names in the update refer to the stored row: a resolved
		// issue seen again is reopened as regressed.
		if _, err := tx.Exec(`
INSERT INTO error_issues (id, title, culprit, plugin, level, count, first_seen, last_seen, last_event)
VALUES (?, ?, ?, ?, ?, 1, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
	title = excluded.title,
	culprit = excluded.culprit,
	plugin = CASE WHEN excluded.plugin <> '' THEN excluded.plugin ELSE plugin END,
	level = excluded.level,
	count = count + 1,
	last_seen = excluded.last_seen,
	regressed = CASE WHEN resolved = 1 THEN 1 ELSE regressed END,
	resolved = 0,
	last_event = excluded.last_event`,
			id, ev.Title(), ev.Culprit(), ev.Plugin(), ev.Level, now, now, string(last)); err != nil {
			return err
		}
		if err := insertOccurrence(tx, id, occ); err != nil {
			return err
		}
		if _, err := tx.Exec(`
DELETE FROM error_occurrences WHERE issue_id = ? AND id <= (
	SELECT id FROM error_occurrences WHERE issue_id = ? ORDER BY id DESC LIMIT 1 OFFSET ?)`,
			id, id, maxOccurrences); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM error_issues WHERE id IN (
	SELECT id FROM error_issues ORDER BY last_seen DESC, id LIMIT -1 OFFSET ?)`, maxIssues)
		return err
	})
}

func insertOccurrence(tx *sql.Tx, issueID string, o Occurrence) error {
	_, err := tx.Exec(`INSERT INTO error_occurrences (issue_id, time, event_id, message, release, launch) VALUES (?, ?, ?, ?, ?, ?)`,
		issueID, database.Millis(o.Time), o.EventID, o.Message, o.Release, nullTimePtr(o.Launch))
	return err
}

// List returns the issues, the most recently seen first.
func (s *Store) List() ([]Summary, error) {
	rows, err := s.db.Query(`SELECT ` + summaryColumns + ` FROM error_issues ORDER BY last_seen DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Summary{}
	for rows.Next() {
		sum, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, sum)
	}
	return list, rows.Err()
}

// Get returns an issue with its latest occurrence and its history.
func (s *Store) Get(id string) (Issue, bool, error) {
	var last string
	sum, err := scanSummary(s.db.QueryRow(`SELECT `+summaryColumns+`, last_event FROM error_issues WHERE id = ?`, id), &last)
	if errors.Is(err, sql.ErrNoRows) {
		return Issue{}, false, nil
	}
	if err != nil {
		return Issue{}, false, err
	}
	issue := Issue{Summary: sum, Occurrences: []Occurrence{}}
	if err := json.Unmarshal([]byte(last), &issue.Last); err != nil {
		return Issue{}, false, fmt.Errorf("issue %s: %w", id, err)
	}
	rows, err := s.db.Query(`SELECT time, event_id, message, release, launch FROM error_occurrences WHERE issue_id = ? ORDER BY id`, id)
	if err != nil {
		return Issue{}, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var o Occurrence
		var at int64
		var launch sql.NullInt64
		if err := rows.Scan(&at, &o.EventID, &o.Message, &o.Release, &launch); err != nil {
			return Issue{}, false, err
		}
		o.Time = database.Time(at)
		if launch.Valid {
			t := database.Time(launch.Int64)
			o.Launch = &t
		}
		issue.Occurrences = append(issue.Occurrences, o)
	}
	return issue, true, rows.Err()
}

// SetResolved marks an issue as resolved or open again. It reports false
// for an unknown issue.
func (s *Store) SetResolved(id string, resolved bool) (bool, error) {
	var found bool
	err := s.inTx(func(tx *sql.Tx) error {
		if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM error_issues WHERE id = ?)`, id).Scan(&found); err != nil || !found {
			return err
		}
		_, err := tx.Exec(`
UPDATE error_issues SET
	resolved = ?,
	regressed = 0,
	resolved_at = CASE WHEN ? THEN ? ELSE resolved_at END
WHERE id = ? AND resolved <> ?`,
			resolved, resolved, database.Millis(s.now()), id, resolved)
		return err
	})
	return found, err
}

// Delete forgets an issue and its history.
func (s *Store) Delete(id string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM error_issues WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// Clear forgets every issue.
func (s *Store) Clear() error {
	_, err := s.db.Exec(`DELETE FROM error_issues`)
	return err
}

func (s *Store) inTx(fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ImportJSON moves the issues of the JSON file used before the database
// into it, then renames the file to <path>.migrated. A missing file is not
// an error.
func (s *Store) ImportJSON(path string) (int, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var file struct {
		Issues []*Issue `json:"issues"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	imported := 0
	err = s.inTx(func(tx *sql.Tx) error {
		for _, is := range file.Issues {
			if is == nil || is.ID == "" {
				continue
			}
			last, err := json.Marshal(is.Last)
			if err != nil {
				return err
			}
			res, err := tx.Exec(`
INSERT OR IGNORE INTO error_issues (id, title, culprit, plugin, level, count, first_seen, last_seen, resolved, regressed, resolved_at, last_event)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				is.ID, is.Title, is.Culprit, is.Plugin, is.Level, is.Count, database.Millis(is.FirstSeen), database.Millis(is.LastSeen),
				is.Resolved, is.Regressed, nullTimePtr(is.ResolvedAt), string(last))
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				continue // already imported
			}
			for _, o := range is.Occurrences {
				if err := insertOccurrence(tx, is.ID, o); err != nil {
					return err
				}
			}
			imported++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return imported, os.Rename(path, path+".migrated")
}
