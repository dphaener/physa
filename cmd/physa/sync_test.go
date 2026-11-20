package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/github"
	"github.com/dphaener/physa/internal/history"
	"github.com/dphaener/physa/internal/prcheck"
	"github.com/dphaener/physa/internal/store"
)

// syncRun is one canned check run for a head.
type syncRun struct {
	id     int64
	status string
	text   string
}

// useSyncGitHub installs a fake gh that lists prs and answers check run
// queries from runs, keyed by head SHA. It counts the check run queries.
func useSyncGitHub(t *testing.T, prs string, runs map[string][]syncRun, queries *int) {
	t.Helper()
	old := newGitHub
	newGitHub = func(dir string) *github.Client {
		return github.NewWithRunner(dir, func(_ string, args ...string) ([]byte, error) {
			if args[0] != "api" {
				return nil, &github.Error{Args: args, Stderr: "unexpected", ExitCode: 1}
			}
			if strings.Contains(args[1], "check-runs") {
				if queries != nil {
					*queries++
				}
				sha := strings.Split(strings.Split(args[1], "/commits/")[1], "/")[0]
				var b bytes.Buffer
				for _, r := range runs[sha] {
					line, _ := json.Marshal(map[string]any{
						"id": r.id, "head_sha": sha, "status": r.status, "conclusion": "neutral",
						"completed_at": fmt.Sprintf("2026-11-20T10:00:%02dZ", r.id%60),
						"output":       map[string]any{"title": "t", "text": r.text},
					})
					b.Write(line)
					b.WriteByte('\n')
				}
				return b.Bytes(), nil
			}
			return []byte(prs), nil
		})
	}
	t.Cleanup(func() { newGitHub = old })
}

// syncText is the check run text for a result with one map problem count.
func syncText(t *testing.T, pr int, rng string) string {
	t.Helper()
	out, err := prcheck.CheckRunOutput(prcheck.Result{Version: prcheck.Version, PR: pr, Range: rng})
	if err != nil {
		t.Fatal(err)
	}
	return out.Text
}

func syncRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "base")
	return dir
}

