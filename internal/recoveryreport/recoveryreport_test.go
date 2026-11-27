package recoveryreport

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/recovering"
	"github.com/dphaener/physa/internal/recovery"
)

func report(used int64) recovery.Report {
	return recovery.Report{
		Used: adapter.Tokens{Input: used},
		Capabilities: []recovery.Outcome{
			{Name: "delivery", Status: recovery.StatusRecovered, Counts: recovering.Counts{Rules: 3, Decisions: 1, Terms: 1}, Attempts: 1},
			{Name: "search", Status: recovery.StatusNotYet},
		},
	}
}

func TestWriteTextWordsCounts(t *testing.T) {
	var b bytes.Buffer
	if err := WriteText(&b, report(1)); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Recovered 1, not yet recovered 1, failed 0; used 1 token.",
		"Kept 3 rules, 1 decision, 0 questions, 1 term.", "Attempts: 1; session none;",
		"run again with --only search", "- search",
	} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("missing %q in\n%s", want, b.String())
		}
	}
	b.Reset()
	if err := WriteText(&b, report(100)); err != nil || !strings.Contains(b.String(), "used 100 tokens.") {
		t.Errorf("err %v, text\n%s", err, b.String())
	}
}

func TestWriteJSON(t *testing.T) {
	var b bytes.Buffer
	if err := WriteJSON(&b, report(5)); err != nil || !strings.Contains(b.String(), `"delivery"`) {
		t.Errorf("err %v, json\n%s", err, b.String())
	}
}

func TestExitCode(t *testing.T) {
	of := func(statuses ...string) recovery.Report {
		var r recovery.Report
		for _, s := range statuses {
			r.Capabilities = append(r.Capabilities, recovery.Outcome{Status: s})
		}
		return r
	}
	for _, c := range []struct {
		r    recovery.Report
		want int
	}{
		{of(), 0},
		{of(recovery.StatusRecovered, recovery.StatusRecovered), 0},
		{of(recovery.StatusRecovered, recovery.StatusNotYet), 1},
		{of(recovery.StatusNotYet, recovery.StatusFailed, recovery.StatusRecovered), 2},
	} {
		if got := ExitCode(c.r); got != c.want {
			t.Errorf("ExitCode(%d capabilities) = %d, want %d", len(c.r.Capabilities), got, c.want)
		}
	}
}
