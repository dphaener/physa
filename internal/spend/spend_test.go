package spend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
)

const opus = "claude-opus-5-5"

var day = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

func TestReadRatioByFamily(t *testing.T) {
	t.Parallel()
	for model, want := range map[string]int64{
		"claude-opus-5-5": 50, "claude-sonnet-5-5": 50, "claude-opus-5-5[1m]": 50, "Claude-Sonnet-5.5": 50,
		"claude-fable-5": 25, "claude-fable-5-1": 25, "claude-mythos-5-2": 25,
		"claude-haiku-4-5": 100, "claude-opus-4-1": 100, "claude-sonnet-5-50": 100, "": 100,
	} {
		if got := ReadRatio(model); got != want {
			t.Errorf("ReadRatio(%q) = %d, want %d", model, got, want)
		}
	}
}

// TestInputPriceUsesThePublishedRatios backs a price out of one model's cost:
// dollars ÷ (uncached + reads × r + 5-minute writes × 1.25 + 1-hour writes
// × 2 + output × 5).
func TestInputPriceUsesThePublishedRatios(t *testing.T) {
	t.Parallel()
	cases := []struct {
		c    adapter.ModelCost
		want int64
	}{
		// 1M input at $4/M is $4.
		{adapter.ModelCost{Model: opus, Input: 1_000_000, MicroUSD: 4_000_000}, 4_000_000},
		// 1M reads at 5% of $4/M is $0.20.
		{adapter.ModelCost{Model: opus, CacheRead: 1_000_000, MicroUSD: 200_000}, 4_000_000},
		// The same reads on a Fable model are 2.5%, on any other model 10%.
		{adapter.ModelCost{Model: "claude-fable-5-1", CacheRead: 1_000_000, MicroUSD: 100_000}, 4_000_000},
		{adapter.ModelCost{Model: "claude-haiku-4-5", CacheRead: 1_000_000, MicroUSD: 100_000}, 1_000_000},
		// 5-minute writes are 1.25 times input, 1-hour writes twice it.
		{adapter.ModelCost{Model: opus, CacheWrite5m: 1_000_000, MicroUSD: 5_000_000}, 4_000_000},
		{adapter.ModelCost{Model: opus, CacheWrite1h: 1_000_000, MicroUSD: 8_000_000}, 4_000_000},
		// Output is five times input.
		{adapter.ModelCost{Model: opus, Output: 1_000_000, MicroUSD: 20_000_000}, 4_000_000},
		// All of them: 1 + 0.05 + 1.25 + 2 + 5 = 9.3 input-millions at $4.
		{adapter.ModelCost{Model: opus, Input: 1e6, CacheRead: 1e6, CacheWrite5m: 1e6, CacheWrite1h: 1e6, Output: 1e6, MicroUSD: 37_200_000}, 4_000_000},
	}
	for _, c := range cases {
		if got, ok := InputPrice(c.c); !ok || got != c.want {
			t.Errorf("InputPrice(%+v) = %d, %v; want %d", c.c, got, ok, c.want)
		}
	}
	if _, ok := InputPrice(adapter.ModelCost{Model: opus, Input: 10}); ok {
		t.Error("a cost with no dollars gave a price")
	}
	if _, ok := InputPrice(adapter.ModelCost{Model: opus, MicroUSD: 10}); ok {
		t.Error("a cost with no tokens gave a price")
	}
}

// session is a session of opus that read 1M uncached input and wrote 10,000
// tokens, at $4 a million input: $4.20.
func session() adapter.Result {
	return adapter.Result{Model: opus + "[1m]", Tokens: adapter.Tokens{Input: 1_000_000, Output: 10_000},
		Cost: adapter.Cost{{Model: opus, Input: 1_000_000, Output: 10_000, MicroUSD: 4_200_000}}}
}

