package areas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/reading"
	"github.com/dphaener/physa/internal/sourceset"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
)

const (
	// DefaultPartChars is the most listing characters one grouping session
	// is given when Session.PartChars is 0 and the window allows it.
	DefaultPartChars = 70_000
	// MaxFixRounds is how often one session is sent back the files it left
	// out or placed twice before the phase fails.
	MaxFixRounds = 2
	minPartChars = 1_000
)

// Store is init's record as the areas phase uses it.
type Store interface {
	Gathered(ctx context.Context) (store.Gather, bool, error)
	PutAreas(context.Context, initrecord.Areas) error
}

// Session is how the phase's sessions are run: the agent, the repo's root
// where each runs read-only, the repo (which must be a vcs.Tracker), where
// progress lines go, the clock (nil is time.Now) and the most listing
// characters a grouping session is given (0 is DefaultPartChars).
type Session struct {
	Agent     func() (adapter.Adapter, error)
	Root      string
	Repo      vcs.Repo
	Progress  io.Writer
	Now       func() time.Time
	PartChars int
}

// AreaCount is one area of a Result: its name, promise and file count.
type AreaCount struct {
	Name    string `json:"name"`
	Promise string `json:"promise"`
	Files   int    `json:"files"`
}

// Result is what the phase ended with: the tracked files listed, those left
// out by reason, the files of each kind and the source count, the areas,
// the parts and merges, the fix rounds run, the unused and dropped patterns,
// and the model and tokens. MergeKept, when set, says why the merge failed
// and the areas were kept apart.
type Result struct {
	Listed    int                    `json:"listed"`
	Left      map[string]int         `json:"left"`
	Kinds     map[sourceset.Kind]int `json:"kinds"`
	Source    int                    `json:"source"`
	Areas     []AreaCount            `json:"areas"`
	Parts     int                    `json:"parts"`
	Merges    []Joint                `json:"merges"`
	MergeKept string                 `json:"merge_kept"`
	FixRounds int                    `json:"fix_rounds"`
	Unused    []string               `json:"unused"`
	Dropped   []string               `json:"dropped"`
	Model     string                 `json:"model"`
	Tokens    adapter.Tokens         `json:"tokens"`
}

