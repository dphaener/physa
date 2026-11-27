package shares

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/reading"
	"github.com/dphaener/physa/internal/store"
)

var (
	gatherAt = time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	nowAt    = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	skillTok = reading.Tokens(mapping.Prompt(reading.Input{}))
	// smallWindow leaves a share room for the skill twice and 8,000 tokens,
	// so a halved share still has room for the skill and 4,000.
	smallWindow = 2*skillTok + 8_000 + mapping.ReplyReserve
)

// gatherOf is a gather of docs, PRs with two review comments each, and
// changes, of the sizes given in bytes.
func gatherOf(docs, docBytes, prs, prBytes, changes int) store.Gather {
	g := store.Gather{Run: store.GatherRun{At: gatherAt}}
	for i := 0; i < docs; i++ {
		g.Docs = append(g.Docs, store.Doc{Path: fmt.Sprintf("docs/d%03d.md", i), Kind: "overview", Body: text(docBytes, fmt.Sprintf("doc %d", i))})
	}
	for i := 0; i < prs; i++ {
		g.PRs = append(g.PRs, store.GatheredPR{Number: i + 1, Title: fmt.Sprintf("PR %d", i+1), MergedAt: day0.Add(time.Duration(i) * time.Hour),
			Body: text(prBytes, fmt.Sprintf("pr %d", i+1)), Comments: []store.GatheredComment{
				{Author: "ann", Body: fmt.Sprintf("First look at %d.", i+1)}, {Author: "bo", RequestsChanges: true, Body: fmt.Sprintf("Fix %d.", i+1)}}})
	}
	for i := 0; i < changes; i++ {
		g.Changes = append(g.Changes, store.GatheredChange{ID: fmt.Sprintf("c%07d", i), Short: fmt.Sprintf("c%07d", i),
			Subject: fmt.Sprintf("feat: Change number %d does a thing", i), At: day0.Add(time.Duration(i) * time.Minute),
			Files: []string{"a.go", "b.go"}})
	}
	return g
}

// large is a gather that takes several shares at smallWindow, with docs
// every share reads.
func large() store.Gather { return gatherOf(2, 1_000, 12, 3_000, 30) }

type fakeStore struct {
	gather store.Gather
	has    bool
	put    []initrecord.Proposal
}

func (s *fakeStore) Gathered() (store.Gather, bool, error) { return s.gather, s.has, nil }

func (s *fakeStore) PutProposal(p initrecord.Proposal) error {
	s.put = append(s.put, p)
	return nil
}

// call is one session a fakeAgent was asked to start.
type call struct {
	prompt, id string
	merge      bool
	err        error
}

// fakeAgent is an agent that never runs. It answers a share's prompt with
// one capability shown by the first PR or change the prompt holds, and the
// merge's with one capability shown by everything the shares gave, and
// records every session it was asked to start. script, when set, may answer
// the nth call (from 0) itself.
type fakeAgent struct {
	window int
	script func(n int, c call) (text string, err error, ok bool)
	extra  []string // links the merge adds to what the shares gave

	calls []call
	gave  []string
}

func (f *fakeAgent) Name() string { return "fake" }

func (f *fakeAgent) Start(_ context.Context, s adapter.Spec) (adapter.Session, error) {
	if !s.PromptOnly {
		return nil, errors.New("propose must run prompt-only sessions")
	}
	c := call{prompt: s.Prompt, id: s.SessionID, merge: strings.HasPrefix(s.Prompt, strings.TrimRight(merging, "\n"))}
	n := len(f.calls)
	res := adapter.Result{SessionID: s.SessionID, Model: "fake-1", Tokens: adapter.Tokens{Input: 100, CachedInput: 10, Output: 5}}
	if f.script != nil {
		if text, err, ok := f.script(n, c); ok {
			c.err = err
			f.calls = append(f.calls, c)
			if err != nil {
				res.Tokens = adapter.Tokens{}
			}
			res.Text = text
			return fakeSession{res, err}, nil
		}
	}
	f.calls = append(f.calls, c)
	if c.merge {
		res.Text = replyOf("everything", append(append([]string{}, f.gave...), f.extra...))
	} else {
		ref := firstRef(s.Prompt)
		f.gave = append(f.gave, ref)
		res.Text = replyOf(fmt.Sprintf("part-%d", n), []string{ref})
	}
	return fakeSession{res, nil}, nil
}

