// Package mapped says whether a repo already holds a Physa map, on its
// default branch or, with no default branch to be found, in its working
// tree, so that init refuses to start a second one and --gather alone
// starts no discovery agent on a mapped repo. It only reads.
package mapped

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dphaener/physa/internal/initbranch"
	"github.com/dphaener/physa/internal/mapfolder"
	"github.com/dphaener/physa/internal/vcs"
)

// Refuse is the early refusal: it errors, before anything is spent,
// when the default branch already holds a map in either layout
// (mapfolder.Products), naming the file found. With no default branch
// to be found it checks the working tree instead, and leaves a missing
// default branch for --write to report.
func Refuse(open func(dir string) (vcs.Writer, error)) error {
	w, err := open(".")
	if err != nil {
		return err
	}
	if _, err := w.DefaultBranch(); err == nil {
		return initbranch.HasMap(w)
	}
	for _, p := range mapfolder.Products {
		if _, err := os.Stat(filepath.FromSlash(p)); err == nil {
			return fmt.Errorf("the repo already has a map (%s in the working tree); init only starts a map, and learning keeps an existing one current: run physa map learn", p)
		}
	}
	return nil
}

// DiscoveryOff is why --gather alone starts no discovery agent, or empty to
// start one: the repo has a map, by the early refusal's check, or whether it
// has one could not be told.
func DiscoveryOff(open func(dir string) (vcs.Writer, error)) string {
	switch err := Refuse(open); {
	case err == nil:
		return ""
	case strings.Contains(err.Error(), "already has a map"):
		return "discovery not started: the repo has a map"
	default:
		return "discovery not started: could not tell whether the repo has a map: " + err.Error()
	}
}
