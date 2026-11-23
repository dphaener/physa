package spend

import (
	"slices"
	"time"
)

// The defaults init estimates with before two runs are recorded, in
// thousandths, measured in weighted tokens on nebari's run 9 (design 10 §3):
// recovery spent 12.5M weighted tokens for sources estimated at 998k, about
// 12.5 times, and propose 1.4M weighted for 682,510 raw, about 2.05 times,
// since a one-turn session writes its whole prompt to a 1-hour cache at
// twice input. The range around the recovery ratio is ×0.5 to ×1.5.
const (
	DefaultRecoverRatio = 12_500
	DefaultProposeRatio = 2_050
)

// Ratio is what one finished init run measured, in thousandths: Recover is
// recovery's weighted tokens ÷ the gathered sources' estimated tokens, and
// Propose is propose's weighted tokens ÷ its raw tokens.
type Ratio struct {
	Recover int64     `json:"recover_milli"`
	Propose int64     `json:"propose_milli"`
	At      time.Time `json:"at"`
}

// Ratios is the range init estimates with: the recovery ratio's and
// propose's factor's, in thousandths, from Measured runs (0 for the
// defaults).
type Ratios struct {
	RecoverLo, RecoverHi int64
	ProposeLo, ProposeHi int64
	Measured             int
}

// RecordRatio keeps what a finished init run measured from its weighted
// spend: recovery's weighted tokens against the sources' estimated tokens,
// and propose's weighted tokens against its raw ones. A run missing any of
// them is not recorded.
func (c *Cache) RecordRatio(recovered, sources, proposed, proposeRaw int64, at time.Time) {
	if recovered <= 0 || sources <= 0 || proposed <= 0 || proposeRaw <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Ratios = append(c.Ratios, Ratio{Recover: recovered * 1000 / sources, Propose: proposed * 1000 / proposeRaw, At: at})
}

// RatioRange is the range init estimates with: the smallest and the largest
// of each recorded ratio once two runs are recorded; else the recovery
// default × 0.5 to × 1.5 and propose's default factor.
func (c *Cache) RatioRange() Ratios {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.Ratios) < 2 {
		return Ratios{RecoverLo: DefaultRecoverRatio / 2, RecoverHi: DefaultRecoverRatio * 3 / 2, ProposeLo: DefaultProposeRatio, ProposeHi: DefaultProposeRatio}
	}
	first := c.Ratios[0]
	r := Ratios{RecoverLo: first.Recover, RecoverHi: first.Recover, ProposeLo: first.Propose, ProposeHi: first.Propose, Measured: len(c.Ratios)}
	for _, x := range c.Ratios {
		r.RecoverLo, r.RecoverHi = min(r.RecoverLo, x.Recover), max(r.RecoverHi, x.Recover)
		r.ProposeLo, r.ProposeHi = min(r.ProposeLo, x.Propose), max(r.ProposeHi, x.Propose)
	}
	return r
}

// mergeRatios is a and b together, each run once, oldest first.
func mergeRatios(a, b []Ratio) []Ratio {
	out := slices.Clone(a)
	for _, r := range b {
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(x, y Ratio) int { return x.At.Compare(y.At) })
	return out
}
