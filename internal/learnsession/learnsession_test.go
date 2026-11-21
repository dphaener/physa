package learnsession

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/learn"
	"github.com/dphaener/physa/internal/vcs"
)

// fakeLogs is an adapter.Logs over logs built in code. It counts the calls
// for each id.
type fakeLogs struct {
	logs   map[string]adapter.Log
	reason string
	calls  map[string]int
}

func newFake(reason string, logs ...adapter.Log) *fakeLogs {
	f := &fakeLogs{logs: map[string]adapter.Log{}, reason: reason, calls: map[string]int{}}
	for _, l := range logs {
		f.logs[l.Session] = l
	}
	return f
}

func (f *fakeLogs) ReadSession(id string) (adapter.Log, error) {
	f.calls[id]++
	if l, ok := f.logs[id]; ok {
		return l, nil
	}
	return adapter.Log{}, fmt.Errorf("%w: %s %s", adapter.ErrUnavailable, f.reason, id)
}

var base = time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)

// at is base plus m minutes.
func at(m int) time.Time { return base.Add(time.Duration(m) * time.Minute) }

// change is a change at minute 60 whose message carries trailers.
func change(trailers ...string) vcs.Change {
	msg := "feat(learning): Something\n\nBody.\n"
	if len(trailers) > 0 {
		msg += "\n"
		for _, t := range trailers {
			msg += t + "\n"
		}
	}
	return vcs.Change{ID: "abcdef0123456789", Short: "abcdef0", Subject: "feat(learning): Something", Message: msg, Time: at(60)}
}

// ofKind returns the signals of kind.
func ofKind(signals []learn.Signal, kind string) []learn.Signal {
	var out []learn.Signal
	for _, s := range signals {
		if s.Kind == kind {
			out = append(out, s)
		}
	}
	return out
}

// texts returns the quotes' texts.
func texts(q []learn.Quote) []string {
	var out []string
	for _, x := range q {
		out = append(out, x.Text)
	}
	return out
}

// fixed runs Signals for one session's reports in the window (after, 60].
func fixed(t *testing.T, after time.Time, reports ...adapter.Report) []learn.Signal {
	t.Helper()
	r := New(newFake("missing", adapter.Log{Session: "s1", Reports: reports}))
	signals, _, gaps := r.Signals(change("Session: s1"), nil, after)
	if len(gaps) != 0 {
		t.Fatalf("gaps = %#v", gaps)
	}
	return ofKind(signals, learn.KindFixedFinding)
}

func fail(m int, group, label, text string) adapter.Report {
	return adapter.Report{At: at(m), Agent: "dharness:verifier " + label, Group: group, Label: label, Verdict: "FAIL",
		Findings: []adapter.Finding{{Level: "major", Text: text}}}
}

func pass(m int, group, label string) adapter.Report {
	return adapter.Report{At: at(m), Agent: "dharness:verifier " + label, Group: group, Label: label, Verdict: "PASS", Passed: true}
}

func TestReaderIsSessions(t *testing.T) {
	var _ learn.Sessions = New()
}

func TestTrailerWithTwoSessions(t *testing.T) {
	f := newFake("missing",
		adapter.Log{Session: "s1", Messages: []adapter.Message{{At: at(10), Text: "use the glossary"}}},
		adapter.Log{Session: "s2", Messages: []adapter.Message{{At: at(20), Text: "smaller commits"}}})
	signals, read, gaps := New(f).Signals(change("Capability: learning", "Session: s1", "Session: s2"), nil, time.Time{})
	if len(gaps) != 0 {
		t.Fatalf("gaps = %#v", gaps)
	}
	if !reflect.DeepEqual(read, []string{"s1", "s2"}) {
		t.Errorf("read = %v", read)
	}
	if len(signals) != 2 {
		t.Fatalf("signals = %#v", signals)
	}
	for i, id := range []string{"s1", "s2"} {
		s := signals[i]
		if s.Kind != learn.KindIntervention || s.Rule != learn.RuleMidRun || s.Session != id || s.MatchedBy != learn.MatchedByTrailer {
			t.Errorf("signal %d = %#v", i, s)
		}
		if !reflect.DeepEqual(s.Changes, []string{"abcdef0"}) {
			t.Errorf("signal %d changes = %v", i, s.Changes)
		}
		if want := []string{"1 intervention in session " + id}; !reflect.DeepEqual(s.Evidence, want) {
			t.Errorf("signal %d evidence = %v", i, s.Evidence)
		}
		if len(s.Quotes) != 1 || s.Quotes[0].Author != "message in the session" {
			t.Errorf("signal %d quotes = %#v", i, s.Quotes)
		}
	}
}

