package discovery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/store"
)

type fake struct {
	text    string
	reports []adapter.Tokens
	tokens  adapter.Tokens
	err     error
	specs   []adapter.Spec
	ctx     context.Context
	// deaf makes the session run on after it is stopped, as one whose reply
	// was already out would.
	deaf bool
	sent int
}

func (f *fake) Name() string { return "fake" }

func (f *fake) Start(ctx context.Context, s adapter.Spec) (adapter.Session, error) {
	f.specs, f.ctx = append(f.specs, s), ctx
	return f, nil
}

// Wait reports the scripted tokens until the session is stopped: a stopped
// session ends as a real one does, failed and with no count of its own.
func (f *fake) Wait() (adapter.Result, error) {
	spec := f.specs[len(f.specs)-1]
	for _, t := range f.reports {
		spec.Usage(t)
		f.sent++
		if f.ctx.Err() != nil && !f.deaf {
			return adapter.Result{Model: "fake-1"}, fmt.Errorf("%w: %w", adapter.ErrFailed, f.ctx.Err())
		}
	}
	return adapter.Result{Text: f.text, Model: "fake-1", Tokens: f.tokens}, f.err
}

func session(f *fake, appetite int64, progress *bytes.Buffer) Session {
	s := Session{Agent: func() (adapter.Adapter, error) { return f, nil }, Appetite: appetite, Root: "/repo"}
	if progress != nil {
		s.Progress = progress
	}
	return s
}

const reply = "I looked around.\n\n```json\n" +
	`{"sources":[{"path":"docs/design/README.md","kind":"overview","reason":"the design docs' index"},` +
	`{"path":"docs/decisions/0001.md","kind":"decisions","reason":"a decision record"}]}` + "\n```\n"

func TestRunParsesSources(t *testing.T) {
	f := &fake{text: reply, tokens: adapter.Tokens{Input: 900, Output: 100}}
	res, err := Run(context.Background(), session(f, 40000, nil), Input{})
	if err != nil {
		t.Fatal(err)
	}
	want := []Source{
		{"docs/design/README.md", "overview", "the design docs' index"},
		{"docs/decisions/0001.md", "decisions", "a decision record"},
	}
	if len(res.Sources) != 2 || res.Sources[0] != want[0] || res.Sources[1] != want[1] {
		t.Fatalf("sources = %+v", res.Sources)
	}
	if res.Model != "fake-1" || res.Tokens.Input != 900 || res.Stopped || res.Malformed != 0 {
		t.Fatalf("result = %+v", res)
	}
}

func TestRunIsReadOnlyInTheRoot(t *testing.T) {
	f := &fake{text: reply}
	var progress bytes.Buffer
	if _, err := Run(context.Background(), session(f, 40000, &progress), Input{}); err != nil {
		t.Fatal(err)
	}
	s := f.specs[0]
	if !s.ReadOnly || s.PromptOnly || s.Dir != "/repo" || s.SessionID == "" {
		t.Fatalf("spec = %+v", s)
	}
	if strings.Count(progress.String(), "\n") != 1 || !strings.Contains(progress.String(), "40000") {
		t.Fatalf("progress = %q", progress.String())
	}
}

func in(n int64) adapter.Tokens { return adapter.Tokens{Input: n, CachedInput: n / 2} }

