// Package writereport says what writing physa/init did: the text a person
// reads, ending with the PR body, and the value --json prints.
package writereport

import (
	"fmt"
	"strings"

	"github.com/dphaener/physa/internal/initbranch"
)

// Describe says what a run did: the
// text a person reads, ending with the body, and the value --json prints. A
// hook or PR never asked for reads "not asked".
func Describe(out initbranch.Outcome, hook initbranch.Hook) (string, map[string]any) {
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
	return b.String() + "\n\n" + strings.TrimRight(out.Body, "\n") + "\n", v
}
