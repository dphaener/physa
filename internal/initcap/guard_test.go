package initcap

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/spend"
)

// perSession is what each fake session spends: 50k input tokens of model m,
// at $4.00 a million.
const perSession = 50_000

// fakeAgent starts sessions that each spend perSession weighted tokens. When
// gate is set, a session's Wait blocks until gate is closed.
type fakeAgent struct {
	mu     sync.Mutex
	starts int
	ctxs   []context.Context
	gate   chan struct{}
}

func (*fakeAgent) Name() string { return "fake" }
func (*fakeAgent) Window() int  { return 1000 }

func (a *fakeAgent) Start(ctx context.Context, _ adapter.Spec) (adapter.Session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.starts++
	a.ctxs = append(a.ctxs, ctx)
	return fakeSession{gate: a.gate}, nil
}

func (a *fakeAgent) started() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.starts
}

type fakeSession struct{ gate chan struct{} }

func (s fakeSession) Wait() (adapter.Result, error) {
	if s.gate != nil {
		<-s.gate
	}
	return adapter.Result{Model: "m", Cost: adapter.Cost{{Model: "m", Input: perSession, MicroUSD: perSession * 4}}}, nil
}

// readerAgent is a fakeAgent that reads the plan's usage: each read gives the
// next of percents, the last repeated, or err when set.
type readerAgent struct {
	*fakeAgent
	mu       sync.Mutex
	percents []float64
	reads    int
	err      error
}

func (r *readerAgent) Usage(context.Context) (adapter.Usage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads++
	if r.err != nil {
		return adapter.Usage{}, r.err
	}
	p := r.percents[min(r.reads-1, len(r.percents)-1)]
	return adapter.Usage{Limits: []adapter.Limit{
		{Kind: "weekly_all", Percent: p, ResetsAt: time.Date(2026, 11, 30, 14, 0, 0, 0, time.UTC), Active: true},
		{Kind: "weekly_scoped", Percent: 99, Active: false},
	}}, nil
}

func (r *readerAgent) readCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reads
}

// priced is a cache that knows model m at $4.00 a million input tokens, as
// fake's default model.
func priced() *spend.Cache {
	c := spend.Open("")
	c.Learn("fake", adapter.Result{Model: "m", Cost: adapter.Cost{{Model: "m", Input: 1_000_000, MicroUSD: 4_000_000}}},
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	return c
}

// guard is a guard for cap around raw, through a real meter on cache.
func guard(c Cap, raw adapter.Adapter, cache *spend.Cache) *Guard {
	return &Guard{Cap: c, Agent: &spend.Meter{Adapter: raw, Cache: cache}, Raw: raw, Cache: cache, AgentName: "fake"}
}

// run starts n sessions one after another, waiting for each, and returns the
// index of each refused start.
func run(t *testing.T, g *Guard, n int) []int {
	t.Helper()
	var refused []int
	for i := range n {
		sess, err := g.Start(context.Background(), adapter.Spec{Prompt: "p"})
		if err != nil {
			if !errors.Is(err, adapter.ErrCapped) {
				t.Fatalf("start %d: %v; want ErrCapped", i, err)
			}
			refused = append(refused, i)
			continue
		}
		if _, err := sess.Wait(); err != nil {
			t.Fatalf("wait %d: %v", i, err)
		}
	}
	return refused
}

// TestTokensCapStopsBeforeThePassingSession: each session spends 50k and is
// held to about 80k before it runs, so under a 200k cap sessions 0 to 2 run
// (150k spent) and session 3, which would take it to 230k, is refused, as is
// every start after it.
func TestTokensCapStopsBeforeThePassingSession(t *testing.T) {
	a := &fakeAgent{}
	g := guard(Cap{Kind: Tokens, Tokens: 200_000}, a, priced())
	refused := run(t, g, 6)
	if fmt.Sprint(refused) != "[3 4 5]" || a.started() != 3 || !g.Stopped() {
		t.Fatalf("refused %v, started %d, stopped %v; want [3 4 5], 3, true", refused, a.started(), g.Stopped())
	}
	msg := g.Reached("physa init --recover")
	t.Log(msg)
	for _, want := range []string{"200k tokens", "150k tokens", "run physa init --recover with a larger --cap, or none"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Reached = %q; want %q in it", msg, want)
		}
	}
}

