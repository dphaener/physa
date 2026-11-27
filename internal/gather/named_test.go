package gather

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/store"
)

// watch returns an opener that records the files named() opens, and the list.
func watch() (func(string) (*os.File, error), *[]string) {
	var opened []string
	return func(name string) (*os.File, error) {
		opened = append(opened, filepath.Base(name))
		return os.Open(name)
	}, &opened
}

func TestNamedRefusals(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	write(t, outside, "secret.md", "secret")
	write(t, root, ".git/config.md", "x")
	write(t, root, "node_modules/a.md", "x")
	write(t, root, "unlisted.md", "x")
	write(t, root, "dir/real.md", "x")
	if err := os.Symlink(filepath.Join(root, "dir", "real.md"), filepath.Join(root, "link.md")); err != nil {
		t.Fatal(err)
	}
	write(t, root, "bin.md", "ab\x00cd")
	write(t, root, "latin.md", "caf\xe9")
	write(t, root, "big.md", strings.Repeat("a", MaxDocBytes+1))
	write(t, root, "kind.md", "x")
	write(t, root, "have.md", "x")
	write(t, root, "twice.md", "x")

	listed := map[string]bool{
		"dir": true, "dir/real.md": true, "link.md": true, "bin.md": true, "latin.md": true, "big.md": true,
		"kind.md": true, "have.md": true, "twice.md": true, ".git/config.md": true, "node_modules/a.md": true,
	}
	have := []store.Doc{{Path: "have.md", Kind: KindReadme, Reason: "the README"}}
	for _, tc := range []struct {
		name string
		src  Source
		why  string
		read bool
	}{
		{"outside absolute", Source{Path: filepath.Join(outside, "secret.md"), Kind: KindAgent}, "outside the repo", false},
		{"outside dotdot", Source{Path: "../" + filepath.Base(outside) + "/secret.md", Kind: KindAgent}, "outside the repo", false},
		{"git", Source{Path: ".git/config.md", Kind: KindAgent}, "under .git", false},
		{"ignored segment", Source{Path: "node_modules/a.md", Kind: KindAgent}, "under an ignored folder", false},
		{"not listed", Source{Path: "unlisted.md", Kind: KindAgent}, "under an ignored folder", false},
		{"missing", Source{Path: "nope.md", Kind: KindAgent}, "missing", false},
		{"not regular dir", Source{Path: "dir", Kind: KindAgent}, "not a regular file", false},
		{"not regular symlink", Source{Path: "link.md", Kind: KindAgent}, "not a regular file", false},
		{"not text nul", Source{Path: "bin.md", Kind: KindAgent}, "not text", true},
		{"not text utf8", Source{Path: "latin.md", Kind: KindAgent}, "not text", true},
		{"over size", Source{Path: "big.md", Kind: KindAgent}, "over 2 MB", false},
		{"unknown kind", Source{Path: "kind.md", Kind: "notes"}, "unknown kind", false},
		{"duplicate of have", Source{Path: "have.md", Kind: KindAgent}, "a duplicate", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			open, opened := watch()
			docs, skipped := named(open, root, listed, []Source{tc.src}, have)
			if len(docs) != 0 {
				t.Fatalf("read %v", docs)
			}
			if len(skipped) != 1 || skipped[0].Why != tc.why {
				t.Fatalf("skipped %v, want why %q", skipped, tc.why)
			}
			if tc.read != (len(*opened) > 0) {
				t.Errorf("opened %v, want opened=%v", *opened, tc.read)
			}
		})
	}

	t.Run("duplicate named twice", func(t *testing.T) {
		open, opened := watch()
		docs, skipped := named(open, root, listed, []Source{
			{"twice.md", KindReadme, "first"}, {"./twice.md", KindAgent, "second"},
		}, nil)
		if len(docs) != 1 || docs[0].Kind != KindReadme || docs[0].Reason != "first" || docs[0].Body != "x" {
			t.Errorf("docs %v", docs)
		}
		if !reflect.DeepEqual(skipped, []Skip{{"twice.md", "a duplicate"}}) || len(*opened) != 1 {
			t.Errorf("%v opened %v", skipped, *opened)
		}
	})

	t.Run("over the limit", func(t *testing.T) {
		many := t.TempDir()
		var srcs []Source
		for i := 0; i < MaxNamed+2; i++ {
			rel := "f" + strings.Repeat("a", i) + ".md"
			write(t, many, rel, "x")
			srcs = append(srcs, Source{rel, KindOverview, "r"})
		}
		open, opened := watch()
		docs, skipped := named(open, many, nil, srcs, nil)
		if len(docs) != MaxNamed || len(skipped) != 2 || len(*opened) != MaxNamed {
			t.Fatalf("%d docs, %d skipped, %d opened", len(docs), len(skipped), len(*opened))
		}
		for _, s := range skipped {
			if s.Why != "over the limit of 200 named sources" {
				t.Errorf("%v", s)
			}
		}
	})
}

func TestNamedAccepts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, root, "docs/a.md", "héllo")
	write(t, root, "b.md", "second")
	docs, skipped := Named(root, map[string]bool{"docs/a.md": true, "b.md": true},
		[]Source{{"docs//a.md", KindOverview, "the plan"}, {"b.md", KindDecisions, ""}}, nil)
	want := []store.Doc{
		{Path: "docs/a.md", Kind: KindOverview, Reason: "the plan", Body: "héllo"},
		{Path: "b.md", Kind: KindDecisions, Body: "second"},
	}
	if len(skipped) != 0 || !reflect.DeepEqual(docs, want) {
		t.Errorf("%v %v", docs, skipped)
	}
}

func TestNamedTextProbeKeepsRuneCutAtItsEnd(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, root, "a.md", strings.Repeat("a", textProbe-1)+"é"+"tail")
	docs, skipped := Named(root, nil, []Source{{"a.md", KindAgent, ""}}, nil)
	if len(docs) != 1 || len(skipped) != 0 {
		t.Errorf("%v %v", docs, skipped)
	}
}

func TestNamedOutsideBySymlinkedFolder(t *testing.T) {
	t.Parallel()
	root, outside := t.TempDir(), t.TempDir()
	write(t, outside, "x.md", "secret")
	if err := os.Symlink(outside, filepath.Join(root, "out")); err != nil {
		t.Fatal(err)
	}
	open, opened := watch()
	docs, skipped := named(open, root, nil, []Source{{"out/x.md", KindAgent, ""}}, nil)
	if len(docs) != 0 || len(skipped) != 1 || skipped[0].Why != "outside the repo" || len(*opened) != 0 {
		t.Errorf("%v %v %v", docs, skipped, *opened)
	}
}
