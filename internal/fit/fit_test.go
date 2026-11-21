package fit

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

func source(t *testing.T, r Report, name string) SourceReport {
	t.Helper()
	for _, s := range r.Sources {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no source %q in %+v", name, r.Sources)
	return SourceReport{}
}

func TestEverythingFits(t *testing.T) {
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
			{ID: "aaa1", Short: "aaa1", Subject: "Start", At: day(1), Files: []string{"a.go"}},
			{ID: "bbb2", Short: "bbb2", Subject: "Grow", At: day(2), Files: []string{"a.go", "b.go"}},
		},
	}
	in := Fit(g, 10_000)
	if len(in.Docs) != 2 || in.Docs[0].Path != "AGENTS.md" || in.Docs[1].Body != g.Docs[0].Body || in.Docs[0].Cut {
		t.Fatalf("docs = %+v", in.Docs)
	}
	if len(in.PRs) != 2 || in.PRs[0].Number != 2 || in.PRs[0].Body != "Second." || len(in.PRs[0].Comments) != 1 ||
		!reflect.DeepEqual(in.PRs[0].Issues, []string{"Make it grow"}) {
		t.Fatalf("prs = %+v", in.PRs)
	}
	if !reflect.DeepEqual(in.Commits, []Commit{{"bbb2", "Grow"}, {"aaa1", "Start"}}) {
		t.Fatalf("commits = %+v", in.Commits)
	}
	want := []string{"docs: kept whole", "pr titles: kept whole", "pr bodies: kept whole",
		"review comments: kept whole", "commits: kept whole", "clusters: none"}
	if got := in.Report.Lines(); !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %q", got)
	}
	if in.Report.Budget != 10_000 || in.Report.Used == 0 || in.Report.Used > 10_000 {
		t.Fatalf("report = %+v", in.Report)
	}
}

func TestDocCutBySection(t *testing.T) {
	doc := "Intro before any heading.\n" + lines(40, "intro") +
		"# Title\n" + lines(5, "short") +
		"## Long part\n" + lines(200, "long") +
		"```\n# not a heading, inside a fence\n```\n" +
		"## Empty part\n" +
		"### Another long part\n" + lines(150, "another")
	g := store.Gather{Docs: []store.Doc{{Path: "README.md", Kind: "readme", Body: doc}}}
	budget := 5_000
	in := Fit(g, budget)
	if len(in.Docs) != 1 || !in.Docs[0].Cut {
		t.Fatalf("docs = %+v", in.Docs)
	}
	got := in.Docs[0].Body
	for _, h := range []string{"# Title\n", "## Long part\n", "## Empty part\n", "### Another long part\n"} {
		if !strings.Contains(got, h) {
			t.Errorf("heading %q lost", h)
		}
	}
	if !strings.Contains(got, "short line 4 with") {
		t.Error("the short section was not kept whole")
	}
	if strings.Contains(got, "## Empty part\n[…") {
		t.Error("an empty section got a marker")
	}
	if !strings.Contains(got, "intro line 39 with") {
		t.Error("the intro, small enough, was not kept whole")
	}
	if n := strings.Count(got, "tokens cut]\n"); n != 2 {
		t.Errorf("markers = %d, want 2 (long, another):\n%s", n, got)
	}
	if !strings.Contains(got, "\nlong line 0 with") || !strings.Contains(got, "\nanother line 0 with") {
		t.Error("a cut section kept none of its text")
	}
	if !strings.Contains(got, "\n[… about ") {
		t.Error("marker not on its own line")
	}
	allow := budget * DocsShare / 100
	if Tokens(got) > allow {
		t.Errorf("doc is %d tokens, allowance %d", Tokens(got), allow)
	}
	if Tokens(got) < allow*3/4 {
		t.Errorf("doc is %d tokens, allowance %d: too much left unused", Tokens(got), allow)
	}
	s := source(t, in.Report, "docs")
	if s.Items != 1 || s.Kept != 1 || s.Partial != 1 || s.Tokens != Tokens(doc) || s.KeptTokens != Tokens(got) {
		t.Fatalf("docs report = %+v", s)
	}
	want := fmt.Sprintf("docs: 1 of 1 kept, 1 cut by section (%s of %s tokens left out)",
		thousands(s.Tokens-s.KeptTokens), thousands(s.Tokens))
	if l := in.Report.Lines()[0]; l != want {
		t.Fatalf("line = %q, want %q", l, want)
	}
}

