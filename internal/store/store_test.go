package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
	if _, err := os.Stat(filepath.Join(dir, "physa", "physa.db")); err != nil {
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
	c := PRCheck{PR: 1, HeadSHA: "abc", CheckRunID: 7, CompletedAt: "t", Result: "{}"}
	if _, err := s.PutPRCheck(c); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, err := s2.PRChecks()
	if err != nil || !reflect.DeepEqual(got, []PRCheck{c}) {
		t.Errorf("rows after reopen = %v, %v", got, err)
	}
}

func TestNewerDatabaseFailsToOpen(t *testing.T) {
	s, dir := open(t)
	c := PRCheck{PR: 1, HeadSHA: "abc", CheckRunID: 7, CompletedAt: "t", Result: "{}"}
	if _, err := s.PutPRCheck(c); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("PRAGMA user_version = 999"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "newer physa") {
		t.Fatalf("Open error = %v, want one naming a newer physa", err)
	} else {
		t.Log("Open error:", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "physa", "physa.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM pr_checks").Scan(&n); err != nil || n != 1 {
		t.Errorf("rows = %d, %v; want 1 untouched", n, err)
	}
	t.Log("rows untouched:", n)
}

func TestPutIsIdempotent(t *testing.T) {
	s, _ := open(t)
	c := PRCheck{PR: 2, HeadSHA: "def", CheckRunID: 9, CompletedAt: "t1", Result: `{"a":1}`}
	for i, want := range []bool{true, false} {
		got, err := s.PutPRCheck(c)
		if err != nil || got != want {
			t.Fatalf("put %d = %v, %v; want %v", i, got, err, want)
		}
	}
	c.Result = `{"a":2}`
	if got, err := s.PutPRCheck(c); err != nil || !got {
		t.Errorf("put with new result = %v, %v; want true", got, err)
	}
	rows, _ := s.PRChecks()
	if len(rows) != 1 || rows[0].Result != `{"a":2}` {
		t.Errorf("rows = %v", rows)
	}
}

func TestHasAndList(t *testing.T) {
	s, _ := open(t)
	for _, c := range []PRCheck{
		{PR: 3, HeadSHA: "b", CheckRunID: 1, CompletedAt: "t", Result: "{}"},
		{PR: 3, HeadSHA: "a", CheckRunID: 2, CompletedAt: "t", Result: "{}"},
		{PR: 1, HeadSHA: "z", CheckRunID: 3, CompletedAt: "t", Result: "{}"},
	} {
		if _, err := s.PutPRCheck(c); err != nil {
			t.Fatal(err)
		}
	}
	if ok, err := s.HasPRCheck(3, "a"); err != nil || !ok {
		t.Errorf("Has(3,a) = %v, %v", ok, err)
	}
	if ok, err := s.HasPRCheck(3, "c"); err != nil || ok {
		t.Errorf("Has(3,c) = %v, %v", ok, err)
	}
	rows, err := s.PRChecks()
	if err != nil || len(rows) != 3 || rows[0].PR != 1 || rows[1].HeadSHA != "a" || rows[2].HeadSHA != "b" {
		t.Errorf("PRChecks = %v, %v", rows, err)
	}
}
