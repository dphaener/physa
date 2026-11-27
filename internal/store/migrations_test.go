package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedMigrationsAreNumberedOnce(t *testing.T) {
	if _, err := migrations(); err != nil {
		t.Fatal(err)
	}
}

func TestOrderMigrations(t *testing.T) {
	ms, err := orderMigrations([]string{"0002_b.sql", "0001_a.sql", "0003_c.sql"})
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"0001_a.sql", "0002_b.sql", "0003_c.sql"} {
		if ms[i].name != want || ms[i].n != i+1 {
			t.Errorf("ms[%d] = %+v, want %s", i, ms[i], want)
		}
	}
}

func TestOrderMigrationsRefuses(t *testing.T) {
	cases := map[string]struct {
		names []string
		want  string
	}{
		"shared number": {[]string{"0001_a.sql", "0002_b.sql", "0002_c.sql"}, `migrations "0002_b.sql" and "0002_c.sql" share number 2`},
		"gap":           {[]string{"0001_a.sql", "0003_c.sql"}, `migration number 2 is missing (next is "0003_c.sql")`},
		"no first":      {[]string{"0002_b.sql"}, "migration number 1 is missing"},
		"bad name":      {[]string{"first.sql"}, "is not named <number>_<name>.sql"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := orderMigrations(c.names)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to contain %q", err, c.want)
			}
		})
	}
}

// TestBetsBecomePursuits shows migration 14 keeps a store's bets as
// pursuits with no signal: their ids, appetites and everything that points
// at them, and leaves the pitches as they were.
func TestBetsBecomePursuits(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "physa.db")+"?_pragma=foreign_keys(1)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	ms, err := migrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms[:13] {
		body, err := migrationFiles.ReadFile("migrations/" + m.name)
		if err != nil {
			t.Fatal(err)
		}
		if err := apply(t.Context(), db, m.n, string(body)); err != nil {
			t.Fatalf("migration %s: %v", m.name, err)
		}
	}
	const now = "2026-11-01T12:00:00Z"
	for _, q := range []string{
		`INSERT INTO ideas (id, slug, text, person, created_at, updated_at) VALUES (1, 'a', 'Try it', 'ann', '` + now + `', '` + now + `')`,
		`INSERT INTO pitches (id, idea_id, problem, appetite_tokens, person, created_at, updated_at) VALUES (1, 1, 'slow', 300, 'ann', '` + now + `', '` + now + `')`,
		`INSERT INTO bets (id, slug, idea_id, pitch_id, person, appetite_tokens, spent_tokens, created_at) VALUES (7, 'a', 1, 1, 'ann', 300, 40, '` + now + `')`,
		`INSERT INTO plans (id, bet_id, created_at) VALUES (1, 7, '` + now + `')`,
		`INSERT INTO changes (id, bet_id, name, capability, created_at, updated_at) VALUES (1, 7, 'x', 'cli', '` + now + `', '` + now + `')`,
		`INSERT INTO spends (bet_id, change, session, tokens, at) VALUES (7, 'x', 's', 40, '` + now + `')`,
		`INSERT INTO needs_you (person, kind, bet_id, text, created_at) VALUES ('ann', 'appetite', 7, 't', '` + now + `')`,
		`INSERT INTO evidence (kind, commit_id, bet, at, source, recorded_at) VALUES ('landed', 'c1', 'a', '` + now + `', 'vcs', '` + now + `')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	var slug string
	var appetite, spent int64
	var claim sql.NullString
	if err := db.QueryRow(`SELECT slug, appetite_tokens, spent_tokens, signal_claim FROM pursuits WHERE id = 7`).Scan(&slug, &appetite, &spent, &claim); err != nil {
		t.Fatal(err)
	}
	if slug != "a" || appetite != 300 || spent != 40 || claim.Valid {
		t.Errorf("pursuit = %q %d %d %v; want a 300 40 and no signal", slug, appetite, spent, claim)
	}
	var n int
	if err := db.QueryRow(`SELECT (SELECT count(*) FROM plans WHERE pursuit_id = 7) + (SELECT count(*) FROM changes WHERE pursuit_id = 7)
		+ (SELECT count(*) FROM spends WHERE pursuit_id = 7) + (SELECT count(*) FROM needs_you WHERE pursuit_id = 7)
		+ (SELECT count(*) FROM evidence WHERE pursuit = 'a') + (SELECT count(*) FROM pitches)`).Scan(&n); err != nil || n != 6 {
		t.Errorf("rows kept = %d, %v; want 6", n, err)
	}
	if _, err := db.Exec(`INSERT INTO plans (pursuit_id, created_at) VALUES (99, ?)`, now); err == nil {
		t.Error("a plan for no pursuit was accepted; the foreign key was lost")
	}
	if _, err := db.Exec(`INSERT INTO pursuits (slug, idea_id, person, created_at) VALUES ('b', 1, 'ann', ?)`, now); err != nil {
		t.Errorf("a pursuit with no appetite was refused: %v", err)
	}
	if rows, err := db.Query(`PRAGMA foreign_key_check`); err != nil {
		t.Fatal(err)
	} else {
		if rows.Next() {
			t.Error("foreign_key_check found a violation after the migration")
		}
		rows.Close()
	}
}

func TestMigrationFiveUpgradesVersionFourStore(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "physa.db"))
	if err != nil {
		t.Fatal(err)
	}
	ms, err := migrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms {
		if m.n > 4 {
			break
		}
		body, err := migrationFiles.ReadFile("migrations/" + m.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("PRAGMA user_version = 4"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO gather_runs VALUES (1, '2026-11-21T09:00:00Z', '2025-11-21T09:00:00Z', 10, 0, '', 'none')`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, want := version(t, s), newestMigration(t); got != want {
		t.Errorf("user_version = %d, want %d", got, want)
	}
	g, ok, err := s.Gathered(t.Context())
	if err != nil || !ok || g.Run.MaxPRs != 10 || g.Run.ForgeReason != "none" {
		t.Errorf("gather after upgrade = %+v, %v, %v; want it intact", g.Run, ok, err)
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name = 'init_proposal'").Scan(&n); err != nil || n != 0 {
		t.Errorf("init_proposal tables = %d, %v; want none after the upgrade", n, err)
	}
}
