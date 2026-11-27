package adapter

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrNoUsage is returned by ReadUsage when the agent does not report its
// plan's usage, or reported no window that applies now. It is wrapped with
// the reason.
var ErrNoUsage = errors.New("the agent does not report its plan's usage")

// ErrCapped is returned by Start, before running anything, by the guard of a
// cap on init's spend when the session would pass the cap. It is wrapped with
// the reason.
var ErrCapped = errors.New("not started: the session would pass init's cap")

// Limit is one window of the plan's usage.
type Limit struct {
	// Kind is the window's name as the agent gives it.
	Kind string
	// Percent is the share of the window spent, 0 to 100.
	Percent float64
	// ResetsAt is when the window starts over.
	ResetsAt time.Time
	// Active is whether the window applies now.
	Active bool
}

// Usage is what an agent reports of the person's plan.
type Usage struct {
	Limits []Limit
}

// Binding is the most restrictive limit that applies now: the highest
// Percent among the active ones, the earlier Kind on a tie. It is false when
// no limit is active.
func (u Usage) Binding() (Limit, bool) {
	var best Limit
	found := false
	for _, l := range u.Limits {
		if !l.Active {
			continue
		}
		if !found || l.Percent > best.Percent || (l.Percent == best.Percent && l.Kind < best.Kind) {
			best, found = l, true
		}
	}
	return best, found
}

// UsageReader is an Adapter that can read its plan's usage. An adapter that
// does not implement it reports none.
type UsageReader interface {
	// Usage reads the plan's usage now.
	Usage(ctx context.Context) (Usage, error)
}

// ReadUsage is a's plan usage now. The error wraps ErrNoUsage when a does not
// implement UsageReader, and when its report has no active limit.
func ReadUsage(ctx context.Context, a Adapter) (Usage, error) {
	r, ok := a.(UsageReader)
	if !ok {
		return Usage{}, fmt.Errorf("%w: %s cannot read it", ErrNoUsage, nameOf(a))
	}
	u, err := r.Usage(ctx)
	if err != nil {
		return Usage{}, err
	}
	if _, ok := u.Binding(); !ok {
		return Usage{}, fmt.Errorf("%w: %s reported no limit that applies now", ErrNoUsage, nameOf(a))
	}
	return u, nil
}

func nameOf(a Adapter) string {
	if a == nil {
		return "no agent"
	}
	return a.Name()
}