// TestMoneyCapStopsBeforeThePassingSession: each session spends $0.20 and is
// held to about $0.32, so under a $1.00 cap sessions 0 to 3 run ($0.80) and
// session 4 is refused.
func TestMoneyCapStopsBeforeThePassingSession(t *testing.T) {
	a := &fakeAgent{}
	g := guard(Cap{Kind: Money, MicroUSD: 1_000_000}, a, priced())
	refused := run(t, g, 6)
	if fmt.Sprint(refused) != "[4 5]" || a.started() != 4 {
		t.Fatalf("refused %v, started %d; want [4 5], 4", refused, a.started())
	}
	msg := g.Reached("physa init --recover")
	t.Log(msg)
	if !strings.Contains(msg, "$0.80 (200k tokens)") || !strings.Contains(msg, "$1.00") {
		t.Errorf("Reached = %q; want the spend in dollars and the cap", msg)
	}
	if end := g.End(); !strings.Contains(end, "$0.80 (200k tokens) of its cap of $1.00") {
		t.Errorf("End = %q", end)
	}
}

// TestMoneyCapCountsBaseSpend checks that init's record's spend before the
// run counts toward the cap: with $0.60 spent, a $1.00 cap runs one session.
func TestMoneyCapCountsBaseSpend(t *testing.T) {
	a := &fakeAgent{}
	g := guard(Cap{Kind: Money, MicroUSD: 1_000_000}, a, priced())
	g.Base = spend.Spend{Tokens: 150_000, MicroUSD: 600_000}
	if line := g.StartLine(); !strings.Contains(line, "$1.00") || !strings.Contains(line, "$0.60 (150k tokens)") {
		t.Errorf("StartLine = %q", line)
	}
	if refused := run(t, g, 3); fmt.Sprint(refused) != "[1 2]" {
		t.Fatalf("refused %v; want [1 2]", refused)
	}
}

// TestMoneyCapWithNoPriceLetsTheFirstSessionStart: with no price known yet a
// budget counts no money, so the first session starts; it teaches the price,
// and its $0.20 is past a $0.10 cap, so the next is refused.
func TestMoneyCapWithNoPriceLetsTheFirstSessionStart(t *testing.T) {
	a := &fakeAgent{}
	g := guard(Cap{Kind: Money, MicroUSD: 100_000}, a, spend.Open(""))
	if refused := run(t, g, 3); fmt.Sprint(refused) != "[1 2]" || a.started() != 1 {
		t.Fatalf("refused %v, started %d; want [1 2], 1", refused, a.started())
	}
}

// TestShareCapReadsUsageBeforeEachStart: usage reads 50%, 60%, then 80%, so
// under an 80% cap sessions 0 and 1 run and session 2 is refused; usage is
// read before each start and not again once stopped.
func TestShareCapReadsUsageBeforeEachStart(t *testing.T) {
	r := &readerAgent{fakeAgent: &fakeAgent{}, percents: []float64{50, 60, 80, 90}}
	g := guard(Cap{Kind: Share, Percent: 80}, r, priced())
	refused := run(t, g, 5)
	if fmt.Sprint(refused) != "[2 3 4]" || r.started() != 2 || r.readCount() != 3 {
		t.Fatalf("refused %v, started %d, reads %d; want [2 3 4], 2, 3", refused, r.started(), r.readCount())
	}
	msg := g.Reached("physa init --recover")
	t.Log(msg)
	for _, want := range []string{"80%", "weekly_all resets", "once the plan's usage is under 80%"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Reached = %q; want %q in it", msg, want)
		}
	}
}

// TestShareCapBelowStartsAndUnreadableRefuses checks a share below the cap
// starts, and a reading that fails refuses the start and says why.
func TestShareCapBelowStartsAndUnreadableRefuses(t *testing.T) {
	r := &readerAgent{fakeAgent: &fakeAgent{}, percents: []float64{79.9}}
	g := guard(Cap{Kind: Share, Percent: 80}, r, priced())
	if refused := run(t, g, 3); len(refused) != 0 || r.started() != 3 {
		t.Fatalf("refused %v, started %d; want none, 3", refused, r.started())
	}
	r.err = errors.New("claude timed out")
	_, err := g.Start(context.Background(), adapter.Spec{})
	if !errors.Is(err, adapter.ErrCapped) || !strings.Contains(err.Error(), "claude timed out") {
		t.Fatalf("err = %v; want ErrCapped naming why", err)
	}
}

// TestReservationsHoldRunningSessions: three sessions started while the
// others still run; the first two hold 160k of a 200k cap, so the third is
// refused, and the two running are not cancelled and end as they would.
func TestReservationsHoldRunningSessions(t *testing.T) {
	a := &fakeAgent{gate: make(chan struct{})}
	g := guard(Cap{Kind: Tokens, Tokens: 200_000}, a, priced())
	var running []adapter.Session
	var refused []int
	for i := range 3 {
		sess, err := g.Start(context.Background(), adapter.Spec{})
		if err != nil {
			refused = append(refused, i)
			continue
		}
		running = append(running, sess)
	}
	if fmt.Sprint(refused) != "[2]" {
		t.Fatalf("refused %v; want [2]", refused)
	}
	for i, ctx := range a.ctxs {
		if ctx.Err() != nil {
			t.Fatalf("session %d was cancelled", i)
		}
	}
	close(a.gate)
	for i, s := range running {
		if res, err := s.Wait(); err != nil || res.Model != "m" {
			t.Fatalf("session %d ended %v, %v", i, res, err)
		}
	}
}

