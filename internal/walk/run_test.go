package walk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/areas"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/productpromise"
	"github.com/dphaener/physa/internal/sourceset"
	"github.com/dphaener/physa/internal/store"
)

// written is the file the fake agent tries to write in every session.
const written = "written-by-a-session.txt"

var areaOf = regexp.MustCompile("## The area `([^`]+)`")

// fakeAgent records every Spec and answers through reply, which is given the
// area the session walks and how many turns that area had before this one.
// It runs reply in Wait, outside its lock, so one area's answer can wait for
// another's. In every turn it tries to write a file in the session's
// directory with the Write tool, and does so only when the spec has no fence
// or its tools hold Write; otherwise it counts a refusal.
type fakeAgent struct {
	mu       sync.Mutex
	specs    []adapter.Spec
	areas    map[string]string // session id to area
	turns    map[string]int
	reply    func(area string, turn int, s adapter.Spec) string
	noResume bool
	refused  int
	late     int // sessions started with a done ctx

	gate          chan struct{} // when set, every Wait blocks until it closes
	started       chan struct{}
	running, most int
}

type fakeSession struct {
	f    *fakeAgent
	s    adapter.Spec
	area string
	turn int
}

func (f *fakeAgent) Name() string { return "fake" }

func (f *fakeAgent) Start(ctx context.Context, s adapter.Spec) (adapter.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ctx.Err() != nil {
		f.late++ // the fake takes it, so a session started after a cancel shows
	}
	if s.Resume && f.noResume {
		return nil, adapter.ErrNoResume
	}
	if f.areas == nil {
		f.areas, f.turns = map[string]string{}, map[string]int{}
	}
	if !s.Resume {
		m := areaOf.FindStringSubmatch(s.Prompt)
		if m == nil {
			m = []string{"", "unknown"}
		}
		f.areas[s.SessionID] = m[1]
	}
	area := f.areas[s.SessionID]
	f.specs = append(f.specs, s)
	if tools := s.Tools(); tools == nil || slices.Contains(tools, "Write") { // nil is no fence
		if err := os.WriteFile(filepath.Join(s.Dir, written), []byte("x"), 0o644); err != nil {
			return nil, err
		}
	} else {
		f.refused++
	}
	f.running++
	f.most = max(f.most, f.running)
	if f.started != nil {
		f.started <- struct{}{}
	}
	f.turns[area]++
	return fakeSession{f, s, area, f.turns[area] - 1}, nil
}

func (s fakeSession) Wait() (adapter.Result, error) {
	if s.f.gate != nil {
		<-s.f.gate
	}
	text := s.f.reply(s.area, s.turn, s.s)
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	s.f.running--
	return adapter.Result{Text: text, Model: "fake-model", SessionID: s.s.SessionID, Tokens: adapter.Tokens{Input: 10, CachedInput: 2, Output: 3}}, nil
}

// of returns the recorded specs of one area's sessions, in order.
func (f *fakeAgent) of(area string) []adapter.Spec {
	var out []adapter.Spec
	for _, s := range f.specs {
		if f.areas[s.SessionID] == area {
			out = append(out, s)
		}
	}
	return out
}

// stored are the fixture's areas, in stored order.
var stored = []areas.AreaRecord{
	{Name: "accounts", Promise: "People sign in and stay signed in.", Files: []string{"app/auth/login.go", "app/auth/session.go", "web/login/form.js"}},
	{Name: "billing", Promise: "People are invoiced and pay.", Files: []string{"app/billing/invoice.go", "app/billing/pay.go", "web/cart/cart.js"}},
	{Name: "tooling", Promise: "The product builds and starts.", Files: []string{"Makefile", "cmd/main.go"}},
}

var storedSplit = sourceset.Division{Files: map[sourceset.Kind][]string{
	sourceset.Test: {"spec/auth_test.go", "spec/billing_test.go"},
	sourceset.Doc:  {"README.md", "docs/guide.md"},
}}

var (
	areasAt = time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	walkAt  = time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
)

func fenceJSON(s string) string { return "Here.\n\n```json\n" + s + "\n```\n" }

// good is a whole answer for each fixture area.
var good = map[string]string{
	"accounts": `{"capabilities":[{"name":"sign-in","promise":"People sign in.","sources":["app/auth/","web/login/form.js"],"tests":["spec/auth_test.go"],"docs":["docs/"]}],"shared":[]}`,
	"billing": `{"capabilities":[{"name":"invoices","promise":"People get invoices.","sources":["app/billing/invoice.go"],"tests":["spec/billing_test.go"],"docs":[]},
		{"name":"payments","promise":"People pay.","sources":["app/billing/pay.go","web/cart/cart.js"],"tests":["spec/"],"docs":[]}],"shared":[]}`,
	"tooling": `{"capabilities":[{"name":"command-line","promise":"People start the product from a terminal.","sources":["cmd/main.go"],"tests":[],"docs":["README.md"]}],
		"shared":[{"path":"Makefile","reason":"Build glue."}]}`,
}

