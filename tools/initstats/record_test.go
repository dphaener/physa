package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/gather"
	"github.com/dphaener/physa/internal/recovering"
	"github.com/dphaener/physa/internal/recovery"
	"github.com/dphaener/physa/internal/recoveryreport"
)

// canary stands in for everything that must stay in the repo: names, paths,
// promises, questions, map text, errors and session ids.
const canary = "Acme Secret/payments"

// keep writes init's three outputs into a kept run's folder, every string field
// set to the canary.
func keep(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, b []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m, _ := json.Marshal(meta{At: "2026-11-24T15:00:00Z", Physa: "physa " + canary, Clone: canary,
		Phases: map[string]Phase{"gather": {Seconds: 12}, "proposal": {Seconds: 340}, "recovery": {Seconds: 2100, Exit: 1}}})
	write("meta.json", m)

	tok := adapter.Tokens{Input: 1000, CachedInput: 400, Output: 50}
	s := gather.Summary{Version: 1, Store: canary, EstimatedTokens: 470000,
		Bound:     gather.BoundSummary{Since: canary, HistorySince: "2026-01-02" + canary, MaxPRs: 50, PRLimitReached: true},
		Docs:      gather.DocsSummary{Count: 3, Bytes: 900, Files: []gather.DocFile{{Path: canary, Kind: canary, Reason: canary}}, Skipped: []gather.Skip{{Path: canary, Why: canary}}},
		History:   gather.HistorySummary{Changes: 800, DistinctFiles: 4000, Shallow: true},
		PRs:       gather.PRsSummary{Available: true, Reason: canary, Count: 200},
		Discovery: gather.Discovery{Started: true, Why: canary, Named: 4, Added: 2, Model: canary, Tokens: tok}}
	var b bytes.Buffer
	if err := gather.WriteJSON(&b, s); err != nil {
		t.Fatal(err)
	}
	write("gather.json", b.Bytes())

	q := map[string]any{"question": canary, "suggested_by": []string{canary}}
	write("proposal.json", mustJSON(t, map[string]any{
		"version": 3,
		"product": map[string]any{"promise": canary},
		"capabilities": []any{
			map[string]any{"name": canary, "promise": canary, "shown_by": []string{canary}, "docs": []string{canary}, "questions": []any{q}, "unshown": true},
			map[string]any{"name": canary, "promise": canary, "shown_by": []string{}, "docs": []string{}, "questions": []any{}, "unshown": false},
		},
		"questions": []any{q},
		"dropped":   []any{map[string]any{"what": canary, "capability": canary, "value": canary, "reason": canary}},
		"malformed": []any{map[string]any{"raw": canary, "reason": canary}},
		"read": map[string]any{"prompt_tokens": 100000, "source_tokens": 470000, "lines": []string{canary},
			"sources": []any{map[string]any{"name": "docs", "items": 3, "tokens": 900}, map[string]any{"name": canary, "items": 1, "tokens": 5}}},
		"session": map[string]any{"id": canary, "model": "claude-opus-5-5", "tokens": tok},
	}))

	b.Reset()
	r := recovery.Report{Used: tok, Capabilities: []recovery.Outcome{
		{Name: canary, Status: recovery.StatusRecovered, File: canary, Terms: canary, Attempts: 2, SessionID: canary, Model: "claude-opus-5-5", Tokens: tok,
			Counts:  recovering.Counts{Rules: 5, Decisions: 2, Questions: 1, Terms: 3},
			Dropped: []recovering.Dropped{{Kind: canary, Slug: canary, Heading: canary, Reasons: []string{canary}}}},
		{Name: canary, Status: recovery.StatusFailed, Problems: []string{canary}, Error: canary, Model: canary},
	}}
	if err := recoveryreport.WriteJSON(&b, r); err != nil {
		t.Fatal(err)
	}
	write("recovery.json", b.Bytes())
	return dir
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRecordHoldsNothingFromTheRepo(t *testing.T) {
	rec, err := build(keep(t), "repo-a")
	if err != nil {
		t.Fatal(err)
	}
	out := string(mustJSON(t, rec))
	for _, part := range []string{"Acme", "Secret", "payments"} {
		if strings.Contains(out, part) {
			t.Fatalf("record holds %q:\n%s", part, out)
		}
	}
}

func TestRecordKeepsTheNumbers(t *testing.T) {
	rec, err := build(keep(t), "repo-a")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Date != "2026-11-24" || rec.Physa != "other" || rec.Phases["recovery"].Seconds != 2100 || rec.Phases["recovery"].Exit != 1 {
		t.Fatalf("meta %+v", rec)
	}
	if g := rec.Gather; g == nil || g.EstimatedTokens != 470000 || g.Docs.Count != 3 || g.Docs.Skipped != 1 || g.History.Changes != 800 || !g.History.Shallow || g.Discovery.Model != "other" || g.Discovery.Tokens.Input != 1000 ||
		g.Bound.Since != "" || g.Bound.HistorySince != "2026-01-02" || g.Bound.MaxPRs != 50 || !g.Bound.PRLimitReached {
		t.Fatalf("gather %+v", rec.Gather)
	}
	p := rec.Proposal
	if p == nil || p.Capabilities != 2 || p.Unshown != 1 || p.Questions != 2 || p.Dropped != 1 || p.Malformed != 1 || p.SourceTokens != 470000 || p.Model != "claude-opus-5-5" {
		t.Fatalf("proposal %+v", p)
	}
	if p.Sources[0].Name != "docs" || p.Sources[1].Name != "other" {
		t.Fatalf("sources %+v", p.Sources)
	}
	r := rec.Recovery
	if r == nil || len(r.Capabilities) != 2 || r.Statuses["recovered"] != 1 || r.Statuses["failed"] != 1 {
		t.Fatalf("recovery %+v", r)
	}
	if c := r.Capabilities[0]; c.Attempts != 2 || c.Counts.Rules != 5 || c.Dropped != 1 || c.Errored {
		t.Fatalf("first %+v", c)
	}
	if c := r.Capabilities[1]; c.Problems != 1 || !c.Errored || c.Model != "other" {
		t.Fatalf("second %+v", c)
	}
}

func TestBuildReadsAPartialRun(t *testing.T) {
	dir := keep(t)
	os.Remove(filepath.Join(dir, "recovery.json"))
	rec, err := build(dir, "repo-a")
	if err != nil || rec.Recovery != nil || rec.Proposal == nil {
		t.Fatalf("rec %+v err %v", rec, err)
	}
}

func TestShapeCountsExtensionsNotNames(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"Acme-Secret.go", "pay/ments.go", "README", "x.weird_extension_name", "y.RB"} {
		p := filepath.Join(dir, f)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte("12345"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored/\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "ignored"), 0o755)
	os.WriteFile(filepath.Join(dir, "ignored", "big.go"), []byte("x"), 0o644)
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	r, err := shape(filepath.Join(dir, "pay"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Files != 6 || r.Bytes != 25+9 {
		t.Fatalf("shape %+v", r)
	}
	got := map[string]int{}
	for _, e := range r.Extensions {
		got[e.Ext] = e.Files
	}
	if got[".go"] != 2 || got[".rb"] != 1 || got["none"] != 3 {
		t.Fatalf("extensions %+v", got)
	}
	if s := string(mustJSON(t, r)); strings.Contains(s, "Acme") || strings.Contains(s, "ments") {
		t.Fatalf("shape names a file: %s", s)
	}
}

func TestCloneIsTheSourcesCommit(t *testing.T) {
	if c := clone(); !commitPattern.MatchString(c) {
		t.Fatalf("clone %q", c)
	}
}

func TestWarnSaysWhatTheGatherLacked(t *testing.T) {
	dir := keep(t)
	os.WriteFile(filepath.Join(dir, "gather.json"), mustJSON(t, gather.Summary{
		History: gather.HistorySummary{Shallow: true}, PRs: gather.PRsSummary{Available: false, Reason: "no forge"}}), 0o644)
	rec, err := build(dir, "repo-a")
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	warn(&b, dir, rec)
	if !strings.Contains(b.String(), "shallow") || !strings.Contains(b.String(), "no PRs were gathered (no forge)") {
		t.Fatalf("warnings:\n%s", b.String())
	}
	b.Reset()
	if rec, err = build(keep(t), "repo-a"); err != nil {
		t.Fatal(err)
	}
	rec.Gather.History.Shallow = false
	warn(&b, "", rec)
	if b.Len() != 0 {
		t.Fatalf("a full gather warned:\n%s", b.String())
	}
}
