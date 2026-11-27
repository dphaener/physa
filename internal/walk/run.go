package walk

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/areas"
	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/sourceset"
	"github.com/dphaener/physa/internal/vcs"
)

const (
	// Concurrency is the most sessions running at once.
	Concurrency = 3
	// MaxFixRounds is how often a session is sent back its answer's faults.
	MaxFixRounds = 2
	partFault    = "this area is already a part of a split area, and a part is never split again: name its capabilities under `new` and its shared files under `shared`"
	answerNow    = "Your answer could not be read: %v. Answer now, as the skill says: end with exactly one fenced block marked `json` and nothing after it."
	answerSoFar  = "## Your answer so far\n\n```json\n%s\n```\n\n"
	stillNot     = "still not whole after %d fix rounds (source files in no capability and not shared: %d, claimed twice: %d, paths outside the area: %d, tests or docs paths of the wrong kind: %d, owners of no source file: %d, other faults: %d)"
)

// Store is init's record as the walk uses it.
type Store interface {
	Areas(ctx context.Context) (initrecord.Areas, bool, error)
	PutWalk(context.Context, initrecord.Walk) error
}

// Session is the agent, the root its sessions read, progress and the clock.
type Session struct {
	Agent    func() (adapter.Adapter, error)
	Root     string
	Progress io.Writer
	Now      func() time.Time
}

// AreaResult is one area of a Result, or a part of the split area Parent.
type AreaResult struct {
	Name         string   `json:"name"`
	Parent       string   `json:"parent,omitempty"`
	Capabilities []string `json:"capabilities"`
	Owned        int      `json:"owned"`
	Shared       int      `json:"shared"`
	Parts        []string `json:"parts"`
}

// Result is what the walk ended with: each area in stored order, its parts
// after it, and the totals. Unclaimed is 0 when the walk was stored.
type Result struct {
	Areas        []AreaResult   `json:"areas"`
	Capabilities int            `json:"capabilities"`
	Owned        int            `json:"owned"`
	Shared       int            `json:"shared"`
	Unclaimed    int            `json:"unclaimed"`
	FixRounds    int            `json:"fix_rounds"`
	Renamed      []Rename       `json:"renamed"`
	Model        string         `json:"model"`
	Tokens       adapter.Tokens `json:"tokens"`
}

