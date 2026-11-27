package learn

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/history"
	"github.com/dphaener/physa/internal/vcs"
)

// Signal is one thing a change carries that the map may learn from (design 08
// section 3): its kind, the rule that found it, the changes it is about and
// the evidence the rule saw.
type Signal struct {
	Kind string `json:"kind"`
	Rule string `json:"rule"`
	// Changes are the short ids of the changes the signal is about: one for
	// the kinds found on a single change.
	Changes []string `json:"changes"`
	// Capability is the capability the signal is about, when it is about one.
	Capability string   `json:"capability,omitempty"`
	Evidence   []string `json:"evidence"`
	// Comments are the PR's comments, oldest first, on an argued-options
	// signal, for drafting; not in the JSON form.
	Comments []forge.Comment `json:"-"`
	// Reverted is the reverted change's subject and message, on a revert
	// signal whose reverted change is in the history, for drafting; not in
	// the JSON form.
	Reverted *ChangeText `json:"-"`
	// Session is the id of the session a session signal was found in.
	Session string `json:"session,omitempty"`
	// MatchedBy says how that session was joined to the change:
	// MatchedByTrailer or MatchedByTime.
	MatchedBy string `json:"matched_by,omitempty"`
	// Quotes are the words a session signal rests on, for drafting; not in
	// the JSON form, and never printed.
	Quotes []Quote `json:"-"`
}

// Quote is one thing said in a session that a session signal rests on.
type Quote struct {
	// Author is who said it, as far as the log shows: "person" on an
	// answered ask, the reporting agent's name and label on a fixed finding.
	// The log does not prove who typed a message mid-run, so an
	// intervention's author never names a person.
	Author string
	At     time.Time
	Text   string
	// Options are the options an ask offered; empty for anything else.
	Options []string
	// Answer is the person's chosen option or own words, on an ask.
	Answer string
}

// How a session was joined to a change.
const (
	// MatchedByTrailer is a session the change's Session trailer names.
	MatchedByTrailer = "trailer"
	// MatchedByTime is a session with a slice record in the change's window,
	// which no Session trailer names.
	MatchedByTime = "time"
)

// Sessions finds the signals in the sessions behind a change (design 08
// section 3). It reads the sessions the change's Session trailers name and,
// matched by time, those of recorded the trailers do not name.
type Sessions interface {
	// Signals returns the signals in the sessions behind change c, each with
	// its Changes, Session and MatchedBy set, the ids of the sessions whose
	// logs it read, and a gap for each session whose log is unavailable.
	// recorded are the distinct sessions of the slice records in c's window;
	// after is the window's start, exclusive (zero means open), so the
	// window is (after, c's time].
	Signals(c vcs.Change, recorded []string, after time.Time) (signals []Signal, read []string, gaps []SessionGap)
}

// SessionGap is a session behind a change whose log could not be read. It
// never makes the change not fully triaged: session logs are an extra
// (design 08 section 5).
type SessionGap struct {
	// Change is the change's short id; Run sets it.
	Change  string `json:"change"`
	Session string `json:"session"`
	Reason  string `json:"reason"`
}

// ChangeText is a change's subject and full message.
type ChangeText struct {
	Subject string
	Message string
}

// The kinds of Signal.
const (
	// KindCorrection is a review comment followed by a later commit in the
	// same PR: the review led to a fix, which may be a rule.
	KindCorrection = "correction"
	// KindArguedOptions is a long discussion among several people on a PR,
	// where options were argued: a decision, with the options rejected.
	KindArguedOptions = "argued-options"
	// KindRevert is a change that says it reverts another.
	KindRevert = "revert"
	// KindServedNotLoaded is a capability served by several changes whose
	// sessions never loaded its slice (design 07 section 4.1).
	KindServedNotLoaded = "served-not-loaded"
	// KindRecurringFinding is the same kind of finding recurring on one
	// capability (design 03 R4). Not built: the findings live in the
	// engine's records or team digests, and the engine does not exist yet,
	// so it is always reported as not checked.
	KindRecurringFinding = "recurring-finding"
	// KindAnsweredAsk is an ask in a session that the person answered: a
	// decision, with the options offered.
	KindAnsweredAsk = "answered-ask"
	// KindIntervention is a person's message sent mid-run in a session
	// (design 03 section 8): a correction, which may be a rule.
	KindIntervention = "intervention"
	// KindFixedFinding is a finding in a check's report that a later step
	// fixed: a later report of the same check in the same run passed, or
	// the finding is marked fixed.
	KindFixedFinding = "fixed-finding"
)

// The rules that find each kind.
const (
	RuleReviewThenCommit = "review-then-commit"
	RuleLongDiscussion   = "long-discussion"
	RuleRevertMessage    = "revert-message"
	RuleServedNotLoaded  = "served-not-loaded"
	RulePersonAnswered   = "person-answered"
	RuleMidRun           = "message-mid-run"
	RuleFailThenPass     = "report-then-pass"
)