func TestNoTrailerRecordedByTime(t *testing.T) {
	f := newFake("missing",
		adapter.Log{Session: "s1", Messages: []adapter.Message{{At: at(10), Text: "a"}}},
		adapter.Log{Session: "s2", Messages: []adapter.Message{{At: at(20), Text: "b"}}})
	signals, read, _ := New(f).Signals(change(), []string{"s1", "s2"}, time.Time{})
	if !reflect.DeepEqual(read, []string{"s1", "s2"}) {
		t.Errorf("read = %v", read)
	}
	if len(signals) != 2 {
		t.Fatalf("signals = %#v", signals)
	}
	for _, s := range signals {
		if s.MatchedBy != learn.MatchedByTime {
			t.Errorf("%s matched by %q, want time", s.Session, s.MatchedBy)
		}
	}
}

func TestTrailerAndRecordsJoin(t *testing.T) {
	f := newFake("missing",
		adapter.Log{Session: "s1", Messages: []adapter.Message{{At: at(10), Text: "a"}}},
		adapter.Log{Session: "s2", Messages: []adapter.Message{{At: at(20), Text: "b"}}})
	signals, read, _ := New(f).Signals(change("Session: s1"), []string{"s1", "s2"}, time.Time{})
	if !reflect.DeepEqual(read, []string{"s1", "s2"}) {
		t.Errorf("read = %v, want each id once", read)
	}
	got := map[string]string{}
	for _, s := range signals {
		got[s.Session] = s.MatchedBy
	}
	if want := map[string]string{"s1": "trailer", "s2": "time"}; !reflect.DeepEqual(got, want) {
		t.Errorf("matched by = %v, want %v", got, want)
	}
	if f.calls["s1"] != 1 || f.calls["s2"] != 1 {
		t.Errorf("calls = %v, want one each", f.calls)
	}
}

func TestReadListsEachIDOnce(t *testing.T) {
	f := newFake("missing", adapter.Log{Session: "s1"}, adapter.Log{Session: "s2"})
	_, read, _ := New(f).Signals(change("Session: s1", "Session: s1", "Session: s2"), []string{"s2", "s1", "s2", ""}, time.Time{})
	if !reflect.DeepEqual(read, []string{"s1", "s2"}) {
		t.Errorf("read = %v", read)
	}
}

func TestAnsweredAsk(t *testing.T) {
	f := newFake("missing", adapter.Log{Session: "0123456789abcdef", Asks: []adapter.Ask{
		{At: at(30), Agent: "dharness:planner", Text: "Which store?", Options: []string{"files", "sqlite"}, Answer: "files"},
		{At: at(20), Text: "Ship now?", Options: []string{"yes", "no"}, Answer: "yes"},
	}})
	signals, _, _ := New(f).Signals(change("Session: 0123456789abcdef"), nil, time.Time{})
	asks := ofKind(signals, learn.KindAnsweredAsk)
	if len(asks) != 1 {
		t.Fatalf("signals = %#v", signals)
	}
	s := asks[0]
	if s.Rule != learn.RulePersonAnswered {
		t.Errorf("rule = %q", s.Rule)
	}
	if want := []string{"2 answered asks in session 01234567"}; !reflect.DeepEqual(s.Evidence, want) {
		t.Errorf("evidence = %v", s.Evidence)
	}
	want := []learn.Quote{
		{Author: "person", At: at(20), Text: "Ship now?", Options: []string{"yes", "no"}, Answer: "yes"},
		{Author: "person", At: at(30), Text: "Which store?", Options: []string{"files", "sqlite"}, Answer: "files"},
	}
	if !reflect.DeepEqual(s.Quotes, want) {
		t.Errorf("quotes = %#v", s.Quotes)
	}
}

func TestWindowExcludesAfterIncludesChangeTime(t *testing.T) {
	f := newFake("missing", adapter.Log{Session: "s1", Asks: []adapter.Ask{
		{At: at(0), Text: "at after"},
		{At: at(60), Text: "at the change"},
		{At: at(61), Text: "after the change"},
	}, Messages: []adapter.Message{
		{At: at(0), Text: "message at after"},
		{At: at(60), Text: "message at the change"},
	}})
	signals, _, _ := New(f).Signals(change("Session: s1"), nil, at(0))
	asks := ofKind(signals, learn.KindAnsweredAsk)
	if len(asks) != 1 || !reflect.DeepEqual(texts(asks[0].Quotes), []string{"at the change"}) {
		t.Errorf("asks = %#v", asks)
	}
	msgs := ofKind(signals, learn.KindIntervention)
	if len(msgs) != 1 || !reflect.DeepEqual(texts(msgs[0].Quotes), []string{"message at the change"}) {
		t.Errorf("interventions = %#v", msgs)
	}
}

