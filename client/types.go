package client

import (
	"encoding/json"

	"github.com/dphaener/physa/internal/plan"
)

// RepoIn is the input of the methods that list: the repo to read.
type RepoIn struct {
	Repo string `json:"repo" desc:"The repo, as an absolute path to it or to a directory inside it."`
}

// SlugIn is the input of the methods that show one idea or pursuit.
type SlugIn struct {
	Repo string `json:"repo" desc:"The repo, as an absolute path to it or to a directory inside it."`
	Slug string `json:"slug" desc:"The idea's or pursuit's slug."`
}

// AddIn is idea.add's input.
type AddIn struct {
	Repo   string `json:"repo" desc:"The repo, as an absolute path to it or to a directory inside it."`
	Text   string `json:"text" desc:"The idea, in a few plain words."`
	Slug   string `json:"slug,omitempty" desc:"The slug to give it; made from the text when left out."`
	Person string `json:"person" desc:"Who adds the idea; it belongs to them."`
}

// StartIn is pursuit.start's input.
type StartIn struct {
	Repo     string    `json:"repo" desc:"The repo, as an absolute path to it or to a directory inside it."`
	Slug     string    `json:"slug" desc:"The idea to pursue."`
	Appetite string    `json:"appetite,omitempty" desc:"The appetite in tokens, such as 200k or 1.5m; left out for none, with no circuit breaker."`
	Signal   SignalOut `json:"signal" desc:"The signal the pursuit is read against; every part is required."`
	Person   string    `json:"person" desc:"The person who starts the pursuit."`
}

// StopIn is pursuit.stop's input.
type StopIn struct {
	Repo   string `json:"repo" desc:"The repo, as an absolute path to it or to a directory inside it."`
	Slug   string `json:"slug" desc:"The pursuit to stop."`
	Reason string `json:"reason,omitempty" desc:"Why it stops; \"stopped by <person>\" when left out."`
	Person string `json:"person" desc:"The person who stops the pursuit."`
}

// SignalOut is a pursuit's signal, in its four parts.
type SignalOut struct {
	Claim    string `json:"claim" desc:"What should change, in the team's words."`
	For      string `json:"for" desc:"What counts as for the claim."`
	Against  string `json:"against" desc:"What counts as against it."`
	Window   string `json:"window" desc:"When it is read: a date, YYYY-MM-DD, today or later when the pursuit starts."`
	Resolver string `json:"resolver" desc:"Who or what reports the result: a named person, or a named check."`
}

// IdeaOut is an idea and its facts. Pursuits is set only where the method
// says so.
type IdeaOut struct {
	Slug         string       `json:"slug" desc:"The idea's slug."`
	Text         string       `json:"text" desc:"The idea."`
	Person       string       `json:"person" desc:"Who added it."`
	CreatedAt    string       `json:"created_at" desc:"When it was added, RFC 3339 in UTC."`
	Pursuit      string       `json:"pursuit,omitempty" desc:"The slug of its latest pursuit; left out when it has none."`
	PursuitState string       `json:"pursuit_state,omitempty" desc:"The state of its latest pursuit: active, done or stopped."`
	Pursuits     []PursuitOut `json:"pursuits,omitempty" desc:"Its pursuits, newest first; set by idea.show."`
}

