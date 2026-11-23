package initcost

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/spend"
)

// priced is a cache that knows claude-code's default model at $4 a million.
func priced() *spend.Cache {
	c := spend.Open("")
	c.Learn("claude-code", adapter.Result{Model: "claude-opus-5-5", Cost: adapter.Cost{{Model: "claude-opus-5-5", Input: 1_000_000, MicroUSD: 4_000_000}}}, time.Now())
	return c
}

func TestEstimateIsPromptPlusSourcesTimesTheRatio(t *testing.T) {
	// (100k prompt + 16k reply) × 2.05 + 470k sources × 6.25 to 18.75:
	// 3.1753M to 9.0503M.
	e := New(priced(), "claude-code", spend.Spend{Tokens: 74_000, MicroUSD: 300_000}, 100_000, 16_000, 470_000)
	if e.Lo.Tokens != 3_175_300 || e.Hi.Tokens != 9_050_300 || e.Lo.Unpriced {
		t.Fatalf("estimate %+v", e)
	}
	want := "discovery spent 74k tokens (about $0.30). The rest of the run should spend about 3.2M to 9.1M tokens (about $13 to $36): propose's prompt of about 100k tokens plus 16k for its reply, at 2.05 times as weighed, and recovering at 6.25 to 18.75 times the sources' 470k tokens (nebari's measured ratio of 12.5, until two runs are recorded on this machine). An estimate, not a limit."
	if got := e.Line(); got != want {
		t.Errorf("Line =\n%s\nwant\n%s", got, want)
	}
	// Q9: with no prices the range is weighted tokens with no cost shown.
	if got := New(spend.Open(""), "claude-code", spend.Spend{}, 100_000, 16_000, 470_000).Range(); got != "3.2M to 9.1M tokens" {
		t.Errorf("unpriced Range = %q", got)
	}
}

// TestEstimateContainsRun9 checks the estimate against nebari's run 9 as a
// recorded run: it spent 14M weighted tokens (discover 32k, propose 1.4M,
// recover 12.5M) for sources estimated at 998k and a propose of 682,510 raw
// tokens, and the range must hold what proposing and recovering spent.
func TestEstimateContainsRun9(t *testing.T) {
	c := priced()
	c.RecordRatio(12_500_000, 998_000, 1_400_000, 682_510, time.Now())
	e := New(c, "claude-code", spend.Spend{Tokens: 32_000}, 682_510-16_000, 16_000, 998_000)
	if spent := int64(14_000_000); e.Lo.Tokens > spent || e.Hi.Tokens < spent {
		t.Errorf("the estimate %d to %d does not hold run 9's 14M", e.Lo.Tokens, e.Hi.Tokens)
	}
	// Recorded twice, the range closes on run 9's own ratios: about 13.9M.
	c.RecordRatio(12_500_000, 998_000, 1_400_000, 682_510, time.Now().Add(time.Hour))
	if e := New(c, "claude-code", spend.Spend{}, 682_510-16_000, 16_000, 998_000); e.Lo.Tokens < 13_800_000 || e.Hi.Tokens > 14_000_000 {
		t.Errorf("from run 9 twice, the estimate is %d to %d", e.Lo.Tokens, e.Hi.Tokens)
	}
}

func TestUnit(t *testing.T) {
	if got := Unit(priced(), "claude-code"); !strings.Contains(got, "each worth an input token of claude-opus-5-5 ($4.00 a million)") {
		t.Errorf("Unit = %q", got)
	}
	if got := Unit(spend.Open(""), "claude-code"); !strings.Contains(got, "no prices are known yet") {
		t.Errorf("Unit with no prices = %q", got)
	}
}

func TestProceed(t *testing.T) {
	e := New(spend.Open(""), "x", spend.Spend{}, 1000, 0, 0) // 2,050 tokens
	var w bytes.Buffer
	if !Proceed(&w, e, Ask{Yes: true, Confirm: func(string) (bool, error) { t.Error("asked with --yes"); return false, nil }}) || w.Len() != 0 {
		t.Errorf("--yes: %q", w.String())
	}
	if Proceed(&w, e, Ask{Confirm: func(string) (bool, error) { return false, nil }}) || !strings.Contains(w.String(), "as asked") {
		t.Errorf("a no: %q", w.String())
	}
	w.Reset()
	if Proceed(&w, e, Ask{Rerun: "physa init --yes"}) || !strings.HasSuffix(w.String(), "estimated at about 2k to 2k tokens, and with no terminal to ask on, init goes on only when told to. The gather is stored; rerun with --yes to go on without asking: physa init --yes\n") {
		t.Errorf("off a terminal: %q", w.String())
	}
}

func TestTable(t *testing.T) {
	got := Table([]Row{{Phase: "propose", Input: 10, Output: 2, Weighted: spend.Spend{Tokens: 20, MicroUSD: 80}}, {Phase: "recover", Input: 5, Weighted: spend.Spend{Tokens: 5, Unpriced: true}}})
	for _, want := range []string{"| propose | 10 | 0 | 2 | 20 | under $0.01 |", "| recover | 5 | 0 | 0 | 5 | none: no price yet |", "| total | 15 | 0 | 2 | 25 | none: no price yet |"} {
		if !strings.Contains(got, want) {
			t.Errorf("table lacks %q:\n%s", want, got)
		}
	}
}

func TestTimes(t *testing.T) {
	for milli, want := range map[int64]string{4000: "4", 12000: "12", 6500: "6.5", 2050: "2.05", 18750: "18.75"} {
		if got := times(milli); got != want {
			t.Errorf("times(%d) = %q, want %q", milli, got, want)
		}
	}
}
