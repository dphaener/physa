package initrun

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dphaener/physa/internal/areas"
	"github.com/dphaener/physa/internal/consolidatedmap"
	"github.com/dphaener/physa/internal/initbranch"
	"github.com/dphaener/physa/internal/initpr"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/initreport"
	"github.com/dphaener/physa/internal/newcodebase"
	"github.com/dphaener/physa/internal/productpromise"
	"github.com/dphaener/physa/internal/recoverinput"
	"github.com/dphaener/physa/internal/recovery"
	"github.com/dphaener/physa/internal/walk"
)

// notReached is why a capability recovering never reached has no recovered
// text.
const notReached = "recovering has not reached it yet"

// writeStored writes what the store holds to physa/init; see storedMap, with init's report as the body's section and in --json. A current
// stored consolidation takes the place of the recovered text; a stale one is said and ignored, and so is a product promise not stated.
func (r *run) writeStored(ctx context.Context) int {
	m, err := storedMap(ctx, r.st, r.root, r.env)
	if err != nil {
		return failed(r.stderr, err)
	}
	if m.ProductPromise == productpromise.NoPromise {
		fmt.Fprintf(r.stderr, "physa init: the stored areas state no product promise, so the map's product says %q; a new physa init states one\n", productpromise.NoPromise)
	}
	m, stale, err := consolidatedmap.Apply(ctx, r.st, m)
	if err != nil {
		return failed(r.stderr, err)
	}
	if stale != "" {
		fmt.Fprintf(r.stderr, "physa init: %s\n", stale)
	}
	rep := initreport.Load(ctx, r.st)
	m.Summary.Skill, m.Summary.Measures = r.skill, initreport.Section(rep)
	return writeMap(ctx, m, rep, r.o, r.env, r.p, r.stderr)
}

// productName is the product's name: the one the stored areas state, else root's folder name.
func productName(p productpromise.Product, root string) string {
	return cmp.Or(p.Name, newcodebase.TitleCase(filepath.Base(root)))
}

// storedMap builds init's map from the store: the walk's capabilities, in walk
// order, each with its stored recovery from that walk (promise only when it
// has no recovered text), the recovered terms in capability order, the
// sessions Physa started (the areas', the walk's, then each recovery's), and
// what the PR body summarises, with what each phase spent as the meter
// counted it. The product's name and promise are the ones the stored areas
// state, or root's folder name and "Not yet stated.".
func storedMap(ctx context.Context, st *initrecord.Store, root string, env Env) (initbranch.Map, error) {
	p, _, err := productpromise.Stored(ctx, st)
	src, ok, lerr := recoverinput.Listed(ctx, st)
	if lerr == nil && !ok {
		lerr = recovery.ErrNoWalk
	}
	ar, _, aerr := areas.Load(ctx, st)
	wr, _, werr := walk.Load(ctx, st)
	rows, rerr := st.Recovered(ctx)
	spent, serr := st.Spending(ctx)
	if err = errors.Join(err, lerr, aerr, werr, rerr, serr); err != nil {
		return initbranch.Map{}, err
	}
	m := initbranch.Map{ProductName: productName(p, root), ProductPromise: p.PromiseOr(), Date: env.Now(), Sessions: slices.Concat(ar.Sessions, wr.Sessions)}
	byName := map[string]initrecord.Recovered{}
	for _, r := range rows {
		if src.Holds(r.ProposalAt) {
			byName[r.Capability] = r
		}
	}
	var terms []string
	for _, c := range src.Capabilities {
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
func withRecovery(c initbranch.Capability, r initrecord.Recovered) (initbranch.Capability, error) {
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

// newMap is a new codebase's map: the person's name, promise, terms and skill line.
func newMap(a newcodebase.Answers, env Env, skill string) initbranch.Map {
	return initbranch.Map{ProductName: a.Name, ProductPromise: a.Promise, Terms: a.GlossaryTerms(), Date: env.Now(),
		Summary: initbranch.Summary{Skill: skill}}
}

// writeMap writes m to physa/init through initbranch, pushing it with
// --push. With --hooks it then installs the Session: hook, after the commit
// and before the PR, so the PR body says what happened to it; a refused
// hook keeps the branch and makes the exit code 2. With --pr it then opens
// the branch's PR, or refreshes the open one. --json carries report when not nil. It returns the exit code.
func writeMap(ctx context.Context, m initbranch.Map, report any, o Flags, env Env, p *printer, stderr io.Writer) int {
	w, err := env.Writer(ctx, ".")
	if err != nil {
		return failed(stderr, err)
	}
	opts := initbranch.Options{Push: o.Push}
	if env.PRExists != nil {
		opts.PRExists = env.PRExists(".")
	}
	out, err := initbranch.Run(ctx, w, m, opts)
	if err != nil {
		return failed(stderr, err)
	}
	code := 0
	if o.Hooks {
		if m.Summary.Hook = initpr.InstallHook(ctx, env.Hooks); m.Summary.Hook.State == initbranch.HookRefused {
			fmt.Fprintf(stderr, "physa init: the branch was written, but the hook was not installed: %s\n", m.Summary.Hook.Detail)
			code = 2
		}
		out.Body = initbranch.Body(m)
	}
	if o.PR {
		out.PR, err = initpr.Open(ctx, w, env.Forge("."), out)
	}
	p.wrote(out, m.Summary.Hook, report)
	if err != nil {
		return failed(stderr, err)
	}
	return code
}
