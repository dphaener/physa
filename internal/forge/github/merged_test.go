package github

import (
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/forge"
)

var _ forge.Merged = (*Client)(nil)

// pager answers each graphql call with the next canned page.
type pager struct {
	pages []string
	calls [][]string
}

func (p *pager) run(_ string, args ...string) ([]byte, error) {
	p.calls = append(p.calls, args)
	if args[0] == "repo" {
		return []byte(repoView), nil
	}
	i := len(p.calls) - 2
	if i >= len(p.pages) {
		return nil, fmt.Errorf("unexpected call %d", i)
	}
	return []byte(p.pages[i]), nil
}

func pageOf(next string, nodes ...string) string {
	info := `{"hasNextPage": false, "endCursor": ""}`
	if next != "" {
		info = `{"hasNextPage": true, "endCursor": "` + next + `"}`
	}
	return `{"data":{"repository":{"pullRequests":{"pageInfo":` + info + `,"nodes":[` + strings.Join(nodes, ",") + `]}}}}`
}

func node(n int, merged, updated, extra string) string {
	return fmt.Sprintf(`{"number":%d,"title":"PR %d","body":"body %d","mergedAt":%q,"updatedAt":%q,"author":{"login":"agent"},
	  "comments":{"nodes":[]},"reviews":{"nodes":[]},"closingIssuesReferences":{"nodes":[]}%s}`, n, n, n, merged, updated, extra)
}

func numbers(prs []forge.PR) []int {
	var out []int
	for _, p := range prs {
		out = append(out, p.Number)
	}
	return out
}

func TestMergedPRsTwoPagesStopsAtSince(t *testing.T) {
	p := &pager{pages: []string{
		pageOf("c1", node(3, "2026-11-10T00:00:00Z", "2026-11-12T00:00:00Z", ""), node(2, "2026-11-08T00:00:00Z", "2026-11-09T00:00:00Z", "")),
		// The second page crosses since: PR 1 was updated before it, and PR 0
		// was merged before it but updated after.
		pageOf("c2", node(0, "2026-10-01T00:00:00Z", "2026-11-05T00:00:00Z", ""), node(1, "2026-11-06T00:00:00Z", "2026-11-06T00:00:00Z", ""), node(9, "2026-09-01T00:00:00Z", "2026-09-01T00:00:00Z", "")),
		pageOf(""),
	}}
	prs, more, err := NewWithRunner(".", p.run).MergedPRs(at("2026-11-01T00:00:00Z"), 100)
	if err != nil || more {
		t.Fatalf("got %v, %v", more, err)
	}
	if got := numbers(prs); !reflect.DeepEqual(got, []int{3, 2, 1}) {
		t.Errorf("numbers = %v", got)
	}
	if len(p.calls) != 3 { // repo view and two pages
		t.Fatalf("calls = %d, want 3", len(p.calls))
	}
	if strings.Join(p.calls[1], " ") == strings.Join(p.calls[2], " ") || !strings.Contains(strings.Join(p.calls[2], " "), "cursor=c1") {
		t.Errorf("second page did not use the cursor: %q", p.calls[2])
	}
	for _, a := range p.calls[1] {
		if strings.HasPrefix(a, "cursor=") {
			t.Errorf("first page sent a cursor: %q", p.calls[1])
		}
	}
	if prs[0].Title != "PR 3" || prs[0].Body != "body 3" || prs[0].Author != "agent" || !prs[0].MergedAt.Equal(at("2026-11-10T00:00:00Z")) {
		t.Errorf("pr = %+v", prs[0])
	}
}

func TestMergedPRsSortsByMergeTime(t *testing.T) {
	p := &pager{pages: []string{
		pageOf("", node(1, "2026-11-05T00:00:00Z", "2026-11-20T00:00:00Z", ""), node(2, "2026-11-09T00:00:00Z", "2026-11-10T00:00:00Z", "")),
	}}
	prs, more, err := NewWithRunner(".", p.run).MergedPRs(at("2026-11-01T00:00:00Z"), 10)
	if err != nil || more || !reflect.DeepEqual(numbers(prs), []int{2, 1}) {
		t.Errorf("got %v, %v, %v", numbers(prs), more, err)
	}
}

