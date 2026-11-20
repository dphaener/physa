package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/prcheck"
)

// reviewRepo is a repo with a sound map of capabilities a and b on main and a
// branch, checked out, with one commit that changes docs/d.md, anchored to a
// and having loaded b.
func reviewRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	root := tempMap(t, "`r` · from docs/d.md")
	write(t, root, ".physa/capabilities/b.md", "---\npromise: y\n---\n# B\n\n")
	gitIn(t, root, "init", "-q", "-b", "main")
	gitIn(t, root, "add", ".")
	gitIn(t, root, "commit", "-q", "-m", "base")
	gitIn(t, root, "checkout", "-q", "-b", "change")
	write(t, root, "docs/d.md", "changed\n")
	gitIn(t, root, "commit", "-q", "-am", "feat: change d\n\nWhy d changes.\n\nCapability: a\nLoaded: b")
	return root
}

// validResult is a session whose answer is a valid review touching a.
func validResult() adapter.Result {
	return adapter.Result{
		Text: "Looks fine.\n\n```json\n" +
			`{"version":1,"touched":["a"],"rules":[{"capability":"a","rule":"r","respected":"yes","why":"It keeps r."}],"contradictions":[],"edits":[]}` +
			"\n```\n",
		Model:  "m-1",
		Tokens: adapter.Tokens{Input: 100, CachedInput: 40, Output: 20},
	}
}

func runReview(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(append([]string{"review"}, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestReviewMarkdown(t *testing.T) {
	t.Chdir(reviewRepo(t))
	f := &fakeAdapter{result: validResult()}
	useAdapter(t, f)
	code, out, errOut := runReview(t)
	if code != 0 || errOut != "" {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	for _, want := range []string{"feat: change d", "Touched: a", "Misses: a", "Excess: b", "It keeps r."} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown lacks %q:\n%s", want, out)
		}
	}
	if !strings.HasSuffix(out, "\nTokens: 100 in (40 cached), 20 out\n") {
		t.Errorf("no tokens line at the end:\n%s", out)
	}
	if len(f.specs) != 1 || !f.specs[0].PromptOnly || !strings.Contains(f.specs[0].Prompt, "+changed") {
		t.Errorf("specs = %+v", f.specs)
	}
}

func TestReviewTokensNotReported(t *testing.T) {
	t.Chdir(reviewRepo(t))
	res := validResult()
	res.Tokens = adapter.Tokens{}
	useAdapter(t, &fakeAdapter{result: res})
	code, out, _ := runReview(t)
	if code != 0 || !strings.HasSuffix(out, "\nTokens: not reported\n") {
		t.Errorf("code %d, out:\n%s", code, out)
	}
}

func TestReviewJSON(t *testing.T) {
	t.Chdir(reviewRepo(t))
	useAdapter(t, &fakeAdapter{result: validResult()})
	code, out, errOut := runReview(t, "--json")
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	var r prcheck.Result
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if r.Range != "main..HEAD" || len(r.Commits) != 1 || r.Reviewer.Status != prcheck.ReviewerRan || len(r.Slice.Excess) != 1 {
		t.Errorf("result = %+v", r)
	}
	if strings.Contains(out, "Tokens:") {
		t.Errorf("JSON output carries a tokens line: %s", out)
	}
}

func TestReviewInvalidOutputExitsOne(t *testing.T) {
	t.Chdir(reviewRepo(t))
	useAdapter(t, &fakeAdapter{result: adapter.Result{Text: "```json\n{\"version\":1}\n```\n", Tokens: adapter.Tokens{Input: 5, Output: 2}}})
	code, out, errOut := runReview(t)
	if code != 1 || !strings.Contains(out, "Reviewer: failed (") || !strings.HasSuffix(out, "\nTokens: 5 in (0 cached), 2 out\n") {
		t.Errorf("code %d, out:\n%s", code, out)
	}
	if !strings.HasPrefix(errOut, "physa review: the reviewer failed: ") {
		t.Errorf("stderr %q", errOut)
	}
}

func TestReviewAgentErrorExitsOne(t *testing.T) {
	t.Chdir(reviewRepo(t))
	useAdapter(t, &fakeAdapter{waitErr: fmt.Errorf("%w: out of credit", adapter.ErrFailed)})
	code, out, errOut := runReview(t)
	if code != 1 || !strings.Contains(out, "out of credit") || !strings.HasSuffix(out, "\nTokens: not reported\n") {
		t.Errorf("code %d, out %q, stderr %q", code, out, errOut)
	}
}

func TestReviewNotInstalledExitsTwo(t *testing.T) {
	t.Chdir(reviewRepo(t))
	useAdapter(t, &fakeAdapter{startErr: fmt.Errorf("%w: claude", adapter.ErrNotInstalled)})
	code, out, errOut := runReview(t)
	if code != 2 || out != "" || errOut != "physa review: fake-agent is not installed\n" {
		t.Errorf("code %d, out %q, stderr %q", code, out, errOut)
	}
}

func TestReviewNoCommitsExitsTwo(t *testing.T) {
	root := reviewRepo(t)
	gitIn(t, root, "checkout", "-q", "main")
	t.Chdir(root)
	f := &fakeAdapter{result: validResult()}
	useAdapter(t, f)
	code, out, errOut := runReview(t)
	if code != 2 || out != "" || errOut != "physa review: no commits on this branch against main\n" {
		t.Errorf("code %d, out %q, stderr %q", code, out, errOut)
	}
	if len(f.specs) != 0 {
		t.Errorf("the agent started")
	}
}

func TestReviewDefaultBranchFromOrigin(t *testing.T) {
	root := reviewRepo(t)
	gitIn(t, root, "update-ref", "refs/remotes/origin/main", "main")
	gitIn(t, root, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	gitIn(t, root, "branch", "-q", "-m", "main", "trunk")
	t.Chdir(root)
	useAdapter(t, &fakeAdapter{result: validResult()})
	code, out, errOut := runReview(t, "--json")
	if code != 0 || !strings.Contains(out, `"range":"origin/main..HEAD"`) {
		t.Errorf("code %d, out %s, stderr %q", code, out, errOut)
	}
}

func TestReviewCannotRun(t *testing.T) {
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(t.TempDir()))
	t.Chdir(tempMap(t, "`r` · from docs/d.md"))
	if code, _, errOut := runReview(t); code != 2 || !strings.Contains(errOut, "not a git repository") {
		t.Errorf("not a repo: code %d, stderr %q", code, errOut)
	}
	t.Chdir(t.TempDir())
	if code, _, errOut := runReview(t); code != 2 || !strings.HasPrefix(errOut, "physa review: ") {
		t.Errorf("no map: code %d, stderr %q", code, errOut)
	}
}

func TestReviewUsage(t *testing.T) {
	for _, args := range [][]string{{"--bogus"}, {"main..HEAD"}, {"--json=1"}} {
		code, out, errOut := runReview(t, args...)
		if code != 2 || out != "" || errOut != usage {
			t.Errorf("%v: code %d, stderr %q", args, code, errOut)
		}
	}
	if !strings.Contains(usage, "physa review [--json]") || !strings.Contains(usage, "--review | --skip-review <reason>") {
		t.Errorf("usage = %q", usage)
	}
}
