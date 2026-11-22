// Package learnreport prints what learning found, as text for people or as
// one JSON object for tools. It reads a learn.Result and changes nothing, and
// it never prints the words quoted from a session.
package learnreport

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/dphaener/physa/internal/learn"
)

// shortLen is how many characters of a change id people are shown.
const shortLen = 7

// sessionLen is how many characters of a session id people are shown.
const sessionLen = 8

// WriteText writes a run's triage for people: what was learned from, how far
// the watermark moves, which sources could not be read, the changes with signals and their evidence,
// and the changes with nothing to learn.
func WriteText(w io.Writer, r learn.Result) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Learning from %s (%s), %d already learned.\n", plural(r.Changes, "change", "changes"), r.Range, r.AlreadyLearned)
	fmt.Fprintf(&b, "Learned through %s (was %s)\n", markID(r.Through), markID(r.Watermark))
	if r.Forge != "" {
		fmt.Fprintf(&b, "Forge: %s\n", r.Forge)
	} else {
		fmt.Fprintf(&b, "Forge: unavailable (%s): %s and %s signals not checked.\n",
			reasonOf(r, learn.KindCorrection), learn.KindCorrection, learn.KindArguedOptions)
	}
	if reason := reasonOf(r, learn.KindServedNotLoaded); reason != "" {
		fmt.Fprintf(&b, "Slice records: unavailable (%s).\n", reason)
	} else {
		fmt.Fprintf(&b, "Slice records: %d of %s had none and were not counted.\n", r.RecordsMissing, plural(r.Changes, "change", "changes"))
	}
	if reason := reasonOf(r, learn.KindAnsweredAsk); reason != "" {
		fmt.Fprintf(&b, "Session logs: unavailable (%s).\n", reason)
	} else {
		fmt.Fprintf(&b, "Session logs: %d read, %d unavailable.\n", r.SessionsRead, unreadSessions(r.SessionsUnavailable))
		for _, g := range r.SessionsUnavailable {
			fmt.Fprintf(&b, "  %s %s: %s\n", g.Change, cut(g.Session, sessionLen), g.Reason)
		}
	}
	for _, u := range r.Unavailable {
		fmt.Fprintf(&b, "Not checked: %s (%s).\n", u.Kind, u.Reason)
	}

	bets := 0
	for _, c := range r.Learn {
		if isBet(c) {
			bets++
		}
	}
	from := plural(len(r.Learn)-bets, "change", "changes")
	if bets > 0 {
		from += ", " + plural(bets, "bet", "bets")
	}
	fmt.Fprintf(&b, "\nSignals (%s):\n", from)
	for _, c := range r.Learn {
		fmt.Fprintf(&b, "  %s %s\n", c.Short, c.Subject)
		for _, s := range c.Signals {
			line := fmt.Sprintf("    %s [%s]", s.Kind, s.Rule)
			if s.Capability != "" {
				line += ": " + s.Capability
			}
			b.WriteString(line + "\n")
			for _, e := range s.Evidence {
				fmt.Fprintf(&b, "      %s\n", e)
			}
			if s.Session != "" {
				fmt.Fprintf(&b, "      session %s, matched by %s\n", cut(s.Session, sessionLen), s.MatchedBy)
			}
		}
	}
	fmt.Fprintf(&b, "\nNothing to learn (%d):\n", len(r.Nothing))
	for _, c := range r.Nothing {
		fmt.Fprintf(&b, "  %s %s\n", c.Short, c.Subject)
	}
	if len(r.Incomplete) > 0 {
		fmt.Fprintf(&b, "\nNot fully triaged (%d):\n", len(r.Incomplete))
		for _, c := range r.Incomplete {
			fmt.Fprintf(&b, "  %s %s (%s)\n", c.Short, c.Subject, c.Reason)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// WriteJSON writes a run's triage for tools, as one JSON object.
func WriteJSON(w io.Writer, r learn.Result) error {
	return json.NewEncoder(w).Encode(r)
}

// isBet reports whether c is a bet with a signal, listed as bet/<slug> with
// its idea, and not a change.
func isBet(c learn.ChangeSignals) bool {
	return len(c.Signals) > 0 && c.Signals[0].Kind == learn.KindBetPitch
}

// reasonOf returns why kind was not checked, or empty when it was.
func reasonOf(r learn.Result, kind string) string {
	for _, u := range r.Unavailable {
		if u.Kind == kind {
			return u.Reason
		}
	}
	return ""
}

// unreadSessions counts the distinct sessions among gaps.
func unreadSessions(gaps []learn.SessionGap) int {
	seen := map[string]bool{}
	for _, g := range gaps {
		seen[g.Session] = true
	}
	return len(seen)
}

// markID abbreviates a watermark for people; "none" when there is none.
func markID(id string) string {
	if id == "" {
		return "none"
	}
	return cut(id, shortLen)
}

// cut returns id's first n characters, or all of id when it is shorter.
func cut(id string, n int) string {
	if len(id) > n {
		return id[:n]
	}
	return id
}

// plural returns n with its noun in singular or plural form.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
