// Package claudelog is the Claude Code adapter's reader of session logs: it
// finds a session's transcript under Claude Code's config directory and
// returns what learning needs from it and its subagents, redacted, never
// writing anything there.
package claudelog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dphaener/physa/internal/adapter"
)

// maxText bounds each text the Log returns, in runes, after redaction.
const maxText = 8000

// validID is the form a session id must have before any path is built from
// it.
var validID = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// notMessages are the starts of lines through the person's channel that are
// not a person's message: slash commands, their output, and interruptions.
var notMessages = []string{"<command-message>", "<command-name>", "<local-command", "[Request interrupted"}

// Logs reads Claude Code sessions' logs from Root. It only ever opens files
// for reading; it never writes under Root.
type Logs struct {
	// Root is Claude Code's config directory, which holds projects/.
	Root string
	// dir is the repo's working directory, which names its project.
	dir string

	mu       sync.Mutex
	listed   bool
	projects []string
}

// New reads the logs of sessions run in dir, the repo's working
// directory. Root is $CLAUDE_CONFIG_DIR, or $HOME/.claude when that is unset.
func New(dir string) *Logs {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	root := os.Getenv("CLAUDE_CONFIG_DIR")
	if root == "" {
		if home, err := os.UserHomeDir(); err == nil {
			root = filepath.Join(home, ".claude")
		}
	}
	return &Logs{Root: root, dir: dir}
}

// projectName is the name Claude Code gives the project of dir: every
// character that is not an ASCII letter or digit becomes "-".
func projectName(dir string) string {
	var b strings.Builder
	for _, r := range dir {
		if r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// ReadSession reads the session id's lead transcript and its subagents'. It
// looks in the project of the Logs' directory first, then in every project,
// since sessions also run in worktrees and temp dirs.
func (l *Logs) ReadSession(id string) (adapter.Log, error) {
	if !validID.MatchString(id) {
		return adapter.Log{}, fmt.Errorf("%w: session id %q is not in Claude Code's form", adapter.ErrUnavailable, id)
	}
	base, err := l.find(id)
	if err != nil {
		return adapter.Log{}, err
	}
	f, err := os.Open(filepath.Join(base, id+".jsonl"))
	if err != nil {
		return adapter.Log{}, fmt.Errorf("%w: %v", adapter.ErrUnavailable, err)
	}
	r := &reader{}
	err = r.transcript(f, transcript{lead: true})
	f.Close()
	if err != nil {
		return adapter.Log{}, fmt.Errorf("%w: session %s: %v", adapter.ErrUnavailable, id, err)
	}
	r.subagents(filepath.Join(base, id, "subagents"))
	return r.log(id), nil
}

// find returns the project directory holding id's lead transcript.
func (l *Logs) find(id string) (string, error) {
	projects := filepath.Join(l.Root, "projects")
	own := projectName(l.dir)
	if isFile(filepath.Join(projects, own, id+".jsonl")) {
		return filepath.Join(projects, own), nil
	}
	names, err := l.list(projects)
	if err != nil {
		return "", fmt.Errorf("%w: session %s: %v", adapter.ErrUnavailable, id, err)
	}
	for _, name := range names {
		if name != own && isFile(filepath.Join(projects, name, id+".jsonl")) {
			return filepath.Join(projects, name), nil
		}
	}
	return "", fmt.Errorf("%w: no log of session %s under %s", adapter.ErrUnavailable, id, projects)
}

// list returns the project directories' names, sorted, read once per Logs.
func (l *Logs) list(projects string) ([]string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.listed {
		return l.projects, nil
	}
	entries, err := os.ReadDir(projects)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			l.projects = append(l.projects, e.Name())
		}
	}
	sort.Strings(l.projects)
	l.listed = true
	return l.projects, nil
}

func isFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

// transcript is one transcript file being read: the lead's or a subagent's.
type transcript struct {
	lead  bool
	agent string
	group string
	label string
}

// reader gathers what one session's transcripts hold for learning.
type reader struct {
	asks     []adapter.Ask
	messages []adapter.Message
	reports  []adapter.Report
	skipped  int
}

