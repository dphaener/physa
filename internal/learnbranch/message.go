package learnbranch

import (
	"fmt"
	"io"
	"strings"

	"github.com/dphaener/physa/internal/learn"
	"github.com/dphaener/physa/internal/propose"
	"github.com/dphaener/physa/internal/vcs"
)

// The trailers of a learning commit (design 08 section 6).
const (
	throughKey = "Learned-through"
	learnedKey = "Learned"
)

// noProposals is the body of a commit that records only the watermark.
const noProposals = "No proposals; the watermark advances."

// record is what one learning commit says.
type record struct {
	changes   int    // changes learned from
	watermark string // the watermark read, full id
	through   string // the watermark recorded, full id
	learned   []string
	// carried are the Learned ids of replayed changes that added nothing; the
	// commit keeps those its watermark does not pass.
	carried []string
	applied []propose.Kept
	// session is the draft session's id, empty when no session ran; Physa
	// started that session, so Physa writes its Session: trailer.
	session string
}

// message is the commit's message: its subject, one line per applied
// proposal, and the trailer block, which always holds Learned-through
// (learn.NoWatermark when there is nothing to learn through yet).
func (r record) message(w vcs.Repo) string {
	var b strings.Builder
	noun := "changes"
	if r.changes == 1 {
		noun = "change"
	}
	fmt.Fprintf(&b, "docs(map): Learn from %d %s\n\n", r.changes, noun)
	if len(r.applied) == 0 {
		b.WriteString(noProposals + "\n")
	}
	for _, k := range r.applied {
		b.WriteString(proposalLine(k) + "\n")
	}
	b.WriteString("\n")
	through := r.through
	if through == "" {
		through = learn.NoWatermark
	}
	fmt.Fprintf(&b, "%s: %s\n", throughKey, through)
	if ids := r.learnedIDs(w); len(ids) > 0 {
		fmt.Fprintf(&b, "%s: %s\n", learnedKey, strings.Join(ids, ", "))
	}
	if r.session != "" {
		fmt.Fprintf(&b, "Session: %s\n", r.session)
	}
	b.WriteString("Capability: learning\n")
	return b.String()
}

// learnedIDs are the run's own Learned ids, then each carried one that names
// a change the recorded watermark does not pass, each once.
func (r record) learnedIDs(w vcs.Repo) []string {
	ids := append([]string{}, r.learned...)
	for _, id := range r.carried {
		if contains(ids, id) {
			continue
		}
		if full, ok := w.Resolve(id); ok && r.through != "" && w.IsAncestor(full, r.through) {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

// proposalLine is the line of a commit's body for k:
// "- <kind> <slug, or the promise> in <capability>, from <signal> on <changes>".
func proposalLine(k propose.Kept) string {
	p := k.Proposal
	what := k.Slug
	if p.Kind == propose.KindPromise {
		what = fmt.Sprintf("%q", strings.TrimSpace(p.Entry))
	}
	return fmt.Sprintf("- %s %s in %s, from %s on %s",
		p.Kind, what, p.Capability, p.Signal.Kind, strings.Join(p.Changes, ", "))
}

// proposalLines returns the proposal lines of a learning commit's message:
// the body's lines that start with "- ".
func proposalLines(message string) []string {
	var out []string
	_, body, _ := strings.Cut(strings.ReplaceAll(message, "\r\n", "\n"), "\n")
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "- ") {
			out = append(out, line)
		}
	}
	return out
}

// WriteText writes, for people, what the run did to the branch: the lines
// printed after the Report.
func WriteText(w io.Writer, o Outcome) error {
	var b strings.Builder
	switch {
	case o.Report.Session == propose.SessionStopped:
		fmt.Fprintf(&b, "Nothing written to %s: the session stopped at the appetite.\n", Branch)
	case o.Commit != nil:
		fmt.Fprintf(&b, "Wrote %s to %s: %s\n", o.Commit.Short, Branch, o.Commit.Subject)
	default:
		fmt.Fprintf(&b, "Nothing new to record on %s.", Branch)
		if n := len(o.Result.Incomplete); n > 0 && o.Result.Through == o.Result.Watermark {
			fmt.Fprintf(&b, " The watermark is held back by %d %s not fully triaged.", n, plural(n, "change"))
		}
		b.WriteString("\n")
	}
	if o.Redone {
		fmt.Fprintf(&b, "%s moved on the remote while this run wrote it; the write was redone once, without a new session", Branch)
		if o.Superseded > 0 {
			fmt.Fprintf(&b, ", dropping %d %s learned meanwhile", o.Superseded, plural(o.Superseded, "proposal"))
		}
		b.WriteString(".\n")
	}
	if o.Pushed {
		fmt.Fprintf(&b, "Pushed %s.\n", Branch)
	}
	switch o.PR.State {
	case PROpened:
		fmt.Fprintf(&b, "Opened PR #%d: %s\n", o.PR.Number, o.PR.URL)
	case PRRefreshed:
		fmt.Fprintf(&b, "Refreshed PR #%d: %s\n", o.PR.Number, o.PR.URL)
	case PRReady:
		fmt.Fprintf(&b, "%s is ready for review; no PR opened", Branch)
		if o.PR.Reason != "" {
			fmt.Fprintf(&b, " (%s)", o.PR.Reason)
		}
		b.WriteString(".\n")
	case PRNone:
		b.WriteString("No PR opened or refreshed: nothing new to review.\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// plural is noun, with an s unless n is 1.
func plural(n int, noun string) string {
	if n == 1 {
		return noun
	}
	return noun + "s"
}