// Why a kind is not checked when its source is missing.
const (
	reasonRecurringFinding = "needs the engine: the engine's records of findings or team digests (07 section 7)"
	reasonNoSessions       = "no session reader"
)

// sessionKinds are the kinds found in session logs.
var sessionKinds = []string{KindAnsweredAsk, KindIntervention, KindFixedFinding}

// evidenceRunes is how much of a comment's body evidence quotes.
const evidenceRunes = 80

// correctionSignal finds review comments on pr followed by a later commit of
// the PR.
func correctionSignal(c vcs.Change, pr forge.PR) (Signal, bool) {
	var evidence []string
	found := 0
	for _, cm := range pr.Comments {
		if cm.Kind != forge.KindReview {
			continue
		}
		for _, commit := range pr.Commits {
			if commit.At.After(cm.At) {
				found++
				evidence = append(evidence,
					fmt.Sprintf("review by %s at %s: %s", cm.Author, stamp(cm.At), clip(cm.Body)),
					fmt.Sprintf("then commit %s at %s", shortID(commit.ID), stamp(commit.At)))
				break
			}
		}
	}
	if found < CorrectionMinComments {
		return Signal{}, false
	}
	return Signal{Kind: KindCorrection, Rule: RuleReviewThenCommit, Changes: []string{c.Short}, Evidence: evidence}, true
}

// arguedSignal finds a discussion on pr long enough, among enough people, for
// options to have been argued.
func arguedSignal(c vcs.Change, pr forge.PR) (Signal, bool) {
	seen := map[string]bool{}
	var people []string
	for _, cm := range pr.Comments {
		if cm.Author != "" && !seen[cm.Author] {
			seen[cm.Author] = true
			people = append(people, cm.Author)
		}
	}
	if len(pr.Comments) < ArguedMinComments || len(people) < ArguedMinPeople {
		return Signal{}, false
	}
	sort.Strings(people)
	comments := append([]forge.Comment(nil), pr.Comments...)
	sort.SliceStable(comments, func(i, j int) bool { return comments[i].At.Before(comments[j].At) })
	return Signal{Kind: KindArguedOptions, Rule: RuleLongDiscussion, Changes: []string{c.Short},
		Evidence: []string{fmt.Sprintf("%d comments from %s", len(pr.Comments), strings.Join(people, ", "))},
		Comments: comments}, true
}

// revertSignal finds a change that says it reverts another, naming the
// reverted change's subject when it is among the collected changes.
func revertSignal(ctx context.Context, repo vcs.Repo, c vcs.Change, collected []vcs.Change) (Signal, bool) {
	id, ok := repo.Reverts(c)
	if !ok {
		return Signal{}, false
	}
	line := "reverts " + id
	var reverted *ChangeText
	if repo.HasChange(ctx, id) {
		if got, err := repo.Latest(ctx, id, 1); err == nil && len(got) > 0 && sameChange(got[0].ID, id) {
			reverted = &ChangeText{Subject: got[0].Subject, Message: got[0].Message}
		}
		for _, other := range collected {
			if sameChange(other.ID, id) {
				line += ": " + other.Subject
				break
			}
		}
	}
	return Signal{Kind: KindRevert, Rule: RuleRevertMessage, Changes: []string{c.Short}, Evidence: []string{line}, Reverted: reverted}, true
}

// servedNotLoaded finds, for each capability, the changes with slice records
// whose Capability trailer names it and whose sessions never loaded it. With
// enough of them, each such change carries the signal.
func servedNotLoaded(entries []*entry, capabilities []string) {
	done := map[string]bool{}
	for _, cp := range capabilities {
		if done[cp] {
			continue
		}
		done[cp] = true
		var hits []*entry
		for _, e := range entries {
			if e.loaded == nil || e.loaded[cp] || !serves(e.change, cp) {
				continue
			}
			hits = append(hits, e)
		}
		if len(hits) < UnloadedMinChanges {
			continue
		}
		s := Signal{Kind: KindServedNotLoaded, Rule: RuleServedNotLoaded, Capability: cp,
			Evidence: []string{fmt.Sprintf("served by %d changes whose sessions never loaded it", len(hits))}}
		for _, e := range hits {
			s.Changes = append(s.Changes, e.change.Short)
			s.Evidence = append(s.Evidence, e.change.Short+" "+e.change.Subject)
		}
		for _, e := range hits {
			e.signals = append(e.signals, s)
		}
	}
}

// serves reports whether c's Capability trailer names capability cp.
func serves(c vcs.Change, cp string) bool {
	for _, v := range history.Trailers(c.Message, "Capability") {
		if v == cp {
			return true
		}
	}
	return false
}

// stamp writes a time for evidence.
func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// clip returns a comment's body on one line, cut to evidenceRunes runes.
func clip(body string) string {
	r := []rune(strings.Join(strings.Fields(body), " "))
	if len(r) > evidenceRunes {
		r = r[:evidenceRunes]
	}
	return string(r)
}

// shortID abbreviates a forge commit id, which may not be in local history.
func shortID(id string) string {
	if len(id) > minSharedID {
		return id[:minSharedID]
	}
	return id
}
