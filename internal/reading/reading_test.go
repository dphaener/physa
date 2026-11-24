package reading

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/store"
)

var day0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func day(n int) time.Time { return day0.AddDate(0, 0, n) }

func lines(n int, prefix string) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "%s line %d with some words to make it longer.\n", prefix, i)
	}
	return b.String()
}

func TestOfReadsInOrder(t *testing.T) {
	g := store.Gather{
		Docs: []store.Doc{
			{Path: "README.md", Kind: "readme", Body: "# Thing\n\nIt does things.\n"},
			{Path: "AGENTS.md", Kind: "agent", Body: "# Agents\n\nBe kind.\n"},
		},
		PRs: []store.GatheredPR{
			{Number: 1, Title: "Start", Body: "First.", MergedAt: day(1)},
			{Number: 2, Title: "Grow", Body: "Second.", MergedAt: day(2), Issues: []string{"Make it grow"},
				Comments: []store.GatheredComment{{Author: "ann", Body: "Nice."}}},
		},
		Changes: []store.GatheredChange{
			{ID: "aaa1", Short: "aaa1", Subject: "Start", Message: "Start\n\nWhy.", At: day(1), Files: []string{"a.go"}},
			{ID: "bbb2", Short: "bbb2", Subject: "Grow", At: day(2), Files: []string{"a.go", "b.go"}},
		},
	}
	in := Of(g, Propose)
	if len(in.Docs) != 2 || in.Docs[0].Path != "AGENTS.md" || in.Docs[1].Body != g.Docs[0].Body {
		t.Fatalf("docs = %+v", in.Docs)
	}
	if len(in.PRs) != 2 || in.PRs[0].Number != 2 || in.PRs[0].Body != "Second." || len(in.PRs[0].Comments) != 1 ||
		!reflect.DeepEqual(in.PRs[0].Issues, []string{"Make it grow"}) {
		t.Fatalf("prs = %+v", in.PRs)
	}
	if !reflect.DeepEqual(in.Commits, []Commit{{Short: "bbb2", Subject: "Grow"}, {Short: "aaa1", Subject: "Start"}}) {
		t.Fatalf("propose reads subjects only: commits = %+v", in.Commits)
	}
	want := []string{"docs: 2, whole, about 32 tokens", "pull requests: 2, whole, about 15 tokens",
		"review comments: 1, whole, about 4 tokens", "changes: 2, whole, about 8 tokens", "clusters: none"}
	if got := in.Report.Lines(); !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %q", got)
	}
	if in.Report.Tokens != 32+15+4+8 {
		t.Fatalf("report = %+v", in.Report)
	}

	in = Of(g, Options{Messages: true})
	if in.Commits[1].Message != "Why." || !in.Commits[1].At.Equal(day(1)) || len(in.Report.Sources) != 4 || in.Clusters != nil {
		t.Fatalf("with messages, without clusters: commits %+v, sources %+v", in.Commits, in.Report.Sources)
	}
}

// TestNothingIsCut checks that a long doc, a long PR body, a long review
// comment and a long change message are each read whole, however large.
func TestNothingIsCut(t *testing.T) {
	long := lines(20_000, "long") // about 1 MB
	g := store.Gather{
		Docs: []store.Doc{{Path: "docs/design.md", Kind: "overview", Body: "# Design\n\n## One\n" + long + "## Two\n" + long}},
		PRs: []store.GatheredPR{{Number: 7, Title: "Big", Body: long, MergedAt: day(1),
			Comments: []store.GatheredComment{{Author: "rev", RequestsChanges: true, Body: long}}}},
		Changes: []store.GatheredChange{{ID: "c1", Short: "c1", Subject: "Big", Message: "Big\n\n" + long, At: day(1)}},
	}
	in := Of(g, Options{Messages: true})
	if in.Docs[0].Body != g.Docs[0].Body || in.PRs[0].Body != long || in.PRs[0].Comments[0].Body != long || in.Commits[0].Message != long {
		t.Fatal("a source was cut")
	}
	if in.Report.Tokens < 4*Tokens(long) {
		t.Fatalf("estimated %d tokens for four texts of %d each and more", in.Report.Tokens, Tokens(long))
	}
}

func TestClusters(t *testing.T) {
	var changes []store.GatheredChange
	add := func(n int, files ...string) {
		for i := 0; i < n; i++ {
			id := fmt.Sprintf("c%03d", len(changes))
			changes = append(changes, store.GatheredChange{ID: id, Short: id, Subject: "s", At: day(len(changes)), Files: files})
		}
	}
	add(3, "b.go", "a.go")
	add(3, "b.go", "c.go")
	add(2, "d.go", "e.go")
	add(4, "g.go", "f.go", "f.go")
	big := []string{"x.go", "y.go"}
	for i := 0; i < ClusterMaxFiles; i++ {
		big = append(big, fmt.Sprintf("big%02d.go", i))
	}
	add(3, big...)

	got := clusters(changes)
	want := []Cluster{{Paths: []string{"a.go", "b.go", "c.go"}, Changes: 6}, {Paths: []string{"f.go", "g.go"}, Changes: 4}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("clusters = %+v", got)
	}

	var wide []string
	for i := 0; i < ClusterPaths+3; i++ {
		wide = append(wide, fmt.Sprintf("w%02d.go", i))
	}
	changes = nil
	add(ClusterMinChanges, wide[:ClusterMaxFiles/2]...)
	add(ClusterMinChanges, wide...)
	got = clusters(changes)
	if len(got) != 1 || len(got[0].Paths) != ClusterPaths || got[0].Paths[0] != "w00.go" || got[0].Changes != 2*ClusterMinChanges {
		t.Fatalf("wide cluster = %+v", got)
	}
}