func TestMergedPRsMaxCutsPage(t *testing.T) {
	p := &pager{pages: []string{
		pageOf("c1", node(3, "2026-11-10T00:00:00Z", "2026-11-12T00:00:00Z", ""), node(2, "2026-11-08T00:00:00Z", "2026-11-09T00:00:00Z", ""), node(1, "2026-11-06T00:00:00Z", "2026-11-07T00:00:00Z", "")),
	}}
	prs, more, err := NewWithRunner(".", p.run).MergedPRs(at("2026-11-01T00:00:00Z"), 2)
	if err != nil || !more || !reflect.DeepEqual(numbers(prs), []int{3, 2}) {
		t.Errorf("got %v, %v, %v", numbers(prs), more, err)
	}
	if len(p.calls) != 2 {
		t.Errorf("asked for more pages: %d calls", len(p.calls))
	}
}

func TestMergedPRsMaxExactlyAtEnd(t *testing.T) {
	// The max is reached and the date also ended the list: not more.
	p := &pager{pages: []string{
		pageOf("c1", node(2, "2026-11-08T00:00:00Z", "2026-11-09T00:00:00Z", ""), node(1, "2026-10-01T00:00:00Z", "2026-10-02T00:00:00Z", "")),
	}}
	prs, more, err := NewWithRunner(".", p.run).MergedPRs(at("2026-11-01T00:00:00Z"), 1)
	if err != nil || more || !reflect.DeepEqual(numbers(prs), []int{2}) {
		t.Errorf("got %v, %v, %v", numbers(prs), more, err)
	}
	prs, more, err = NewWithRunner(".", (&pager{pages: []string{pageOf("")}}).run).MergedPRs(at("2026-11-01T00:00:00Z"), 0)
	if err != nil || more || len(prs) != 0 {
		t.Errorf("max 0: %v, %v, %v", prs, more, err)
	}
}

func TestMergedPRsReviewMarksBotsAndIssues(t *testing.T) {
	n := `{"number":5,"title":"T","body":"B","mergedAt":"2026-11-10T00:00:00Z","updatedAt":"2026-11-10T00:00:00Z","author":null,
	  "comments":{"nodes":[
	    {"author":{"login":"ci","__typename":"Bot"},"createdAt":"2026-11-09T10:00:00Z","body":"deployed"},
	    {"author":{"login":"ana","__typename":"User"},"createdAt":"2026-11-09T10:30:00Z","body":"why?"}]},
	  "reviews":{"nodes":[
	    {"author":{"login":"bo","__typename":"User"},"state":"CHANGES_REQUESTED","submittedAt":"2026-11-09T11:00:00Z","body":"Rename.",
	     "comments":{"nodes":[{"author":{"login":"bo","__typename":"User"},"createdAt":"2026-11-09T10:59:00Z","body":"here"}]}},
	    {"author":{"login":"cy","__typename":"User"},"state":"APPROVED","submittedAt":"2026-11-09T12:00:00Z","body":"Fine.",
	     "comments":{"nodes":[{"author":{"login":"cy","__typename":"User"},"createdAt":"2026-11-09T11:59:00Z","body":"nit"}]}},
	    {"author":{"login":"di","__typename":"User"},"state":"PENDING","submittedAt":null,"body":"draft","comments":{"nodes":[]}}]},
	  "closingIssuesReferences":{"nodes":[{"number":4,"title":"Bug","body":"It breaks."},{"number":6,"title":"Other","body":""}]}}`
	p := &pager{pages: []string{pageOf("", n)}}
	prs, _, err := NewWithRunner(".", p.run).MergedPRs(at("2026-11-01T00:00:00Z"), 5)
	if err != nil || len(prs) != 1 {
		t.Fatalf("got %v, %v", prs, err)
	}
	want := []forge.Comment{
		{Author: "ana", At: at("2026-11-09T10:30:00Z"), Kind: forge.KindDiscussion, Body: "why?"},
		{Author: "bo", At: at("2026-11-09T10:59:00Z"), Kind: forge.KindReview, Body: "here", RequestsChanges: true},
		{Author: "bo", At: at("2026-11-09T11:00:00Z"), Kind: forge.KindReview, Body: "Rename.", RequestsChanges: true},
		{Author: "cy", At: at("2026-11-09T11:59:00Z"), Kind: forge.KindReview, Body: "nit"},
		{Author: "cy", At: at("2026-11-09T12:00:00Z"), Kind: forge.KindReview, Body: "Fine."},
	}
	if !reflect.DeepEqual(prs[0].Comments, want) {
		t.Errorf("comments =\n%+v\nwant\n%+v", prs[0].Comments, want)
	}
	wantIssues := []forge.Issue{{Number: 4, Title: "Bug", Body: "It breaks."}, {Number: 6, Title: "Other"}}
	if !reflect.DeepEqual(prs[0].Issues, wantIssues) || prs[0].Author != "" {
		t.Errorf("issues = %+v, author %q", prs[0].Issues, prs[0].Author)
	}
}

