package spend

import (
	"context"
	"sync"
	"time"

	"github.com/dphaener/physa/internal/adapter"
)

// Meter is an adapter whose sessions are weighed as each ends: the prices
// each session's cost shows are learned into Cache, and its spend is added
// to what the meter has counted. It never stops a session.
type Meter struct {
	adapter.Adapter
	// Cache holds the prices sessions are weighed by, and learns theirs.
	Cache *Cache
	// Now dates what is learned; nil means time.Now.
	Now func() time.Time
	// Ended, when set, is called after each session ends with all the meter
	// has counted, outside any lock.
	Ended func(total Spend)

	mu    sync.Mutex
	total Spend
	raw   adapter.Tokens
}

// Count is what a meter has counted: its sessions' spend, weighed, and their
// raw tokens.
type Count struct {
	Spend Spend
	Raw   adapter.Tokens
}

// Since is what c counted beyond o, an earlier count of the same meter.
func (c Count) Since(o Count) Count {
	s := Spend{Tokens: c.Spend.Tokens - o.Spend.Tokens, MicroUSD: c.Spend.MicroUSD - o.Spend.MicroUSD, Unpriced: c.Spend.Unpriced}
	r := adapter.Tokens{Input: c.Raw.Input - o.Raw.Input, CachedInput: c.Raw.CachedInput - o.Raw.CachedInput, Output: c.Raw.Output - o.Raw.Output}
	return Count{Spend: s, Raw: r}
}

// Count is all the meter has counted so far; a nil meter has counted
// nothing.
func (m *Meter) Count() Count {
	if m == nil {
		return Count{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return Count{Spend: m.total, Raw: m.raw}
}

// Name is the adapter's name; empty for a meter with no adapter, as when the
// agent could not be resolved.
func (m *Meter) Name() string {
	if m == nil || m.Adapter == nil {
		return ""
	}
	return m.Adapter.Name()
}

// Window is the window the meter's adapter states, 0 when it states none or
// there is none.
func (m *Meter) Window() int {
	if m == nil || m.Adapter == nil {
		return 0
	}
	return adapter.Window(m.Adapter)
}

// Start starts s on the meter's adapter, weighing the session when it ends.
func (m *Meter) Start(ctx context.Context, s adapter.Spec) (adapter.Session, error) {
	sess, err := m.Adapter.Start(ctx, s)
	if err != nil {
		return nil, err
	}
	return metered{sess, m}, nil
}

// Spent is all the meter has counted so far.
func (m *Meter) Spent() Spend {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.total
}

// metered is a session whose result the meter weighs.
type metered struct {
	adapter.Session
	m *Meter
}

func (s metered) Wait() (adapter.Result, error) {
	res, err := s.Session.Wait()
	m := s.m
	now := time.Now
	if m.Now != nil {
		now = m.Now
	}
	m.Cache.Learn(m.Name(), res, now())
	w := m.Cache.Weighted(m.Name(), res)
	m.mu.Lock()
	m.total = m.total.Add(w)
	m.raw.Input, m.raw.CachedInput, m.raw.Output = m.raw.Input+res.Tokens.Input, m.raw.CachedInput+res.Tokens.CachedInput, m.raw.Output+res.Tokens.Output
	total := m.total
	m.mu.Unlock()
	if m.Ended != nil {
		m.Ended(total)
	}
	return res, err
}

// Input is n input tokens of the default model of the adapter named agent,
// weighed: n weighted tokens, at that model's input price when it has one,
// else Unpriced.
func (c *Cache) Input(agent string, n int64) Spend {
	_, p, ok := c.Reference(agent)
	if !ok || p.PerMillion <= 0 {
		return Spend{Tokens: n, Unpriced: n > 0}
	}
	return Spend{Tokens: n, MicroUSD: n * p.PerMillion / 1_000_000}
}

// Range reads like "1.9M to 5.6M tokens (about $7.60 to $23)", or without
// the cost when either end is Unpriced.
func Range(lo, hi Spend) string {
	s := Tokens(lo.Tokens) + " to " + Tokens(hi.Tokens) + " tokens"
	if lo.Unpriced || hi.Unpriced {
		return s
	}
	return s + " (about " + Dollars(lo.MicroUSD) + " to " + Dollars(hi.MicroUSD) + ")"
}
