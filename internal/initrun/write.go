package initrun

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/initbranch"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/newcodebase"
	"github.com/dphaener/physa/internal/recovery"
	"github.com/dphaener/physa/internal/sessionhook"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
)

// notReached is why a capability recovering never reached has no recovered
// text.
const notReached = "recovering has not reached it yet"

// writeStored writes what the store holds to physa/init; see storedMap.
func (r *run) writeStored() int {
	m, err := storedMap(r.st, r.root, r.env)
	if err != nil {
		return failed(r.stderr, err)
	}
	return writeMap(m, r.o, r.env, r.p, r.stderr)
}

// storedMap builds init's map from the store: the proposal's promise,
// questions and capabilities in its order, each with its stored recovery
// (promise only when it has no recovered text), the recovered terms in capability
// order, the sessions Physa started, and what the PR body summarises, with
// what each phase spent as the meter counted it. The product is named after
// root.
func storedMap(st *store.Store, root string, env Env) (initbranch.Map, error) {
	prop, ok, err := st.Proposed()
	if err == nil && !ok {
		err = recovery.ErrNoProposal
	}
	var s mapping.Stored
	if err == nil {
		err = json.Unmarshal(prop.Body, &s)
	}
	rows, rerr := st.Recovered()
	spent, serr := st.Spending()
	if err = errors.Join(err, rerr, serr); err != nil {
		return initbranch.Map{}, err
	}
	m := initbranch.Map{ProductName: newcodebase.TitleCase(filepath.Base(root)), ProductPromise: s.Proposal.Product.Promise,
		Sessions: []string{prop.SessionID}, Date: env.Now()}
	for _, q := range s.Proposal.Questions {
		m.ProductQuestions = append(m.ProductQuestions, initbranch.Question{Question: q.Question, SuggestedBy: q.SuggestedBy})
	}
	for _, d := range s.Dropped {
		value := strings.TrimPrefix(d.Capability+": "+d.Value, ": ")
		m.Summary.Dropped = append(m.Summary.Dropped, initbranch.Drop{What: d.What, Value: value, Reason: d.Reason})
	}
	for _, mf := range s.Malformed {
		m.Summary.Dropped = append(m.Summary.Dropped, initbranch.Drop{What: "malformed item", Value: mf.Raw, Reason: mf.Reason})
	}
	byName := map[string]store.Recovered{}
	for _, r := range rows {
		byName[r.Capability] = r
	}
	var terms []string
	for _, c := range s.Proposal.Capabilities {
		ic := initbranch.Capability{Name: c.Name, Promise: c.Promise, Status: recovery.StatusNotYet, Reason: notReached}
		if r, ok := byName[c.Name]; ok {
			if ic, err = withRecovery(ic, r); err != nil {
				return m, err
			}
			if r.File != "" && strings.TrimSpace(r.Terms) != "" {
				terms = append(terms, strings.TrimSpace(r.Terms))
			}
			m.Sessions = append(m.Sessions, r.SessionID)
		}
		m.Capabilities = append(m.Capabilities, ic)
	}
	m.Terms = strings.Join(terms, "\n\n")
	for _, p := range spent {
		m.Summary.Tokens = append(m.Summary.Tokens, initbranch.Tokens{Phase: p.Phase, Input: p.Raw.Input, CachedInput: p.Raw.CachedInput, Output: p.Raw.Output, Weighted: p.Spend})
	}
	return m, nil
}

// withRecovery gives c its stored recovery r: the text, how it ended and
// why, and the entries dropped from it.
func withRecovery(c initbranch.Capability, r store.Recovered) (initbranch.Capability, error) {
	var d recovery.Outcome
	if err := json.Unmarshal(r.Details, &d); len(r.Details) > 0 && err != nil {
		return c, fmt.Errorf("the stored recovery of %s cannot be read: %w", r.Capability, err)
	}
	c.File, c.Status, c.Reason = r.File, r.Status, strings.Join(append(d.Problems, d.Error), "; ")
	if c.Reason = strings.Trim(c.Reason, "; "); r.File != "" {
		c.Reason = ""
	} else if c.Reason == "" && r.Status == recovery.StatusNotYet {
		c.Reason = notReached
	}
	for _, dr := range d.Dropped {
		value := dr.Heading
		if dr.Slug != "" {
			value += " (" + dr.Slug + ")"
		}
		c.Dropped = append(c.Dropped, initbranch.Drop{What: dr.Kind, Value: value, Reason: strings.Join(dr.Reasons, "; ")})
	}
	return c, nil
}