// Run is init's areas phase end to end. It lists the repo's tracked files,
// has one session name the test, doc and other files as patterns, then has
// one session (one per part when the listing is larger than a part) place
// every source file in exactly one area; files left out or claimed twice go
// back to that session, at most MaxFixRounds times. It stores the areas and
// the split in init's record, and writes nothing to the repo or the map. On
// any error it stores nothing.
func Run(ctx context.Context, st Store, s Session) (Result, error) {
	res := Result{Left: map[string]int{}, Kinds: map[sourceset.Kind]int{}}
	g, ok, err := st.Gathered(ctx)
	if err != nil {
		return res, err
	}
	if !ok {
		return res, mapping.ErrNoGather
	}
	tr, ok := s.Repo.(vcs.Tracker)
	if !ok {
		return res, errors.New("the repo cannot list its tracked files, so its areas cannot be found")
	}
	tracked, err := tr.Tracked(ctx)
	if err != nil {
		return res, err
	}
	l, err := sourceset.List(s.Root, tracked)
	if err != nil {
		return res, err
	}
	res.Listed = len(tracked)
	for reason, paths := range l.Left {
		res.Left[reason] = len(paths)
	}
	if len(l.Files) == 0 {
		return res, errors.New("the repo has no files to divide into areas once vendored, generated, binary and lock files are left out")
	}
	a, err := s.Agent()
	if err != nil {
		return res, err
	}
	r := &runner{ctx: ctx, a: a, s: s, res: &res, byName: map[string]*Area{}}

	sys, msg := SplitPrompt(sourceset.Kinds(l.Files))
	out, err := r.turn(adapter.Spec{System: sys, Prompt: msg}, fmt.Sprintf("the split names the test, doc and other files among %d", len(l.Files)))
	if err != nil {
		return res, err
	}
	tests, docs, other, err := ParseSplit(out.Text)
	if err != nil {
		return res, fmt.Errorf("the split answer: %w", err)
	}
	d := sourceset.Split(l.Files, tests, docs, other)
	res.Unused, res.Dropped = d.Unused, d.Dropped
	for k, files := range d.Files {
		res.Kinds[k] = len(files)
	}
	source := d.Files[sourceset.Source]
	if res.Source = len(source); res.Source == 0 {
		dropped := strings.Join(d.Dropped, ", ")
		if dropped == "" {
			dropped = "none"
		}
		return res, fmt.Errorf("the split leaves no source file to divide into areas (patterns dropped for matching every file: %s)", dropped)
	}
	src := sourceset.Only(l.Files, source)
	overview := sourceset.Overview(src)
	size := partSize(s.PartChars, adapter.Window(a), overview)
	var parts []sourceset.Part
	if len(sourceset.Tree(src)) > size {
		parts = sourceset.Cut(src, size)
	}
	if len(parts) < 2 {
		res.Parts = 1
		sys, msg := AreasPrompt(sourceset.Tree(src))
		err = r.group(sys, msg, source, fmt.Sprintf("grouping places %d source files", res.Source))
	} else {
		res.Parts = len(parts)
		for i := 0; err == nil && i < len(parts); i++ {
			sys, msg := PartPrompt(i+1, len(parts), overview, r.briefs(nil), parts[i].Text)
			err = r.group(sys, msg, parts[i].Files, fmt.Sprintf("grouping part %d of %d places %d source files", i+1, len(parts), len(parts[i].Files)))
		}
		if err == nil {
			err = r.merge()
		}
	}
	if err == nil {
		err = r.covers(source)
	}
	if err != nil {
		return res, err
	}
	for _, area := range r.areas {
		res.Areas = append(res.Areas, AreaCount{Name: area.Name, Promise: area.Promise, Files: len(area.Files)})
	}
	body, err := json.Marshal(record(l, d, r.areas, res.Parts, res.Merges, r.ids))
	if err != nil {
		return res, err
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	return res, st.PutAreas(ctx, initrecord.Areas{
		GatherAt: g.Run.At, At: now().UTC(), Model: res.Model,
		Input: res.Tokens.Input, CachedInput: res.Tokens.CachedInput, Output: res.Tokens.Output, Body: body,
	})
}

// partSize is chars, lowered to the room a known window leaves at about 2
// characters a token once the reply, the skills and the overview are counted.
func partSize(chars, window int, overview string) int {
	if chars <= 0 {
		chars = DefaultPartChars
	}
	if window > 0 {
		room := (window - mapping.ReplyReserve - reading.Tokens(Areas+Parts+overview)) * 2
		chars = min(chars, max(room, minPartChars))
	}
	return chars
}

// runner holds a run's agent, its areas so far in order, and its sessions.
type runner struct {
	ctx    context.Context
	a      adapter.Adapter
	s      Session
	res    *Result
	ids    []string
	areas  []*Area
	byName map[string]*Area
}

// turn runs one turn of a session in the root, read-only unless prompt-only,
// after a progress line, adding what it used to the result.
func (r *runner) turn(spec adapter.Spec, what string) (adapter.Result, error) {
	if spec.SessionID == "" {
		spec.SessionID = draft.NewSessionID()
	}
	spec.Dir, spec.ReadOnly = r.s.Root, !spec.PromptOnly
	if r.s.Progress != nil {
		fmt.Fprintf(r.s.Progress, "physa init: areas: %s; session %s\n", what, spec.SessionID)
	}
	var last atomic.Pointer[adapter.Tokens]
	spec.Usage = func(t adapter.Tokens) { last.Store(&t) }
	sess, err := r.a.Start(r.ctx, spec)
	if err != nil {
		return adapter.Result{}, err
	}
	if !spec.Resume {
		r.ids = append(r.ids, spec.SessionID)
	}
	out, err := sess.Wait()
	if p := last.Load(); p != nil && out.Tokens == (adapter.Tokens{}) {
		out.Tokens = *p
	}
	t := &r.res.Tokens
	t.Input, t.CachedInput, t.Output = t.Input+out.Tokens.Input, t.CachedInput+out.Tokens.CachedInput, t.Output+out.Tokens.Output
	if out.Model != "" {
		r.res.Model = out.Model
	}
	return out, err
}

// take adds a session's areas to the run and their patterns to claims. An
// area named like an earlier session's joins it, a promise given replacing
// the earlier one. With fresh set, a name this session already took gets
// "-2" (or the next free number); otherwise it joins too.
func (r *runner) take(list []Area, here map[string]bool, claims *[]sourceset.Claim, fresh bool) {
	for _, a := range list {
		name := a.Name
		for n := 2; fresh && here[name]; n++ {
			if name = fmt.Sprintf("%s-%d", a.Name, n); r.byName[name] == nil {
				break
			}
		}
		area := r.byName[name]
		if area == nil {
			area = &Area{Name: name}
			r.byName[name], r.areas = area, append(r.areas, area)
		}
		if a.Promise != "" {
			area.Promise = a.Promise
		}
		here[name] = true
		*claims = append(*claims, sourceset.Claim{Owner: name, Patterns: a.Sources})
	}
}

// group runs one grouping session over files, and its fix rounds, then
// places each file in the one area claiming it. The session's patterns claim
// only its own files, and a fix's exact paths win over them.
func (r *runner) group(system, message string, files []string, what string) error {
	id := draft.NewSessionID()
	out, err := r.turn(adapter.Spec{System: system, Prompt: message, SessionID: id}, what)
	if err != nil {
		return err
	}
	answer, err := ParseAreas(out.Text)
	if err != nil {
		return fmt.Errorf("the areas answer: %w", err)
	}
	here, ours, adds := map[string]bool{}, set(files), map[string]string{}
	var claims []sourceset.Claim
	r.take(answer, here, &claims, true)
	resolve := func() (map[string]string, []string, map[string][]string) {
		where, missing, twice := sourceset.Claims(files, claims)
		var still []string
		for _, p := range missing {
			if adds[p] == "" {
				still = append(still, p)
			}
		}
		for p, name := range adds {
			where[p] = name
			delete(twice, p)
		}
		return where, still, twice
	}
	where, missing, twice := resolve()
	for round := 1; (len(missing) > 0 || len(twice) > 0) && round <= MaxFixRounds; round++ {
		r.res.FixRounds++
		fix := FixPrompt(missing, twice)
		what := fmt.Sprintf("fix round %d sends back %d files", round, len(missing)+len(twice))
		out, err = r.turn(adapter.Spec{Prompt: fix, SessionID: id, Resume: true}, what)
		if errors.Is(err, adapter.ErrNoResume) {
			var w strings.Builder
			fenced(&w, "## Areas so far", r.briefs(where))
			id = draft.NewSessionID()
			out, err = r.turn(adapter.Spec{System: system, Prompt: w.String() + "\n" + fix, SessionID: id}, what+" to a new session, as the agent cannot resume")
		}
		if err != nil {
			return err
		}
		if f, err := ParseFix(out.Text); err == nil {
			r.take(f.NewAreas, here, &claims, false)
			for name, paths := range f.Add {
				for _, p := range paths {
					if p = strings.TrimPrefix(p, "./"); ours[p] && r.byName[name] != nil {
						adds[p] = name
					}
				}
			}
		}
		where, missing, twice = resolve()
	}
	if n := len(missing) + len(twice); n > 0 {
		for _, p := range files {
			if twice[p] != nil {
				missing = append(missing, p)
			}
		}
		return fmt.Errorf("%d source files are still in no area or in two after %d fix rounds, so no areas are stored: %s",
			n, MaxFixRounds, strings.Join(missing[:min(20, n)], ", "))
	}
	for _, p := range files {
		r.byName[where[p]].Files = append(r.byName[where[p]].Files, p)
	}
	r.drop(func(a *Area) bool { return len(a.Files) == 0 })
	return nil
}

// briefs is one Brief line per area so far. extra maps files not yet placed
// to the area claiming them, so they are counted too.
func (r *runner) briefs(extra map[string]string) string {
	var lines []string
	for _, a := range r.areas {
		b := *a
		b.Files = append([]string{}, a.Files...)
		for p, name := range extra {
			if name == a.Name {
				b.Files = append(b.Files, p)
			}
		}
		lines = append(lines, sourceset.Brief(b.Name, b.Promise, b.Files))
	}
	return strings.Join(lines, "\n")
}

// merge runs one prompt-only session over the briefs and applies the merges
// it names between existing areas, moving only whole areas. A failed or
// unreadable merge keeps the areas apart and says so in the result.
func (r *runner) merge() error {
	sys, msg := MergePrompt(r.briefs(nil))
	out, err := r.turn(adapter.Spec{System: sys, Prompt: msg, PromptOnly: true}, fmt.Sprintf("merge reads the briefs of %d areas", len(r.areas)))
	if cerr := r.ctx.Err(); cerr != nil {
		return cerr
	}
	var joints []Joint
	if err == nil {
		if joints, err = ParseMerge(out.Text); err != nil {
			err = fmt.Errorf("the merge answer: %w", err)
		}
	}
	if err != nil {
		r.res.MergeKept = err.Error() + "; the areas are kept apart"
		return nil
	}
	gone := map[*Area]bool{}
	for _, j := range joints {
		into := r.byName[j.Into]
		var from []string
		for _, name := range j.From {
			if a := r.byName[name]; into != nil && !gone[into] && a != nil && a != into && !gone[a] {
				into.Files, gone[a], from = append(into.Files, a.Files...), true, append(from, name)
			}
		}
		if len(from) > 0 {
			if j.Promise != "" {
				into.Promise = j.Promise
			}
			r.res.Merges = append(r.res.Merges, Joint{Into: into.Name, From: from, Promise: into.Promise, Why: j.Why})
		}
	}
	r.drop(func(a *Area) bool { return gone[a] })
	return nil
}

// drop removes the areas out names.
func (r *runner) drop(out func(*Area) bool) {
	kept := r.areas[:0]
	for _, a := range r.areas {
		if out(a) {
			delete(r.byName, a.Name)
		} else {
			kept = append(kept, a)
		}
	}
	r.areas = kept
}

// covers checks that every source file is in exactly one area. Only source
// files are ever placed, since each session claims only its own.
func (r *runner) covers(source []string) error {
	in := map[string]int{}
	for _, a := range r.areas {
		for _, p := range a.Files {
			in[p]++
		}
	}
	for _, p := range source {
		if in[p] != 1 {
			return fmt.Errorf("source file %s is in %d areas, not exactly one, so no areas are stored", p, in[p])
		}
	}
	return nil
}

func set(list []string) map[string]bool {
	out := make(map[string]bool, len(list))
	for _, s := range list {
		out[s] = true
	}
	return out
}
