package store

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	if got := version(t, s); got != 3 {
		t.Errorf("user_version = %d, want 3", got)
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name = 'pr_checks'").Scan(&n); err != nil || n != 0 {
		t.Errorf("pr_checks tables = %d, %v; want none", n, err)
	}
}

func TestFreshStoreMigratesToThree(t *testing.T) {
	s, _ := open(t)
	if got := version(t, s); got != 3 {
		t.Errorf("user_version = %d, want 3", got)
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name = 'slice_records'").Scan(&n); err != nil || n != 1 {
		t.Errorf("slice_records tables = %d, %v; want 1", n, err)
	}
}

func TestSliceRecordRoundTrip(t *testing.T) {
	s, _ := open(t)
	at := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	want := SliceRecord{At: at, Capabilities: []string{"learning", "map"}, With: "all", Budget: 500, Session: "sess-1"}
	if err := s.PutSliceRecord(want); err != nil {
		t.Fatal(err)
	}
	got, err := s.SliceRecords()
	if err != nil || len(got) != 1 {
		t.Fatalf("records = %v, %v", got, err)
	}
	g := got[0]
	if !g.At.Equal(want.At) || strings.Join(g.Capabilities, ",") != "learning,map" ||
		g.With != "all" || g.Budget != 500 || g.Session != "sess-1" {
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

func TestSliceRecordNullBudgetAndSession(t *testing.T) {
	s, _ := open(t)
	if err := s.PutSliceRecord(SliceRecord{At: time.Now(), Capabilities: []string{"map"}}); err != nil {
		t.Fatal(err)
	}
	var budget, session sql.NullString
	if err := s.db.QueryRow("SELECT budget, session FROM slice_records").Scan(&budget, &session); err != nil {
		t.Fatal(err)
	}
	if budget.Valid || session.Valid {
		t.Errorf("budget %v, session %v; want both null", budget, session)
	}
	got, err := s.SliceRecords()
	if err != nil || got[0].Budget != 0 || got[0].Session != "" {
		t.Errorf("read back %+v, %v", got, err)
	}
	b, err := json.Marshal(got[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"budget":null`) || !strings.Contains(string(b), `"session":null`) {
		t.Errorf("json = %s, want null budget and session", b)
	}
	b, _ = json.Marshal(SliceRecord{At: time.Now(), Budget: 9, Session: "x"})
	if !strings.Contains(string(b), `"budget":9`) || !strings.Contains(string(b), `"session":"x"`) {
		t.Errorf("json = %s", b)
	}
}
