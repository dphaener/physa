package initcap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/reading"
	"github.com/dphaener/physa/internal/spend"
)

// Guard is an adapter that starts a session on Agent only when the session's
// budget would not pass Cap. Once it refuses a start it refuses every later
// one, so a stopped run does not skip ahead to smaller sessions; sessions
// already running are never cancelled, so a run can end past its cap by what
// they spend over their budgets. It is safe for concurrent use.
type Guard struct {
	// Cap is what init's spend is capped at.
	Cap Cap
	// Agent is what sessions start on: the metered agent.
	Agent adapter.Adapter
	// Raw is the agent a share's usage is read from; nil means Agent.
	Raw adapter.Adapter
	// Cache prices a session's budget for a money cap; nil prices nothing.
	Cache *spend.Cache
	// AgentName is the agent whose default model a budget is priced at.
	AgentName string
	// Base is what init's record had spent before this run.
	Base spend.Spend
	// Spent is what this run has spent so far; nil means Agent's, when Agent
	// is a *spend.Meter, else nothing.
	Spent func() spend.Spend

	mu       sync.Mutex
	stopped  bool
	why      string
	reserved spend.Spend
	ended    int64
	endedSum int64
	limit    adapter.Limit
	read     bool
	readErr  error
}

// Name is Agent's name.
func (g *Guard) Name() string { return g.Agent.Name() }

// Window is the window Agent states, 0 when it states none.
func (g *Guard) Window() int { return adapter.Window(g.Agent) }

// Warnings is what the raw agent warns of before any session.
func (g *Guard) Warnings() []string { return adapter.Warnings(g.raw()) }

func (g *Guard) raw() adapter.Adapter {
	if g.Raw != nil {
		return g.Raw
	}
	return g.Agent
}

// spent is init's record's spend before this run and this run's together.
func (g *Guard) spent() spend.Spend {
	s := g.Base
	switch {
	case g.Spent != nil:
		s = s.Add(g.Spent())
	default:
		if m, ok := g.Agent.(*spend.Meter); ok {
			s = s.Add(m.Spent())
		}
	}
	return s
}

// Stopped reports whether the guard has refused a start; a nil guard never
// has.
func (g *Guard) Stopped() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.stopped
}

// budget is what a session of spec is held to before it runs, in weighted
// tokens: its system and prompt twice over, since a one-turn session writes
// them to the 1-hour cache at twice input, and the reply reserve five times
// over, since output weighs five times input; or the mean of the sessions
// this guard has seen end, when larger. Call with mu held.
func (g *Guard) budget(spec adapter.Spec) int64 {
	b := 2*int64(reading.Tokens(spec.System+spec.Prompt)) + 5*int64(mapping.ReplyReserve)
	if g.ended > 0 && g.endedSum/g.ended > b {
		b = g.endedSum / g.ended
	}
	return b
}

// price is n weighted tokens at the agent's default model's input price;
// with no price known it counts no money.
func (g *Guard) price(n int64) spend.Spend {
	if g.Cache == nil {
		return spend.Spend{Tokens: n, Unpriced: n > 0}
	}
	return g.Cache.Input(g.AgentName, n)
}

// refuse latches the guard stopped for why and is the error Start returns.
// Call with mu held.
func (g *Guard) refuse(why string) error {
	if !g.stopped {
		g.stopped, g.why = true, why
	}
	return fmt.Errorf("%w: %s", adapter.ErrCapped, g.why)
}

// Start starts spec on Agent unless its budget would pass the cap: for money
// and tokens, when what init has spent, the budgets of sessions running and
// this one's together pass it; for a share, when the plan's usage, read now,
// is at or over it, or cannot be read. A refused start returns an error
// wrapping adapter.ErrCapped, and so does every start after it.
func (g *Guard) Start(ctx context.Context, spec adapter.Spec) (adapter.Session, error) {
	if g.Cap.Kind == Share {
		if err := g.readShare(ctx); err != nil {
			return nil, err
		}
	}
	g.mu.Lock()
	if g.stopped {
		defer g.mu.Unlock()
		return nil, g.refuse("")
	}
	hold := g.price(g.budget(spec))
	if g.Cap.Kind == Money || g.Cap.Kind == Tokens {
		spent := g.spent()
		total := spent.Add(g.reserved).Add(hold)
		total.Unpriced = spent.Unpriced
		if g.Cap.over(total) {
			defer g.mu.Unlock()
			return nil, g.refuse(fmt.Sprintf("a session held to %s would take init's spend to %s, past its cap of %s",
				g.Cap.Show(hold), g.Cap.Show(total), g.Cap))
		}
	}
	g.reserved = g.reserved.Add(hold)
	g.reserved.Unpriced = false
	g.mu.Unlock()
	sess, err := g.Agent.Start(ctx, spec)
	if err != nil {
		g.release(hold, nil)
		return nil, err
	}
	return &guarded{Session: sess, g: g, hold: hold}, nil
}

