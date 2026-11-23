package evidence

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/vcs"
	"github.com/dphaener/physa/internal/vcs/git"
)

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir,
		"-c", "user.name=T", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newRepo(t *testing.T) (vcs.Repo, string) {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	r, err := git.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return r, dir
}

func commit(t *testing.T, dir, message string) string {
	t.Helper()
	n++
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d", n)), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", message)
	return run(t, dir, "rev-parse", "HEAD")
}

var n int

var now = time.Now().UTC().Truncate(time.Second)

type fakeForge struct {
	prs  []forge.PR
	err  error
	from time.Time
}

func (f *fakeForge) MergedPRs(since time.Time, max int) ([]forge.PR, bool, error) {
	f.from = since
	return f.prs, false, f.err
}

func kinds(facts []Fact) []string {
	var k []string
	for _, f := range facts {
		k = append(k, f.Kind)
	}
	return k
}

func TestLandedAndReverted(t *testing.T) {
	r, dir := newRepo(t)
	one := commit(t, dir, "feat: One\n\nCapability: ideas\nChange: my-pursuit/one\n")
	two := commit(t, dir, "feat: Two\n\nCapability: map\nChange: my-pursuit/two\n")
	run(t, dir, "revert", "--no-edit", two)
	revert := run(t, dir, "rev-parse", "HEAD")

	facts, next, ferr, err := Observe(r, nil, Watermark{}, now)
	if err != nil || ferr != nil {
		t.Fatal(err, ferr)
	}
	if got := strings.Join(kinds(facts), ","); got != "landed,landed,landed,reverted" {
		t.Fatalf("kinds = %s", got)
	}
	if f := facts[0]; f.Commit != one || f.Capability != "ideas" || f.Pursuit != "my-pursuit" || f.Change != "one" || f.Source != SourceVCS {
		t.Fatalf("first = %+v", f)
	}
	rv := facts[3]
	if rv.Commit != two || rv.ByCommit != revert || rv.Capability != "map" || rv.Pursuit != "my-pursuit" || rv.Change != "two" {
		t.Fatalf("reverted = %+v", rv)
	}
	if next.Commit != revert || next.Time.IsZero() {
		t.Fatalf("next = %+v", next)
	}

	again, next2, _, err := Observe(r, nil, next, now)
	if err != nil || len(again) != 0 || next2.Commit != revert {
		t.Fatalf("again = %+v %+v %v", again, next2, err)
	}
	three := commit(t, dir, "fix: Three")
	more, _, _, _ := Observe(r, nil, next, now)
	if len(more) != 1 || more[0].Commit != three || more[0].Capability != "" {
		t.Fatalf("more = %+v", more)
	}
}

func TestRewrittenBranchFallsBackToSince(t *testing.T) {
	r, dir := newRepo(t)
	commit(t, dir, "feat: One")
	old := commit(t, dir, "feat: Two")
	_, next, _, _ := Observe(r, nil, Watermark{}, now)
	run(t, dir, "reset", "-q", "--hard", "HEAD~1")
	run(t, dir, "reflog", "expire", "--expire=now", "--all")
	run(t, dir, "gc", "-q", "--prune=now")
	fresh := commit(t, dir, "feat: Replacement")
	next.Commit = old
	facts, got, _, err := Observe(r, nil, next, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) == 0 || facts[len(facts)-1].Commit != fresh || got.Commit != fresh {
		t.Fatalf("facts = %+v next = %+v", facts, got)
	}
}

func TestNoDefaultBranch(t *testing.T) {
	r, _ := newRepo(t)
	facts, next, ferr, err := Observe(r, &fakeForge{}, Watermark{}, now)
	if len(facts) != 0 || ferr != nil || err != nil || next.Commit != "" {
		t.Fatalf("%+v %+v %v %v", facts, next, ferr, err)
	}
}

func pr(n int, opened, merged time.Time, commit string, checks ...string) forge.PR {
	p := forge.PR{Number: n, CreatedAt: opened, MergedAt: merged, MergeCommit: commit}
	for _, c := range checks {
		p.Commits = append(p.Commits, forge.Commit{ID: "x", Checks: c})
	}
	return p
}

func TestForgeLeadTimeAndCI(t *testing.T) {
	r, dir := newRepo(t)
	sha := commit(t, dir, "feat: One\n\nCapability: ideas\nChange: my-pursuit/one\n")
	ff := &fakeForge{prs: []forge.PR{ // newest merge first
		pr(4, now.Add(-time.Hour), now, "", "", "PENDING"),
		pr(3, now.Add(-3*time.Hour), now.Add(-time.Hour), sha, "FAILURE", "SUCCESS"),
		pr(2, now.Add(-4*time.Hour), now.Add(-2*time.Hour), "", "SUCCESS"),
		pr(1, now.Add(-5*time.Hour), now.Add(-3*time.Hour), "", "ERROR"),
	}}
	facts, next, ferr, err := Observe(r, ff, Watermark{}, now)
	if err != nil || ferr != nil {
		t.Fatal(err, ferr)
	}
	var forged []Fact
	for _, f := range facts {
		if f.Source == SourceForge {
			forged = append(forged, f)
		}
	}
	if got := strings.Join(kinds(forged), ","); got != "lead_time,ci_failed,lead_time,ci_passed,lead_time,ci_passed_after_failing,lead_time" {
		t.Fatalf("kinds = %s", got)
	}
	lead := forged[4]
	if lead.PR != 3 || *lead.Seconds != 7200 || lead.Commit != sha || lead.Pursuit != "my-pursuit" || !lead.At.Equal(now.Add(-time.Hour)) {
		t.Fatalf("lead = %+v", lead)
	}
	if !next.Forge.Equal(now) {
		t.Fatalf("forge mark = %v", next.Forge)
	}
	if !ff.from.Equal(now.Add(-window)) {
		t.Fatalf("first look from %v", ff.from)
	}
	again, _, _, _ := Observe(r, ff, next, now)
	if len(again) != 0 || !ff.from.Equal(now) {
		t.Fatalf("again = %+v from %v", again, ff.from)
	}
}

func TestForgeErrorKeepsVCSFacts(t *testing.T) {
	r, dir := newRepo(t)
	commit(t, dir, "feat: One")
	mark := now.Add(-time.Hour)
	ff := &fakeForge{err: forge.ErrUnauthenticated}
	facts, next, ferr, err := Observe(r, ff, Watermark{Forge: mark}, now)
	if err != nil || !errors.Is(ferr, forge.ErrUnauthenticated) {
		t.Fatalf("err = %v, forgeErr = %v", err, ferr)
	}
	if len(facts) != 1 || facts[0].Kind != KindLanded || !next.Forge.Equal(mark) || next.Commit == "" {
		t.Fatalf("facts = %+v next = %+v", facts, next)
	}
}

func TestMissingWatermarkCommitFallsBack(t *testing.T) {
	r, dir := newRepo(t)
	commit(t, dir, "feat: One")
	_, _, _, err := Observe(r, nil, Watermark{Commit: "nothere"}, now)
	if err != nil {
		t.Fatalf("a missing watermark commit falls back, got %v", err)
	}
}
