package person

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/proc"
)

type loginForge struct {
	forge.Forge
	login string
	err   error
}

func (f loginForge) Login(_ context.Context) (string, error) { return f.login, f.err }

func TestForRepo(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	cfg := t.TempDir()
	env := func() []string {
		return append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "XDG_CONFIG_HOME="+cfg)
	}
	st := func() proc.State { return proc.State{Dir: repo, Wd: repo, Env: env()} }
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = env()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	git("init", "-q")

	var stderr bytes.Buffer
	if got := ForRepo(t.Context(), st(), repo, loginForge{login: "dphaener"}, &stderr); got != "dphaener" || stderr.Len() != 0 {
		t.Errorf("forge login: %q, stderr %q", got, stderr.String())
	}
	if b, _ := os.ReadFile(filepath.Join(cfg, "physa", "person")); strings.TrimSpace(string(b)) != "dphaener" {
		t.Errorf("setting %q", b)
	}

	cfg = t.TempDir()
	git("config", "user.email", "ana@example.com")
	if got := ForRepo(t.Context(), st(), repo, nil, &stderr); got != "ana" {
		t.Errorf("email: %q", got)
	}

	cfg = t.TempDir()
	git("config", "--unset", "user.email")
	if got := ForRepo(t.Context(), st(), repo, nil, &stderr); got != "" || !strings.Contains(stderr.String(), "physa: no person named: ") {
		t.Errorf("unknown: %q, stderr %q", got, stderr.String())
	}
}