// answers replies with good, or with first[area][turn] when there is one.
func answers(first map[string][]string) func(string, int, adapter.Spec) string {
	return func(area string, turn int, _ adapter.Spec) string {
		if turn < len(first[area]) {
			return fenceJSON(first[area][turn])
		}
		return fenceJSON(good[area])
	}
}

// setup builds a repo root holding the fixture's files and a store holding
// list as the stored areas, and returns the store and the session for f.
func setup(t *testing.T, f *fakeAgent, list []areas.AreaRecord) (*initrecord.Store, Session) {
	t.Helper()
	root := t.TempDir()
	paths := slices.Concat(storedSplit.Files[sourceset.Test], storedSplit.Files[sourceset.Doc])
	for _, a := range list {
		paths = append(paths, a.Files...)
	}
	for _, rel := range paths {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("code\nmore\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { raw.Close() })
	st := initrecord.Of(raw)
	if err := st.PutGather(context.Background(), store.Gather{Run: store.GatherRun{At: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}}); err != nil {
		t.Fatal(err)
	}
	if list != nil {
		body, err := json.Marshal(areas.Record{Split: storedSplit, Areas: list})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.PutAreas(context.Background(), initrecord.Areas{At: areasAt, Body: body}); err != nil {
			t.Fatal(err)
		}
	}
	return st, Session{Agent: func() (adapter.Adapter, error) { return f, nil }, Root: root, Now: func() time.Time { return walkAt }}
}

func load(t *testing.T, st *initrecord.Store) (Record, bool) {
	t.Helper()
	r, ok, err := Load(t.Context(), st)
	if err != nil {
		t.Fatal(err)
	}
	return r, ok
}

// ownedOnce fails unless every source file of every area in list is owned by
// exactly one stored capability of that area or named shared in it, and
// nothing else is. It returns the owner of each file ("shared" for a shared
// one).
func ownedOnce(t *testing.T, r Record, list []areas.AreaRecord) map[string]string {
	t.Helper()
	where, in := map[string]string{}, map[string]string{}
	put := func(path, owner, area string) {
		if where[path] != "" {
			t.Errorf("%s is owned by %s and %s", path, where[path], owner)
		}
		where[path], in[path] = owner, area
	}
	for _, c := range r.Capabilities {
		if len(c.Sources) == 0 || !slices.IsSorted(c.Sources) {
			t.Errorf("capability %s: sources %v", c.Name, c.Sources)
		}
		for _, p := range c.Sources {
			put(p, c.Name, c.Area)
		}
	}
	for _, s := range r.Shared {
		if s.Reason == "" {
			t.Errorf("shared %s has no reason", s.Path)
		}
		put(s.Path, "shared", s.Area)
	}
	n := 0
	for _, a := range list {
		for _, p := range a.Files {
			if n++; where[p] == "" {
				t.Errorf("%s of area %s is owned by no capability and not shared", p, a.Name)
			} else if in[p] != a.Name {
				t.Errorf("%s of area %s was placed by area %s", p, a.Name, in[p])
			}
		}
	}
	if len(where) != n {
		t.Errorf("the record places %d files, the areas hold %d", len(where), n)
	}
	return where
}

// tree lists every file under root with its content.
func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		out[p] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// (a) Every source file of every area is owned by one capability or named
// shared in the stored record.
func TestRunEveryFileOwnedOrSharedIsStored(t *testing.T) {
	f := &fakeAgent{reply: answers(nil)}
	st, s := setup(t, f, stored)
	var progress bytes.Buffer
	s.Progress = &progress
	before := tree(t, s.Root)
	res, err := Run(context.Background(), st, s)
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := load(t, st)
	if !ok {
		t.Fatal("no walk stored")
	}
	where := ownedOnce(t, rec, stored)
	t.Logf("stored record places every area file once: %v", where)
	if where["app/auth/session.go"] != "sign-in" || where["Makefile"] != "shared" || where["web/cart/cart.js"] != "payments" {
		t.Errorf("owners %v", where)
	}
	var names []string
	for _, c := range rec.Capabilities {
		names = append(names, c.Area+"/"+c.Name)
	}
	if want := []string{"accounts/sign-in", "billing/invoices", "billing/payments", "tooling/command-line"}; !slices.Equal(names, want) {
		t.Errorf("capabilities %v, want %v in area order", names, want)
	}
	signIn, payments := rec.Capabilities[0], rec.Capabilities[2]
	if !slices.Equal(signIn.Tests, []string{"spec/auth_test.go"}) || !slices.Equal(signIn.Docs, []string{"docs/guide.md"}) {
		t.Errorf("sign-in tests %v docs %v", signIn.Tests, signIn.Docs)
	}
	if !slices.Equal(payments.Tests, []string{"spec/auth_test.go", "spec/billing_test.go"}) || payments.Docs == nil {
		t.Errorf("payments tests %v docs %v", payments.Tests, payments.Docs)
	}
	if len(rec.Sessions) != 3 || len(rec.Splits) != 0 || len(rec.Renamed) != 0 {
		t.Errorf("sessions %v splits %v renamed %v", rec.Sessions, rec.Splits, rec.Renamed)
	}
	w, _, err := st.Walk(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !w.AreasAt.Equal(areasAt) || !w.At.Equal(walkAt) || w.Model != "fake-model" || w.Input != 30 || w.CachedInput != 6 || w.Output != 9 {
		t.Errorf("walk row %+v", w)
	}
	want := Result{
		Areas: []AreaResult{
			{Name: "accounts", Capabilities: []string{"sign-in"}, Owned: 3, Parts: []string{}},
			{Name: "billing", Capabilities: []string{"invoices", "payments"}, Owned: 3, Parts: []string{}},
			{Name: "tooling", Capabilities: []string{"command-line"}, Owned: 1, Shared: 1, Parts: []string{}},
		},
		Capabilities: 4, Owned: 7, Shared: 1, Renamed: []Rename{}, Model: "fake-model",
		Tokens: adapter.Tokens{Input: 30, CachedInput: 6, Output: 9},
	}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("result\n got %+v\nwant %+v", res, want)
	}
	if !reflect.DeepEqual(tree(t, s.Root), before) {
		t.Error("the walk changed the repo")
	}
	for _, a := range stored {
		if !strings.Contains(progress.String(), "physa init: walk: "+a.Name+": walking ") || !strings.Contains(progress.String(), "; session ") {
			t.Errorf("progress lacks %s:\n%s", a.Name, progress.String())
		}
	}
	msg := f.of("accounts")[0].Prompt
	if !strings.Contains(msg, "app/auth/login.go (2 lines)") || !strings.Contains(msg, "People sign in and stay signed in.") {
		t.Errorf("message:\n%s", msg)
	}
	var text bytes.Buffer
	if err := Text(&text, res); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		"Walked the areas for their capabilities (4): 7 files owned, 1 file shared, after fix rounds (0).\n",
		"- billing (3 files owned, 0 files shared): invoices, payments\n", "Model: fake-model\n",
	} {
		if !strings.Contains(text.String(), line) {
			t.Errorf("text lacks %q:\n%s", line, text.String())
		}
	}
	if strings.Contains(text.String(), ".go") || strings.Contains(text.String(), rec.Sessions[0]) {
		t.Errorf("text names a file or a session:\n%s", text.String())
	}
}

const billingLeavesCart = `{"capabilities":[{"name":"invoices","promise":"People get invoices.","sources":["app/billing/invoice.go"],"tests":[],"docs":[]},
	{"name":"payments","promise":"People pay.","sources":["app/billing/pay.go"],"tests":[],"docs":[]}],"shared":[]}`

// (b) An unclaimed file is sent back in the same resumed session and its
// fixed answer is stored.
func TestRunUnclaimedGoesBackToTheSameSession(t *testing.T) {
	f := &fakeAgent{reply: answers(map[string][]string{"billing": {billingLeavesCart, `{"add":{"payments":["web/cart/cart.js"]}}`}})}
	st, s := setup(t, f, stored)
	res, err := Run(context.Background(), st, s)
	if err != nil {
		t.Fatal(err)
	}
	specs := f.of("billing")
	if len(specs) != 2 {
		t.Fatalf("billing had %d turns, want 2", len(specs))
	}
	fix := specs[1]
	if !fix.Resume || fix.SessionID != specs[0].SessionID || fix.Dir != s.Root || !fix.ReadOnly || fix.System != "" {
		t.Errorf("fix spec %+v", fix)
	}
	if !strings.Contains(fix.Prompt, "Source files in no capability and not shared") || !strings.Contains(fix.Prompt, "web/cart/cart.js") {
		t.Errorf("fix prompt:\n%s", fix.Prompt)
	}
	rec, _ := load(t, st)
	if where := ownedOnce(t, rec, stored); where["web/cart/cart.js"] != "payments" {
		t.Errorf("owners %v", where)
	}
	if res.FixRounds != 1 || res.Unclaimed != 0 || len(rec.Sessions) != 3 {
		t.Errorf("fix rounds %d, unclaimed %d, sessions %d", res.FixRounds, res.Unclaimed, len(rec.Sessions))
	}
}

// (c) Still unclaimed after the fix rounds: nothing is stored and the error
// names the area.
func TestRunStillUnclaimedStoresNothing(t *testing.T) {
	f := &fakeAgent{reply: answers(map[string][]string{"billing": {billingLeavesCart, `{}`, `{"add":{}}`}})}
	st, s := setup(t, f, stored)
	res, err := Run(context.Background(), st, s)
	if err == nil {
		t.Fatal("no error")
	}
	for _, want := range []string{"1 of 3 areas", "billing: still not whole after 2 fix rounds", "source files in no capability and not shared: 1,", "no footprints are stored"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "accounts") || strings.Contains(err.Error(), "tooling") {
		t.Errorf("error names an area that ended whole: %v", err)
	}
	if _, ok := load(t, st); ok {
		t.Error("a walk was stored")
	}
	if n := len(f.of("billing")); n != 1+MaxFixRounds {
		t.Errorf("billing had %d turns, want %d", n, 1+MaxFixRounds)
	}
	t.Logf("nothing stored; error: %v", err)
	if res.FixRounds != MaxFixRounds || res.Unclaimed != 1 {
		t.Errorf("fix rounds %d, unclaimed %d", res.FixRounds, res.Unclaimed)
	}
}

// (d) Every session is read-only in the root, and a session's attempt to
// write is refused: the file never appears.
func TestRunWriteIsRefused(t *testing.T) {
	f := &fakeAgent{reply: answers(map[string][]string{"billing": {billingLeavesCart, `{"add":{"payments":["web/cart/cart.js"]}}`}})}
	st, s := setup(t, f, stored)
	if _, err := Run(context.Background(), st, s); err != nil {
		t.Fatal(err)
	}
	if len(f.specs) != 4 {
		t.Fatalf("%d turns, want 4", len(f.specs))
	}
	for _, spec := range f.specs {
		if !spec.ReadOnly || spec.PromptOnly || spec.Dir != s.Root || spec.SessionID == "" {
			t.Errorf("spec not read-only in the root: %+v", spec)
		}
		if tools := spec.Tools(); !slices.Equal(tools, []string{"Read", "Glob", "Grep"}) {
			t.Errorf("tools %v", tools)
		}
	}
	if f.refused != len(f.specs) {
		t.Errorf("%d of %d write attempts were refused", f.refused, len(f.specs))
	}
	if _, err := os.Stat(filepath.Join(s.Root, written)); !os.IsNotExist(err) {
		t.Errorf("the session's file exists (stat: %v)", err)
	}
	t.Logf("%d turns, each read-only in the root with tools Read, Glob, Grep; %d write attempts refused; %s does not exist", len(f.specs), f.refused, written)
	// The same fake does write when a session has no fence, so the refusal
	// above is the fence's doing.
	if _, err := f.Start(context.Background(), adapter.Spec{Prompt: "## The area `none`", Dir: s.Root, SessionID: "free"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, written)); err != nil {
		t.Errorf("an unfenced session could not write: %v", err)
	}
}

const (
	billingSplit = `{"split":[{"name":"invoicing","promise":"People get invoices.","sources":["app/billing/invoice.go"]},
		{"name":"paying","promise":"People pay.","sources":["app/billing/pay.go","web/cart/cart.js"]}]}`
	invoicingAgain = `{"split":[{"name":"one","promise":"A.","sources":["app/billing/invoice.go"]},{"name":"two","promise":"B.","sources":["app/billing/invoice.go"]}]}`
	invoicingNew   = `{"new":[{"name":"invoices","promise":"People get invoices.","sources":["app/billing/invoice.go"],"tests":[],"docs":[]}]}`
	payingGood     = `{"capabilities":[{"name":"payments","promise":"People pay.","sources":["app/billing/"],"tests":[],"docs":[]}],"shared":[{"path":"web/cart/cart.js","reason":"The cart every purchase uses."}]}`
)

func splitAnswers() func(string, int, adapter.Spec) string {
	return answers(map[string][]string{"billing": {billingSplit}, "invoicing": {invoicingAgain, invoicingNew}, "paying": {payingGood}})
}

// (e) A top-level area splits: each part gets its own fresh session without
// the split section, and a part that answers with a split gets a fix.
func TestRunSplitWalksEachPartOnce(t *testing.T) {
	f := &fakeAgent{reply: splitAnswers()}
	st, s := setup(t, f, stored)
	res, err := Run(context.Background(), st, s)
	if err != nil {
		t.Fatal(err)
	}
	top := f.of("billing")
	if len(top) != 1 || top[0].System != Walk+"\n"+Split {
		t.Fatalf("billing specs %+v", top)
	}
	inv, pay := f.of("invoicing"), f.of("paying")
	if len(inv) != 2 || len(pay) != 1 {
		t.Fatalf("invoicing had %d turns, paying %d", len(inv), len(pay))
	}
	for _, spec := range []adapter.Spec{inv[0], pay[0]} {
		if spec.Resume || spec.System != Walk || strings.Contains(spec.System, "Splitting an area") || spec.SessionID == top[0].SessionID {
			t.Errorf("a part's session is not fresh or holds the split section: resume %v, id %s", spec.Resume, spec.SessionID)
		}
		if !strings.Contains(spec.Prompt, "It is a part of the area `billing`") {
			t.Errorf("part prompt:\n%s", spec.Prompt)
		}
	}
	if inv[0].SessionID == pay[0].SessionID || strings.Contains(inv[0].Prompt, "pay.go") || !strings.Contains(inv[0].Prompt, "app/billing/invoice.go") {
		t.Errorf("parts share a session or files:\n%s", inv[0].Prompt)
	}
	if fix := inv[1]; !fix.Resume || fix.SessionID != inv[0].SessionID || !strings.Contains(fix.Prompt, "a part is never split again") {
		t.Errorf("the part's split got no fix: %+v", fix)
	}
	rec, _ := load(t, st)
	ownedOnce(t, rec, []areas.AreaRecord{stored[0], {Name: "invoicing", Files: []string{"app/billing/invoice.go"}},
		{Name: "paying", Files: []string{"app/billing/pay.go", "web/cart/cart.js"}}, stored[2]})
	wantSplit := []SplitRecord{{Area: "billing", Parts: []areas.AreaRecord{
		{Name: "invoicing", Promise: "People get invoices.", Files: []string{"app/billing/invoice.go"}},
		{Name: "paying", Promise: "People pay.", Files: []string{"app/billing/pay.go", "web/cart/cart.js"}},
	}}}
	if !reflect.DeepEqual(rec.Splits, wantSplit) {
		t.Errorf("splits %+v", rec.Splits)
	}
	var got []string
	for _, c := range rec.Capabilities {
		got = append(got, c.Parent+"/"+c.Area+"/"+c.Name)
	}
	if want := []string{"/accounts/sign-in", "billing/invoicing/invoices", "billing/paying/payments", "/tooling/command-line"}; !slices.Equal(got, want) {
		t.Errorf("capabilities %v, want %v", got, want)
	}
	var order []string
	for _, a := range res.Areas {
		order = append(order, a.Name)
	}
	if want := []string{"accounts", "billing", "invoicing", "paying", "tooling"}; !slices.Equal(order, want) {
		t.Errorf("areas %v, want %v", order, want)
	}
	if b := res.Areas[1]; !slices.Equal(b.Parts, []string{"invoicing", "paying"}) || len(b.Capabilities) != 0 || res.Areas[2].Parent != "billing" {
		t.Errorf("billing %+v, invoicing %+v", b, res.Areas[2])
	}
	if res.FixRounds != 1 || res.Capabilities != 4 || res.Shared != 2 || len(rec.Sessions) != 5 {
		t.Errorf("result %+v, sessions %d", res, len(rec.Sessions))
	}
	var text bytes.Buffer
	if err := Text(&text, res); err != nil || !strings.Contains(text.String(), "- billing: split into invoicing, paying\n") {
		t.Errorf("text (%v):\n%s", err, text.String())
	}
}

// A split that leaves a file out or names it twice is fixed like any answer.
func TestRunSplitWithAFileInNoPartIsFixed(t *testing.T) {
	short := `{"split":[{"name":"invoicing","promise":"People get invoices.","sources":["app/billing/invoice.go"]},{"name":"paying","promise":"People pay.","sources":["app/billing/pay.go"]}]}`
	f := &fakeAgent{reply: answers(map[string][]string{"billing": {short, `{"add":{"paying":["web/cart/cart.js"]}}`},
		"invoicing": {`{"capabilities":[{"name":"invoices","promise":"People get invoices.","sources":["app/billing/"],"tests":[],"docs":[]}]}`}, "paying": {payingGood}})}
	st, s := setup(t, f, stored)
	if _, err := Run(context.Background(), st, s); err != nil {
		t.Fatal(err)
	}
	fix := f.of("billing")[1]
	if !fix.Resume || !strings.Contains(fix.Prompt, "web/cart/cart.js") || !strings.Contains(fix.Prompt, "`add` maps a part's name") {
		t.Errorf("split fix prompt:\n%s", fix.Prompt)
	}
	rec, _ := load(t, st)
	if got := rec.Splits[0].Parts[1].Files; !slices.Equal(got, []string{"app/billing/pay.go", "web/cart/cart.js"}) {
		t.Errorf("paying files %v", got)
	}
}

// (f) No more than Concurrency sessions run at once.
func TestRunNeverMoreThanConcurrencyAtOnce(t *testing.T) {
	var list []areas.AreaRecord
	replies := map[string][]string{}
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		list = append(list, areas.AreaRecord{Name: "area-" + n, Promise: "P.", Files: []string{"src/" + n + ".go"}})
		replies["area-"+n] = []string{`{"capabilities":[{"name":"cap-` + n + `","promise":"P.","sources":["src/` + n + `.go"],"tests":[],"docs":[]}]}`}
	}
	f := &fakeAgent{reply: answers(replies), gate: make(chan struct{}), started: make(chan struct{}, len(list))}
	st, s := setup(t, f, list)
	done := make(chan error)
	go func() {
		_, err := Run(context.Background(), st, s)
		done <- err
	}()
	for range Concurrency {
		<-f.started
	}
	select {
	case <-f.started:
		t.Error("a session started beyond Concurrency while the others ran")
	case <-time.After(100 * time.Millisecond):
	}
	close(f.gate)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if f.most != Concurrency {
		t.Errorf("%d sessions ran at once, want %d", f.most, Concurrency)
	}
	rec, _ := load(t, st)
	ownedOnce(t, rec, list)
	for i, c := range rec.Capabilities {
		if c.Area != list[i].Name {
			t.Errorf("capability %d is of %s, want stored order", i, c.Area)
		}
	}
	if len(f.specs) != len(list) {
		t.Errorf("%d sessions, want %d", len(f.specs), len(list))
	}
}

