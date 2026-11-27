package merged

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/forge"
)

var (
	start = time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	since = time.Date(2025, 11, 26, 0, 0, 0, 0, time.UTC)
)

// answer is one scripted reply of the fake forge.
type answer struct {
	page forge.Page
	err  error
}

// fakeForge replies with its answers in order and records the cursors asked.
type fakeForge struct {
	answers []answer
	cursors []string
}

func (f *fakeForge) MergedPage(_ context.Context, _ time.Time, cursor string) (forge.Page, error) {
	f.cursors = append(f.cursors, cursor)
	if len(f.answers) == 0 {
		return forge.Page{}, errors.New("fake forge: no answer left")
	}
	a := f.answers[0]
	f.answers = f.answers[1:]
	return a.page, a.err
}

// clock is a fake clock whose Sleep records each wait and moves time on.
type clock struct {
	now   time.Time
	slept []time.Duration
}

func (c *clock) wait(limit time.Duration, progress *strings.Builder) Wait {
	w := Wait{
		Limit: limit,
		Sleep: func(d time.Duration) { c.slept = append(c.slept, d); c.now = c.now.Add(d) },
		Now:   func() time.Time { return c.now },
	}
	if progress != nil {
		w.Progress = progress
	}
	return w
}

func (c *clock) total() time.Duration {
	var t time.Duration
	for _, d := range c.slept {
		t += d
	}
	return t
}

// pr is PR n merged days days before start.
func pr(n, days int) forge.PR {
	return forge.PR{Number: n, MergedAt: start.AddDate(0, 0, -days)}
}

func page(next string, prs ...forge.PR) answer {
	return answer{page: forge.Page{PRs: prs, Next: next}}
}

func limited(rl *forge.RateLimit) answer { return answer{err: rl} }

func numbers(prs []forge.PR) string {
	var s []string
	for _, p := range prs {
		s = append(s, fmt.Sprint(p.Number))
	}
	return strings.Join(s, ",")
}

func TestListSecondaryLimitAfterTwoPages(t *testing.T) {
	t.Parallel()
	f := &fakeForge{answers: []answer{
		page("c1", pr(1, 1), pr(2, 2)),
		page("c2", pr(3, 3)),
		limited(&forge.RateLimit{Secondary: true, RetryAfter: 90 * time.Second}),
		page("", pr(4, 4), pr(5, 400)),
	}}
	c := &clock{now: start}
	var progress strings.Builder
	prs, more, err := List(t.Context(), f, since, 0, c.wait(time.Hour, &progress))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if more {
		t.Error("more = true with no cap")
	}
	if got := numbers(prs); got != "1,2,3,4" {
		t.Errorf("PRs = %s, want 1,2,3,4 (5 is before since)", got)
	}
	if got := strings.Join(f.cursors, " "); got != " c1 c2 c2" {
		t.Errorf("cursors = %q, want the limited page asked again", got)
	}
	if len(c.slept) != 1 || c.slept[0] != 90*time.Second {
		t.Errorf("slept %v, want [1m30s]", c.slept)
	}
	if got := progress.String(); !strings.Contains(got, "secondary rate limit") || strings.Count(got, "\n") != 1 {
		t.Errorf("progress = %q, want one line naming the secondary limit", got)
	}
}

func TestListDoublesWithNoHints(t *testing.T) {
	t.Parallel()
	f := &fakeForge{answers: []answer{
		page("c1", pr(1, 1)),
		limited(&forge.RateLimit{Secondary: true}),
		limited(&forge.RateLimit{Secondary: true}),
		limited(&forge.RateLimit{Secondary: true}),
		page("c2", pr(2, 2)),
		limited(&forge.RateLimit{Secondary: true}),
		page("", pr(3, 3)),
	}}
	c := &clock{now: start}
	prs, _, err := List(t.Context(), f, since, 0, c.wait(time.Hour, nil))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, time.Minute}
	if fmt.Sprint(c.slept) != fmt.Sprint(want) {
		t.Errorf("slept %v, want %v (doubling, reset by a page that succeeds)", c.slept, want)
	}
	if got := numbers(prs); got != "1,2,3" {
		t.Errorf("PRs = %s, want 1,2,3", got)
	}
}

func TestListWaitsForPrimaryReset(t *testing.T) {
	t.Parallel()
	reset := start.Add(17 * time.Minute)
	f := &fakeForge{answers: []answer{
		limited(&forge.RateLimit{Reset: reset}),
		page("", pr(1, 1)),
	}}
	c := &clock{now: start}
	var progress strings.Builder
	prs, _, err := List(t.Context(), f, since, 0, c.wait(0, &progress))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(c.slept) != 1 || c.slept[0] != 17*time.Minute+time.Second {
		t.Errorf("slept %v, want [17m1s]", c.slept)
	}
	if !strings.Contains(progress.String(), "primary rate limit") || !strings.Contains(progress.String(), reset.Add(time.Second).Format(time.RFC3339)) {
		t.Errorf("progress = %q, want the primary limit and when it resumes", progress.String())
	}
	if numbers(prs) != "1" {
		t.Errorf("PRs = %s, want 1", numbers(prs))
	}
}

