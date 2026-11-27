package shares

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/reading"
	"github.com/dphaener/physa/internal/store"
)

var day0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// text is about n bytes of words, distinct per prefix.
func text(n int, prefix string) string {
	var b strings.Builder
	for i := 0; b.Len() < n; i++ {
		fmt.Fprintf(&b, "%s line %d with words that make it longer.\n", prefix, i)
	}
	return b.String()
}

// head stands in for a session's skill: what its prompt says before the
// sources.
var head = text(9_000, "skill")

// prompt is a session's prompt for in, as these tests write it: head, then
// the sources, whole, under their own headings, each doc, PR and review
// comment in its own fence and the changes in one, as draft.Fence fences
// them, so a share's real prompt can be longer than its estimate.
func prompt(in reading.Input) string {
	var w strings.Builder
	w.WriteString(head + "\n## Docs\n\n")
	for i, d := range in.Docs {
		fmt.Fprintf(&w, "Doc %d of %d:\n\n", i+1, len(in.Docs))
		draft.Fence(&w, "path: "+d.Path+"\nkind: "+d.Kind+"\ntext:\n"+d.Body)
	}
	w.WriteString("\n## Pull requests\n\n")
	for _, p := range in.PRs {
		fmt.Fprintf(&w, "#%d, merged %s:\n\n", p.Number, p.MergedAt.UTC().Format(time.DateOnly))
		draft.Fence(&w, "title: "+p.Title+"\nbody:\n"+p.Body)
		for _, c := range p.Comments {
			fmt.Fprintf(&w, "\nReview comment on #%d:\n\n", p.Number)
			draft.Fence(&w, "author: "+c.Author+"\ntext:\n"+c.Body)
		}
	}
	w.WriteString("\n## Changes\n\n")
	if len(in.Commits) > 0 {
		var t strings.Builder
		for _, c := range in.Commits {
			t.WriteString(c.Short + " " + c.Subject + "\n")
		}
		draft.Fence(&w, t.String())
	}
	return w.String()
}

// gathered is a gather of docs docs of docBytes each, prs PRs of prBytes
// each with two review comments, and changes changes, read with change
// subjects only.
func gathered(docs, docBytes, prs, prBytes, changes int) reading.Input {
	var g store.Gather
	for i := 0; i < docs; i++ {
		g.Docs = append(g.Docs, store.Doc{Path: fmt.Sprintf("docs/d%03d.md", i), Kind: "overview", Body: text(docBytes, fmt.Sprintf("doc %d", i))})
	}
	for i := 0; i < prs; i++ {
		g.PRs = append(g.PRs, store.GatheredPR{Number: i + 1, Title: fmt.Sprintf("PR %d", i+1), MergedAt: day0.Add(time.Duration(i) * time.Hour),
			Body: text(prBytes, fmt.Sprintf("pr %d", i+1)), Comments: []store.GatheredComment{
				{Author: "ann", Body: fmt.Sprintf("First look at %d.", i+1)}, {Author: "bo", RequestsChanges: true, Body: fmt.Sprintf("Fix %d.", i+1)}}})
	}
	for i := 0; i < changes; i++ {
		g.Changes = append(g.Changes, store.GatheredChange{ID: fmt.Sprintf("c%07d", i), Short: fmt.Sprintf("c%07d", i),
			Subject: fmt.Sprintf("feat: Change number %d does a thing", i), At: day0.Add(time.Duration(i) * time.Minute),
			Files: []string{"a.go", "b.go"}})
	}
	return reading.Of(g, reading.Options{})
}

// check asserts that shares read every source of in exactly once, in
// order and whole, the docs in every share when shared, that each share's
// prompt is at most room and that each reports only what it reads.
func check(t *testing.T, in reading.Input, shares []reading.Input, room int, shared bool) {
	t.Helper()
	var all reading.Input
	for i, s := range shares {
		if got := reading.Tokens(prompt(s)); got > room {
			t.Errorf("share %d of %d: prompt about %d tokens, over room %d", i+1, len(shares), got, room)
		}
		if !reflect.DeepEqual(s.Report, s.Recounted().Report) || len(s.Report.Sources) != len(in.Report.Sources) {
			t.Errorf("share %d reports %+v", i+1, s.Report)
		}
		for j := range s.Report.Sources {
			if s.Report.Sources[j].Name != in.Report.Sources[j].Name {
				t.Errorf("share %d source %d is %q", i+1, j, s.Report.Sources[j].Name)
			}
		}
		if shared {
			if !reflect.DeepEqual(s.Docs, in.Docs) {
				t.Errorf("share %d does not read all the docs", i+1)
			}
		} else {
			all.Docs = append(all.Docs, s.Docs...)
		}
		all.PRs = append(all.PRs, s.PRs...)
		all.Commits = append(all.Commits, s.Commits...)
	}
	if !shared && !reflect.DeepEqual(all.Docs, in.Docs) {
		t.Errorf("docs: read %d, want each of %d once, in order, whole", len(all.Docs), len(in.Docs))
	}
	if !reflect.DeepEqual(all.PRs, in.PRs) || !reflect.DeepEqual(all.Commits, in.Commits) {
		t.Errorf("PRs or changes not each read once, in order, whole: %d/%d PRs, %d/%d changes",
			len(all.PRs), len(in.PRs), len(all.Commits), len(in.Commits))
	}
	for i := range all.PRs {
		if i < len(in.PRs) && (all.PRs[i].Body != in.PRs[i].Body || len(all.PRs[i].Comments) != len(in.PRs[i].Comments)) {
			t.Errorf("PR #%d not whole, or parted from its review comments", in.PRs[i].Number)
		}
	}
}

