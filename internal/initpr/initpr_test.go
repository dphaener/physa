package initpr

import (
	"context"
	"errors"
	"testing"

	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/initbranch"
	"github.com/dphaener/physa/internal/proc"
	"github.com/dphaener/physa/internal/vcs"
)

// readOnlyForge is a forge that cannot open PRs: it has the read side only.
type readOnlyForge struct{ forge.Forge }

// openingForge is a forge that can open PRs, and fails each call with err.
type openingForge struct {
	forge.Forge
	err error
}

func (f openingForge) OpenPR(context.Context, string) (int, string, bool, error) {
	return 0, "", false, f.err
}
func (f openingForge) CreatePR(context.Context, string, string, string, string) (int, string, error) {
	return 0, "", f.err
}
func (f openingForge) EditPR(context.Context, int, string) error { return f.err }

func TestOpenLeavesTheBranchReadyWithoutPRs(t *testing.T) {
	t.Parallel()
	pr, err := Open(t.Context(), nil, readOnlyForge{}, initbranch.Outcome{})
	if err != nil || pr != (initbranch.PR{State: initbranch.PRReady, Reason: "the forge cannot open PRs"}) {
		t.Fatalf("PR = %+v, %v", pr, err)
	}
}

func TestOpenLeavesTheBranchReadyWhenTheForgeIsUnavailable(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{forge.ErrUnavailable, forge.ErrUnauthenticated} {
		pr, err := Open(t.Context(), nil, openingForge{err: cause}, initbranch.Outcome{})
		if err != nil {
			t.Fatalf("%v: err %v", cause, err)
		}
		if pr.State != initbranch.PRReady || pr.Reason != forge.Reason(cause) {
			t.Errorf("%v: PR = %+v", cause, pr)
		}
	}
}

func TestOpenReportsAnyOtherForgeErrorWithTheBranch(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	_, err := Open(t.Context(), nil, openingForge{err: boom}, initbranch.Outcome{})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the forge error", err)
	}
}

func TestInstallHookRefusesWithItsReason(t *testing.T) {
	t.Parallel()
	boom := errors.New("no hooks here")
	h := InstallHook(t.Context(), proc.State{Dir: ".", Wd: "/repo"}, func(context.Context, string) (vcs.Hooks, error) { return nil, boom })
	if h.State != initbranch.HookRefused || h.Detail != boom.Error() {
		t.Fatalf("hook = %+v", h)
	}
}

func TestInstallHookOpensTheHooksOfTheStatesDirectory(t *testing.T) {
	t.Parallel()
	var asked []string
	boom := errors.New("no hooks here")
	h := InstallHook(t.Context(), proc.State{Dir: "sub", Wd: "/repo"}, func(_ context.Context, dir string) (vcs.Hooks, error) {
		asked = append(asked, dir)
		return nil, boom
	})
	if len(asked) != 1 || asked[0] != "sub" || h.State != initbranch.HookRefused {
		t.Fatalf("asked %v, hook %+v", asked, h)
	}
}

func TestInstallHookRefusesWithoutAWorkingDirectory(t *testing.T) {
	t.Parallel()
	gone := errors.New("getwd: no such file or directory")
	h := InstallHook(t.Context(), proc.State{Dir: ".", WdErr: gone}, func(context.Context, string) (vcs.Hooks, error) {
		t.Error("the hooks were opened with no working directory")
		return nil, nil
	})
	if h.State != initbranch.HookRefused || h.Detail != gone.Error() {
		t.Fatalf("hook = %+v", h)
	}
}