// readShare reads the plan's usage before a session and refuses the start
// when the binding limit is at or over the cap, or the usage cannot be read.
func (g *Guard) readShare(ctx context.Context) error {
	if g.Stopped() {
		return nil
	}
	u, err := adapter.ReadUsage(ctx, g.raw())
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.judge(u, err)
}

// judge refuses when the reading u failed with err or its binding limit is
// at or over the cap, and keeps it for the messages. Call with mu held.
func (g *Guard) judge(u adapter.Usage, err error) error {
	if g.stopped {
		return g.refuse("")
	}
	if err != nil {
		g.readErr = err
		return g.refuse("the plan's usage could not be read: " + err.Error())
	}
	g.limit, _ = u.Binding()
	g.read, g.readErr = true, nil
	if g.limit.Percent >= g.Cap.Percent {
		return g.refuse(fmt.Sprintf("the plan's usage is %s, at or over the cap of %s", g.usage(), g.Cap))
	}
	return nil
}

// release gives back a session's hold and, when it ended with r, counts its
// weighted spend for the mean.
func (g *Guard) release(hold spend.Spend, r *adapter.Result) {
	var w int64
	if r != nil {
		if g.Cache != nil {
			w = g.Cache.Weighted(g.AgentName, *r).Tokens
		} else {
			w = r.Tokens.Input + r.Tokens.Output
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.reserved = spend.Spend{Tokens: g.reserved.Tokens - hold.Tokens, MicroUSD: g.reserved.MicroUSD - hold.MicroUSD}
	if r != nil {
		g.ended++
		g.endedSum += w
	}
}

// guarded is a session whose hold the guard gives back when it ends.
type guarded struct {
	adapter.Session
	g    *Guard
	hold spend.Spend
	once sync.Once
}

func (s *guarded) Wait() (adapter.Result, error) {
	res, err := s.Session.Wait()
	s.once.Do(func() { s.g.release(s.hold, &res) })
	return res, err
}

// noUsage is the error Check returns when the agent cannot report its plan's
// usage; it wraps adapter.ErrNoUsage and the reason.
type noUsage struct {
	agent string
	err   error
}

func (e noUsage) Error() string {
	why := strings.TrimPrefix(e.err.Error(), adapter.ErrNoUsage.Error()+": ")
	return e.agent + " cannot report its plan's usage: " + why
}

func (e noUsage) Unwrap() []error { return []error{adapter.ErrNoUsage, e.err} }

// Check runs before anything starts. For a share it reads the plan's usage
// once: an agent that cannot report it gives an error wrapping
// adapter.ErrNoUsage that says why, and usage already at or over the cap
// stops the guard and wraps adapter.ErrCapped. For money and tokens, init's
// record already at the cap stops the guard the same way.
func (g *Guard) Check(ctx context.Context) error {
	switch g.Cap.Kind {
	case Share:
		raw := g.raw()
		if _, ok := raw.(adapter.UsageReader); !ok {
			return noUsage{g.Name(), fmt.Errorf("%w: it has no way to read it", adapter.ErrNoUsage)}
		}
		u, err := adapter.ReadUsage(ctx, raw)
		if err != nil {
			return noUsage{g.Name(), err}
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		return g.judge(u, nil)
	case Money, Tokens:
		g.mu.Lock()
		defer g.mu.Unlock()
		if s := g.Base; g.Cap.at(s) {
			return g.refuse(fmt.Sprintf("init's record has already spent %s, at or past its cap of %s", g.Cap.Show(s), g.Cap))
		}
	}
	return nil
}

// usage is the last reading as a person reads it: "59% (weekly_all resets
// Mon 30 Nov 14:00 UTC)". Call with mu held.
func (g *Guard) usage() string {
	if !g.read {
		return "not read yet"
	}
	return fmt.Sprintf("%s%% (%s)", trimFloat(g.limit.Percent), g.resets())
}

// resets is the binding limit's kind and when it resets. Call with mu held.
func (g *Guard) resets() string {
	if g.limit.ResetsAt.IsZero() {
		return g.limit.Kind
	}
	return g.limit.Kind + " resets " + g.limit.ResetsAt.Local().Format("Mon 2 Jan 15:04 MST")
}

func trimFloat(f float64) string { return strings.TrimSuffix(fmt.Sprintf("%.1f", f), ".0") }

// StartLine is what initrun prints once before the first session when a cap
// is set, after "physa init: ": the cap, that init stops before a session
// that would pass it, and what init's record has spent so far in the cap's
// unit, or, for a share, the plan's usage as read. A nil guard, a run with no
// cap, says init runs to the end.
func (g *Guard) StartLine() string {
	if g == nil {
		return "no appetite and no circuit breaker: init runs to the end."
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Cap.Kind == Share {
		return fmt.Sprintf("the cap is %s of the plan's usage: init starts no session while it is at or over that; the plan's usage is %s.",
			g.Cap, g.usage())
	}
	return fmt.Sprintf("the cap is %s: init stops before a session that would pass it; init has spent %s so far.",
		g.Cap, g.Cap.Show(g.spent()))
}

// Reached is what initrun prints after "physa init: " when the guard
// stopped a run: the cap was reached, nothing more was started, sessions
// running finished, what init has spent in the cap's unit (or the plan's
// usage), and how to continue with continueLine.
func (g *Guard) Reached(continueLine string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	head := fmt.Sprintf("the cap of %s was reached: nothing more was started and sessions already running finished; ", g.Cap)
	if g.Cap.Kind != Share {
		return head + fmt.Sprintf("init has spent %s. To continue, run %s with a larger --cap, or none.",
			g.Cap.Show(g.spent()), continueLine)
	}
	if g.readErr != nil {
		return head + fmt.Sprintf("the plan's usage could not be read (%v). To continue, run %s once it can be read and is under %s.",
			g.readErr, continueLine, g.Cap)
	}
	return head + fmt.Sprintf("the plan's usage is %s. To continue, run %s once the plan's usage is under %s (%s).",
		g.usage(), continueLine, g.Cap, g.resets())
}

// End is what initrun prints after "physa init: " when a capped run ended
// without being stopped: what init has spent, in the cap's unit, against the
// cap.
func (g *Guard) End() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Cap.Kind == Share {
		return fmt.Sprintf("init has spent %s; the plan's usage was %s when last read, under the cap of %s.",
			g.spent(), g.usage(), g.Cap)
	}
	return fmt.Sprintf("init has spent %s of its cap of %s.", g.Cap.Show(g.spent()), g.Cap)
}

// Spending is init's record's spend, phase by phase.
type Spending interface {
	Spending() ([]initrecord.Spent, error)
}

// Run is what Start needs of a capped run of init.
type Run struct {
	// Cap is the run's cap.
	Cap Cap
	// Agent resolves the agent sessions start on: the metered agent.
	Agent func() (adapter.Adapter, error)
	// Cache prices budgets.
	Cache *spend.Cache
	// Record is init's record, whose spend so far is the guard's Base unless
	// Fresh: a run that gathers anew clears it.
	Record Spending
	Fresh  bool
	// Again is the line that continues the run when the cap is already reached.
	Again string
	// Say prints the cap's StartLine; a bare run says it in its own first line.
	Say bool
	// Out is where Start prints, each line after "physa init: ".
	Out io.Writer
}

// Start holds a capped run's sessions to its cap from here on and checks it
// before anything starts. It returns the guard, nil when the agent cannot be
// resolved (the phase then says why), and the code to exit with, 0 to go on:
// 2 when the agent cannot report the usage a share needs, saying why, or
// init's record cannot be read; 1 when the cap is already reached, saying
// how to continue.
func Start(ctx context.Context, r Run) (*Guard, int) {
	a, err := r.Agent()
	if err != nil {
		return nil, 0
	}
	g := &Guard{Cap: r.Cap, Agent: a, Raw: a, Cache: r.Cache, AgentName: a.Name()}
	if m, ok := a.(*spend.Meter); ok {
		g.Raw = m.Adapter
	}
	if !r.Fresh {
		rows, err := r.Record.Spending()
		if err != nil {
			fmt.Fprintf(r.Out, "physa init: %v\n", err)
			return g, 2
		}
		g.Base = Sum(rows)
	}
	err = g.Check(ctx)
	switch {
	case errors.Is(err, adapter.ErrNoUsage):
		fmt.Fprintf(r.Out, "physa init: --cap %s needs the agent's plan usage, and %v\n", g.Cap, err)
		return g, 2
	case err != nil:
		fmt.Fprintf(r.Out, "physa init: %s\n", g.Reached(r.Again))
		return g, 1
	}
	if r.Say {
		fmt.Fprintf(r.Out, "physa init: %s\n", g.StartLine())
	}
	return g, 0
}

// Finish says on w, after "physa init: ", how a capped run with code ended
// against its cap and returns its code: when the guard stopped it, Reached
// with again and a code of at least 1; otherwise End and code. A nil guard
// says nothing.
func (g *Guard) Finish(w io.Writer, code int, again string) int {
	switch {
	case g == nil:
		return code
	case g.Stopped():
		fmt.Fprintf(w, "physa init: %s\n", g.Reached(again))
		return max(code, 1)
	}
	fmt.Fprintf(w, "physa init: %s\n", g.End())
	return code
}
