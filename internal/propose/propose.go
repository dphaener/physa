// Package propose is learning's fourth stage (design 08 section 2): every
// proposed map entry is checked against the signals it claims and, on a
// scratch copy of the map, with the map check; those that pass are kept and
// every other one is dropped with its reason, never silently. It writes
// the map only through ApplyAll, to the root it is given, and never in Check.
package propose

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/vcs"
)

// The kinds of proposal, and the section each names: a kind file's word,
// or the promise in index.md's front matter.
const (
	KindRule     = "rule"
	KindDecision = "decision"
	KindQuestion = "question"
	KindPromise  = "promise"
)

var sections = map[string]string{
	KindRule:     "Rules",
	KindDecision: "Decisions",
	KindQuestion: "Questions",
	KindPromise:  "promise",
}

var entryKinds = map[string]mapfile.Kind{
	KindRule:     mapfile.Rule,
	KindDecision: mapfile.Decision,
	KindQuestion: mapfile.Question,
}

// Options carries what the map check cannot learn from the map's files.
type Options struct {
	// OpenRepo opens the version control holding the repo, for commit
	// anchors. Nil leaves them unresolved, as in mapcheck.
	OpenRepo func(ctx context.Context, root string) (vcs.Repo, error)
	// PRExists tells whether a PR with this number exists. Nil checks #N
	// anchors for form only.
	PRExists func(ctx context.Context, n int) (bool, error)
}

// Kept is a proposal that passed every check.
type Kept struct {
	Proposal draft.Proposal `json:"proposal"`
	// Slug is the new entry's slug; empty for a promise.
	Slug string `json:"slug"`
}

// Dropped is a proposal, or an item of the session's output that was not
// one, left out with its reason.
type Dropped struct {
	// Proposal is the proposal as the session wrote it; zero for an item
	// that was not a proposal.
	Proposal draft.Proposal `json:"proposal"`
	// Raw is an item that was not a proposal, as the session wrote it.
	Raw    string `json:"raw,omitempty"`
	Reason string `json:"reason"`
}

// FromMalformed turns the items draft.Parse could not read as proposals into
// Dropped, each with its reason.
func FromMalformed(ms []draft.Malformed) []Dropped {
	var out []Dropped
	for _, m := range ms {
		out = append(out, Dropped{Raw: m.Raw, Reason: m.Reason})
	}
	return out
}

// Check checks each of ps, in order, against b and the map under root (the
// repo root), and returns those kept and those dropped: every proposal is in
// exactly one of the two. A proposal is first checked for what it claims (its
// kind and section, its capability, its signal and changes, its entry); then
// it is applied alone to a scratch copy of the map and kept only when the map
// check finds no problem the map did not have already, the capability's
// kind file holds exactly one more entry of its kind, and that entry's
// anchors cite one of its changes. Nothing under root is written.
func Check(ctx context.Context, root string, b draft.Batch, ps []draft.Proposal, o Options) ([]Kept, []Dropped) {
	var (
		kept    []Kept
		dropped []Dropped
	)
	drop := func(p draft.Proposal, format string, args ...any) {
		dropped = append(dropped, Dropped{Proposal: p, Reason: fmt.Sprintf(format, args...)})
	}
	if len(ps) == 0 {
		return kept, dropped
	}
	m, _, err := mapfile.Read(root)
	if err != nil {
		for _, p := range ps {
			drop(p, "the map cannot be read: %v", err)
		}
		return kept, dropped
	}
	co := once(o)
	base, err := mapcheck.CheckWith(ctx, root, mapcheck.Options{OpenRepo: co.OpenRepo, PRExists: co.PRExists})
	if err != nil {
		for _, p := range ps {
			drop(p, "the map cannot be checked: %v", err)
		}
		return kept, dropped
	}
	baseline := problemSet(base.Problems)
	for _, p := range ps {
		if reason := claims(m, b, p); reason != "" {
			drop(p, "%s", reason)
			continue
		}
		slug, reason := tryOn(ctx, root, m, p, baseline, co)
		if reason != "" {
			drop(p, "%s", reason)
			continue
		}
		kept = append(kept, Kept{Proposal: p, Slug: slug})
	}
	return kept, dropped
}

