package work

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/signal"
	"github.com/dphaener/physa/internal/state"
	"github.com/dphaener/physa/internal/store"
)

var bg = context.Background()

// clock is the tests' fake clock; each command moves it with tick.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) tick() {
	c.mu.Lock()
	c.t = c.t.Add(time.Minute)
	c.mu.Unlock()
}

type env struct {
	t  *testing.T
	s  *state.State
	st *store.Store
	c  *clock
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	c := &clock{t: time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)}
	s, err := state.Open(st, state.Options{Now: c.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(bg) })
	return &env{t, s, st, c}
}

// counts is the rows in each work table and the events, as one string.
func (e *env) counts() string {
	e.t.Helper()
	var out []string
	for _, table := range []string{"ideas", "pursuits", "events"} {
		var n int
		if err := e.st.DB().QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, table+"="+strconv.Itoa(n))
	}
	return strings.Join(out, " ")
}

// events returns the events newest first.
func (e *env) events() []state.Event {
	e.t.Helper()
	evs, err := e.s.Events(bg, 500)
	if err != nil {
		e.t.Fatal(err)
	}
	return evs
}

// oneEvent runs f and checks it appended exactly one event of kind.
func (e *env) oneEvent(kind string, f func() error) state.Event {
	e.t.Helper()
	e.c.tick()
	before := len(e.events())
	if err := f(); err != nil {
		e.t.Fatalf("%s: %v", kind, err)
	}
	evs := e.events()
	if len(evs) != before+1 {
		e.t.Fatalf("%s appended %d events, want 1", kind, len(evs)-before)
	}
	if evs[0].Kind != kind {
		e.t.Fatalf("event kind = %q, want %q", evs[0].Kind, kind)
	}
	return evs[0]
}

// refused runs f, checks it fails with kind and that nothing was written.
func (e *env) refused(kind error, f func() error) error {
	e.t.Helper()
	before := e.counts()
	err := f()
	if !errors.Is(err, kind) {
		e.t.Fatalf("err = %v, want %v", err, kind)
	}
	var r *Refusal
	if !errors.As(err, &r) {
		e.t.Fatalf("err %v is not a Refusal", err)
	}
	if after := e.counts(); after != before {
		e.t.Fatalf("a refused command wrote: before %s, after %s", before, after)
	}
	return err
}

func (e *env) add(text, slug string) IdeaFacts {
	e.t.Helper()
	e.c.tick()
	f, err := AddIdea(bg, e.s, IdeaIn{Text: text, Slug: slug, Person: "ana"})
	if err != nil {
		e.t.Fatal(err)
	}
	return f
}

func sig() signal.Signal {
	return signal.Signal{Claim: "People get fewer pings.", For: "pings per person fall", Against: "pings stay the same",
		Window: "2026-12-15", Resolver: "ana"}
}

// pursue starts a pursuit of slug with appetite, or fails the test.
func (e *env) pursue(slug string, appetite int64) PursuitDetail {
	e.t.Helper()
	e.c.tick()
	d, err := StartPursuit(bg, e.s, PursueIn{Slug: slug, Appetite: appetite, Signal: sig(), Person: "ana"})
	if err != nil {
		e.t.Fatal(err)
	}
	return d
}

