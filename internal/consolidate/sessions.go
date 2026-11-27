package consolidate

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/consolidateprompt"
	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/recovering"
)

// runner holds what every session of one consolidation shares.
type runner struct {
	a      adapter.Adapter
	s      Session
	window int

	mu  sync.Mutex // guards progress and res
	res Result
}

// ask is one session's question and, once it has run, its answer.
type ask struct {
	step, item, system, msg string
	parse                   func(string) error
	run                     SessionRun
}

// fits says whether a session of system and msg holds its prompt and reply.
func (r *runner) fits(system, msg string) bool { return recovering.Fits(system, msg, r.window) }

// all runs asks, at most Concurrency at once, and returns when every one has
// ended. Each keeps its own answer, so the order they end in never matters.
func (r *runner) all(ctx context.Context, asks []*ask) {
	slots := make(chan struct{}, Concurrency)
	var wg sync.WaitGroup
	for _, q := range asks {
		slots <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			r.one(ctx, q)
		}()
	}
	wg.Wait()
	for _, q := range asks {
		r.res.Sessions = append(r.res.Sessions, q.run)
	}
}

// one runs q's session, prompt-only, with one answer-now turn when its reply
// cannot be read, and leaves in q whether it answered or why not.
func (r *runner) one(ctx context.Context, q *ask) {
	q.run = SessionRun{Step: q.step, Item: q.item, IDs: []string{}}
	text, err := r.turn(ctx, q, "", q.msg, "asked")
	for try := 0; err == nil; try++ {
		perr := q.parse(text)
		switch {
		case perr == nil:
			q.run.Answered = true
			return
		case try == 1:
			err = fmt.Errorf("its reply could not be read: %v", perr)
		default:
			text, err = r.turn(ctx, q, q.run.IDs[len(q.run.IDs)-1], fmt.Sprintf(consolidateprompt.AnswerNow, perr), "asked to answer now")
		}
	}
	q.run.Error = err.Error()
}

// turn sends prompt to q's session: a new one when id is empty, else a
// resume of id. When the agent cannot resume, it starts a new session with
// q's message and then prompt. It adds what the session used to the result.
func (r *runner) turn(ctx context.Context, q *ask, id, prompt, what string) (string, error) {
	// A cancelled ctx starts no session.
	if err := ctx.Err(); err != nil {
		return "", err
	}
	spec := adapter.Spec{Prompt: prompt, PromptOnly: true, SessionID: id, Resume: id != "", Dir: r.s.Root}
	if !spec.Resume {
		spec.System, spec.SessionID = q.system, draft.NewSessionID()
	}
	var last atomic.Pointer[adapter.Tokens]
	spec.Usage = func(t adapter.Tokens) { last.Store(&t) }
	r.mu.Lock()
	if !spec.Resume {
		q.run.IDs = append(q.run.IDs, spec.SessionID)
	}
	if r.s.Progress != nil {
		fmt.Fprintf(r.s.Progress, "physa init: consolidate: %s %s: %s; session %s\n", q.step, q.item, what, spec.SessionID)
	}
	r.mu.Unlock()
	sess, err := r.a.Start(ctx, spec)
	if spec.Resume && errors.Is(err, adapter.ErrNoResume) {
		return r.turn(ctx, q, "", q.msg+"\n"+prompt, what+" in a new session, as the agent cannot resume")
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
