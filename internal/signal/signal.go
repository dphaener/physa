// Package signal writes and reads a pursuit's signal as markdown: its claim,
// its direction (what counts for and what counts against), the window it is
// read in and its resolver, every part checked present and every problem
// reported together on one line. It does no I/O and resolves nothing;
// callers hand it text and the day it is, and keep what it returns.
package signal

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Layout is how a window is written: a date, YYYY-MM-DD.
const Layout = "2006-01-02"

// Signal is what a pursuit is read against. Every part is required.
type Signal struct {
	Claim    string // what should change, in the team's words
	For      string // what counts as for the claim
	Against  string // what counts as against it
	Window   string // the day it is read, YYYY-MM-DD
	Resolver string // who or what reports the result: a named person or check
}

// partNames are the four parts in the order a signal is written.
var partNames = [4]string{"Claim", "Direction", "Window", "Resolver"}

const (
	partDirection = 1
	forMark       = "For:"
	againstMark   = "Against:"
)

var guidance = [4]string{
	"What should change, in the team's words.",
	`What counts as for the claim and what counts as against it: write each after its label. Both are required.`,
	"The day the signal is read: a date, YYYY-MM-DD, today or later.",
	"Who or what reports the result: a named person, or a named check.",
}

var comment = regexp.MustCompile(`(?s)<!--.*?-->`)

// Template returns the markdown to edit: the idea's text as the title, then
// the four parts in order, each with a line of guidance in a comment.
func Template(ideaText string) string {
	var b strings.Builder
	b.WriteString("# " + strings.TrimSpace(ideaText) + "\n")
	for i, name := range partNames {
		fmt.Fprintf(&b, "\n## %s\n<!-- %s -->\n", name, guidance[i])
		if i == partDirection {
			b.WriteString(forMark + "\n" + againstMark + "\n")
		}
	}
	return b.String()
}

// Parse reads a signal from markdown and checks it on the day today falls
// on. Headings match without regard to case or surrounding space, comments
// are dropped and the title line is ignored. Under Direction, "For:" and
// "Against:" each start their part. Every problem found is returned
// together in one error.
func Parse(md string, today time.Time) (Signal, error) {
	md = comment.ReplaceAllString(strings.ReplaceAll(md, "\r\n", "\n"), "")
	var bodies [4]strings.Builder
	var seen [4]bool
	var problems []string
	cur := -1 // -1 before any part, -2 inside an unknown or repeated one
	for _, line := range strings.Split(md, "\n") {
		if rest, ok := strings.CutPrefix(line, "## "); ok {
			name := strings.TrimSpace(rest)
			idx := partIndex(name)
			switch {
			case idx < 0:
				problems = append(problems, fmt.Sprintf("unknown heading %q", name))
				cur = -2
			case seen[idx]:
				problems = append(problems, fmt.Sprintf("repeated heading %q", partNames[idx]))
				cur = -2
			default:
				seen[idx] = true
				cur = idx
			}
			continue
		}
		if cur >= 0 {
			bodies[cur].WriteString(line + "\n")
		}
	}
	var s Signal
	skip := map[string]bool{}
	for i, name := range partNames {
		if !seen[i] {
			problems = append(problems, fmt.Sprintf("missing part %q", name))
			skip[name] = true
		}
	}
	s.Claim, s.Window, s.Resolver = bodies[0].String(), bodies[2].String(), bodies[3].String()
	if seen[partDirection] {
		var dp []string
		s.For, s.Against, dp = direction(bodies[partDirection].String())
		problems = append(problems, dp...)
	}
	problems = append(problems, check(s, today, skip)...)
	if len(problems) > 0 {
		return Signal{}, Problems(problems)
	}
	return Trim(s), nil
}

// direction splits Direction's text into what counts for and against, with
// a problem for text outside both labels and for a label given twice.
func direction(body string) (forText, against string, problems []string) {
	var parts [2]strings.Builder
	var seen [2]bool
	cur := -1
	stray := false
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		idx := -1
		for i, mark := range []string{forMark, againstMark} {
			if len(t) >= len(mark) && strings.EqualFold(t[:len(mark)], mark) {
				idx, t = i, t[len(mark):]
			}
		}
		if idx >= 0 {
			if seen[idx] {
				problems = append(problems, fmt.Sprintf("repeated %q in Direction", []string{forMark, againstMark}[idx]))
			}
			seen[idx], cur = true, idx
			parts[idx].WriteString(t + "\n")
			continue
		}
		if cur < 0 {
			stray = stray || t != ""
			continue
		}
		parts[cur].WriteString(line + "\n")
	}
	if stray {
		problems = append(problems, fmt.Sprintf("text in Direction before %q or %q", forMark, againstMark))
	}
	return parts[0].String(), parts[1].String(), problems
}

func partIndex(name string) int {
	for i, n := range partNames {
		if strings.EqualFold(n, name) {
			return i
		}
	}
	return -1
}

// Trim returns s with the space around each part removed.
func Trim(s Signal) Signal {
	return Signal{strings.TrimSpace(s.Claim), strings.TrimSpace(s.For), strings.TrimSpace(s.Against),
		strings.TrimSpace(s.Window), strings.TrimSpace(s.Resolver)}
}

// Check returns every problem with s on the day today falls on, in the
// order the parts are written: a part left empty, and a window that is not
// a date or is before today. None means s may be recorded.
func Check(s Signal, today time.Time) []string {
	return check(s, today, nil)
}

func check(s Signal, today time.Time, skip map[string]bool) []string {
	s = Trim(s)
	var problems []string
	empty := func(part, text, msg string) bool {
		if skip[part] || text != "" {
			return false
		}
		problems = append(problems, msg)
		return true
	}
	empty("Claim", s.Claim, `empty part "Claim"`)
	empty("Direction", s.For, fmt.Sprintf("empty %q in Direction", forMark))
	empty("Direction", s.Against, fmt.Sprintf("empty %q in Direction", againstMark))
	if !empty("Window", s.Window, `empty part "Window"`) && !skip["Window"] {
		day, err := time.ParseInLocation(Layout, s.Window, today.Location())
		if err != nil {
			problems = append(problems, fmt.Sprintf("window %q is not a date: write it as YYYY-MM-DD", s.Window))
		} else if now := today.Format(Layout); day.Format(Layout) < now {
			problems = append(problems, fmt.Sprintf("window %s is before today, %s", s.Window, now))
		}
	}
	empty("Resolver", s.Resolver, `empty part "Resolver"`)
	return problems
}

// Problems is the one error for every problem with a signal, on one line.
func Problems(problems []string) error {
	return errors.New("the signal has problems: " + strings.Join(problems, "; "))
}
