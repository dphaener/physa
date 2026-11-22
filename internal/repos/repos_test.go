package repos

import (
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func gitInit(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if out, err := exec.Command("git", "init", d).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return d
}

func TestListMissing(t *testing.T) {
	got, err := List(t.TempDir())
	if err != nil || len(got) != 0 {
		t.Fatalf("List = %v, %v; want empty, nil", got, err)
	}
}

func TestAddAndList(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	for _, p := range []string{b, a + "/./"} {
		added, err := Add(dir, p)
		if err != nil || !added {
			t.Fatalf("Add(%q) = %v, %v", p, added, err)
		}
	}
	got, err := List(dir)
	if err != nil || !reflect.DeepEqual(got, []string{b, a}) {
		t.Fatalf("List = %v, %v", got, err)
	}
}

func TestAddTwice(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "r")
	if added, err := Add(dir, root); err != nil || !added {
		t.Fatalf("first Add = %v, %v", added, err)
	}
	if added, err := Add(dir, root); err != nil || added {
		t.Fatalf("second Add = %v, %v; want false, nil", added, err)
	}
	got, _ := List(dir)
	if len(got) != 1 {
		t.Fatalf("List = %v; want one entry", got)
	}
}

func TestAddRelativeRefused(t *testing.T) {
	dir := t.TempDir()
	added, err := Add(dir, "some/repo")
	if err == nil || added || !strings.Contains(err.Error(), "some/repo") {
		t.Fatalf("Add = %v, %v; want an error naming the path", added, err)
	}
	if got, _ := List(dir); len(got) != 0 {
		t.Fatalf("List = %v; want empty", got)
	}
}

func TestStoreDirRepo(t *testing.T) {
	root := gitInit(t)
	got, err := StoreDir(root)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, ".git", "physa")
	g, err1 := filepath.EvalSymlinks(filepath.Dir(got))
	w, err2 := filepath.EvalSymlinks(filepath.Dir(want))
	if err1 != nil || err2 != nil {
		t.Fatalf("EvalSymlinks: %v, %v", err1, err2)
	}
	if filepath.Join(g, filepath.Base(got)) != filepath.Join(w, "physa") {
		t.Fatalf("StoreDir = %q; want %q", got, want)
	}
}

func TestStoreDirNotARepo(t *testing.T) {
	d := t.TempDir()
	_, err := StoreDir(d)
	if err == nil || !strings.Contains(err.Error(), d) {
		t.Fatalf("StoreDir = %v; want an error naming %s", err, d)
	}
}
