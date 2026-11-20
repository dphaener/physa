package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
)

const initLine = `{"type":"system","subtype":"init","cwd":"/x","session_id":"s","model":"claude-test-1","tools":[]}`

const okResult = `{"type":"result","subtype":"success","is_error":false,"result":"the answer","usage":{"input_tokens":10,"cache_creation_input_tokens":200,"cache_read_input_tokens":3000,"output_tokens":45}}`

func TestParseSuccessWithUsage(t *testing.T) {
	in := initLine + "\n" + `{"type":"assistant","message":{"content":[]}}` + "\n" + okResult + "\n"
	st, err := parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if st.model != "claude-test-1" {
		t.Errorf("model = %q", st.model)
	}
	if st.result == nil || st.result.Result != "the answer" || st.result.IsError {
		t.Fatalf("result = %+v", st.result)
	}
	got := st.result.Usage.tokens()
	want := adapter.Tokens{Input: 10 + 200 + 3000, CachedInput: 3000, Output: 45}
	if got != want {
		t.Errorf("tokens = %+v, want %+v", got, want)
	}
}

func TestParseErrorResult(t *testing.T) {
	in := `{"type":"result","subtype":"error_during_execution","is_error":true,"result":"limit reached"}`
	st, err := parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if st.result == nil || !st.result.IsError || st.result.Result != "limit reached" {
		t.Fatalf("result = %+v", st.result)
	}
}

