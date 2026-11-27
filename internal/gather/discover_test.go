package gather

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/discovery"
	"github.com/dphaener/physa/internal/vcs"
)

// fakeAgent is scripted with a reply and the token reports of its session.
type fakeAgent struct {
	t       *testing.T
	text    string
	reports []adapter.Tokens
	tokens  adapter.Tokens
	err     error
	noStart bool
	specs   []adapter.Spec
}

func (f *fakeAgent) Name() string { return "fake" }

func (f *fakeAgent) Start(_ context.Context, s adapter.Spec) (adapter.Session, error) {
	if f.noStart {
		f.t.Fatal("discovery started a session")
	}
	f.specs = append(f.specs, s)
	return f, nil
}

func (f *fakeAgent) Wait() (adapter.Result, error) {
	for _, t := range f.reports {
		if u := f.specs[len(f.specs)-1].Usage; u != nil {
			u(t)
		}
	}
	return adapter.Result{Text: f.text, Model: "fake-1", Tokens: f.tokens}, f.err
}

func (f *fakeAgent) discover() Discover {
	return Discover{Session: discovery.Session{Agent: func() (adapter.Adapter, error) { return f, nil }}}
}

func reply(sources ...string) string {
	return "Found some.\n\n```json\n{\"sources\":[" + strings.Join(sources, ",") + "]}\n```\n"
}

func source(path, kind, reason string) string {
	return fmt.Sprintf(`{"path":%q,"kind":%q,"reason":%q}`, path, kind, reason)
}

// withoutLister hides the Lister of the repo it wraps.
type withoutLister struct{ vcs.Repo }

// listerRepo lists only paths.
type listerRepo struct {
	vcs.Repo
	paths []string
}

func (l listerRepo) Listed(ctx context.Context) ([]string, error) { return l.paths, nil }

func discoverRepo(t *testing.T) *testRepo {
	r := newTestRepo(t)
	write(t, r.dir, "README.md", "# Hello\n")
	write(t, r.dir, "docs/design/index.md", "the design docs\n")
	write(t, r.dir, "docs/design/api.md", "the api design\n")
	r.commit("init", "main.go")
	return r
}

func TestDiscoverMergesNamedDocs(t *testing.T) {
	r := discoverRepo(t)
	f := &fakeAgent{t: t, tokens: adapter.Tokens{Input: 900, Output: 100}, text: reply(
		source("docs/design/index.md", "overview", "the design docs' index"),
		source("docs/design/api.md", "decisions", "the api decisions"),
		source("nope.md", "readme", "not there"),
	)}
	g, s, err := Run(t.Context(), r.dir, r.open(), nil, bounds(t), f.discover(), gatherNow(t))
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]string{}
	kinds := map[string]string{}
	for _, d := range g.Docs {
		reasons[d.Path], kinds[d.Path] = d.Reason, d.Kind
	}
	if kinds["README.md"] != KindReadme {
		t.Fatalf("the deterministic find was lost: %+v", g.Docs)
	}
	if kinds["docs/design/index.md"] != "overview" || reasons["docs/design/index.md"] != "the design docs' index" ||
		kinds["docs/design/api.md"] != "decisions" {
		t.Fatalf("named docs = %v %v", kinds, reasons)
	}
	d := s.Discovery
	if !d.Started || d.Named != 3 || d.Added != 2 || d.Why != "" || d.Model != "fake-1" || d.Tokens.Input != 900 {
		t.Fatalf("discovery = %+v", d)
	}
	if len(s.Docs.Skipped) != 1 || s.Docs.Skipped[0] != (Skip{"nope.md", "missing"}) {
		t.Fatalf("skipped = %+v", s.Docs.Skipped)
	}
	if s.Docs.Count != len(g.Docs) || s.Docs.Count != len(s.Docs.Files) {
		t.Fatalf("docs summary = %+v", s.Docs)
	}
	bytes := 0
	for _, d := range g.Docs {
		bytes += len(d.Body)
	}
	if s.Docs.Bytes != bytes {
		t.Fatalf("bytes = %d, want %d", s.Docs.Bytes, bytes)
	}
	if !f.specs[0].ReadOnly {
		t.Fatalf("spec = %+v", f.specs[0])
	}
}

func TestDiscoverKeepsFindsWhenNoSourcesNamed(t *testing.T) {
	r := discoverRepo(t)
	f := &fakeAgent{t: t, text: reply()}
	g, s, err := Run(t.Context(), r.dir, r.open(), nil, bounds(t), f.discover(), gatherNow(t))
	if err != nil || len(g.Docs) != 1 || g.Docs[0].Path != "README.md" {
		t.Fatalf("docs = %+v, err = %v", g.Docs, err)
	}
	if !s.Discovery.Started || s.Discovery.Added != 0 || s.Discovery.Why == "" {
		t.Fatalf("discovery = %+v", s.Discovery)
	}
}