type fakeSession struct {
	res adapter.Result
	err error
}

func (s fakeSession) Wait() (adapter.Result, error) { return s.res, s.err }

// windowed is a fakeAgent that states its window.
type windowed struct{ *fakeAgent }

func (w windowed) Window() int { return w.window }

var (
	prRef     = regexp.MustCompile(`(?m)^#(\d+), merged `)
	changeRef = regexp.MustCompile(`(?m)^(c\d{7}) `)
)

func firstRef(prompt string) string {
	if m := prRef.FindStringSubmatch(prompt); m != nil {
		return "#" + m[1]
	}
	if m := changeRef.FindStringSubmatch(prompt); m != nil {
		return m[1]
	}
	return ""
}

func replyOf(name string, shown []string) string {
	b, _ := json.Marshal(map[string]any{
		"product": map[string]string{"promise": "Teams hear of every event."},
		"capabilities": []map[string]any{{"name": name, "promise": "It does " + name + ".", "shown_by": shown,
			"questions": []any{}}},
		"questions": []any{},
	})
	return "Here it is.\n\n```json\n" + string(b) + "\n```\n"
}

func tooLong() error {
	return fmt.Errorf("%w: %w: Prompt is too long", adapter.ErrFailed, adapter.ErrTooLong)
}

// propose runs Propose on g with f, which states its window when it has one.
func propose(g store.Gather, f *fakeAgent) (mapping.Result, error, *fakeStore, string) {
	st := &fakeStore{gather: g, has: true}
	var progress bytes.Buffer
	var a adapter.Adapter = f
	if f.window > 0 {
		a = windowed{f}
	}
	res, err := Propose(context.Background(), st, mapping.Session{
		Agent:    func() (adapter.Adapter, error) { return a, nil },
		Progress: &progress,
		Now:      func() time.Time { return nowAt },
	})
	return res, err, st, progress.String()
}

// readOnce asserts that the share sessions that ended well read every PR
// and change of g exactly once between them.
func readOnce(t *testing.T, g store.Gather, calls []call) {
	t.Helper()
	count := func(marker string) (n int) {
		for _, c := range calls {
			if !c.merge && c.err == nil {
				n += strings.Count(c.prompt, marker)
			}
		}
		return n
	}
	for _, p := range g.PRs {
		if n := count(fmt.Sprintf("title: %s\n", p.Title)); n != 1 {
			t.Errorf("PR #%d was read by %d shares, want exactly 1", p.Number, n)
		}
		if n := count(p.Body); n != 1 {
			t.Errorf("PR #%d's body is whole in %d shares, want exactly 1", p.Number, n)
		}
	}
	for _, c := range g.Changes {
		if n := count(c.Short + " " + c.Subject + "\n"); n != 1 {
			t.Errorf("change %s was read by %d shares, want exactly 1", c.Short, n)
		}
	}
}

func TestProposeNoGatherResolvesNoAgent(t *testing.T) {
	resolved := 0
	_, err := Propose(context.Background(), &fakeStore{}, mapping.Session{Agent: func() (adapter.Adapter, error) {
		resolved++
		return &fakeAgent{}, nil
	}})
	if !errors.Is(err, mapping.ErrNoGather) || resolved != 0 {
		t.Fatalf("err %v, agent resolved %d times", err, resolved)
	}
}

