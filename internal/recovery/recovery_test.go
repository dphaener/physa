package recovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/mapfolder"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/pathlint"
	"github.com/dphaener/physa/internal/place"
	"github.com/dphaener/physa/internal/reading"
	"github.com/dphaener/physa/internal/recovering"
	"github.com/dphaener/physa/internal/recoverinput"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
	"github.com/dphaener/physa/internal/walk"
)

var (
	gatherAt   = time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	proposedAt = time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	runAt      = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
)

const (
	idA = "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
	idB = "b1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
	idC = "c1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
)

// step is one scripted attempt of a session: what it answers, what it
// reports having used, and how it ends. Zero tokens mean the prompt's
// estimate in and 500 out. An attempt waits for wait to close, when set.
type step struct {
	text    string
	tokens  adapter.Tokens
	reports []adapter.Tokens
	err     error
	wait    chan struct{}
}

// fakeAdapter plays a script per capability and attempt. It knows a first
// attempt's capability by the name in its prompt and a resumed one's by its
// session id, records every Spec, and counts the sessions running at once.
type fakeAdapter struct {
	mu      sync.Mutex
	scripts map[string][]step
	byID    map[string]string
	specs   map[string][]adapter.Spec
	order   []string
	running int
	most    int
	started chan struct{} // receives once per attempt started, when set
	gate    chan struct{} // attempts wait for it to close, when set
	onStart func(name string, attempt int)
	// noResume makes every resumed attempt fail with adapter.ErrNoResume
	// before it runs, as an agent that cannot resume does.
	noResume bool
}

func newFake(scripts map[string][]step) *fakeAdapter {
	return &fakeAdapter{scripts: scripts, byID: map[string]string{}, specs: map[string][]adapter.Spec{}}
}

func (f *fakeAdapter) Name() string { return "fake" }

func (f *fakeAdapter) Start(_ context.Context, s adapter.Spec) (adapter.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s.Resume && f.noResume {
		return nil, fmt.Errorf("fake: %w", adapter.ErrNoResume)
	}
	name := f.byID[s.SessionID]
	if !s.Resume {
		for n := range f.scripts {
			if strings.Contains(s.Prompt, "name: "+n+"\n") {
				name = n
			}
		}
		f.byID[s.SessionID] = name
	}
	if name == "" {
		return nil, errors.New("fake: no script for this session")
	}
	n := len(f.specs[name])
	f.specs[name] = append(f.specs[name], s)
	f.order = append(f.order, name)
	if n >= len(f.scripts[name]) {
		return nil, errors.New("fake: the script of " + name + " has run out")
	}
	f.running++
	f.most = max(f.most, f.running)
	if f.onStart != nil {
		f.onStart(name, n+1)
	}
	return &fakeSession{f: f, spec: s, step: f.scripts[name][n]}, nil
}

func (f *fakeAdapter) starts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.order)
}

type fakeSession struct {
	f    *fakeAdapter
	spec adapter.Spec
	step step
}

func (s *fakeSession) Wait() (adapter.Result, error) {
	if s.f.started != nil {
		s.f.started <- struct{}{}
	}
	if s.f.gate != nil {
		<-s.f.gate
	}
	if s.step.wait != nil {
		<-s.step.wait
	}
	defer func() {
		s.f.mu.Lock()
		s.f.running--
		s.f.mu.Unlock()
	}()
	for _, t := range s.step.reports {
		s.spec.Usage(t)
	}
	t := s.step.tokens
	if t == (adapter.Tokens{}) && len(s.step.reports) == 0 {
		t = adapter.Tokens{Input: int64(reading.Tokens(s.spec.Prompt)), Output: 500}
	}
	return adapter.Result{Text: s.step.text, Model: "fake-1", SessionID: s.spec.SessionID, Tokens: t}, s.step.err
}

// fakeStore holds a gather, a proposal, a walk with its placement and a
// seed, each nil when not stored, and what was put in it.
type fakeStore struct {
	gather   store.Gather
	proposal *initrecord.Proposal
	walk     *initrecord.Walk
	place    *initrecord.Place
	seed     *uint64
	seeds    []uint64
	puts     [][]initrecord.Recovered
	rows     []initrecord.Recovered // what init's record already holds
}

func (s *fakeStore) Recovered() ([]initrecord.Recovered, error) { return s.rows, nil }

func (s *fakeStore) Walk() (initrecord.Walk, bool, error) {
	if s.walk == nil {
		return initrecord.Walk{}, false, nil
	}
	return *s.walk, true, nil
}

func (s *fakeStore) Place() (initrecord.Place, bool, error) {
	if s.place == nil {
		return initrecord.Place{}, false, nil
	}
	return *s.place, true, nil
}

func (s *fakeStore) RecoverSeed() (uint64, bool, error) {
	if s.seed == nil {
		return 0, false, nil
	}
	return *s.seed, true, nil
}

func (s *fakeStore) PutRecoverSeed(seed uint64, _ time.Time) error {
	s.seed, s.seeds = &seed, append(s.seeds, seed)
	return nil
}

func (s *fakeStore) Gathered() (store.Gather, bool, error) { return s.gather, true, nil }

func (s *fakeStore) Proposed() (initrecord.Proposal, bool, error) {
	if s.proposal == nil {
		return initrecord.Proposal{}, false, nil
	}
	return *s.proposal, true, nil
}

func (s *fakeStore) PutRecovered(rs []initrecord.Recovered) error {
	s.puts = append(s.puts, rs)
	return nil
}

func testGather() store.Gather {
	return store.Gather{
		Run:  store.GatherRun{At: gatherAt},
		Docs: []store.Doc{{Path: "README.md", Kind: "readme", Body: "# Notes\n\nPeople keep their notes.\n"}},
		Changes: []store.GatheredChange{
			{ID: idA, Short: "a1b2c3d", Subject: "feat(export): Export every note", Message: "feat(export): Export every note\n\nDeleted notes go too.\n", Author: "dana", At: gatherAt.Add(-3 * time.Hour)},
			{ID: idB, Short: "b1b2c3d", Subject: "Revert \"feat(export): Export every note\"", Author: "sam", At: gatherAt.Add(-2 * time.Hour), Reverts: idA},
			{ID: idC, Short: "c1b2c3d", Subject: "feat(search): Rank by date", Author: "dana", At: gatherAt.Add(-time.Hour)},
		},
		PRs: []store.GatheredPR{
			{Number: 12, Title: "Export every note", Author: "dana", Body: "Exports hold every note.", MergedAt: gatherAt.Add(-3 * time.Hour),
				Comments: []store.GatheredComment{{Author: "sam", At: gatherAt.Add(-4 * time.Hour), RequestsChanges: true, Body: "An export must hold deleted notes too."}}},
			{Number: 7, Title: "Search by word", Author: "sam", Body: "Find a note by any word in it.", MergedAt: gatherAt.Add(-5 * time.Hour)},
		},
	}
}

// names are the proposed capabilities; each is shown by #12 but search,
// which #7 shows.
var names = []string{"export", "search", "sharing", "sync", "tags"}

func testStore(t *testing.T) *fakeStore {
	t.Helper()
	var caps []mapping.Capability
	for _, n := range names {
		c := mapping.Capability{Name: n, Promise: "People can rely on " + n + ".", ShownBy: []string{"#12"}}
		if n == "search" {
			c.ShownBy = []string{"#7"}
		}
		caps = append(caps, c)
	}
	body, err := json.Marshal(mapping.Stored{Proposal: mapping.Proposal{
		Product: mapping.Product{Promise: "People keep their notes and find them again."}, Capabilities: caps}})
	if err != nil {
		t.Fatal(err)
	}
	return &fakeStore{gather: testGather(), proposal: &initrecord.Proposal{GatherAt: gatherAt, At: proposedAt, Body: body}}
}

// reply wraps a capability text and terms as a session's answer.
func reply(file, terms string) string {
	return "The sources show this.\n\n````markdown\n" + file + "````\n\n```glossary\n" + terms + "```\n"
}

