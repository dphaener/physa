package mapped

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/vcs"
)

// noDefault is a writer whose repo has no default branch to be found.
type noDefault struct{ vcs.Writer }

func (noDefault) DefaultBranch(context.Context) (string, error) {
	return "", errors.New("the default branch cannot be found")
}

// opener opens noDefault, recording each directory it was asked for.
func opener(asked *[]string) func(context.Context, string) (vcs.Writer, error) {
	return func(_ context.Context, dir string) (vcs.Writer, error) {
		*asked = append(*asked, dir)
		return noDefault{}, nil
	}
}

func TestRefuseChecksTheWorkingTreeAtDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, ".physa", "product", "index.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("# P\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var asked []string
	err := Refuse(t.Context(), dir, opener(&asked))
	if err == nil || !strings.Contains(err.Error(), "(.physa/product/index.md in the working tree)") {
		t.Errorf("err %v", err)
	}
	if len(asked) != 1 || asked[0] != dir {
		t.Errorf("opened %v, want [%s]", asked, dir)
	}
	if off := DiscoveryOff(t.Context(), dir, opener(&asked)); off != "discovery not started: the repo has a map" {
		t.Errorf("off %q", off)
	}
}

func TestRefuseLetsAnUnmappedWorkingTreeStart(t *testing.T) {
	t.Parallel()
	var asked []string
	if err := Refuse(t.Context(), t.TempDir(), opener(&asked)); err != nil {
		t.Errorf("err %v", err)
	}
}
