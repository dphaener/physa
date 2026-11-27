package initcost

import (
	"time"

	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/reading"
	"github.com/dphaener/physa/internal/spend"
	"github.com/dphaener/physa/internal/store"
)

// Stored is what Record reads of the store: the gather, and what each
// phase spent.
type Stored interface {
	Gathered() (store.Gather, bool, error)
	Spending() ([]initrecord.Spent, error)
}

// Record keeps what a recovery of every proposed capability measured, for
// the next estimate (spend.Cache.RecordRatio): recovered, recovery's spend,
// against the stored gather's estimated tokens, and propose's spend as the
// store holds it, whether it ran in the same run or in an earlier
// --propose, against its raw tokens. Unpriced spend would mix units, and a
// store without a gather or a propose holds nothing to measure against, so
// neither is recorded.
func Record(c *spend.Cache, st Stored, recovered spend.Count, at time.Time) {
	g, ok, err := st.Gathered()
	if err != nil || !ok {
		return
	}
	spent, err := st.Spending()
	if err != nil {
		return
	}
	for _, p := range spent {
		if p.Phase == "propose" && !p.Spend.Unpriced && !recovered.Spend.Unpriced {
			c.RecordRatio(recovered.Spend.Tokens, int64(reading.GatherTokens(g)), p.Spend.Tokens, p.Raw.Input+p.Raw.Output, at)
		}
	}
}