// (g) A name two areas take is numbered in area order, whichever session
// ends first.
func TestRunRenamesInAreaOrder(t *testing.T) {
	billingDone := make(chan struct{})
	taken := `{"capabilities":[{"name":"sign-in","promise":"People pay signed in.","sources":["app/billing/","web/cart/"],"tests":[],"docs":[]}]}`
	f := &fakeAgent{}
	f.reply = func(area string, turn int, s adapter.Spec) string {
		switch area {
		case "billing":
			defer close(billingDone)
			return fenceJSON(taken)
		case "accounts":
			<-billingDone // accounts, the earlier area, ends after billing
		}
		return fenceJSON(good[area])
	}
	st, s := setup(t, f, stored)
	res, err := Run(context.Background(), st, s)
	if err != nil {
		t.Fatal(err)
	}
	want := []Rename{{Area: "billing", From: "sign-in", To: "sign-in-2"}}
	rec, _ := load(t, st)
	if !reflect.DeepEqual(res.Renamed, want) || !reflect.DeepEqual(rec.Renamed, want) {
		t.Errorf("renamed %+v and %+v, want %+v", res.Renamed, rec.Renamed, want)
	}
	if a, b := rec.Capabilities[0], rec.Capabilities[1]; a.Name != "sign-in" || a.Area != "accounts" || b.Name != "sign-in-2" || b.Area != "billing" {
		t.Errorf("capabilities %+v, %+v", a, b)
	}
	if !slices.Equal(res.Areas[1].Capabilities, []string{"sign-in-2"}) {
		t.Errorf("billing result %+v", res.Areas[1])
	}
	var text bytes.Buffer
	if err := Text(&text, res); err != nil || !strings.Contains(text.String(), "An earlier area took the name sign-in, so billing keeps it as sign-in-2.\n") {
		t.Errorf("text (%v):\n%s", err, text.String())
	}
}

