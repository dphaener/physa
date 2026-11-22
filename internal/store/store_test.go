package store

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, dir
}

func version(t *testing.T, s *Store) int {
	t.Helper()
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func newestMigration(t *testing.T) int {
	t.Helper()
	ms, err := migrations()
	if err != nil {
		t.Fatal(err)
	}
	return ms[len(ms)-1].n
}

func TestOpenCreatesFile(t *testing.T) {
	_, dir := open(t)
	if _, err := os.Stat(filepath.Join(dir, "physa.db")); err != nil {
		t.Fatal(err)
	}
}

func TestUserVersionIsNewestMigration(t *testing.T) {
	s, _ := open(t)
	ms, err := migrations()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := version(t, s), ms[len(ms)-1].n; got != want {
		t.Errorf("user_version = %d, want %d", got, want)
	}
}

func TestReopenKeepsRows(t *testing.T) {
	s, dir := open(t)
	if _, err := s.db.Exec("CREATE TABLE scratch (n INTEGER); INSERT INTO scratch VALUES (7)"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	var n int
	if err := s2.db.QueryRow("SELECT n FROM scratch").Scan(&n); err != nil || n != 7 {
		t.Errorf("row after reopen = %d, %v", n, err)
	}
}

func TestNewerDatabaseFailsToOpen(t *testing.T) {
	s, dir := open(t)
	if _, err := s.db.Exec("CREATE TABLE scratch (n INTEGER); INSERT INTO scratch VALUES (7)"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("PRAGMA user_version = 999"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "newer physa") {
		t.Fatalf("Open error = %v, want one naming a newer physa", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "physa.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM scratch").Scan(&n); err != nil || n != 1 {
		t.Errorf("rows = %d, %v; want 1 untouched", n, err)
	}
}

func TestMigrationTwoDropsPRChecks(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "physa.db"))
	if err != nil {
		t.Fatal(err)
	}
	body, err := migrationFiles.ReadFile("migrations/0001_pr_checks.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		string(body),
		"INSERT INTO pr_checks VALUES (1, 'abc', 7, 't', '{}')",
		"PRAGMA user_version = 1",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, want := version(t, s), newestMigration(t); got != want {
		t.Errorf("user_version = %d, want %d", got, want)
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name = 'pr_checks'").Scan(&n); err != nil || n != 0 {
		t.Errorf("pr_checks tables = %d, %v; want none", n, err)
	}
}

func TestFreshStoreMigratesToEight(t *testing.T) {
	s, _ := open(t)
	if got := version(t, s); got != 8 {
		t.Errorf("user_version = %d, want 8", got)
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name = 'slice_records'").Scan(&n); err != nil || n != 1 {
		t.Errorf("slice_records tables = %d, %v; want 1", n, err)
	}
}

func TestSliceRecordRoundTrip(t *testing.T) {
	s, _ := open(t)
	at := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	want := SliceRecord{At: at, Capabilities: []string{"learning", "map"}, With: "all", Entries: []string{"no-adr", "never-cut"}, Session: "sess-1"}
	if err := s.PutSliceRecord(want); err != nil {
		t.Fatal(err)
	}
	got, err := s.SliceRecords()
	if err != nil || len(got) != 1 {
		t.Fatalf("records = %v, %v", got, err)
	}
	g := got[0]
	if !g.At.Equal(want.At) || strings.Join(g.Capabilities, ",") != "learning,map" ||
		g.With != "all" || strings.Join(g.Entries, ",") != "no-adr,never-cut" || g.Session != "sess-1" {
		t.Errorf("got %+v, want %+v", g, want)
	}
}

func TestSliceRecordsNewestFirst(t *testing.T) {
	s, _ := open(t)
	t0 := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	for i, at := range []time.Time{t0, t0.Add(time.Hour), t0.Add(time.Hour)} {
		if err := s.PutSliceRecord(SliceRecord{At: at, Capabilities: []string{"c" + string(rune('a'+i))}}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.SliceRecords()
	if err != nil || len(got) != 3 {
		t.Fatalf("records = %v, %v", got, err)
	}
	var order []string
	for _, r := range got {
		order = append(order, r.Capabilities[0])
	}
	if strings.Join(order, ",") != "cc,cb,ca" {
		t.Errorf("order = %v, want cc,cb,ca", order)
	}
}

func TestSliceRecordNullEntriesAndSession(t *testing.T) {
	s, _ := open(t)
	if err := s.PutSliceRecord(SliceRecord{At: time.Now(), Capabilities: []string{"map"}}); err != nil {
		t.Fatal(err)
	}
	var entries, session sql.NullString
	if err := s.db.QueryRow("SELECT entries, session FROM slice_records").Scan(&entries, &session); err != nil {
		t.Fatal(err)
	}
	if entries.Valid || session.Valid {
		t.Errorf("entries %v, session %v; want both null", entries, session)
	}
	got, err := s.SliceRecords()
	if err != nil || got[0].Entries != nil || got[0].Session != "" {
		t.Errorf("read back %+v, %v", got, err)
	}
	b, err := json.Marshal(got[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"entries":null`) || !strings.Contains(string(b), `"session":null`) || strings.Contains(string(b), "budget") {
		t.Errorf("json = %s, want null entries and session, no budget", b)
	}
	b, _ = json.Marshal(SliceRecord{At: time.Now(), Entries: []string{"a", "b"}, Session: "x"})
	if !strings.Contains(string(b), `"entries":["a","b"]`) || !strings.Contains(string(b), `"session":"x"`) {
		t.Errorf("json = %s", b)
	}
}

func TestMigrationSevenKeepsOldSliceRecords(t *testing.T) {
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
		if m.n > 6 {
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
	for _, q := range []string{
		`INSERT INTO slice_records (at, capabilities, "with", budget, session) VALUES ('2026-11-01T12:00:00Z', '["map"]', 'all', 500, 's1')`,
		`INSERT INTO slice_records (at, capabilities, "with", budget, session) VALUES ('2026-11-02T12:00:00Z', '["init"]', '', NULL, NULL)`,
		"PRAGMA user_version = 6",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, want := version(t, s), newestMigration(t); got != want {
		t.Errorf("user_version = %d, want %d", got, want)
	}
	got, err := s.SliceRecords()
	if err != nil || len(got) != 2 {
		t.Fatalf("records = %v, %v; want 2 kept", got, err)
	}
	if got[0].Capabilities[0] != "init" || got[1].Capabilities[0] != "map" || got[1].With != "all" || got[1].Session != "s1" {
		t.Errorf("records = %+v", got)
	}
	for _, r := range got {
		if r.Entries != nil {
			t.Errorf("entries = %v, want none", r.Entries)
		}
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM pragma_table_info('slice_records') WHERE name = 'budget'").Scan(&n); err != nil || n != 0 {
		t.Errorf("budget columns = %d, %v; want 0", n, err)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM pragma_table_info('slice_records') WHERE name = 'entries'").Scan(&n); err != nil || n != 1 {
		t.Errorf("entries columns = %d, %v; want 1", n, err)
	}
}

func TestConcurrentOpenMigrates(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := Open(dir)
			if err != nil {
				errs <- err
				return
			}
			s.Close()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestEngineTables(t *testing.T) {
	s, _ := open(t)
	want := map[string][]string{
		"ideas":          {"id", "slug", "text", "person", "created_at", "updated_at"},
		"pitches":        {"id", "idea_id", "problem", "appetite_tokens", "solution", "scope", "signal", "rabbit_holes", "no_gos", "person", "created_at", "updated_at"},
		"bets":           {"id", "slug", "idea_id", "pitch_id", "person", "appetite_tokens", "spent_tokens", "state", "stop_reason", "created_at", "started_at", "ended_at"},
		"plans":          {"id", "bet_id", "created_at"},
		"plan_revisions": {"id", "plan_id", "n", "changes", "reason", "person", "created_at"},
		"events":         {"id", "at", "kind", "subject", "subject_id", "person", "data"},
		"intents":        {"id", "kind", "payload", "state", "attempts", "next_at", "last_error", "result", "created_at", "done_at"},
		"timers":         {"id", "kind", "key", "due_at", "payload"},
	}
	for table, cols := range want {
		rows, err := s.DB().Query("SELECT name FROM pragma_table_info(?)", table)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				t.Fatal(err)
			}
			got = append(got, c)
		}
		rows.Close()
		if strings.Join(got, ",") != strings.Join(cols, ",") {
			t.Errorf("%s columns = %v, want %v", table, got, cols)
		}
	}
}

func TestEngineTableChecks(t *testing.T) {
	s, _ := open(t)
	const now = "2026-11-01T12:00:00Z"
	if _, err := s.db.Exec(`INSERT INTO ideas (id, slug, text, person, created_at, updated_at) VALUES (1, 'a', 't', 'p', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	for name, q := range map[string]string{
		"paused bet":       `INSERT INTO bets (slug, idea_id, person, appetite_tokens, state, created_at) VALUES ('b', 1, 'p', 10, 'paused', '` + now + `')`,
		"zero appetite":    `INSERT INTO pitches (idea_id, appetite_tokens, person, created_at, updated_at) VALUES (1, 0, 'p', '` + now + `', '` + now + `')`,
		"bet without idea": `INSERT INTO bets (slug, idea_id, person, appetite_tokens, created_at) VALUES ('c', 99, 'p', 10, '` + now + `')`,
	} {
		if _, err := s.db.Exec(q); err == nil {
			t.Errorf("%s was accepted, want refused", name)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO bets (slug, idea_id, person, appetite_tokens, created_at) VALUES ('ok', 1, 'p', 10, ?)`, now); err != nil {
		t.Errorf("a valid bet was refused: %v", err)
	}
}
