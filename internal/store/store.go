// Package store keeps Physa's local records for the repo in one SQLite
// database, physa.db in the store directory the version-control interface
// names (shared by every worktree, never committed). It keeps slice records:
// which capabilities were read, with what, which entries were taken by slug,
// when and by which session. It also keeps one gather of the repo's sources (docs,
// history, PRs), replaced whole in one transaction, which clears init's record
// beside it. Init's record (the areas, the proposal, the recoveries and init's
// spend) has its tables here but its code in package initrecord. It also holds
// the engine's tables (ideas, pursuits, plans, plan revisions, events, intents
// and timers), which only the engine's state package writes. Schema changes,
// init's record's included, are numbered migrations applied at open.
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
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := whileBusy(func() error { return db.Ping() }); err != nil {
		db.Close()
		return nil, err
	}
	if err := whileBusy(func() error { return migrate(db) }); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// OpenReadOnly opens an existing <path> without creating, migrating or
// writing it. It is for readers outside the engine that must never write and
// must work when no engine runs (the commit hook). A missing file gives an
// error wrapping fs.ErrNotExist.
func OpenReadOnly(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(250)&_pragma=query_only(1)")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s read-only: %w", path, err)
	}
	return db, nil
}

// whileBusy runs f again, with a backoff of 10ms doubling, while it fails
// with SQLITE_BUSY, which busy_timeout does not cover for a connection's
// first statement on a fresh file. It gives up after 5s.
func whileBusy(f func() error) error {
	deadline := time.Now().Add(5 * time.Second)
	wait := 10 * time.Millisecond
	for {
		err := f()
		if err == nil || !isBusy(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(wait)
		wait *= 2
	}
}

func isBusy(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "SQLITE_BUSY") || strings.Contains(msg, "database is locked")
}

// DB returns the open database. It is for the engine's state package, which
// alone writes the engine's tables (codecheck's engine-tables-writer rule),
// and for package initrecord, which keeps init's record.
func (s *Store) DB() *sql.DB { return s.db }

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
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return orderMigrations(names)
}

// orderMigrations parses migration file names and orders them by number. The
// numbers must run 1, 2, 3… with none shared and none missing: a second file
// with a number already applied would be skipped without a word, and a gap
// means a file went missing.
func orderMigrations(names []string) ([]migration, error) {
	var ms []migration
	for _, name := range names {
		num, _, ok := strings.Cut(name, "_")
		n, err := strconv.Atoi(num)
		if !ok || err != nil {
			return nil, fmt.Errorf("migration %q is not named <number>_<name>.sql", name)
		}
		ms = append(ms, migration{n, name})
	}
	sort.Slice(ms, func(i, j int) bool {
		if ms[i].n != ms[j].n {
			return ms[i].n < ms[j].n
		}
		return ms[i].name < ms[j].name
	})
	for i, m := range ms {
		if i > 0 && m.n == ms[i-1].n {
			return nil, fmt.Errorf("migrations %q and %q share number %d", ms[i-1].name, m.name, m.n)
		}
		if m.n != i+1 {
			return nil, fmt.Errorf("migration number %d is missing (next is %q)", i+1, m.name)
		}
	}
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
	var current int
	if err := tx.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
		return err
	}
	if current >= n {
		return nil // another process applied it meanwhile
	}
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