func TestLayFor(t *testing.T) {
	skill := reading.Tokens(prompt(reading.Input{}))
	for _, tc := range []struct {
		name   string
		in     reading.Input
		room   int
		shared bool
		shares int // 0: more than one
	}{
		{"one share when everything fits", gathered(3, 2_000, 10, 2_000, 50), 1_000_000, true, 1},
		{"docs in every share", gathered(2, 1_000, 40, 3_000, 200), skill + 8_000, true, 0},
		{"docs divided when they take more than half", gathered(6, 3_000, 20, 2_000, 100), skill + 8_000, false, 0},
		{"docs divided when a PR fits only an empty share", func() reading.Input {
			in := gathered(2, 3_000, 10, 1_000, 20)
			in.PRs[3].Body = text(16_000, "big") // about 7,000 tokens; the docs about 2,700
			return in.Recounted()
		}(), skill + 8_000, false, 0},
		{"only docs, divided", gathered(9, 4_000, 0, 0, 0), skill + 4_000, false, 0},
		{"nothing gathered", gathered(0, 0, 0, 0, 0), skill, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shares, err := LayFor(tc.in, tc.room, prompt)
			if err != nil {
				t.Fatal(err)
			}
			if tc.shares == 1 && (len(shares) != 1 || prompt(shares[0]) != prompt(tc.in) || !reflect.DeepEqual(shares[0].Report, tc.in.Report)) {
				t.Fatalf("want one share that is the whole input, got %d", len(shares))
			}
			if tc.shares == 0 && len(shares) < 2 {
				t.Fatalf("want more than one share, got %d", len(shares))
			}
			check(t, tc.in, shares, tc.room, tc.shared)
			again, _ := LayFor(tc.in, tc.room, prompt)
			if !reflect.DeepEqual(again, shares) {
				t.Fatal("the same input and room gave other shares")
			}
		})
	}
}

func TestLayForMovesSourcesWhenARealPromptIsOver(t *testing.T) {
	// Backticks in subjects lengthen the changes' fence beyond the estimate's
	// slack, so the real prompt is what decides.
	in := gathered(0, 0, 0, 0, 400)
	for i := range in.Commits {
		in.Commits[i].Subject += strings.Repeat("`", 30+i%40)
	}
	room := reading.Tokens(prompt(reading.Input{})) + 1_000
	shares, err := LayFor(in, room, prompt)
	if err != nil {
		t.Fatal(err)
	}
	check(t, in, shares, room, true)
}

func TestLayForTooLarge(t *testing.T) {
	skill := reading.Tokens(prompt(reading.Input{}))
	room := skill + 2_000
	pr := gathered(1, 500, 3, 500, 5)
	pr.PRs[1].Body = text(10_000, "huge")
	pr.PRs[1].MergedAt = time.Date(2026, 3, 2, 15, 0, 0, 0, time.UTC)
	doc := gathered(2, 500, 1, 500, 5)
	doc.Docs[1].Body = text(10_000, "huge")
	change := gathered(0, 0, 0, 0, 3)
	change.Commits[2].Subject = text(10_000, "long")
	for _, tc := range []struct {
		name string
		in   reading.Input
		want []string
	}{
		{"PR", pr, []string{"PR #" + fmt.Sprint(pr.PRs[1].Number) + " takes about ", "more than one share holds", "Nothing is cut.",
			"--since a date after it merged on 2026-03-02", "physa init --gather --since 2026-03-03", "a smaller --max-prs"}},
		{"doc", doc, []string{"doc " + doc.Docs[1].Path + " takes about ", "Nothing is cut", "ignore it in git", "physa init --gather."}},
		{"change", change, []string{"change " + change.Commits[2].Short + " takes about ", "--since a date after the change's"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shares, err := LayFor(tc.in, room, prompt)
			var tl *TooLarge
			if !errors.As(err, &tl) || shares != nil {
				t.Fatalf("LayFor = %d shares, %v; want a TooLarge and no shares", len(shares), err)
			}
			if tl.Tokens < 4_000 || tl.Room != room {
				t.Errorf("tokens %d, room %d", tl.Tokens, tl.Room)
			}
			msg := err.Error()
			t.Logf("message: %s", msg)
			for _, w := range append(tc.want, reading.Thousands(int64(tl.Tokens)), reading.Thousands(int64(room))) {
				if !strings.Contains(msg, w) {
					t.Errorf("message lacks %q:\n%s", w, msg)
				}
			}
		})
	}
}

func TestLayForIsQuickOnALargeGather(t *testing.T) {
	in := gathered(5, 2_000, 1_000, 1_500, 5_000)
	room := 50_000
	start := time.Now()
	shares, err := LayFor(in, room, prompt)
	took := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if took > 2*time.Second {
		t.Fatalf("laying 1,000 PRs and 5,000 changes took %s", took)
	}
	t.Logf("%d shares in %s", len(shares), took)
	check(t, in, shares, room, true)
}
