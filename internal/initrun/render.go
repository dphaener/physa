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
	"github.com/dphaener/physa/internal/recovery"
	"github.com/dphaener/physa/internal/recoveryreport"
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
	p.emit("recovery", func(w io.Writer) error { return recoveryreport.WriteText(w, r) }, func(w io.Writer) error { return recoveryreport.WriteJSON(w, r) })
}

// newCodebase prints what a new codebase's map will hold.
func (p *printer) newCodebase(a newcodebase.Answers, write bool) {
	text, v := a.Describe(write)
	p.value("new_codebase", text, v)
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
