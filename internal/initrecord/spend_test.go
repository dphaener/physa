package initrecord

import (
	"reflect"
	"testing"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/spend"
)

func sampleSpent(phase string, input, output, tokens, micro int64) Spent {
	return Spent{Phase: phase, Count: spend.Count{
		Raw:   adapter.Tokens{Input: input, Output: output},
		Spend: spend.Spend{Tokens: tokens, MicroUSD: micro},
	}}
}

func TestSpending(t *testing.T) {
	t.Run("run order with an unknown phase last", func(t *testing.T) {
		s, _ := open(t)
		// Added out of run order; Spending must put them back in it.
		for _, p := range []Spent{
			sampleSpent("walk", 1, 1, 1, 1),
			sampleSpent("recover", 1, 1, 1, 1),
			sampleSpent("zeta", 1, 1, 1, 1),
			sampleSpent("discover", 1, 1, 1, 1),
			sampleSpent("consolidate", 1, 1, 1, 1),
			sampleSpent("propose", 1, 1, 1, 1),
			sampleSpent("areas", 1, 1, 1, 1),
		} {
			if err := s.AddSpent(t.Context(), p); err != nil {
				t.Fatal(err)
			}
		}
		got, err := s.Spending(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		var phases []string
		for _, p := range got {
			phases = append(phases, p.Phase)
		}
		want := []string{"discover", "areas", "walk", "propose", "recover", "consolidate", "zeta"}
		if !reflect.DeepEqual(phases, want) {
			t.Errorf("phases = %v; want %v", phases, want)
		}
	})

	t.Run("a second add adds to the phase's row", func(t *testing.T) {
		s, _ := open(t)
		if err := s.AddSpent(t.Context(), sampleSpent("walk", 10, 3, 5, 7)); err != nil {
			t.Fatal(err)
		}
		if err := s.AddSpent(t.Context(), sampleSpent("walk", 20, 4, 6, 8)); err != nil {
			t.Fatal(err)
		}
		got, err := s.Spending(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Fatalf("rows = %d; want 1", len(got))
		}
		p := got[0]
		if p.Raw.Input != 30 || p.Raw.Output != 7 || p.Spend.Tokens != 11 || p.Spend.MicroUSD != 15 {
			t.Errorf("walk = %+v; want input 30, output 7, tokens 11, micro_usd 15", p)
		}
	})

	t.Run("an empty store has no rows and no error", func(t *testing.T) {
		s, _ := open(t)
		got, err := s.Spending(t.Context())
		if err != nil {
			t.Fatalf("Spending error = %v; want nil", err)
		}
		if len(got) != 0 {
			t.Errorf("Spending = %+v; want no rows", got)
		}
	})
}