func TestSectionsIgnoreFencedHeadings(t *testing.T) {
	secs := sections("# A\nx\n~~~ go\n# no\n```\n## still no\n~~~\n  ## B\n#nope\n####### nope\n")
	var heads []string
	for _, s := range secs {
		heads = append(heads, s.heading)
	}
	if !reflect.DeepEqual(heads, []string{"# A\n", "  ## B\n"}) {
		t.Fatalf("headings = %q", heads)
	}
}

func TestHeadingsTooBigLeftOut(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 2_000; i++ {
		fmt.Fprintf(&b, "## Heading number %d\nbody\n", i)
	}
	g := store.Gather{Docs: []store.Doc{
		{Path: "docs/decisions.md", Kind: "decisions", Body: b.String()},
		{Path: "AGENTS.md", Kind: "agent", Body: "# Agents\nBe kind.\n"},
	}}
	in := Fit(g, 5_000)
	if len(in.Docs) != 1 || in.Docs[0].Path != "AGENTS.md" {
		t.Fatalf("docs = %+v", in.Docs)
	}
	s := source(t, in.Report, "docs")
	if s.Items != 2 || s.Kept != 1 || s.Partial != 0 {
		t.Fatalf("docs report = %+v", s)
	}
	line := in.Report.Lines()[0]
	want := fmt.Sprintf("docs: 1 of 2 kept (%s of %s tokens left out)", thousands(Tokens(b.String())), thousands(s.Tokens))
	if line != want {
		t.Fatalf("line = %q, want %q", line, want)
	}
	t.Logf("reported: %s", line)
}

func TestPRLayersFillInOrder(t *testing.T) {
	prs := []store.GatheredPR{
		{Number: 1, Title: "Oldest", MergedAt: day(1), Body: "old body\n",
			Comments: []store.GatheredComment{{Author: "rev", RequestsChanges: true, Body: "Please change the name of this.\n"}}},
		{Number: 3, Title: "Newest", MergedAt: day(3), Body: lines(200, "big"),
			Comments: []store.GatheredComment{{Author: "fan", Body: "Looks fine to me, ship it today.\n"}}},
		{Number: 2, Title: "Middle", MergedAt: day(2), Body: "mid body\n"},
	}
	var headers int
	for _, p := range prs {
		headers += headerTokens(p)
	}

	kept, titles, bodies, _ := fitPRs(prs, headers)
	if len(kept) != 3 || kept[0].Number != 3 || kept[1].Number != 2 || kept[2].Number != 1 {
		t.Fatalf("kept = %+v", kept)
	}
	if titles.Kept != 3 || bodies.Kept != 0 || kept[0].Body != "" {
		t.Fatalf("headers only: titles %+v, bodies %+v", titles, bodies)
	}

	kept, _, _, _ = fitPRs(prs, headers-1)
	if len(kept) != 2 || kept[1].Number != 2 {
		t.Fatalf("one header short: kept = %+v", kept)
	}

	rc := commentTokens("rev", prs[0].Comments[0].Body)
	left := headers + PRBodyTokens + Tokens("mid body\n") + Tokens("old body\n") + rc
	kept, _, bodies, comments := fitPRs(prs, left)
	if bodies.Kept != 3 || bodies.Partial != 1 || Tokens(kept[0].Body) > PRBodyTokens ||
		!strings.HasPrefix(kept[0].Body, "big line 0 ") || !strings.HasSuffix(kept[0].Body, "tokens cut]\n") {
		t.Fatalf("bodies = %+v, newest body %q", bodies, kept[0].Body)
	}
	if comments.Kept != 1 || len(kept[0].Comments) != 0 || len(kept[2].Comments) != 1 || !kept[2].Comments[0].RequestsChanges {
		t.Fatalf("comments = %+v, kept = %+v", comments, kept)
	}

	kept, _, _, comments = fitPRs(prs, left+commentTokens("fan", prs[1].Comments[0].Body))
	if comments.Kept != 2 || len(kept[0].Comments) != 1 || kept[0].Comments[0].Author != "fan" {
		t.Fatalf("both comments: %+v", kept)
	}
}

