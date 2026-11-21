// Package history reads anchors from the history of changes, through version
// control: the changes of a range with the capability each one's Capability
// trailer names and the sessions its Session trailers name, and checks the
// capabilities as design 07 section 5 asks. Every change
// names exactly one capability, and its slug is in the map as that change
// leaves it. It reads history only through a vcs.Repo and never changes it.
package history

import (
	"github.com/dphaener/physa/internal/vcs"
)

// CapabilitiesDir is where the map keeps its capability files, relative to
// the repo's working directory.
const CapabilitiesDir = ".physa/capabilities"

// Commit is one change of a range with the capabilities its trailers name.
// Capabilities holds the slugs of its Capability trailers in the order
// written; Sessions holds the ids of its Session trailers, without
// duplicates, in the order written.
type Commit struct {
	Hash         string   `json:"hash"`
	Short        string   `json:"short"`
	Subject      string   `json:"subject"`
	Capabilities []string `json:"capabilities"`
	Sessions     []string `json:"sessions"`
}

// Read returns the non-merge changes of the range rng, newest first, each
// with the capabilities its trailers name. The adapter's own message is the
// error when the range is not valid.
func Read(repo vcs.Repo, rng string) ([]Commit, error) {
	changes, err := repo.Changes(rng)
	if err != nil {
		return nil, err
	}
	commits := make([]Commit, 0, len(changes))
	for _, ch := range changes {
		commits = append(commits, Commit{
			Hash: ch.ID, Short: ch.Short, Subject: ch.Subject,
			Capabilities: readTrailers(ch.Message), Sessions: Sessions(ch.Message),
		})
	}
	return commits, nil
}
