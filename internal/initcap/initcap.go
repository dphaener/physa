// Package initcap is a person's optional cap on what init spends, in the
// unit they pay in: an amount of money, a share of their plan's usage as a
// ceiling, or weighted tokens. Its guard starts no session that would pass
// the cap: once one is refused, init starts nothing more, sessions already
// running finish, and the guard says what was spent in the cap's unit and
// how to continue. Its only I/O is the agent's own usage reading and the
// lines it prints for init to the writer it is given.
package initcap

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/spend"
)

// Kind is the unit a cap is given in.
type Kind int

const (
	// None is no cap: init runs to the end.
	None Kind = iota
	// Money is an amount of money, in millionths of a dollar.
	Money
	// Share is a share of the plan's usage, as a percent ceiling.
	Share
	// Tokens is an amount of weighted tokens.
	Tokens
)

// Cap is what a person caps init's spend at. The zero Cap is none.
type Cap struct {
	Kind Kind
	// MicroUSD is a Money cap, in millionths of a dollar.
	MicroUSD int64
	// Percent is a Share cap, above 0 and at most 100.
	Percent float64
	// Tokens is a Tokens cap, in weighted tokens.
	Tokens int64
}

// forms names the three forms a cap is given in, for a bad value's error.
const forms = "give an amount of money ($5 or 5usd), a share of the plan's usage (80%) or tokens (2m)"

// Parse reads a cap as a person gives it: "" is none; "$5" or "5usd" (any
// case, to the cent) is money; "80%" (above 0, at most 100) is a share of the
// plan's usage; anything else is weighted tokens, read as an appetite is
// (200k, 1.5m, 50000). Every amount is above zero.
func Parse(s string) (Cap, error) {
	v := strings.TrimSpace(s)
	low := strings.ToLower(v)
	bad := func(why string) (Cap, error) {
		return Cap{}, fmt.Errorf("--cap %q %s: %s", s, why, forms)
	}
	switch {
	case v == "":
		return Cap{}, nil
	case strings.HasPrefix(low, "$") || strings.HasSuffix(low, "usd"):
		n := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(low, "$"), "usd"))
		micro, ok := cents(n)
		if !ok {
			return bad("is not an amount of money")
		}
		if micro <= 0 {
			return bad("must be more than zero")
		}
		return Cap{Kind: Money, MicroUSD: micro}, nil
	case strings.HasSuffix(v, "%"):
		n := strings.TrimSpace(strings.TrimSuffix(v, "%"))
		if !decimal(n) {
			return bad("is not a share")
		}
		p, err := strconv.ParseFloat(n, 64)
		if err != nil || p <= 0 || p > 100 {
			return bad("must be above 0% and at most 100%")
		}
		return Cap{Kind: Share, Percent: p}, nil
	}
	n, ok := count(v)
	if !ok {
		return bad("is not a cap")
	}
	if n <= 0 {
		return bad("must be more than zero")
	}
	return Cap{Kind: Tokens, Tokens: n}, nil
}

// Read is Parse for init's line: a cap with a phase that starts no session
// (starts false, as --place) is refused too.
func Read(s string, starts bool) (Cap, error) {
	c, err := Parse(s)
	if err == nil && !starts && c.Kind != None {
		return Cap{}, errors.New("--place starts no session, so it takes no --cap")
	}
	return c, err
}

// count reads a number of tokens the way an appetite is written: an integer,
// optionally ending in k (thousand) or m (million), decimals only with a
// suffix and no finer than one token, commas and underscores between digits.
// It is read here rather than with the engine's package, since the map's side
// never imports the engine.
func count(s string) (int64, bool) {
	s = strings.NewReplacer(",", "", "_", "").Replace(s)
	mult, places := int64(1), 0
	if s != "" {
		switch s[len(s)-1] {
		case 'k', 'K':
			mult, places, s = 1_000, 3, s[:len(s)-1]
		case 'm', 'M':
			mult, places, s = 1_000_000, 6, s[:len(s)-1]
		}
	}
	whole, frac, dot := strings.Cut(s, ".")
	if !decimal(s) || whole == "" || (dot && len(frac) > places) {
		return 0, false
	}
	n, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || n > math.MaxInt64/mult {
		return 0, false
	}
	n *= mult
	if dot {
		f, _ := strconv.ParseInt(frac, 10, 64)
		for i := len(frac); i < places; i++ {
			f *= 10
		}
		n += f
	}
	return n, true
}

// decimal reports whether s is digits with at most one point among them.
func decimal(s string) bool {
	whole, frac, dot := strings.Cut(s, ".")
	if whole == "" && frac == "" {
		return false
	}
	return digits(whole) && (!dot || (frac != "" && digits(frac)))
}

