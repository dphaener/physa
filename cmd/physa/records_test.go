package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/store"
)

// listed lists the slice records of the store of d.
func listed(t *testing.T, d deps) []store.SliceRecord {
	t.Helper()
	var out, errb bytes.Buffer
	if code := run(t.Context(), d, []string{"map", "records", "--json"}, &out, &errb); code != 0 {
		t.Fatalf("records code = %d; stderr %q", code, errb.String())
	}
	var recs []struct {
		At           string   `json:"at"`
		Capabilities []string `json:"capabilities"`
		With         string   `json:"with"`
		Entries      []string `json:"entries"`
		Session      *string  `json:"session"`
	}
	if err := json.Unmarshal(out.Bytes(), &recs); err != nil {
		t.Fatalf("records --json = %q: %v", out.String(), err)
	}
	var res []store.SliceRecord
	for _, r := range recs {
		sr := store.SliceRecord{Capabilities: r.Capabilities, With: r.With, Entries: r.Entries}
		if r.Session != nil {
			sr.Session = *r.Session
		}
		res = append(res, sr)
	}
	return res
}

func TestSliceRecordsEachPrintedSlice(t *testing.T) {
	t.Parallel()
	d := depsIn(t, ownRoot, "CLAUDE_CODE_SESSION_ID=s-1")
	var out, errb bytes.Buffer
	if code := run(t.Context(), d, []string{"map", "slice", "slices", "map", "--with", "all"}, &out, &errb); code != 0 {
		t.Fatalf("code = %d; stderr %q", code, errb.String())
	}
	d = inDir(t, d, ownRoot, "CLAUDE_CODE_SESSION_ID=") // the same store, outside a session
	if code := run(t.Context(), d, []string{"map", "slice", "map"}, &out, &errb); code != 0 {
		t.Fatalf("code = %d; stderr %q", code, errb.String())
	}
	recs := listed(t, d)
	if len(recs) != 2 {
		t.Fatalf("records = %+v, want 2", recs)
	}
	if r := recs[0]; strings.Join(r.Capabilities, ",") != "map" || r.With != "" || r.Entries != nil || r.Session != "" {
		t.Errorf("newest = %+v, want map, no with, no entries, no session", r)
	}
	if r := recs[1]; strings.Join(r.Capabilities, ",") != "slices,map" || r.With != "all" || r.Session != "s-1" {
		t.Errorf("oldest = %+v, want slices,map with all session s-1", r)
	}
	var js, e bytes.Buffer
	run(t.Context(), d, []string{"map", "records", "--json"}, &js, &e)
	if !strings.Contains(js.String(), `"session":null`) || !strings.Contains(js.String(), `"entries":null`) {
		t.Errorf("json = %q, want null session and entries", js.String())
	}
	var txt bytes.Buffer
	run(t.Context(), d, []string{"map", "records"}, &txt, &e)
	lines := strings.Split(strings.TrimSpace(txt.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], " — map") || !strings.HasSuffix(lines[1], "s-1 slices,map with all") {
		t.Errorf("records = %q", txt.String())
	}
}

func TestEntryReadRecordedWithItsSlugs(t *testing.T) {
	t.Parallel()
	d := depsIn(t, ownRoot, "CLAUDE_CODE_SESSION_ID=s-2")
	if code := run(t.Context(), d, []string{"map", "slice", "--entry", "no-adr", "github-first"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("code = %d", code)
	}
	recs := listed(t, d)
	if len(recs) != 1 {
		t.Fatalf("records = %+v, want 1", recs)
	}
	if r := recs[0]; strings.Join(r.Entries, ",") != "no-adr,github-first" || len(r.Capabilities) != 0 || r.With != "" || r.Session != "s-2" {
		t.Errorf("record = %+v, want entries no-adr,github-first with no capabilities", r)
	}
	var txt, e bytes.Buffer
	run(t.Context(), d, []string{"map", "records"}, &txt, &e)
	if !strings.Contains(txt.String(), "entries no-adr, github-first") {
		t.Errorf("records = %q, want the entries shown", txt.String())
	}
}

func TestSliceWithDecisionsAndQuestionsRecorded(t *testing.T) {
	t.Parallel()
	d := depsIn(t, ownRoot)
	for _, w := range []string{"decisions", "questions"} {
		if code := run(t.Context(), d, []string{"map", "slice", "map", "--with", w}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
			t.Fatalf("code = %d", code)
		}
	}
	recs := listed(t, d)
	if len(recs) != 2 || recs[0].With != "questions" || recs[1].With != "decisions" {
		t.Errorf("records = %+v", recs)
	}
}

func TestNoRecordForFailedSlices(t *testing.T) {
	t.Parallel()
	d := depsIn(t, ownRoot)
	for _, args := range [][]string{{"slice", "nope"}, {"slice"}, {"slice", "map", "--with", "bad"}, {"map", "slice", "--entry", "nope"}} {
		run(t.Context(), d, args, &bytes.Buffer{}, &bytes.Buffer{})
	}
	if recs := listed(t, d); len(recs) != 0 {
		t.Errorf("records = %+v, want none", recs)
	}
}

func TestFailingStoreStillPrintsSlice(t *testing.T) {
	t.Parallel()
	d := depsIn(t, ownRoot)
	var want bytes.Buffer
	run(t.Context(), d, []string{"map", "slice", "map"}, &want, &bytes.Buffer{})
	d.openStore = func(context.Context, string) (*store.Store, error) { return nil, errors.New("disk on fire") }
	var out, errb bytes.Buffer
	if code := run(t.Context(), d, []string{"map", "slice", "map"}, &out, &errb); code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if out.String() != want.String() {
		t.Errorf("stdout differs when the store fails")
	}
	if got := errb.String(); got != "physa map slice: not recorded: disk on fire\n" {
		t.Errorf("stderr = %q, want one warning line", got)
	}
}

func TestRecordsEmpty(t *testing.T) {
	t.Parallel()
	d := depsIn(t, ownRoot)
	var out bytes.Buffer
	if code := run(t.Context(), d, []string{"map", "records"}, &out, &bytes.Buffer{}); code != 0 || out.String() != "No slice records.\n" {
		t.Errorf("code %d, out %q", code, out.String())
	}
	out.Reset()
	if code := run(t.Context(), d, []string{"map", "records", "--json"}, &out, &bytes.Buffer{}); code != 0 || out.String() != "[]\n" {
		t.Errorf("code %d, out %q", code, out.String())
	}
}

func TestRecordsOutsideRepoAndBadUsage(t *testing.T) {
	t.Parallel()
	d := depsIn(t, t.TempDir())
	d.openStore = func(ctx context.Context, dir string) (*store.Store, error) {
		return gitStore(ctx, dir, d.proc().Environ())
	} // the real opener: a temp dir is no repo
	var out, errb bytes.Buffer
	if code := run(t.Context(), d, []string{"map", "records"}, &out, &errb); code != 2 || !strings.HasPrefix(errb.String(), "physa map records: ") {
		t.Errorf("code %d, stderr %q", code, errb.String())
	}
	errb.Reset()
	if code := run(t.Context(), d, []string{"map", "records", "--bad"}, &out, &errb); code != 2 || !strings.Contains(errb.String(), "physa map records [--json]") {
		t.Errorf("code %d, stderr %q", code, errb.String())
	}
}
