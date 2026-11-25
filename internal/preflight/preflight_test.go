package preflight

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/gather"
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
