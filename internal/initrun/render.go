package initrun

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/dphaener/physa/internal/gather"
	"github.com/dphaener/physa/internal/initbranch"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/newcodebase"
	"github.com/dphaener/physa/internal/recovering"
	"github.com/dphaener/physa/internal/recovery"
)

// printer prints a run phase by phase: a run of one phase as that phase's
// command does; a run of more (the full run, or one that writes) each
// phase's text in turn, or with --json one document of them all, by done.
type printer struct {
	w           io.Writer
	json, multi bool
	sections    int
	doc         map[string]any
	err         error
}

func newPrinter(o Flags, w io.Writer) *printer {
	return &printer{w: w, json: o.JSON, multi: o.Full() || o.Write, doc: map[string]any{}}
}

// emit prints one phase as text, or as JSON under key.
func (p *printer) emit(key string, text, js func(io.Writer) error) {
	switch {
	case p.err != nil:
	case p.json && p.multi:
		var b bytes.Buffer
		if p.err = js(&b); p.err == nil {
			p.doc[key] = json.RawMessage(bytes.TrimSpace(b.Bytes()))
		}
	case p.json:
		p.err = js(p.w)
	default:
		if p.sections++; p.sections > 1 {
			_, p.err = io.WriteString(p.w, "\n")
		}
		if p.err == nil {
			p.err = text(p.w)
		}
	}
}

// value prints one phase as text, or as v under key.
func (p *printer) value(key, text string, v any) {
	p.emit(key, func(w io.Writer) error { _, err := io.WriteString(w, text); return err },
		func(w io.Writer) error { return json.NewEncoder(w).Encode(v) })
}

func (p *printer) gather(s gather.Summary) {
	p.emit("gather", func(w io.Writer) error { return gather.WriteText(w, s) }, func(w io.Writer) error { return gather.WriteJSON(w, s) })
}

func (p *printer) proposal(r mapping.Result) {
	p.emit("proposal", func(w io.Writer) error { return mapping.WriteText(w, r) }, func(w io.Writer) error { return mapping.WriteJSON(w, r) })
}

func (p *printer) recovery(r recovery.Report) {
	p.emit("recovery", func(w io.Writer) error { return writeRecoveryText(w, r) }, func(w io.Writer) error { return writeRecoveryJSON(w, r) })
}

// newCodebase prints what a new codebase's map will hold.
func (p *printer) newCodebase(a newcodebase.Answers, write bool) {
	names, terms := []string{}, []map[string]string{}
	for _, t := range a.Terms {
		names = append(names, t.Name)
		terms = append(terms, map[string]string{"name": t.Name, "meaning": t.Meaning})
	}
	text := fmt.Sprintf("A new codebase: nothing to recover, so the map starts from your answers.\n"+
		"- .physa/product.md: %s, promising: %s\n- .physa/glossary.md: %s (%s)\n- .physa/README.md, and the pointer in AGENTS.md\n",
		a.Name, a.Promise, plural(len(a.Terms), "term", "terms"), strings.Join(names, ", "))
	if !write {
		text += "Nothing is written without --write.\n"
	}
	p.value("new_codebase", text, map[string]any{"name": a.Name, "promise": a.Promise, "terms": terms})
}

// wrote prints what writing physa/init did, then in text the PR body.
func (p *printer) wrote(out initbranch.Outcome, hook initbranch.Hook) {
	pr, h := out.PR, hook.State
	if pr.State == initbranch.PRNotAsked {
		pr.State = "not asked"
	}
	if h == initbranch.HookNotAsked {
		h = "not asked"
	}
	v := map[string]any{"branch": initbranch.Branch, "files": out.Files, "pushed": out.Pushed, "body": out.Body,
		"pr":   map[string]any{"state": pr.State, "number": pr.Number, "url": pr.URL, "reason": pr.Reason},
		"hook": map[string]string{"state": h, "detail": hook.Detail}}
	var b strings.Builder
	if c := out.Commit; c != nil {
		v["commit"] = map[string]string{"id": c.ID, "short": c.Short, "subject": c.Subject}
		fmt.Fprintf(&b, "Wrote %s at %s: %s\n", initbranch.Branch, c.Short, c.Subject)
	}
	fmt.Fprintf(&b, "Files: %s\nPushed: %v\nPR: %s", strings.Join(out.Files, ", "), out.Pushed, pr.State)
	if pr.Number != 0 {
		fmt.Fprintf(&b, " #%d %s", pr.Number, pr.URL)
	}
	if pr.Reason != "" {
		b.WriteString(": " + pr.Reason)
	}
	if b.WriteString("\nHook: " + h); hook.Detail != "" {
		b.WriteString(": " + hook.Detail)
	}
	p.value("write", b.String()+"\n\n"+strings.TrimRight(out.Body, "\n")+"\n", v)
}

