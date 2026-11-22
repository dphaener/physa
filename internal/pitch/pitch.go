// Package pitch writes and reads a pitch as markdown: the seven parts of a
// shaped idea, each checked present. It does no I/O; callers hand it text and
// keep what it returns.
package pitch

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Pitch is a shaped idea. Appetite is a number of tokens.
type Pitch struct {
	Problem     string
	Appetite    int64
	Solution    string
	Scope       string
	Signal      string
	RabbitHoles string
	NoGos       string
}

// partNames are the seven parts in the order a pitch is written.
var partNames = [7]string{"Problem", "Appetite", "Solution", "Scope", "Signal", "Rabbit holes", "No-gos"}

var guidance = [7]string{
	"The problem people have today, in their words.",
	"Tokens the team will spend: 200k, 1.5m. A budget, set before the work starts.",
	"The shape of the answer, rough enough to leave room to build.",
	"What the pitch covers. Too broad for one bet means more than one idea.",
	"What real use shows if the idea is right.",
	"Known risks that could swallow the appetite.",
	"What the pitch deliberately leaves out.",
}

var comment = regexp.MustCompile(`(?s)<!--.*?-->`)

func (p *Pitch) bodies() [7]string {
	appetite := ""
	if p.Appetite > 0 {
		appetite = Tokens(p.Appetite)
	}
	return [7]string{p.Problem, appetite, p.Solution, p.Scope, p.Signal, p.RabbitHoles, p.NoGos}
}

// Template returns the markdown to edit: the idea's text as the title, then
// the seven parts in order, each with a line of guidance. A prior pitch fills
// the parts in for shaping again.
func Template(ideaText string, prior *Pitch) string {
	var bodies [7]string
	if prior != nil {
		bodies = prior.bodies()
	}
	var b strings.Builder
	b.WriteString("# " + strings.TrimSpace(ideaText) + "\n")
	for i, name := range partNames {
		fmt.Fprintf(&b, "\n## %s\n<!-- %s -->\n", name, guidance[i])
		if bodies[i] != "" {
			b.WriteString(bodies[i] + "\n")
		}
	}
	return b.String()
}

// Parse reads a pitch from markdown. Headings match without regard to case
// or surrounding space, comments are dropped and the title line is ignored.
// Every problem found is returned together in one error.
func Parse(md string) (Pitch, error) {
	md = comment.ReplaceAllString(strings.ReplaceAll(md, "\r\n", "\n"), "")
	var bodies [7]strings.Builder
	var seen [7]bool
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
	var text [7]string
	for i, name := range partNames {
		text[i] = strings.TrimSpace(bodies[i].String())
		switch {
		case !seen[i]:
			problems = append(problems, fmt.Sprintf("missing part %q", name))
		case text[i] == "":
			problems = append(problems, fmt.Sprintf("empty part %q", name))
		}
	}
	var appetite int64
	if seen[1] && text[1] != "" {
		n, err := ParseTokens(text[1])
		if err != nil {
			problems = append(problems, "appetite: "+err.Error())
		}
		appetite = n
	}
	if len(problems) > 0 {
		return Pitch{}, errors.New("the pitch has problems: " + strings.Join(problems, "; "))
	}
	return Pitch{text[0], appetite, text[2], text[3], text[4], text[5], text[6]}, nil
}

func partIndex(name string) int {
	for i, n := range partNames {
		if strings.EqualFold(n, name) {
			return i
		}
	}
	return -1
}

// ParseTokens reads an appetite: an integer, optionally ending in k (thousand)
// or m (million). Decimals need a suffix (1.5m); commas and underscores
// separate digits. The result is above zero.
func ParseTokens(s string) (int64, error) {
	orig := s
	s = strings.NewReplacer(",", "", "_", "").Replace(strings.TrimSpace(s))
	mult, places := int64(1), 0
	if s != "" {
		switch s[len(s)-1] {
		case 'k', 'K':
			mult, places, s = 1_000, 3, s[:len(s)-1]
		case 'm', 'M':
			mult, places, s = 1_000_000, 6, s[:len(s)-1]
		}
	}
	bad := fmt.Errorf("%q is not a token count (try 200k, 1.5m or 50000)", orig)
	whole, frac, hasDot := strings.Cut(s, ".")
	if hasDot && (mult == 1 || len(frac) == 0 || len(frac) > places) {
		return 0, bad
	}
	if !digits(whole) || (hasDot && !digits(frac)) {
		return 0, bad
	}
	n, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || n > math.MaxInt64/mult {
		return 0, fmt.Errorf("%q is too large", orig)
	}
	n *= mult
	if hasDot {
		f, _ := strconv.ParseInt(frac, 10, 64)
		for i := len(frac); i < places; i++ {
			f *= 10
		}
		n += f
	}
	if n <= 0 {
		return 0, fmt.Errorf("%q must be more than zero", orig)
	}
	return n, nil
}

func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Tokens writes an appetite the way people say it: 200k, 1.5m, or the plain
// number when it is not a round multiple.
func Tokens(n int64) string {
	switch {
	case n >= 1_000_000 && n%100_000 == 0:
		if n%1_000_000 == 0 {
			return strconv.FormatInt(n/1_000_000, 10) + "m"
		}
		return fmt.Sprintf("%d.%dm", n/1_000_000, n%1_000_000/100_000)
	case n >= 1000 && n%1000 == 0:
		return strconv.FormatInt(n/1000, 10) + "k"
	}
	return strconv.FormatInt(n, 10)
}