func TestListRetryAfterBeforeReset(t *testing.T) {
	t.Parallel()
	f := &fakeForge{answers: []answer{
		limited(&forge.RateLimit{RetryAfter: 30 * time.Second, Reset: start.Add(time.Hour)}),
		page(""),
	}}
	c := &clock{now: start}
	if _, _, err := List(t.Context(), f, since, 0, c.wait(0, nil)); err != nil {
		t.Fatalf("List: %v", err)
	}
	if fmt.Sprint(c.slept) != "[30s]" {
		t.Errorf("slept %v, want [30s]", c.slept)
	}
}

func TestListRefusesPastLimit(t *testing.T) {
	t.Parallel()
	rl := &forge.RateLimit{Secondary: true}
	f := &fakeForge{answers: []answer{
		page("c1", pr(1, 1)),
		page("c2", pr(2, 2)),
		limited(rl), limited(rl), limited(rl), limited(rl), limited(rl),
	}}
	c := &clock{now: start}
	limit := 5 * time.Minute
	prs, more, err := List(t.Context(), f, since, 0, c.wait(limit, nil))
	var got *forge.RateLimit
	if !errors.As(err, &got) {
		t.Fatalf("err = %v, want a *forge.RateLimit", err)
	}
	if !strings.Contains(err.Error(), "still limited after waiting 3m0s") {
		t.Errorf("err = %q, want it to say the total waited", err)
	}
	if numbers(prs) != "1,2" {
		t.Errorf("PRs = %s, want the two pages' PRs, newest merge first", numbers(prs))
	}
	if more {
		t.Error("more = true on a stop")
	}
	if c.total() > limit {
		t.Errorf("slept %v in total, over the limit %v", c.total(), limit)
	}
	if fmt.Sprint(c.slept) != "[1m0s 2m0s]" {
		t.Errorf("slept %v, want [1m0s 2m0s] and no 4m wait past the limit", c.slept)
	}
	t.Logf("kept PRs %s; slept %v (total %v of limit %v); err: %v", numbers(prs), c.slept, c.total(), limit, err)
}

func TestListMax(t *testing.T) {
	t.Parallel()
	pages := func() *fakeForge {
		return &fakeForge{answers: []answer{
			page("c1", pr(1, 1), pr(2, 2)),
			page("c2", pr(3, 3), pr(4, 4)),
			page("", pr(5, 5), pr(6, 500)),
		}}
	}
	f := pages()
	prs, more, err := List(t.Context(), f, since, 3, Wait{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !more || numbers(prs) != "1,2,3" {
		t.Errorf("max 3: PRs %s more %v, want 1,2,3 and more", numbers(prs), more)
	}
	if len(f.cursors) != 2 {
		t.Errorf("max 3 asked %d pages, want 2", len(f.cursors))
	}

	prs, more, err = List(t.Context(), pages(), since, 5, Wait{})
	if err != nil || more || numbers(prs) != "1,2,3,4,5" {
		t.Errorf("max 5 (the list ends at 5): PRs %s more %v err %v, want 1-5, no more", numbers(prs), more, err)
	}

	prs, more, err = List(t.Context(), pages(), since, 0, Wait{})
	if err != nil || more || numbers(prs) != "1,2,3,4,5" {
		t.Errorf("max 0: PRs %s more %v err %v, want every PR since, no more", numbers(prs), more, err)
	}
}

func TestListPlainError(t *testing.T) {
	t.Parallel()
	boom := errors.New("GitHub GraphQL: something broke")
	f := &fakeForge{answers: []answer{{err: boom}}}
	c := &clock{now: start}
	prs, more, err := List(t.Context(), f, since, 0, c.wait(0, nil))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the forge's error", err)
	}
	if prs == nil || len(prs) != 0 || more {
		t.Errorf("PRs %v more %v, want none listed", prs, more)
	}
	if len(c.slept) != 0 {
		t.Errorf("slept %v on a plain error", c.slept)
	}
}

func TestListPlainErrorKeepsPages(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	f := &fakeForge{answers: []answer{page("c1", pr(1, 1)), {err: boom}}}
	prs, _, err := List(t.Context(), f, since, 0, Wait{})
	if !errors.Is(err, boom) || numbers(prs) != "1" {
		t.Errorf("PRs %s err %v, want 1 and the error", numbers(prs), err)
	}
}

func TestOldest(t *testing.T) {
	t.Parallel()
	if !Oldest(nil).IsZero() {
		t.Error("Oldest(nil) not zero")
	}
	if got := Oldest([]forge.PR{pr(1, 3), pr(2, 9), pr(3, 1)}); !got.Equal(start.AddDate(0, 0, -9)) {
		t.Errorf("Oldest = %v", got)
	}
}