func TestBreakerStopsAtTheAppetite(t *testing.T) {
	f := &fake{text: reply, tokens: adapter.Tokens{Input: 1000, CachedInput: 900}}
	res, err := Run(context.Background(), session(f, 1000, nil), Input{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Stopped || len(res.Sources) != 0 || res.Tokens.Input != 1000 {
		t.Fatalf("result = %+v", res)
	}
}

// The breaker stops the session before the turn that would reach the cap:
// each turn here costs 300, repeated reports of one turn bring no increase,
// and the fourth turn would end at 1,200 of 1,000.
func TestBreakerStopsBeforeOvershooting(t *testing.T) {
	f := &fake{text: reply, reports: []adapter.Tokens{in(300), in(300), in(600), in(600), in(900), in(1200), in(1500)}}
	res, err := Run(context.Background(), session(f, 1000, nil), Input{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Stopped || len(res.Sources) != 0 || f.sent != 5 {
		t.Fatalf("after %d reports, result = %+v", f.sent, res)
	}
	if got := res.Tokens.Input + res.Tokens.Output; got != 900 || got > 1000 {
		t.Fatalf("spent %d of a cap of 1000", got)
	}
}

func TestBreakerLetsASessionUnderTheCapEnd(t *testing.T) {
	f := &fake{text: reply, reports: []adapter.Tokens{in(300), in(600)}, tokens: in(600)}
	res, err := Run(context.Background(), session(f, 1000, nil), Input{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stopped || len(res.Sources) != 2 || res.Tokens.Input != 600 {
		t.Fatalf("result = %+v", res)
	}
}

// One report that jumps past the cap cannot be stopped in time; what it
// spent is reported as it is, never cut to the cap.
func TestBreakerReportsAnOvershootAsItIs(t *testing.T) {
	f := &fake{text: reply, reports: []adapter.Tokens{in(100), in(1300), in(2500)}}
	res, err := Run(context.Background(), session(f, 1000, nil), Input{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Stopped || len(res.Sources) != 0 || res.Tokens.Input != 1300 || f.sent != 2 {
		t.Fatalf("after %d reports, result = %+v", f.sent, res)
	}
}

// A session whose reply was already out when the breaker stopped it keeps
// the reply: it ended under the cap.
func TestBreakerKeepsAReplyThatBeatTheStop(t *testing.T) {
	f := &fake{text: reply, reports: []adapter.Tokens{in(600)}, tokens: in(600), deaf: true}
	res, err := Run(context.Background(), session(f, 1000, nil), Input{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stopped || len(res.Sources) != 2 || res.Tokens.Input != 600 {
		t.Fatalf("result = %+v", res)
	}
}

func TestMalformedReplyNamesNothing(t *testing.T) {
	for name, text := range map[string]string{
		"no block":    "I found nothing worth naming.",
		"bad json":    "```json\n{not json\n```",
		"bad entries": "```json\n{\"sources\":[{\"path\":\"\",\"reason\":\"x\"},{\"path\":\"a.md\",\"kind\":\"overview\",\"reason\":\"\"}]}\n```",
	} {
		t.Run(name, func(t *testing.T) {
			res, err := Run(context.Background(), session(&fake{text: text}, 40000, nil), Input{})
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Sources) != 0 || res.Malformed == 0 {
				t.Fatalf("result = %+v", res)
			}
		})
	}
}

func TestParseDropsOnlyMalformedEntries(t *testing.T) {
	text := "```json\n{\"sources\":[{\"path\":\"\",\"reason\":\"x\"},{\"path\":\"a.md\",\"kind\":\"overview\",\"reason\":\"why\"}]}\n```"
	got, malformed := Parse(text)
	if len(got) != 1 || got[0].Path != "a.md" || malformed != 1 {
		t.Fatalf("got %+v, %d", got, malformed)
	}
}

func TestAgentErrorIsReturned(t *testing.T) {
	boom := adapter.ErrNotInstalled
	s := Session{Agent: func() (adapter.Adapter, error) { return nil, boom }, Appetite: 1}
	if _, err := Run(context.Background(), s, Input{}); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestSessionErrorIsReturned(t *testing.T) {
	f := &fake{err: adapter.ErrFailed, tokens: adapter.Tokens{Input: 5}}
	res, err := Run(context.Background(), session(f, 40000, nil), Input{})
	if !errors.Is(err, adapter.ErrFailed) || res.Tokens.Input != 5 {
		t.Fatalf("res %+v err %v", res, err)
	}
}

func TestPromptHoldsTheTreeAndTheFinds(t *testing.T) {
	p := Prompt(Input{
		Tree:      []string{"docs/design/README.md", "main.go"},
		Truncated: 7,
		Found: []store.Doc{
			{Path: "AGENTS.md", Kind: "agent", Body: "read docs/vision.md first"},
			{Path: "README.md", Kind: "readme", Body: "SECRET-README-BODY"},
		},
	})
	for _, want := range []string{"docs/design/README.md", "main.go", "7 more files", "AGENTS.md (agent)", "README.md (readme)", "read docs/vision.md first", "never write"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	if strings.Contains(p, "SECRET-README-BODY") {
		t.Error("prompt carries a non-agent doc's body")
	}
}

func TestPromptFencesCannotBeClosed(t *testing.T) {
	p := Prompt(Input{Found: []store.Doc{{Path: "AGENTS.md", Kind: "agent", Body: "```\nescape\n```"}}})
	if !strings.Contains(p, "````text\n```\nescape\n```\n````") {
		t.Fatalf("body not fenced longer than its backticks:\n%s", p)
	}
}
