// Package store keeps Physa's local records for the repo in one SQLite
// database, physa.db in the store directory the version-control interface
// names (shared by every worktree, never committed). It keeps slice records:
// which capabilities were read, with what, which entries were taken by slug,
// when and by which session. It also keeps one gather of the repo's sources (docs,
// history, PRs), replaced whole in one transaction, and the one init proposal made from it,
// which a new gather clears. Schema changes are numbered migrations applied at
// open.
package store

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Store is an open database.
type Store struct {
	db *sql.DB
}

// Open opens <dir>/physa.db, creating the directory and the file, and
// applies any migrations it has not seen.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.Join(dir, "physa.db") +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

type migration struct {
	n    int
	name string
}

// migrations lists the embedded migrations in number order.
func migrations() ([]migration, error) {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	var ms []migration
	for _, e := range entries {
		num, _, ok := strings.Cut(e.Name(), "_")
		n, err := strconv.Atoi(num)
		if !ok || err != nil {
			return nil, fmt.Errorf("migration %q is not named <number>_<name>.sql", e.Name())
		}
		ms = append(ms, migration{n, e.Name()})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].n < ms[j].n })
	return ms, nil
}

func migrate(db *sql.DB) error {
	ms, err := migrations()
	if err != nil {
		return err
	}
	var current int
	if err := db.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
		return err
	}
	if len(ms) > 0 && current > ms[len(ms)-1].n {
		return fmt.Errorf("the store is at version %d, made by a newer physa (this one knows %d)", current, ms[len(ms)-1].n)
	}
	if len(ms) == 0 && current > 0 {
		return fmt.Errorf("the store is at version %d, made by a newer physa", current)
	}
	for _, m := range ms {
		if m.n <= current {
			continue
		}
		body, err := migrationFiles.ReadFile("migrations/" + m.name)
		if err != nil {
			return err
		}
		if err := apply(db, m.n, string(body)); err != nil {
			return fmt.Errorf("migration %s: %w", m.name, err)
		}
	}
	return nil
}

func apply(db *sql.DB, n int, body string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(body); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", n)); err != nil {
		return err
	}
	return tx.Commit()
}

// SliceRecord is one printed slice. Entries holds the slugs of an --entry read,
// whose Capabilities are empty. Entries and Session are empty when there are
// none or the calling session is unknown; Entries encodes as null then, and
// Session likewise.
type SliceRecord struct {
	At           time.Time
	Capabilities []string
	With         string
	Entries      []string
	Session      string
}

// MarshalJSON encodes the record with entries and session null when absent.
func (r SliceRecord) MarshalJSON() ([]byte, error) {
	caps := r.Capabilities
	if caps == nil {
		caps = []string{}
	}
	out := struct {
		At           string   `json:"at"`
		Capabilities []string `json:"capabilities"`
		With         string   `json:"with"`
		Entries      []string `json:"entries"`
		Session      *string  `json:"session"`
	}{At: r.At.UTC().Format(time.RFC3339), Capabilities: caps, With: r.With}
	if len(r.Entries) > 0 {
		out.Entries = r.Entries
	}
	if r.Session != "" {
		out.Session = &r.Session
	}
	return json.Marshal(out)
}

// PutSliceRecord stores one slice record.
func (s *Store) PutSliceRecord(r SliceRecord) error {
	caps := r.Capabilities
	if caps == nil {
		caps = []string{}
	}
	cj, err := json.Marshal(caps)
	if err != nil {
		return err
	}
	var entries, session any
	if len(r.Entries) > 0 {
		ej, err := json.Marshal(r.Entries)
		if err != nil {
			return err
		}
		entries = string(ej)
	}
	if r.Session != "" {
		session = r.Session
	}
	_, err = s.db.Exec(`INSERT INTO slice_records (at, capabilities, "with", entries, session) VALUES (?, ?, ?, ?, ?)`,
		r.At.UTC().Format(time.RFC3339), string(cj), r.With, entries, session)
	return err
}

// SliceRecords returns every slice record, newest first.
func (s *Store) SliceRecords() ([]SliceRecord, error) {
	rows, err := s.db.Query(`SELECT at, capabilities, "with", entries, session FROM slice_records ORDER BY at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SliceRecord
	for rows.Next() {
		var at, caps, with string
		var entries, session sql.NullString
		if err := rows.Scan(&at, &caps, &with, &entries, &session); err != nil {
			return nil, err
		}
		r := SliceRecord{With: with, Session: session.String}
		if r.At, err = time.Parse(time.RFC3339, at); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(caps), &r.Capabilities); err != nil {
			return nil, err
		}
		if entries.Valid {
			if err := json.Unmarshal([]byte(entries.String), &r.Entries); err != nil {
				return nil, err
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
