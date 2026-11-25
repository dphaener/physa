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
	st, err := parse(strings.NewReader(in), nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.model != "claude-test-1" || st.id != "s" {
		t.Errorf("model = %q, session id = %q", st.model, st.id)
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
	st, err := parse(strings.NewReader(in), nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.result == nil || !st.result.IsError || st.result.Result != "limit reached" {
		t.Fatalf("result = %+v", st.result)
	}
}

func TestParseNoResult(t *testing.T) {
	st, err := parse(strings.NewReader(initLine+"\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.result != nil {
		t.Fatalf("result = %+v, want none", st.result)
	}
}

func TestParseSkipsJunk(t *testing.T) {
	in := "not json\n\n{broken\n[1,2]\n" + `{"type":"stream_event","event":{}}` + "\n" + `{"type":"result","is_error":"not a bool"}` + "\n" + okResult + "\n" + "trailing junk"
	st, err := parse(strings.NewReader(in), nil)
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
	st, err := parse(strings.NewReader(in), nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.result == nil || len(st.result.Result) != len(big) {
		t.Fatalf("long result not read")
	}
}

func TestArgs(t *testing.T) {
	base := []string{"-p", "--output-format", "stream-json", "--verbose", "--no-session-persistence"}
	if got := args(adapter.Spec{}, ""); !reflect.DeepEqual(got, base) {
		t.Errorf("args = %q, want %q", got, base)
	}
	want := append(append([]string{}, base...), "--tools", "", "--strict-mcp-config")
	if got := args(adapter.Spec{PromptOnly: true}, ""); !reflect.DeepEqual(got, want) {
		t.Errorf("prompt-only args = %q, want %q", got, want)
	}
}

func TestArgsReadOnly(t *testing.T) {
	base := []string{"-p", "--output-format", "stream-json", "--verbose", "--no-session-persistence"}
	readOnly := append(append([]string{}, base...), "--tools", "Read,Glob,Grep", "--strict-mcp-config")
	promptOnly := append(append([]string{}, base...), "--tools", "", "--strict-mcp-config")
	cases := []struct {
		name string
		spec adapter.Spec
		want []string
	}{
		{"read-only", adapter.Spec{ReadOnly: true}, readOnly},
		{"read-only and prompt-only", adapter.Spec{ReadOnly: true, PromptOnly: true}, promptOnly},
		{"prompt-only", adapter.Spec{PromptOnly: true}, promptOnly},
	}
	for _, c := range cases {
		if got := args(c.spec, ""); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: args = %q, want %q", c.name, got, c.want)
		}
	}
}

const testSessionID = "0f8e7d6c-5b4a-4392-8170-a1b2c3d4e5f6"

func TestArgsWithSessionID(t *testing.T) {
	want := []string{"-p", "--output-format", "stream-json", "--verbose", "--session-id", testSessionID}
	if got := args(adapter.Spec{SessionID: testSessionID}, ""); !reflect.DeepEqual(got, want) {
		t.Errorf("args = %q, want %q", got, want)
	}
	want = append(want, "--tools", "", "--strict-mcp-config")
	if got := args(adapter.Spec{SessionID: testSessionID, PromptOnly: true}, ""); !reflect.DeepEqual(got, want) {
		t.Errorf("prompt-only args = %q, want %q", got, want)
	}
}

func TestSessionArgvWithSessionID(t *testing.T) {
	cmd, rec := fake(t, okResult+"\n", "", 0)
	if _, err := run(t, &Adapter{Command: cmd}, adapter.Spec{Prompt: "p", SessionID: testSessionID}); err != nil {
		t.Fatal(err)
	}
	argv := strings.Split(strings.TrimSuffix(read(t, filepath.Join(rec, "argv")), "\n"), "\n")
	found := false
	for i, a := range argv {
		if a == "--no-session-persistence" {
			t.Errorf("argv %q has --no-session-persistence with a session id", argv)
		}
		if a == "--session-id" && i+1 < len(argv) && argv[i+1] == testSessionID {
			found = true
		}
	}
	if !found {
		t.Errorf("argv %q lacks --session-id %s", argv, testSessionID)
	}
}

func TestArgsResume(t *testing.T) {
	want := []string{"-p", "--output-format", "stream-json", "--verbose", "--resume", testSessionID}
	if got := args(adapter.Spec{SessionID: testSessionID, Resume: true}, ""); !reflect.DeepEqual(got, want) {
		t.Errorf("resume args = %q, want %q", got, want)
	}
	want = append(want, "--tools", "", "--strict-mcp-config")
	if got := args(adapter.Spec{SessionID: testSessionID, Resume: true, PromptOnly: true}, ""); !reflect.DeepEqual(got, want) {
		t.Errorf("prompt-only resume args = %q, want %q", got, want)
	}
}

func TestSessionArgvResume(t *testing.T) {
	cmd, rec := fake(t, okResult+"\n", "", 0)
	spec := adapter.Spec{Prompt: "next", SessionID: testSessionID, Resume: true, PromptOnly: true}
	if _, err := run(t, &Adapter{Command: cmd}, spec); err != nil {
		t.Fatal(err)
	}
	argv := strings.Split(strings.TrimSuffix(read(t, filepath.Join(rec, "argv")), "\n"), "\n")
	found := false
	for i, a := range argv {
		if a == "--session-id" {
			t.Errorf("argv %q has --session-id on a resume", argv)
		}
		if a == "--resume" && i+1 < len(argv) && argv[i+1] == testSessionID {
			found = true
		}
	}
	if !found {
		t.Errorf("argv %q lacks --resume %s", argv, testSessionID)
	}
}

func TestStartRefusesResumeWithoutSessionID(t *testing.T) {
	cmd, rec := fake(t, okResult+"\n", "", 0)
	sess, err := (&Adapter{Command: cmd}).Start(context.Background(), adapter.Spec{Prompt: "p", Resume: true})
	if err == nil || sess != nil {
		t.Fatalf("Start: session %v, err %v; want an error", sess, err)
	}
	if _, err := os.Stat(filepath.Join(rec, "argv")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the agent ran for a resume without an id (argv recorded: %v)", err)
	}
}

func TestStartRefusesBadSessionID(t *testing.T) {
	cmd, rec := fake(t, okResult+"\n", "", 0)
	for _, id := range []string{
		"not-a-uuid",
		"0f8e7d6c5b4a43928170a1b2c3d4e5f6",
		"0f8e7d6c-5b4a-4392-8170-a1b2c3d4e5f6x",
		"0f8e7d6c-5b4a-4392-8170-a1b2c3d4e5g6",
		"--resume",
		"../0f8e7d6c-5b4a-4392-8170-a1b2c3d4e5f6",
	} {
		sess, err := (&Adapter{Command: cmd}).Start(context.Background(), adapter.Spec{Prompt: "p", SessionID: id})
		if err == nil || sess != nil {
			t.Errorf("Start with id %q: session %v, err %v; want an error", id, sess, err)
		}
		if err != nil && !strings.Contains(err.Error(), "not a UUID") {
			t.Errorf("Start with id %q: err = %v", id, err)
		}
	}
	if _, err := os.Stat(filepath.Join(rec, "argv")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the agent ran for a bad id (argv recorded: %v)", err)
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
		"prev=; for a in \"$@\"; do [ \"$prev\" = --append-system-prompt-file ] && cp \"$a\" \"$d/rec/system\"; prev=$a; done\n" +
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
	want := adapter.Result{Text: "the answer", Model: "claude-test-1", SessionID: "s", Tokens: adapter.Tokens{Input: 3210, CachedInput: 3000, Output: 45}}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("result = %+v, want %+v", res, want)
	}
	if got := read(t, filepath.Join(rec, "stdin")); got != prompt {
		t.Errorf("stdin got %d bytes, want the %d-byte prompt", len(got), len(prompt))
	}
	wantArgv := strings.Join(args(adapter.Spec{PromptOnly: true}, ""), "\n") + "\n"
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
	want := strings.Join(args(adapter.Spec{}, ""), "\n") + "\n"
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

const (
	asstA1 = `{"type":"assistant","message":{"id":"m1","usage":{"input_tokens":1,"cache_creation_input_tokens":2,"cache_read_input_tokens":3,"output_tokens":4}}}`
	asstA2 = `{"type":"assistant","message":{"id":"m1","usage":{"input_tokens":1,"cache_creation_input_tokens":2,"cache_read_input_tokens":3,"output_tokens":10}}}`
	asstB  = `{"type":"assistant","message":{"id":"m2","usage":{"input_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":7}}}`
)

func TestParseUsageRunningSums(t *testing.T) {
	in := initLine + "\n" + asstA1 + "\n" + asstA2 + "\n" + asstB + "\n" + okResult + "\n"
	var calls []adapter.Tokens
	st, err := parse(strings.NewReader(in), func(tk adapter.Tokens) { calls = append(calls, tk) })
	if err != nil {
		t.Fatal(err)
	}
	want := []adapter.Tokens{
		{Input: 6, CachedInput: 3, Output: 4},
		{Input: 6, CachedInput: 3, Output: 10},
		{Input: 11, CachedInput: 3, Output: 17},
		st.result.Usage.tokens(),
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %+v, want %+v", calls, want)
	}
	if want[3] != (adapter.Tokens{Input: 3210, CachedInput: 3000, Output: 45}) {
		t.Errorf("last call = %+v", want[3])
	}
}

func TestParseUsageSkipsAssistantWithoutUsage(t *testing.T) {
	in := `{"type":"assistant","message":{"id":"m1"}}` + "\n" +
		`{"type":"assistant","message":"junk"}` + "\n" +
		`{"type":"assistant","message":{"id":"m1","usage":"junk"}}` + "\n" +
		`{"type":"assistant"` + "\n" + asstB + "\n"
	var calls []adapter.Tokens
	_, err := parse(strings.NewReader(in), func(tk adapter.Tokens) { calls = append(calls, tk) })
	if err != nil {
		t.Fatal(err)
	}
	if want := []adapter.Tokens{{Input: 5, Output: 7}}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %+v, want %+v", calls, want)
	}
}

func TestParseUsageNilCallback(t *testing.T) {
	st, err := parse(strings.NewReader(asstA1+"\n"+okResult+"\n"), nil)
	if err != nil || st.result == nil || st.result.Result != "the answer" {
		t.Fatalf("st = %+v, err = %v", st, err)
	}
}

func TestSessionUsageCallbackMatchesResult(t *testing.T) {
	cmd, _ := fake(t, asstA1+"\n"+okResult+"\n", "", 0)
	var last adapter.Tokens
	res, err := run(t, &Adapter{Command: cmd}, adapter.Spec{Prompt: "p", Usage: func(tk adapter.Tokens) { last = tk }})
	if err != nil {
		t.Fatal(err)
	}
	if last != res.Tokens {
		t.Errorf("last usage = %+v, result tokens = %+v", last, res.Tokens)
	}
}

func TestSessionUsageCancelAtLimit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake agent is a shell script")
	}
	dir := t.TempDir()
	write(t, filepath.Join(dir, "stream"), asstA1+"\n"+asstB+"\n")
	cmd := filepath.Join(dir, "claude")
	script := "#!/bin/sh\ncat > /dev/null\ncat '" + dir + "/stream'\nexec sleep 60\n"
	if err := os.WriteFile(cmd, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const limit = 10
	spec := adapter.Spec{Prompt: "p", Usage: func(tk adapter.Tokens) {
		if tk.Input+tk.Output > limit {
			cancel()
		}
	}}
	sess, err := (&Adapter{Command: cmd}).Start(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = sess.Wait()
	if !errors.Is(err, adapter.ErrFailed) || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want ErrFailed and context.Canceled", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("Wait took %v after cancel", d)
	}
}

// costResult is a result line with modelUsage as Claude Code writes it: one
// model with its cache writes split, one without the split.
const costResult = `{"type":"result","subtype":"success","is_error":false,"result":"ok","total_cost_usd":0.123457,` +
	`"usage":{"input_tokens":1,"output_tokens":1},"modelUsage":{` +
	`"claude-opus-5-5[1m]":{"inputTokens":100,"outputTokens":20,"cacheReadInputTokens":3000,"cacheCreationInputTokens":500,` +
	`"cacheCreationEphemeral5mInputTokens":200,"cacheCreationEphemeral1hInputTokens":300,"costUSD":0.1234565,"canonicalModel":"claude-opus-5-5"},` +
	`"claude-haiku-4-5":{"inputTokens":7,"outputTokens":3,"cacheReadInputTokens":0,"cacheCreationInputTokens":40,"costUSD":0.000001}}}`

func TestParseCostPerModel(t *testing.T) {
	st, err := parse(strings.NewReader(initLine+"\n"+costResult+"\n"), nil)
	if err != nil || st.result == nil {
		t.Fatalf("result %+v, err %v", st.result, err)
	}
	want := adapter.Cost{
		{Model: "claude-haiku-4-5", Input: 7, CacheWrite5m: 40, Output: 3, MicroUSD: 1},
		{Model: "claude-opus-5-5", Input: 100, CacheRead: 3000, CacheWrite5m: 200, CacheWrite1h: 300, Output: 20, MicroUSD: 123457},
	}
	if got := st.result.cost(); !reflect.DeepEqual(got, want) {
		t.Errorf("cost = %+v\nwant   %+v", got, want)
	}
	if got := want[1].Raw(); got != 3620 {
		t.Errorf("Raw = %d, want 3620", got)
	}
}

func TestParseNoModelUsageNoCost(t *testing.T) {
	st, err := parse(strings.NewReader(okResult+"\n"), nil)
	if err != nil || st.result == nil || st.result.cost() != nil {
		t.Fatalf("cost of a result without modelUsage = %+v, err %v", st.result.cost(), err)
	}
}

func TestWindow(t *testing.T) {
	if got := adapter.Window(New()); got != 1_000_000 {
		t.Errorf("Window = %d, want 1000000", got)
	}
}

func TestSessionTooLong(t *testing.T) {
	stream := initLine + "\n" + `{"type":"result","subtype":"success","is_error":true,"result":"Prompt is too long","usage":{"input_tokens":4,"output_tokens":1}}` + "\n"
	cmd, _ := fake(t, stream, "", 1)
	_, err := run(t, &Adapter{Command: cmd}, adapter.Spec{Prompt: "p"})
	if !errors.Is(err, adapter.ErrFailed) || !errors.Is(err, adapter.ErrTooLong) {
		t.Fatalf("err = %v, want ErrFailed and ErrTooLong", err)
	}
	if !strings.Contains(err.Error(), "Prompt is too long") {
		t.Errorf("err = %q, want the agent's message kept", err)
	}
}

func TestSessionTooLongOnStderr(t *testing.T) {
	cmd, _ := fake(t, "", "Error: prompt is too long: 1200000 tokens", 1)
	_, err := run(t, &Adapter{Command: cmd}, adapter.Spec{Prompt: "p"})
	if !errors.Is(err, adapter.ErrFailed) || !errors.Is(err, adapter.ErrTooLong) {
		t.Fatalf("err = %v, want ErrFailed and ErrTooLong", err)
	}
}

func TestSessionOtherFailureIsNotTooLong(t *testing.T) {
	stream := initLine + "\n" + `{"type":"result","subtype":"success","is_error":true,"result":"rate limited","usage":{"input_tokens":4,"output_tokens":1}}` + "\n"
	cmd, _ := fake(t, stream, "", 1)
	_, err := run(t, &Adapter{Command: cmd}, adapter.Spec{Prompt: "p"})
	if !errors.Is(err, adapter.ErrFailed) {
		t.Fatalf("err = %v, want ErrFailed", err)
	}
	if errors.Is(err, adapter.ErrTooLong) {
		t.Fatalf("err = %v, must not be ErrTooLong", err)
	}
}

func TestSessionSystemGoesInAFileThatIsRemoved(t *testing.T) {
	cmd, rec := fake(t, okResult+"\n", "", 0)
	system := "the docs every capability reads " + strings.Repeat("s", 300<<10)
	if _, err := run(t, &Adapter{Command: cmd}, adapter.Spec{Prompt: "p", System: system, PromptOnly: true}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(rec, "system")); got != system {
		t.Fatalf("system file held %d bytes, want the %d-byte system text", len(got), len(system))
	}
	argv := strings.Split(strings.TrimSpace(read(t, filepath.Join(rec, "argv"))), "\n")
	file := ""
	for i, a := range argv {
		if a == "--append-system-prompt-file" && i+1 < len(argv) {
			file = argv[i+1]
		}
	}
	if file == "" {
		t.Fatalf("argv %q has no --append-system-prompt-file", argv)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("system file %s left behind: %v", file, err)
	}
}

func TestResumeSendsNoSystem(t *testing.T) {
	cmd, rec := fake(t, okResult+"\n", "", 0)
	if _, err := run(t, &Adapter{Command: cmd}, adapter.Spec{Prompt: "p", System: "s", SessionID: testSessionID, Resume: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(t, filepath.Join(rec, "argv")), "--append-system-prompt-file") {
		t.Error("a resume resent the system text")
	}
}
