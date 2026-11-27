package sourceset

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/dphaener/physa/internal/vcs"
)

func write(t *testing.T, root, rel string, data []byte) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestList(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.go", []byte("one\ntwo\nthree\n"))
	early := bytes.Repeat([]byte("x"), 200)
	early[100] = 0
	write(t, root, "early.bin", early)
	late := append(bytes.Repeat([]byte("y\n"), 4100), 0)
	write(t, root, "late.txt", late)
	write(t, root, "web/yarn.lock", []byte("lock\n"))
	write(t, root, "go.sum", []byte("sum\n"))
	write(t, root, "vendor/v.go", []byte("v\n"))
	write(t, root, "gen/g.go", []byte("g\n"))
	if err := os.Symlink("a.go", filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}
	tracked := []vcs.TrackedFile{
		{Path: "a.go"}, {Path: "early.bin"}, {Path: "late.txt"},
		{Path: "web/yarn.lock"}, {Path: "go.sum"}, {Path: "link.go"},
		{Path: "vendor/v.go", Vendored: true}, {Path: "gen/g.go", Generated: true},
		{Path: "gone.go"},
	}
	got, err := List(root, tracked)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := []File{{Path: "a.go", Lines: 3}, {Path: "late.txt", Lines: 4100}}
	if !reflect.DeepEqual(got.Files, wantFiles) {
		t.Fatalf("Files = %v, want %v", got.Files, wantFiles)
	}
	wantLeft := map[string][]string{
		LeftBinary:    {"early.bin"},
		LeftLock:      {"web/yarn.lock", "go.sum"},
		LeftNotAFile:  {"link.go", "gone.go"},
		LeftVendored:  {"vendor/v.go"},
		LeftGenerated: {"gen/g.go"},
	}
	if !reflect.DeepEqual(got.Left, wantLeft) {
		t.Fatalf("Left = %v, want %v", got.Left, wantLeft)
	}
}

func TestListEmpty(t *testing.T) {
	got, err := List(t.TempDir(), nil)
	if err != nil || got.Left == nil || len(got.Files) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestClaims(t *testing.T) {
	files := []string{"web/a.go", "web/api/b.go", "lib/c.go", "lib/d.go", "orphan.go"}
	claims := []Claim{
		{Owner: "zeta", Patterns: []string{"web/", "lib/"}},
		{Owner: "alpha", Patterns: []string{"web/api/b.go", "lib/"}},
	}
	where, missing, twice := Claims(files, claims)
	wantWhere := map[string]string{"web/a.go": "zeta", "web/api/b.go": "alpha"}
	if !reflect.DeepEqual(where, wantWhere) {
		t.Fatalf("where = %v", where)
	}
	if !reflect.DeepEqual(missing, []string{"orphan.go"}) {
		t.Fatalf("missing = %v", missing)
	}
	wantTwice := map[string][]string{"lib/c.go": {"alpha", "zeta"}, "lib/d.go": {"alpha", "zeta"}}
	if !reflect.DeepEqual(twice, wantTwice) {
		t.Fatalf("twice = %v", twice)
	}
	if _, ok := where["lib/c.go"]; ok {
		t.Fatal("a twice-claimed file must be in no owner")
	}
}

func TestClaimsSameOwnerTwiceIsNotTwice(t *testing.T) {
	where, _, twice := Claims([]string{"a/b.go"}, []Claim{{Owner: "x", Patterns: []string{"a/", "*.go"}}})
	if where["a/b.go"] != "x" || len(twice) != 0 {
		t.Fatalf("%v %v", where, twice)
	}
}
