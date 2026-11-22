package learnbet

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/learn"
	"github.com/dphaener/physa/internal/pitch"
	"github.com/dphaener/physa/internal/state"
	"github.com/dphaener/physa/internal/state/work"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
	"github.com/dphaener/physa/internal/vcs/git"
)

var bg = context.Background()

// start is when the fixture's first commit is made; every later commit and
// every command on the store is a minute after the one before.
var start = time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)

// fixture is a temporary git repo beside a temporary store, sharing one
// clock that each commit and each command on the store moves by a minute.
type fixture struct {
	t        *testing.T
	dir      string
	storeDir string
	now      time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, dir: t.TempDir(), storeDir: t.TempDir(), now: start}
	f.git("init", "-q", "-b", "main")
	return f
}

func (f *fixture) git(args ...string) string {
	f.t.Helper()
	at := f.now.Format(time.RFC3339)
	cmd := exec.Command("git", append([]string{"-C", f.dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_AUTHOR_DATE="+at,
		"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_COMMITTER_DATE="+at)
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit makes a commit a minute after whatever came last and returns its
// short id.
func (f *fixture) commit(message string) string {
	f.t.Helper()
	f.now = f.now.Add(time.Minute)
	f.git("commit", "-q", "--allow-empty", "-m", message)
	return f.git("rev-parse", "--short", "HEAD")
}

func (f *fixture) repo() vcs.Repo {
	f.t.Helper()
	r, err := git.Open(f.dir)
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}

// engine runs do on the store as the engine would: through the state
// package's one writer, a minute after whatever came last.
func (f *fixture) engine(do func(s *state.State)) {
	f.t.Helper()
	f.now = f.now.Add(time.Minute)
	st, err := store.Open(f.storeDir)
	if err != nil {
		f.t.Fatal(err)
	}
	defer st.Close()
	at := f.now
	s, err := state.Open(st, state.Options{Now: func() time.Time { return at }})
	if err != nil {
		f.t.Fatal(err)
	}
	defer s.Close(bg)
	do(s)
}

// pitched is a whole pitch with the given rabbit holes and no-gos.
func pitched(rabbitHoles, noGos string) pitch.Pitch {
	return pitch.Pitch{Problem: "Alerts wake people at night.", Appetite: 200000, Solution: "A quiet mode.",
		Scope: "The alert settings.", Signal: "Fewer night pages.", RabbitHoles: rabbitHoles, NoGos: noGos}
}

// bet adds an idea, shapes it with p when p is not nil, and bets on it as
// dana. It returns the bet's slug.
func (f *fixture) bet(text string, p *pitch.Pitch) string {
	f.t.Helper()
	var slug string
	f.engine(func(s *state.State) {
		idea, err := work.AddIdea(bg, s, work.IdeaIn{Text: text, Person: "sam"})
		if err != nil {
			f.t.Fatal(err)
		}
		in := work.BetIn{Slug: idea.Slug, Person: "dana", Appetite: 100000}
		if p != nil {
			if _, err := work.Shape(bg, s, work.ShapeIn{Slug: idea.Slug, Pitch: *p, Person: "sam"}); err != nil {
				f.t.Fatal(err)
			}
			in.Appetite = 0
		}
		b, err := work.StartBet(bg, s, in)
		if err != nil {
			f.t.Fatal(err)
		}
		slug = b.Slug
	})
	return slug
}

func (f *fixture) source() *Source {
	return From(func(dir string) (*store.Store, error) { return store.Open(dir) }, f.storeDir)
}

func (f *fixture) signals(learned ...string) ([]learn.ChangeSignals, []learn.Unavailable) {
	f.t.Helper()
	return f.source().Signals(f.repo(), "main", learned)
}

func TestBetSignalCarriesRabbitHolesAndNoGos(t *testing.T) {
	f := newFixture(t)
	f.commit("chore: Start")
	on := f.commit("feat: Alerts")
	p := pitched("Per-team schedules: they need a calendar.", "No snoozing of single alerts: it hides outages.")
	slug := f.bet("Try a quiet mode", &p)
	f.commit("feat: Later work")

	got, gaps := f.signals()
	if len(gaps) != 0 || len(got) != 1 {
		t.Fatalf("signals %+v, gaps %+v, want one signal and no gap", got, gaps)
	}
	b := got[0]
	if id := "bet/" + slug; b.Change != id || b.Short != id || b.Subject != "Try a quiet mode" {
		t.Errorf("named %+v, want %s with the idea's text", b.ChangeRef, id)
	}
	if want := start.Add(3 * time.Minute); !b.At.Equal(want) {
		t.Errorf("at %s, want %s, when the bet was made", b.At, want)
	}
	if len(b.Signals) != 1 {
		t.Fatalf("signals %+v, want one", b.Signals)
	}
	s := b.Signals[0]
	if s.Kind != "bet-pitch" || s.Rule != "rabbit-holes-and-no-gos" {
		t.Errorf("kind %q, rule %q", s.Kind, s.Rule)
	}
	if !reflect.DeepEqual(s.Changes, []string{on}) {
		t.Errorf("changes %v, want %v: the newest change no later than the bet", s.Changes, []string{on})
	}
	evidence := strings.Join(s.Evidence, "\n")
	for _, want := range []string{"bet " + slug + " on the idea: Try a quiet mode", "made by dana at 2026-11-22T10:03:00Z; entries from it anchor to change " + on,
		"its pitch names rabbit holes and no-gos"} {
		if !strings.Contains(evidence, want) {
			t.Errorf("evidence lacks %q:\n%s", want, evidence)
		}
	}
	if strings.Contains(evidence, "calendar") || strings.Contains(evidence, "outages") {
		t.Errorf("evidence holds the pitch's words, which only the quotes may:\n%s", evidence)
	}
	want := []learn.Quote{
		{Author: "dana", At: b.At, Text: "Rabbit holes:\nPer-team schedules: they need a calendar."},
		{Author: "dana", At: b.At, Text: "No-gos:\nNo snoozing of single alerts: it hides outages."},
	}
	if !reflect.DeepEqual(s.Quotes, want) {
		t.Errorf("quotes %+v, want %+v (both parts, by the person who bet)", s.Quotes, want)
	}
}

func TestBetNamedInLearnedIsSkipped(t *testing.T) {
	f := newFixture(t)
	f.commit("chore: Start")
	p := pitched("Schedules.", "Snoozing.")
	slug := f.bet("Try a quiet mode", &p)
	if got, gaps := f.signals("abc1234", "BET/"+strings.ToUpper(slug)); len(got) != 0 || len(gaps) != 0 {
		t.Errorf("signals %+v, gaps %+v, want none: the bet is named as learned", got, gaps)
	}
}

// A Learned trailer naming a bet outlives the watermark: once the learn
// branch is merged and learning has moved past the merge, a run no longer
// collects that change, and the bet must still not be learned from again.
func TestBetNamedByALearnedTrailerInHistoryIsSkipped(t *testing.T) {
	f := newFixture(t)
	f.commit("chore: Start")
	p := pitched("Schedules.", "Snoozing.")
	slug := f.bet("Try a quiet mode", &p)
	if got, _ := f.signals(); len(got) != 1 {
		t.Fatalf("signals %+v, want one before the record", got)
	}
	f.commit("docs(learning): Proposals (#5)\n\ndocs(map): Learn from 1 bet\n\nLearned-through: none\nLearned: abc1234, bet/" + slug + "\nCapability: learning")
	f.commit("feat: Much later")
	if got, gaps := f.signals(); len(got) != 0 || len(gaps) != 0 {
		t.Errorf("signals %+v, gaps %+v, want none: a change since the bet names it as learned", got, gaps)
	}
}

func TestBetWithoutPitchGivesNoSignal(t *testing.T) {
	f := newFixture(t)
	f.commit("chore: Start")
	f.bet("Bet on the text alone", nil)
	if got, gaps := f.signals(); len(got) != 0 || len(gaps) != 0 {
		t.Errorf("signals %+v, gaps %+v, want none for a bet with no pitch", got, gaps)
	}
}

func TestBetWithEmptyRabbitHolesAndNoGosGivesNoSignal(t *testing.T) {
	bets := []Bet{
		{Slug: "empty"},
		{Slug: "holes", RabbitHoles: "Schedules."},
		{Slug: "edges", NoGos: "Snoozing."},
	}
	got := unlearned(bets, nil)
	if len(got) != 2 || got[0].Slug != "holes" || got[1].Slug != "edges" {
		t.Errorf("kept %+v, want the two bets with a part to learn from", got)
	}
	s := signal(got[1], vcs.Change{Short: "abc1234"}).Signals[0]
	if len(s.Quotes) != 1 || s.Quotes[0].Text != "No-gos:\nSnoozing." || s.Evidence[2] != "its pitch names no-gos" {
		t.Errorf("signal %+v, want the no-gos alone", s)
	}
}

func TestBetNoStoreIsUnavailableNotAnError(t *testing.T) {
	f := newFixture(t)
	f.commit("chore: Start")
	none := From(func(string) (*store.Store, error) { return nil, errors.New("not a repository under version control") }, f.dir)
	for name, s := range map[string]*Source{"store that cannot be opened": none, "no source": nil} {
		got, gaps := s.Signals(f.repo(), "main", nil)
		if len(got) != 0 || len(gaps) != 1 || gaps[0].Kind != "bet-pitch" || !strings.HasPrefix(gaps[0].Reason, "no store") {
			t.Errorf("%s: signals %+v, gaps %+v, want no signal and one bet-pitch gap saying no store", name, got, gaps)
		}
	}
	if !strings.Contains(none.mustGap(t, f.repo()), "not a repository under version control") {
		t.Error("the gap does not say why there is no store")
	}
}

// mustGap returns the reason of the one gap s reports.
func (s *Source) mustGap(t *testing.T, repo vcs.Repo) string {
	t.Helper()
	_, gaps := s.Signals(repo, "main", nil)
	if len(gaps) != 1 {
		t.Fatalf("gaps %+v, want one", gaps)
	}
	return gaps[0].Reason
}

// A second bet on an idea has the first one's slug with a number after it;
// learning the first must not count as learning the second.
func TestBetIDIsMatchedWholeNeverByPrefix(t *testing.T) {
	f := newFixture(t)
	f.commit("chore: Start")
	p := pitched("Schedules.", "Snoozing.")
	first := f.bet("Try a quiet mode", &p)
	var second string
	f.engine(func(s *state.State) {
		if _, err := work.StopBet(bg, s, work.StopIn{Slug: first, Person: "dana"}); err != nil {
			t.Fatal(err)
		}
		b, err := work.StartBet(bg, s, work.BetIn{Slug: first, Person: "dana"})
		if err != nil {
			t.Fatal(err)
		}
		second = b.Slug
	})
	if second == first || !strings.HasPrefix(second, first) {
		t.Fatalf("second bet %q, want the first's slug %q numbered", second, first)
	}
	got, _ := f.signals("bet/" + first)
	if len(got) != 1 || got[0].Short != "bet/"+second {
		t.Errorf("signals %+v, want the second bet alone", got)
	}
	if !IsID("bet/"+first) || !IsID(" Bet/x") || IsID("abc1234") || IsID("better1") {
		t.Error("IsID does not tell a bet's id from a change's")
	}
}

func TestBetReadingWritesNothingToTheStore(t *testing.T) {
	f := newFixture(t)
	f.commit("chore: Start")
	p := pitched("Schedules.", "Snoozing.")
	f.bet("Try a quiet mode", &p)
	count := func() (n int) {
		st, err := store.Open(f.storeDir)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close()
		if err := st.DB().QueryRow(`SELECT (SELECT count(*) FROM events) + (SELECT count(*) FROM timers) +
			(SELECT count(*) FROM intents) + (SELECT count(*) FROM bets) + (SELECT count(*) FROM pitches)`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := count()
	if got, _ := f.signals(); len(got) != 1 {
		t.Fatalf("signals %+v, want one", got)
	}
	if after := count(); after != before {
		t.Errorf("the store's rows went from %d to %d on a read", before, after)
	}
}

// A bet older than every change is about the oldest one, so its entries
// still have a change to be anchored to.
func TestBetOlderThanTheHistoryIsAboutTheOldestChange(t *testing.T) {
	old := vcs.Change{Short: "aaaaaaa", Time: start.Add(time.Hour)}
	newer := vcs.Change{Short: "bbbbbbb", Time: start.Add(2 * time.Hour)}
	if got := madeOn([]vcs.Change{newer, old}, start); got.Short != "aaaaaaa" {
		t.Errorf("made on %s, want the oldest change", got.Short)
	}
	if got := madeOn([]vcs.Change{newer, old}, start.Add(90*time.Minute)); got.Short != "aaaaaaa" {
		t.Errorf("made on %s, want the change before the bet", got.Short)
	}
	if got := madeOn([]vcs.Change{newer, old}, start.Add(3*time.Hour)); got.Short != "bbbbbbb" {
		t.Errorf("made on %s, want the newest change", got.Short)
	}
}