func data(t *testing.T, ev state.Event) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(ev.Data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMakeSlug(t *testing.T) {
	for text, want := range map[string]string{
		"Try a quiet mode":                  "try-a-quiet-mode",
		"  Ship it, NOW!  ":                 "ship-it-now",
		"one two three four five six seven": "one-two-three-four-five",
		"Café crème 2":                      "caf-cr-me-2",
		"!!! ???":                           "idea",
		"":                                  "idea",
		strings.Repeat("a", 50):             strings.Repeat("a", 40),
		"supercalifragilistic expialidocious again": "supercalifragilistic-expialidocious",
	} {
		if got := MakeSlug(text); got != want {
			t.Errorf("MakeSlug(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestValidSlugReserved(t *testing.T) {
	for _, s := range []string{"quiet-mode", "a", "v2-api"} {
		if err := ValidSlug(s); err != nil {
			t.Errorf("ValidSlug(%q) = %v", s, err)
		}
	}
	for _, s := range []string{"", "Quiet", "quiet--mode", "-quiet", "quiet-", "quiet mode", "add", "list", "show", "stop", "plan", "work", strings.Repeat("a", 41)} {
		if err := ValidSlug(s); !errors.Is(err, ErrSlugInvalid) {
			t.Errorf("ValidSlug(%q) = %v, want ErrSlugInvalid", s, err)
		}
	}
}

func TestNumberedStaysShort(t *testing.T) {
	base := strings.Repeat("a", 38) + "-b"
	if got := numbered(base, 12); len(got) > maxSlugLen || !strings.HasSuffix(got, "-12") || strings.Contains(got, "--") {
		t.Errorf("numbered = %q", got)
	}
}

func TestAddIdea(t *testing.T) {
	e := newEnv(t)
	var got IdeaFacts
	ev := e.oneEvent("idea.added", func() (err error) {
		got, err = AddIdea(bg, e.s, IdeaIn{Text: "  Try a quiet mode ", Person: " ana "})
		return err
	})
	if got.Slug != "try-a-quiet-mode" || got.Text != "Try a quiet mode" || got.Person != "ana" || !got.CreatedAt.Equal(e.c.Now()) {
		t.Errorf("idea = %+v", got)
	}
	if ev.Subject != "idea" || ev.Person != "ana" || data(t, ev)["slug"] != "try-a-quiet-mode" {
		t.Errorf("event = %+v %s", ev, ev.Data)
	}
	if e.counts() != "ideas=1 pursuits=0 events=1" {
		t.Errorf("counts = %s", e.counts())
	}
}

func TestAddIdeaSlugTakenNumbers(t *testing.T) {
	e := newEnv(t)
	for i, want := range []string{"quiet-mode", "quiet-mode-2", "quiet-mode-3"} {
		if got := e.add("Quiet mode", "").Slug; got != want {
			t.Errorf("add %d: slug = %q, want %q", i, got, want)
		}
	}
	if got := e.add("Something", "quiet-mode-4").Slug; got != "quiet-mode-4" {
		t.Errorf("chosen slug = %q", got)
	}
	if got := e.add("Quiet mode", "").Slug; got != "quiet-mode-5" {
		t.Errorf("after a chosen -4: slug = %q", got)
	}
}

func TestAddIdeaChosenSlugTaken(t *testing.T) {
	e := newEnv(t)
	e.add("Quiet mode", "")
	err := e.refused(ErrSlugTaken, func() error {
		_, err := AddIdea(bg, e.s, IdeaIn{Text: "Other", Slug: "quiet-mode", Person: "ana"})
		return err
	})
	if !strings.Contains(err.Error(), `"quiet-mode"`) {
		t.Errorf("message should name the slug: %v", err)
	}
}

func TestAddIdeaReservedMadeSlugNumbers(t *testing.T) {
	e := newEnv(t)
	for text, want := range map[string]string{"Add": "add-2", "list!": "list-2", "Stop": "stop-2", "plan": "plan-2"} {
		if got := e.add(text, "").Slug; got != want {
			t.Errorf("AddIdea(%q) slug = %q, want %q", text, got, want)
		}
	}
	e.refused(ErrSlugInvalid, func() error {
		_, err := AddIdea(bg, e.s, IdeaIn{Text: "x", Slug: "show", Person: "ana"})
		return err
	})
}

func TestAddIdeaRefusesEmpty(t *testing.T) {
	e := newEnv(t)
	e.refused(ErrNoText, func() error { _, err := AddIdea(bg, e.s, IdeaIn{Text: "  ", Person: "ana"}); return err })
	e.refused(ErrNoPerson, func() error { _, err := AddIdea(bg, e.s, IdeaIn{Text: "x", Person: " "}); return err })
	e.refused(ErrSlugInvalid, func() error {
		_, err := AddIdea(bg, e.s, IdeaIn{Text: "x", Slug: "Not OK", Person: "ana"})
		return err
	})
}

func TestStartPursuitRecordsItsSignal(t *testing.T) {
	e := newEnv(t)
	e.add("Quiet mode", "")
	var got PursuitDetail
	ev := e.oneEvent("pursuit.started", func() (err error) {
		got, err = StartPursuit(bg, e.s, PursueIn{Slug: "quiet-mode", Appetite: 200_000, Signal: sig(), Person: "bo"})
		return err
	})
	now := e.c.Now()
	if got.Slug != "quiet-mode" || got.Idea != "quiet-mode" || got.Appetite != 200_000 || got.State != "active" || got.Person != "bo" ||
		!got.CreatedAt.Equal(now) || !got.StartedAt.Equal(now) || !got.EndedAt.IsZero() || got.Signal == nil || *got.Signal != sig() {
		t.Errorf("pursuit = %+v signal %+v", got.PursuitFacts, got.Signal)
	}
	d := data(t, ev)
	s, _ := d["signal"].(map[string]any)
	if ev.Subject != "pursuit" || d["appetite"] != float64(200_000) || s["claim"] != "People get fewer pings." || s["window"] != "2026-12-15" ||
		s["for"] != "pings per person fall" || s["against"] != "pings stay the same" || s["resolver"] != "ana" {
		t.Errorf("event = %+v %s", ev, ev.Data)
	}
}

func TestStartPursuitWithNoAppetite(t *testing.T) {
	e := newEnv(t)
	e.add("Quiet mode", "")
	var got PursuitDetail
	ev := e.oneEvent("pursuit.started", func() (err error) {
		got, err = StartPursuit(bg, e.s, PursueIn{Slug: "quiet-mode", Signal: sig(), Person: "ana"})
		return err
	})
	if got.Appetite != 0 || got.State != "active" {
		t.Errorf("pursuit = %+v", got.PursuitFacts)
	}
	if d := data(t, ev); d["appetite"] != nil {
		t.Errorf("event appetite = %v, want null", d["appetite"])
	}
	e.refused(ErrBadAppetite, func() error {
		_, err := StartPursuit(bg, e.s, PursueIn{Slug: "quiet-mode", Appetite: -5, Signal: sig(), Person: "ana"})
		return err
	})
}

func TestStartPursuitNeedsTheWholeSignal(t *testing.T) {
	e := newEnv(t)
	e.add("Quiet mode", "")
	half := sig()
	half.Against, half.Resolver, half.Window = " ", "", "2026-11-01"
	err := e.refused(ErrSignal, func() error {
		_, err := StartPursuit(bg, e.s, PursueIn{Slug: "quiet-mode", Appetite: 1000, Signal: half, Person: "ana"})
		return err
	})
	for _, want := range []string{"the signal has problems: ", `empty "Against:" in Direction`, "window 2026-11-01 is before today", `empty part "Resolver"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q lacks %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "\n") {
		t.Errorf("message is not one line: %q", err)
	}
}

func TestStartPursuitSecondActiveRefused(t *testing.T) {
	e := newEnv(t)
	e.add("Quiet mode", "")
	e.pursue("quiet-mode", 1000)
	err := e.refused(ErrActivePursuit, func() error {
		_, err := StartPursuit(bg, e.s, PursueIn{Slug: "quiet-mode", Appetite: 1000, Signal: sig(), Person: "ana"})
		return err
	})
	if !strings.Contains(err.Error(), `"quiet-mode"`) {
		t.Errorf("message should name the pursuit: %v", err)
	}
	// Once stopped, the idea can be pursued again, under a numbered slug.
	if _, err := StopPursuit(bg, e.s, StopIn{Slug: "quiet-mode", Person: "ana"}); err != nil {
		t.Fatal(err)
	}
	if again := e.pursue("quiet-mode", 1000); again.Slug != "quiet-mode-2" {
		t.Fatalf("second pursuit = %+v", again)
	}
}

func TestStartPursuitUnknownIdea(t *testing.T) {
	e := newEnv(t)
	e.refused(ErrUnknownIdea, func() error {
		_, err := StartPursuit(bg, e.s, PursueIn{Slug: "nope", Appetite: 1, Signal: sig(), Person: "ana"})
		return err
	})
	e.refused(ErrNoPerson, func() error {
		_, err := StartPursuit(bg, e.s, PursueIn{Slug: "nope", Appetite: 1, Signal: sig()})
		return err
	})
}

func TestStopPursuit(t *testing.T) {
	e := newEnv(t)
	e.add("Quiet mode", "")
	e.add("Loud mode", "")
	e.pursue("quiet-mode", 1000)
	e.pursue("loud-mode", 1000)
	var got PursuitDetail
	ev := e.oneEvent("pursuit.stopped", func() (err error) {
		got, err = StopPursuit(bg, e.s, StopIn{Slug: "quiet-mode", Reason: " out of time ", Person: "bo"})
		return err
	})
	if got.State != "stopped" || got.StopReason != "out of time" || !got.EndedAt.Equal(e.c.Now()) {
		t.Errorf("pursuit = %+v", got.PursuitFacts)
	}
	if d := data(t, ev); ev.Subject != "pursuit" || ev.Person != "bo" || d["slug"] != "quiet-mode" || d["reason"] != "out of time" {
		t.Errorf("event = %+v %s", ev, ev.Data)
	}
	got, err := StopPursuit(bg, e.s, StopIn{Slug: "loud-mode", Person: "bo"})
	if err != nil || got.StopReason != "stopped by bo" {
		t.Errorf("default reason = %q, %v", got.StopReason, err)
	}
}

func TestStopPursuitNotActiveUnknown(t *testing.T) {
	e := newEnv(t)
	e.add("Quiet mode", "")
	e.pursue("quiet-mode", 1000)
	if _, err := StopPursuit(bg, e.s, StopIn{Slug: "quiet-mode", Person: "ana"}); err != nil {
		t.Fatal(err)
	}
	err := e.refused(ErrNotActive, func() error { _, err := StopPursuit(bg, e.s, StopIn{Slug: "quiet-mode", Person: "ana"}); return err })
	if !strings.Contains(err.Error(), "stopped") {
		t.Errorf("message should say its state: %v", err)
	}
	err = e.refused(ErrUnknownPursuit, func() error { _, err := StopPursuit(bg, e.s, StopIn{Slug: "nope", Person: "ana"}); return err })
	if !strings.Contains(err.Error(), `"nope"`) {
		t.Errorf("message should name the slug: %v", err)
	}
	e.refused(ErrNoPerson, func() error { _, err := StopPursuit(bg, e.s, StopIn{Slug: "quiet-mode"}); return err })
}

func TestReadNewestFirst(t *testing.T) {
	e := newEnv(t)
	for _, text := range []string{"First", "Second", "Third"} {
		e.add(text, "")
	}
	e.pursue("first", 2000)
	e.pursue("third", 0)
	before := e.counts()
	ideas, err := Ideas(bg, e.s)
	if err != nil {
		t.Fatal(err)
	}
	var slugs []string
	for _, i := range ideas {
		slugs = append(slugs, i.Slug)
	}
	if strings.Join(slugs, " ") != "third second first" {
		t.Errorf("ideas = %v", slugs)
	}
	if f := ideas[2]; f.Pursuit != "first" || f.PursuitState != "active" || ideas[1].Pursuit != "" {
		t.Errorf("facts = %+v", ideas)
	}
	all, err := Pursuits(bg, e.s)
	if err != nil || len(all) != 2 || all[0].Slug != "third" || all[0].Appetite != 0 || all[1].Slug != "first" || all[1].IdeaText != "First" {
		t.Errorf("pursuits = %+v, %v", all, err)
	}
	d, err := Idea(bg, e.s, "first")
	if err != nil || len(d.Pursuits) != 1 || d.Pursuits[0].Slug != "first" {
		t.Errorf("idea = %+v, %v", d, err)
	}
	if d, err := Idea(bg, e.s, "second"); err != nil || len(d.Pursuits) != 0 {
		t.Errorf("idea never pursued = %+v, %v", d, err)
	}
	p, err := Pursuit(bg, e.s, "first")
	if err != nil || p.Signal == nil || p.Idea != "first" || p.Appetite != 2000 || p.Spent != 0 {
		t.Errorf("pursuit = %+v, %v", p, err)
	}
	if after := e.counts(); after != before {
		t.Errorf("reads wrote: %s, then %s", before, after)
	}
}

func TestPursuitFromBeforeSignalsHasNone(t *testing.T) {
	e := newEnv(t)
	e.add("Quiet mode", "")
	if _, err := e.st.DB().Exec(`INSERT INTO pursuits (slug, idea_id, person, appetite_tokens, created_at) VALUES ('old', 1, 'ana', 500, '2026-11-01T10:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	p, err := Pursuit(bg, e.s, "old")
	if err != nil || p.Signal != nil || p.Appetite != 500 {
		t.Errorf("pursuit = %+v signal %v, %v; want no signal", p.PursuitFacts, p.Signal, err)
	}
}

func TestReadEmptyAndUnknown(t *testing.T) {
	e := newEnv(t)
	if ideas, err := Ideas(bg, e.s); err != nil || ideas == nil || len(ideas) != 0 {
		t.Errorf("ideas = %#v, %v", ideas, err)
	}
	if all, err := Pursuits(bg, e.s); err != nil || all == nil || len(all) != 0 {
		t.Errorf("pursuits = %#v, %v", all, err)
	}
	e.refused(ErrUnknownIdea, func() error { _, err := Idea(bg, e.s, "nope"); return err })
	e.refused(ErrUnknownPursuit, func() error { _, err := Pursuit(bg, e.s, "nope"); return err })
}