func digits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// cents reads an amount of dollars, to the cent at most, in millionths of a
// dollar.
func cents(s string) (int64, bool) {
	if !decimal(s) {
		return 0, false
	}
	whole, frac, _ := strings.Cut(s, ".")
	if len(frac) > 2 || len(whole) > 12 {
		return 0, false
	}
	var d, c int64
	if whole != "" {
		d, _ = strconv.ParseInt(whole, 10, 64)
	}
	for len(frac) < 2 {
		frac += "0"
	}
	c, _ = strconv.ParseInt(frac, 10, 64)
	return d*1_000_000 + c*10_000, true
}

// dollars is micro shown to the cent: "$0.95", "$12.40".
func dollars(micro int64) string {
	return fmt.Sprintf("$%.2f", float64(micro)/1e6)
}

// String is c as a person would write it: "$5.00", "80%", "2M tokens", or
// "none".
func (c Cap) String() string {
	switch c.Kind {
	case Money:
		return dollars(c.MicroUSD)
	case Share:
		return strconv.FormatFloat(c.Percent, 'f', -1, 64) + "%"
	case Tokens:
		return spend.Tokens(c.Tokens) + " tokens"
	}
	return "none"
}

// Show is s in c's unit: money leads with dollars, tokens beside
// ("$0.95 (240k tokens)"), and says no cost is known when s is Unpriced;
// tokens and a share show s as spend does ("240k tokens (about $0.95)"),
// a share's usage being shown from its reading.
func (c Cap) Show(s spend.Spend) string {
	if c.Kind == Money {
		if s.Unpriced {
			return "no cost known yet (" + spend.Tokens(s.Tokens) + " tokens)"
		}
		return dollars(s.MicroUSD) + " (" + spend.Tokens(s.Tokens) + " tokens)"
	}
	return s.String()
}

// over reports whether s, in c's unit, passes c. A share is never passed by
// spend: its reading is.
func (c Cap) over(s spend.Spend) bool {
	switch c.Kind {
	case Money:
		return s.MicroUSD > c.MicroUSD
	case Tokens:
		return s.Tokens > c.Tokens
	}
	return false
}

// at reports whether s, in c's unit, has reached c, so any session would
// pass it.
func (c Cap) at(s spend.Spend) bool {
	switch c.Kind {
	case Money:
		return s.MicroUSD >= c.MicroUSD
	case Tokens:
		return s.Tokens >= c.Tokens
	}
	return false
}

// Sum is what init's record has spent, phase by phase, together: a capped
// run's Base when it does not gather anew.
func Sum(rows []initrecord.Spent) spend.Spend {
	var s spend.Spend
	for _, p := range rows {
		s = s.Add(p.Spend)
	}
	return s
}

// recoverTakes is each flag a continued --recover keeps from a bare run's
// words, true when it takes a value.
var recoverTakes = map[string]bool{"--agent": true, "--write": false, "--push": false, "--pr": false, "--hooks": false, "--json": false}

// takesValue is each of init's flags that takes a value.
var takesValue = map[string]bool{"--agent": true, "--answers": true, "--since": true, "--max-prs": true, "--cap": true, "--only": true}

// Again is the line that continues a run the cap stopped, from the words
// typed after init: "physa init" and those words without --cap, or, when
// recovering was stopped in a bare run, "physa init --recover" and the
// words of the bare run --recover takes (--agent, --write, --push, --pr,
// --hooks and --json).
func Again(words []string, recovering bool) string { return again(words, recovering, false) }

// Continue is the line that continues a run an interrupt stopped: Again's,
// keeping the --cap typed, quoted when it holds a $ so a shell keeps it, as
// the person's cap still applies.
func Continue(words []string, recovering bool) string { return again(words, recovering, true) }

func again(words []string, recovering, keepCap bool) string {
	out := []string{"physa", "init"}
	if recovering {
		out = append(out, "--recover")
	}
	for i := 0; i < len(words); i++ {
		w := words[i]
		name, _, joined := strings.Cut(w, "=")
		keep := name != "--cap"
		if recovering {
			_, keep = recoverTakes[name]
		}
		keep = keep || keepCap && name == "--cap"
		if keep {
			out = append(out, quoted(w))
		}
		if takesValue[name] && !joined && i+1 < len(words) {
			if i++; keep {
				out = append(out, quoted(words[i]))
			}
		}
	}
	return strings.Join(out, " ")
}

// quoted is w in single quotes when it holds a $, so a shell passes it as typed.
func quoted(w string) string {
	if strings.Contains(w, "$") {
		return "'" + w + "'"
	}
	return w
}