func TestMergedPRsErrors(t *testing.T) {
	since := at("2026-11-01T00:00:00Z")
	s := &script{errs: map[string]error{"repo view": &exec.Error{Name: "gh", Err: exec.ErrNotFound}}}
	if _, _, err := NewWithRunner(".", s.run).MergedPRs(since, 5); !errors.Is(err, forge.ErrUnavailable) {
		t.Errorf("gh missing: %v", err)
	}
	s = &script{answers: map[string]string{"repo view": repoView},
		errs: map[string]error{"api graphql": &Error{ExitCode: 4, Stderr: "HTTP 401: Bad credentials"}}}
	if _, _, err := NewWithRunner(".", s.run).MergedPRs(since, 5); !errors.Is(err, forge.ErrUnauthenticated) {
		t.Errorf("401: %v", err)
	}
	s = &script{answers: map[string]string{"repo view": repoView,
		"api graphql": `{"data":null,"errors":[{"message":"Could not resolve to a Repository"}]}`}}
	if _, _, err := NewWithRunner(".", s.run).MergedPRs(since, 5); err == nil || !strings.Contains(err.Error(), "Could not resolve") {
		t.Errorf("graphql error: %v", err)
	}
	s.answers["api graphql"] = "not json"
	if _, _, err := NewWithRunner(".", s.run).MergedPRs(since, 5); err == nil {
		t.Errorf("bad json: no error")
	}
	s.answers["api graphql"] = `{"data":{"repository":null}}`
	if _, _, err := NewWithRunner(".", s.run).MergedPRs(since, 5); err == nil {
		t.Errorf("no repository: no error")
	}
}

func TestMergedPRsOpenedMergeCommitAndChecks(t *testing.T) {
	n := node(7, "2026-11-10T00:00:00Z", "2026-11-10T00:00:00Z", `,"createdAt":"2026-11-09T00:00:00Z","mergeCommit":{"oid":"m7"},
	  "commits":{"nodes":[
	    {"commit":{"oid":"c2","committedDate":"2026-11-09T02:00:00Z","statusCheckRollup":{"state":"SUCCESS"}}},
	    {"commit":{"oid":"c1","committedDate":"2026-11-09T01:00:00Z","statusCheckRollup":{"state":"FAILURE"}}},
	    {"commit":{"oid":"c3","committedDate":"2026-11-09T03:00:00Z","statusCheckRollup":null}}]}`)
	p := &pager{pages: []string{pageOf("", n)}}
	prs, _, err := NewWithRunner(".", p.run).MergedPRs(at("2026-11-01T00:00:00Z"), 5)
	if err != nil || len(prs) != 1 {
		t.Fatalf("got %v, %v", prs, err)
	}
	pr := prs[0]
	if !pr.CreatedAt.Equal(at("2026-11-09T00:00:00Z")) || pr.MergeCommit != "m7" {
		t.Errorf("created %v, merge commit %q", pr.CreatedAt, pr.MergeCommit)
	}
	want := []forge.Commit{
		{ID: "c1", At: at("2026-11-09T01:00:00Z"), Checks: "FAILURE"},
		{ID: "c2", At: at("2026-11-09T02:00:00Z"), Checks: "SUCCESS"},
		{ID: "c3", At: at("2026-11-09T03:00:00Z")},
	}
	if !reflect.DeepEqual(pr.Commits, want) {
		t.Errorf("commits = %+v, want %+v", pr.Commits, want)
	}
}

func TestMergedPRsNoRollupNoMergeCommit(t *testing.T) {
	n := node(8, "2026-11-10T00:00:00Z", "2026-11-10T00:00:00Z", `,"mergeCommit":null,
	  "commits":{"nodes":[{"commit":{"oid":"c1","committedDate":"2026-11-09T01:00:00Z"}}]}`)
	p := &pager{pages: []string{pageOf("", n, node(9, "2026-11-09T00:00:00Z", "2026-11-09T00:00:00Z", ""))}}
	prs, _, err := NewWithRunner(".", p.run).MergedPRs(at("2026-11-01T00:00:00Z"), 5)
	if err != nil || len(prs) != 2 {
		t.Fatalf("got %v, %v", prs, err)
	}
	if len(prs[0].Commits) != 1 || prs[0].Commits[0].Checks != "" || prs[0].MergeCommit != "" {
		t.Errorf("pr = %+v", prs[0])
	}
	if prs[1].Commits == nil || len(prs[1].Commits) != 0 {
		t.Errorf("a PR with no commits: %#v", prs[1].Commits)
	}
}
