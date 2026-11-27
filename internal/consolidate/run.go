package consolidate

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/capabilitytext"
	"github.com/dphaener/physa/internal/consolidateguard"
	"github.com/dphaener/physa/internal/consolidateprompt"
	"github.com/dphaener/physa/internal/consolidating"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/mapfolder"
	"github.com/dphaener/physa/internal/repeats"
)

// notJudged is why a lift candidate of a capability whose merge was never
// judged is refused.
const notJudged = "the capability merge did not judge it"

// Run is init's consolidation of the rows recovered from the stored walk,
// never of a stored consolidation: a session chooses which candidate groups
// of capabilities are one promise and code merges them; code merges exact
// duplicates; one session per area merges, moves and drops entries by id;
// sessions choose which rules repeated across areas the product's rules
// take. A session that fails or cannot be read leaves its step's items as
// they were, reported. One consolidation is stored only when the guard and
// the map check's form hold; otherwise, when ctx is cancelled or the cap
// stopped a session (s.Stopped), nothing is stored and the error says why. With no error, the exit code is the
// Result's ExitCode (0, or 1 when any step was left as it was); an error is
// exit code 2.
func Run(ctx context.Context, st *initrecord.Store, s Session) (Result, error) {
	in, err := read(ctx, st, s.Root)
	if err != nil {
		return Result{}, err
	}
	a, err := s.Agent()
	if err != nil {
		return Result{}, err
	}
	r := &runner{ctx: ctx, a: a, s: s, window: adapter.Window(a)}
	c := &run{runner: r, in: in, state: consolidating.New(cmp.Or(s.Product, "Product"), in.caps, nil), unjudged: map[string]bool{}}
	for _, f := range in.refused {
		c.state.Refuse(f)
	}
	for _, step := range []func(){c.merge, func() { c.state.ExactDuplicates(repeats.Normal) }, c.areas, c.lift} {
		if step(); ctx.Err() != nil {
			return Result{}, ctx.Err()
		} else if s.Stopped != nil && s.Stopped() {
			return Result{}, errors.New("the cap was reached, so nothing is stored")
		}
	}
	out, rec := c.state.Output(), c.state.Record()
	if bad := consolidateguard.Guard(in.caps, out, rec); len(bad) > 0 {
		var why []string
		for _, b := range bad {
			why = append(why, b.Item+": "+b.Reason)
		}
		return Result{}, errors.New("the consolidation fails its guard, so nothing is stored: " + strings.Join(why, "; "))
	}
	if err := formCheck(out, in.promise, s.Root); err != nil {
		return Result{}, err
	}
	res := in.result(out, rec)
	res.Sessions, res.Model, res.Tokens = append([]SessionRun{}, r.res.Sessions...), r.res.Model, r.res.Tokens
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	body, _ := json.Marshal(res) // plain values: it cannot fail
	t := res.Tokens
	return res, st.PutConsolidated(ctx, initrecord.Consolidated{WalkAt: in.walkAt, At: now().UTC(), Model: res.Model,
		Input: t.Input, CachedInput: t.CachedInput, Output: t.Output, Body: body})
}

// run is one consolidation under way.
type run struct {
	*runner
	in       input
	state    *consolidating.State
	unjudged map[string]bool // capabilities of candidate groups no merge session judged
}

// refuse records an item refused at step, with why.
func (c *run) refuse(step, item, why string) {
	c.state.Refuse(consolidating.Refusal{Step: step, Item: item, Reason: why})
}

// faults records each item of a reply that was not read.
func (c *run) faults(step string, fs []consolidateprompt.Fault) {
	for _, f := range fs {
		c.refuse(step, f.Item, f.Reason)
	}
}

// answered says whether q's session answered, recording it when not.
func (c *run) answered(q *ask) bool {
	if !q.run.Answered {
		c.refuse(q.step, "session "+q.item, "the session did not answer, so its step is left as it was: "+q.run.Error)
	}
	return q.run.Answered
}

