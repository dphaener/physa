package newcodebase

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/store"
)

func changes(n int) []store.GatheredChange {
	return make([]store.GatheredChange, n)
}

func TestNew(t *testing.T) {
	readme := store.Doc{Path: "README.md", Kind: "readme", Body: "# X\n"}
	for _, tc := range []struct {
		name string
		g    store.Gather
		want bool
	}{
		{"readme only, 3 changes", store.Gather{Docs: []store.Doc{readme}, Changes: changes(3)}, true},
		{"empty", store.Gather{}, true},
		{"agent doc", store.Gather{Docs: []store.Doc{{Path: "AGENTS.md", Kind: "agent"}}, Changes: changes(1)}, false},
		{"contributing", store.Gather{Docs: []store.Doc{{Kind: "contributing"}}}, false},
		{"decisions", store.Gather{Docs: []store.Doc{{Kind: "decisions"}}}, false},
		{"6 changes", store.Gather{Docs: []store.Doc{readme}, Changes: changes(6)}, false},
		{"5 changes", store.Gather{Changes: changes(FewChanges)}, true},
		{"a PR", store.Gather{PRs: []store.GatheredPR{{Number: 1}}}, false},
	} {
		if got := New(tc.g); got != tc.want {
			t.Errorf("%s: New = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestQuestions(t *testing.T) {
	g := store.Gather{Docs: []store.Doc{{Kind: "readme", Body: "# Lathe\n\n[![ci](x)](y)\n\nLathe turns sketches into cut lists. It is fast.\nMore.\n"}}}
	qs := Questions(g, "wood-shop")
	if len(qs) != 3 {
		t.Fatalf("got %d questions", len(qs))
	}
	for i, q := range qs {
		if q.Number != i+1 {
			t.Errorf("question %d numbered %d", i+1, q.Number)
		}
	}
	if qs[0].Suggested != "Wood Shop" {
		t.Errorf("name = %q", qs[0].Suggested)
	}
	if qs[1].Suggested != "Lathe turns sketches into cut lists." {
		t.Errorf("promise = %q", qs[1].Suggested)
	}
	if qs[2].Suggested != "none for now" {
		t.Errorf("terms = %q", qs[2].Suggested)
	}
	bare := Questions(store.Gather{Docs: []store.Doc{{Kind: "readme", Body: "# Only a title\n"}}}, "lathe")
	if bare[1].Suggested != "Lathe lets its users …" {
		t.Errorf("fallback promise = %q", bare[1].Suggested)
	}
}

func TestAskAcceptsSuggestions(t *testing.T) {
	qs := Questions(store.Gather{}, "lathe")
	var out strings.Builder
	a, err := Ask(strings.NewReader("\nyes\n\n"), &out, qs[:1])
	if err != nil || a.Name != "Lathe" {
		t.Fatalf("got %+v, %v", a, err)
	}
	if !strings.Contains(out.String(), "1. What is the product called?\n   Suggested: Lathe\n") {
		t.Errorf("output:\n%s", out.String())
	}
	qs[1].Suggested = "Lathe cuts."
	a, err = Ask(strings.NewReader("\nyes\n\n"), &out, qs)
	if err != nil || a.Name != "Lathe" || a.Promise != "Lathe cuts." || len(a.Terms) != 0 {
		t.Fatalf("got %+v, %v", a, err)
	}
}

func TestAskEdited(t *testing.T) {
	qs := Questions(store.Gather{}, "lathe")
	in := "Lathe Pro\nIt turns sketches into cut lists.\nCut list: The parts to cut; Part: One piece\n"
	a, err := Ask(strings.NewReader(in), &strings.Builder{}, qs)
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "Lathe Pro" || a.Promise != "It turns sketches into cut lists." || len(a.Terms) != 2 {
		t.Fatalf("got %+v", a)
	}
	want := "**Cut list**: The parts to cut\n\n**Part**: One piece"
	if got := a.GlossaryTerms(); got != want {
		t.Errorf("GlossaryTerms = %q", got)
	}
}

func TestAskPromiseWithEllipsisAskedAgain(t *testing.T) {
	qs := Questions(store.Gather{}, "lathe")[1:2]
	a, err := Ask(strings.NewReader("\nstill …\nLathe cuts wood.\n"), &strings.Builder{}, qs)
	if err != nil || a.Promise != "Lathe cuts wood." {
		t.Fatalf("got %+v, %v", a, err)
	}
	var out strings.Builder
	_, err = Ask(strings.NewReader("\n\n\nLathe cuts wood.\n"), &out, qs)
	if err == nil || !strings.Contains(err.Error(), "question 2") || !strings.Contains(err.Error(), "…") {
		t.Fatalf("err = %v", err)
	}
	if n := strings.Count(out.String(), "2. What does"); n != 3 {
		t.Errorf("asked %d times, want 3", n)
	}
}

func TestAskInputEnds(t *testing.T) {
	qs := Questions(store.Gather{}, "lathe")
	if _, err := Ask(strings.NewReader("Lathe\n"), &strings.Builder{}, qs); err == nil || !strings.Contains(err.Error(), "question 2") {
		t.Fatalf("err = %v", err)
	}
}

func write(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "answers.txt")
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadAnswers(t *testing.T) {
	p := write(t, "# answers\n\n1: Lathe\n2: Lathe turns sketches into cut lists.\n3: Cut list: The parts to cut, with their sizes\n")
	a, err := ReadAnswers(p)
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "Lathe" || a.Promise != "Lathe turns sketches into cut lists." ||
		len(a.Terms) != 1 || a.Terms[0] != (Term{"Cut list", "The parts to cut, with their sizes"}) {
		t.Fatalf("got %+v", a)
	}
}

func TestReadAnswersMissingTakeSuggestion(t *testing.T) {
	qs := Questions(store.Gather{}, "lathe")
	qs[1].Suggested = "Lathe cuts."
	a, err := ReadAnswers(write(t, "1: Lathe Pro\n"), qs...)
	if err != nil || a.Name != "Lathe Pro" || a.Promise != "Lathe cuts." || len(a.Terms) != 0 {
		t.Fatalf("got %+v, %v", a, err)
	}
	if _, err := ReadAnswers(write(t, "3: none\n")); err == nil || !strings.Contains(err.Error(), "question 1") {
		t.Fatalf("no suggestion for the name: %v", err)
	}
}

func TestReadAnswersBad(t *testing.T) {
	for _, tc := range []struct{ name, text, want string }{
		{"bad term", "1: L\n2: L cuts.\n3: Widget\n", `question 3: term "Widget" needs the form Name: meaning`},
		{"empty meaning", "1: L\n2: L cuts.\n3: Widget:\n", "question 3"},
		{"ellipsis promise", "1: L\n2: L lets its users …\n", "question 2: the promise still holds …"},
		{"unnumbered", "1: L\nL cuts.\n", "line 2"},
		{"number out of range", "4: x\n", "line 1"},
		{"twice", "1: L\n1: M\n", "answered twice"},
	} {
		_, err := ReadAnswers(write(t, tc.text))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
	if _, err := ReadAnswers(filepath.Join(t.TempDir(), "none")); err == nil {
		t.Error("missing file: want an error")
	}
}