func TestNothingInWindowNoSignal(t *testing.T) {
	f := newFake("missing", adapter.Log{Session: "s1", Asks: []adapter.Ask{{At: at(90), Text: "later"}}})
	signals, read, _ := New(f).Signals(change("Session: s1"), nil, at(0))
	if len(signals) != 0 {
		t.Errorf("signals = %#v", signals)
	}
	if !reflect.DeepEqual(read, []string{"s1"}) {
		t.Errorf("read = %v", read)
	}
}

func TestFailThenPassSameLabel(t *testing.T) {
	got := fixed(t, time.Time{}, fail(10, "run1", "verify:W4.T1", "tests missing"), pass(20, "run1", "verify:W4.T1"))
	if len(got) != 1 {
		t.Fatalf("fixed = %#v", got)
	}
	s := got[0]
	if s.Rule != learn.RuleFailThenPass || s.MatchedBy != "trailer" || s.Session != "s1" {
		t.Errorf("signal = %#v", s)
	}
	if want := []string{"1 fixed finding in session s1"}; !reflect.DeepEqual(s.Evidence, want) {
		t.Errorf("evidence = %v", s.Evidence)
	}
	want := []learn.Quote{{Author: "dharness:verifier verify:W4.T1", At: at(10), Text: "major: tests missing"}}
	if !reflect.DeepEqual(s.Quotes, want) {
		t.Errorf("quotes = %#v", s.Quotes)
	}
}

func TestFailFailPassGivesOne(t *testing.T) {
	got := fixed(t, time.Time{},
		fail(10, "run1", "verify:W4.T1", "tests missing"),
		fail(15, "run1", "verify:W4.T1", "tests missing"),
		pass(20, "run1", "verify:W4.T1"))
	if len(got) != 1 || len(got[0].Quotes) != 1 || !got[0].Quotes[0].At.Equal(at(10)) {
		t.Errorf("fixed = %#v", got)
	}
}

func TestFailBeforeWindowPassInside(t *testing.T) {
	got := fixed(t, at(30), fail(10, "run1", "verify:W4.T1", "tests missing"), pass(40, "run1", "verify:W4.T1"))
	if len(got) != 1 || !reflect.DeepEqual(texts(got[0].Quotes), []string{"major: tests missing"}) {
		t.Errorf("fixed = %#v", got)
	}
}

func TestPassAfterWindowNotCounted(t *testing.T) {
	got := fixed(t, time.Time{}, fail(10, "run1", "verify:W4.T1", "tests missing"), pass(90, "run1", "verify:W4.T1"))
	if len(got) != 0 {
		t.Errorf("fixed = %#v", got)
	}
}

func TestFailAndPassInOtherGroupNotFixed(t *testing.T) {
	got := fixed(t, time.Time{}, fail(10, "run1", "verify:W4.T1", "tests missing"), pass(20, "run2", "verify:W4.T1"))
	if len(got) != 0 {
		t.Errorf("fixed = %#v", got)
	}
}

func TestFailAndPassOfOtherLabelNotFixed(t *testing.T) {
	got := fixed(t, time.Time{}, fail(10, "run1", "verify:W4.T1", "tests missing"), pass(20, "run1", "verify:W4.T2"))
	if len(got) != 0 {
		t.Errorf("fixed = %#v", got)
	}
}

func TestEmptyLabelsNeverPair(t *testing.T) {
	got := fixed(t, time.Time{}, fail(10, "", "", "tests missing"), pass(20, "", ""))
	if len(got) != 0 {
		t.Errorf("fixed = %#v", got)
	}
}

func TestPassWithMinorFindingNotFixed(t *testing.T) {
	p := pass(20, "run1", "verify:W4.T1")
	p.Findings = []adapter.Finding{{Level: "minor", Text: "a nit"}}
	got := fixed(t, time.Time{}, pass(10, "run1", "verify:W4.T1"), p, pass(30, "run1", "verify:W4.T1"))
	if len(got) != 0 {
		t.Errorf("fixed = %#v", got)
	}
}

func TestFindingMarkedFixed(t *testing.T) {
	marked := adapter.Report{At: at(30), Agent: "reviewer review:quality", Findings: []adapter.Finding{
		{Level: "major", Text: "unchecked error", Fixed: true},
		{Level: "minor", Text: "naming"},
	}}
	got := fixed(t, at(20), marked)
	if len(got) != 1 {
		t.Fatalf("fixed = %#v", got)
	}
	want := []learn.Quote{{Author: "reviewer review:quality", At: at(30), Text: "major: unchecked error"}}
	if !reflect.DeepEqual(got[0].Quotes, want) {
		t.Errorf("quotes = %#v", got[0].Quotes)
	}
	if got := fixed(t, at(40), marked); len(got) != 0 {
		t.Errorf("marked fixed before the window counted: %#v", got)
	}
}

