package recoverinput

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/place"
	"github.com/dphaener/physa/internal/reading"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/walk"
)

// Chosen is the stored walk w's capabilities to recover, as Walk reads them
// against the placement st holds and the gather g: those only names, in its
// order and each once, or all of them in recovery's order for seed. A name
// the walk lacks is an error naming it and the names the walk found.
func Chosen(st place.Loader, root string, w initrecord.Walk, g store.Gather, only []string, seed uint64) ([]Walked, error) {
	var wr walk.Record
	if err := json.Unmarshal(w.Body, &wr); err != nil {
		return nil, fmt.Errorf("the stored walk cannot be read: %w", err)
	}
	pl, _, err := place.Load(st)
	if err != nil {
		return nil, err
	}
	caps := Walk(root, wr, pl, g)
	if len(caps) == 0 {
		return nil, errors.New("the walk found no capability; run physa init --walk again")
	}
	if len(only) == 0 {
		return Order(caps, seed), nil
	}
	names := make([]string, len(caps))
	for i, c := range caps {
		names[i] = c.Name
	}
	picked, err := Choose(names, only, "no capability %q in the walk; it found: %s")
	if err != nil {
		return nil, err
	}
	out := make([]Walked, len(picked))
	for k, i := range picked {
		out[k] = caps[i]
	}
	return out, nil
}

// Choose returns the indices in names of those only names, in its order and
// each once, or of all of them when only is empty. A name not in names is an
// error, missing formatted with the name and the names there are.
func Choose(names, only []string, missing string) ([]int, error) {
	var out []int
	if len(only) == 0 {
		for i := range names {
			out = append(out, i)
		}
		return out, nil
	}
	seen := map[string]bool{}
	for _, name := range only {
		i := slices.Index(names, name)
		if i < 0 {
			return nil, fmt.Errorf(missing, name, strings.Join(names, ", "))
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, i)
		}
	}
	return out, nil
}

// Done is the set of capabilities rows holds recovered from the source at
// at (the walk's time or the proposal's): a row of another time is of an
// earlier source, and one that failed or is not yet recovered is not done.
func Done(rows []initrecord.Recovered, at time.Time) map[string]bool {
	done := map[string]bool{}
	for _, r := range rows {
		if r.Status == "recovered" && r.ProposalAt.Equal(at) {
			done[r.Capability] = true
		}
	}
	return done
}

// Plural counts n things: "1 rule", "3 rules".
func Plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// Add adds t to sum.
func Add(sum *adapter.Tokens, t adapter.Tokens) {
	sum.Input += t.Input
	sum.CachedInput += t.CachedInput
	sum.Output += t.Output
}

// Kept is the set of capabilities init's record holds recovered from the
// source at at, for a run that names none with only; a named capability is
// always recovered again, so only non-empty gives nil.
func Kept(st interface {
	Recovered() ([]initrecord.Recovered, error)
}, only []string, at time.Time) (map[string]bool, error) {
	if len(only) > 0 {
		return nil, nil
	}
	rows, err := st.Recovered()
	return Done(rows, at), err
}

// KeptLine says how many capabilities a continued run keeps from what, and
// how many it recovers.
func KeptLine(kept, left int, what string) string {
	verb := "are"
	if kept == 1 {
		verb = "is"
	}
	return fmt.Sprintf("physa init: %s already recovered from %s %s kept; recovering the other %d", Plural(kept, "capability", "capabilities"), what, verb, left)
}

// TooLong is err, a session refused as too long, with how big physa made
// its prompt, system and message, by its own estimate, and what read, its
// sources, counts. An agent's own account may count everything physa
// passes outside the message as its system prompt, which here holds only
// the skill and the docs every capability reads; the capability's own
// sources are in the message.
func TooLong(err error, system, message string, read reading.Report) error {
	return fmt.Errorf("%w (by physa's estimate, the system prompt, the recovering skill and the docs every capability reads, came to about %s tokens, and the message, with this capability's own sources, to about %s; its sources: %s)",
		err, reading.Thousands(int64(reading.Tokens(system))), reading.Thousands(int64(reading.Tokens(message))), strings.Join(read.Lines(), "; "))
}

// Resolver answers whether a PR exists: yes for every PR of g, without
// asking, and otherwise what exists says, each number asked once. It is safe
// for capabilities checked at the same time, and asks exists one call at a
// time. A nil exists gives nil: anchors are then checked for form only.
func Resolver(g store.Gather, exists func(int) (bool, error)) func(int) (bool, error) {
	if exists == nil {
		return nil
	}
	known := map[int]bool{}
	for _, p := range g.PRs {
		known[p.Number] = true
	}
	var mu sync.Mutex
	return func(n int) (bool, error) {
		mu.Lock()
		defer mu.Unlock()
		if ok, seen := known[n]; seen {
			return ok, nil
		}
		ok, err := exists(n)
		if err != nil {
			return false, err
		}
		known[n] = ok
		return ok, nil
	}
}
