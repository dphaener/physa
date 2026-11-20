// Package store keeps the repo's local measures in one SQLite database in
// git's common directory, shared by every worktree and never committed. It
// holds the PR check results physa sync pulls from GitHub; schema changes are
// numbered migrations applied at open.
package store

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Store is an open database.
type Store struct {
	db *sql.DB
}

// PRCheck is the result of the physa check run for one PR head. Result is
// the result JSON as text.
type PRCheck struct {
	PR          int
	HeadSHA     string
	CheckRunID  int64
	CompletedAt string
	Result      string
}

// Open opens <commonDir>/physa/physa.db, creating it, and applies any
// migrations it has not seen.
func Open(commonDir string) (*Store, error) {
	dir := filepath.Join(commonDir, "physa")
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

// PutPRCheck stores c, replacing the row for the same PR and head only when
// something differs. changed says whether the database changed.
func (s *Store) PutPRCheck(c PRCheck) (changed bool, err error) {
	res, err := s.db.Exec(`INSERT INTO pr_checks (pr, head_sha, check_run_id, completed_at, result)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (pr, head_sha) DO UPDATE SET
  check_run_id = excluded.check_run_id,
  completed_at = excluded.completed_at,
  result = excluded.result
WHERE pr_checks.check_run_id IS NOT excluded.check_run_id
   OR pr_checks.completed_at IS NOT excluded.completed_at
   OR pr_checks.result IS NOT excluded.result`,
		c.PR, c.HeadSHA, c.CheckRunID, c.CompletedAt, c.Result)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// HasPRCheck reports whether a result is stored for the PR at that head.
func (s *Store) HasPRCheck(pr int, headSHA string) (bool, error) {
	var one int
	err := s.db.QueryRow("SELECT 1 FROM pr_checks WHERE pr = ? AND head_sha = ?", pr, headSHA).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// PRChecks returns every stored result ordered by PR, then head.
func (s *Store) PRChecks() ([]PRCheck, error) {
	rows, err := s.db.Query("SELECT pr, head_sha, check_run_id, completed_at, result FROM pr_checks ORDER BY pr, head_sha")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PRCheck
	for rows.Next() {
		var c PRCheck
		if err := rows.Scan(&c.PR, &c.HeadSHA, &c.CheckRunID, &c.CompletedAt, &c.Result); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