// (h) With no stored areas the walk fails and no session starts.
func TestRunNoAreasStartsNoSession(t *testing.T) {
	f := &fakeAgent{reply: answers(nil)}
	for name, list := range map[string][]areas.AreaRecord{"none stored": nil, "stored empty": {}} {
		st, s := setup(t, f, list)
		resolved := 0
		s.Agent = func() (adapter.Adapter, error) { resolved++; return f, nil }
		_, err := Run(context.Background(), st, s)
		if err == nil || !strings.Contains(err.Error(), "physa init --areas") {
			t.Errorf("%s: error %v", name, err)
		}
		t.Logf("%s: %d agents resolved, %d sessions started; error: %v", name, resolved, len(f.specs), err)
		if _, ok := load(t, st); ok || resolved != 0 || len(f.specs) != 0 {
			t.Errorf("%s: resolved %d agents, started %d sessions", name, resolved, len(f.specs))
		}
	}
}

// (i) No prompt gives a number of capabilities or parts to aim for, and no
// system text names the file the capability definition was taken from.
func TestRunPromptsGiveNoCountToAimFor(t *testing.T) {
	f := &fakeAgent{reply: splitAnswers()}
	st, s := setup(t, f, stored)
	if _, err := Run(context.Background(), st, s); err != nil {
		t.Fatal(err)
	}
	count := regexp.MustCompile(`(?i)\d.*(capabilit|\bparts?\b)|(capabilit|\bparts?\b).*\d`)
	resumed := 0
	for _, spec := range f.specs {
		if spec.Resume {
			resumed++
		}
		for _, line := range strings.Split(spec.System+"\n"+spec.Prompt, "\n") {
			if count.MatchString(line) {
				t.Errorf("a line pairs a number with capabilities or parts: %s", line)
			}
		}
		if strings.Contains(spec.System, "glossary.md") || strings.Contains(spec.Prompt, "glossary.md") {
			t.Error("a session is pointed at the glossary file")
		}
		if !spec.Resume && !strings.Contains(spec.System, CapabilityDefinition) {
			t.Error("a session's system text lacks the capability definition")
		}
	}
	if resumed == 0 || len(f.specs) != 6 {
		t.Errorf("%d turns, %d resumed: the fix prompt was not among them", len(f.specs), resumed)
	}
}