// done ends the run, writing the one document of a --json run of more than
// one phase. It returns code, or 2 when printing failed.
func (p *printer) done(code int, stderr io.Writer) int {
	if p.err == nil && p.json && p.multi && len(p.doc) > 0 {
		enc := json.NewEncoder(p.w)
		enc.SetIndent("", "  ")
		p.err = enc.Encode(p.doc)
	}
	if p.err != nil {
		return failed(stderr, p.err)
	}
	return code
}

// exitCode is how a recovery run ends: 2 when any capability failed, else 1
// when any is not yet recovered, else 0.
func exitCode(r recovery.Report) int {
	code := 0
	for _, o := range r.Capabilities {
		switch o.Status {
		case recovery.StatusFailed:
			return 2
		case recovery.StatusNotYet:
			code = 1
		}
	}
	return code
}

// count says how many of the report's capabilities ended with status.
func count(r recovery.Report, status string) int {
	n := 0
	for _, o := range r.Capabilities {
		if o.Status == status {
			n++
		}
	}
	return n
}

// writeRecoveryJSON writes the report as one indented JSON document, each
// capability's file and terms included.
func writeRecoveryJSON(w io.Writer, r recovery.Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// writeRecoveryText writes the report as text: a first line with the counts
// and the tokens used of the appetite, then one block per capability that
// ran, then the capabilities not yet recovered.
func writeRecoveryText(w io.Writer, r recovery.Report) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Recovered %d, not yet recovered %d, failed %d; used %d of %d tokens.\n",
		count(r, recovery.StatusRecovered), count(r, recovery.StatusNotYet), count(r, recovery.StatusFailed),
		r.Used.Input+r.Used.Output, r.Appetite)
	var notYet []string
	for _, o := range r.Capabilities {
		if o.Status == recovery.StatusNotYet {
			notYet = append(notYet, o.Name)
			continue
		}
		fmt.Fprintf(&b, "\n## %s: %s\n", o.Name, o.Status)
		if o.Status == recovery.StatusRecovered {
			fmt.Fprintf(&b, "Kept %s.\n", kept(o.Counts))
		}
		for _, d := range o.Dropped {
			fmt.Fprintf(&b, "Dropped %s\n", dropped(d))
		}
		for _, p := range o.Problems {
			fmt.Fprintf(&b, "Problem: %s\n", p)
		}
		if o.Error != "" {
			fmt.Fprintf(&b, "Error: %s\n", o.Error)
		}
		fmt.Fprintf(&b, "Attempts: %d; session %s; tokens: %s.\n", o.Attempts, orNone(o.SessionID), o.Tokens)
	}
	if len(notYet) > 0 {
		fmt.Fprintf(&b, "\nNot yet recovered (the appetite ran out; run again with --only %s):\n", strings.Join(notYet, ","))
		for _, n := range notYet {
			fmt.Fprintf(&b, "- %s\n", n)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// kept words what a capability kept: "3 rules, 1 decision, 0 questions, 2 terms".
func kept(c recovering.Counts) string {
	return strings.Join([]string{
		plural(c.Rules, "rule", "rules"), plural(c.Decisions, "decision", "decisions"),
		plural(c.Questions, "question", "questions"), plural(c.Terms, "term", "terms"),
	}, ", ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// dropped words one dropped entry or term and why.
func dropped(d recovering.Dropped) string {
	s := d.Kind + " " + fmt.Sprintf("%q", d.Heading)
	if d.Slug != "" {
		s += " (" + d.Slug + ")"
	}
	return s + ": " + strings.Join(d.Reasons, "; ")
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
