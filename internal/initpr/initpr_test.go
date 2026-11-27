package initpr

import (
	"errors"
	"testing"

	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/initbranch"
	"github.com/dphaener/physa/internal/vcs"
)

// readOnlyForge is a forge that cannot open PRs: it has the read side only.
type readOnlyForge struct{ forge.Forge }

// openingForge is a forge that can open PRs, and fails each call with err.
type openingForge struct {
	forge.Forge
	err error
}

func (f openingForge) OpenPR(string) (int, string, bool, error) { return 0, "", false, f.err }
func (f openingForge) CreatePR(string, string, string, string) (int, string, error) {
	return 0, "", f.err
}
func (f openingForge) EditPR(int, string) error { return f.err }

func TestOpenLeavesTheBranchReadyWithoutPRs(t *testing.T) {
	pr, err := Open(nil, readOnlyForge{}, initbranch.Outcome{})
	if err != nil || pr != (initbranch.PR{State: initbranch.PRReady, Reason: "the forge cannot open PRs"}) {
		t.Fatalf("PR = %+v, %v", pr, err)
	}
}

func TestOpenLeavesTheBranchReadyWhenTheForgeIsUnavailable(t *testing.T) {
	for _, cause := range []error{forge.ErrUnavailable, forge.ErrUnauthenticated} {
		pr, err := Open(nil, openingForge{err: cause}, initbranch.Outcome{})
		if err != nil {
			t.Fatalf("%v: err %v", cause, err)
		}
		if pr.State != initbranch.PRReady || pr.Reason != forge.Reason(cause) {
			t.Errorf("%v: PR = %+v", cause, pr)
		}
	}
}

func TestOpenReportsAnyOtherForgeErrorWithTheBranch(t *testing.T) {
	boom := errors.New("boom")
	_, err := Open(nil, openingForge{err: boom}, initbranch.Outcome{})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the forge error", err)
	}
}

func TestInstallHookRefusesWithItsReason(t *testing.T) {
	boom := errors.New("no hooks here")
	h := InstallHook(func(string) (vcs.Hooks, error) { return nil, boom })
	if h.State != initbranch.HookRefused || h.Detail != boom.Error() {
		t.Fatalf("hook = %+v", h)
	}
}