func TestWeightedIsDollarsOverTheDefaultModelsInput(t *testing.T) {
	t.Parallel()
	c := Open("")
	r := session()
	c.Learn("claude-code", r, day)
	got := c.Weighted("claude-code", r)
	if want := (Spend{Tokens: 1_050_000, MicroUSD: 4_200_000}); got != want {
		t.Fatalf("Weighted = %+v, want %+v", got, want)
	}
	if got.String() != "1.1M tokens (about $4.20)" {
		t.Errorf("String = %q", got.String())
	}
	// A cheaper model's session weighs less: haiku at $1 a million.
	r.Model = opus // the lead stays the default
	r.Cost = append(r.Cost, adapter.ModelCost{Model: "claude-haiku-4-5", Input: 400_000, MicroUSD: 400_000})
	c.Learn("claude-code", r, day.Add(time.Minute))
	if got := c.Weighted("claude-code", r); got.Tokens != 1_150_000 || got.MicroUSD != 4_600_000 || got.Unpriced {
		t.Errorf("with haiku, Weighted = %+v", got)
	}
}

// TestWeightedFromCachedPrices weighs a session that reported no dollars by
// its tokens by kind at its model's cached price.
func TestWeightedFromCachedPrices(t *testing.T) {
	t.Parallel()
	c := Open("")
	c.Learn("claude-code", session(), day)
	r := adapter.Result{Model: opus, Tokens: adapter.Tokens{Input: 2_000_000, CachedInput: 1_000_000, Output: 100_000}}
	// 1M uncached + 1M reads × 0.05 + 100k output × 5 = 1.55M, at $4/M = $6.20.
	if got, want := c.Weighted("claude-code", r), (Spend{Tokens: 1_550_000, MicroUSD: 6_200_000}); got != want {
		t.Errorf("Weighted = %+v, want %+v", got, want)
	}
}

// TestWeightedWithNoPriceIsRaw is Q9: a model with no cached price counts at
// raw tokens, weight 1, and no dollar figure is shown for it.
func TestWeightedWithNoPriceIsRaw(t *testing.T) {
	t.Parallel()
	c := Open("")
	r := adapter.Result{Model: opus, Tokens: adapter.Tokens{Input: 2_000_000, CachedInput: 1_000_000, Output: 100_000}}
	got := c.Weighted("claude-code", r)
	if want := (Spend{Tokens: 2_100_000, Unpriced: true}); got != want {
		t.Fatalf("with no prices, Weighted = %+v, want %+v", got, want)
	}
	if s := got.String(); s != "2.1M tokens" || strings.Contains(s, "$") {
		t.Errorf("String = %q, want no dollars", s)
	}
	// A priced default with an unpriced second model: that model is raw.
	c.Learn("claude-code", session(), day)
	r.Cost = adapter.Cost{{Model: opus, Input: 1_000_000, MicroUSD: 4_000_000}, {Model: "other-model", Input: 300}}
	if got := c.Weighted("claude-code", r); got != (Spend{Tokens: 1_000_300, MicroUSD: 4_000_000, Unpriced: true}) {
		t.Errorf("with one unpriced model, Weighted = %+v", got)
	}
	// Dollars reported, but no default model priced: raw, no dollars.
	if got := Open("").Weighted("claude-code", session()); got != (Spend{Tokens: 1_010_000, Unpriced: true}) {
		t.Errorf("with no default priced, Weighted = %+v", got)
	}
	if got := c.Weighted("claude-code", adapter.Result{}); got != (Spend{}) {
		t.Errorf("an empty session weighs %+v", got)
	}
}

func TestLearnNewestWins(t *testing.T) {
	t.Parallel()
	c := Open("")
	cheap := session()
	cheap.Cost[0].MicroUSD = 2_100_000 // $2 a million
	c.Learn("claude-code", session(), day)
	c.Learn("claude-code", cheap, day.Add(-time.Hour)) // older: ignored
	if p, _ := c.Price(opus); p.PerMillion != 4_000_000 {
		t.Errorf("an older price replaced a newer one: %+v", p)
	}
	c.Learn("claude-code", cheap, day.Add(time.Hour))
	if m, p, _ := c.Reference("claude-code"); m != opus || p.PerMillion != 2_000_000 {
		t.Errorf("the newer price did not win: %+v", p)
	}
}

