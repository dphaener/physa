package initrecord

import (
	"testing"
	"time"

	"github.com/dphaener/physa/internal/store"
)

// open opens a store in a fresh directory and gives its init record.
func open(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return Of(st), dir
}

// sampleGather is a gather with a run, enough for PutGather to replace the
// stored one.
func sampleGather() store.Gather {
	t0 := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	return store.Gather{Run: store.GatherRun{At: t0, Since: t0.AddDate(-1, 0, 0), MaxPRs: 1000, Forge: "github"}}
}
