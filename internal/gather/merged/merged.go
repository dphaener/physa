// Package merged lists a forge's merged PRs page by page within gather's
// bound, and waits out the forge's rate limits as GitHub says: its
// retry-after, else until its primary limit resets, else a minute doubling on
// each repeat, up to a total wait limit. It keeps every PR it listed, even
// when the forge stops it, so gather can keep what it has, and says how far
// back a stopped list reached with Stopped.
package merged

import (
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"time"

	"github.com/dphaener/physa/internal/forge"
)

// DefaultLimit is how long List waits in total before it gives up: one full
// hour of the primary limit plus a few secondary waits.
const DefaultLimit = 65 * time.Minute

// backoff is the first wait for a rate limit that gives no hint; it doubles
// on each consecutive limit.
const backoff = time.Minute

// Wait is how List waits out the forge's rate limits. A zero field means its
// default.
type Wait struct {
	// Limit is the most List waits in total; DefaultLimit when zero.
	Limit time.Duration
	// Sleep waits for a duration; time.Sleep when nil.
	Sleep func(time.Duration)
	// Now is the current time; time.Now when nil.
	Now func() time.Time
	// Progress, when set, gets one line for each wait, naming the limit and
	// when List resumes.
	Progress io.Writer
}

// Defaults stands in for the zero fields of every Wait, before the package's
// own defaults. It is a seam for the tests of callers that make their own
// Wait, as init does: they set a short Limit and a Sleep and Now that do not
// wait. Nothing else sets it.
var Defaults Wait

func (w Wait) defaults() Wait {
	if w.Limit <= 0 {
		w.Limit = Defaults.Limit
	}
	if w.Limit <= 0 {
		w.Limit = DefaultLimit
	}
	if w.Sleep == nil {
		w.Sleep = Defaults.Sleep
	}
	if w.Sleep == nil {
		w.Sleep = time.Sleep
	}
	if w.Now == nil {
		w.Now = Defaults.Now
	}
	if w.Now == nil {
		w.Now = time.Now
	}
	return w
}

// List returns the PRs merged at or after since, newest merge first, at most
// max (max 0 or below means no cap). more is true when max stopped the list
// before since did. It asks f for pages from the first until no page is left
// or max is reached. When f answers with a *forge.RateLimit it waits as the
// limit says and asks for the same page again; a wait that would take its
// total waiting past w.Limit is not slept, and List returns the PRs so far
// with an error wrapping that limit. Any other error returns the PRs so far
// and the error.
func List(f forge.MergedPages, since time.Time, max int, w Wait) (prs []forge.PR, more bool, err error) {
	w = w.defaults()
	var waited time.Duration
	limits := 0
	cursor := ""
	for {
		page, perr := f.MergedPage(since, cursor)
		if perr != nil {
			var rl *forge.RateLimit
			if !errors.As(perr, &rl) {
				return finish(prs, max), false, perr
			}
			limits++
			d := wait(rl, limits, w.Now())
			if waited+d > w.Limit {
				return finish(prs, max), false, fmt.Errorf("still limited after waiting %s in total: %w", waited, perr)
			}
			if w.Progress != nil {
				fmt.Fprintf(w.Progress, "%s: waiting %s, resuming at %s\n",
					kind(rl), d, w.Now().Add(d).Format(time.RFC3339))
			}
			w.Sleep(d)
			waited += d
			continue
		}
		limits = 0
		for _, pr := range page.PRs {
			if !pr.MergedAt.Before(since) {
				prs = append(prs, pr)
			}
		}
		exhausted := page.Next == ""
		if max > 0 && (len(prs) > max || (len(prs) == max && !exhausted)) {
			more = true
		}
		if exhausted || (max > 0 && len(prs) >= max) {
			return finish(prs, max), more, nil
		}
		cursor = page.Next
	}
}