func TestDiscoverNotInstalledKeepsFinds(t *testing.T) {
	r := discoverRepo(t)
	d := Discover{Session: discovery.Session{
		Agent: func() (adapter.Adapter, error) { return nil, fmt.Errorf("claude: %w", adapter.ErrNotInstalled) },
	}}
	g, s, err := Run(t.Context(), r.dir, r.open(), nil, bounds(t), d, gatherNow(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Docs) != 1 || g.Docs[0].Path != "README.md" {
		t.Fatalf("docs = %+v", g.Docs)
	}
	if s.Discovery.Started || !strings.HasPrefix(s.Discovery.Why, "no agent: ") || !strings.Contains(s.Discovery.Why, "not installed") {
		t.Fatalf("discovery = %+v", s.Discovery)
	}
}

func TestDiscoverFailingSessionKeepsFinds(t *testing.T) {
	r := discoverRepo(t)
	f := &fakeAgent{t: t, err: errors.New("boom"), tokens: adapter.Tokens{Input: 5}}
	g, s, err := Run(t.Context(), r.dir, r.open(), nil, bounds(t), f.discover(), gatherNow(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Docs) != 1 || g.Docs[0].Path != "README.md" {
		t.Fatalf("docs = %+v", g.Docs)
	}
	if !s.Discovery.Started || !strings.Contains(s.Discovery.Why, "boom") || s.Discovery.Tokens.Input != 5 {
		t.Fatalf("discovery = %+v", s.Discovery)
	}
}

// TestDiscoverIsNeverStopped checks that a discovery session spending far
// more than the old cap still adds what it names: init is never bounded.
func TestDiscoverIsNeverStopped(t *testing.T) {
	r := discoverRepo(t)
	f := &fakeAgent{t: t, text: reply(source("docs/design/api.md", "overview", "x")),
		reports: []adapter.Tokens{{Input: 4_000_000, Output: 500}}, tokens: adapter.Tokens{Input: 4_000_000, Output: 500}}
	g, s, err := Run(t.Context(), r.dir, r.open(), nil, bounds(t), f.discover(), gatherNow(t))
	if err != nil {
		t.Fatal(err)
	}
	if d := s.Discovery; len(g.Docs) != 2 || !d.Started || d.Added != 1 || d.Tokens.Input != 4_000_000 {
		t.Fatalf("docs = %+v, discovery = %+v", g.Docs, d)
	}
}

func TestDiscoverOffStartsNothing(t *testing.T) {
	r := discoverRepo(t)
	f := &fakeAgent{t: t, noStart: true}
	d := f.discover()
	d.Off = "discovery not started: the repo has a map"
	_, s, err := Run(t.Context(), r.dir, r.open(), nil, bounds(t), d, gatherNow(t))
	if err != nil {
		t.Fatal(err)
	}
	if s.Discovery.Started || s.Discovery.Why != d.Off {
		t.Fatalf("discovery = %+v", s.Discovery)
	}
}

func TestDiscoverZeroValueIsNotAsked(t *testing.T) {
	r := discoverRepo(t)
	_, s, err := Run(t.Context(), r.dir, r.open(), nil, bounds(t), Discover{}, gatherNow(t))
	if err != nil || s.Discovery.Started || s.Discovery.Why != "discovery not asked" {
		t.Fatalf("discovery = %+v, err = %v", s.Discovery, err)
	}
}

func TestDiscoverRefusesWhatTheListerDoesNotList(t *testing.T) {
	r := discoverRepo(t)
	f := &fakeAgent{t: t, text: reply(source("docs/design/api.md", "overview", "x"))}
	repo := listerRepo{Repo: r.open(), paths: []string{"README.md", "listed-only.go", "docs/design/index.md"}}
	g, s, err := Run(t.Context(), r.dir, repo, nil, bounds(t), f.discover(), gatherNow(t))
	if err != nil {
		t.Fatal(err)
	}
	// docs/design/api.md exists but the repo does not list it.
	if len(g.Docs) != 1 || s.Discovery.Added != 0 || len(s.Docs.Skipped) != 1 {
		t.Fatalf("docs = %+v, discovery = %+v, skipped = %+v", g.Docs, s.Discovery, s.Docs.Skipped)
	}
}

func TestDiscoverWithoutAListerReadsTheWorkingTree(t *testing.T) {
	r := discoverRepo(t)
	write(t, r.dir, "node_modules/dep/index.js", "x")
	f := &fakeAgent{t: t, text: reply(source("docs/design/api.md", "overview", "the api"))}
	g, s, err := Run(t.Context(), r.dir, withoutLister{r.open()}, nil, bounds(t), f.discover(), gatherNow(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Docs) != 2 || s.Discovery.Added != 1 {
		t.Fatalf("docs = %+v, discovery = %+v", g.Docs, s.Discovery)
	}
}

func TestDiscoverPromptListsNoPaths(t *testing.T) {
	r := discoverRepo(t)
	f := &fakeAgent{t: t, text: reply()}
	if _, _, err := Run(t.Context(), r.dir, r.open(), nil, bounds(t), f.discover(), gatherNow(t)); err != nil {
		t.Fatal(err)
	}
	if p := f.specs[0].Prompt; p != discovery.Prompt() || strings.Contains(p, "main.go") || strings.Contains(p, "README.md (readme)") {
		t.Fatalf("prompt = %s", p)
	}
}