// PursuitOut is a pursuit. Signal is set only by pursuit.show, pursuit.start
// and pursuit.stop, and left out for a pursuit started before signals.
type PursuitOut struct {
	Slug       string        `json:"slug" desc:"The pursuit's slug."`
	Idea       string        `json:"idea" desc:"The slug of the idea it pursues."`
	IdeaText   string        `json:"idea_text" desc:"The idea."`
	Person     string        `json:"person" desc:"The person who started the pursuit."`
	Appetite   int64         `json:"appetite,omitempty" desc:"The appetite in tokens; left out when it has none."`
	Spent      int64         `json:"spent" desc:"The tokens spent so far."`
	State      string        `json:"state" desc:"active, done or stopped."`
	StopReason string        `json:"stop_reason,omitempty" desc:"Why it stopped; left out unless it did."`
	CreatedAt  string        `json:"created_at" desc:"When it was started, RFC 3339 in UTC."`
	StartedAt  string        `json:"started_at,omitempty" desc:"When it started, RFC 3339 in UTC."`
	EndedAt    string        `json:"ended_at,omitempty" desc:"When it ended, RFC 3339 in UTC; left out while active."`
	Signal     *SignalOut    `json:"signal,omitempty" desc:"The signal it is read against; left out for a pursuit started before signals."`
	Progress   *ProgressOut  `json:"progress,omitempty" desc:"Its progress as facts; set by pursuit.show."`
	Evidence   []EvidenceOut `json:"evidence,omitempty" desc:"The facts observed for the pursuit, newest first, at most 50; set by pursuit.show."`
	NeedsYou   []NeedOut     `json:"needs_you,omitempty" desc:"Its open Needs you items, newest first; set by pursuit.show."`
}

// PlanSetIn is plan.set's input.
type PlanSetIn struct {
	Repo    string          `json:"repo" desc:"The repo, as an absolute path to it or to a directory inside it."`
	Pursuit string          `json:"pursuit" desc:"The pursuit the plan is for."`
	Reason  string          `json:"reason,omitempty" desc:"Why the plan is revised: what the plan missed. Required from the second revision on."`
	Person  string          `json:"person" desc:"Who sets the plan."`
	Plan    json.RawMessage `json:"plan" desc:"The plan: {\"changes\":[...]}, each change with name, capability (one slug), what, acceptance, must_not and after."`
}

// PlanShowIn is plan.show's input.
type PlanShowIn struct {
	Repo    string `json:"repo" desc:"The repo, as an absolute path to it or to a directory inside it."`
	Pursuit string `json:"pursuit" desc:"The pursuit whose plan to show."`
	History bool   `json:"history,omitempty" desc:"Also return every revision, oldest first."`
}

// WorkIn is pursuit.work's input.
type WorkIn struct {
	Repo    string `json:"repo" desc:"The repo, as an absolute path to it or to a directory inside it."`
	Pursuit string `json:"pursuit" desc:"The pursuit."`
	Change  string `json:"change" desc:"The planned change the session works on."`
	Session string `json:"session" desc:"The session's id."`
}

// WorkOut is pursuit.work's output.
type WorkOut struct {
	Pursuit string `json:"pursuit" desc:"The pursuit."`
	Change  string `json:"change" desc:"The change the session now works on."`
}

// SpendIn is pursuit.spend's input.
type SpendIn struct {
	Repo    string `json:"repo" desc:"The repo, as an absolute path to it or to a directory inside it."`
	Pursuit string `json:"pursuit" desc:"The pursuit the tokens were spent on."`
	Change  string `json:"change,omitempty" desc:"The planned change they were spent on; left out for the pursuit itself."`
	Session string `json:"session" desc:"The session's id."`
	Tokens  int64  `json:"tokens" desc:"The session's running total of tokens on this pursuit and change, not an increase."`
}

// SpendOut is pursuit.spend's output.
type SpendOut struct {
	Spent    int64  `json:"spent" desc:"The tokens the pursuit has spent."`
	Appetite int64  `json:"appetite,omitempty" desc:"The pursuit's appetite in tokens, left out when it has none; it is never raised."`
	State    string `json:"state" desc:"active, done or stopped."`
	Warned   bool   `json:"warned" desc:"True when this report raised the notice at 80% of the appetite."`
	Broke    bool   `json:"broke" desc:"True when this report made the circuit breaker stop the pursuit."`
}

// RevisionOut is one revision of a plan.
type RevisionOut struct {
	N       int      `json:"n" desc:"The revision's number, from 1."`
	Reason  string   `json:"reason,omitempty" desc:"Why it was revised; empty for the first."`
	Person  string   `json:"person" desc:"Who set it."`
	At      string   `json:"at" desc:"When, RFC 3339 in UTC."`
	Changes []string `json:"changes" desc:"The names of its changes."`
}