func runSync(t *testing.T, dir string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := sync(dir, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func storeRows(t *testing.T, dir string) []store.PRCheck {
	t.Helper()
	repo, err := history.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	common, err := repo.CommonDir()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(common)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, err := s.PRChecks()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

const syncPRs = `{"number":1,"state":"closed","merged_at":"2026-11-01T00:00:00Z","head":"aaa"}
{"number":2,"state":"open","merged_at":null,"head":"bbb"}
{"number":3,"state":"open","merged_at":null,"head":"ccc"}
`

func TestSyncAddsThenChangesNothing(t *testing.T) {
	dir := syncRepo(t)
	queries := 0
	useSyncGitHub(t, syncPRs, map[string][]syncRun{
		"aaa": {{1, "completed", syncText(t, 1, "a..b")}},
		"bbb": {{2, "completed", syncText(t, 2, "a..b")}},
	}, &queries)

	code, out, errOut := runSync(t, dir)
	if code != 0 || errOut != "" {
		t.Fatalf("first sync = %d, %q, %q", code, out, errOut)
	}
	if want := "physa sync: 3 PRs read, 2 added, 0 updated, 0 unchanged, 1 without a physa check run\n"; out != want {
		t.Errorf("first line = %q, want %q", out, want)
	}
	rows1 := storeRows(t, dir)
	if len(rows1) != 2 {
		t.Fatalf("rows = %v", rows1)
	}

	code, out, _ = runSync(t, dir)
	if code != 0 {
		t.Fatalf("second sync = %d", code)
	}
	if want := "physa sync: 3 PRs read, 0 added, 0 updated, 2 unchanged, 1 without a physa check run\n"; out != want {
		t.Errorf("second line = %q, want %q", out, want)
	}
	rows2 := storeRows(t, dir)
	if fmt.Sprint(rows1) != fmt.Sprint(rows2) {
		t.Errorf("rows changed:\n%v\n%v", rows1, rows2)
	}
}

func TestSyncSkipsStoredMergedHeads(t *testing.T) {
	dir := syncRepo(t)
	queries := 0
	useSyncGitHub(t, `{"number":1,"state":"closed","merged_at":"2026-11-01T00:00:00Z","head":"aaa"}`+"\n",
		map[string][]syncRun{"aaa": {{1, "completed", syncText(t, 1, "r")}}}, &queries)
	runSync(t, dir)
	runSync(t, dir)
	if queries != 1 {
		t.Errorf("check run queries = %d, want 1 (the merged head is skipped the second time)", queries)
	}
}

func TestSyncCountsUnreadableRuns(t *testing.T) {
	dir := syncRepo(t)
	useSyncGitHub(t, `{"number":2,"state":"open","merged_at":null,"head":"bbb"}`+"\n",
		map[string][]syncRun{"bbb": {{5, "completed", "not a result"}, {6, "in_progress", syncText(t, 2, "r")}}}, nil)
	code, out, _ := runSync(t, dir)
	want := "physa sync: 1 PRs read, 0 added, 0 updated, 0 unchanged, 0 without a physa check run, 1 unreadable\n"
	if code != 0 || out != want {
		t.Errorf("sync = %d, %q, want %q", code, out, want)
	}
	if rows := storeRows(t, dir); len(rows) != 0 {
		t.Errorf("rows = %v, want none", rows)
	}
}

func TestSyncUpdatesOpenPRWithNewerRun(t *testing.T) {
	dir := syncRepo(t)
	prs := `{"number":2,"state":"open","merged_at":null,"head":"bbb"}` + "\n"
	useSyncGitHub(t, prs, map[string][]syncRun{"bbb": {{5, "completed", syncText(t, 2, "old")}}}, nil)
	runSync(t, dir)
	useSyncGitHub(t, prs, map[string][]syncRun{"bbb": {
		{5, "completed", syncText(t, 2, "old")},
		{9, "completed", syncText(t, 2, "new")},
		{7, "completed", "junk"},
	}}, nil)
	code, out, _ := runSync(t, dir)
	if want := "physa sync: 1 PRs read, 0 added, 1 updated, 0 unchanged, 0 without a physa check run\n"; code != 0 || out != want {
		t.Errorf("sync = %d, %q, want %q", code, out, want)
	}
	rows := storeRows(t, dir)
	if len(rows) != 1 || rows[0].CheckRunID != 9 || !strings.Contains(rows[0].Result, `"range":"new"`) {
		t.Errorf("rows = %v", rows)
	}
}

func TestSyncNoGH(t *testing.T) {
	dir := syncRepo(t)
	old := newGitHub
	newGitHub = func(dir string) *github.Client {
		return github.NewWithRunner(dir, func(string, ...string) ([]byte, error) { return nil, exec.ErrNotFound })
	}
	t.Cleanup(func() { newGitHub = old })
	code, out, errOut := runSync(t, dir)
	if code != 2 || out != "" || errOut != "physa sync: gh is not installed\n" {
		t.Errorf("sync = %d, %q, %q", code, out, errOut)
	}
}

func TestSyncUnauthenticated(t *testing.T) {
	dir := syncRepo(t)
	old := newGitHub
	newGitHub = func(dir string) *github.Client {
		return github.NewWithRunner(dir, func(_ string, args ...string) ([]byte, error) {
			return nil, &github.Error{Args: args, Stderr: "To get started with GitHub CLI, please run: gh auth login", ExitCode: 4}
		})
	}
	t.Cleanup(func() { newGitHub = old })
	_, _, errOut := runSync(t, dir)
	if errOut != "physa sync: gh is not authenticated; run gh auth login\n" {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestSyncOtherErrorExitsTwo(t *testing.T) {
	dir := syncRepo(t)
	old := newGitHub
	newGitHub = func(dir string) *github.Client {
		return github.NewWithRunner(dir, func(string, ...string) ([]byte, error) { return nil, errors.New("boom") })
	}
	t.Cleanup(func() { newGitHub = old })
	code, _, errOut := runSync(t, dir)
	if code != 2 || !strings.Contains(errOut, "boom") {
		t.Errorf("sync = %d, %q", code, errOut)
	}
}

func TestSyncUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"sync", "extra"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "usage:") {
		t.Errorf("sync extra = %d, %q", code, stderr.String())
	}
}

func TestSyncWorktreeSharesTheDatabase(t *testing.T) {
	dir := syncRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitIn(t, dir, "worktree", "add", "-q", "-b", "other", wt)
	useSyncGitHub(t, `{"number":2,"state":"open","merged_at":null,"head":"bbb"}`+"\n",
		map[string][]syncRun{"bbb": {{5, "completed", syncText(t, 2, "r")}}}, nil)
	if code, out, _ := runSync(t, wt); code != 0 || !strings.Contains(out, "1 added") {
		t.Fatalf("sync in worktree = %d, %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(wt, ".git", "physa")); err == nil {
		t.Error("the worktree has its own store")
	}
	if rows := storeRows(t, dir); len(rows) != 1 {
		t.Errorf("rows in the main checkout = %v", rows)
	}
	if code, out, _ := runSync(t, dir); code != 0 || !strings.Contains(out, "0 added, 0 updated") {
		t.Errorf("sync in main checkout = %d, %q", code, out)
	}
}