// An agent that cannot resume gets the fix in a fresh session, after the
// area's message and the answer so far.
func TestRunFixWithoutResumeStartsAFreshSession(t *testing.T) {
	f := &fakeAgent{noResume: true, reply: answers(map[string][]string{"billing": {billingLeavesCart, `{"add":{"payments":["web/cart/cart.js"]}}`}})}
	st, s := setup(t, f, stored)
	if _, err := Run(context.Background(), st, s); err != nil {
		t.Fatal(err)
	}
	var fresh []adapter.Spec
	for _, spec := range f.specs {
		if strings.Contains(spec.Prompt, "## The area `billing`") {
			fresh = append(fresh, spec)
		}
	}
	if len(fresh) != 2 {
		t.Fatalf("billing started %d sessions, want 2", len(fresh))
	}
	fix := fresh[1]
	if fix.Resume || fix.SessionID == fresh[0].SessionID || fix.System != Walk+"\n"+Split || fix.Dir != s.Root || !fix.ReadOnly {
		t.Errorf("fresh fix spec %+v", fix)
	}
	for _, want := range []string{"app/billing/pay.go (2 lines)", "## Your answer so far", `"name":"payments"`, "Source files in no capability and not shared", "web/cart/cart.js"} {
		if !strings.Contains(fix.Prompt, want) {
			t.Errorf("fresh fix prompt lacks %q:\n%s", want, fix.Prompt)
		}
	}
	rec, _ := load(t, st)
	ownedOnce(t, rec, stored)
	if len(rec.Sessions) != 4 {
		t.Errorf("sessions %v", rec.Sessions)
	}
}