func TestProposeThatFitsRunsOneSessionUnchanged(t *testing.T) {
	g := gatherOf(2, 1_000, 3, 1_000, 10)
	for _, window := range []int{0, 1_000_000} {
		t.Run(fmt.Sprintf("window %d", window), func(t *testing.T) {
			f := &fakeAgent{window: window}
			res, err, st, progress := propose(g, f)
			if err != nil {
				t.Fatal(err)
			}
			if len(f.calls) != 1 || f.calls[0].prompt != mapping.Prompt(mapping.Read(g)) {
				t.Fatalf("%d sessions; want exactly one, with mapping.Propose's prompt", len(f.calls))
			}
			if len(res.Shares) != 0 || res.Merge != nil || len(st.put) != 1 {
				t.Fatalf("shares %d, merge %v, stored %d", len(res.Shares), res.Merge, len(st.put))
			}
			// The same run through mapping.Propose stores the same body.
			want := &fakeStore{gather: g, has: true}
			if _, err := mapping.Propose(context.Background(), want, mapping.Session{
				Agent: func() (adapter.Adapter, error) { return &fakeAgent{}, nil }, Now: func() time.Time { return nowAt }}); err != nil {
				t.Fatal(err)
			}
			if string(st.put[0].Body) != string(want.put[0].Body) || strings.Contains(string(st.put[0].Body), `"sessions"`) {
				t.Errorf("stored body differs from mapping.Propose's:\n%s\n%s", st.put[0].Body, want.put[0].Body)
			}
			if !strings.Contains(progress, "propose reads its sources whole") || strings.Contains(progress, "share") {
				t.Errorf("progress: %q", progress)
			}
		})
	}
}

func TestProposeInSharesAndAMerge(t *testing.T) {
	g := large()
	in := mapping.Read(g)
	room := smallWindow - mapping.ReplyReserve
	laid, err := Lay(in, room)
	if err != nil || len(laid) < 2 {
		t.Fatalf("Lay: %d shares, %v", len(laid), err)
	}
	k := len(laid)
	// The merge adds a PR that was gathered but that no share gave, and one
	// that does not exist.
	f := &fakeAgent{window: smallWindow, extra: []string{"#2", "#999999"}}
	res, err, st, progress := propose(g, f)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != k+1 {
		t.Fatalf("%d sessions, want %d shares and a merge", len(f.calls), k)
	}
	var ids []string
	promptTokens := 0
	for i, c := range f.calls {
		ids = append(ids, c.id)
		promptTokens += reading.Tokens(c.prompt)
		if i < k {
			if c.merge || c.prompt != mapping.Prompt(laid[i]) || reading.Tokens(c.prompt) > room {
				t.Errorf("session %d is not share %d of Lay's, within room", i+1, i+1)
			}
			if !strings.Contains(progress, fmt.Sprintf("propose reads share %d of %d whole, ~%d tokens, in a prompt of ~%d tokens; session %s\n",
				i+1, k, laid[i].Report.Tokens, reading.Tokens(c.prompt), c.id)) {
				t.Errorf("no progress line for share %d:\n%s", i+1, progress)
			}
			continue
		}
		if !c.merge || c.prompt != MergePrompt(mergedProps(t, res, f, g)) {
			t.Error("the last session is not the merge of the shares' checked proposals")
		}
		for _, src := range []string{"### Docs", "### Pull requests", g.PRs[0].Body, g.Docs[0].Body} {
			if strings.Contains(c.prompt, src) {
				t.Errorf("the merge read a source: %.40q", src)
			}
		}
	}
	readOnce(t, g, f.calls)
	for i, c := range f.calls[:k] {
		for _, d := range g.Docs {
			if !strings.Contains(c.prompt, d.Body) {
				t.Errorf("share %d does not read doc %s whole", i+1, d.Path)
			}
		}
	}

	// The result: the merge's proposal, without the links no share gave.
	if len(res.Proposal.Capabilities) != 1 || res.Proposal.Capabilities[0].Name != "everything" {
		t.Fatalf("proposal: %+v", res.Proposal)
	}
	if got := strings.Join(res.Proposal.Capabilities[0].ShownBy, " "); got != strings.Join(f.gave, " ") {
		t.Errorf("shown by %q, want what the shares gave, %q", got, strings.Join(f.gave, " "))
	}
	for i, want := range []string{"#2", "#999999"} {
		if len(res.Dropped) <= i || res.Dropped[i].Value != want || res.Dropped[i].Reason != "no share gave it" || res.Dropped[i].What != mapping.WhatRef {
			t.Errorf("drop %d: %+v, want %s as no share gave it", i, res.Dropped, want)
		}
	}
	if len(res.Shares) != k || res.Merge == nil || res.Merge.SessionID != ids[k] || len(res.Merge.Read.Sources) != 0 {
		t.Fatalf("shares %d, merge %+v", len(res.Shares), res.Merge)
	}
	for i, s := range res.Shares {
		if s.SessionID != ids[i] || s.Model != "fake-1" || s.PromptTokens != reading.Tokens(f.calls[i].prompt) || s.Read.Tokens != laid[i].Report.Tokens {
			t.Errorf("share %d: %+v", i+1, s)
		}
	}
	n := int64(k + 1)
	if res.SessionID != ids[k] || res.Model != "fake-1" || res.PromptTokens != promptTokens ||
		res.Tokens != (adapter.Tokens{Input: 100 * n, CachedInput: 10 * n, Output: 5 * n}) || res.Read.Tokens != in.Report.Tokens {
		t.Errorf("result: session %s, prompt %d (want %d), tokens %+v", res.SessionID, res.PromptTokens, promptTokens, res.Tokens)
	}

	// Stored once, as mapping.Propose stores, with every session.
	if len(st.put) != 1 {
		t.Fatalf("stored %d proposals", len(st.put))
	}
	p := st.put[0]
	if !p.GatherAt.Equal(gatherAt) || !p.At.Equal(nowAt) || p.SessionID != ids[k] || p.Model != "fake-1" || p.Input != 100*n || p.CachedInput != 10*n || p.Output != 5*n {
		t.Errorf("stored: %+v", p)
	}
	var stored mapping.Stored
	if err := json.Unmarshal(p.Body, &stored); err != nil {
		t.Fatal(err)
	}
	if strings.Join(stored.Sessions, " ") != strings.Join(ids, " ") || strings.Join(stored.Ran(p.SessionID), " ") != strings.Join(ids, " ") {
		t.Errorf("stored sessions %v, want the %d shares' then the merge's, %v", stored.Sessions, k, ids)
	}
	if len(stored.Read.Shares) != k || stored.Read.Merge == nil || stored.Read.PromptTokens != promptTokens || stored.Read.SourceTokens != in.Report.Tokens {
		t.Errorf("stored read: %+v", stored.Read)
	}
	if len(stored.Proposal.Capabilities) != 1 || len(stored.Dropped) != 2 || stored.Malformed == nil {
		t.Errorf("stored proposal %+v, dropped %+v, malformed %v", stored.Proposal, stored.Dropped, stored.Malformed)
	}
	var text bytes.Buffer
	if err := mapping.WriteText(&text, res); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{fmt.Sprintf("in %d shares and a merge", k), fmt.Sprintf("Share %d of %d,", k, k), "The merge read only the shares' proposals"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("the proposal's text lacks %q", want)
		}
	}
	if !strings.Contains(progress, fmt.Sprintf("propose merges the proposals of its %d shares, in a prompt of ~%d tokens; session %s\n", k, reading.Tokens(f.calls[k].prompt), ids[k])) {
		t.Errorf("no progress line for the merge:\n%s", progress)
	}
}

