package learnbranch

import (
	"context"
	"fmt"
	"strings"

	"github.com/dphaener/physa/internal/vcs"
)

// prTitle is the title of the branch's PR.
const prTitle = "docs(learning): Proposals learned from merged changes"

// prIntro opens the PR's body.
const prIntro = "Learning proposes these map entries from merged changes. " +
	"This PR never merges itself: stewards accept, edit or drop each one."

// openPR opens or refreshes the branch's PR after a run that wrote to it.
// Without a forge the branch is ready for review. A run that wrote nothing,
// or a branch that holds no proposal, leaves the PR alone; an open PR is
// refreshed with the rebuilt body, and a new one is opened only when this
// run applied a proposal. It never merges a PR.
func openPR(ctx context.Context, w vcs.Writer, out *Outcome, st state, o Options) error {
	if o.PRs == nil {
		out.PR = PR{State: PRReady, Reason: o.ForgeReason}
		return nil
	}
	out.PR = PR{State: PRNone}
	if out.Commit == nil {
		return nil
	}
	body, ok, err := prBody(ctx, w, st.def, out.Commit.ID)
	if err != nil || !ok {
		return err
	}
	n, url, open, err := o.PRs.OpenPR(Branch)
	if err != nil {
		return fmt.Errorf("the PR of %s cannot be read: %w", Branch, err)
	}
	if open {
		if err := o.PRs.EditPR(n, body); err != nil {
			return fmt.Errorf("PR #%d cannot be refreshed: %w", n, err)
		}
		out.PR = PR{State: PRRefreshed, Number: n, URL: url}
		return nil
	}
	if len(out.Report.Kept) == 0 {
		return nil
	}
	n, url, err = o.PRs.CreatePR(Branch, st.name, prTitle, body)
	if err != nil {
		return fmt.Errorf("the PR of %s cannot be opened: %w", Branch, err)
	}
	out.PR = PR{State: PROpened, Number: n, URL: url}
	return nil
}

// prBody is the PR's body: the intro, then the proposal lines of every
// change of def..tip, oldest first. It is false when there are none.
func prBody(ctx context.Context, w vcs.Repo, def, tip string) (string, bool, error) {
	changes, err := w.Changes(ctx, def+".."+tip)
	if err != nil {
		return "", false, err
	}
	var lines []string
	for i := len(changes) - 1; i >= 0; i-- {
		lines = append(lines, proposalLines(changes[i].Message)...)
	}
	if len(lines) == 0 {
		return "", false, nil
	}
	return prIntro + "\n\n" + strings.Join(lines, "\n") + "\n", true, nil
}
