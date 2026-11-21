// Package learnsession finds learning's signals in the sessions behind a
// change (design 08 section 3): its answered asks, its interventions and its
// fixed findings. It joins a change to its sessions by the change's Session
// trailers, else by time through the slice records of the change's window,
// and reads each session's log only through the adapters' read-only logs.
// Nothing but those three kinds' text is passed on, and a log that cannot be
// read is a gap, never an error.
package learnsession

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/history"
	"github.com/dphaener/physa/internal/learn"
	"github.com/dphaener/physa/internal/vcs"
)

// The authors a Quote names, as far as the log shows.
const (
	// authorPerson is the author of an answered ask: a person answered it.
	authorPerson = "person"
	// authorMessage is the author of an intervention. That a message came
	// through the person's channel does not prove a person wrote it.
	authorMessage = "message in the session"
)

// idLen is how much of a session's id evidence shows.
const idLen = 8

// Reader finds the signals in sessions' logs, read through one or more
// adapters' Logs. It implements learn.Sessions. Each session's log is read
// once and kept, so a session behind several changes is read once.
type Reader struct {
	logs  []adapter.Logs
	mu    sync.Mutex
	cache map[string]result
}

// result is what reading one session's log gave.
type result struct {
	log    adapter.Log
	reason string // empty when the log was read
}

// New returns a Reader over logs, asked in order: the first that has a
// session's log wins.
func New(logs ...adapter.Logs) *Reader {
	return &Reader{logs: logs, cache: map[string]result{}}
}

var _ learn.Sessions = (*Reader)(nil)

// Signals returns the signals in the sessions behind change c: those its
// Session trailers name, matched by trailer, then those of recorded the
// trailers do not name, matched by time. Asks and interventions count when
// they fall in the window (after, c.Time]; a fixed finding counts where its
// passing report falls, or, when its own report marks it fixed, where that
// report falls. read lists each session whose log was read, once; gaps holds
// one entry, with Change left empty, for each session whose log is
// unavailable.
func (r *Reader) Signals(c vcs.Change, recorded []string, after time.Time) (signals []learn.Signal, read []string, gaps []learn.SessionGap) {
	for _, s := range joined(c, recorded) {
		res := r.read(s.id)
		if res.reason != "" {
			gaps = append(gaps, learn.SessionGap{Session: s.id, Reason: res.reason})
			continue
		}
		read = append(read, s.id)
		w := window{after: after, until: c.Time}
		for _, sig := range sessionSignals(s.id, res.log, w) {
			sig.Changes = []string{c.Short}
			sig.Session = s.id
			sig.MatchedBy = s.matchedBy
			signals = append(signals, sig)
		}
	}
	return signals, read, gaps
}

// session is one session joined to a change.
type session struct {
	id        string
	matchedBy string
}

// joined returns the sessions behind c: its trailers' sessions in the order
// written, then the recorded sessions they do not name, in the order given,
// each once.
func joined(c vcs.Change, recorded []string) []session {
	seen := map[string]bool{}
	var out []session
	for _, id := range history.Sessions(c.Message) {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, session{id, learn.MatchedByTrailer})
		}
	}
	for _, id := range recorded {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, session{id, learn.MatchedByTime})
		}
	}
	return out
}

// read returns session id's log from the first of r's logs that has it, or
// the first reason given when none has it. The outcome is kept, so each
// adapter is asked at most once per id.
func (r *Reader) read(id string) result {
	r.mu.Lock()
	defer r.mu.Unlock()
	if res, ok := r.cache[id]; ok {
		return res
	}
	res := result{reason: "no session log reader"}
	for i, l := range r.logs {
		log, err := l.ReadSession(id)
		if err == nil {
			res = result{log: log}
			break
		}
		if i == 0 {
			res.reason = err.Error()
		}
	}
	r.cache[id] = res
	return res
}

// window is the time a change's session events count in: after it, up to
// and including until. A zero after means open.
type window struct {
	after, until time.Time
}

// holds reports whether t falls in w.
func (w window) holds(t time.Time) bool {
	return (w.after.IsZero() || t.After(w.after)) && !t.After(w.until)
}

