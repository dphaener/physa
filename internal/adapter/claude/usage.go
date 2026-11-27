package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os/exec"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/adapter"
)

var _ adapter.UsageReader = (*Adapter)(nil)

// Usage reads the plan's usage with claude -p /usage, run without
// ANTHROPIC_API_KEY, since /usage reports the login's plan. It needs a
// Claude Code that prints a usage_report; the text it also prints is never
// read.
func (a *Adapter) Usage(ctx context.Context) (adapter.Usage, error) {
	if len(a.Env) == 0 {
		return adapter.Usage{}, errNoEnv
	}
	command := a.Command
	if command == "" {
		command = "claude"
	}
	path, err := exec.LookPath(command)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			return adapter.Usage{}, fmt.Errorf("%w: %s: %v", adapter.ErrNotInstalled, command, err)
		}
		return adapter.Usage{}, err
	}
	cmd := exec.CommandContext(ctx, path, "-p", "/usage", "--output-format", "stream-json", "--verbose")
	cmd.Env = []string{} // never nil, which would hand claude this process's environment
	for _, e := range a.Env {
		if !strings.HasPrefix(e, apiKey+"=") {
			cmd.Env = append(cmd.Env, e)
		}
	}
	var out bytes.Buffer
	tail := &tailWriter{max: stderrTail}
	cmd.Stdout, cmd.Stderr = &out, tail
	cmd.WaitDelay = waitDelay
	if err := cmd.Run(); err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return adapter.Usage{}, cerr
		}
		msg := strings.TrimSpace(tail.String())
		if msg == "" {
			msg = "no message on stderr"
		}
		return adapter.Usage{}, fmt.Errorf("%w: reading the plan's usage: %v: %s", adapter.ErrFailed, err, msg)
	}
	return parseUsage(&out)
}

// usageLine is the stream line that carries the plan's usage.
type usageLine struct {
	Report *struct {
		RateLimits struct {
			Limits []struct {
				Kind     string    `json:"kind"`
				Percent  float64   `json:"percent"`
				ResetsAt time.Time `json:"resets_at"`
				Active   bool      `json:"is_active"`
			} `json:"limits"`
		} `json:"rate_limits"`
	} `json:"usage_report"`
}

// parseUsage reads the first usage_report in a stream. A stream without one
// gives an error wrapping adapter.ErrNoUsage.
func parseUsage(r io.Reader) (adapter.Usage, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	for sc.Scan() {
		b := bytes.TrimSpace(sc.Bytes())
		if len(b) == 0 || b[0] != '{' {
			continue
		}
		var l usageLine
		if err := json.Unmarshal(b, &l); err != nil || l.Report == nil {
			continue
		}
		var u adapter.Usage
		for _, x := range l.Report.RateLimits.Limits {
			u.Limits = append(u.Limits, adapter.Limit{Kind: x.Kind, Percent: x.Percent, ResetsAt: x.ResetsAt, Active: x.Active})
		}
		return u, nil
	}
	if err := sc.Err(); err != nil {
		return adapter.Usage{}, fmt.Errorf("%w: reading the stream: %v", adapter.ErrFailed, err)
	}
	return adapter.Usage{}, fmt.Errorf("%w: claude printed no usage_report; update Claude Code", adapter.ErrNoUsage)
}