// TestConcurrentStartsRefuseOne starts three sessions at once under a cap
// two budgets fill: exactly one is refused, whichever comes last.
func TestConcurrentStartsRefuseOne(t *testing.T) {
	a := &fakeAgent{gate: make(chan struct{})}
	g := guard(Cap{Kind: Tokens, Tokens: 200_000}, a, priced())
	var wg sync.WaitGroup
	errs := make([]error, 3)
	sessions := make([]adapter.Session, 3)
	for i := range 3 {
		wg.Go(func() { sessions[i], errs[i] = g.Start(context.Background(), adapter.Spec{}) })
	}
	wg.Wait()
	close(a.gate)
	n := 0
	for i, err := range errs {
		if errors.Is(err, adapter.ErrCapped) {
			n++
			continue
		}
		if _, err := sessions[i].Wait(); err != nil {
			t.Fatal(err)
		}
	}
	if n != 1 {
		t.Fatalf("%d refused; want 1", n)
	}
}

// TestCheckRefusesAShareTheAgentCannotRead checks that a share with an agent
// that cannot report its plan's usage refuses to start and says why.
func TestCheckRefusesAShareTheAgentCannotRead(t *testing.T) {
	for name, raw := range map[string]adapter.Adapter{
		"no reader":     &fakeAgent{},
		"reads nothing": &readerAgent{fakeAgent: &fakeAgent{}, err: fmt.Errorf("%w: claude printed no usage_report", adapter.ErrNoUsage)},
	} {
		g := guard(Cap{Kind: Share, Percent: 80}, raw, priced())
		err := g.Check(context.Background())
		t.Logf("%s: %v", name, err)
		if !errors.Is(err, adapter.ErrNoUsage) || !strings.Contains(err.Error(), "fake cannot report its plan's usage") {
			t.Errorf("%s: Check = %v; want ErrNoUsage saying the agent cannot report its plan's usage", name, err)
		}
		if errors.Is(err, adapter.ErrCapped) {
			t.Errorf("%s: Check = %v; want no ErrCapped", name, err)
		}
	}
}

// TestCheckStopsAtTheCap checks that a share already at its cap, and init's
// record already at a money cap, stop the guard before anything starts.
func TestCheckStopsAtTheCap(t *testing.T) {
	r := &readerAgent{fakeAgent: &fakeAgent{}, percents: []float64{80}}
	g := guard(Cap{Kind: Share, Percent: 80}, r, priced())
	if err := g.Check(context.Background()); !errors.Is(err, adapter.ErrCapped) || !g.Stopped() {
		t.Fatalf("share Check = %v, stopped %v; want ErrCapped, true", err, g.Stopped())
	}
	if r.readCount() != 1 {
		t.Errorf("reads = %d; want 1", r.readCount())
	}
	m := guard(Cap{Kind: Money, MicroUSD: 1_000_000}, &fakeAgent{}, priced())
	m.Base = spend.Spend{Tokens: 250_000, MicroUSD: 1_000_000}
	if err := m.Check(context.Background()); !errors.Is(err, adapter.ErrCapped) || !m.Stopped() {
		t.Fatalf("money Check = %v; want ErrCapped", err)
	}
	ok := guard(Cap{Kind: Share, Percent: 80}, &readerAgent{fakeAgent: &fakeAgent{}, percents: []float64{59}}, priced())
	if err := ok.Check(context.Background()); err != nil || ok.Stopped() {
		t.Fatalf("Check below the cap = %v", err)
	}
	if line := ok.StartLine(); !strings.Contains(line, "80% of the plan's usage") || !strings.Contains(line, "59% (weekly_all resets") {
		t.Errorf("StartLine = %q", line)
	}
}

// TestGuardDelegates checks that the guard gives the agent's name, window
// and warnings.
func TestGuardDelegates(t *testing.T) {
	g := guard(Cap{Kind: Tokens, Tokens: 1}, &fakeAgent{}, priced())
	if g.Name() != "fake" || adapter.Window(g) != 1000 || adapter.Warnings(g) != nil {
		t.Fatalf("name %q, window %d", g.Name(), adapter.Window(g))
	}
}
