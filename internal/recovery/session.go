package recovery

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/recovering"
)

// runner holds what every capability's session shares.
type runner struct {
	agent adapter.Adapter
	check checker

	mu       sync.Mutex // guards progress
	progress io.Writer
}

// all runs jobs in order, at most Concurrency at once, and returns one
// outcome per job; once the context ends, jobs not started are not yet
// recovered.
func (r *runner) all(ctx context.Context, jobs []job) []Outcome {
	outcomes := make([]Outcome, len(jobs))
	var start sync.Mutex // held while the next job is taken, so jobs start in order
	var wg sync.WaitGroup
	next, halted := 0, false
	for range min(Concurrency, len(jobs)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				start.Lock()
				if next >= len(jobs) {
					start.Unlock()
					return
				}
				i := next
				next++
				halted = halted || ctx.Err() != nil
				run := !halted
				start.Unlock()

				if run {
					outcomes[i] = r.capability(ctx, jobs[i])
				} else {
					outcomes[i] = Outcome{Name: jobs[i].capability.Name, Status: StatusNotYet}
				}
				r.say(outcomes[i])
			}
		}()
	}
	wg.Wait()
	return outcomes
}

// say writes the progress line of a capability that ended.
func (r *runner) say(o Outcome) {
	if r.progress == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	line := fmt.Sprintf("physa init: %s: %s", o.Name, o.Status)
	if o.Attempts > 0 {
		line += fmt.Sprintf(" (%s; %s; %s)", kept(o.Counts), plural(o.Attempts, "attempt", "attempts"), o.Tokens)
	}
	fmt.Fprintln(r.progress, line)
}

// kept reads like "3 rules, 1 decision, 2 questions, 0 terms".
func kept(c recovering.Counts) string {
	return plural(c.Rules, "rule", "rules") + ", " + plural(c.Decisions, "decision", "decisions") + ", " +
		plural(c.Questions, "question", "questions") + ", " + plural(c.Terms, "term", "terms")
}

// capability runs one capability's session to its end: the first attempt,
// then a resumed attempt on the same
// session for each answer with problems, up to MaxRetries. Every attempt runs
// in one fresh directory, removed afterwards, since an agent finds a session
// to resume by its directory.
func (r *runner) capability(ctx context.Context, j job) Outcome {
	c := j.capability
	out := Outcome{Name: c.Name, SessionID: draft.NewSessionID()}
	fail := func(err error) Outcome {
		out.Status, out.Error = StatusFailed, err.Error()
		return out
	}
	dir, err := os.MkdirTemp("", "physa-recover-")
	if err != nil {
		return fail(err)
	}
	defer os.RemoveAll(dir)

	spec := adapter.Spec{Prompt: j.prompt, System: j.system, Dir: dir, SessionID: out.SessionID, PromptOnly: true}
	var last *answer      // the last answer that was read and checked
	var problems []string // what is wrong with the newest answer
	for {
		res, err := attempt(ctx, r.agent, spec)
		out.Attempts++
		add(&out.Tokens, res.Tokens)
		if res.Model != "" {
			out.Model = res.Model
		}
		if res.SessionID != "" {
			out.SessionID = res.SessionID
		}
		if err != nil {
			return fail(err)
		}
		reply, err := recovering.Parse(res.Text)
		if err != nil {
			problems = []string{"the session's output: " + err.Error()}
		} else {
			a := answer{file: recovering.File(c.Name, c.Promise, reply.File), terms: reply.Terms}
			if a.problems, err = r.check.run(c.Name, a.file, a.terms); err != nil {
				return fail(err)
			}
			last = &a
			if len(a.problems) == 0 {
				break
			}
			problems = describe(a)
		}
		if out.Attempts > MaxRetries {
			break
		}
		spec.Prompt, spec.Resume = recovering.Retry(problems), true
	}
	if last == nil {
		out.Status, out.Problems = StatusFailed, problems
		return out
	}
	if err := r.check.settle(c.Name, *last, &out); err != nil {
		return fail(err)
	}
	return out
}

// attempt runs one turn of a session and waits for it, with no circuit
// breaker. The result's tokens are the last the session reported when it
// ended without a count, and it has no text when the session failed.
func attempt(ctx context.Context, a adapter.Adapter, spec adapter.Spec) (adapter.Result, error) {
	var mu sync.Mutex
	var last adapter.Tokens
	spec.Usage = func(t adapter.Tokens) {
		mu.Lock()
		defer mu.Unlock()
		last = t
	}
	s, err := a.Start(ctx, spec)
	if err != nil {
		return adapter.Result{}, err
	}
	res, err := s.Wait()
	mu.Lock()
	defer mu.Unlock()
	if res.Tokens == (adapter.Tokens{}) {
		res.Tokens = last
	}
	if err != nil {
		res.Text = ""
	}
	return res, err
}
