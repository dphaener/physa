package spend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
)

type oneResult struct{ r adapter.Result }

func (oneResult) Name() string { return "claude-code" }
func (a oneResult) Start(context.Context, adapter.Spec) (adapter.Session, error) {
	return session1(a), nil
}

type session1 struct{ r adapter.Result }

func (s session1) Wait() (adapter.Result, error) { return s.r, nil }

func TestMeterLearnsWeighsAndAdds(t *testing.T) {
	c := Open("")
	var seen []Spend
	m := &Meter{Adapter: oneResult{session()}, Cache: c, Now: func() time.Time { return day }, Ended: func(s Spend) { seen = append(seen, s) }}
	for range 2 {
		s, err := m.Start(context.Background(), adapter.Spec{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := m.Spent(), (Spend{Tokens: 2_100_000, MicroUSD: 8_400_000}); got != want {
		t.Errorf("Spent = %+v, want %+v", got, want)
	}
	if c := m.Count(); c.Raw != (adapter.Tokens{Input: 2_000_000, Output: 20_000}) || c.Since(Count{Spend: seen[0], Raw: session().Tokens}) != (Count{Spend: Spend{Tokens: 1_050_000, MicroUSD: 4_200_000}, Raw: session().Tokens}) {
		t.Errorf("Count = %+v", c)
	}
	if (*Meter)(nil).Count() != (Count{}) {
		t.Error("a nil meter counted something")
	}
	if len(seen) != 2 || seen[0].Tokens != 1_050_000 {
		t.Errorf("Ended saw %+v", seen)
	}
	if _, p, ok := c.Reference("claude-code"); !ok || p.PerMillion != 4_000_000 {
		t.Errorf("the meter learned %+v %v", p, ok)
	}
}

func TestInputAndRange(t *testing.T) {
	c := Open("")
	lo, hi := c.Input("claude-code", 1_000_000), c.Input("claude-code", 3_000_000)
	if lo != (Spend{Tokens: 1_000_000, Unpriced: true}) || Range(lo, hi) != "1M to 3M tokens" {
		t.Errorf("with no prices: %+v, %q", lo, Range(lo, hi))
	}
	c.Learn("claude-code", session(), day)
	lo, hi = c.Input("claude-code", 1_000_000), c.Input("claude-code", 3_000_000)
	if got := Range(lo, hi); got != "1M to 3M tokens (about $4.00 to $12)" {
		t.Errorf("Range = %q", got)
	}
}

func TestSaveDropsTheRatioRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), File)
	// A cache from before holds ratios under both older keys: ignored and dropped.
	old := `{"models":{"m":{"micro_usd_per_million_input":4000000,"at":"2026-11-22T00:00:00Z"}},"init_ratios":[{"milli":5900,"at":"2026-11-22T00:00:00Z"}],"init_weighted_ratios":[{"recover":6250,"at":"2026-11-23T00:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	c := Open(path)
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "init_weighted_ratios") || strings.Contains(string(b), "init_ratios") {
		t.Errorf("the ratio records were kept:\n%s", b)
	}
	if _, ok := Open(path).Price("m"); !ok {
		t.Errorf("the prices were lost:\n%s", b)
	}
}

type windowedResult struct{ oneResult }

func (windowedResult) Window() int { return 1_000_000 }

func TestMeterWindow(t *testing.T) {
	if got := (&Meter{Adapter: windowedResult{}}).Window(); got != 1_000_000 {
		t.Errorf("Window through a meter = %d, want 1000000", got)
	}
	if got := adapter.Window(&Meter{Adapter: windowedResult{}}); got != 1_000_000 {
		t.Errorf("adapter.Window of a meter = %d", got)
	}
	if got := adapter.Window(&Meter{Adapter: oneResult{}}); got != 0 {
		t.Errorf("Window over an adapter with none = %d, want 0", got)
	}
	var nilMeter *Meter
	if nilMeter.Window() != 0 || (&Meter{}).Window() != 0 {
		t.Error("a nil meter or one with no adapter states a window")
	}
}