// mergedProps is each share's checked proposal, as the fake answered it.
func mergedProps(t *testing.T, res mapping.Result, f *fakeAgent, g store.Gather) []mapping.Proposal {
	t.Helper()
	var ps []mapping.Proposal
	for i := range res.Shares {
		r, _, err := mapping.Parse(replyOf(fmt.Sprintf("part-%d", i), []string{f.gave[i]}))
		if err != nil {
			t.Fatal(err)
		}
		p, _ := mapping.Check(r, g)
		ps = append(ps, p)
	}
	return ps
}

func TestProposeHalvesWhenAShareIsRefused(t *testing.T) {
	g := large()
	room := smallWindow - mapping.ReplyReserve
	laid, _ := Lay(mapping.Read(g), room)
	f := &fakeAgent{window: smallWindow, script: func(n int, c call) (string, error, bool) {
		return "", tooLong(), n == 1 // the second share, once
	}}
	res, err, st, progress := propose(g, f)
	if err != nil {
		t.Fatal(err)
	}
	refused, shares := 0, 0
	for i, c := range f.calls {
		switch {
		case c.err != nil:
			refused++
		case !c.merge:
			shares++
			if i > 1 && reading.Tokens(c.prompt) > room/2 {
				t.Errorf("session %d, after the refusal, has a prompt of %d tokens, over half the room %d", i+1, reading.Tokens(c.prompt), room/2)
			}
		}
	}
	if refused != 1 || shares <= len(laid) || !f.calls[len(f.calls)-1].merge {
		t.Fatalf("%d refused calls and %d shares (Lay gave %d at the full room); want one refused call and more shares", refused, shares, len(laid))
	}
	if f.calls[0].prompt != mapping.Prompt(laid[0]) {
		t.Error("the share that ran before the refusal was not kept")
	}
	readOnce(t, g, f.calls)
	if len(res.Shares) != shares || res.Merge == nil || len(st.put) != 1 {
		t.Fatalf("result has %d shares, stored %d", len(res.Shares), len(st.put))
	}
	var stored mapping.Stored
	if err := json.Unmarshal(st.put[0].Body, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Sessions) != shares+1 {
		t.Errorf("stored %d sessions, want the %d shares that ran and the merge", len(stored.Sessions), shares)
	}
	for _, id := range stored.Sessions {
		if id == f.calls[1].id {
			t.Error("the refused session is recorded as one the proposal ran")
		}
	}
	if n := int64(shares + 1); res.Tokens.Input != 100*n {
		t.Errorf("tokens %+v, want those of the %d sessions that ran", res.Tokens, n)
	}
	want := fmt.Sprintf("share 2 of %d was refused as too long; the room for a share is halved, to ~%d tokens", len(laid), room/2)
	if strings.Count(progress, "refused as too long") != 1 || !strings.Contains(progress, want) {
		t.Errorf("progress lacks %q:\n%s", want, progress)
	}
}

