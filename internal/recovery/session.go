package recovery

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/reading"
	"github.com/dphaener/physa/internal/recovering"
)

// runner holds what every capability's session shares.
type runner struct {
	agent  adapter.Adapter
	ledger *ledger
	check  checker

	mu       sync.Mutex // guards progress
	progress io.Writer
}

// all runs jobs in order, at most Concurrency at once, and returns one
// outcome per job. Each first attempt's reservation is queued in the ledger
// before any starts; once the context ends, jobs not started are not yet
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
				r.ledger.start(jobs[i].reserve, run)
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
// whose reservation the caller holds, then a resumed attempt on the same
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
	reserved := j.reserve
	dir, err := os.MkdirTemp("", "physa-recover-")
	if err != nil {
		r.ledger.settle(reserved, adapter.Tokens{})
		return fail(err)
	}
	defer os.RemoveAll(dir)

	spec := adapter.Spec{Prompt: j.prompt, Dir: dir, SessionID: out.SessionID, PromptOnly: true}
	var last *answer      // the last answer that was read and checked
	var problems []string // what is wrong with the newest answer
	starved := false      // the appetite ended the session before its retries did
	for {
		res, stopped, err := attempt(ctx, r.agent, spec, reserved)
		r.ledger.settle(reserved, res.Tokens)
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
		if stopped {
			starved = true
			break
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
		message := recovering.Retry(problems)
		reserved = res.Tokens.Input + res.Tokens.Output + int64(reading.Tokens(message)+ReplyReserve)
		if !r.ledger.reserve(reserved) {
			starved = true
			break
		}
		spec.Prompt, spec.Resume = message, true
	}
	switch {
	case last != nil:
		if err := r.check.settle(c.Name, *last, &out); err != nil {
			return fail(err)
		}
	case starved:
		out.Status = StatusNotYet
	default:
		out.Status, out.Problems = StatusFailed, problems
	}
	return out
}

// attempt runs one turn of a session and waits for it. Its circuit breaker
// stops the session when a report of its tokens, cached input included,
// reaches limit: stopped is then true, with no text and a nil error, as it
// is when the result alone shows the limit reached. The result's tokens are
// the last the session reported when it ended without a count.
func attempt(ctx context.Context, a adapter.Adapter, spec adapter.Spec, limit int64) (res adapter.Result, stopped bool, err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	var last adapter.Tokens
	tripped := false
	over := func(t adapter.Tokens) bool { return t.Input+t.Output >= limit }
	spec.Usage = func(t adapter.Tokens) {
		mu.Lock()
		defer mu.Unlock()
		last = t
		if over(t) {
			tripped = true
			cancel()
		}
	}
	s, err := a.Start(ctx, spec)
	if err != nil {
		return adapter.Result{}, false, err
	}
	res, err = s.Wait()
	mu.Lock()
	defer mu.Unlock()
	if tripped || res.Tokens == (adapter.Tokens{}) {
		res.Tokens = last
	}
	if tripped || over(res.Tokens) {
		res.Text = ""
		return res, true, nil
	}
	if err != nil {
		res.Text = ""
	}
	return res, false, err
}