// Run is init's walk: one read-only session per stored area, at most
// Concurrency at once, names the area's capabilities with their footprints
// and its shared files, or splits it once into parts walked the same way.
// The walk is stored in init's record only when every area ended whole;
// otherwise the error names each that failed. Nothing reaches the repo or map.
func Run(ctx context.Context, st Store, s Session) (Result, error) {
	stored, _, err := st.Areas(ctx)
	if err != nil {
		return Result{}, err
	}
	from, ok, err := areas.Load(st)
	if err == nil && (!ok || len(from.Areas) == 0) {
		err = errors.New("no areas stored yet; run physa init --areas first")
	}
	if err != nil {
		return Result{}, err
	}
	a, err := s.Agent()
	if err != nil {
		return Result{}, err
	}
	r := &runner{ctx: ctx, a: a, s: s, split: from.Split, sizes: map[string]int{}}
	var jobs []*job
	for _, ar := range from.Areas {
		jobs = append(jobs, &job{system: Walk + "\n" + Split, area: Area{Name: ar.Name, Promise: ar.Promise, Files: ar.Files}})
		for _, f := range ar.Files {
			if l, err := sourceset.List(s.Root, []vcs.TrackedFile{{Path: f}}); err == nil && len(l.Files) == 1 {
				r.sizes[f] = l.Files[0].Lines
			}
		}
	}
	r.all(jobs)
	rec, failed := collect(jobs, &r.res)
	rec.Sessions = r.ids
	if len(failed) > 0 {
		return r.res, fmt.Errorf("%d of %d areas did not end whole, so no footprints are stored: %s", len(failed), len(r.res.Areas), strings.Join(failed, "; "))
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	body, _ := json.Marshal(rec) // strings only: it cannot fail
	t := r.res.Tokens
	return r.res, st.PutWalk(ctx, initrecord.Walk{AreasAt: stored.At, At: now().UTC(), Model: r.res.Model, Input: t.Input, CachedInput: t.CachedInput, Output: t.Output, Body: body})
}

// job is one area's walk, written only by the goroutine walking it.
type job struct {
	area              Area
	system, msg, id   string
	rounds, unclaimed int
	fp                Footprints
	parts             []*job
	err               error
}

// runner holds what every area's session shares.
type runner struct {
	ctx   context.Context
	a     adapter.Adapter
	s     Session
	split sourceset.Division
	sizes map[string]int

	mu  sync.Mutex // guards progress, res and ids
	res Result
	ids []string
}

// all walks jobs in order, at most Concurrency at once, each one's parts
// after it when it split, and returns when none is left.
func (r *runner) all(jobs []*job) {
	slots := make(chan struct{}, Concurrency)
	var wg sync.WaitGroup
	var start func(jobs []*job)
	start = func(jobs []*job) {
		for _, j := range jobs {
			slots <- struct{}{} // taken before the next job is, so jobs start in order
			wg.Add(1)
			go func() {
				defer wg.Done()
				j.err = r.walk(j)
				<-slots
				start(j.parts)
			}()
		}
	}
	start(jobs)
	wg.Wait()
}

// turn sends prompt to j's session, read-only in the root, and adds what it
// used to the result. The first prompt starts the session; when the agent
// cannot resume, a later one starts afresh after the area's message and sofar.
func (r *runner) turn(j *job, sofar, prompt, what string) (string, error) {
	spec := adapter.Spec{Prompt: prompt, SessionID: j.id, Resume: j.id != "", Dir: r.s.Root, ReadOnly: true}
	var last atomic.Pointer[adapter.Tokens]
	spec.Usage = func(t adapter.Tokens) { last.Store(&t) }
	r.mu.Lock()
	if !spec.Resume {
		j.id = draft.NewSessionID()
		spec.System, spec.SessionID = j.system, j.id
		r.ids = append(r.ids, j.id)
	}
	if r.s.Progress != nil {
		fmt.Fprintf(r.s.Progress, "physa init: walk: %s: %s; session %s\n", j.area.Name, what, spec.SessionID)
	}
	r.mu.Unlock()
	sess, err := r.a.Start(r.ctx, spec)
	if spec.Resume && errors.Is(err, adapter.ErrNoResume) {
		j.id = ""
		return r.turn(j, "", j.msg+"\n"+sofar+prompt, what+" to a new session, as the agent cannot resume")
	}
	if err != nil {
		return "", err
	}
	out, err := sess.Wait()
	if p := last.Load(); p != nil && out.Tokens == (adapter.Tokens{}) {
		out.Tokens = *p
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t := &r.res.Tokens
	t.Input, t.CachedInput, t.Output = t.Input+out.Tokens.Input, t.CachedInput+out.Tokens.CachedInput, t.Output+out.Tokens.Output
	r.res.Model = cmp.Or(out.Model, r.res.Model)
	return out.Text, err
}

// walk runs one area's session to its end: the answer, one more turn when
// it cannot be read, then a fix round for each answer a check faults. It
// leaves in j the footprints, or the parts of a top-level area that split.
func (r *runner) walk(j *job) error {
	a := j.area
	j.msg, j.unclaimed = Message(a, r.split, r.sizes), len(a.Files)
	text, err := r.turn(j, "", j.msg, "walking "+sourceset.FileCount(len(a.Files)))
	if err != nil {
		return err
	}
	ans, err := Parse(text)
	if err != nil {
		if text, err = r.turn(j, "", fmt.Sprintf(answerNow, err), "asked to answer now"); err != nil {
			return err
		}
		if ans, err = Parse(text); err != nil {
			return fmt.Errorf("the walk answer: %w", err)
		}
	}
	for round := 0; ; round++ {
		var p Problems
		var parts []Area
		switch {
		case len(ans.Capabilities) > 0:
			ans.Split = nil
			j.fp, p = Check(a, ans, r.split)
			j.unclaimed = len(p.Unclaimed) + len(p.Twice)
		case a.Parent != "":
			p.Names = []string{partFault}
		default:
			j.fp = Footprints{}
			parts, p = CheckSplit(a, ans)
		}
		if p.Empty() {
			for _, part := range parts {
				j.parts = append(j.parts, &job{area: part, system: Walk})
			}
			j.unclaimed = 0
			return nil
		}
		if round == MaxFixRounds {
			return fmt.Errorf(stillNot, MaxFixRounds, len(p.Unclaimed), len(p.Twice), len(p.Outside), len(p.NotTest)+len(p.NotDoc), len(p.NoSource), len(p.Names))
		}
		j.rounds++
		sofar, _ := json.Marshal(ans)
		if text, err = r.turn(j, fmt.Sprintf(answerSoFar, sofar), FixPrompt(p), fmt.Sprintf("fix round %d", round+1)); err != nil {
			return err
		}
		if whole, err := Parse(text); err == nil {
			ans = whole
		} else if fix, err := ParseFix(text); err == nil {
			ans = Apply(ans, fix)
		}
	}
}