func TestProposeGoesOnInSharesWhenTheOneSessionIsRefused(t *testing.T) {
	g := gatherOf(2, 1_000, 40, 3_000, 30)
	whole := mapping.Prompt(mapping.Read(g))
	for _, window := range []int{0, reading.Tokens(whole) + mapping.ReplyReserve} {
		t.Run(fmt.Sprintf("window %d", window), func(t *testing.T) {
			f := &fakeAgent{window: window, script: func(n int, c call) (string, error, bool) { return "", tooLong(), n == 0 }}
			res, err, st, progress := propose(g, f)
			if err != nil {
				t.Fatal(err)
			}
			if f.calls[0].prompt != whole || len(res.Shares) < 2 || len(f.calls) != len(res.Shares)+2 || len(st.put) != 1 {
				t.Fatalf("%d sessions, %d shares, stored %d", len(f.calls), len(res.Shares), len(st.put))
			}
			room := (reading.Tokens(whole) - mapping.ReplyReserve) / 2
			if window != 0 {
				room = (window - mapping.ReplyReserve) / 2
			}
			for _, c := range f.calls[1 : len(f.calls)-1] {
				if reading.Tokens(c.prompt) > room {
					t.Errorf("a share's prompt of %d tokens is over the halved room %d", reading.Tokens(c.prompt), room)
				}
			}
			readOnce(t, g, f.calls[1:])
			if !strings.Contains(progress, "propose's session was refused as too long; it goes on in shares") {
				t.Errorf("progress: %s", progress)
			}
		})
	}
}

func TestProposeARefusalThatHalvingCannotHelpEnds(t *testing.T) {
	// An agent that refuses everything: the run ends with a source too large
	// for a share, or the refusal, never an endless loop.
	f := &fakeAgent{window: smallWindow, script: func(int, call) (string, error, bool) { return "", tooLong(), true }}
	_, err, st, _ := propose(large(), f)
	var tl *TooLarge
	if !errors.As(err, &tl) && !errors.Is(err, adapter.ErrTooLong) {
		t.Fatalf("err = %v", err)
	}
	if len(st.put) != 0 || len(f.calls) > 20 {
		t.Fatalf("stored %d, %d sessions", len(st.put), len(f.calls))
	}
	seen := map[string]bool{}
	for _, c := range f.calls {
		if seen[c.prompt] {
			t.Fatal("a refused prompt was asked again")
		}
		seen[c.prompt] = true
	}
}

