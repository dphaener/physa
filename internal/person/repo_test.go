package person

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/forge"
)

type loginForge struct {
	forge.Forge
	login string
	err   error
}

func (f loginForge) Login() (string, error) { return f.login, f.err }

func TestForRepo(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	var stderr bytes.Buffer
	if got := ForRepo(repo, loginForge{login: "dphaener"}, &stderr); got != "dphaener" || stderr.Len() != 0 {
		t.Errorf("forge login: %q, stderr %q", got, stderr.String())
	}
	cfg, _ := ConfigDir()
	if b, _ := os.ReadFile(filepath.Join(cfg, "person")); strings.TrimSpace(string(b)) != "dphaener" {
		t.Errorf("setting %q", b)
	}

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if out, err := exec.Command("git", "-C", repo, "config", "user.email", "ana@example.com").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got := ForRepo(repo, nil, &stderr); got != "ana" {
		t.Errorf("email: %q", got)
	}

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if out, err := exec.Command("git", "-C", repo, "config", "--unset", "user.email").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got := ForRepo(repo, nil, &stderr); got != "" || !strings.Contains(stderr.String(), "physa: no person named: ") {
		t.Errorf("unknown: %q, stderr %q", got, stderr.String())
	}
}
