package recoverinput

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/place"
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