// newMap is a new codebase's map: the person's name, promise and terms.
func newMap(a newcodebase.Answers, env Env) initbranch.Map {
	return initbranch.Map{ProductName: a.Name, ProductPromise: a.Promise, Terms: a.GlossaryTerms(), Date: env.Now()}
}

// writeMap writes m to physa/init through initbranch, pushing it with
// --push. With --hooks it then installs the Session: hook, after the commit
// and before the PR, so the PR body says what happened to it; a refused
// hook keeps the branch and makes the exit code 2. With --pr it then opens
// the branch's PR, or refreshes the open one. It returns the exit code.
func writeMap(m initbranch.Map, o Flags, env Env, p *printer, stderr io.Writer) int {
	w, err := env.Writer(".")
	if err != nil {
		return failed(stderr, err)
	}
	opts := initbranch.Options{Push: o.Push}
	if env.PRExists != nil {
		opts.PRExists = env.PRExists(".")
	}
	out, err := initbranch.Run(w, m, opts)
	if err != nil {
		return failed(stderr, err)
	}
	code := 0
	if o.Hooks {
		if m.Summary.Hook = installHook(env); m.Summary.Hook.State == initbranch.HookRefused {
			fmt.Fprintf(stderr, "physa init: the branch was written, but the hook was not installed: %s\n", m.Summary.Hook.Detail)
			code = 2
		}
		out.Body = initbranch.Body(m)
	}
	if o.PR {
		out.PR, err = openPR(w, env.Forge("."), out)
	}
	p.wrote(out, m.Summary.Hook)
	if err != nil {
		return failed(stderr, err)
	}
	return code
}

// installHook installs the Session: hook in the repo here and says what
// happened; any error is a refusal, with its reason.
func installHook(env Env) initbranch.Hook {
	root, err := filepath.Abs(".")
	var h vcs.Hooks
	if err == nil {
		h, err = env.Hooks(".")
	}
	var got sessionhook.Outcome
	if err == nil {
		got, err = sessionhook.Install(root, h)
	}
	switch {
	case err != nil:
		return initbranch.Hook{State: initbranch.HookRefused, Detail: err.Error()}
	case got.Written:
		return initbranch.Hook{State: initbranch.HookInstalled, Detail: got.Path}
	case got.Set:
		return initbranch.Hook{State: initbranch.HookInstalled, Detail: "core.hooksPath = " + sessionhook.Dir}
	}
	return initbranch.Hook{State: initbranch.HookAlready, Detail: got.Path}
}

// openPR opens the PR of physa/init into the default branch with out's
// body, or refreshes the open one's body. A forge that cannot open PRs, is
// unavailable or is not signed in leaves the branch ready, with the reason.
// It never merges a PR.
func openPR(w vcs.Writer, f forge.Forge, out initbranch.Outcome) (initbranch.PR, error) {
	prs, ok := f.(forge.PRs)
	if !ok {
		return initbranch.PR{State: initbranch.PRReady, Reason: "the forge cannot open PRs"}, nil
	}
	pr := initbranch.PR{State: initbranch.PRRefreshed}
	n, url, open, err := prs.OpenPR(initbranch.Branch)
	if err == nil && open {
		err = prs.EditPR(n, out.Body)
	} else if err == nil {
		pr.State = initbranch.PROpened
		var def string
		if def, err = w.DefaultBranch(); err == nil {
			if remote, name, ok := strings.Cut(def, "/"); ok && remote != "" {
				def = name
			}
			n, url, err = prs.CreatePR(initbranch.Branch, def, out.Commit.Subject, out.Body)
		}
	}
	if errors.Is(err, forge.ErrUnavailable) || errors.Is(err, forge.ErrUnauthenticated) {
		return initbranch.PR{State: initbranch.PRReady, Reason: forge.Reason(err)}, nil
	}
	if err != nil {
		return initbranch.PR{}, fmt.Errorf("the PR of %s: %w", initbranch.Branch, err)
	}
	pr.Number, pr.URL = n, url
	return pr, nil
}