func TestBytesPerTokenRoundsUp(t *testing.T) {
	for n, want := range map[int]int{0: 0, 1: 1, 23: 10, 24: 11, 230: 100} {
		if got := BytesTokens(n); got != want {
			t.Errorf("BytesTokens(%d) = %d, want %d", n, got, want)
		}
	}
	if Figure() != "2.3" {
		t.Fatalf("figure = %q", Figure())
	}
	for n, want := range map[int64]string{0: "0", 999: "999", 1000: "1,000", 14300: "14,300", 1234567: "1,234,567", -1000: "-1,000"} {
		if got := Thousands(n); got != want {
			t.Errorf("Thousands(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestDeterministic(t *testing.T) {
	g := synthetic()
	a := Of(g, Propose)
	for i := 0; i < 3; i++ {
		if b := Of(g, Propose); !reflect.DeepEqual(a, b) {
			t.Fatal("two readings of one gather differ")
		}
	}
}

func TestLargeGatherReadsQuickly(t *testing.T) {
	g := synthetic()
	start := time.Now()
	in := Of(g, Propose)
	took := time.Since(start)
	if took >= time.Second {
		t.Fatalf("Of took %v", took)
	}
	if in.Report.Tokens < 400_000 || len(in.Docs) != 3 || len(in.PRs) != 500 || len(in.Commits) != 1_500 || len(in.Clusters) == 0 {
		t.Fatalf("docs %d, prs %d, changes %d, clusters %d, report %+v", len(in.Docs), len(in.PRs), len(in.Commits), len(in.Clusters), in.Report)
	}
	t.Logf("read %d tokens whole in %v", in.Report.Tokens, took)
}

// synthetic builds a gather of about 425k tokens.
func synthetic() store.Gather {
	var g store.Gather
	for d, kind := range []string{"agent", "readme", "decisions"} {
		var b strings.Builder
		for s := 0; b.Len() < 200_000; s++ {
			fmt.Fprintf(&b, "## Section %d of doc %d\n", s, d)
			b.WriteString(lines(30, "doc"))
			if s%7 == 0 {
				b.WriteString("```\n# a comment in code\n```\n")
			}
		}
		g.Docs = append(g.Docs, store.Doc{Path: fmt.Sprintf("doc%d.md", d), Kind: kind, Body: b.String()})
	}
	for i := 0; i < 500; i++ {
		p := store.GatheredPR{Number: i + 1, Title: fmt.Sprintf("Change number %d to the product", i), MergedAt: day(i),
			Body: lines(18, "pr body"), Issues: []string{fmt.Sprintf("Issue %d", i)}}
		for c := 0; c < 3; c++ {
			p.Comments = append(p.Comments, store.GatheredComment{Author: "rev", RequestsChanges: c == 0 && i%3 == 0, Body: lines(4, "comment")})
		}
		g.PRs = append(g.PRs, p)
	}
	for i := 0; i < 1_500; i++ {
		id := fmt.Sprintf("%040x", i)
		files := []string{fmt.Sprintf("pkg%d/a.go", i%40), fmt.Sprintf("pkg%d/b.go", i%40), fmt.Sprintf("pkg%d/c.go", (i+1)%40)}
		if i%50 == 0 {
			for f := 0; f < 30; f++ {
				files = append(files, fmt.Sprintf("wide/%d.go", f))
			}
		}
		subject := fmt.Sprintf("Change %d of history", i)
		g.Changes = append(g.Changes, store.GatheredChange{ID: id, Short: id[:7], Subject: subject, Message: subject + "\n\n" + lines(4, "message"),
			At: day(i), Files: files})
	}
	return g
}

// TestRecounted checks that a part of an Input reports only what it reads,
// under the whole's source names, and that the whole recounts as Of counts.
func TestRecounted(t *testing.T) {
	g := store.Gather{
		Docs: []store.Doc{{Path: "README.md", Kind: "readme", Body: "# Thing\n"}},
		PRs: []store.GatheredPR{
			{Number: 1, Title: "Start", Body: "First.", MergedAt: day(1)},
			{Number: 2, Title: "Grow", Body: "Second.", MergedAt: day(2), Comments: []store.GatheredComment{{Author: "ann", Body: "Nice."}}},
		},
		Changes: []store.GatheredChange{{ID: "aaa1", Short: "aaa1", Subject: "Start", At: day(1)}},
	}
	in := Of(g, Propose)
	if got := in.Recounted(); !reflect.DeepEqual(got, in) {
		t.Fatalf("recounted whole = %+v, want %+v", got.Report, in.Report)
	}
	part := Input{PRs: in.PRs[:1], Report: in.Report}.Recounted() // #2, the newest
	want := []string{"docs: none", "pull requests: 1, whole, about 5 tokens", "review comments: 1, whole, about 4 tokens", "changes: none", "clusters: none"}
	if got := part.Report.Lines(); !reflect.DeepEqual(got, want) || part.Report.Tokens != 9 {
		t.Fatalf("part reports %q, %d tokens", got, part.Report.Tokens)
	}
	if bare := (Input{Commits: in.Commits}).Recounted(); len(bare.Report.Sources) != 4 || bare.Report.Sources[3].Items != 1 {
		t.Fatalf("an Input with no report names = %+v", bare.Report)
	}
}
