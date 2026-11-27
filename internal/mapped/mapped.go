// Package mapped says whether a repo already holds a Physa map, on its
// default branch or, with no default branch to be found, in its working
// tree, so that init refuses to start a second one and --gather alone
// starts no discovery agent on a mapped repo. It only reads.
package mapped

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dphaener/physa/internal/initbranch"
	"github.com/dphaener/physa/internal/mapfolder"
	"github.com/dphaener/physa/internal/vcs"
)

// Refuse is the early refusal on the repo at dir: it errors, before
// anything is spent, when the default branch already holds a map in either
// layout (mapfolder.Products), naming the file found. With no default
// branch to be found it checks the working tree at dir instead, and leaves
// a missing default branch for --write to report.
func Refuse(ctx context.Context, dir string, open func(ctx context.Context, dir string) (vcs.Writer, error)) error {
	w, err := open(ctx, dir)
	if err != nil {
		return err
	}
	if _, err := w.DefaultBranch(ctx); err == nil {
		return initbranch.HasMap(ctx, w)
	}
	for _, p := range mapfolder.Products {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err == nil {
			return fmt.Errorf("the repo already has a map (%s in the working tree); init only starts a map, and learning keeps an existing one current: run physa map learn", p)
		}
	}
	return nil
}

// DiscoveryOff is why --gather alone starts no discovery agent on the repo
// at dir, or empty to start one: the repo has a map, by the early refusal's
// check, or whether it has one could not be told.
func DiscoveryOff(ctx context.Context, dir string, open func(ctx context.Context, dir string) (vcs.Writer, error)) string {
	switch err := Refuse(ctx, dir, open); {
	case err == nil:
		return ""
	case strings.Contains(err.Error(), "already has a map"):
		return "discovery not started: the repo has a map"
	default:
		return "discovery not started: could not tell whether the repo has a map: " + err.Error()
	}
}