// claims returns why p does not hold against what it claims, or "" when it
// does: its kind and section, its capability, its signal and its changes, and
// its entry.
func claims(m *mapfile.Map, b draft.Batch, p draft.Proposal) string {
	want, ok := sections[p.Kind]
	if !ok {
		return fmt.Sprintf("kind %q is not one of rule, decision, question, promise", p.Kind)
	}
	if p.Section != want {
		return fmt.Sprintf("section %q does not match a %s: want %s", p.Section, p.Kind, want)
	}
	f := capability(m, p.Capability)
	if f == nil {
		return fmt.Sprintf("the map has no capability `%s`", p.Capability)
	}
	sig, ok := signal(b, p.Signal)
	if !ok {
		return "no signal in the batch supports it"
	}
	if len(p.Changes) == 0 {
		return "it is anchored to no change"
	}
	for _, c := range p.Changes {
		if !slices.Contains(sig, c) {
			return fmt.Sprintf("change `%s` is not one its signal is about", c)
		}
	}
	entry := strings.TrimSpace(p.Entry)
	if entry == "" {
		return "its entry is empty"
	}
	if p.Kind == KindPromise {
		if strings.ContainsAny(entry, "\r\n") {
			return "a promise is one line"
		}
		if entry == strings.TrimSpace(f.Promise) {
			return "the promise is the same as today's"
		}
	}
	return ""
}

// capability returns the capability of m named name, or nil.
func capability(m *mapfile.Map, name string) *mapfile.File {
	for _, f := range m.Capabilities {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// signal returns the changes of the batch's signal that ref names: the same
// kind and the same changes, in any order.
func signal(b draft.Batch, ref draft.SignalRef) ([]string, bool) {
	want := sorted(ref.Changes)
	for _, c := range b.Changes {
		for _, s := range c.Signals {
			if s.Kind == ref.Kind && slices.Equal(sorted(s.Changes), want) {
				return s.Changes, true
			}
		}
	}
	return nil, false
}

func sorted(xs []string) []string {
	out := slices.Clone(xs)
	slices.Sort(out)
	return out
}

// lineRef is a "path.md:line" reference inside a problem's message, and
// budgetRe a budget problem's line count: both move when lines are added
// elsewhere, so problems are compared without them.
var (
	lineRef  = regexp.MustCompile(`(\.md):\d+`)
	budgetRe = regexp.MustCompile(`^\d+ lines, budget is`)
)

// problemKey is what two problems are compared by: the path relative to the
// map, and the message without line numbers that adding lines would move.
func problemKey(p mapfile.Problem) string {
	msg := lineRef.ReplaceAllString(p.Message, "$1")
	msg = budgetRe.ReplaceAllString(msg, "lines, budget is")
	return p.Path + "\x00" + msg
}

// problemSet counts problems by key.
func problemSet(ps []mapfile.Problem) map[string]int {
	set := map[string]int{}
	for _, p := range ps {
		set[problemKey(p)]++
	}
	return set
}

// newProblems returns the problems of ps beyond those counted in baseline.
func newProblems(ps []mapfile.Problem, baseline map[string]int) []mapfile.Problem {
	left := map[string]int{}
	for k, n := range baseline {
		left[k] = n
	}
	var out []mapfile.Problem
	for _, p := range ps {
		k := problemKey(p)
		if left[k] > 0 {
			left[k]--
			continue
		}
		out = append(out, p)
	}
	return out
}

// once returns o with version control opened at most once and each PR
// number resolved at most once, so that checking many scratch copies asks
// no more of either than checking the map does. After the first resolver
// error every call fails the same way, as mapcheck stops resolving at one.
func once(o Options) Options {
	var out Options
	if o.OpenRepo != nil {
		var (
			mu     sync.Mutex
			opened bool
			repo   vcs.Repo
			err    error
		)
		out.OpenRepo = func(ctx context.Context, root string) (vcs.Repo, error) {
			mu.Lock()
			defer mu.Unlock()
			if !opened {
				opened = true
				repo, err = o.OpenRepo(ctx, root)
			}
			return repo, err
		}
	}
	if o.PRExists != nil {
		var (
			mu    sync.Mutex
			known = map[int]bool{}
			fail  error
		)
		out.PRExists = func(ctx context.Context, n int) (bool, error) {
			mu.Lock()
			defer mu.Unlock()
			if fail != nil {
				return false, fail
			}
			if v, ok := known[n]; ok {
				return v, nil
			}
			v, err := o.PRExists(ctx, n)
			if err != nil {
				fail = err
				return false, err
			}
			known[n] = v
			return v, nil
		}
	}
	return out
}
