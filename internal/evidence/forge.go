package evidence

import (
	"context"
	"time"

	"github.com/dphaener/physa/internal/forge"
)

// Merged is the part of a forge that observing reads: the PRs merged since a
// time.
type Merged = forge.Merged

// forgeMax is how many merged PRs one look asks for.
const forgeMax = 500

// observeForge returns the lead time and CI facts of the PRs merged after
// since, and the newest merged-at seen (since itself when none is newer).
// When the forge fails it returns the error and since unmoved.
func observeForge(ctx context.Context, repo Repo, f Merged, since, now time.Time) ([]Fact, time.Time, error) {
	from := since
	if from.IsZero() {
		from = now.Add(-window)
	}
	prs, _, err := f.MergedPRs(ctx, from, forgeMax)
	if err != nil {
		return nil, since, err
	}
	newest := since
	var facts []Fact
	for i := len(prs) - 1; i >= 0; i-- { // newest merge first, so oldest last
		pr := prs[i]
		if pr.MergedAt.IsZero() || (!since.IsZero() && !pr.MergedAt.After(since)) {
			continue
		}
		if pr.MergedAt.After(newest) {
			newest = pr.MergedAt
		}
		base := Fact{Commit: pr.MergeCommit, PR: pr.Number, At: pr.MergedAt, Source: SourceForge}
		if id, ok := repo.Resolve(ctx, pr.MergeCommit); ok && pr.MergeCommit != "" {
			if c, err := repo.Latest(ctx, id, 1); err == nil && len(c) == 1 {
				base = fromMessage(base, c[0].Message)
			}
		}
		if !pr.CreatedAt.IsZero() {
			lead := base
			lead.Kind = KindLeadTime
			secs := int64(pr.MergedAt.Sub(pr.CreatedAt) / time.Second)
			lead.Seconds = &secs
			facts = append(facts, lead)
		}
		if kind := ciKind(pr.Commits); kind != "" {
			ci := base
			ci.Kind = kind
			facts = append(facts, ci)
		}
	}
	return facts, newest, nil
}

// ciKind classifies a PR's commits, oldest first, by their checks: the last
// one that has a result decides, and an earlier failure makes a pass
// "passed after failing". No result gives "".
func ciKind(commits []forge.Commit) string {
	failed, last := false, ""
	for _, c := range commits {
		switch c.Checks {
		case "FAILURE", "ERROR":
			failed, last = true, "fail"
		case "SUCCESS":
			last = "pass"
		}
	}
	switch {
	case last == "pass" && failed:
		return KindCIPassedAfterFails
	case last == "pass":
		return KindCIPassed
	case last == "fail":
		return KindCIFailed
	}
	return ""
}