// rules is every live rule of caps, as the finders read them.
func rules(caps []capabilitytext.Capability) []repeats.Rule {
	var out []repeats.Rule
	for _, c := range caps {
		for _, e := range c.Kinded(mapfolder.Rule) {
			out = append(out, repeats.Rule{ID: consolidating.ID(c.Name, e.Slug), Capability: c.Name, Area: c.Area.String(),
				Kind: mapfolder.Rule.String(), Heading: e.Heading, Body: e.Body()})
		}
	}
	return out
}

// merge is step 1: the candidate groups go to one session with every
// capability or, when that does not fit, to batches given only their own,
// halved until each fits; merges apply in batch order, then reply order.
func (c *run) merge() {
	caps := c.state.Capabilities()
	var rcaps []repeats.Capability
	for _, x := range caps {
		rcaps = append(rcaps, repeats.Capability{Name: x.Name, Area: x.Area.String()})
	}
	groups := repeats.Find(repeats.Input{Capabilities: rcaps, Renamed: c.in.renamed, Rules: rules(caps)}).Capabilities
	if len(groups) == 0 {
		return
	}
	batches, tooLarge := [][]repeats.CapabilityGroup{groups}, []repeats.CapabilityGroup(nil)
	every := c.fits(consolidateprompt.Merge, consolidateprompt.MergeMessage(caps, groups, true))
	if !every {
		batches, tooLarge = consolidateprompt.Fit(consolidateprompt.Merge, c.window, groups, func(gs []repeats.CapabilityGroup) string {
			return consolidateprompt.MergeMessage(caps, gs, false)
		})
	}
	for _, g := range tooLarge {
		c.refuse(StepMerge, "group "+consolidateprompt.Names(g), "its capabilities are more than one session holds, so they are left apart")
	}
	asks := make([]*ask, len(batches))
	replies := make([]consolidateprompt.MergeReply, len(batches))
	for i, b := range batches {
		asks[i] = &ask{step: StepMerge, item: fmt.Sprintf("batch %d of %d", i+1, len(batches)), system: consolidateprompt.Merge,
			msg:   consolidateprompt.MergeMessage(caps, b, every),
			parse: func(t string) (err error) { replies[i], err = consolidateprompt.ParseMerge(t); return err }}
	}
	c.all(asks)
	taken := map[string]bool{}
	for i, b := range batches {
		if !c.answered(asks[i]) {
			tooLarge = append(tooLarge, b...)
			continue
		}
		c.faults(StepMerge, replies[i].Faults)
		for _, m := range replies[i].Merge {
			c.mergeOne(m, b, taken)
		}
	}
	for _, g := range tooLarge {
		for _, m := range g.Capabilities {
			c.unjudged[m.Name] = true
		}
	}
}

// mergeOne checks a merge against its session's candidate groups and the
// merges so far, then merges the capabilities it names, in walk order.
func (c *run) mergeOne(m consolidateprompt.MergeItem, batch []repeats.CapabilityGroup, taken map[string]bool) {
	item := strings.Join(m.Capabilities, ", ") + " into " + m.Name
	if why := m.Check(batch); why != "" {
		c.refuse(StepMerge, item, why)
		return
	}
	for _, n := range m.Capabilities {
		if taken[n] {
			c.refuse(StepMerge, item, n+" is already in an earlier merge")
			return
		}
	}
	var group []string // in walk order, a name given twice kept twice for Merge to refuse
	for _, x := range c.state.Capabilities() {
		for _, n := range m.Capabilities {
			if n == x.Name {
				group = append(group, n)
			}
		}
	}
	if c.state.Merge(group, m.Name, m.PromiseOf, repeats.Normal) {
		for _, n := range group {
			taken[n] = true
		}
	}
}

// areaAsk is one session of an area and the capabilities it is given.
type areaAsk struct {
	area  capabilitytext.Area
	caps  []capabilitytext.Capability
	reply consolidateprompt.AreaReply
	q     *ask
}