// An answer that cannot be read gets one more turn in the same session; a
// second one fails the area.
func TestRunUnreadableAnswerIsAskedOnce(t *testing.T) {
	f := &fakeAgent{}
	f.reply = func(area string, turn int, _ adapter.Spec) string {
		if area == "tooling" && turn == 0 {
			return "I looked around."
		}
		return fenceJSON(good[area])
	}
	st, s := setup(t, f, stored)
	if _, err := Run(context.Background(), st, s); err != nil {
		t.Fatal(err)
	}
	again := f.of("tooling")
	if len(again) != 2 || !again[1].Resume || !strings.Contains(again[1].Prompt, "could not be read") {
		t.Fatalf("tooling specs %+v", again)
	}
	rec, _ := load(t, st)
	ownedOnce(t, rec, stored)

	f = &fakeAgent{}
	f.reply = func(area string, _ int, _ adapter.Spec) string {
		if area == "tooling" {
			return "Still nothing."
		}
		return fenceJSON(good[area])
	}
	st, s = setup(t, f, stored)
	_, err := Run(context.Background(), st, s)
	if err == nil || !strings.Contains(err.Error(), "tooling: the walk answer: it has no fenced json block") {
		t.Errorf("error %v", err)
	}
	if _, ok := load(t, st); ok || len(f.of("tooling")) != 2 {
		t.Errorf("stored a walk, or tooling had %d turns", len(f.of("tooling")))
	}
}

