package preflight

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/gather"
	"github.com/dphaener/physa/internal/store"
)

func TestGatherWarnsOfAThinGather(t *testing.T) {
	full := gather.Summary{PRs: gather.PRsSummary{Available: true, Count: 3}}
	if w := Gather(full); w != nil {
		t.Errorf("a full gather: %q", w)
	}
	// A forge that worked and found none is a young repo's: no warning.
	if w := Gather(gather.Summary{PRs: gather.PRsSummary{Available: true}}); w != nil {
		t.Errorf("no PRs found: %q", w)
	}
	thin := gather.Summary{History: gather.HistorySummary{Shallow: true}, PRs: gather.PRsSummary{Reason: "no auth"}}
	w := Gather(thin)
	if len(w) != 2 || !strings.HasPrefix(w[0], "the clone is shallow") || !strings.HasPrefix(w[1], "no PRs were gathered (no auth)") {
		t.Errorf("shallow and no forge: %q", w)
	}
}

func TestWarnWritesALineEach(t *testing.T) {
	var b bytes.Buffer
	Warn(&b, []string{"one", "two"})
	if got := b.String(); got != "physa init: warning: one\nphysa init: warning: two\n" {
		t.Errorf("Warn wrote %q", got)
	}
}

func TestStoppedWarnsOfAStoppedGather(t *testing.T) {
	asked := time.Date(2025, 11, 26, 0, 0, 0, 0, time.UTC)
	run := store.GatherRun{Since: asked, Stopped: true, Reached: time.Date(2026, 6, 4, 9, 0, 0, 0, time.UTC),
		StopReason: "still limited after waiting 3m0s in total: secondary rate limit of the forge"}
	w := Stopped(run)
	if len(w) != 1 {
		t.Fatalf("a stopped gather: %q", w)
	}
	for _, want := range []string{"stopped short by the forge", "secondary rate limit of the forge", "its PRs reach back to 2026-06-04", "of the asked 2025-11-26",
		"runs on what was gathered", "miss the PRs merged before 2026-06-04", "physa init --gather"} {
		if !strings.Contains(w[0], want) {
			t.Errorf("warning %q lacks %q", w[0], want)
		}
	}
	run.Reached = time.Time{}
	if w := Stopped(run); len(w) != 1 || !strings.Contains(w[0], "no PR was listed of the asked 2025-11-26") || !strings.Contains(w[0], "miss every PR since 2025-11-26") {
		t.Errorf("stopped with no PR listed: %q", w)
	}
}

func TestStoppedSaysNothingOfACompleteGather(t *testing.T) {
	if w := Stopped(store.GatherRun{Since: time.Now(), Forge: "github"}); w != nil {
		t.Errorf("a complete gather: %q", w)
	}
}
