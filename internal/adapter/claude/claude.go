// Package claude is the Claude Code adapter: it runs claude -p with streamed
// JSON output and reads the result and tokens from the stream.
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
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/adapter"
)

// Name is the agent's name as Physa records it.
const Name = "claude-code"

// maxLine bounds one line of the stream; a result can be large.
const maxLine = 64 << 20

// stderrTail is how much of the end of stderr a failure quotes.
const stderrTail = 2 << 10

// waitDelay is how long a cancelled session's pipes may stay open after its
// process is killed.
const waitDelay = 5 * time.Second

// Adapter runs sessions with the Claude Code command.
type Adapter struct {
	// Command is the claude command to run; empty means "claude" on PATH.
	Command string
}

var _ adapter.Adapter = (*Adapter)(nil)

// New returns an adapter that runs "claude" from PATH.
func New() *Adapter { return &Adapter{Command: "claude"} }

// Name returns "claude-code".
func (a *Adapter) Name() string { return Name }

// args is the argv after the command for a session of s.
func args(s adapter.Spec) []string {
	argv := []string{"-p", "--output-format", "stream-json", "--verbose", "--no-session-persistence"}
	if s.PromptOnly {
		argv = append(argv, "--tools", "", "--strict-mcp-config")
	}
	return argv
}

// Start runs claude -p in s.Dir with the prompt on stdin.
func (a *Adapter) Start(ctx context.Context, s adapter.Spec) (adapter.Session, error) {
	command := a.Command
	if command == "" {
		command = "claude"
	}
	path, err := exec.LookPath(command)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s: %v", adapter.ErrNotInstalled, command, err)
		}
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, args(s)...)
	cmd.Dir = s.Dir
	cmd.Env = append(os.Environ(), s.Env...)
	cmd.Stdin = strings.NewReader(s.Prompt)
	cmd.Cancel = func() error { return cmd.Process.Kill() }
	cmd.WaitDelay = waitDelay
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	tail := &tailWriter{max: stderrTail}
	cmd.Stderr = tail
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s: %w", command, err)
	}
	done := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		pw.Close()
		done <- err
	}()
	return &session{ctx: ctx, out: pr, done: done, stderr: tail, usage: s.Usage}, nil
}

// session is one running claude -p process.
type session struct {
	ctx    context.Context
	out    *io.PipeReader
	done   chan error
	stderr *tailWriter
	usage  func(adapter.Tokens)
}

// Wait reads the stream to its end, then waits for the process.
func (s *session) Wait() (adapter.Result, error) {
	st, readErr := parse(s.out, s.usage)
	s.out.Close()
	exitErr := <-s.done
	res := adapter.Result{Model: st.model}
	if st.result != nil {
		res.Text = st.result.Result
		res.Tokens = st.result.Usage.tokens()
	}
	failed := readErr != nil || st.result == nil || st.result.IsError || exitErr != nil
	if !failed {
		return res, nil
	}
	if err := s.ctx.Err(); err != nil {
		return res, fmt.Errorf("%w: %w", adapter.ErrFailed, err)
	}
	if readErr != nil {
		return res, fmt.Errorf("%w: reading the stream: %v", adapter.ErrFailed, readErr)
	}
	return res, fmt.Errorf("%w: %s", adapter.ErrFailed, s.message(st.result, exitErr))
}

// message is the agent's own account of a failure: the exit status when there
// is one, then the result's text, else the end of stderr, else what the stream
// said.
func (s *session) message(r *resultLine, exitErr error) string {
	var parts []string
	if exitErr != nil {
		parts = append(parts, exitErr.Error())
	}
	stderr := strings.TrimSpace(s.stderr.String())
	switch {
	case r != nil && strings.TrimSpace(r.Result) != "":
		parts = append(parts, strings.TrimSpace(r.Result))
	case stderr != "":
		parts = append(parts, stderr)
	case r == nil:
		parts = append(parts, "no result in the stream")
	case r.Subtype != "":
		parts = append(parts, r.Subtype)
	default:
		parts = append(parts, "error result")
	}
	return strings.Join(parts, ": ")
}

// stream is what Physa reads from one session's streamed JSON.
type stream struct {
	model  string
	result *resultLine
}

// line is the part of a stream line that says what it is.
type line struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Model   string `json:"model"`
}

// resultLine is the stream's final line.
type resultLine struct {
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
	Usage   usage  `json:"usage"`
}

// usage is the result line's token counts.
type usage struct {
	InputTokens              int64 `json:"input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
}

func (u usage) tokens() adapter.Tokens {
	return adapter.Tokens{
		Input:       u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens,
		CachedInput: u.CacheReadInputTokens,
		Output:      u.OutputTokens,
	}
}

// assistantLine is a stream line for one message of the agent; a message can
// appear on several lines, each with the usage so far.
type assistantLine struct {
	Message struct {
		ID    string `json:"id"`
		Usage *usage `json:"usage"`
	} `json:"message"`
}

// sum is the tokens of all messages, the last usage seen for each.
func sum(byID map[string]usage) adapter.Tokens {
	var total usage
	for _, u := range byID {
		total.InputTokens += u.InputTokens
		total.CacheCreationInputTokens += u.CacheCreationInputTokens
		total.CacheReadInputTokens += u.CacheReadInputTokens
		total.OutputTokens += u.OutputTokens
	}
	return total.tokens()
}

// parse reads a stream to its end. Lines that are not JSON, and lines of
// types it does not read, are skipped; the last result line wins. After each
// assistant line that carries usage, and on the result line, it calls onUsage
// (when not nil) with the tokens so far.
func parse(r io.Reader, onUsage func(adapter.Tokens)) (stream, error) {
	var st stream
	byID := map[string]usage{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	for sc.Scan() {
		b := bytes.TrimSpace(sc.Bytes())
		if len(b) == 0 || b[0] != '{' {
			continue
		}
		var l line
		if json.Unmarshal(b, &l) != nil {
			continue
		}
		switch {
		case l.Type == "system" && l.Subtype == "init":
			st.model = l.Model
		case l.Type == "assistant":
			var a assistantLine
			if json.Unmarshal(b, &a) == nil && a.Message.Usage != nil {
				byID[a.Message.ID] = *a.Message.Usage
				if onUsage != nil {
					onUsage(sum(byID))
				}
			}
		case l.Type == "result":
			var res resultLine
			if json.Unmarshal(b, &res) == nil {
				st.result = &res
				if onUsage != nil {
					onUsage(res.Usage.tokens())
				}
			}
		}
	}
	return st, sc.Err()
}

// tailWriter keeps the last max bytes written to it.
type tailWriter struct {
	max int
	buf []byte
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	if over := len(w.buf) - w.max; over > 0 {
		w.buf = append(w.buf[:0], w.buf[over:]...)
	}
	return len(p), nil
}

func (w *tailWriter) String() string { return string(w.buf) }
