package learn

import (
	"strings"

	"github.com/dphaener/physa/internal/history"
	"github.com/dphaener/physa/internal/vcs"
)

// throughKey is the trailer that records how far learning has got: the
// newest default-branch change learned from, with every change before it
// learned (design 08 section 6).
const throughKey = "Learned-through"

// NoWatermark is the Learned-through value of a record made before any
// change was learned through: the run started at the history's first change
// and the oldest it collected was not fully triaged. It names no change, so
// reading it gives no watermark, but it still marks the commit as a record.
const NoWatermark = "none"

// readWatermark returns the full id of the latest change named by the newest
// Learned-through trailer on each of the learn branch's refs and on the
// default branch ref, or "" when none names a change part of ref's history.
// One that names no such change is ignored. Of two that are not part of each
// other's history, the one read first is kept: the default branch's, then
// the local learn branch's, then the remote's.
func readWatermark(repo vcs.Repo, ref string) (string, error) {
	best := ""
	for _, from := range append([]string{ref}, repo.Branch(learnBranch)...) {
		c, ok, err := repo.Trailer(from, throughKey)
		if err != nil {
			return "", err
		}
		if !ok {
			continue
		}
		named := history.Trailers(c.Message, throughKey)
		if len(named) == 0 || strings.EqualFold(named[len(named)-1], NoWatermark) || !repo.HasChange(named[len(named)-1]) {
			continue
		}
		id, ok := repo.Resolve(named[len(named)-1])
		if !ok || !repo.IsAncestor(id, ref) {
			continue
		}
		if best == "" || (id != best && repo.IsAncestor(best, id)) {
			best = id
		}
	}
	return best, nil
}

// isRecord reports whether c is one of learning's own records: a change
// carrying a Learned-through trailer, such as a learning commit or the
// squash merge of the learn branch. Records are never learned from.
func isRecord(c vcs.Change) bool {
	return len(history.Trailers(c.Message, throughKey)) > 0
}

// learnedIDs returns the ids named by Learned trailers, lower-cased, on the
// collected changes (a squash merge of the learn branch carries them) and on
// the learn branch's commits, here and on the remote, that are not on the
// default branch ref. The learn branch's older history is never read: what
// it learned is recorded by the watermark.
func learnedIDs(repo vcs.Repo, ref string, collected []vcs.Change) ([]string, error) {
	var ids []string
	add := func(changes []vcs.Change) {
		for _, c := range changes {
			for _, id := range history.Trailers(c.Message, "Learned") {
				ids = append(ids, strings.ToLower(id))
			}
		}
	}
	add(collected)
	for _, branch := range repo.Branch(learnBranch) {
		changes, err := repo.Changes(ref + ".." + branch)
		if err != nil {
			return nil, err
		}
		add(changes)
	}
	return ids, nil
}

// nextWatermark walks the collected changes, newest first in collected,
// from the oldest, and returns the full id of the newest change the
// watermark can pass: through every change learned, with signals or with
// nothing to learn, up to the first not fully triaged. Learning's own
// records neither move it nor stop it. It returns the read watermark when it
// would not move past it.
func nextWatermark(repo vcs.Repo, read string, collected []vcs.Change, incomplete map[string]bool) string {
	through := ""
	for i := len(collected) - 1; i >= 0; i-- {
		c := collected[i]
		if isRecord(c) {
			continue
		}
		if incomplete[c.ID] {
			break
		}
		through = c.ID
	}
	if read == "" {
		return through
	}
	if through == "" || through == read || !repo.IsAncestor(read, through) {
		return read
	}
	return through
}

// markID abbreviates a watermark for people; "none" when there is none.
func markID(id string) string {
	if id == "" {
		return "none"
	}
	return shortID(id)
}
