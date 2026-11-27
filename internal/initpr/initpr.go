// Package initpr is init's last step on the repo: it installs the Session:
// hook, and opens or refreshes the PR of physa/init with the body of what
// was written. It never merges a PR; a forge that cannot open PRs, or is
// unavailable or not signed in, leaves the branch ready, with the reason.
package initpr

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/initbranch"
	"github.com/dphaener/physa/internal/sessionhook"
	"github.com/dphaener/physa/internal/vcs"
)

// InstallHook installs the Session: hook in the repo here and says what
// happened; any error is a refusal, with its reason. hooks gives the repo's
// hooks for the directory it is called with.
func InstallHook(hooks func(dir string) (vcs.Hooks, error)) initbranch.Hook {
	root, err := filepath.Abs(".")
	var h vcs.Hooks
	if err == nil {
		h, err = hooks(".")
	}
	var got sessionhook.Outcome
	if err == nil {
		got, err = sessionhook.Install(root, h)
	}
	switch {
	case err != nil:
		return initbranch.Hook{State: initbranch.HookRefused, Detail: err.Error()}
	case got.Written:
		return initbranch.Hook{State: initbranch.HookInstalled, Detail: got.Path}
	case got.Set:
		return initbranch.Hook{State: initbranch.HookInstalled, Detail: "core.hooksPath = " + sessionhook.Dir}
	}
	return initbranch.Hook{State: initbranch.HookAlready, Detail: got.Path}
}

// Open opens the PR of physa/init into the default branch with out's
// body, or refreshes the open one's body. A forge that cannot open PRs, is
// unavailable or is not signed in leaves the branch ready, with the reason.
// It never merges a PR.
func Open(w vcs.Writer, f forge.Forge, out initbranch.Outcome) (initbranch.PR, error) {
	prs, ok := f.(forge.PRs)
	if !ok {
		return initbranch.PR{State: initbranch.PRReady, Reason: "the forge cannot open PRs"}, nil
	}
	pr := initbranch.PR{State: initbranch.PRRefreshed}
	n, url, open, err := prs.OpenPR(initbranch.Branch)
	if err == nil && open {
		err = prs.EditPR(n, out.Body)
	} else if err == nil {
		pr.State = initbranch.PROpened
		var def string
		if def, err = w.DefaultBranch(context.TODO()); err == nil {
			if remote, name, ok := strings.Cut(def, "/"); ok && remote != "" {
				def = name
			}
			n, url, err = prs.CreatePR(initbranch.Branch, def, out.Commit.Subject, out.Body)
		}
	}
	if errors.Is(err, forge.ErrUnavailable) || errors.Is(err, forge.ErrUnauthenticated) {
		return initbranch.PR{State: initbranch.PRReady, Reason: forge.Reason(err)}, nil
	}
	if err != nil {
		return initbranch.PR{}, fmt.Errorf("the PR of %s: %w", initbranch.Branch, err)
	}
	pr.Number, pr.URL = n, url
	return pr, nil
}
