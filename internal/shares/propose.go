package shares

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/mapping/shareview"
	"github.com/dphaener/physa/internal/reading"
	"github.com/dphaener/physa/internal/store"
)

// Propose is init's propose phase end to end, in one session or in shares.
// It reads the stored gather and resolves the agent once. When the whole
// prompt with its reply reserve fits the window the agent states, or the
// agent states none, it is mapping.Propose, unchanged. Otherwise, and when
// that one session is refused as too long, it lays the sources into shares
// (Lay), runs one session per share, one at a time, each reading its sources
// whole, then a merge session that reads only the shares' checked proposals
// and may add no link (CheckLinks), and stores the merged proposal with
// every session it ran.
//
// A share's session refused as too long halves the room: its sources and
// those of every share not yet run are laid again, in order, and the shares
// already run are kept. A source too large for one share is a *TooLarge,
// returned before any share's session starts when the window is right. No
// session has an appetite or a circuit breaker: init is never bounded.
//
// With no gather it returns mapping.ErrNoGather and resolves no agent. An
// agent that fails, or a reply that cannot be read (the error starts "the
// session's output: "), stores nothing; the Result then still says what the
// sessions used.
func Propose(ctx context.Context, st mapping.Store, s mapping.Session) (mapping.Result, error) {
	g, ok, err := st.Gathered()
	if err != nil {
		return mapping.Result{}, err
	}
	if !ok {
		return mapping.Result{}, mapping.ErrNoGather
	}
	if s.Agent == nil {
		return mapping.Result{}, errors.New("no agent to propose with")
	}
	a, err := s.Agent()
	if err != nil {
		return mapping.Result{}, err
	}
	in, window := mapping.Read(g), adapter.Window(a)
	p := &proposing{ctx: ctx, a: a, g: g, out: s.Progress, rest: in, room: window - mapping.ReplyReserve}
	if Fits(in, window) {
		one := s
		one.Agent = func() (adapter.Adapter, error) { return a, nil }
		res, err := mapping.Propose(ctx, st, one)
		if !errors.Is(err, adapter.ErrTooLong) {
			return res, err
		}
		// The window was wrong, or unknown: go on in shares of half of it.
		if window == 0 {
			p.room = res.PromptTokens - mapping.ReplyReserve
		}
		if err := p.halve(mapping.Prompt(in), err); err != nil {
			return p.res, err
		}
		p.say("physa init: propose's session was refused as too long; it goes on in shares, each with room for ~%s\n", tokens(p.room))
	} else if err := p.lay(); err != nil {
		return p.res, err
	}
	if err := p.shares(); err != nil {
		return p.res, err
	}
	if err := p.merge(); err != nil {
		return p.res, err
	}
	p.res.Read = in.Report

	body, err := json.Marshal(mapping.Stored{Proposal: p.res.Proposal, Dropped: p.res.Dropped, Malformed: p.res.Malformed, Read: read(p.res), Sessions: p.ids})
	if err != nil {
		return p.res, err
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	err = st.PutProposal(initrecord.Proposal{
		GatherAt: g.Run.At, At: now().UTC(),
		SessionID: p.res.SessionID, Model: p.res.Model,
		Input: p.res.Tokens.Input, CachedInput: p.res.Tokens.CachedInput, Output: p.res.Tokens.Output,
		Body: body,
	})
	return p.res, err
}

// proposing is one run of propose in shares.
type proposing struct {
	ctx context.Context
	a   adapter.Adapter
	g   store.Gather
	out io.Writer

	// room is the most estimated tokens one share's prompt may take.
	room int
	// rest is what no finished share has read; laid is rest in shares of at
	// most room, and shared says every one of them reads all of rest's docs.
	rest   reading.Input
	laid   []reading.Input
	shared bool

	// props are the finished shares' checked proposals, drops and malformed
	// what was taken out of their replies, and ids every session that ran.
	props     []mapping.Proposal
	drops     mapping.Dropped
	malformed []mapping.Malformed
	ids       []string
	res       mapping.Result
}

func (p *proposing) say(format string, args ...any) {
	if p.out != nil {
		fmt.Fprintf(p.out, format, args...)
	}
}

// lay lays rest into shares of at most room.
func (p *proposing) lay() error {
	laid, err := Lay(p.rest, p.room)
	if err != nil {
		return err
	}
	docs := 0
	for _, s := range laid {
		docs += len(s.Docs)
	}
	p.laid, p.shared = laid, docs != len(p.rest.Docs)
	return nil
}

// halve halves the room after a session with prompt refused was refused as
// too long with cause, and lays rest again. It halves until the first share
// is no longer the refused prompt, so that a refusal is never asked again;
// that costs no session. It returns the *TooLarge when a source then fits no
// share, and cause when there is nothing left to halve.
func (p *proposing) halve(refused string, cause error) error {
	for {
		if p.room /= 2; p.room <= 0 {
			return cause
		}
		if err := p.lay(); err != nil {
			return fmt.Errorf("a session was refused as too long, and with the room for a share halved: %w", err)
		}
		if mapping.Prompt(p.laid[0]) != refused {
			return nil
		}
	}
}

// run runs one prompt-only session and counts what it used.
func (p *proposing) run(prompt, id string, read reading.Report) (draft.Outcome, mapping.Share, error) {
	out, err := draft.Run(p.ctx, p.a, prompt, math.MaxInt64, id) // no circuit breaker: init is never bounded
	sh := mapping.Share{Read: read, PromptTokens: reading.Tokens(prompt), SessionID: out.SessionID, Model: out.Model, Tokens: out.Tokens}
	if errors.Is(err, adapter.ErrTooLong) {
		return out, sh, err
	}
	p.res.SessionID, p.res.Model = out.SessionID, out.Model
	p.res.PromptTokens += sh.PromptTokens
	p.res.Tokens.Input += out.Tokens.Input
	p.res.Tokens.CachedInput += out.Tokens.CachedInput
	p.res.Tokens.Output += out.Tokens.Output
	if err == nil && out.SessionID != "" {
		p.ids = append(p.ids, out.SessionID)
	}
	return out, sh, err
}

// shares runs the laid shares one at a time, laying again at half the room
// whenever one is refused as too long.
func (p *proposing) shares() error {
	for len(p.laid) > 0 {
		share := p.laid[0]
		i, k := len(p.res.Shares)+1, len(p.res.Shares)+len(p.laid)
		prompt, id := mapping.Prompt(share), draft.NewSessionID()
		p.say("physa init: propose reads share %d of %d whole, ~%s, in a prompt of ~%s; session %s\n",
			i, k, tokens(share.Report.Tokens), tokens(reading.Tokens(prompt)), id)
		out, sh, err := p.run(prompt, id, share.Report)
		if errors.Is(err, adapter.ErrTooLong) {
			if err := p.halve(prompt, err); err != nil {
				return err
			}
			p.say("physa init: share %d of %d was refused as too long; the room for a share is halved, to ~%s, and its sources and the later shares' are laid again\n",
				i, k, tokens(p.room))
			continue
		}
		p.res.Shares = append(p.res.Shares, sh)
		if err != nil {
			return err
		}
		reply, malformed, err := mapping.Parse(out.Text)
		if err != nil {
			return fmt.Errorf("the session's output: share %d of %d: %w", i, k, err)
		}
		prop, dropped := mapping.Check(reply, p.g)
		p.props = append(p.props, prop)
		for _, d := range dropped {
			d.Reason = fmt.Sprintf("in share %d: %s", i, d.Reason)
			p.drops = append(p.drops, d)
		}
		for _, m := range malformed {
			m.Reason = fmt.Sprintf("in share %d: %s", i, m.Reason)
			p.malformed = append(p.malformed, m)
		}
		if !p.shared {
			p.rest.Docs = p.rest.Docs[len(share.Docs):]
		}
		p.rest.PRs = p.rest.PRs[len(share.PRs):]
		p.rest.Commits = p.rest.Commits[len(share.Commits):]
		p.rest.Clusters = p.rest.Clusters[len(share.Clusters):]
		p.laid = p.laid[1:]
	}
	return nil
}

// merge runs the merge session over the shares' checked proposals and
// checks its reply: no link no share gave, then against the gather. A merge
// refused as too long fails: it reads no source, so nothing can be halved.
func (p *proposing) merge() error {
	prompt, id := MergePrompt(p.props), draft.NewSessionID()
	p.say("physa init: propose merges the proposals of its %d shares, in a prompt of ~%s; session %s\n",
		len(p.props), tokens(reading.Tokens(prompt)), id)
	out, sh, err := p.run(prompt, id, reading.Report{})
	if err != nil {
		if errors.Is(err, adapter.ErrTooLong) {
			err = fmt.Errorf("the merge of the shares' proposals: %w", err)
		}
		return err
	}
	p.res.Merge = &sh
	reply, malformed, err := mapping.Parse(out.Text)
	if err != nil {
		return fmt.Errorf("the session's output: the merge: %w", err)
	}
	reply, links := CheckLinks(reply, p.props)
	prop, dropped := mapping.Check(reply, p.g)
	p.res.Proposal = prop
	p.res.Dropped = append(append(links, dropped...), p.drops...)
	p.res.Malformed = append(append([]mapping.Malformed{}, malformed...), p.malformed...)
	return nil
}

// read is what a proposal in shares read, in its JSON form.
func read(r mapping.Result) mapping.Reading {
	lines := r.Read.Lines()
	if lines == nil {
		lines = []string{}
	}
	f := mapping.Reading{PromptTokens: r.PromptTokens, SourceTokens: r.Read.Tokens, Sources: shareview.Sources(r.Read), Lines: lines}
	for _, s := range r.Shares {
		f.Shares = append(f.Shares, s.JSON())
	}
	if r.Merge != nil {
		m := r.Merge.JSON()
		f.Merge = &m
	}
	return f
}

func tokens(n int) string {
	if n == 1 {
		return "1 token"
	}
	return fmt.Sprintf("%d tokens", n)
}

// Estimate is what init's estimate counts for propose of g with an agent
// whose window is window: the prompts' estimated tokens together, the reply
// reserves, and the number of shares. When the prompt fits one session (or
// window is 0) that is mapping.Estimate, one reply reserve and 0 shares.
// Otherwise it is every share's prompt and the merge's (MergeTokens), a
// reply reserve for each share and the merge, and the shares Propose would
// run. A source too large for one share gives the *TooLarge.
func Estimate(g store.Gather, window int) (prompt, reply, shares int, err error) {
	in := mapping.Read(g)
	if Fits(in, window) {
		return mapping.Estimate(g), mapping.ReplyReserve, 0, nil
	}
	laid, err := Lay(in, window-mapping.ReplyReserve)
	if err != nil {
		return 0, 0, 0, err
	}
	for _, s := range laid {
		prompt += reading.Tokens(mapping.Prompt(s))
	}
	k := len(laid)
	return prompt + MergeTokens(k), (k + 1) * mapping.ReplyReserve, k, nil
}
