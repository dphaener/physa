package discovery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/adapter"
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
		if spec.Usage != nil {
			spec.Usage(t)
		}
		f.sent++
		if f.ctx.Err() != nil && !f.deaf {
			return adapter.Result{Model: "fake-1"}, fmt.Errorf("%w: %w", adapter.ErrFailed, f.ctx.Err())
		}
	}
	return adapter.Result{Text: f.text, Model: "fake-1", Tokens: f.tokens}, f.err
}

func session(f *fake, progress *bytes.Buffer) Session {
	s := Session{Agent: func() (adapter.Adapter, error) { return f, nil }, Root: "/repo"}
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
	res, err := Run(context.Background(), session(f, nil))
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
	if res.Model != "fake-1" || res.Tokens.Input != 900 || res.Malformed != 0 {
		t.Fatalf("result = %+v", res)
	}
}

func TestRunIsReadOnlyInTheRoot(t *testing.T) {
	f := &fake{text: reply}
	var progress bytes.Buffer
	if _, err := Run(context.Background(), session(f, &progress)); err != nil {
		t.Fatal(err)
	}
	s := f.specs[0]
	if !s.ReadOnly || s.PromptOnly || s.Dir != "/repo" || s.SessionID == "" {
		t.Fatalf("spec = %+v", s)
	}
	if strings.Count(progress.String(), "\n") != 1 || strings.Contains(progress.String(), "up to") {
		t.Fatalf("progress = %q", progress.String())
	}
}

func in(n int64) adapter.Tokens { return adapter.Tokens{Input: n, CachedInput: n / 2} }

// TestNoCircuitBreaker checks that discovery is never stopped, however much
// it spends: init is never bounded (design 10 Q6).
func TestNoCircuitBreaker(t *testing.T) {
	f := &fake{text: reply, reports: []adapter.Tokens{in(300_000), in(900_000), in(5_000_000)}, tokens: in(5_000_000)}
	res, err := Run(context.Background(), session(f, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Sources) != 2 || res.Tokens.Input != 5_000_000 || f.specs[0].Usage != nil {
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
			res, err := Run(context.Background(), session(&fake{text: text}, nil))
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
	s := Session{Agent: func() (adapter.Adapter, error) { return nil, boom }}
	if _, err := Run(context.Background(), s); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestSessionErrorIsReturned(t *testing.T) {
	f := &fake{err: adapter.ErrFailed, tokens: adapter.Tokens{Input: 5}}
	res, err := Run(context.Background(), session(f, nil))
	if !errors.Is(err, adapter.ErrFailed) || res.Tokens.Input != 5 {
		t.Fatalf("res %+v err %v", res, err)
	}
}

func TestPromptNamesNoPaths(t *testing.T) {
	p := Prompt()
	for _, want := range []string{"never write", "You are given no list of files", "```json"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	for _, not := range []string{"## The repo's files", "## Already found", "Agent doc "} {
		if strings.Contains(p, not) {
			t.Errorf("prompt holds %q", not)
		}
	}
}
