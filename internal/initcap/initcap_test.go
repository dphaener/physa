package initcap

import (
	"slices"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/spend"
)

// TestParseReadsEachUnit checks that each form of --cap is read into its unit
// and given back as a person would write it.
func TestParseReadsEachUnit(t *testing.T) {
	for _, c := range []struct {
		in   string
		want Cap
		str  string
	}{
		{"", Cap{}, "none"},
		{"$5", Cap{Kind: Money, MicroUSD: 5_000_000}, "$5.00"},
		{"5usd", Cap{Kind: Money, MicroUSD: 5_000_000}, "$5.00"},
		{"2.5USD", Cap{Kind: Money, MicroUSD: 2_500_000}, "$2.50"},
		{"$0.95", Cap{Kind: Money, MicroUSD: 950_000}, "$0.95"},
		{"80%", Cap{Kind: Share, Percent: 80}, "80%"},
		{"12.5%", Cap{Kind: Share, Percent: 12.5}, "12.5%"},
		{"100%", Cap{Kind: Share, Percent: 100}, "100%"},
		{"2m", Cap{Kind: Tokens, Tokens: 2_000_000}, "2M tokens"},
		{"200k", Cap{Kind: Tokens, Tokens: 200_000}, "200k tokens"},
		{"1.5m", Cap{Kind: Tokens, Tokens: 1_500_000}, "1.5M tokens"},
		{"50000", Cap{Kind: Tokens, Tokens: 50_000}, "50k tokens"},
		{"1,500,000", Cap{Kind: Tokens, Tokens: 1_500_000}, "1.5M tokens"},
		{"2.25K", Cap{Kind: Tokens, Tokens: 2_250}, "2.2k tokens"},
	} {
		got, err := Parse(c.in)
		if err != nil || got != c.want {
			t.Errorf("Parse(%q) = %+v, %v; want %+v", c.in, got, err, c.want)
			continue
		}
		if got.String() != c.str {
			t.Errorf("Parse(%q).String() = %q; want %q", c.in, got.String(), c.str)
		}
	}
}

// TestParseRefusesBadCaps checks that a bad --cap is refused with the three
// forms named.
func TestParseRefusesBadCaps(t *testing.T) {
	for _, in := range []string{"0%", "120%", "$0", "abc", "-5", "$1.005", "5%%", "$", "0usd", "1e2%", "0", "1.5", "1.2345k", "k", ".5m", "99999999999999999999"} {
		_, err := Parse(in)
		if err == nil {
			t.Errorf("Parse(%q) = nil error; want refused", in)
			continue
		}
		t.Logf("Parse(%q): %v", in, err)
		for _, form := range []string{"money ($5 or 5usd)", "share of the plan's usage (80%)", "tokens (2m)"} {
			if !strings.Contains(err.Error(), form) {
				t.Errorf("Parse(%q) error %q does not name %q", in, err, form)
			}
		}
	}
}

// TestShowSpendsInTheCapsUnit checks that spend is shown in the cap's unit.
func TestShowSpendsInTheCapsUnit(t *testing.T) {
	s := spend.Spend{Tokens: 240_000, MicroUSD: 950_000}
	money := Cap{Kind: Money, MicroUSD: 5_000_000}
	if got := money.Show(s); got != "$0.95 (240k tokens)" {
		t.Errorf("money Show = %q", got)
	}
	if got := money.Show(spend.Spend{Tokens: 240_000, Unpriced: true}); got != "no cost known yet (240k tokens)" {
		t.Errorf("money Show unpriced = %q", got)
	}
	for _, c := range []Cap{{Kind: Tokens, Tokens: 1}, {Kind: Share, Percent: 80}} {
		if got := c.Show(s); got != s.String() {
			t.Errorf("%s Show = %q; want %q", c, got, s.String())
		}
	}
}

// TestContinueKeepsTheCap checks that the line an interrupt prints keeps the
// --cap typed, quoted when it holds a $, where the cap's own line drops it.
func TestContinueKeepsTheCap(t *testing.T) {
	for _, c := range []struct {
		words []string
		again string
		cont  string
	}{
		{[]string{"--walk", "--cap", "$5"}, "physa init --walk", "physa init --walk --cap '$5'"},
		{[]string{"--recover", "--cap=80%"}, "physa init --recover", "physa init --recover --cap=80%"},
		{[]string{"--cap", "2m", "--json"}, "physa init --json", "physa init --cap 2m --json"},
		{[]string{"--areas"}, "physa init --areas", "physa init --areas"},
	} {
		if got := Again(c.words, "").String(); got != c.again {
			t.Errorf("Again(%q, none) = %q, want %q", c.words, got, c.again)
		}
		if got := Continue(c.words, "").String(); got != c.cont {
			t.Errorf("Continue(%q, none) = %q, want %q", c.words, got, c.cont)
		}
	}
}

var typed = []string{"--cap", "$5", "--agent", "x", "--json", "--write", "--push", "--pr", "--hooks", "--max-prs", "5", "--since", "2026-01-01"}

// TestAgainNamesThePhaseAndThoseAfterIt checks each phase's lines: the phase
// first, then the later ones, each with only the words its phase takes, and
// the write flags on the --consolidate line only.
func TestAgainNamesThePhaseAndThoseAfterIt(t *testing.T) {
	for phase, want := range map[string]Lines{
		"areas": {"physa init --areas --agent x --json", "physa init --walk --agent x --json", "physa init --place --json",
			"physa init --recover --agent x --json", "physa init --consolidate --agent x --json --write --push --pr --hooks"},
		"walk": {"physa init --walk --agent x --json", "physa init --place --json",
			"physa init --recover --agent x --json", "physa init --consolidate --agent x --json --write --push --pr --hooks"},
		"place":       {"physa init --place --json", "physa init --recover --agent x --json", "physa init --consolidate --agent x --json --write --push --pr --hooks"},
		"recover":     {"physa init --recover --agent x --json", "physa init --consolidate --agent x --json --write --push --pr --hooks"},
		"consolidate": {"physa init --consolidate --agent x --json --write --push --pr --hooks"},
		"":            {"physa init --agent x --json --write --push --pr --hooks --max-prs 5 --since 2026-01-01"},
	} {
		if got := Again(typed, phase); !slices.Equal(got, want) {
			t.Errorf("Again(%q) = %q, want %q", phase, got, want)
		}
	}
}

// TestContinueKeepsTheCapOnLinesThatStartSessions checks that Continue keeps
// --cap quoted on every line but --place's.
func TestContinueKeepsTheCapOnLinesThatStartSessions(t *testing.T) {
	want := Lines{"physa init --walk --cap '$5' --agent x --json", "physa init --place --json",
		"physa init --recover --cap '$5' --agent x --json", "physa init --consolidate --cap '$5' --agent x --json --write --push --pr --hooks"}
	if got := Continue(typed, "walk"); !slices.Equal(got, want) {
		t.Errorf("Continue = %q, want %q", got, want)
	}
}

// TestLinesJoin checks the joined form of one, two and several lines.
func TestLinesJoin(t *testing.T) {
	for want, l := range map[string]Lines{"A": {"A"}, "A, then B": {"A", "B"}, "A, then B, C and D": {"A", "B", "C", "D"}, "": nil} {
		if got := l.String(); got != want {
			t.Errorf("%q joined = %q, want %q", []string(l), got, want)
		}
	}
}