// subagents reads every subagent transcript under dir: those the lead
// started directly and those in each workflow run. Anything else there (a
// run's journal, tool results) is never opened.
func (r *reader) subagents(dir string) {
	r.agentsIn(dir, "")
	runs, err := os.ReadDir(filepath.Join(dir, "workflows"))
	if err != nil {
		return
	}
	for _, run := range runs {
		if run.IsDir() {
			r.agentsIn(filepath.Join(dir, "workflows", run.Name()), run.Name())
		}
	}
}

// agentsIn reads each agent-<id>.jsonl in dir, as part of run group.
func (r *reader) agentsIn(dir, group string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || !strings.HasPrefix(name, "agent-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		stem := strings.TrimSuffix(name, ".jsonl")
		t := r.meta(filepath.Join(dir, stem+".meta.json"), strings.TrimPrefix(stem, "agent-"))
		t.group = group
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			r.skipped++
			continue
		}
		if err := r.transcript(f, t); err != nil {
			r.skipped++
		}
		f.Close()
	}
}

// meta names a subagent from its .meta.json, or by its agent id when there
// is none.
func (r *reader) meta(path, agentID string) transcript {
	data, err := os.ReadFile(path)
	if err != nil {
		return transcript{agent: agentID}
	}
	var m struct {
		AgentType   string `json:"agentType"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		r.skipped++
		return transcript{agent: agentID}
	}
	agent := strings.TrimSpace(m.AgentType + " " + m.Description)
	if agent == "" {
		agent = agentID
	}
	return transcript{agent: agent, label: m.Description}
}

// logLine is the part of a transcript line learning reads. Parts whose shape
// varies are kept raw and read only when needed.
type logLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	IsMeta    bool   `json:"isMeta"`
	Origin    *struct {
		Kind string `json:"kind"`
	} `json:"origin"`
	TurnPosition *struct {
		PromptIndex int `json:"promptIndex"`
	} `json:"turnPosition"`
	Message *struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`
	Attachment    json.RawMessage `json:"attachment"`
}

// block is one block of a message's content.
type block struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
}

