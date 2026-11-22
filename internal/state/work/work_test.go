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

	"github.com/dphaener/physa/internal/pitch"
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
	for _, table := range []string{"ideas", "pitches", "bets", "events"} {
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

func full(appetite int64) pitch.Pitch {
	return pitch.Pitch{Problem: "People wait.", Appetite: appetite, Solution: "A quiet mode.", Scope: "The CLI only.",
		Signal: "Fewer pings.", RabbitHoles: "Terminal detection on Windows.", NoGos: "No new settings file."}
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
	for _, s := range []string{"", "Quiet", "quiet--mode", "-quiet", "quiet-", "quiet mode", "add", "list", "show", "shape", "stop", "plan", strings.Repeat("a", 41)} {
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
	if e.counts() != "ideas=1 pitches=0 bets=0 events=1" {
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

func TestShapeKeepsHistory(t *testing.T) {
	e := newEnv(t)
	e.add("Quiet mode", "")
	ev := e.oneEvent("pitch.shaped", func() error {
		_, err := Shape(bg, e.s, ShapeIn{Slug: "quiet-mode", Pitch: full(200_000), Person: "ana"})
		return err
	})
	if d := data(t, ev); ev.Subject != "pitch" || d["idea"] != "quiet-mode" || d["appetite"] != float64(200_000) {
		t.Errorf("event = %+v %s", ev, ev.Data)
	}
	second := full(500_000)
	second.Problem = "People still wait."
	var got IdeaDetail
	e.oneEvent("pitch.shaped", func() (err error) {
		got, err = Shape(bg, e.s, ShapeIn{Slug: "quiet-mode", Pitch: second, Person: "bo"})
		return err
	})
	if got.Pitches != 2 || got.Pitch == nil || got.Pitch.Pitch != second || got.Pitch.Person != "bo" || !got.Pitch.At.Equal(e.c.Now()) {
		t.Errorf("detail = %+v pitch %+v", got, got.Pitch)
	}
	if e.counts() != "ideas=1 pitches=2 bets=0 events=3" {
		t.Errorf("counts = %s", e.counts())
	}
}

func TestShapeRefusals(t *testing.T) {
	e := newEnv(t)
	e.add("Quiet mode", "")
	err := e.refused(ErrUnknownIdea, func() error {
		_, err := Shape(bg, e.s, ShapeIn{Slug: "nope", Pitch: full(1), Person: "ana"})
		return err
	})
	if !strings.Contains(err.Error(), `"nope"`) {
		t.Errorf("message should name the slug: %v", err)
	}
	half := full(0)
	half.NoGos = " "
	err = e.refused(ErrPitchIncomplete, func() error {
		_, err := Shape(bg, e.s, ShapeIn{Slug: "quiet-mode", Pitch: half, Person: "ana"})
		return err
	})
	if !strings.Contains(err.Error(), "Appetite, No-gos") {
		t.Errorf("message should name both parts: %v", err)
	}
	e.refused(ErrNoPerson, func() error { _, err := Shape(bg, e.s, ShapeIn{Slug: "quiet-mode", Pitch: full(1)}); return err })
}

func TestStartBetAppetiteFromPitch(t *testing.T) {
	e := newEnv(t)
	e.add("Quiet mode", "")
	if _, err := Shape(bg, e.s, ShapeIn{Slug: "quiet-mode", Pitch: full(200_000), Person: "ana"}); err != nil {
		t.Fatal(err)
	}
	var got BetDetail
	ev := e.oneEvent("bet.started", func() (err error) {
		got, err = StartBet(bg, e.s, BetIn{Slug: "quiet-mode", Person: "bo"})
		return err
	})
	now := e.c.Now()
	if got.Slug != "quiet-mode" || got.Idea != "quiet-mode" || got.Appetite != 200_000 || got.State != "active" || got.Person != "bo" ||
		!got.CreatedAt.Equal(now) || !got.StartedAt.Equal(now) || !got.EndedAt.IsZero() || got.Pitch == nil || got.Pitch.Pitch != full(200_000) {
		t.Errorf("bet = %+v pitch %+v", got.BetFacts, got.Pitch)
	}
	d := data(t, ev)
	if ev.Subject != "bet" || d["pitch"] != true || d["appetite"] != float64(200_000) ||
		d["rabbit_holes"] != "Terminal detection on Windows." || d["no_gos"] != "No new settings file." {
		t.Errorf("event = %+v %s", ev, ev.Data)
	}
}

func TestStartBetAppetiteGivenWinsAndNoPitch(t *testing.T) {
	e := newEnv(t)
	e.add("Quiet mode", "")
	e.add("Loud mode", "")
	if _, err := Shape(bg, e.s, ShapeIn{Slug: "quiet-mode", Pitch: full(200_000), Person: "ana"}); err != nil {
		t.Fatal(err)
	}
	got, err := StartBet(bg, e.s, BetIn{Slug: "quiet-mode", Appetite: 50_000, Person: "ana"})
	if err != nil || got.Appetite != 50_000 {
		t.Fatalf("bet = %+v, %v", got, err)
	}
	var bare BetDetail
	ev := e.oneEvent("bet.started", func() (err error) {
		bare, err = StartBet(bg, e.s, BetIn{Slug: "loud-mode", Appetite: 10_000, Person: "ana"})
		return err
	})
	if bare.Pitch != nil || bare.Appetite != 10_000 || data(t, ev)["pitch"] != false {
		t.Errorf("bet with no pitch = %+v, event %s", bare, ev.Data)
	}
}

func TestStartBetNoAppetite(t *testing.T) {
	e := newEnv(t)
	e.add("Quiet mode", "")
	e.refused(ErrNoAppetite, func() error { _, err := StartBet(bg, e.s, BetIn{Slug: "quiet-mode", Person: "ana"}); return err })
	e.refused(ErrNoAppetite, func() error {
		_, err := StartBet(bg, e.s, BetIn{Slug: "quiet-mode", Appetite: -5, Person: "ana"})
		return err
	})
}

func TestStartBetSecondActiveRefused(t *testing.T) {
	e := newEnv(t)
	e.add("Quiet mode", "")
	if _, err := StartBet(bg, e.s, BetIn{Slug: "quiet-mode", Appetite: 1000, Person: "ana"}); err != nil {
		t.Fatal(err)
	}
	err := e.refused(ErrActiveBet, func() error {
		_, err := StartBet(bg, e.s, BetIn{Slug: "quiet-mode", Appetite: 1000, Person: "ana"})
		return err
	})
	if !strings.Contains(err.Error(), `"quiet-mode"`) {
		t.Errorf("message should name the bet: %v", err)
	}
	// Once stopped, the idea can be bet on again, under a numbered slug.
	if _, err := StopBet(bg, e.s, StopIn{Slug: "quiet-mode", Person: "ana"}); err != nil {
		t.Fatal(err)
	}
	again, err := StartBet(bg, e.s, BetIn{Slug: "quiet-mode", Appetite: 1000, Person: "ana"})
	if err != nil || again.Slug != "quiet-mode-2" {
		t.Fatalf("second bet = %+v, %v", again, err)
	}
}

func TestStartBetUnknownIdea(t *testing.T) {
	e := newEnv(t)
	e.refused(ErrUnknownIdea, func() error { _, err := StartBet(bg, e.s, BetIn{Slug: "nope", Appetite: 1, Person: "ana"}); return err })
	e.refused(ErrNoPerson, func() error { _, err := StartBet(bg, e.s, BetIn{Slug: "nope", Appetite: 1}); return err })
}

func TestStopBet(t *testing.T) {
	e := newEnv(t)
	e.add("Quiet mode", "")
	e.add("Loud mode", "")
	for _, slug := range []string{"quiet-mode", "loud-mode"} {
		if _, err := StartBet(bg, e.s, BetIn{Slug: slug, Appetite: 1000, Person: "ana"}); err != nil {
			t.Fatal(err)
		}
	}
	var got BetDetail
	ev := e.oneEvent("bet.stopped", func() (err error) {
		got, err = StopBet(bg, e.s, StopIn{Slug: "quiet-mode", Reason: " out of time ", Person: "bo"})
		return err
	})
	if got.State != "stopped" || got.StopReason != "out of time" || !got.EndedAt.Equal(e.c.Now()) {
		t.Errorf("bet = %+v", got.BetFacts)
	}
	if d := data(t, ev); ev.Subject != "bet" || ev.Person != "bo" || d["slug"] != "quiet-mode" || d["reason"] != "out of time" {
		t.Errorf("event = %+v %s", ev, ev.Data)
	}
	got, err := StopBet(bg, e.s, StopIn{Slug: "loud-mode", Person: "bo"})
	if err != nil || got.StopReason != "stopped by bo" {
		t.Errorf("default reason = %q, %v", got.StopReason, err)
	}
}

func TestStopBetNotActiveUnknown(t *testing.T) {
	e := newEnv(t)
	e.add("Quiet mode", "")
	if _, err := StartBet(bg, e.s, BetIn{Slug: "quiet-mode", Appetite: 1000, Person: "ana"}); err != nil {
		t.Fatal(err)
	}
	if _, err := StopBet(bg, e.s, StopIn{Slug: "quiet-mode", Person: "ana"}); err != nil {
		t.Fatal(err)
	}
	err := e.refused(ErrNotActive, func() error { _, err := StopBet(bg, e.s, StopIn{Slug: "quiet-mode", Person: "ana"}); return err })
	if !strings.Contains(err.Error(), "stopped") {
		t.Errorf("message should say its state: %v", err)
	}
	err = e.refused(ErrUnknownBet, func() error { _, err := StopBet(bg, e.s, StopIn{Slug: "nope", Person: "ana"}); return err })
	if !strings.Contains(err.Error(), `"nope"`) {
		t.Errorf("message should name the slug: %v", err)
	}
	e.refused(ErrNoPerson, func() error { _, err := StopBet(bg, e.s, StopIn{Slug: "quiet-mode"}); return err })
}

func TestReadNewestFirst(t *testing.T) {
	e := newEnv(t)
	for _, text := range []string{"First", "Second", "Third"} {
		e.add(text, "")
	}
	if _, err := Shape(bg, e.s, ShapeIn{Slug: "first", Pitch: full(1000), Person: "ana"}); err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"first", "third"} {
		e.c.tick()
		if _, err := StartBet(bg, e.s, BetIn{Slug: slug, Appetite: 2000, Person: "ana"}); err != nil {
			t.Fatal(err)
		}
	}
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
	if f := ideas[2]; f.Pitches != 1 || f.Bet != "first" || f.BetState != "active" || ideas[1].Bet != "" {
		t.Errorf("facts = %+v", ideas)
	}
	bets, err := Bets(bg, e.s)
	if err != nil || len(bets) != 2 || bets[0].Slug != "third" || bets[1].Slug != "first" || bets[1].IdeaText != "First" {
		t.Errorf("bets = %+v, %v", bets, err)
	}
	d, err := Idea(bg, e.s, "first")
	if err != nil || d.Pitch == nil || len(d.Bets) != 1 || d.Bets[0].Slug != "first" {
		t.Errorf("idea = %+v, %v", d, err)
	}
	if d, err := Idea(bg, e.s, "second"); err != nil || d.Pitch != nil || len(d.Bets) != 0 {
		t.Errorf("unshaped idea = %+v, %v", d, err)
	}
	b, err := Bet(bg, e.s, "third")
	if err != nil || b.Pitch != nil || b.Idea != "third" || b.Appetite != 2000 || b.Spent != 0 {
		t.Errorf("bet = %+v, %v", b, err)
	}
	if after := e.counts(); after != before {
		t.Errorf("reads wrote: %s, then %s", before, after)
	}
}

func TestReadEmptyAndUnknown(t *testing.T) {
	e := newEnv(t)
	if ideas, err := Ideas(bg, e.s); err != nil || ideas == nil || len(ideas) != 0 {
		t.Errorf("ideas = %#v, %v", ideas, err)
	}
	if bets, err := Bets(bg, e.s); err != nil || bets == nil || len(bets) != 0 {
		t.Errorf("bets = %#v, %v", bets, err)
	}
	e.refused(ErrUnknownIdea, func() error { _, err := Idea(bg, e.s, "nope"); return err })
	e.refused(ErrUnknownBet, func() error { _, err := Bet(bg, e.s, "nope"); return err })
}
