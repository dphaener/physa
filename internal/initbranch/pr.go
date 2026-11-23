package initbranch

import (
	"fmt"
	"os"
	"strings"

	"github.com/dphaener/physa/internal/initcost"
	"github.com/dphaener/physa/internal/mapfile"
)

// openPR opens the branch's PR, or refreshes the open one with the new body.
// Without a forge the branch is ready for review. It never merges a PR.
func openPR(out *Outcome, st state, o Options) error {
	if o.PRs == nil {
		out.PR = PR{State: PRReady, Reason: o.ForgeReason}
		return nil
	}
	n, url, open, err := o.PRs.OpenPR(Branch)
	if err != nil {
		return fmt.Errorf("the PR of %s cannot be read: %w", Branch, err)
	}
	if open {
		if err := o.PRs.EditPR(n, out.Body); err != nil {
			return fmt.Errorf("PR #%d cannot be refreshed: %w", n, err)
		}
		out.PR = PR{State: PRRefreshed, Number: n, URL: url}
		return nil
	}
	n, url, err = o.PRs.CreatePR(Branch, st.name, out.Commit.Subject, out.Body)
	if err != nil {
		return fmt.Errorf("the PR of %s cannot be opened: %w", Branch, err)
	}
	out.PR = PR{State: PROpened, Number: n, URL: url}
	return nil
}

// Body is the PR body for m (design 02 section 2.6): the capabilities
// proposed, the entries recovered for each with their anchors or why it has
// none, the questions raised, the entries dropped and the tokens spent, then
// what happened to the Session: hook. Entries are read back from m's files
// as the map reader reads them; Run's Outcome.Body is the same text, read
// from the branch's working copy.
func Body(m Map) string {
	dir, err := os.MkdirTemp("", "physa-init-body-")
	if err != nil {
		return body(m, nil)
	}
	defer os.RemoveAll(dir)
	files, m := mapFiles(m)
	if _, err := writeFiles(dir, files); err != nil {
		return body(m, nil)
	}
	read, _, err := mapfile.Read(dir)
	if err != nil {
		return body(m, nil)
	}
	return body(m, read)
}

// body is the PR body, with entries from read; nil read lists none.
func body(m Map, read *mapfile.Map) string {
	files := map[string]*mapfile.File{}
	var prod *mapfile.File
	if read != nil {
		prod = read.Product
		for _, f := range read.Capabilities {
			files[f.Name] = f
		}
	}
	var b strings.Builder
	if len(m.Capabilities) == 0 {
		b.WriteString("Physa init starts the map of a new codebase from a person's answers: the product's promise and its first terms. Capabilities come as the product grows.\n")
	} else {
		b.WriteString("Physa init recovered this map from the repo's docs, history and PRs. Nothing here is settled until a person merges it: edit, delete or merge it like any PR. Physa never merges it.\n")
	}
	b.WriteString("\n## Capabilities proposed\n\n")
	if len(m.Capabilities) == 0 {
		b.WriteString("None yet.\n")
	}
	for _, c := range m.Capabilities {
		fmt.Fprintf(&b, "- `%s`: %s\n", c.Name, c.Promise)
	}
	if len(m.Capabilities) > 0 {
		b.WriteString("\n## Entries recovered\n")
		for _, c := range m.Capabilities {
			recovered(&b, c, files[c.Name])
		}
	}
	b.WriteString("\n## Questions raised\n\n")
	questions(&b, m, prod, files)
	b.WriteString("\n## Entries dropped\n\n")
	dropped(&b, m)
	b.WriteString("\n## Tokens spent\n\n")
	b.WriteString(initcost.Table(m.Summary.Tokens))
	b.WriteString("\n## The Session: hook\n\n" + hookLine(m.Summary.Hook) + "\n")
	return b.String()
}

// recovered writes the rules and decisions recovered for c, or why there are
// none.
func recovered(b *strings.Builder, c Capability, f *mapfile.File) {
	fmt.Fprintf(b, "\n### %s\n\n", c.Name)
	if c.File == "" {
		status := c.Status
		if status == "" {
			status = "not yet recovered"
		}
		line := strings.ToUpper(status[:1]) + status[1:]
		if c.Reason != "" {
			line += ": " + c.Reason
		}
		b.WriteString(line + ". Its folder holds its promise only, to be filled from future PRs.\n")
		return
	}
	n := 0
	if f != nil {
		for _, e := range f.Entries {
			if e.Kind != mapfile.Question {
				fmt.Fprintf(b, "- %s: %s%s\n", e.Kind, e.Heading, anchors(e.Anchors))
				n++
			}
		}
	}
	if n == 0 {
		b.WriteString("No rules or decisions recovered.\n")
	}
}

// questions writes every question of the map: the product's, then each
// capability's.
func questions(b *strings.Builder, m Map, prod *mapfile.File, files map[string]*mapfile.File) {
	n := 0
	list := func(where string, f *mapfile.File) {
		if f == nil {
			return
		}
		for _, e := range f.Entries {
			if e.Kind == mapfile.Question {
				fmt.Fprintf(b, "- %s: %s%s\n", where, e.Heading, anchors(e.Anchors))
				n++
			}
		}
	}
	list("product", prod)
	for _, c := range m.Capabilities {
		list(c.Name, files[c.Name])
	}
	if n == 0 {
		b.WriteString("None.\n")
	}
}

// dropped writes what proposing took out, then each capability's dropped
// entries and terms.
func dropped(b *strings.Builder, m Map) {
	n := 0
	for _, d := range m.Summary.Dropped {
		fmt.Fprintf(b, "- proposal: %s\n", drop(d))
		n++
	}
	for _, c := range m.Capabilities {
		for _, d := range c.Dropped {
			fmt.Fprintf(b, "- %s: %s\n", c.Name, drop(d))
			n++
		}
	}
	if n == 0 {
		b.WriteString("None.\n")
	}
}

// drop reads like `rule "Sends are queued": a rule needs a Because: line`.
func drop(d Drop) string {
	s := d.What
	if d.Value != "" {
		s += fmt.Sprintf(" %q", d.Value)
	}
	if d.Reason != "" {
		s += ": " + d.Reason
	}
	return s
}

// anchors reads like " (#12 a1b2c3d from docs/x.md)"; "" when there are none.
func anchors(as []mapfile.Anchor) string {
	if len(as) == 0 {
		return ""
	}
	parts := make([]string, len(as))
	for i, a := range as {
		switch a.Kind {
		case mapfile.PR:
			parts[i] = "#" + a.Value
		case mapfile.Doc:
			parts[i] = "from " + a.Value
		default:
			parts[i] = a.Value
		}
	}
	return " (" + strings.Join(parts, " ") + ")"
}

// hookLine says what happened to the Session: hook. It is local config,
// never part of the PR.
func hookLine(h Hook) string {
	switch h.State {
	case HookInstalled:
		s := "Installed"
		if h.Detail != "" {
			s += " at " + h.Detail
		}
		return s + ": commits made in Claude Code now carry `Session:`. It is local config, not part of this PR."
	case HookAlready:
		return "Already installed: commits made in Claude Code carry `Session:`."
	case HookRefused:
		return "Not installed: " + h.Detail
	}
	return "Not asked for. Run `physa map hooks` so commits made in Claude Code carry `Session:`."
}
