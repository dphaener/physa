package recovery

import (
	"sync"

	"github.com/dphaener/physa/internal/store"
)

// resolver answers whether a PR exists: yes for every PR of g, without
// asking, and otherwise what exists says, each number asked once. It is safe
// for capabilities checked at the same time, and asks exists one call at a
// time. A nil exists gives nil: anchors are then checked for form only.
func resolver(g store.Gather, exists func(int) (bool, error)) func(int) (bool, error) {
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