func TestQuotesOrderedByTimeThenText(t *testing.T) {
	f := newFake("missing", adapter.Log{Session: "s1", Messages: []adapter.Message{
		{At: at(20), Text: "b"}, {At: at(10), Text: "z"}, {At: at(20), Text: "a"},
	}})
	signals, _, _ := New(f).Signals(change("Session: s1"), nil, time.Time{})
	if len(signals) != 1 || !reflect.DeepEqual(texts(signals[0].Quotes), []string{"z", "a", "b"}) {
		t.Errorf("signals = %#v", signals)
	}
}

func TestKindsInOrder(t *testing.T) {
	f := newFake("missing", adapter.Log{Session: "s1",
		Asks:     []adapter.Ask{{At: at(30), Text: "ask"}},
		Messages: []adapter.Message{{At: at(10), Text: "msg"}},
		Reports:  []adapter.Report{fail(5, "g", "l", "x"), pass(6, "g", "l")},
	})
	signals, _, _ := New(f).Signals(change("Session: s1"), nil, time.Time{})
	var kinds []string
	for _, s := range signals {
		kinds = append(kinds, s.Kind)
	}
	if want := []string{learn.KindAnsweredAsk, learn.KindIntervention, learn.KindFixedFinding}; !reflect.DeepEqual(kinds, want) {
		t.Errorf("kinds = %v", kinds)
	}
}

func TestCachingReadsEachIDOnce(t *testing.T) {
	f := newFake("missing", adapter.Log{Session: "s1"})
	r := New(f)
	r.Signals(change("Session: s1"), []string{"s2"}, time.Time{})
	r.Signals(change("Session: s1", "Session: s2"), nil, time.Time{})
	if f.calls["s1"] != 1 || f.calls["s2"] != 1 {
		t.Errorf("calls = %v, want one per id", f.calls)
	}
}

func TestFirstReaderThatHasItWins(t *testing.T) {
	first := newFake("not in first")
	second := newFake("not in second", adapter.Log{Session: "s1", Messages: []adapter.Message{{At: at(10), Text: "hi"}}})
	signals, read, gaps := New(first, second).Signals(change("Session: s1"), nil, time.Time{})
	if len(gaps) != 0 || !reflect.DeepEqual(read, []string{"s1"}) || len(signals) != 1 {
		t.Errorf("signals = %#v, read = %v, gaps = %#v", signals, read, gaps)
	}
	both := newFake("in both", adapter.Log{Session: "s1"})
	other := newFake("unused", adapter.Log{Session: "s1"})
	New(both, other).Signals(change("Session: s1"), nil, time.Time{})
	if other.calls["s1"] != 0 {
		t.Errorf("second reader asked after the first had it")
	}
}

func TestUnavailableFromBothIsGap(t *testing.T) {
	first := newFake("first reason")
	second := newFake("second reason")
	r := New(first, second)
	signals, read, gaps := r.Signals(change("Session: s1"), nil, time.Time{})
	if len(signals) != 0 || len(read) != 0 {
		t.Errorf("signals = %#v, read = %v", signals, read)
	}
	want := []learn.SessionGap{{Session: "s1", Reason: "session log unavailable: first reason s1"}}
	if !reflect.DeepEqual(gaps, want) {
		t.Errorf("gaps = %#v, want %#v", gaps, want)
	}
	_, _, again := r.Signals(change("Session: s1"), nil, time.Time{})
	if !reflect.DeepEqual(again, want) || first.calls["s1"] != 1 || second.calls["s1"] != 1 {
		t.Errorf("again = %#v, calls %v %v", again, first.calls, second.calls)
	}
}

func TestNoReaderIsGap(t *testing.T) {
	_, _, gaps := New().Signals(change("Session: s1"), nil, time.Time{})
	if len(gaps) != 1 || gaps[0].Session != "s1" || gaps[0].Reason == "" {
		t.Errorf("gaps = %#v", gaps)
	}
}

func TestNoSessionsNothingRead(t *testing.T) {
	f := newFake("missing")
	signals, read, gaps := New(f).Signals(change(), nil, time.Time{})
	if len(signals) != 0 || len(read) != 0 || len(gaps) != 0 || len(f.calls) != 0 {
		t.Errorf("signals = %#v, read = %v, gaps = %#v, calls = %v", signals, read, gaps, f.calls)
	}
}
