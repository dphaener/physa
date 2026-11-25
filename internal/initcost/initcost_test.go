package initcost

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/shares"
	"github.com/dphaener/physa/internal/spend"
	"github.com/dphaener/physa/internal/store"
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
	want := "discovery spent 74k tokens (about $0.30). The rest of the run should spend about 3.2M to 9.1M tokens (about $13 to $36): propose's prompt of about 100k tokens plus 16k for its reply, at 2.05 times as weighed, and recovering at 6.25 to 18.75 times the sources' 470k tokens (nebari's measured ratio of 12.5, until a run is recorded on this machine). An estimate, not a limit."
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

// TestOneRecordedRunSetsTheRange checks that one run recorded on the
// machine replaces nebari's ratio: a recovery that weighed 2 times its
// sources gives 1 to 3 times, with that run's propose factor.
func TestOneRecordedRunSetsTheRange(t *testing.T) {
	c := priced()
	c.RecordRatio(200_000, 100_000, 180_000, 100_000, time.Now())
	// (100k prompt + 16k reply) × 1.8 + 470k sources × 1 to 3: 678.8k to 1.6188M.
	e := New(c, "claude-code", spend.Spend{}, 100_000, 16_000, 470_000)
	if e.Lo.Tokens != 678_800 || e.Hi.Tokens != 1_618_800 {
		t.Fatalf("estimate %d to %d", e.Lo.Tokens, e.Hi.Tokens)
	}
	if want := "and recovering at 1 to 3 times the sources' 470k tokens (the one run recorded on this machine measured 2; half to one and a half times that, until two are recorded)."; !strings.Contains(e.Line(), want) {
		t.Errorf("Line =\n%s\nwant it to hold\n%s", e.Line(), want)
	}
}

func TestLineWithShares(t *testing.T) {
	// (1.25M prompts + 64k replies) × 2.05 + 470k sources × 6.25 to 18.75:
	// 5.63M to 11.51M.
	e := New(priced(), "claude-code", spend.Spend{Tokens: 74_000, MicroUSD: 300_000}, 1_250_000, 64_000, 470_000)
	e.Shares = 3
	want := "discovery spent 74k tokens (about $0.30). The rest of the run should spend about 5.6M to 11.5M tokens (about $23 to $46): propose's 3 shares and their merge, prompts of about 1.2M tokens plus 64k for their replies, at 2.05 times as weighed, and recovering at 6.25 to 18.75 times the sources' 470k tokens (nebari's measured ratio of 12.5, until a run is recorded on this machine). An estimate, not a limit."
	if got := e.Line(); got != want {
		t.Errorf("Line =\n%s\nwant\n%s", got, want)
	}
	e.Shares = 0
	if got := e.Line(); !strings.Contains(got, ": propose's prompt of about 1.2M tokens plus 64k for its reply, at 2.05 times") {
		t.Errorf("Line with no shares = %s", got)
	}
}

// windowed is an adapter that states its window and never runs.
type windowed int

func (windowed) Name() string { return "claude-code" }
func (windowed) Start(context.Context, adapter.Spec) (adapter.Session, error) {
	return nil, errors.New("the estimate starts no session")
}
func (w windowed) Window() int { return int(w) }

func TestForCountsTheShares(t *testing.T) {
	g := store.Gather{}
	for n := 1; n <= 3; n++ {
		g.PRs = append(g.PRs, store.GatheredPR{Number: n, Title: "A change", Body: strings.Repeat("Why it changed. ", 1_500), MergedAt: time.Now()})
	}
	one, reply, k, err := shares.Estimate(g, 0)
	if err != nil || k != 0 {
		t.Fatalf("with no window: %d shares, %v", k, err)
	}
	m := &spend.Meter{Adapter: windowed(one + reply + 1), Cache: priced()}
	if e, err := For(priced(), m, g, 9_000); err != nil || e.Shares != 0 || e.Prompt != one || e.Reply != reply || e.Sources != 9_000 {
		t.Errorf("within the window: %+v, %v", e, err)
	}
	m.Adapter = windowed(one + reply - 1)
	prompt, reply, k, _ := shares.Estimate(g, int(m.Adapter.(windowed)))
	e, err := For(priced(), m, g, 9_000)
	if err != nil || k < 2 || e.Shares != k || e.Prompt != prompt || e.Reply != reply || !strings.Contains(e.Line(), fmt.Sprintf("propose's %d shares and their merge", k)) {
		t.Errorf("over the window: %+v, %v; want %d shares", e, err, k)
	}
	m.Adapter = windowed(mapping.ReplyReserve + 100)
	var big *shares.TooLarge
	if _, err := For(priced(), m, g, 9_000); !errors.As(err, &big) || !strings.HasPrefix(big.Source, "PR #") {
		t.Errorf("a PR larger than a share: %v", err)
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