// sessionSignals returns the signals of session sid's log in window w, one
// per kind at or above its threshold, in the order answered asks,
// interventions, fixed findings. Changes, Session and MatchedBy are left for
// the caller.
func sessionSignals(sid string, log adapter.Log, w window) []learn.Signal {
	id := short(sid)
	var out []learn.Signal
	add := func(kind, rule, noun string, min int, quotes []learn.Quote) {
		if len(quotes) == 0 || len(quotes) < min {
			return
		}
		sortQuotes(quotes)
		out = append(out, learn.Signal{Kind: kind, Rule: rule,
			Evidence: []string{fmt.Sprintf("%s in session %s", count(len(quotes), noun), id)},
			Quotes:   quotes})
	}
	add(learn.KindAnsweredAsk, learn.RulePersonAnswered, "answered ask", learn.AnsweredAskMin, asks(log.Asks, w))
	add(learn.KindIntervention, learn.RuleMidRun, "intervention", learn.InterventionMin, interventions(log.Messages, w))
	add(learn.KindFixedFinding, learn.RuleFailThenPass, "fixed finding", learn.FixedFindingMin, fixedFindings(log.Reports, w))
	return out
}

// asks returns one Quote per answered ask in w.
func asks(list []adapter.Ask, w window) []learn.Quote {
	var out []learn.Quote
	for _, a := range list {
		if !w.holds(a.At) {
			continue
		}
		out = append(out, learn.Quote{Author: authorPerson, At: a.At, Text: a.Text,
			Options: append([]string(nil), a.Options...), Answer: a.Answer})
	}
	return out
}

// interventions returns one Quote per message mid-run in w.
func interventions(list []adapter.Message, w window) []learn.Quote {
	var out []learn.Quote
	for _, m := range list {
		if !w.holds(m.At) {
			continue
		}
		out = append(out, learn.Quote{Author: authorMessage, At: m.At, Text: m.Text})
	}
	return out
}

// fixedFindings returns one Quote per distinct fixed finding that counts in
// w. Reports are paired over the whole log first, so a failure before the
// window whose pass falls in it counts. A finding of a report that did not
// pass is fixed when a later report in the same group, with the same
// non-empty label, passed; it counts where that report falls. A finding its
// own report marks fixed counts where its report falls. Quotes with the same
// text are kept once, the earliest.
func fixedFindings(list []adapter.Report, w window) []learn.Quote {
	reports := append([]adapter.Report(nil), list...)
	sort.SliceStable(reports, func(i, j int) bool { return reports[i].At.Before(reports[j].At) })
	var out []learn.Quote
	for i, rep := range reports {
		pass, paired := passAfter(reports, i)
		for _, f := range rep.Findings {
			var at time.Time
			switch {
			case f.Fixed:
				at = rep.At
			case paired:
				at = pass
			default:
				continue
			}
			if !w.holds(at) {
				continue
			}
			out = append(out, learn.Quote{Author: rep.Agent, At: rep.At, Text: findingText(f)})
		}
	}
	sortQuotes(out)
	return dedupe(out)
}

// passAfter returns the time of the first report after reports[i] in the
// same group, with the same label, that passed. A report that passed, or
// has no label, is never paired.
func passAfter(reports []adapter.Report, i int) (time.Time, bool) {
	rep := reports[i]
	if rep.Passed || rep.Label == "" {
		return time.Time{}, false
	}
	for _, later := range reports[i+1:] {
		if later.Passed && later.Group == rep.Group && later.Label == rep.Label && later.At.After(rep.At) {
			return later.At, true
		}
	}
	return time.Time{}, false
}

// findingText writes a finding as "<level>: <text>", or its text alone when
// it has no level.
func findingText(f adapter.Finding) string {
	if f.Level == "" {
		return f.Text
	}
	return f.Level + ": " + f.Text
}

// sortQuotes orders quotes by time, then text.
func sortQuotes(q []learn.Quote) {
	sort.SliceStable(q, func(i, j int) bool {
		if !q[i].At.Equal(q[j].At) {
			return q[i].At.Before(q[j].At)
		}
		return q[i].Text < q[j].Text
	})
}

// dedupe keeps the first quote of each text, in order.
func dedupe(q []learn.Quote) []learn.Quote {
	seen := map[string]bool{}
	out := q[:0]
	for _, x := range q {
		if !seen[x.Text] {
			seen[x.Text] = true
			out = append(out, x)
		}
	}
	return out
}

// count writes n and noun, plural when n is not 1.
func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// short returns the first idLen characters of a session's id.
func short(id string) string {
	if len(id) > idLen {
		return id[:idLen]
	}
	return id
}
