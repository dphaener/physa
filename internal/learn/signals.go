package learn

import (
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
	// KindAsk is an ask or a person's intervention in a session. Not built:
	// it needs the engine's session records or team digests, and the engine
	// does not exist yet, so it is always reported as not checked.
	KindAsk = "ask"
)

// The rules that find each kind.
const (
	RuleReviewThenCommit = "review-then-commit"
	RuleLongDiscussion   = "long-discussion"
	RuleRevertMessage    = "revert-message"
	RuleServedNotLoaded  = "served-not-loaded"
)

// Why the kinds that need the engine are not checked.
const (
	reasonRecurringFinding = "needs the engine: the engine's records of findings or team digests (07 section 7)"
	reasonAsk              = "needs the engine: the engine's session records or team digests (07 section 7)"
)

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
	return Signal{Kind: KindArguedOptions, Rule: RuleLongDiscussion, Changes: []string{c.Short},
		Evidence: []string{fmt.Sprintf("%d comments from %s", len(pr.Comments), strings.Join(people, ", "))}}, true
}

// revertSignal finds a change that says it reverts another, naming the
// reverted change's subject when it is among the collected changes.
func revertSignal(repo vcs.Repo, c vcs.Change, collected []vcs.Change) (Signal, bool) {
	id, ok := repo.Reverts(c)
	if !ok {
		return Signal{}, false
	}
	line := "reverts " + id
	if repo.HasChange(id) {
		for _, other := range collected {
			if sameChange(other.ID, id) {
				line += ": " + other.Subject
				break
			}
		}
	}
	return Signal{Kind: KindRevert, Rule: RuleRevertMessage, Changes: []string{c.Short}, Evidence: []string{line}}, true
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
