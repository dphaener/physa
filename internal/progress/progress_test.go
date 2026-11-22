package progress

import (
	"errors"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/vcs"
)

type fake struct {
	changes []vcs.Change
	noRef   bool
	since   time.Time
}

func (f *fake) DefaultBranch() (string, error) {
	if f.noRef {
		return "", errors.New("no default branch")
	}
	return "origin/main", nil
}

func (f *fake) Since(ref string, t time.Time) ([]vcs.Change, error) {
	f.since = t
	return f.changes, nil
}

var t0 = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func change(id string, hours int, msg string) vcs.Change {
	return vcs.Change{ID: id, Short: id[:2], Message: msg, Time: t0.Add(time.Duration(hours) * time.Hour)}
}

func TestLandedOldestKept(t *testing.T) {
	f := &fake{changes: []vcs.Change{ // newest first
		change("cc3333", 3, "fix: again\n\nCapability: ideas\nChange: my-bet/one\n"),
		change("bb2222", 2, "feat: two\n\nCapability: ideas\nChange: my-bet/two\n"),
		change("aa1111", 1, "feat: one\n\nCapability: ideas\nChange: my-bet/one\n"),
	}}
	got, err := Landed(f, "my-bet", t0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["one"].Commit != "aa1111" || got["two"].Commit != "bb2222" {
		t.Fatalf("got %+v", got)
	}
	if got["one"].Short != "aa" || !got["one"].At.Equal(t0.Add(time.Hour)) {
		t.Fatalf("landing %+v", got["one"])
	}
	if !f.since.Equal(t0.Add(-time.Minute)) {
		t.Fatalf("since = %v, want a minute of slack", f.since)
	}
}

func TestLandedIgnoresOtherBetAndOutsideBlock(t *testing.T) {
	f := &fake{changes: []vcs.Change{
		change("aa1111", 1, "feat: x\n\nChange: other-bet/one\n"),
		change("bb2222", 2, "feat: y\n\nChange: my-bet/one\n\nBody text after.\n"),
	}}
	got, _ := Landed(f, "my-bet", time.Time{})
	if len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestLandedMalformed(t *testing.T) {
	f := &fake{changes: []vcs.Change{
		change("aa1111", 1, "feat: x\n\nChange: my-bet\n"),
		change("bb2222", 2, "feat: y\n\nChange: my-bet/a/b\n"),
		change("cc3333", 3, "feat: z\n\nChange: My-Bet/One\n"),
	}}
	got, err := Landed(f, "my-bet", time.Time{})
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestLandedNoDefaultBranch(t *testing.T) {
	got, err := Landed(&fake{noRef: true}, "my-bet", t0)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestParse(t *testing.T) {
	if b, c, ok := Parse("my-bet/retry-queue"); !ok || b != "my-bet" || c != "retry-queue" {
		t.Fatal(b, c, ok)
	}
	for _, bad := range []string{"", "a", "a/", "/a", "a/b/c", "A/b", "a b/c"} {
		if _, _, ok := Parse(bad); ok {
			t.Errorf("Parse(%q) ok", bad)
		}
	}
}
