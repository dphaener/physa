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
	return session1{a.r}, nil
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

func TestRatioRangeDefaultThenMeasured(t *testing.T) {
	path := filepath.Join(t.TempDir(), File)
	// A cache from before holds raw ratios under the old key: ignored.
	if err := os.WriteFile(path, []byte(`{"init_ratios":[{"milli":5900,"at":"2026-11-22T00:00:00Z"},{"milli":6100,"at":"2026-11-23T00:00:00Z"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := Open(path)
	defaults := Ratios{RecoverLo: 6250, RecoverHi: 18750, ProposeLo: 2050, ProposeHi: 2050}
	if got := c.RatioRange(); got != defaults {
		t.Errorf("with only old raw ratios: %+v, want %+v", got, defaults)
	}
	c.RecordRatio(1250, 100, 205, 100, day)
	c.RecordRatio(0, 100, 205, 100, day)  // nothing recovered: not a ratio
	c.RecordRatio(1250, 100, 0, 100, day) // nothing proposed: not a ratio
	if got, want := c.RatioRange(), (Ratios{RecoverLo: 6250, RecoverHi: 18750, ProposeLo: 2050, ProposeHi: 2050, Measured: 1}); got != want {
		t.Errorf("with one run: %+v, want its own ratios %+v", got, want)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	other := Open(path)
	other.RecordRatio(800, 100, 190, 100, day.Add(time.Hour))
	if err := other.Save(); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil { // an older copy saved again keeps the newer run
		t.Fatal(err)
	}
	want := Ratios{RecoverLo: 8000, RecoverHi: 12500, ProposeLo: 1900, ProposeHi: 2050, Measured: 2}
	if got := Open(path).RatioRange(); got != want {
		t.Errorf("with two runs: %+v, want %+v", got, want)
	}
	if b, _ := os.ReadFile(path); strings.Contains(string(b), `"init_ratios"`) {
		t.Errorf("the old raw ratios were kept:\n%s", b)
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