func TestProposeTooLargeStartsNoSession(t *testing.T) {
	g := large()
	g.PRs[4].Body = text(60_000, "huge")
	g.PRs[4].MergedAt = time.Date(2026, 3, 2, 15, 0, 0, 0, time.UTC)
	f := &fakeAgent{window: smallWindow}
	res, err, st, progress := propose(g, f)
	var tl *TooLarge
	if !errors.As(err, &tl) {
		t.Fatalf("err = %v, want a TooLarge", err)
	}
	t.Logf("sessions started: %d; error: %s", len(f.calls), err)
	if len(f.calls) != 0 || len(st.put) != 0 || progress != "" || len(res.Shares) != 0 {
		t.Fatalf("%d sessions started, %d stored, progress %q", len(f.calls), len(st.put), progress)
	}
	for _, want := range []string{"PR #5 takes about ", "Nothing is cut", "physa init --gather --since 2026-03-03"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %s", want, err)
		}
	}
}

func TestProposeUnreadableShareStoresNothing(t *testing.T) {
	f := &fakeAgent{window: smallWindow, script: func(n int, c call) (string, error, bool) { return "I could not decide.", nil, n == 1 }}
	res, err, st, _ := propose(large(), f)
	if err == nil || !strings.HasPrefix(err.Error(), "the session's output: share 2 of ") || !strings.Contains(err.Error(), "no fenced json block") {
		t.Fatalf("err = %v", err)
	}
	if len(st.put) != 0 || len(f.calls) != 2 || res.Tokens.Input != 200 {
		t.Fatalf("stored %d, sessions %d, tokens %+v", len(st.put), len(f.calls), res.Tokens)
	}
}

func TestProposeAFailedOrRefusedSessionStoresNothing(t *testing.T) {
	boom := fmt.Errorf("%w: rate limited", adapter.ErrFailed)
	for name, tc := range map[string]struct {
		fail func(n int, c call) bool
		err  error
		want string
	}{
		"a share fails":        {func(n int, c call) bool { return n == 0 }, boom, "rate limited"},
		"the merge fails":      {func(n int, c call) bool { return c.merge }, boom, "rate limited"},
		"the merge is refused": {func(n int, c call) bool { return c.merge }, tooLong(), "the merge of the shares' proposals: "},
	} {
		t.Run(name, func(t *testing.T) {
			merges := 0
			f := &fakeAgent{window: smallWindow, script: func(n int, c call) (string, error, bool) {
				if c.merge {
					merges++
				}
				return "", tc.err, tc.fail(n, c)
			}}
			_, err, st, _ := propose(large(), f)
			if !errors.Is(err, adapter.ErrFailed) || !strings.Contains(err.Error(), tc.want) || len(st.put) != 0 || merges > 1 {
				t.Fatalf("err = %v, stored %d, merges %d", err, len(st.put), merges)
			}
		})
	}
}

func TestProposeKeepsWhatASharesReplyLost(t *testing.T) {
	// The first share names a PR that was never gathered and one malformed
	// capability: both are reported, after the merge's own drops.
	bad := "```json\n" + `{"product": {"promise": "Teams hear of every event."},
 "capabilities": [{"name": "part-0", "promise": "It does a thing.", "shown_by": ["#1", "#4242"], "questions": []}, 7],
 "questions": []}` + "\n```\n"
	f := &fakeAgent{window: smallWindow, script: func(n int, c call) (string, error, bool) { return bad, nil, n == 0 }}
	f.gave = []string{"#1"}
	res, err, _, _ := propose(large(), f)
	if err != nil {
		t.Fatal(err)
	}
	last := res.Dropped[len(res.Dropped)-1]
	if len(res.Dropped) != 1 || last.Value != "#4242" || !strings.HasPrefix(last.Reason, "in share 1: ") {
		t.Errorf("dropped: %+v", res.Dropped)
	}
	if len(res.Malformed) != 1 || !strings.HasPrefix(res.Malformed[0].Reason, "in share 1: ") {
		t.Errorf("malformed: %+v", res.Malformed)
	}
}