func TestCollectKeepsEntriesAsWrittenAndOldBodiesLoad(t *testing.T) {
	j := &job{area: Area{Name: "a"}, fp: Footprints{Capabilities: []Footprint{{
		Name: "x", Promise: "p", Sources: []string{"s/a.go"},
		Tests: []string{"t/a_test.go", "t/b_test.go"}, Other: []string{"cfg/a.yml"},
		Entries: Entries{Tests: []string{"t/"}, Docs: []string{"docs/a.md"}, Other: []string{"cfg/"}},
	}}}}
	var res Result
	rec, failed := collect([]*job{j}, &res)
	if len(failed) != 0 {
		t.Fatal(failed)
	}
	c := rec.Capabilities[0]
	if !slices.Equal(c.Entries.Tests, []string{"t/"}) || !slices.Equal(c.Entries.Other, []string{"cfg/"}) || !slices.Equal(c.Other, []string{"cfg/a.yml"}) || len(c.Tests) != 2 {
		t.Errorf("record %+v", c)
	}
	j.fp.Capabilities[0].Entries.Tests[0] = "changed"
	if c.Entries.Tests[0] != "t/" {
		t.Error("entries share the footprint's slice")
	}
	var old Record
	body := `{"capabilities":[{"name":"x","promise":"p","sources":["s/a.go"],"tests":["t/a_test.go"],"docs":[],"area":"a","parent":""}],"shared":[],"splits":[],"renamed":[],"sessions":[]}`
	if err := json.Unmarshal([]byte(body), &old); err != nil {
		t.Fatal(err)
	}
	if o := old.Capabilities[0]; len(o.Other) != 0 || o.Entries.Tests != nil || o.Entries.Docs != nil || o.Entries.Other != nil || len(o.Tests) != 1 {
		t.Errorf("old body %+v", o)
	}
}