// areas is step 3: one session per area with two live entries or more,
// split by capability into sessions that fit; choices apply once all have
// returned, in area order, then session order, then reply order.
func (c *run) areas() {
	fits := func(xs ...capabilitytext.Capability) bool {
		return c.fits(consolidateprompt.Consolidate, consolidateprompt.AreaMessage(xs))
	}
	var jobs []*areaAsk
	var asks []*ask
	for _, a := range c.state.Areas() {
		var parts [][]capabilitytext.Capability
		for _, x := range c.state.Capabilities() {
			switch {
			case x.Area != a:
			case !fits(x):
				c.refuse(StepArea, "capability "+x.Name, "its entries are more than one session holds, so they are left as they are")
			case len(parts) > 0 && fits(append(slices.Clone(parts[len(parts)-1]), x)...):
				parts[len(parts)-1] = append(parts[len(parts)-1], x)
			default:
				parts = append(parts, []capabilitytext.Capability{x})
			}
		}
		for i, p := range parts {
			n := 0
			for _, x := range p {
				n += len(x.Entries())
			}
			if n < 2 {
				continue
			}
			j := &areaAsk{area: a, caps: p}
			item := a.String()
			if len(parts) > 1 {
				item += fmt.Sprintf(" (part %d of %d)", i+1, len(parts))
			}
			j.q = &ask{step: StepArea, item: item, system: consolidateprompt.Consolidate, msg: consolidateprompt.AreaMessage(p),
				parse: func(t string) (err error) { j.reply, err = consolidateprompt.ParseArea(t); return err }}
			jobs, asks = append(jobs, j), append(asks, j.q)
		}
	}
	c.all(asks)
	for _, j := range jobs {
		if c.answered(j.q) {
			ch, unshown := j.reply.Choices(j.caps)
			c.faults(StepArea, append(j.reply.Faults, unshown...))
			c.state.ApplyArea(j.area, ch)
		}
	}
}

// lift is step 4: rules repeated across areas after steps 1 to 3 go to
// sessions in batches of LiftBatch groups, halved until each fits, and lifts
// apply in batch order, then reply order. A group holding a rule of a
// capability whose merge no session judged is refused, never lifted.
func (c *run) lift() {
	caps := c.state.Capabilities()
	var groups []repeats.RuleGroup
	for _, g := range repeats.RuleGroups(rules(caps)) {
		if slices.ContainsFunc(g.Rules, func(r repeats.Rule) bool { return c.unjudged[r.Capability] }) {
			ids, _ := consolidateprompt.Shown([]repeats.RuleGroup{g})
			c.refuse(StepLift, "group "+strings.Join(ids, ", "), notJudged)
			continue
		}
		groups = append(groups, g)
	}
	msg := func(gs []repeats.RuleGroup) string {
		return consolidateprompt.LiftMessage(c.in.promise, c.in.stated, caps, gs)
	}
	var batches [][]repeats.RuleGroup
	for i := 0; i < len(groups); i += LiftBatch {
		b, tooLarge := consolidateprompt.Fit(consolidateprompt.Lift, c.window, groups[i:min(i+LiftBatch, len(groups))], msg)
		batches = append(batches, b...)
		for _, g := range tooLarge {
			ids, _ := consolidateprompt.Shown([]repeats.RuleGroup{g})
			c.refuse(StepLift, "group "+strings.Join(ids, ", "), "its rules are more than one session holds, so they are left where they are")
		}
	}
	asks := make([]*ask, len(batches))
	replies := make([]consolidateprompt.LiftReply, len(batches))
	for i, b := range batches {
		asks[i] = &ask{step: StepLift, item: fmt.Sprintf("batch %d of %d", i+1, len(batches)), system: consolidateprompt.Lift, msg: msg(b),
			parse: func(t string) (err error) { replies[i], err = consolidateprompt.ParseLift(t); return err }}
	}
	c.all(asks)
	for i, b := range batches {
		if c.answered(asks[i]) {
			c.faults(StepLift, replies[i].Faults)
			shown, each := consolidateprompt.Shown(b)
			for _, l := range replies[i].Lift {
				c.state.Lift(consolidating.LiftChoice{Keep: l.Keep, With: l.With}, shown, each)
			}
		}
	}
}