// wait is how long to wait for the n-th consecutive limit rl, at now:
// RetryAfter when given; else until Reset and one second more; else the
// backoff doubled for each limit before it. It is never under a second, so a
// Reset already past does not ask again at once.
func wait(rl *forge.RateLimit, n int, now time.Time) time.Duration {
	var d time.Duration
	switch {
	case rl.RetryAfter > 0:
		d = rl.RetryAfter
	case !rl.Reset.IsZero():
		d = rl.Reset.Add(time.Second).Sub(now)
	default:
		d = backoff << min(n-1, 16)
	}
	return max(d, time.Second)
}

func kind(rl *forge.RateLimit) string {
	if rl.Secondary {
		return "the forge's secondary rate limit"
	}
	return "the forge's primary rate limit"
}

// finish sorts prs newest merge first and cuts them to max (no cap when max
// is 0 or below). It never returns nil.
func finish(prs []forge.PR, max int) []forge.PR {
	sort.SliceStable(prs, func(i, j int) bool { return prs[i].MergedAt.After(prs[j].MergedAt) })
	if max > 0 && len(prs) > max {
		prs = prs[:max]
	}
	if prs == nil {
		prs = []forge.PR{}
	}
	return prs
}

// Oldest returns the oldest merge time of prs, the date a list reached back
// to; zero when prs is empty.
func Oldest(prs []forge.PR) time.Time {
	var t time.Time
	for _, pr := range prs {
		if t.IsZero() || pr.MergedAt.Before(t) {
			t = pr.MergedAt
		}
	}
	return t
}

// Read lists through f the PRs merged at or after since, at most max (0 or
// below means no cap): page by page with List when f offers
// forge.MergedPages, else in one call to forge.Merged. reason is not empty
// when PRs are unavailable: f is nil, offers neither, or is unavailable or
// unauthenticated before it listed any PR. Any other error, or one after PRs
// were listed, is a *Stopped, with prs holding every PR listed and more
// false.
func Read(f forge.Forge, since time.Time, max int, w Wait) (prs []forge.PR, more bool, reason string, err error) {
	if p, ok := f.(forge.MergedPages); ok {
		prs, more, err = List(p, since, max, w)
	} else if m, ok := f.(forge.Merged); ok {
		if max <= 0 {
			max = math.MaxInt // no cap
		}
		prs, more, err = m.MergedPRs(since, max)
	} else if f == nil {
		return nil, false, "no forge", nil
	} else {
		return nil, false, "the forge cannot list merged PRs", nil
	}
	if err == nil {
		return prs, more, "", nil
	}
	if len(prs) == 0 && (errors.Is(err, forge.ErrUnavailable) || errors.Is(err, forge.ErrUnauthenticated)) {
		return nil, false, forge.Reason(err), nil
	}
	return prs, false, "", &Stopped{Reached: Oldest(prs), Since: since, PRs: len(prs), Err: err}
}

// Stopped is the error of a list the forge stopped before it reached back to
// Since: the PRs kept, PRs of them, reach back to Reached (zero when none
// were listed), and Err is why the forge stopped.
type Stopped struct {
	Reached time.Time
	Since   time.Time
	PRs     int
	Err     error
}

// Error names why the forge stopped, the date the PRs kept reach back to (or
// that none were listed), the date asked for and how many PRs were kept.
func (e *Stopped) Error() string {
	since := e.Since.UTC().Format(time.DateOnly)
	if e.PRs == 0 {
		return fmt.Sprintf("the forge stopped the gather: %v; no PR was listed of the asked %s", e.Err, since)
	}
	prs := "PRs"
	if e.PRs == 1 {
		prs = "PR"
	}
	return fmt.Sprintf("the forge stopped the gather: %v; the %d %s kept reach back to %s of the asked %s",
		e.Err, e.PRs, prs, e.Reached.UTC().Format(time.DateOnly), since)
}

// Unwrap returns why the forge stopped.
func (e *Stopped) Unwrap() error { return e.Err }