func TestCommentCappedAndCut(t *testing.T) {
	prs := []store.GatheredPR{{Number: 1, Title: "T", MergedAt: day(1),
		Comments: []store.GatheredComment{{Author: "rev", Body: lines(100, "comment")}}}}
	kept, _, _, comments := fitPRs(prs, 10_000)
	c := kept[0].Comments[0]
	if comments.Partial != 1 || Tokens(c.Body) > CommentTokens || !strings.HasSuffix(c.Body, "tokens cut]\n") {
		t.Fatalf("comment = %+v (%d tokens)", comments, Tokens(c.Body))
	}
}

func TestUnusedDocsShareSpillsToPRs(t *testing.T) {
	var prs []store.GatheredPR
	for i := 0; i < 40; i++ {
		prs = append(prs, store.GatheredPR{Number: i + 1, Title: "A title", MergedAt: day(i), Body: lines(3, "body")})
	}
	budget := 1_000
	in := Fit(store.Gather{PRs: prs}, budget)
	b := source(t, in.Report, "pr bodies")
	tl := source(t, in.Report, "pr titles")
	if spent := b.KeptTokens + tl.KeptTokens; spent <= budget*PRsShare/100 {
		t.Fatalf("PRs spent %d, no more than their own share", spent)
	}
	if in.Report.Used > budget {
		t.Fatalf("used %d of %d", in.Report.Used, budget)
	}

	withDocs := Fit(store.Gather{PRs: prs, Docs: []store.Doc{{Path: "README.md", Kind: "readme", Body: lines(400, "doc")}}}, budget)
	if got := source(t, withDocs.Report, "pr bodies").Kept; got >= b.Kept {
		t.Fatalf("with docs using their share, PRs kept %d bodies, without %d", got, b.Kept)
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

func TestBudgetTooSmall(t *testing.T) {
	if b, err := Budget(1_000_000, 2_000); err != nil || b != InputBudget {
		t.Fatalf("large appetite: %d, %v", b, err)
	}
	if b, err := Budget(50_000, 2_000); err != nil || b != 32_000 {
		t.Fatalf("medium appetite: %d, %v", b, err)
	}
	if b, err := Budget(23_000, 2_000); err != nil || b != MinBudget {
		t.Fatalf("smallest appetite: %d, %v", b, err)
	}
	_, err := Budget(22_999, 2_000)
	if err == nil || !strings.Contains(err.Error(), "at least 23,000") || !strings.Contains(err.Error(), "too small") {
		t.Fatalf("err = %v", err)
	}
	t.Logf("error: %v", err)
}

func TestDeterministic(t *testing.T) {
	g := synthetic()
	a := Fit(g, 30_000)
	for i := 0; i < 3; i++ {
		if b := Fit(g, 30_000); !reflect.DeepEqual(a, b) {
			t.Fatal("two fits of one gather differ")
		}
	}
}

func TestLargeGatherFitsQuickly(t *testing.T) {
	g := synthetic()
	total := 0
	for _, d := range g.Docs {
		total += Tokens(d.Body)
	}
	for _, p := range g.PRs {
		total += Tokens(p.Title + p.Body)
		for _, c := range p.Comments {
			total += Tokens(c.Body)
		}
	}
	for _, c := range g.Changes {
		total += Tokens(c.Message + strings.Join(c.Files, "\n"))
	}
	if total < 400_000 {
		t.Fatalf("synthetic gather is only %d tokens", total)
	}
	start := time.Now()
	in := Fit(g, InputBudget)
	took := time.Since(start)
	if in.Report.Used > InputBudget {
		t.Fatalf("used %d of %d", in.Report.Used, InputBudget)
	}
	if took >= time.Second {
		t.Fatalf("Fit took %v", took)
	}
	sum := 0
	for _, s := range in.Report.Sources {
		sum += s.KeptTokens
	}
	if sum != in.Report.Used || len(in.Docs) != 3 || len(in.PRs) != 500 || len(in.Clusters) == 0 {
		t.Fatalf("docs %d, prs %d, clusters %d, report %+v", len(in.Docs), len(in.PRs), len(in.Clusters), in.Report)
	}
	t.Logf("gather of %d tokens fitted to %d of %d in %v", total, in.Report.Used, InputBudget, took)
	for _, l := range in.Report.Lines() {
		t.Log(l)
	}
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