// rule is a rule that checks clean, anchored to a gathered PR.
func rule(slug string) string {
	return "### The rule " + slug + "\n`" + slug + "` · #12\nAn export holds every note.\nBecause: a reviewer asked for it in #12.\n"
}

// bare is a rule with no Because: line, which the check fails.
func bare(slug string) string {
	return "### The rule " + slug + "\n`" + slug + "` · #12\nAn export holds every note.\n"
}

func good(slugs ...string) string {
	file := "# Notes\n\n## Rules\n"
	for _, s := range slugs {
		file += "\n" + rule(s)
	}
	return file
}

// harness is one run's parts.
type harness struct {
	store    *fakeStore
	agent    *fakeAdapter
	session  Session
	progress *bytes.Buffer
	resolved int
	root     string
}

func newHarness(t *testing.T, scripts map[string][]step) *harness {
	t.Helper()
	h := &harness{store: testStore(t), agent: newFake(scripts), progress: &bytes.Buffer{}, root: t.TempDir()}
	if err := os.WriteFile(filepath.Join(h.root, "README.md"), []byte("# Notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.session = Session{
		Agent:    func() (adapter.Adapter, error) { h.resolved++; return h.agent, nil },
		Root:     h.root,
		PRExists: func(n int) (bool, error) { return n == 12 || n == 7, nil },
		Progress: h.progress,
		Now:      func() time.Time { return runAt },
	}
	return h
}

func (h *harness) run(t *testing.T) Report {
	t.Helper()
	rep, err := Run(context.Background(), h.store, h.session)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

// rootFiles lists everything under the repo root, to show a run left it be.
func (h *harness) rootFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(h.root, func(p string, _ os.DirEntry, err error) error {
		out = append(out, p)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func statuses(rep Report) []string {
	var out []string
	for _, o := range rep.Capabilities {
		out = append(out, o.Name+": "+o.Status)
	}
	return out
}

func used(t adapter.Tokens) int64 { return t.Input + t.Output }

func TestHappyPathStoresBothCapabilities(t *testing.T) {
	h := newHarness(t, map[string][]step{
		"export": {{text: reply(good("export-deleted"), "**Export**: A copy of every note, in a form other tools read.\n")}},
		"search": {{text: reply("# Search\n\n## Questions\n\n### Does search read deleted notes?\n`search-deleted` · raised by physa · 2026-11-21 · #7\nNothing in #7 says.\n", "")}},
	})
	h.session.Only = []string{"export", "search"}
	before := h.rootFiles(t)
	rep := h.run(t)

	if got := statuses(rep); !reflect.DeepEqual(got, []string{"export: recovered", "search: recovered"}) {
		t.Fatalf("statuses %v", got)
	}
	ex, se := rep.Capabilities[0], rep.Capabilities[1]
	if ex.Counts != (recovering.Counts{Rules: 1, Terms: 1}) || se.Counts != (recovering.Counts{Questions: 1}) {
		t.Errorf("counts %+v and %+v", ex.Counts, se.Counts)
	}
	if !strings.HasPrefix(ex.File, "---\npromise: People can rely on export.\n---\n\n# Notes\n") || !strings.Contains(ex.Terms, "**Export**") {
		t.Errorf("file %q, terms %q", ex.File, ex.Terms)
	}
	if ex.Attempts != 1 || ex.Model != "fake-1" || ex.SessionID == "" || ex.SessionID == se.SessionID {
		t.Errorf("export outcome %+v", ex)
	}
	if used(rep.Used) != used(ex.Tokens)+used(se.Tokens) || used(rep.Used) == 0 {
		t.Errorf("used %v", rep.Used)
	}

	spec := h.agent.specs["export"][0]
	if !spec.ReadOnly || spec.PromptOnly || spec.Resume || spec.SessionID != ex.SessionID || spec.Dir != h.root {
		t.Errorf("spec %+v", spec)
	}
	if rep.From != "proposal" || !strings.Contains(h.progress.String(), "at most 3 at once, from the proposal\n") || len(h.store.seeds) != 0 {
		t.Errorf("from %q, seeds %v, progress:\n%s", rep.From, h.store.seeds, h.progress)
	}
	if !strings.Contains(spec.Prompt, "#12, merged") || strings.Contains(spec.Prompt, "#7, merged") || !strings.Contains(spec.System, "Today's date: 2026-11-21") {
		t.Error("export's prompt should hold its own PR, not search's, and its system the run's date")
	}
	if other := h.agent.specs["search"][0]; other.System != spec.System || spec.System == "" {
		t.Error("every capability of a run should share one system text, so it can be cached")
	}

	if len(h.store.puts) != 1 || len(h.store.puts[0]) != 2 {
		t.Fatalf("puts %+v", h.store.puts)
	}
	row := h.store.puts[0][0]
	if row.Capability != "export" || row.Status != StatusRecovered || row.File != ex.File || row.Terms != ex.Terms ||
		!row.ProposalAt.Equal(proposedAt) || !row.At.Equal(runAt) || row.SessionID != ex.SessionID || row.Model != "fake-1" ||
		row.Attempts != 1 || row.Appetite != 0 || row.Input != ex.Tokens.Input || row.Output != 500 {
		t.Errorf("row %+v", row)
	}
	var d Outcome
	if err := json.Unmarshal(row.Details, &d); err != nil || d.Counts != ex.Counts || d.Dropped == nil || d.Problems == nil {
		t.Errorf("details %s: %v", row.Details, err)
	}
	if got := h.rootFiles(t); !reflect.DeepEqual(got, before) {
		t.Errorf("the repo changed: %v", got)
	}
	lines := strings.Split(strings.TrimSpace(h.progress.String()), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], "recovering 2 capabilities, at most 3 at once") || !strings.Contains(h.progress.String(), "export: recovered (1 rule, 0 decisions, 0 questions, 1 term; 1 attempt;") {
		t.Errorf("progress:\n%s", h.progress)
	}
}

func TestRetryFixesTheFileOnTheSameSession(t *testing.T) {
	h := newHarness(t, map[string][]step{"export": {
		{text: reply("# Notes\n\n## Rules\n\n"+bare("export-deleted"), "")},
		{text: reply(good("export-deleted"), "")},
	}})
	h.session.Only = []string{"export"}
	rep := h.run(t)

	o := rep.Capabilities[0]
	if o.Status != StatusRecovered || o.Attempts != 2 || o.Counts.Rules != 1 || len(o.Dropped) != 0 {
		t.Fatalf("outcome %+v", o)
	}
	specs := h.agent.specs["export"]
	if len(specs) != 2 {
		t.Fatalf("%d attempts", len(specs))
	}
	first, second := specs[0], specs[1]
	if first.Resume || !second.Resume || !second.ReadOnly || second.SessionID != first.SessionID || second.Dir != first.Dir {
		t.Errorf("first %+v\nsecond %+v", first, second)
	}
	for _, want := range []string{"a rule needs a `Because:` line", `the rules, at "### The rule export-deleted"`, "Write your whole answer again"} {
		if !strings.Contains(second.Prompt, want) {
			t.Errorf("the retry lacks %q:\n%s", want, second.Prompt)
		}
	}
	if strings.Contains(second.Prompt, "## Sources") {
		t.Error("the retry sent the sources again")
	}
	if used(o.Tokens) != used(adapter.Tokens{Input: int64(reading.Tokens(first.Prompt) + reading.Tokens(second.Prompt)), Output: 1000}) {
		t.Errorf("tokens %v should add both attempts", o.Tokens)
	}
}

func TestRetriesExhaustedDropTheFailingEntries(t *testing.T) {
	bad := reply("# Notes\n\n## Rules\n\n"+rule("export-deleted")+"\n"+bare("export-signed")+"\n### Exports name PR 99\n`export-ninety-nine` · #99\nSee it.\nBecause: it says so.\n",
		"**Export**: A copy of every note.\n\n**Export**: The same word again.\n")
	h := newHarness(t, map[string][]step{"export": {{text: bad}, {text: bad}, {text: bad}}})
	h.session.Only = []string{"export"}
	rep := h.run(t)

	o := rep.Capabilities[0]
	if o.Status != StatusRecovered || o.Attempts != 1+MaxRetries || len(h.agent.specs["export"]) != 3 {
		t.Fatalf("outcome %+v after %d attempts", o, len(h.agent.specs["export"]))
	}
	if o.Counts != (recovering.Counts{Rules: 1, Terms: 1}) || strings.Contains(o.File, "export-signed") || strings.Contains(o.File, "#99") || !strings.Contains(o.File, "export-deleted") {
		t.Errorf("counts %+v, file:\n%s", o.Counts, o.File)
	}
	if strings.Count(o.Terms, "**Export**") != 1 {
		t.Errorf("terms %q", o.Terms)
	}
	var got []string
	for _, d := range o.Dropped {
		got = append(got, d.Kind+" "+d.Slug+": "+strings.Join(d.Reasons, "; "))
	}
	want := []string{
		"rule export-signed: a rule needs a `Because:` line",
		"rule export-ninety-nine: anchor #99 names no PR in this repo",
		`term : term "Export" is already defined at line 3`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dropped\n%q\nwant\n%q", got, want)
	}
	t.Logf("after %d retries, dropped and listed: %q", MaxRetries, got)
	var d Outcome
	if err := json.Unmarshal(h.store.puts[0][0].Details, &d); err != nil || len(d.Dropped) != 3 {
		t.Errorf("details %s: %v", h.store.puts[0][0].Details, err)
	}
}

func TestFileLevelProblemFailsTheCapability(t *testing.T) {
	bad := reply("# Notes\n\n## Notes\n\nSome words.\n\n## Rules\n\n"+rule("export-deleted"), "")
	h := newHarness(t, map[string][]step{"export": {{text: bad}, {text: bad}, {text: bad}}, "search": {{text: reply(good("search-words"), "")}}})
	h.session.Only = []string{"export", "search"}
	rep := h.run(t)

	o := rep.Capabilities[0]
	if o.Status != StatusFailed || o.File != "" || o.Terms != "" || o.Counts != (recovering.Counts{}) || o.Attempts != 3 {
		t.Fatalf("outcome %+v", o)
	}
	if len(o.Problems) != 1 || !strings.Contains(o.Problems[0], `the file, at "## Notes": unknown section "Notes"`) {
		t.Errorf("problems %q", o.Problems)
	}
	if rep.Capabilities[1].Status != StatusRecovered {
		t.Errorf("search %+v", rep.Capabilities[1])
	}
	if row := h.store.puts[0][0]; row.Status != StatusFailed || row.File != "" {
		t.Errorf("row %+v", row)
	}
}

func TestAnswerThatCannotBeReadIsRetriedThenFails(t *testing.T) {
	h := newHarness(t, map[string][]step{"export": {{text: "No file."}, {text: "Still none."}, {text: "None."}}})
	h.session.Only = []string{"export"}
	rep := h.run(t)
	o := rep.Capabilities[0]
	if o.Status != StatusFailed || o.Attempts != 3 || !reflect.DeepEqual(o.Problems, []string{"the session's output: it has no fenced markdown block"}) {
		t.Fatalf("outcome %+v", o)
	}
	if !strings.Contains(h.agent.specs["export"][1].Prompt, "it has no fenced markdown block") {
		t.Error("the retry does not say what was wrong")
	}
}

func TestNeverMoreThanConcurrencyAtOnce(t *testing.T) {
	scripts := map[string][]step{}
	for _, n := range names {
		scripts[n] = []step{{text: reply(good(n+"-rule"), "")}}
	}
	h := newHarness(t, scripts)
	h.agent.started = make(chan struct{}, len(names))
	h.agent.gate = make(chan struct{})
	done := make(chan Report)
	go func() {
		rep, err := Run(context.Background(), h.store, h.session)
		if err != nil {
			t.Error(err)
		}
		done <- rep
	}()
	for range Concurrency {
		<-h.agent.started
	}
	close(h.agent.gate)
	rep := <-done

	if h.agent.most != Concurrency {
		t.Errorf("%d sessions ran at once, want %d", h.agent.most, Concurrency)
	}
	t.Logf("%d capabilities, at most %d sessions running at once", len(rep.Capabilities), h.agent.most)
	for i, o := range rep.Capabilities {
		if o.Name != names[i] || o.Status != StatusRecovered {
			t.Errorf("capability %d: %+v", i, o)
		}
	}
	if h.agent.starts() != len(names) {
		t.Errorf("started %v", h.agent.order)
	}
}

func TestOnlyFiltersAndKeepsItsOrder(t *testing.T) {
	h := newHarness(t, map[string][]step{
		"tags":   {{text: reply(good("tags-rule"), "")}},
		"export": {{text: reply(good("export-rule"), "")}},
	})
	h.session.Only = []string{"tags", "export", "tags"}
	rep := h.run(t)
	if got := statuses(rep); !reflect.DeepEqual(got, []string{"tags: recovered", "export: recovered"}) {
		t.Fatalf("statuses %v", got)
	}
	if len(h.store.puts[0]) != 2 || h.agent.starts() != 2 {
		t.Errorf("%d rows, %d sessions", len(h.store.puts[0]), h.agent.starts())
	}
}

func TestRefusalsStartNoAgent(t *testing.T) {
	t.Run("unknown only", func(t *testing.T) {
		h := newHarness(t, nil)
		h.session.Only = []string{"export", "billing"}
		_, err := Run(context.Background(), h.store, h.session)
		if err == nil || !strings.Contains(err.Error(), `no capability "billing"`) || !strings.Contains(err.Error(), "export, search, sharing, sync, tags") {
			t.Errorf("error %v", err)
		}
		if h.resolved != 0 || h.agent.starts() != 0 || len(h.store.puts) != 0 {
			t.Errorf("resolved %d, started %d, puts %d", h.resolved, h.agent.starts(), len(h.store.puts))
		}
	})
	t.Run("no proposal", func(t *testing.T) {
		h := newHarness(t, nil)
		h.store.proposal = nil
		_, err := Run(context.Background(), h.store, h.session)
		if !errors.Is(err, ErrNoProposal) || err.Error() != "nothing proposed yet; run physa init --propose first" {
			t.Errorf("error %v", err)
		}
		if h.resolved != 0 || h.agent.starts() != 0 {
			t.Errorf("resolved %d, started %d", h.resolved, h.agent.starts())
		}
	})
}

// TestNoCircuitBreaker checks that a session reporting far more than any
// appetite is never stopped: its answer is checked and kept, since init is
// never bounded (design 10 Q6).
func TestNoCircuitBreaker(t *testing.T) {
	big := adapter.Tokens{Input: 9_000_000, Output: 100}
	h := newHarness(t, map[string][]step{
		"export": {{text: reply(good("export-rule"), ""), reports: []adapter.Tokens{{Input: 1000}, big}, tokens: big}},
	})
	h.session.Only = []string{"export"}
	rep := h.run(t)
	if o := rep.Capabilities[0]; o.Status != StatusRecovered || o.Attempts != 1 || o.Tokens != big {
		t.Fatalf("outcome %+v", o)
	}
}

func TestDuplicateAcrossCapabilitiesIsDroppedFromTheLater(t *testing.T) {
	// tags runs first and sorts last: run order decides, not the name.
	h := newHarness(t, map[string][]step{
		"tags":   {{text: reply(good("shared-rule", "tags-rule"), "**Note**: What a person keeps.\n")}},
		"export": {{text: reply(good("export-rule", "shared-rule"), "**note**: The same word.\n\n**Export**: A copy of every note.\n")}},
	})
	h.session.Only = []string{"tags", "export"}
	rep := h.run(t)
	tags, export := rep.Capabilities[0], rep.Capabilities[1]
	if tags.Counts != (recovering.Counts{Rules: 2, Terms: 1}) || len(tags.Dropped) != 0 {
		t.Errorf("tags %+v", tags)
	}
	if export.Status != StatusRecovered || export.Counts != (recovering.Counts{Rules: 1, Terms: 1}) || strings.Contains(export.File, "shared-rule") || strings.Contains(export.Terms, "**note**") {
		t.Fatalf("export %+v", export)
	}
	var got []string
	for _, d := range export.Dropped {
		got = append(got, d.Kind+" "+d.Heading+": "+strings.Join(d.Reasons, "; "))
	}
	want := []string{
		`rule The rule shared-rule: slug "shared-rule" is already used in the capability "tags"`,
		`term note: term "note" is already defined in the capability "tags"`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dropped %q", got)
	}
	if row := h.store.puts[0][1]; row.File != export.File || row.Terms != export.Terms {
		t.Errorf("the stored row holds what was dropped: %+v", row)
	}
}

func TestAgentErrorFailsOnlyItsCapability(t *testing.T) {
	h := newHarness(t, map[string][]step{
		"export": {{err: adapter.ErrFailed, tokens: adapter.Tokens{Input: 700, Output: 3}}},
		"search": {{text: reply(good("search-rule"), "")}},
	})
	h.session.Only = []string{"export", "search"}
	rep := h.run(t)
	o := rep.Capabilities[0]
	if o.Status != StatusFailed || o.Error != adapter.ErrFailed.Error() || o.File != "" || o.Tokens.Input != 700 {
		t.Fatalf("outcome %+v", o)
	}
	if rep.Capabilities[1].Status != StatusRecovered {
		t.Errorf("search %+v", rep.Capabilities[1])
	}
	var d Outcome
	if err := json.Unmarshal(h.store.puts[0][0].Details, &d); err != nil || d.Error != o.Error {
		t.Errorf("details %s: %v", h.store.puts[0][0].Details, err)
	}
}

// TestTooLongSaysHowBigThePromptWas checks that a session refused as too
// long fails with physa's own estimate of its system prompt and message.
func TestTooLongSaysHowBigThePromptWas(t *testing.T) {
	h := newHarness(t, map[string][]step{
		"export": {{err: fmt.Errorf("%w: %w: Prompt is too long", adapter.ErrFailed, adapter.ErrTooLong)}},
	})
	h.session.Only = []string{"export"}
	o := h.run(t).Capabilities[0]
	for _, want := range []string{"Prompt is too long (by physa's estimate, the system prompt, the recovering skill and the docs every capability reads, came to about ",
		"and the message, with this capability's own sources, to about ", "; its sources: docs: ", "changes: "} {
		if o.Status != StatusFailed || !strings.Contains(o.Error, want) {
			t.Errorf("error lacks %q: %+v", want, o)
		}
	}
}

func TestScaffoldAloneChecksClean(t *testing.T) {
	for _, product := range []string{"People keep their notes.", ""} {
		c := checker{product: product, root: t.TempDir()}
		dir, split, err := c.scratch("export", recovering.File("export", "Notes leave the product.", ""), "")
		if err != nil {
			t.Fatal(err)
		}
		res, err := mapcheck.Check(dir)
		if err != nil || len(res.Problems) != 0 || res.Capabilities != 1 || split != nil {
			t.Errorf("product %q: %+v, split %v, error %v", product, res, split, err)
		}
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	}
}

// fakeRepo is a history holding one change.
type fakeRepo struct{ vcs.Repo }

func (fakeRepo) HasChange(id string) bool { return strings.HasPrefix(idA, id) }
func (fakeRepo) Partial() bool            { return false }

func TestCommitAnchorsResolveAgainstTheRepoAtRoot(t *testing.T) {
	file := "# Notes\n\n## Rules\n\n### Exports hold deleted notes\n`export-deleted` · a1b2c3d\nAn export holds every note.\nBecause: the change says so.\n" +
		"\n### Exports are signed\n`export-signed` · fffffff\nAn export is signed.\nBecause: a change that does not exist says so.\n"
	h := newHarness(t, map[string][]step{"export": {{text: reply(file, "")}, {text: reply(file, "")}, {text: reply(file, "")}}})
	h.session.Only = []string{"export"}
	var opened []string
	h.session.OpenRepo = func(dir string) (vcs.Repo, error) {
		opened = append(opened, dir)
		return fakeRepo{}, nil
	}
	rep := h.run(t)
	o := rep.Capabilities[0]
	if o.Status != StatusRecovered || o.Counts.Rules != 1 || len(o.Dropped) != 1 || o.Dropped[0].Slug != "export-signed" ||
		!strings.Contains(o.Dropped[0].Reasons[0], "does not name a commit in this repo") {
		t.Fatalf("outcome %+v", o)
	}
	for _, dir := range opened {
		if dir != h.root {
			t.Errorf("opened %s, not the repo root", dir)
		}
	}
	if len(opened) == 0 {
		t.Error("the repo was never opened")
	}
}

func TestGlossaryProblemOutsideAnyTermDropsTheTermsAndKeepsTheFile(t *testing.T) {
	bad := reply(good("export-rule"), "_Avoid_: dump\n\n**Export**: A copy of every note.\n")
	h := newHarness(t, map[string][]step{"export": {{text: bad}, {text: bad}, {text: bad}}})
	h.session.Only = []string{"export"}
	o := h.run(t).Capabilities[0]
	if o.Status != StatusRecovered || o.Counts != (recovering.Counts{Rules: 1}) || o.Terms != "" || len(o.Dropped) != 1 ||
		o.Dropped[0].Kind != recovering.KindTerm || !strings.Contains(o.Dropped[0].Reasons[0], "_Avoid_ must directly follow a term") {
		t.Fatalf("outcome %+v", o)
	}
}

// realPuts keeps what a run reads from the fake and stores into a real store.
type realPuts struct {
	*fakeStore
	real *initrecord.Store
}

func (r realPuts) PutRecovered(rs []initrecord.Recovered) error { return r.real.PutRecovered(rs) }

func TestRerunKeepsEarlierRecoveredFile(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := newHarness(t, map[string][]step{
		"export": {{text: reply(good("export-deleted"), "")}, {err: errors.New("agent down")}},
	})
	h.session.Only = []string{"export"}
	rs := realPuts{fakeStore: h.store, real: initrecord.Of(st)}
	if rep, err := Run(context.Background(), rs, h.session); err != nil || statuses(rep)[0] != "export: recovered" {
		t.Fatalf("first run %v, %v", statuses(rep), err)
	}
	rep, err := Run(context.Background(), rs, h.session)
	if err != nil {
		t.Fatal(err)
	}
	if got := statuses(rep); got[0] != "export: failed" {
		t.Fatalf("second run %v; want failed", got)
	}
	rows, err := initrecord.Of(st).Recovered()
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows %+v, %v", rows, err)
	}
	t.Logf("second run reported %v; stored row status=%q file=%q", statuses(rep), rows[0].Status, rows[0].File)
	if rows[0].Status != StatusRecovered || rows[0].File == "" {
		t.Errorf("stored row %+v; want the first recovered file kept", rows[0])
	}
}

func TestWhole(t *testing.T) {
	r := Report{Capabilities: []Outcome{{Status: StatusRecovered}, {Status: StatusFailed}}}
	if !r.Whole() {
		t.Error("a failed capability got its session; the report is whole")
	}
	if r.Capabilities = append(r.Capabilities, Outcome{Status: StatusNotYet}); r.Whole() {
		t.Error("a capability not yet recovered left the report whole")
	}
}

func TestPluralCountsSingularAndPlural(t *testing.T) {
	for _, c := range []struct {
		n    int
		want string
	}{{1, "1 token"}, {2, "2 tokens"}, {60_000, "60000 tokens"}} {
		if got := recoverinput.Plural(c.n, "token", "tokens"); got != c.want {
			t.Errorf("plural(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestProgressLineCountsCapabilitiesAndTokens(t *testing.T) {
	h := newHarness(t, map[string][]step{"export": {{text: reply(good("export-deleted"), "**Export**: A copy of every note, in a form other tools read.\n")}}})
	h.session.Only = []string{"export"}
	h.run(t)
	line := strings.SplitN(h.progress.String(), "\n", 2)[0]
	if want := "physa init: recovering 1 capability, at most 3 at once, from the proposal"; line != want {
		t.Errorf("progress = %q, want %q", line, want)
	}
}

// decision is a decision whose meta line lacks how it was decided, which
// the check fails.
const decision = "### Exports are one file\n`one-file` · decided by dana · 2026-11-20 · #12\nOne file per export.\nBecause: a file is easy to share.\n"

func TestDropBadDecisionNamesItsFolderFile(t *testing.T) {
	text := "# Notes\n\n## Rules\n\n" + rule("export-deleted") + "\n## Decisions\n\n" + decision
	h := newHarness(t, map[string][]step{"export": {{text: reply(text, "")}, {text: reply(text, "")}, {text: reply(text, "")}}})
	c := checker{product: "People keep their notes.", root: h.root, prExists: h.session.PRExists}
	problems, err := c.run("export", recovering.File("export", "People can rely on export.", text), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || problems[0].Path != ".physa/capabilities/export/decisions.md" || problems[0].Line != 10 {
		t.Fatalf("problems %v", problems)
	}
	t.Logf("the check found: %v", problems[0])

	h.session.Only = []string{"export"}
	o := h.run(t).Capabilities[0]
	if o.Status != StatusRecovered || o.Counts != (recovering.Counts{Rules: 1}) || strings.Contains(o.File, "one-file") || !strings.Contains(o.File, "export-deleted") {
		t.Fatalf("outcome %+v, file:\n%s", o, o.File)
	}
	if len(o.Dropped) != 1 || o.Dropped[0].Kind != "decision" || o.Dropped[0].Slug != "one-file" {
		t.Errorf("dropped %+v", o.Dropped)
	}
	t.Logf("dropped and listed: %+v", o.Dropped)
	if retry := h.agent.specs["export"][1].Prompt; !strings.Contains(retry, "the decisions, at \"`one-file` · decided by dana") {
		t.Errorf("the retry does not quote the decision:\n%s", retry)
	}
}

func TestSplitProblemIsAskedOfTheSession(t *testing.T) {
	h := newHarness(t, map[string][]step{"export": {
		{text: reply("# Notes\n\n"+rule("export-deleted"), "")},
		{text: reply(good("export-deleted"), "")},
	}})
	h.session.Only = []string{"export"}
	o := h.run(t).Capabilities[0]
	if o.Status != StatusRecovered || o.Attempts != 2 || o.Counts.Rules != 1 || o.Error != "" {
		t.Fatalf("outcome %+v", o)
	}
	retry := h.agent.specs["export"][1].Prompt
	if !strings.Contains(retry, `the file, at "### The rule export-deleted": entry outside a Rules, Decisions or Questions section`) {
		t.Errorf("the retry lacks the split problem:\n%s", retry)
	}
	t.Logf("asked of the session: %s", strings.SplitN(strings.SplitN(retry, "\n- ", 2)[1], "\n", 2)[0])
}

func TestOverviewOverItsLimitIsReplacedByThePromise(t *testing.T) {
	text := "# Notes\n\n" + strings.Repeat("Notes leave the product as files.\n", 25) + "\n## Rules\n\n" + rule("export-deleted")
	h := newHarness(t, map[string][]step{"export": {{text: reply(text, "")}, {text: reply(text, "")}, {text: reply(text, "")}}})
	h.session.Only = []string{"export"}
	o := h.run(t).Capabilities[0]
	if o.Status != StatusRecovered || o.Counts != (recovering.Counts{Rules: 1}) || strings.Contains(o.File, "as files") {
		t.Fatalf("outcome %+v, file:\n%s", o, o.File)
	}
	if len(o.Dropped) != 1 || o.Dropped[0].Kind != recovering.KindOverview || !strings.HasPrefix(o.Dropped[0].Reasons[0], "33 lines, budget is 30") {
		t.Errorf("dropped %+v", o.Dropped)
	}
	t.Logf("dropped and listed: %+v", o.Dropped)
	f, err := mapfolder.Split(o.File)
	if err != nil || !strings.Contains(f.Files[mapfolder.IndexFile], "# Notes\n\nPeople can rely on export.\n\n- [Rules](rules.md)") {
		t.Errorf("index.md %q, %v", f.Files[mapfolder.IndexFile], err)
	}
	if retry := h.agent.specs["export"][1].Prompt; !strings.Contains(retry, "the title and overview: 33 lines, budget is 30") {
		t.Errorf("the retry lacks the overview's problem:\n%s", retry)
	}
}

// windowed is the fake with a window, as the claude adapter states one.
type windowed struct {
	*fakeAdapter
	window int
}

func (w windowed) Window() int { return w.window }

// bigExport gives export three more PRs of about 26k estimated tokens each,
// so its sources are more than a small window holds, and recovers it alone.
func bigExport(t *testing.T, h *harness) {
	t.Helper()
	for _, n := range []int{21, 22, 23} {
		h.store.gather.PRs = append(h.store.gather.PRs, store.GatheredPR{Number: n, Title: fmt.Sprintf("Export part %d", n),
			Body: fmt.Sprintf("marker-%d ", n) + strings.Repeat("Exports hold every note. ", 2400), MergedAt: gatherAt.Add(-time.Duration(n) * time.Minute)})
	}
	var stored mapping.Stored
	if err := json.Unmarshal(h.store.proposal.Body, &stored); err != nil {
		t.Fatal(err)
	}
	stored.Proposal.Capabilities[0].ShownBy = []string{"#12", "#21", "#22", "#23"}
	body, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	h.store.proposal.Body = body
	h.session.Only = []string{"export"}
}

// readOnce checks that each big PR reached exactly one of the sessions' first
// prompts, and returns those prompts.
func readOnce(t *testing.T, specs []adapter.Spec) []string {
	t.Helper()
	var firsts []string
	for _, s := range specs {
		if !s.Resume {
			firsts = append(firsts, s.Prompt)
		}
	}
	for _, n := range []int{21, 22, 23} {
		seen := 0
		for _, p := range firsts {
			seen += strings.Count(p, fmt.Sprintf("marker-%d ", n))
		}
		if seen != 1 {
			t.Errorf("#%d was read %d times", n, seen)
		}
	}
	return firsts
}

func TestSourcesMoreThanAWindowAreRecoveredInShares(t *testing.T) {
	steps := []step{{text: reply(good("export-first"), "")}, {text: reply(good("export-first", "export-more"), "")},
		{text: reply(good("export-first", "export-more", "export-last"), "")}, {text: reply(good("export-first", "export-more", "export-last"), "")}}
	h := newHarness(t, map[string][]step{"export": steps})
	bigExport(t, h)
	h.session.Agent = func() (adapter.Adapter, error) {
		return windowed{h.agent, reading.Tokens(recoveringSystem(t, h)) + recovering.Reserve + 40_000}, nil
	}
	o := h.run(t).Capabilities[0]
	if o.Status != StatusRecovered || o.Shares < 3 || o.Attempts != o.Shares || o.Counts.Rules < 3 {
		t.Fatalf("outcome %+v", o)
	}
	firsts := readOnce(t, h.agent.specs["export"])
	if len(firsts) != o.Shares || !strings.Contains(firsts[0], "this session reads share 1") || strings.Contains(firsts[0], "The text recovered so far") {
		t.Fatalf("%d sessions for %d shares; the first:\n%.600s", len(firsts), o.Shares, firsts[0])
	}
	if !strings.Contains(firsts[1], "The text recovered so far") || !strings.Contains(firsts[1], "export-first") {
		t.Errorf("the second share does not carry the first's text:\n%.2000s", firsts[1])
	}
	if !strings.Contains(h.progress.String(), fmt.Sprintf("export: its sources are more than one session holds, so it is recovered in %d shares", o.Shares)) {
		t.Errorf("progress:\n%s", h.progress)
	}
}

func TestARefusalAsTooLongLaysTheSourcesIntoShares(t *testing.T) {
	tooLong := fmt.Errorf("%w: %w: Prompt is too long", adapter.ErrFailed, adapter.ErrTooLong)
	steps := []step{{err: tooLong}}
	for range 6 {
		steps = append(steps, step{text: reply(good("export-rule"), "")})
	}
	h := newHarness(t, map[string][]step{"export": steps})
	bigExport(t, h)
	o := h.run(t).Capabilities[0]
	if o.Status != StatusRecovered || o.Shares < 2 {
		t.Fatalf("outcome %+v\n%s", o, h.progress)
	}
	readOnce(t, h.agent.specs["export"][1:])
	if !strings.Contains(h.progress.String(), "export: a session was refused as too long; the room for a share is halved") {
		t.Errorf("progress:\n%s", h.progress)
	}
}

// recoveringSystem is the system prompt every capability of h's run shares.
func recoveringSystem(t *testing.T, h *harness) string {
	t.Helper()
	system, _ := recovering.Split(recovering.Input{Product: "People keep their notes and find them again.", Date: runAt,
		Sources: reading.Of(h.store.gather, recovering.Reads)})
	return system
}

func TestASourceLargerThanAShareFailsItsCapabilityNamed(t *testing.T) {
	h := newHarness(t, map[string][]step{"export": {{text: reply(good("export-rule"), "")}}})
	bigExport(t, h)
	h.session.Agent = func() (adapter.Adapter, error) {
		return windowed{h.agent, reading.Tokens(recoveringSystem(t, h)) + recovering.Reserve + 5_000}, nil
	}
	o := h.run(t).Capabilities[0]
	if o.Status != StatusFailed || !strings.HasPrefix(o.Error, "PR #2") || !strings.Contains(o.Error, "nothing is cut") || h.agent.starts() != 0 {
		t.Errorf("outcome %+v, %d sessions", o, h.agent.starts())
	}
}

var walkedAt = time.Date(2026, 11, 20, 11, 0, 0, 0, time.UTC)

// walkCaps are the walk's capabilities and their areas, across three areas.
var walkCaps = [][2]string{{"export", "notes-out"}, {"search", "finding"}, {"tags", "finding"}, {"sharing", "people"}}

// walkHarness is a harness whose store holds a walk and its placement, and
// a proposal only when proposed: export owns internal/export/export.go, of
// 40 lines, with #12 and a1b2c3d placed on it, and search has #7.
func walkHarness(t *testing.T, scripts map[string][]step, proposed bool) *harness {
	t.Helper()
	h := newHarness(t, scripts)
	if !proposed {
		h.store.proposal = nil
	}
	var wr walk.Record
	for _, c := range walkCaps {
		src := "internal/" + c[0] + "/" + c[0] + ".go"
		wr.Capabilities = append(wr.Capabilities, walk.CapabilityRecord{Area: c[1],
			Capability: walk.Capability{Name: c[0], Promise: "People can rely on " + c[0] + ".", Sources: []string{src}, Tests: []string{"internal/" + c[0] + "/" + c[0] + "_test.go"}},
			Entries:    walk.Entries{Tests: []string{"internal/" + c[0] + "/"}, Docs: []string{"docs/" + c[0] + ".md"}}})
	}
	if err := os.MkdirAll(filepath.Join(h.root, "internal", "export"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.root, "internal", "export", "export.go"), []byte(strings.Repeat("package export\n", 40)), 0o644); err != nil {
		t.Fatal(err)
	}
	pr := place.Record{ByCapability: []place.CapabilityPlaced{
		{Capability: "export", Area: "notes-out", PRs: []int{12}, Changes: []string{idA}},
		{Capability: "search", Area: "finding", PRs: []int{7}},
	}}
	wb, err := json.Marshal(wr)
	if err != nil {
		t.Fatal(err)
	}
	pb, err := json.Marshal(pr)
	if err != nil {
		t.Fatal(err)
	}
	h.store.walk = &initrecord.Walk{AreasAt: gatherAt, At: walkedAt, Body: wb}
	h.store.place = &initrecord.Place{WalkAt: walkedAt, At: walkedAt.Add(time.Minute), Body: pb}
	return h
}

// goodScripts answers every walk capability with a rule that checks clean,
// n times.
func goodScripts(n int) map[string][]step {
	scripts := map[string][]step{}
	for _, c := range walkCaps {
		for range n {
			scripts[c[0]] = append(scripts[c[0]], step{text: reply(good(c[0]+"-rule"), "")})
		}
	}
	return scripts
}

// walkOrder is the walk harness's capabilities in recovery's order for seed.
func walkOrder(t *testing.T, h *harness, seed uint64) []string {
	t.Helper()
	var wr walk.Record
	if err := json.Unmarshal(h.store.walk.Body, &wr); err != nil {
		t.Fatal(err)
	}
	pr, _, err := place.Load(h.store)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, w := range recoverinput.Order(recoverinput.Walk(h.root, wr, pr, h.store.gather), seed) {
		out = append(out, w.Name)
	}
	return out
}

func runNames(rep Report) []string {
	var out []string
	for _, o := range rep.Capabilities {
		out = append(out, o.Name)
	}
	return out
}

// TestWalkRecoversEveryCapabilityReadOnly checks that with a walk and its
// placement stored, every walk capability is recovered from them, each in a
// read-only session in the repo's root, given its area, files and placed
// PRs and changes.
func TestWalkRecoversEveryCapabilityReadOnly(t *testing.T) {
	h := walkHarness(t, goodScripts(1), true)
	before := h.rootFiles(t)
	rep := h.run(t)

	if len(rep.Capabilities) != len(walkCaps) || rep.From != "walk" {
		t.Fatalf("from %q, outcomes %v", rep.From, statuses(rep))
	}
	for _, o := range rep.Capabilities {
		if o.Status != StatusRecovered {
			t.Errorf("%s: %+v", o.Name, o)
		}
		spec := h.agent.specs[o.Name][0]
		if !spec.ReadOnly || spec.PromptOnly || spec.Dir != h.root || spec.SessionID == "" || spec.SessionID != o.SessionID {
			t.Errorf("%s: spec ReadOnly %v, PromptOnly %v, Dir %q, session %q", o.Name, spec.ReadOnly, spec.PromptOnly, spec.Dir, spec.SessionID)
		}
	}
	ex := h.agent.specs["export"][0]
	for _, want := range []string{"The area of the code it was found in:", "notes-out", "## Its files", "internal/export/export.go (40 lines)",
		"internal/export/\n", "docs/export.md", "#12, merged", "Export every note", "Revert"} {
		if !strings.Contains(ex.Prompt, want) {
			t.Errorf("export's message lacks %q:\n%s", want, ex.Prompt)
		}
	}
	if strings.Contains(ex.Prompt, "#7, merged") || strings.Contains(ex.Prompt, "The proposal's questions") {
		t.Error("export's message holds search's PR or the proposal's form")
	}
	if !strings.Contains(ex.System, "People keep their notes and find them again.") {
		t.Error("the product's promise is the stored proposal's")
	}
	if !strings.Contains(h.progress.String(), "physa init: recovering 4 capabilities, at most 3 at once, from the walk and placements\n") {
		t.Errorf("progress:\n%s", h.progress)
	}
	for _, row := range h.store.puts[0] {
		if !row.ProposalAt.Equal(walkedAt) {
			t.Errorf("%s: ProposalAt %v, want the walk's time", row.Capability, row.ProposalAt)
		}
	}
	if got := h.rootFiles(t); !reflect.DeepEqual(got, before) {
		t.Errorf("the repo changed: %v", got)
	}
	t.Logf("from %q: %v; export's spec ReadOnly %v, Dir is root %v", rep.From, statuses(rep), ex.ReadOnly, ex.Dir == h.root)
}

// TestWalkWithoutProposalUsesTheStandIn checks that the walk path runs with
// no proposal stored, with "Not yet stated." for the product's promise.
func TestWalkWithoutProposalUsesTheStandIn(t *testing.T) {
	h := walkHarness(t, goodScripts(1), false)
	rep := h.run(t)
	if rep.From != "walk" || !rep.Whole() || len(rep.Capabilities) != len(walkCaps) {
		t.Fatalf("from %q, %v", rep.From, statuses(rep))
	}
	if sys := h.agent.specs["export"][0].System; !strings.Contains(sys, "Not yet stated.") {
		t.Error("the product's promise is not the stand-in")
	}
}

// TestWalkOrderFollowsTheStoredSeed checks that a walk run goes in
// recovery's order for the gather's seed, stored before the first session,
// and that a later run reads the stored seed and keeps its order.
func TestWalkOrderFollowsTheStoredSeed(t *testing.T) {
	h := walkHarness(t, goodScripts(1), true)
	h.agent.onStart = func(string, int) {
		if h.store.seed == nil {
			t.Error("a session started before the seed was stored")
		}
	}
	rep := h.run(t)
	want := walkOrder(t, h, recoverinput.Seed(h.store.gather))
	if got := runNames(rep); !reflect.DeepEqual(got, want) {
		t.Errorf("order %v, want %v", got, want)
	}
	if !slices.Equal(h.store.seeds, []uint64{recoverinput.Seed(h.store.gather)}) {
		t.Errorf("seeds stored %v", h.store.seeds)
	}

	// A seed stored earlier is read and kept: pick one whose order differs.
	var other uint64
	for other = 1; slices.Equal(walkOrder(t, h, other), want); other++ {
	}
	h.store.seed, h.store.seeds = &other, nil
	h.agent = newFake(goodScripts(1))
	rep = h.run(t)
	if got, want := runNames(rep), walkOrder(t, h, other); !reflect.DeepEqual(got, want) || len(h.store.seeds) != 0 {
		t.Errorf("order %v, want %v for the stored seed %d; seeds stored %v", got, want, other, h.store.seeds)
	}
	t.Logf("seed %d: %v; stored seed %d: %v", recoverinput.Seed(h.store.gather), want, other, runNames(rep))
}

// TestWalkOnlyKeepsItsOrder checks that Only names walk capabilities, in
// its order and each once, and that a name the walk lacks starts nothing.
func TestWalkOnlyKeepsItsOrder(t *testing.T) {
	h := walkHarness(t, goodScripts(1), true)
	h.session.Only = []string{"tags", "export", "tags"}
	rep := h.run(t)
	if got := statuses(rep); !reflect.DeepEqual(got, []string{"tags: recovered", "export: recovered"}) || rep.From != "walk" {
		t.Fatalf("statuses %v, from %q", got, rep.From)
	}
	if h.agent.starts() != 2 || len(h.store.seeds) != 0 {
		t.Errorf("%d sessions, seeds %v", h.agent.starts(), h.store.seeds)
	}

	h = walkHarness(t, nil, true)
	h.session.Only = []string{"export", "billing"}
	_, err := Run(context.Background(), h.store, h.session)
	if err == nil || !strings.Contains(err.Error(), `no capability "billing" in the walk; it found: export, search, tags, sharing`) {
		t.Errorf("error %v", err)
	}
	if h.resolved != 0 || h.agent.starts() != 0 || len(h.store.puts) != 0 || len(h.store.seeds) != 0 {
		t.Errorf("resolved %d, started %d, puts %d, seeds %v", h.resolved, h.agent.starts(), len(h.store.puts), h.store.seeds)
	}
}

// TestWalkWithoutPlacementRecoversFromTheProposal checks that a walk with
// no placement stored leaves recovery on the proposal.
func TestWalkWithoutPlacementRecoversFromTheProposal(t *testing.T) {
	h := walkHarness(t, map[string][]step{"export": {{text: reply(good("export-rule"), "")}}}, true)
	h.store.place = nil
	h.session.Only = []string{"export"}
	rep := h.run(t)
	if rep.From != "proposal" || strings.Contains(h.agent.specs["export"][0].Prompt, "## Its files") {
		t.Errorf("from %q", rep.From)
	}
	h.store.proposal = nil
	if _, err := Run(context.Background(), h.store, h.session); !errors.Is(err, ErrNoProposal) {
		t.Errorf("a walk with no placement and no proposal: %v", err)
	}
}

// TestWalkRetryDropsBadAnchorAndPath checks the failure case: an entry
// whose anchor names no file in the repo and one that names a file path in
// its body go back to the same session with both problems, and when every
// retry answers the same, both are dropped with their reasons and the rest
// of the file is kept.
func TestWalkRetryDropsBadAnchorAndPath(t *testing.T) {
	bad := reply("# Export\n\n## Rules\n\n"+rule("export-deleted")+
		"\n### Exports are zipped\n`export-zipped` · from internal/export/zip.go\nAn export is one zip file.\nBecause: the code writes one.\n"+
		"\n### Exports hold every note\n`export-every` · #12\nThe writer in internal/export/export.go reads every note.\nBecause: a reviewer asked for it in #12.\n", "")
	h := walkHarness(t, map[string][]step{"export": {{text: bad}, {text: bad}, {text: bad}}}, true)
	h.session.Only = []string{"export"}
	rep := h.run(t)

	o := rep.Capabilities[0]
	specs := h.agent.specs["export"]
	if o.Status != StatusRecovered || o.Attempts != 1+MaxRetries || len(specs) != 1+MaxRetries {
		t.Fatalf("outcome %+v", o)
	}
	for _, s := range specs[1:] {
		if !s.Resume || s.SessionID != specs[0].SessionID || !s.ReadOnly {
			t.Errorf("a retry went to another session: %+v", s)
		}
	}
	anchor := `anchor "internal/export/zip.go" does not name a file in the repo`
	path := pathlint.Message("internal/export/export.go")
	for _, want := range []string{anchor, path, "at \"`export-zipped` · from internal/export/zip.go\"", `at "The writer in internal/export/export.go reads every note."`} {
		if !strings.Contains(strings.Join(strings.Fields(specs[1].Prompt), " "), want) {
			t.Errorf("the retry lacks %q:\n%s", want, specs[1].Prompt)
		}
	}
	var got []string
	for _, d := range o.Dropped {
		got = append(got, d.Kind+" "+d.Slug+": "+strings.Join(d.Reasons, "; "))
	}
	if want := []string{"rule export-zipped: " + anchor, "rule export-every: " + path}; !reflect.DeepEqual(got, want) {
		t.Errorf("dropped\n%q\nwant\n%q", got, want)
	}
	if o.Counts.Rules != 1 || !strings.Contains(o.File, "export-deleted") || strings.Contains(o.File, "export-zipped") || strings.Contains(o.File, "export-every") {
		t.Errorf("counts %+v, file:\n%s", o.Counts, o.File)
	}
	t.Logf("after %d retries, dropped with their reasons: %q", MaxRetries, got)
}

// TestRetryWithoutResumeStartsAFreshSession checks that when the agent
// cannot resume, a retry starts a new session on the capability's prompt
// followed by the retry.
func TestRetryWithoutResumeStartsAFreshSession(t *testing.T) {
	h := walkHarness(t, map[string][]step{"export": {
		{text: reply("# Export\n\n## Rules\n\n"+bare("export-deleted"), "")},
		{text: reply(good("export-deleted"), "")},
	}}, true)
	h.agent.noResume = true
	h.session.Only = []string{"export"}
	rep := h.run(t)

	o := rep.Capabilities[0]
	specs := h.agent.specs["export"]
	if o.Status != StatusRecovered || o.Attempts != 2 || len(specs) != 2 {
		t.Fatalf("outcome %+v, %d specs", o, len(specs))
	}
	first, second := specs[0], specs[1]
	if second.Resume || second.SessionID == first.SessionID || o.SessionID != second.SessionID || second.System != first.System || !second.ReadOnly {
		t.Errorf("first %+v\nsecond %+v", first, second)
	}
	if !strings.HasPrefix(second.Prompt, first.Prompt+"\n\n") || !strings.Contains(second.Prompt, "a rule needs a `Because:` line") {
		t.Errorf("the fresh session's prompt:\n%s", second.Prompt)
	}
	if !strings.Contains(h.progress.String(), "the agent cannot resume, so the retry starts a new session") {
		t.Errorf("progress:\n%s", h.progress)
	}
}

// rowOf is a stored row of capability name with status, from the source at at.
func rowOf(name, status string, at time.Time) initrecord.Recovered {
	return initrecord.Recovered{Capability: name, ProposalAt: at, At: runAt, Status: status, File: "# " + name + "\n"}
}

// TestContinueSkipsWhatIsAlreadyRecovered checks that a run with no --only
// starts sessions only for the capabilities not yet recovered from this
// walk, in the seeded order, says how many it kept, leaves the stored rows
// of the others alone, and reports a whole run.
func TestContinueSkipsWhatIsAlreadyRecovered(t *testing.T) {
	h := walkHarness(t, goodScripts(1), true)
	order := walkOrder(t, h, recoverinput.Seed(h.store.gather))
	h.store.rows = []initrecord.Recovered{rowOf(order[1], StatusRecovered, walkedAt), rowOf(order[3], StatusRecovered, walkedAt)}
	rep := h.run(t)
	if want := []string{order[0], order[2]}; !slices.Equal(runNames(rep), want) {
		t.Errorf("ran %v, want %v in the seeded order", runNames(rep), want)
	}
	for _, n := range []string{order[1], order[3]} {
		if len(h.agent.specs[n]) != 0 {
			t.Errorf("%s started a session", n)
		}
	}
	for _, row := range h.store.puts[0] {
		if row.Capability == order[1] || row.Capability == order[3] {
			t.Errorf("%s was rewritten", row.Capability)
		}
	}
	if !rep.Whole() {
		t.Error("the continued run is not whole")
	}
	if want := "physa init: 2 capabilities already recovered from the walk and placements are kept; recovering the other 2\n"; !strings.Contains(h.progress.String(), want) {
		t.Errorf("progress:\n%s", h.progress)
	}
}

// TestContinueRecoversAgainWhatIsNotFromThisSource checks that rows from an
// earlier walk, failed rows and not yet recovered rows are recovered again.
func TestContinueRecoversAgainWhatIsNotFromThisSource(t *testing.T) {
	h := walkHarness(t, goodScripts(1), true)
	order := walkOrder(t, h, recoverinput.Seed(h.store.gather))
	h.store.rows = []initrecord.Recovered{
		rowOf(order[0], StatusRecovered, walkedAt.Add(-time.Hour)),
		rowOf(order[1], StatusFailed, walkedAt),
		rowOf(order[2], StatusNotYet, walkedAt),
		rowOf(order[3], StatusRecovered, proposedAt),
	}
	rep := h.run(t)
	if !slices.Equal(runNames(rep), order) {
		t.Errorf("ran %v, want all of %v", runNames(rep), order)
	}
	if strings.Contains(h.progress.String(), "already recovered") {
		t.Errorf("progress:\n%s", h.progress)
	}
}

// TestContinueOnlyRecoversANamedCapabilityAgain checks that --only recovers a
// capability already recovered.
func TestContinueOnlyRecoversANamedCapabilityAgain(t *testing.T) {
	h := walkHarness(t, goodScripts(1), true)
	order := walkOrder(t, h, recoverinput.Seed(h.store.gather))
	h.store.rows = []initrecord.Recovered{rowOf(order[0], StatusRecovered, walkedAt)}
	h.session.Only = []string{order[0]}
	if rep := h.run(t); !slices.Equal(runNames(rep), []string{order[0]}) {
		t.Errorf("ran %v", runNames(rep))
	}
}

// TestContinueWithEverythingRecoveredRunsNothing checks that when every
// capability is already recovered no agent resolves and the report is whole.
func TestContinueWithEverythingRecoveredRunsNothing(t *testing.T) {
	h := walkHarness(t, goodScripts(1), true)
	for _, n := range walkOrder(t, h, recoverinput.Seed(h.store.gather)) {
		h.store.rows = append(h.store.rows, rowOf(n, StatusRecovered, walkedAt))
	}
	rep := h.run(t)
	if h.resolved != 0 || h.agent.starts() != 0 || len(h.store.puts) != 0 || len(rep.Capabilities) != 0 || !rep.Whole() || rep.From != "walk" {
		t.Errorf("resolved %d, started %d, puts %d, report %+v", h.resolved, h.agent.starts(), len(h.store.puts), rep)
	}
	if !strings.Contains(h.progress.String(), "recovering the other 0") {
		t.Errorf("progress:\n%s", h.progress)
	}
}

// capping refuses, as a cap's guard does, the first session of the
// capability first names and every resumed session, and starts the rest on
// its fake.
type capping struct {
	*fakeAdapter
	first string
}

func (c capping) Start(ctx context.Context, s adapter.Spec) (adapter.Session, error) {
	if s.Resume || strings.Contains(s.Prompt, "name: "+c.first+"\n") {
		return nil, fmt.Errorf("%w: a session held to 80k tokens would pass the cap", adapter.ErrCapped)
	}
	return c.fakeAdapter.Start(ctx, s)
}

// TestCappedSessionLeavesItNotYetRecovered checks that a capability whose
// first session, or whose retry, the cap refuses ends not yet recovered,
// not failed, with nothing of it stored as recovered, while the others are.
func TestCappedSessionLeavesItNotYetRecovered(t *testing.T) {
	h := newHarness(t, map[string][]step{
		"export":  {{text: reply("# Notes\n\n## Rules\n\n"+bare("export-signed"), "")}}, // its retry is refused
		"search":  {{text: reply(good("search-rule"), "")}},                             // its first session is refused
		"sharing": {{text: reply(good("sharing-rule"), "")}},
	})
	h.session.Only = []string{"export", "search", "sharing"}
	h.session.Agent = func() (adapter.Adapter, error) { return capping{fakeAdapter: h.agent, first: "search"}, nil }
	rep := h.run(t)
	want := []string{"export: " + StatusNotYet, "search: " + StatusNotYet, "sharing: " + StatusRecovered}
	if got := statuses(rep); !reflect.DeepEqual(got, want) {
		t.Fatalf("statuses %q, want %q", got, want)
	}
	for _, o := range rep.Capabilities[:2] {
		if o.Error != "" || o.File != "" {
			t.Errorf("%s: error %q, file %q", o.Name, o.Error, o.File)
		}
	}
	if rep.Whole() {
		t.Error("a capped run is whole")
	}
	for _, row := range h.store.puts[0] {
		if row.Capability != "sharing" && (row.Status != StatusNotYet || row.File != "" || !row.KeepEarlier) {
			t.Errorf("stored %s as %q with file %q", row.Capability, row.Status, row.File)
		}
	}
	if o := rep.Capabilities[0]; o.Attempts != 1 || len(h.agent.specs["export"]) != 1 {
		t.Errorf("export ran %d attempts, %d started; the refused retry counts none", o.Attempts, len(h.agent.specs["export"]))
	}
	if !strings.Contains(h.progress.String(), "physa init: search: "+StatusNotYet+"\n") {
		t.Errorf("progress:\n%s", h.progress)
	}
}