func TestSaveKeepsBothRunsNewestWins(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "physa", File)
	a, b := Open(path), Open(path)
	a.Learn("claude-code", session(), day)
	other := adapter.Result{Model: "claude-fable-5-1", Cost: adapter.Cost{{Model: "claude-fable-5-1", Input: 1_000_000, MicroUSD: 10_000_000}}}
	b.Learn("claude-code", other, day.Add(time.Hour))
	if err := a.Save(); err != nil {
		t.Fatal(err)
	}
	if err := b.Save(); err != nil {
		t.Fatal(err)
	}
	got := Open(path)
	if p, ok := got.Price(opus); !ok || p.PerMillion != 4_000_000 {
		t.Errorf("the first run's price was lost: %+v %v", p, ok)
	}
	if d := got.Defaults["claude-code"]; d.Model != "claude-fable-5-1" {
		t.Errorf("default model %+v, want the newer run's", d)
	}
	if fi, err := os.Stat(filepath.Dir(path)); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("cache directory %v, %v", fi, err)
	}
}

func TestOpenBadFileIsEmpty(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), File)
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := Open(path)
	if len(c.Models) != 0 || len(c.Defaults) != 0 {
		t.Fatalf("a bad file read as %+v", c)
	}
	c.Learn("claude-code", session(), day)
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if _, ok := Open(path).Price(opus); !ok {
		t.Error("Save did not replace the bad file")
	}
	if err := Open("").Save(); err != nil {
		t.Errorf("an in-memory cache's Save: %v", err)
	}
}

func TestFormatting(t *testing.T) {
	t.Parallel()
	for n, want := range map[int64]string{0: "0", 980: "980", 1000: "1k", 7_449: "7.4k", 74_135: "74k", 999_499: "999k", 999_500: "1M", 2_100_000: "2.1M", 6_770_000: "6.8M"} {
		if got := Tokens(n); got != want {
			t.Errorf("Tokens(%d) = %q, want %q", n, got, want)
		}
	}
	for micro, want := range map[int64]string{0: "$0.00", 4_000: "$0.00", 5_000: "$0.01", 4_200_000: "$4.20", 27_600_000: "$28", 9_995_000: "$10"} {
		if got := Dollars(micro); got != want {
			t.Errorf("Dollars(%d) = %q, want %q", micro, got, want)
		}
	}
	if got := (Spend{Tokens: 2_100_000, MicroUSD: 28_000_000}).String(); got != "2.1M tokens (about $28)" {
		t.Errorf("String = %q", got)
	}
	for s, want := range map[Spend]string{{Tokens: 1, MicroUSD: 4_000}: "under $0.01", {Tokens: 1, MicroUSD: 5_000}: "about $0.01", {Tokens: 1, Unpriced: true}: "none: no price yet"} {
		if got := s.Cost(); got != want {
			t.Errorf("Cost of %+v = %q, want %q", s, got, want)
		}
	}
	if got := (Spend{Tokens: 1, MicroUSD: 2}).Add(Spend{Tokens: 3, Unpriced: true}); got != (Spend{Tokens: 4, MicroUSD: 2, Unpriced: true}) {
		t.Errorf("Add = %+v", got)
	}
}

func TestPathIsFileInTheSettingsDirectory(t *testing.T) {
	t.Parallel()
	x := t.TempDir()
	getenv := func(k string) string { return map[string]string{"XDG_CONFIG_HOME": x}[k] }
	if got, err := Path(getenv); err != nil || got != filepath.Join(x, "physa", File) {
		t.Errorf("Path = %q, %v, want %s", got, err, filepath.Join(x, "physa", File))
	}
	if got, err := Path(func(string) string { return "" }); err == nil || err.Error() != "no XDG_CONFIG_HOME and no HOME" {
		t.Errorf("Path with no settings directory = %q, %v, want the configdir error", got, err)
	}
}
