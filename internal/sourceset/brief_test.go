package sourceset

import "testing"

func TestBrief(t *testing.T) {
	files := []string{"a/1", "a/2", "b/1", "c/1", "d/1", "e/1", "f/1", "g/1"}
	want := "- `checkout`: People pay. (8 files; mostly in `a/`, `b/`, `c/`, `d/`, `e/`, `f/`)"
	if got := Brief("checkout", "People pay.\n", files); got != want {
		t.Errorf("got %q", got)
	}
}
