package gather

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/vcs"
	"github.com/dphaener/physa/internal/vcs/git"
)

var base = time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)

type testRepo struct {
	t   *testing.T
	dir string
	n   int
}

func newTestRepo(t *testing.T) *testRepo {
	t.Helper()
	r := &testRepo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "main")
	return r
}

func (r *testRepo) git(args ...string) string {
	r.t.Helper()
	at := base.Add(time.Duration(r.n) * 24 * time.Hour).Format(time.RFC3339)
	cmd := exec.Command("git", append([]string{"-C", r.dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_AUTHOR_DATE="+at,
		"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_COMMITTER_DATE="+at)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit makes the next commit, a day after the last, touching files.
func (r *testRepo) commit(message string, files ...string) string {
	r.t.Helper()
	r.n++
	for _, f := range files {
		write(r.t, r.dir, f, fmt.Sprintf("%s %d", f, r.n))
	}
	r.git("add", "-A")
	r.git("commit", "-q", "--allow-empty", "-m", message)
	return r.git("rev-parse", "HEAD")
}

func (r *testRepo) open() vcs.Repo {
	r.t.Helper()
	v, err := git.Open(r.dir)
	if err != nil {
		r.t.Fatal(err)
	}
	return v
}

func (r *testRepo) at(days int) time.Time { return base.Add(time.Duration(days) * 24 * time.Hour) }

// plainForge implements forge.Forge only.
type plainForge struct{}

func (plainForge) Name() string                            { return "fake" }
func (plainForge) PRExists(int) (bool, error)              { return false, nil }
func (plainForge) ChangePR(string) (forge.PR, bool, error) { return forge.PR{}, false, nil }

// mergedForge also implements forge.Merged.
type mergedForge struct {
	plainForge
	prs  []forge.PR
	more bool
	err  error
}

func (m mergedForge) MergedPRs(time.Time, int) ([]forge.PR, bool, error) {
	return m.prs, m.more, m.err
}

func gatherNow(t *testing.T) time.Time { return base.Add(60 * 24 * time.Hour) }

func run(t *testing.T, r *testRepo, f forge.Forge, o Flags) (Summary, int) {
	t.Helper()
	g, s, err := Run(r.dir, r.open(), f, o, gatherNow(t))
	if err != nil {
		t.Fatal(err)
	}
	return s, len(g.Changes)
}

func flags(t *testing.T, args ...string) Flags {
	t.Helper()
	o, ok := ParseFlags(args, gatherNow(t))
	if !ok {
		t.Fatalf("flags %v", args)
	}
	return o
}

func TestRunHistory(t *testing.T) {
	r := newTestRepo(t)
	r.commit("feat: first\n\nSession: s1\nCapability: init", "a.go", "b.go")
	bad := r.commit("feat: second\n\nbody", "b.go")
	r.git("revert", "--no-edit", bad)
	r.n++
	write(t, r.dir, "AGENTS.md", "agents")
	g, s, err := Run(r.dir, r.open(), nil, flags(t, "--gather"), gatherNow(t))
	if err != nil {
		t.Fatal(err)
	}
	h := s.History
	if h.Changes != 3 || h.WithTrailers != 1 || h.Reverts != 1 || h.WithAltered != 3 || h.DistinctFiles != 2 {
		t.Errorf("history %+v", h)
	}
	var first, revert bool
	for _, c := range g.Changes {
		if c.Subject == "feat: first" {
			first = len(c.Trailers) == 2 && c.Trailers[0].Key == "Session" && c.Trailers[1].Value == "init" && len(c.Files) == 2
		}
		if strings.HasPrefix(c.Subject, "Revert") {
			revert = c.Reverts != "" && strings.HasPrefix(bad, c.Reverts)
		}
	}
	if !first || !revert {
		t.Errorf("first %v revert %v: %+v", first, revert, g.Changes)
	}
	if s.Docs.Count != 1 || s.Docs.Files[0].Path != "AGENTS.md" {
		t.Errorf("docs %+v", s.Docs)
	}
}

func TestRunSinceCutsOlderChanges(t *testing.T) {
	r := newTestRepo(t)
	r.commit("feat: old", "a")
	r.commit("feat: mid", "a")
	r.commit("feat: new", "a")
	// Changes are on days 1, 2 and 3 at 10:00; the date cuts day 1.
	o := flags(t, "--gather", "--since", r.at(2).Format("2006-01-02"))
	s, n := run(t, r, nil, o)
	if n != 2 || s.History.Changes != 2 {
		t.Errorf("changes %d", n)
	}
	if !s.Bound.FromFlag || s.Bound.Since != r.at(2).Format("2006-01-02") {
		t.Errorf("bound %+v", s.Bound)
	}
}

func TestRunUnavailableNoForge(t *testing.T) {
	r := newTestRepo(t)
	r.commit("feat: x", "a")
	s, _ := run(t, r, nil, flags(t, "--gather"))
	if s.PRs.Available || s.PRs.Reason != "no forge" {
		t.Errorf("%+v", s.PRs)
	}
}

func TestRunUnavailableWithoutMerged(t *testing.T) {
	r := newTestRepo(t)
	r.commit("feat: x", "a")
	s, _ := run(t, r, plainForge{}, flags(t, "--gather"))
	if s.PRs.Available || s.PRs.Reason == "" {
		t.Errorf("%+v", s.PRs)
	}
}

func TestRunUnavailableForgeError(t *testing.T) {
	r := newTestRepo(t)
	r.commit("feat: x", "a")
	f := mergedForge{err: &forge.Error{Class: forge.ErrUnavailable, Reason: "gh is not installed"}}
	g, s, err := Run(r.dir, r.open(), f, flags(t, "--gather"), gatherNow(t))
	if err != nil {
		t.Fatalf("an unavailable forge failed the run: %v", err)
	}
	if s.PRs.Available || s.PRs.Reason != "gh is not installed" || s.PRs.Count != 0 || len(g.PRs) != 0 {
		t.Errorf("%+v", s.PRs)
	}
	if g.Run.ForgeReason != "gh is not installed" || g.Run.Forge != "fake" {
		t.Errorf("%+v", g.Run)
	}
	var buf bytes.Buffer
	if err := WriteText(&buf, s); err != nil || !strings.Contains(buf.String(), "PRs: unavailable (gh is not installed)") {
		t.Errorf("%v\n%s", err, buf.String())
	}
	if s.History.Changes != 1 {
		t.Errorf("history still gathered: %+v", s.History)
	}
}

func TestRunOtherForgeErrorIsUnavailable(t *testing.T) {
	r := newTestRepo(t)
	r.commit("feat: x", "a")
	s, _ := run(t, r, mergedForge{err: errors.New("boom")}, flags(t, "--gather"))
	if s.PRs.Available || s.PRs.Reason != "boom" {
		t.Errorf("%+v", s.PRs)
	}
}

func TestRunMoreStartsHistoryAtOldestPR(t *testing.T) {
	r := newTestRepo(t)
	for i := 0; i < 5; i++ {
		r.commit(fmt.Sprintf("feat: c%d", i), "a")
	}
	// Changes on days 1..5. PRs merged on days 4 and 3 (newest first).
	f := mergedForge{more: true, prs: []forge.PR{
		{Number: 9, MergedAt: r.at(4)}, {Number: 8, MergedAt: r.at(3)},
	}}
	g, s, err := Run(r.dir, r.open(), f, flags(t, "--gather", "--max-prs", "2"), gatherNow(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Changes) != 3 || !s.Bound.PRLimitReached || !g.Run.PRsLimited {
		t.Errorf("changes %d bound %+v", len(g.Changes), s.Bound)
	}
	if s.Bound.HistorySince != r.at(3).Format("2006-01-02") {
		t.Errorf("history since %s", s.Bound.HistorySince)
	}
	var buf bytes.Buffer
	if err := WriteText(&buf, s); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "The PR limit was reached first; history starts at "+s.Bound.HistorySince+".") {
		t.Errorf("%s", buf.String())
	}
}

func TestRunPRCounts(t *testing.T) {
	r := newTestRepo(t)
	r.commit("feat: x", "a")
	f := mergedForge{prs: []forge.PR{
		{Number: 2, Title: "two", Body: "bb", MergedAt: r.at(1), Issues: []forge.Issue{{Number: 1, Title: "it", Body: "ib"}, {Number: 3, Title: "jt"}},
			Comments: []forge.Comment{
				{Author: "a", Kind: forge.KindReview, Body: "no", RequestsChanges: true},
				{Author: "b", Kind: forge.KindDiscussion, Body: "ok"},
			}},
		{Number: 1, Title: "one", MergedAt: r.at(0)},
	}}
	g, s, err := Run(r.dir, r.open(), f, flags(t, "--gather"), gatherNow(t))
	if err != nil {
		t.Fatal(err)
	}
	p := s.PRs
	if !p.Available || p.Count != 2 || p.ReviewComments != 2 || p.RequestingChanges != 1 || p.LinkedIssues != 2 {
		t.Errorf("%+v", p)
	}
	if s.Bound.PRLimitReached || len(g.PRs) != 2 || !g.PRs[0].Comments[0].RequestsChanges {
		t.Errorf("%+v", g.PRs)
	}
	if g.PRs[0].Issues[0] != "#1 it\n\nib" || g.PRs[0].Issues[1] != "#3 jt" {
		t.Errorf("issues %q", g.PRs[0].Issues)
	}
	wantBytes := len("two") + len("bb") + len("no") + len("ok") + len("#1 it\n\nib") + len("#3 jt") + len("one")
	if p.Bytes != wantBytes {
		t.Errorf("bytes %d, want %d", p.Bytes, wantBytes)
	}
}

func TestRunTokenEstimate(t *testing.T) {
	r := newTestRepo(t)
	r.commit("feat: x\n\nCapability: a", "f.go")
	write(t, r.dir, "README.md", strings.Repeat("r", 100))
	f := mergedForge{prs: []forge.PR{{Number: 1, Title: "t", Body: strings.Repeat("b", 50), MergedAt: r.at(1)}}}
	g, s, err := Run(r.dir, r.open(), f, flags(t, "--gather"), gatherNow(t))
	if err != nil {
		t.Fatal(err)
	}
	total := s.Docs.Bytes + s.History.Bytes + s.PRs.Bytes
	hist := len(g.Changes[0].Message) + len("f.go")
	if s.Docs.Bytes != 100 || s.History.Bytes != hist || s.PRs.Bytes != 51 {
		t.Errorf("bytes %+v", s)
	}
	if s.EstimatedTokens != total/4 || s.EstimatedTokens == 0 {
		t.Errorf("tokens %d, bytes %d", s.EstimatedTokens, total)
	}
}

func TestRunDocsSkippedAndHistoryFallback(t *testing.T) {
	r := newTestRepo(t)
	r.commit("feat: x", "a")
	write(t, r.dir, "README.md", strings.Repeat("x", MaxDocBytes+1))
	s, _ := run(t, r, nil, flags(t, "--gather"))
	if s.Docs.Count != 0 || len(s.Docs.Skipped) != 1 {
		t.Errorf("%+v", s.Docs)
	}
}

func fullSummary() Summary {
	return Summary{
		Version: 1, Store: "/s/physa.db",
		Bound: BoundSummary{Since: "2025-11-21", MaxPRs: 1000},
		Docs: DocsSummary{Count: 4, Bytes: 40, Skipped: []string{"BIG.md"}, Files: []DocFile{
			{"AGENTS.md", KindAgent}, {"docs/records/0001-a.md", KindDecisions}, {"docs/records/0002-b.md", KindDecisions}, {"decisions.md", KindDecisions},
		}},
		History:         HistorySummary{Changes: 10, WithTrailers: 4, Reverts: 1, WithAltered: 9, DistinctFiles: 30, Bytes: 80},
		PRs:             PRsSummary{Available: true, Count: 3, ReviewComments: 5, RequestingChanges: 2, LinkedIssues: 1, Bytes: 20},
		EstimatedTokens: 35,
	}
}

func TestWriteText(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteText(&buf, fullSummary()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	t.Log("\n" + out)
	for _, want := range []string{
		"Gathered into /s/physa.db, replacing the previous gather.\n",
		"Bound: since 2025-11-21 (the last 12 months), at most 1000 PRs.\n",
		"Docs: 4 files\n",
		"  AGENTS.md (agent)\n",
		"  decisions.md (decisions)\n",
		"  docs/records/ (2 files)\n",
		"  skipped BIG.md",
		"History: 10 changes, 4 with trailers, 1 reverts, 9 with altered files, 30 distinct files.\n",
		"PRs: 3, 5 review comments (2 requesting changes), 1 linked issues.\n",
		"About 35 tokens to read it all (an estimate: bytes / 4).\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, "0001-a.md") || strings.Contains(out, "limit was reached") {
		t.Errorf("unexpected: %s", out)
	}
	s := fullSummary()
	s.PRs = PRsSummary{Reason: "no forge"}
	s.Bound.FromFlag = true
	buf.Reset()
	if err := WriteText(&buf, s); err != nil || !strings.Contains(buf.String(), "PRs: unavailable (no forge)\n") || !strings.Contains(buf.String(), "(from --since)") {
		t.Errorf("%v\n%s", err, buf.String())
	}
}

func TestWriteJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, fullSummary()); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"version", "store", "bound", "docs", "history", "prs", "estimated_tokens"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing %s", k)
		}
	}
	if m["version"].(float64) != 1 || m["estimated_tokens"].(float64) != 35 {
		t.Errorf("%v", m)
	}
	if b := m["bound"].(map[string]any); b["max_prs"].(float64) != 1000 || b["since"] != "2025-11-21" {
		t.Errorf("%v", b)
	}
	if d := m["docs"].(map[string]any); d["skipped"].([]any)[0] != "BIG.md" || d["files"].([]any)[0].(map[string]any)["kind"] != "agent" {
		t.Errorf("%v", d)
	}
}