// transcript reads one transcript, line by line, whatever a line's length.
// A line that cannot be parsed is counted in skipped; only a failed read
// fails the transcript.
func (r *reader) transcript(f io.Reader, t transcript) error {
	br := bufio.NewReader(f)
	asked := map[string]bool{}
	for {
		data, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(data)) > 0 {
			var ln logLine
			if jerr := json.Unmarshal(data, &ln); jerr != nil {
				r.skipped++
			} else {
				r.line(&ln, t, asked)
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// line takes what learning needs from one line, ignoring everything else.
func (r *reader) line(ln *logLine, t transcript, asked map[string]bool) {
	switch ln.Type {
	case "user":
		if t.lead {
			r.userMessage(ln)
		}
		r.answer(ln, t, asked)
	case "attachment":
		if t.lead {
			r.queued(ln)
		}
	case "assistant":
		r.toolUses(ln, t, asked)
	}
}

// blocks returns a line's content blocks; none when its content is a string.
func blocks(ln *logLine) []block {
	if ln.Message == nil {
		return nil
	}
	var bs []block
	if json.Unmarshal(ln.Message.Content, &bs) != nil {
		return nil
	}
	return bs
}

// at parses a line's time, counting the line skipped when it has none.
func (r *reader) at(s string) (time.Time, bool) {
	ts, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		r.skipped++
		return time.Time{}, false
	}
	return ts, true
}

// userMessage takes a person's message mid-run from a user line.
func (r *reader) userMessage(ln *logLine) {
	if ln.IsMeta || ln.Origin == nil || ln.Origin.Kind != "human" || ln.TurnPosition == nil || ln.TurnPosition.PromptIndex <= 1 || ln.Message == nil {
		return
	}
	var text string
	if json.Unmarshal(ln.Message.Content, &text) != nil || !isMessage(text) {
		return
	}
	if ts, ok := r.at(ln.Timestamp); ok {
		r.messages = append(r.messages, adapter.Message{At: ts, Text: text})
	}
}

// queued takes a person's message typed while the agent worked.
func (r *reader) queued(ln *logLine) {
	var a struct {
		Type        string `json:"type"`
		CommandMode string `json:"commandMode"`
		Origin      *struct {
			Kind string `json:"kind"`
		} `json:"origin"`
		HumanTurn bool            `json:"humanTurn"`
		Prompt    json.RawMessage `json:"prompt"`
		Timestamp string          `json:"timestamp"`
	}
	if json.Unmarshal(ln.Attachment, &a) != nil || a.Type != "queued_command" || a.CommandMode != "prompt" ||
		a.Origin == nil || a.Origin.Kind != "human" || !a.HumanTurn {
		return
	}
	var text string
	if json.Unmarshal(a.Prompt, &text) != nil || !isMessage(text) {
		return
	}
	stamp := a.Timestamp
	if stamp == "" {
		stamp = ln.Timestamp
	}
	if ts, ok := r.at(stamp); ok {
		r.messages = append(r.messages, adapter.Message{At: ts, Text: text})
	}
}

// isMessage reports whether text sent through the person's channel is a
// message rather than a slash command, its output or an interruption.
func isMessage(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	for _, p := range notMessages {
		if strings.HasPrefix(trimmed, p) {
			return false
		}
	}
	return true
}

// toolUses notes asks and takes checks' reports from an assistant line.
func (r *reader) toolUses(ln *logLine, t transcript, asked map[string]bool) {
	for _, b := range blocks(ln) {
		if b.Type != "tool_use" {
			continue
		}
		switch {
		case b.Name == "AskUserQuestion" && b.ID != "":
			asked[b.ID] = true
		case b.Name == "StructuredOutput" && !t.lead:
			r.structured(ln, t, b.Input)
		case b.Name == "ReportFindings":
			r.reportFindings(ln, t, b.Input)
		}
	}
}

// structured takes a check's report from StructuredOutput's input: one with
// a verdict and findings or discrepancies. A task's result has no verdict
// and is ignored.
func (r *reader) structured(ln *logLine, t transcript, input json.RawMessage) {
	var in struct {
		Verdict  *string `json:"verdict"`
		Findings *[]struct {
			Severity string `json:"severity"`
			Text     string `json:"text"`
		} `json:"findings"`
		Discrepancies *[]string `json:"discrepancies"`
	}
	if json.Unmarshal(input, &in) != nil || in.Verdict == nil || in.Findings == nil && in.Discrepancies == nil {
		return
	}
	ts, ok := r.at(ln.Timestamp)
	if !ok {
		return
	}
	rep := adapter.Report{At: ts, Agent: t.agent, Group: t.group, Label: t.label, Verdict: *in.Verdict}
	if in.Findings != nil {
		for _, f := range *in.Findings {
			rep.Findings = append(rep.Findings, adapter.Finding{Level: f.Severity, Text: f.Text})
		}
	}
	if in.Discrepancies != nil {
		for _, d := range *in.Discrepancies {
			rep.Findings = append(rep.Findings, adapter.Finding{Text: d})
		}
	}
	rep.Passed = passed(rep.Verdict)
	r.reports = append(r.reports, rep)
}

// reportFindings takes a report from ReportFindings' input, whose findings
// may each be marked fixed.
func (r *reader) reportFindings(ln *logLine, t transcript, input json.RawMessage) {
	var in struct {
		Findings *[]struct {
			Summary  string `json:"summary"`
			Severity string `json:"severity"`
			Outcome  string `json:"outcome"`
		} `json:"findings"`
	}
	if json.Unmarshal(input, &in) != nil || in.Findings == nil {
		return
	}
	ts, ok := r.at(ln.Timestamp)
	if !ok {
		return
	}
	rep := adapter.Report{At: ts, Agent: t.agent, Group: t.group, Label: t.label}
	for _, f := range *in.Findings {
		rep.Findings = append(rep.Findings, adapter.Finding{Level: f.Severity, Text: f.Summary, Fixed: f.Outcome == "fixed"})
	}
	rep.Passed = len(rep.Findings) == 0
	r.reports = append(r.reports, rep)
}

// passed reports whether a harness's verdict says the check passed.
func passed(verdict string) bool {
	v := strings.TrimSpace(verdict)
	return strings.EqualFold(v, "PASS") || strings.EqualFold(v, "PASSED")
}

// answer takes the asks answered on a user line: the result of an
// AskUserQuestion this transcript made, holding the person's answers. A
// refused ask, or a question with no answer, is left out.
func (r *reader) answer(ln *logLine, t transcript, asked map[string]bool) {
	if len(ln.ToolUseResult) == 0 {
		return
	}
	matched := false
	for _, b := range blocks(ln) {
		if b.Type == "tool_result" && asked[b.ToolUseID] && !b.IsError {
			matched = true
		}
	}
	if !matched {
		return
	}
	var res struct {
		Questions []struct {
			Question string `json:"question"`
			Options  []struct {
				Label string `json:"label"`
			} `json:"options"`
		} `json:"questions"`
		Answers     map[string]json.RawMessage `json:"answers"`
		Annotations map[string]struct {
			Notes string `json:"notes"`
		} `json:"annotations"`
	}
	if json.Unmarshal(ln.ToolUseResult, &res) != nil || len(res.Answers) == 0 {
		return
	}
	ts, ok := r.at(ln.Timestamp)
	if !ok {
		return
	}
	for _, q := range res.Questions {
		answer := answerText(res.Answers[q.Question])
		if answer == "" {
			continue
		}
		if notes := strings.TrimSpace(res.Annotations[q.Question].Notes); notes != "" {
			answer += "\nNotes: " + notes
		}
		ask := adapter.Ask{At: ts, Agent: t.agent, Text: q.Question, Answer: answer}
		for _, o := range q.Options {
			ask.Options = append(ask.Options, o.Label)
		}
		r.asks = append(r.asks, ask)
	}
}

// answerText reads an answer: a label or free text, or several labels.
func answerText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		return strings.TrimSpace(strings.Join(many, ", "))
	}
	return ""
}

