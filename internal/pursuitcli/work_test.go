package pursuitcli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCurrentWithNothingRecorded(t *testing.T) {
	notRepo := t.TempDir()
	if p, c, err := Current(notRepo, "sess-1"); p != "" || c != "" || err != nil {
		t.Errorf("outside a repo: %q %q %v", p, c, err)
	}
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if p, c, err := Current(repo, "sess-1"); p != "" || c != "" || err != nil {
		t.Errorf("no store file: %q %q %v", p, c, err)
	}
	if p, c, err := Current(repo, ""); p != "" || c != "" || err != nil {
		t.Errorf("no session: %q %q %v", p, c, err)
	}
}

func TestCurrentWithACorruptStoreIsAnError(t *testing.T) {
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	dir := filepath.Join(repo, ".git", "physa")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "physa.db"), []byte("this is not a database, just garbage bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p, c, err := Current(repo, "sess-1"); err == nil {
		t.Errorf("a corrupt store gave %q %q and no error", p, c)
	}
}
