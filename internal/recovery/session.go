package recovery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/reading"
	"github.com/dphaener/physa/internal/recovering"
	"github.com/dphaener/physa/internal/shares"
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

// sayf writes a progress line.
func (r *runner) sayf(format string, a ...any) {
	if r.progress == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	fmt.Fprintf(r.progress, format, a...)
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
		in := ""
		if o.Shares > 0 {
			in = fmt.Sprintf(" in %d shares", o.Shares)
		}
		line += fmt.Sprintf(" (%s; %s%s; %s)", kept(o.Counts), plural(o.Attempts, "attempt", "attempts"), in, o.Tokens)
	}
	fmt.Fprintln(r.progress, line)
}

// kept reads like "3 rules, 1 decision, 2 questions, 0 terms".
func kept(c recovering.Counts) string {
	return plural(c.Rules, "rule", "rules") + ", " + plural(c.Decisions, "decision", "decisions") + ", " +
		plural(c.Questions, "question", "questions") + ", " + plural(c.Terms, "term", "terms")
}

// capability runs one capability to its end: in one session, or, when its
// sources are more than one session holds, in shares, one session after
// another (shares.go), then settles its last answer.
func (r *runner) capability(ctx context.Context, j job) Outcome {
	c := j.capability
	out := Outcome{Name: c.Name, Shares: len(j.parts)}
	fail := func(err error) Outcome {
		out.Status, out.Error = StatusFailed, err.Error()
		return out
	}
	if j.err != nil {
		return fail(j.err)
	}
	var last *answer
	parts, room := j.parts, j.room
	for k := 0; k < max(1, len(parts)); k++ {
		system, prompt, read := j.system, j.prompt, j.rec.Sources.Report
		if len(parts) > 0 {
			read = parts[k].Report
			var earlier, terms string
			if last != nil {
				earlier, terms = last.file, last.terms
			}
			system, prompt = recovering.Split(j.rec.InShare(parts, k, earlier, terms))
		}
		a, problems, err := r.turns(ctx, c, adapter.Spec{Prompt: prompt, System: system, ReadOnly: true, Dir: r.check.root}, &out)
		if errors.Is(err, adapter.ErrTooLong) {
			if room = recovering.Half(room, prompt); room < recovering.MinRoom {
				return fail(fmt.Errorf("%w; with the room for a share halved to about %s tokens it still did not fit", tooLong(err, system, prompt, read), reading.Thousands(int64(room*2))))
			}
			laid, lerr := j.rec.Lay(shares.JoinCapability(parts[min(k, len(parts)):], j.rec.Sources), room)
			if lerr != nil {
				return fail(lerr)
			}
			r.sayf("physa init: %s: a session was refused as too long; the room for a share is halved, to about %s tokens, and its sources not yet read are laid again, into %d shares\n", c.Name, reading.Thousands(int64(room)), len(laid))
			parts, k = append(parts[:min(k, len(parts))], laid...), k-1
			out.Shares = len(parts)
			continue
		}
		if err != nil {
			return fail(err)
		}
		if a == nil {
			out.Status, out.Problems = StatusFailed, problems
			return out
		}
		last = a
	}
	if err := r.check.settle(c.Name, *last, &out); err != nil {
		return fail(err)
	}
	return out
}

// turns runs one session on spec, read-only in the repo's root, to its
// end: the first attempt, then a resumed attempt on the same session for
// each answer with problems, up to MaxRetries. When the agent cannot resume,
// a retry starts a new session on the first prompt followed by the retry. It
// adds its attempts, tokens, model and session id to out and returns its
// last answer that was read and checked, nil when none was, and what is
// wrong with the newest.
func (r *runner) turns(ctx context.Context, c mapping.Capability, spec adapter.Spec, out *Outcome) (*answer, []string, error) {
	spec.SessionID = draft.NewSessionID()
	out.SessionID = spec.SessionID
	first := spec.Prompt
	var last *answer      // the last answer that was read and checked
	var problems []string // what is wrong with the newest answer
	for tries := 0; ; tries++ {
		res, err := attempt(ctx, r.agent, spec)
		if spec.Resume && errors.Is(err, adapter.ErrNoResume) {
			spec.Prompt, spec.Resume, spec.SessionID = first+"\n\n"+spec.Prompt, false, draft.NewSessionID()
			out.SessionID = spec.SessionID
			r.sayf("physa init: %s: the agent cannot resume, so the retry starts a new session, %s\n", c.Name, spec.SessionID)
			res, err = attempt(ctx, r.agent, spec)
		}
		out.Attempts++
		add(&out.Tokens, res.Tokens)
		if res.Model != "" {
			out.Model = res.Model
		}
		if res.SessionID != "" {
			out.SessionID = res.SessionID
		}
		if err != nil {
			return last, problems, err
		}
		reply, err := recovering.Parse(res.Text)
		if err != nil {
			problems = []string{"the session's output: " + err.Error()}
		} else {
			a := answer{file: recovering.File(c.Name, c.Promise, reply.File), terms: reply.Terms}
			if a.problems, err = r.check.run(c.Name, a.file, a.terms); err != nil {
				return last, problems, err
			}
			last = &a
			if len(a.problems) == 0 {
				return last, nil, nil
			}
			problems = describe(a)
		}
		if tries >= MaxRetries {
			return last, problems, nil
		}
		spec.Prompt, spec.Resume = recovering.Retry(problems), true
	}
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