func TestParseNoResult(t *testing.T) {
	st, err := parse(strings.NewReader(initLine + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if st.result != nil {
		t.Fatalf("result = %+v, want none", st.result)
	}
}

func TestParseSkipsJunk(t *testing.T) {
	in := "not json\n\n{broken\n[1,2]\n" + `{"type":"stream_event","event":{}}` + "\n" + `{"type":"result","is_error":"not a bool"}` + "\n" + okResult + "\n" + "trailing junk"
	st, err := parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if st.result == nil || st.result.Result != "the answer" {
		t.Fatalf("result = %+v", st.result)
	}
}

func TestParseVeryLongLine(t *testing.T) {
	big := strings.Repeat("x", 17<<20)
	in := initLine + "\n" + `{"type":"result","subtype":"success","is_error":false,"result":"` + big + `"}` + "\n"
	st, err := parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if st.result == nil || len(st.result.Result) != len(big) {
		t.Fatalf("long result not read")
	}
}

func TestArgs(t *testing.T) {
	base := []string{"-p", "--output-format", "stream-json", "--verbose", "--no-session-persistence"}
	if got := args(adapter.Spec{}); !reflect.DeepEqual(got, base) {
		t.Errorf("args = %q, want %q", got, base)
	}
	want := append(append([]string{}, base...), "--tools", "", "--strict-mcp-config")
	if got := args(adapter.Spec{PromptOnly: true}); !reflect.DeepEqual(got, want) {
		t.Errorf("prompt-only args = %q, want %q", got, want)
	}
}

func TestName(t *testing.T) {
	if New().Name() != "claude-code" || New().Command != "claude" {
		t.Fatalf("New() = %+v, Name %q", New(), New().Name())
	}
}

// fake writes a stand-in claude command into a temp dir. It records its argv
// (one per line), stdin, working directory and $PHYSA_FAKE_ENV into rec, then
// prints the file stream to stdout, the file stderr to stderr, and exits with
// code. It returns the command path and the record dir.
func fake(t *testing.T, stream, stderr string, code int) (string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake agent is a shell script")
	}
	dir := t.TempDir()
	rec := filepath.Join(dir, "rec")
	if err := os.Mkdir(rec, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "stream"), stream)
	write(t, filepath.Join(dir, "stderr"), stderr)
	script := "#!/bin/sh\n" +
		"d='" + dir + "'\n" +
		"printf '%s\\n' \"$@\" > \"$d/rec/argv\"\n" +
		"cat > \"$d/rec/stdin\"\n" +
		"pwd > \"$d/rec/cwd\"\n" +
		"printf '%s' \"$PHYSA_FAKE_ENV\" > \"$d/rec/env\"\n" +
		"cat \"$d/stream\"\n" +
		"cat \"$d/stderr\" >&2\n" +
		"exit " + strconv.Itoa(code) + "\n"
	cmd := filepath.Join(dir, "claude")
	if err := os.WriteFile(cmd, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return cmd, rec
}

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func run(t *testing.T, a *Adapter, s adapter.Spec) (adapter.Result, error) {
	t.Helper()
	sess, err := a.Start(context.Background(), s)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return sess.Wait()
}

func TestSessionSuccess(t *testing.T) {
	cmd, rec := fake(t, initLine+"\n"+okResult+"\n", "", 0)
	work := t.TempDir()
	prompt := "review this\nwith a second line " + strings.Repeat("p", 300<<10)
	res, err := run(t, &Adapter{Command: cmd}, adapter.Spec{
		Prompt:     prompt,
		Dir:        work,
		Env:        []string{"PHYSA_FAKE_ENV=added"},
		PromptOnly: true,
	})
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	want := adapter.Result{Text: "the answer", Model: "claude-test-1", Tokens: adapter.Tokens{Input: 3210, CachedInput: 3000, Output: 45}}
	if res != want {
		t.Errorf("result = %+v, want %+v", res, want)
	}
	if got := read(t, filepath.Join(rec, "stdin")); got != prompt {
		t.Errorf("stdin got %d bytes, want the %d-byte prompt", len(got), len(prompt))
	}
	wantArgv := strings.Join(args(adapter.Spec{PromptOnly: true}), "\n") + "\n"
	if got := read(t, filepath.Join(rec, "argv")); got != wantArgv {
		t.Errorf("argv = %q, want %q", got, wantArgv)
	}
	if got := read(t, filepath.Join(rec, "env")); got != "added" {
		t.Errorf("env = %q, want added", got)
	}
	gotDir, _ := filepath.EvalSymlinks(strings.TrimSpace(read(t, filepath.Join(rec, "cwd"))))
	wantDir, _ := filepath.EvalSymlinks(work)
	if gotDir != wantDir {
		t.Errorf("cwd = %q, want %q", gotDir, wantDir)
	}
}

func TestSessionArgvWithoutPromptOnly(t *testing.T) {
	cmd, rec := fake(t, okResult+"\n", "", 0)
	if _, err := run(t, &Adapter{Command: cmd}, adapter.Spec{Prompt: "p"}); err != nil {
		t.Fatal(err)
	}
	want := strings.Join(args(adapter.Spec{}), "\n") + "\n"
	if got := read(t, filepath.Join(rec, "argv")); got != want {
		t.Errorf("argv = %q, want %q", got, want)
	}
	if strings.Contains(read(t, filepath.Join(rec, "argv")), "--tools") {
		t.Error("--tools passed without PromptOnly")
	}
}

func TestSessionEnvOverrides(t *testing.T) {
	t.Setenv("PHYSA_FAKE_ENV", "inherited")
	cmd, rec := fake(t, okResult+"\n", "", 0)
	if _, err := run(t, &Adapter{Command: cmd}, adapter.Spec{Prompt: "p"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(rec, "env")); got != "inherited" {
		t.Errorf("inherited env = %q", got)
	}
	if _, err := run(t, &Adapter{Command: cmd}, adapter.Spec{Prompt: "p", Env: []string{"PHYSA_FAKE_ENV=mine"}}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(rec, "env")); got != "mine" {
		t.Errorf("added env = %q, want mine", got)
	}
}

func TestSessionErrorResult(t *testing.T) {
	stream := initLine + "\n" + `{"type":"result","subtype":"success","is_error":true,"result":"Credit balance is too low","usage":{"input_tokens":4,"output_tokens":1}}` + "\n"
	cmd, _ := fake(t, stream, "", 1)
	res, err := run(t, &Adapter{Command: cmd}, adapter.Spec{Prompt: "p"})
	if !errors.Is(err, adapter.ErrFailed) {
		t.Fatalf("err = %v, want ErrFailed", err)
	}
	if !strings.Contains(err.Error(), "Credit balance is too low") || !strings.Contains(err.Error(), "exit status 1") {
		t.Errorf("err = %q, want the agent's message and exit status", err)
	}
	if res.Model != "claude-test-1" || res.Tokens.Input != 4 || res.Tokens.Output != 1 {
		t.Errorf("failed result keeps what was reported: %+v", res)
	}
}

func TestSessionErrorResultExitZero(t *testing.T) {
	cmd, _ := fake(t, `{"type":"result","subtype":"error_max_turns","is_error":true}`+"\n", "", 0)
	_, err := run(t, &Adapter{Command: cmd}, adapter.Spec{Prompt: "p"})
	if !errors.Is(err, adapter.ErrFailed) || !strings.Contains(err.Error(), "error_max_turns") {
		t.Fatalf("err = %v, want ErrFailed naming the subtype", err)
	}
}

func TestSessionNoResultUsesStderr(t *testing.T) {
	cmd, _ := fake(t, initLine+"\n", strings.Repeat("noise ", 1000)+"Invalid API key", 1)
	_, err := run(t, &Adapter{Command: cmd}, adapter.Spec{Prompt: "p"})
	if !errors.Is(err, adapter.ErrFailed) || !strings.Contains(err.Error(), "Invalid API key") {
		t.Fatalf("err = %v, want ErrFailed with the end of stderr", err)
	}
	if len(err.Error()) > stderrTail+200 {
		t.Errorf("message is %d bytes; stderr should be cut to its last %d", len(err.Error()), stderrTail)
	}
}

func TestSessionNoResultExitZero(t *testing.T) {
	cmd, _ := fake(t, initLine+"\n", "", 0)
	_, err := run(t, &Adapter{Command: cmd}, adapter.Spec{Prompt: "p"})
	if !errors.Is(err, adapter.ErrFailed) || !strings.Contains(err.Error(), "no result") {
		t.Fatalf("err = %v, want ErrFailed saying no result", err)
	}
}

func TestSessionGoodResultNonZeroExit(t *testing.T) {
	cmd, _ := fake(t, okResult+"\n", "", 3)
	_, err := run(t, &Adapter{Command: cmd}, adapter.Spec{Prompt: "p"})
	if !errors.Is(err, adapter.ErrFailed) || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("err = %v, want ErrFailed with exit status", err)
	}
}

func TestSessionCancel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake agent is a shell script")
	}
	dir := t.TempDir()
	cmd := filepath.Join(dir, "claude")
	if err := os.WriteFile(cmd, []byte("#!/bin/sh\ncat > /dev/null\nexec sleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	sess, err := (&Adapter{Command: cmd}).Start(ctx, adapter.Spec{Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	start := time.Now()
	_, err = sess.Wait()
	if !errors.Is(err, context.Canceled) || !errors.Is(err, adapter.ErrFailed) {
		t.Fatalf("err = %v, want ErrFailed and context.Canceled", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("Wait took %v after cancel", d)
	}
}

func TestStartNotInstalled(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-claude")
	_, err := (&Adapter{Command: missing}).Start(context.Background(), adapter.Spec{Prompt: "p"})
	if !errors.Is(err, adapter.ErrNotInstalled) {
		t.Fatalf("path: err = %v, want ErrNotInstalled", err)
	}
	t.Setenv("PATH", t.TempDir())
	_, err = (&Adapter{Command: "physa-no-such-agent"}).Start(context.Background(), adapter.Spec{Prompt: "p"})
	if !errors.Is(err, adapter.ErrNotInstalled) {
		t.Fatalf("PATH: err = %v, want ErrNotInstalled", err)
	}
	_, err = (&Adapter{}).Start(context.Background(), adapter.Spec{Prompt: "p"})
	if !errors.Is(err, adapter.ErrNotInstalled) {
		t.Fatalf("default command with empty PATH: err = %v, want ErrNotInstalled", err)
	}
}

func TestTailWriter(t *testing.T) {
	w := &tailWriter{max: 4}
	w.Write([]byte("ab"))
	w.Write([]byte("cdef"))
	if w.String() != "cdef" {
		t.Fatalf("tail = %q", w.String())
	}
}