// PlanOut is a pursuit's current plan.
type PlanOut struct {
	Pursuit string        `json:"pursuit" desc:"The pursuit."`
	N       int           `json:"n" desc:"The current revision's number."`
	Replans int           `json:"replans" desc:"How many times the plan was revised after the first."`
	Reason  string        `json:"reason,omitempty" desc:"Why the current revision was made; empty for the first."`
	Person  string        `json:"person" desc:"Who set the current revision."`
	At      string        `json:"at" desc:"When, RFC 3339 in UTC."`
	Changes []plan.Change `json:"changes" desc:"The planned changes."`
	History []RevisionOut `json:"history,omitempty" desc:"Every revision, oldest first; set when asked for."`
}

// LandedOut is a planned change that landed.
type LandedOut struct {
	Change string `json:"change" desc:"The change's name."`
	Commit string `json:"commit" desc:"The commit where it first landed on the default branch."`
	At     string `json:"at" desc:"When, RFC 3339 in UTC."`
}

// SpendPerOut is the tokens spent on one change.
type SpendPerOut struct {
	Change string `json:"change" desc:"The change's name; empty for the pursuit itself."`
	Tokens int64  `json:"tokens" desc:"The tokens spent."`
}

// ProgressOut is a pursuit's progress as facts, never one number.
type ProgressOut struct {
	Planned   []string      `json:"planned" desc:"The planned changes, in plan order."`
	Landed    []LandedOut   `json:"landed" desc:"The planned changes that landed on the default branch."`
	InWork    []string      `json:"in_work" desc:"Planned changes not landed that a session works on."`
	AsksOpen  int           `json:"asks_open" desc:"Asks open on the pursuit."`
	Spent     int64         `json:"spent" desc:"Tokens spent."`
	Appetite  int64         `json:"appetite,omitempty" desc:"The appetite in tokens; left out when the pursuit has none."`
	Replans   int           `json:"replans" desc:"How many times the plan was revised."`
	PerChange []SpendPerOut `json:"per_change" desc:"Spend per change, largest first."`
}

// NeedOut is an open Needs you item.
type NeedOut struct {
	Kind string `json:"kind" desc:"appetite or breaker."`
	Text string `json:"text" desc:"What the person needs to know."`
	At   string `json:"at" desc:"When it was raised, RFC 3339 in UTC."`
}

// EvidenceIn is evidence.list's input.
type EvidenceIn struct {
	Repo       string `json:"repo" desc:"The repo, as an absolute path to it or to a directory inside it."`
	Capability string `json:"capability,omitempty" desc:"Only facts for this capability's slug."`
	Pursuit    string `json:"pursuit,omitempty" desc:"Only facts for this pursuit's slug."`
	Limit      int    `json:"limit,omitempty" desc:"The most facts to return; 50 when left out, at most 500."`
}

// EvidenceOut is one observed fact.
type EvidenceOut struct {
	Kind       string `json:"kind" desc:"landed, reverted, lead_time, ci_passed, ci_failed or ci_passed_after_failing."`
	Commit     string `json:"commit,omitempty" desc:"The commit, as its first eight characters."`
	PR         int    `json:"pr,omitempty" desc:"The merged PR's number, for facts the forge showed."`
	Capability string `json:"capability,omitempty" desc:"The capability's slug, from the commit's Capability: trailer."`
	Pursuit    string `json:"pursuit,omitempty" desc:"The pursuit's slug, from the commit's Change: trailer."`
	Change     string `json:"change,omitempty" desc:"The planned change's name, from the commit's Change: trailer."`
	Idea       string `json:"idea,omitempty" desc:"The slug of the pursuit's idea."`
	At         string `json:"at" desc:"When it was observed to happen, RFC 3339 in UTC."`
	Source     string `json:"source" desc:"vcs or forge."`
	Seconds    *int64 `json:"seconds,omitempty" desc:"The lead time in seconds; set only for lead_time."`
	ByCommit   string `json:"by_commit,omitempty" desc:"For reverted, the revert's commit, as its first eight characters."`
}
