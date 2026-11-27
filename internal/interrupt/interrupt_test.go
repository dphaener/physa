package interrupt

import (
	"bytes"
	"context"
	"testing"
)

func TestInterruptedOnlyOnceCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	if Interrupted(ctx) {
		t.Error("a live context is interrupted")
	}
	cancel()
	if !Interrupted(ctx) {
		t.Error("a cancelled context is not interrupted")
	}
}

func TestInterruptSaysWhatWasKeptAndHowToContinue(t *testing.T) {
	var w bytes.Buffer
	if code := Say(&w, Kept(2), "physa init --recover"); code != 130 {
		t.Errorf("code %d, want 130", code)
	}
	want := "physa init: interrupted: nothing more was started and sessions running were stopped; 2 capabilities recovered in this run are kept. To continue, run physa init --recover.\n"
	if w.String() != want {
		t.Errorf("said %q\nwant %q", w.String(), want)
	}
	for got, want := range map[string]string{
		Kept(0):             "0 capabilities recovered in this run are kept",
		Kept(1):             "1 capability recovered in this run is kept",
		Nothing("the walk"): "nothing of the walk was stored",
	} {
		if got != want {
			t.Errorf("%q, want %q", got, want)
		}
	}
}