// A cancel while the first Concurrency sessions run starts no session after
// it: the agent receives exactly those, the run returns the cancel, and
// nothing is stored.
func TestRunCancelStartsNoSessionAfter(t *testing.T) {
	var list []areas.AreaRecord
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		list = append(list, areas.AreaRecord{Name: "area-" + n, Promise: "P.", Files: []string{"src/" + n + ".go"}})
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f := &fakeAgent{reply: answers(nil), gate: make(chan struct{}), started: make(chan struct{}, len(list))}
	st, s := setup(t, f, list)
	done := make(chan error)
	go func() {
		_, err := Run(ctx, st, s)
		done <- err
	}()
	for range Concurrency {
		<-f.started
	}
	cancel()
	close(f.gate)
	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v; want cancelled", err)
	}
	t.Logf("the agent received %d of %d sessions, %d after the cancel; error: %v", len(f.specs), len(list), f.late, err)
	if f.late != 0 || len(f.specs) != Concurrency {
		t.Errorf("%d sessions started, %d after the cancel; want the %d before it", len(f.specs), f.late, Concurrency)
	}
	if _, ok := load(t, st); ok {
		t.Error("a cancelled walk stored a record")
	}
}

// putProduct stores the fixture's areas again with a product stated.
func putProduct(t *testing.T, st *initrecord.Store, product string) {
	t.Helper()
	body, err := json.Marshal(struct {
		Split sourceset.Division `json:"split"`
		Areas []areas.AreaRecord `json:"areas"`
		Prod  json.RawMessage    `json:"product"`
	}{storedSplit, stored, json.RawMessage(product)})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.PutAreas(t.Context(), initrecord.Areas{At: areasAt, Body: body}); err != nil {
		t.Fatal(err)
	}
}

// A stated product's promise leads every session's message, a split part's too.
func TestRunProductPromiseLeadsEverySessionsMessage(t *testing.T) {
	f := &fakeAgent{reply: splitAnswers()}
	st, s := setup(t, f, stored)
	putProduct(t, st, `{"name":"Shop","promise":"People buy things."}`)
	if _, err := Run(t.Context(), st, s); err != nil {
		t.Fatal(err)
	}
	block := productpromise.Block("People buy things.") + "\n## The area `"
	first := 0
	for _, spec := range f.specs {
		if spec.Resume {
			continue
		}
		first++
		if !strings.HasPrefix(spec.Prompt, block) {
			t.Errorf("the message does not begin with the promise block:\n%s", spec.Prompt)
		}
	}
	if first != 5 { // accounts, billing, tooling, and billing's two parts
		t.Errorf("%d sessions started, want 5", first)
	}
}

// With no product stated, a message is what Message gives and says nothing of a promise.
func TestRunProductPromiseNoneLeavesTheMessage(t *testing.T) {
	f := &fakeAgent{reply: splitAnswers()}
	st, s := setup(t, f, stored)
	if _, err := Run(t.Context(), st, s); err != nil {
		t.Fatal(err)
	}
	for _, spec := range f.specs {
		if spec.Resume {
			continue
		}
		if !strings.HasPrefix(spec.Prompt, "## The area `") || strings.Contains(spec.Prompt, "promise:\n") || strings.Contains(spec.Prompt, "Not yet stated.") {
			t.Errorf("the message changed with no product stated:\n%s", spec.Prompt)
		}
	}
}
