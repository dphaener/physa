package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
)

func TestParseUsageRecordedReport(t *testing.T) {
	t.Parallel()
	f, err := os.Open("testdata/usage_report.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	u, err := parseUsage(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Limits) != 2 {
		t.Fatalf("limits = %+v", u.Limits)
	}
	all, scoped := u.Limits[0], u.Limits[1]
	reset := time.Date(2026, 11, 30, 14, 0, 0, 393612000, time.UTC)
	if all.Kind != "weekly_all" || all.Percent != 59 || !all.Active || !all.ResetsAt.Equal(reset) {
		t.Errorf("weekly_all = %+v", all)
	}
	if scoped.Kind != "weekly_scoped" || scoped.Percent != 3 || scoped.Active {
		t.Errorf("weekly_scoped = %+v", scoped)
	}
	if b, ok := u.Binding(); !ok || b.Kind != "weekly_all" {
		t.Errorf("Binding = %+v, %v", b, ok)
	}
}

func TestParseUsageOldTextOnlyIsErrNoUsage(t *testing.T) {
	t.Parallel()
	in := initLine + "\n" + `{"type":"result","subtype":"success","is_error":false,"result":"Current week (all models): 59% used · resets Nov 30, 6am"}` + "\n"
	_, err := parseUsage(strings.NewReader(in))
	if !errors.Is(err, adapter.ErrNoUsage) || !strings.Contains(err.Error(), "usage_report") {
		t.Fatalf("err = %v; want ErrNoUsage naming usage_report", err)
	}
}

func TestUsageRunsUsageWithoutAPIKey(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the fake agent is a shell script")
	}
	dir := t.TempDir()
	report, err := os.ReadFile("testdata/usage_report.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "stream"), string(report))
	script := "#!/bin/sh\nd='" + dir + "'\nprintf '%s\\n' \"$@\" > \"$d/argv\"\nenv > \"$d/env\"\ncat > \"$d/stdin\"\ncat \"$d/stream\"\n"
	cmd := filepath.Join(dir, "claude")
	write(t, cmd, script)
	if err := os.Chmod(cmd, 0o755); err != nil {
		t.Fatal(err)
	}
	u, err := (&Adapter{Command: cmd, Env: testEnv("ANTHROPIC_API_KEY=sk-secret", "PHYSA_KEEP=yes")}).Usage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := u.Binding(); !ok || b.Percent != 59 {
		t.Errorf("Binding = %+v, %v", b, ok)
	}
	if got := read(t, filepath.Join(dir, "argv")); got != "-p\n/usage\n--output-format\nstream-json\n--verbose\n" {
		t.Errorf("argv = %q", got)
	}
	env := read(t, filepath.Join(dir, "env"))
	if strings.Contains(env, "ANTHROPIC_API_KEY") {
		t.Error("the child's environment has ANTHROPIC_API_KEY")
	}
	if !strings.Contains(env, "PHYSA_KEEP=yes") {
		t.Error("the child's environment lost the rest of the adapter's")
	}
	if strings.HasPrefix(env, "HOME=") || strings.Contains(env, "\nHOME=") {
		t.Error("the child's environment has HOME, which only this process's has")
	}
	if got := read(t, filepath.Join(dir, "stdin")); got != "" {
		t.Errorf("stdin = %q; want none", got)
	}
}

func TestUsageFailureAndMissingCommand(t *testing.T) {
	t.Parallel()
	cmd, _ := fake(t, "", "boom happened", 3)
	_, err := (&Adapter{Command: cmd, Env: testEnv()}).Usage(context.Background())
	if !errors.Is(err, adapter.ErrFailed) || !strings.Contains(err.Error(), "boom happened") {
		t.Errorf("err = %v; want ErrFailed with stderr", err)
	}
	_, err = (&Adapter{Command: "/no/such/claude-x", Env: testEnv()}).Usage(context.Background())
	if !errors.Is(err, adapter.ErrNotInstalled) {
		t.Errorf("err = %v; want ErrNotInstalled", err)
	}
}
