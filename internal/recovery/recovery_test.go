package recovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/mapfolder"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/reading"
	"github.com/dphaener/physa/internal/recovering"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
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
}

func newFake(scripts map[string][]step) *fakeAdapter {
	return &fakeAdapter{scripts: scripts, byID: map[string]string{}, specs: map[string][]adapter.Spec{}}
}

func (f *fakeAdapter) Name() string { return "fake" }

func (f *fakeAdapter) Start(_ context.Context, s adapter.Spec) (adapter.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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

// fakeStore holds a gather and a proposal, and what was put in it.
type fakeStore struct {
	gather   store.Gather
	proposal *store.Proposal
	puts     [][]store.Recovered
}

func (s *fakeStore) Gathered() (store.Gather, bool, error) { return s.gather, true, nil }

func (s *fakeStore) Proposed() (store.Proposal, bool, error) {
	if s.proposal == nil {
		return store.Proposal{}, false, nil
	}
	return *s.proposal, true, nil
}

func (s *fakeStore) PutRecovered(rs []store.Recovered) error {
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
	return &fakeStore{gather: testGather(), proposal: &store.Proposal{GatherAt: gatherAt, At: proposedAt, Body: body}}
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
	if !spec.PromptOnly || spec.Resume || spec.SessionID != ex.SessionID || spec.Dir == "" || strings.HasPrefix(spec.Dir, h.root) {
		t.Errorf("spec %+v", spec)
	}
	if _, err := os.Stat(spec.Dir); !os.IsNotExist(err) {
		t.Errorf("the session's directory %s is still there", spec.Dir)
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
	if first.Resume || !second.Resume || !second.PromptOnly || second.SessionID != first.SessionID || second.Dir != first.Dir {
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

func TestSourcesAreTheCapabilitysOwn(t *testing.T) {
	g := testGather()
	sub := sources(g, mapping.Capability{Name: "export", ShownBy: []string{"#12"},
		Questions: []mapping.Question{{Question: "Was it reverted?", SuggestedBy: []string{"b1b2c3d"}}}})
	if len(sub.Docs) != 1 || len(sub.PRs) != 1 || sub.PRs[0].Number != 12 {
		t.Errorf("docs %d, PRs %+v", len(sub.Docs), sub.PRs)
	}
	// b1b2c3d is linked; a1b2c3d comes with it as the change it reverts.
	if len(sub.Changes) != 2 || sub.Changes[0].ID != idA || sub.Changes[1].ID != idB {
		t.Errorf("changes %+v", sub.Changes)
	}
	// And the other way: the revert comes with the change it reverts.
	sub = sources(g, mapping.Capability{ShownBy: []string{"a1b2c3d"}})
	if len(sub.Changes) != 2 || len(sub.PRs) != 0 {
		t.Errorf("changes %+v, PRs %+v", sub.Changes, sub.PRs)
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
	real *store.Store
}

func (r realPuts) PutRecovered(rs []store.Recovered) error { return r.real.PutRecovered(rs) }

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
	rs := realPuts{fakeStore: h.store, real: st}
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
	rows, err := st.Recovered()
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows %+v, %v", rows, err)
	}
	t.Logf("second run reported %v; stored row status=%q file=%q", statuses(rep), rows[0].Status, rows[0].File)
	if rows[0].Status != StatusRecovered || rows[0].File == "" {
		t.Errorf("stored row %+v; want the first recovered file kept", rows[0])
	}
}

func TestPluralCountsSingularAndPlural(t *testing.T) {
	for _, c := range []struct {
		n    int
		want string
	}{{1, "1 token"}, {2, "2 tokens"}, {60_000, "60000 tokens"}} {
		if got := plural(c.n, "token", "tokens"); got != c.want {
			t.Errorf("plural(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestProgressLineCountsCapabilitiesAndTokens(t *testing.T) {
	h := newHarness(t, map[string][]step{"export": {{text: reply(good("export-deleted"), "**Export**: A copy of every note, in a form other tools read.\n")}}})
	h.session.Only = []string{"export"}
	h.run(t)
	line := strings.SplitN(h.progress.String(), "\n", 2)[0]
	if want := "physa init: recovering 1 capability, at most 3 at once"; line != want {
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

func TestSourcesHoldOnlyTheCapabilitysDocs(t *testing.T) {
	g := testGather()
	g.Docs = []store.Doc{
		{Path: "AGENTS.md", Kind: "agent"}, {Path: "README.md", Kind: "readme"}, {Path: "CONTRIBUTING.md", Kind: "contributing"},
		{Path: "docs/export.md", Kind: "overview"}, {Path: "docs/search.md", Kind: "overview"}, {Path: "docs/decisions.md", Kind: "decisions"},
	}
	sub := sources(g, mapping.Capability{Name: "export", Docs: []string{"docs/export.md", "docs/decisions.md"}})
	var got []string
	for _, d := range sub.Docs {
		got = append(got, d.Path)
	}
	want := []string{"AGENTS.md", "README.md", "CONTRIBUTING.md", "docs/export.md", "docs/decisions.md"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("docs = %v, want %v", got, want)
	}
}
