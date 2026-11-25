// Package claude is the Claude Code adapter: it starts sessions with claude -p
// and streamed JSON output, optionally under an id assigned by whoever starts
// them (--session-id, so the session keeps its log) or continuing one
// (--resume <id>, which works with -p, as claude --help shows, in the same
// directory the session ran in), and reads the result, the
// tokens and what the session cost from the stream. Its sessions' logs are
// read by claudelog.
package claude

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"regexp"
	"slices"
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

// window is the most estimated tokens one session holds, the owner's choice of
// 2026-11-24: a propose prompt of about 670k estimated tokens ran in one
// session, and one of about 1.2M was refused as too long.
const window = 1_000_000

// Adapter runs sessions with the Claude Code command.
type Adapter struct {
	// Command is the claude command to run; empty means "claude" on PATH.
	Command string
}

var _ adapter.Adapter = (*Adapter)(nil)

// New returns an adapter that runs "claude" from PATH.
func New() *Adapter { return &Adapter{Command: "claude"} }

// Window states 1,000,000 estimated tokens for one session.
func (a *Adapter) Window() int { return window }

// Name returns "claude-code".
func (a *Adapter) Name() string { return Name }

// uuid is the form Claude Code takes a session id in: 8-4-4-4-12 hex.
var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// args is the argv after the command for a session of s, with system the
// file holding s.System, or empty for none. A session with an
// assigned id keeps its log, so it can be read later; one without leaves none.
// A resumed session continues the one under SessionID.
func args(s adapter.Spec, system string) []string {
	argv := []string{"-p", "--output-format", "stream-json", "--verbose"}
	if system != "" {
		argv = append(argv, "--append-system-prompt-file", system)
	}
	if s.Resume {
		argv = append(argv, "--resume", s.SessionID)
	} else if s.SessionID != "" {
		argv = append(argv, "--session-id", s.SessionID)
	} else {
		argv = append(argv, "--no-session-persistence")
	}
	if s.PromptOnly || s.ReadOnly {
		tools := map[bool]string{true: "", false: "Read,Glob,Grep"}[s.PromptOnly]
		argv = append(argv, "--tools", tools, "--strict-mcp-config")
	}
	return argv
}

// Start runs claude -p in s.Dir with the prompt on stdin. An assigned session
// id that is not a UUID, and a resume without one, are refused before anything
// runs.
func (a *Adapter) Start(ctx context.Context, s adapter.Spec) (adapter.Session, error) {
	if s.Resume && s.SessionID == "" {
		return nil, errors.New("resuming a session needs its session id")
	}
	if s.SessionID != "" && !uuid.MatchString(s.SessionID) {
		return nil, fmt.Errorf("session id %q is not a UUID", s.SessionID)
	}
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
	// System goes in a file, since one argument holding a repo's docs would
	// pass the kernel's limit on one argument's length.
	system := ""
	if s.System != "" && !s.Resume {
		f, err := os.CreateTemp("", "physa-system-*.txt")
		if err != nil {
			return nil, err
		}
		system = f.Name()
		_, err = f.WriteString(s.System)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(system)
			return nil, err
		}
	}
	cmd := exec.CommandContext(ctx, path, args(s, system)...)
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
		if system != "" {
			os.Remove(system)
		}
		return nil, fmt.Errorf("starting %s: %w", command, err)
	}
	done := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		if system != "" {
			os.Remove(system)
		}
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
	res := adapter.Result{Model: st.model, SessionID: st.id}
	if st.result != nil {
		res.Text = st.result.Result
		res.Tokens = st.result.Usage.tokens()
		res.Cost = st.result.cost()
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
	msg := s.message(st.result, exitErr)
	if s.tooLong(st.result) {
		return res, fmt.Errorf("%w: %w: %s", adapter.ErrFailed, adapter.ErrTooLong, msg)
	}
	return res, fmt.Errorf("%w: %s", adapter.ErrFailed, msg)
}

// tooLong is whether the agent refused the session's prompt as too long, by
// the result's text or the end of stderr.
func (s *session) tooLong(r *resultLine) bool {
	text := s.stderr.String()
	if r != nil {
		text += "\n" + r.Result
	}
	return strings.Contains(strings.ToLower(text), "prompt is too long")
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
	id     string
	result *resultLine
}

// line is the part of a stream line that says what it is.
type line struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Model   string `json:"model"`
	Session string `json:"session_id"`
}

// resultLine is the stream's final line.
type resultLine struct {
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
	Usage   usage  `json:"usage"`
	// Models is what each model's part of the session used and cost, keyed by
	// the model's name.
	Models map[string]modelUsage `json:"modelUsage"`
}

// modelUsage is one model's entry in the result line's modelUsage. The cache
// writes are split into 5-minute and 1-hour ones when the agent says so;
// otherwise every write is taken to be a 5-minute one, the agent's default.
type modelUsage struct {
	Input     int64   `json:"inputTokens"`
	Output    int64   `json:"outputTokens"`
	Read      int64   `json:"cacheReadInputTokens"`
	Write     int64   `json:"cacheCreationInputTokens"`
	Write5m   int64   `json:"cacheCreationEphemeral5mInputTokens"`
	Write1h   int64   `json:"cacheCreationEphemeral1hInputTokens"`
	USD       float64 `json:"costUSD"`
	Canonical string  `json:"canonicalModel"`
}

// cost is what the session cost per model, named canonically when the agent
// gives a canonical name, in the order of the names.
func (r *resultLine) cost() adapter.Cost {
	var c adapter.Cost
	for name, u := range r.Models {
		w5 := u.Write5m
		if u.Write5m+u.Write1h == 0 {
			w5 = u.Write
		}
		c = append(c, adapter.ModelCost{Model: cmp.Or(u.Canonical, name), Input: u.Input, CacheRead: u.Read,
			CacheWrite5m: w5, CacheWrite1h: u.Write1h, Output: u.Output, MicroUSD: int64(math.Round(u.USD * 1e6))})
	}
	slices.SortFunc(c, func(a, b adapter.ModelCost) int { return strings.Compare(a.Model, b.Model) })
	return c
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
			st.model, st.id = l.Model, l.Session
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
