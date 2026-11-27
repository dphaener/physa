package initcap

import (
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