// log returns what was read, each text redacted then bounded, messages
// deduplicated, and each list in time order.
func (r *reader) log(id string) adapter.Log {
	out := adapter.Log{Session: id, Skipped: r.skipped}
	for _, a := range r.asks {
		a.Agent, a.Text, a.Answer = clean(a.Agent), clean(a.Text), clean(a.Answer)
		for i := range a.Options {
			a.Options[i] = clean(a.Options[i])
		}
		out.Asks = append(out.Asks, a)
	}
	seen := map[string]bool{}
	for _, m := range r.messages {
		key := m.At.UTC().Format(time.RFC3339Nano) + "\x00" + m.Text
		if seen[key] {
			continue
		}
		seen[key] = true
		m.Agent, m.Text = clean(m.Agent), clean(m.Text)
		out.Messages = append(out.Messages, m)
	}
	for _, rep := range r.reports {
		rep.Agent, rep.Group, rep.Label, rep.Verdict = clean(rep.Agent), clean(rep.Group), clean(rep.Label), clean(rep.Verdict)
		for i := range rep.Findings {
			rep.Findings[i].Level, rep.Findings[i].Text = clean(rep.Findings[i].Level), clean(rep.Findings[i].Text)
		}
		out.Reports = append(out.Reports, rep)
	}
	sort.SliceStable(out.Asks, func(i, j int) bool { return out.Asks[i].At.Before(out.Asks[j].At) })
	sort.SliceStable(out.Messages, func(i, j int) bool { return out.Messages[i].At.Before(out.Messages[j].At) })
	sort.SliceStable(out.Reports, func(i, j int) bool { return out.Reports[i].At.Before(out.Reports[j].At) })
	return out
}

// clean redacts s, then bounds it, so a cut never splits a key out of the
// shape Redact knows.
func clean(s string) string {
	s = adapter.Redact(s)
	if rs := []rune(s); len(rs) > maxText {
		s = string(rs[:maxText])
	}
	return s
}
