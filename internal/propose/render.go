package propose

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/draft"
)

// ReportVersion is the version of Report's JSON form. It changes only when a
// field changes meaning or goes away.
const ReportVersion = 1

// The values of Report.Session.
const (
	SessionNone    = "none"
	SessionRan     = "ran"
	SessionStopped = "stopped"
)

// Report is what one run of learning's draft and check stages found.
type Report struct {
	Version int `json:"version"`
	// Range is the range of changes the run read.
	Range string `json:"range"`
	// Changes is how many changes carry the batch's signals, Signals how
	// many signals the batch holds, and Capabilities how many capabilities
	// it bears on.
	Changes      int `json:"changes"`
	Signals      int `json:"signals"`
	Capabilities int `json:"capabilities"`
	// Session is SessionNone when no session started, SessionStopped when
	// the circuit breaker stopped it, and SessionRan otherwise.
	Session string `json:"session"`
	// DraftSession is the id of the draft's agent session, empty when none ran.
	DraftSession string         `json:"draft_session"`
	Appetite     int64          `json:"appetite"`
	Tokens       adapter.Tokens `json:"tokens"`
	Kept         []Kept         `json:"kept"`
	Dropped      []Dropped      `json:"dropped"`
}

// WriteMarkdown writes r for people: what was kept, each entry as it would
// sit in the map, then what was dropped and why, then what the session cost.
func WriteMarkdown(w io.Writer, r Report) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s kept from %s on %s\n\n",
		plural(len(r.Kept), "proposal"), plural(r.Signals, "signal"), plural(r.Changes, "change"))
	if r.Range != "" {
		fmt.Fprintf(&b, "Range `%s`; the batch bears on %s.\n\n", r.Range, plural(r.Capabilities, "capability"))
	}
	for _, k := range r.Kept {
		p := k.Proposal
		fmt.Fprintf(&b, "## %s · %s\n\n", p.Capability, p.Section)
		fmt.Fprintf(&b, "From a %s signal on %s.\n\n", p.Signal.Kind, strings.Join(p.Signal.Changes, ", "))
		b.WriteString(strings.TrimSpace(p.Entry) + "\n\n")
	}
	if len(r.Dropped) > 0 {
		b.WriteString("## Dropped\n\n")
		for _, d := range r.Dropped {
			fmt.Fprintf(&b, "- %s: %s\n", what(d), oneLine(d.Reason))
		}
		b.WriteString("\n")
	}
	switch r.Session {
	case SessionNone:
		b.WriteString("No signals, so no session started.\n")
	case SessionStopped:
		fmt.Fprintf(&b, "Stopped at the appetite (%s) after %s; nothing kept.\n", plural(int(r.Appetite), "token"), r.Tokens)
	default:
		fmt.Fprintf(&b, "Tokens: %s of an appetite of %d.\n", r.Tokens, r.Appetite)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// WriteJSON writes r in its versioned JSON form, with empty lists as [].
func WriteJSON(w io.Writer, r Report) error {
	r.Kept = append([]Kept{}, r.Kept...)
	for i := range r.Kept {
		r.Kept[i].Proposal = filled(r.Kept[i].Proposal)
	}
	r.Dropped = append([]Dropped{}, r.Dropped...)
	for i := range r.Dropped {
		r.Dropped[i].Proposal = filled(r.Dropped[i].Proposal)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// filled returns p with its nil lists as empty ones, so the JSON form never
// holds null where a list belongs.
func filled(p draft.Proposal) draft.Proposal {
	if p.Changes == nil {
		p.Changes = []string{}
	}
	if p.Signal.Changes == nil {
		p.Signal.Changes = []string{}
	}
	return p
}

// what names a dropped item for the list: its kind and capability, or that
// it was not a proposal at all.
func what(d Dropped) string {
	if d.Proposal.Kind == "" && d.Proposal.Capability == "" {
		if d.Raw != "" {
			return "an item that is not a proposal"
		}
		return "the session's output, which could not be read"
	}
	kind, capability := d.Proposal.Kind, d.Proposal.Capability
	if kind == "" {
		kind = "a proposal with no kind"
	}
	if capability == "" {
		return kind + " for no capability"
	}
	return fmt.Sprintf("%s for `%s`", kind, capability)
}

// oneLine keeps a reason on one line of the list.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	if strings.HasSuffix(word, "y") {
		return fmt.Sprintf("%d %sies", n, strings.TrimSuffix(word, "y"))
	}
	return fmt.Sprintf("%d %ss", n, word)
}
