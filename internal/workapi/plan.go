package workapi

import (
	"context"
	"log"
	"path/filepath"

	"github.com/dphaener/physa/internal/api"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/plan"
	"github.com/dphaener/physa/internal/progress"
	"github.com/dphaener/physa/internal/repos"
	stateplan "github.com/dphaener/physa/internal/state/plan"
	"github.com/dphaener/physa/internal/state/work"
	"github.com/dphaener/physa/internal/vcs/git"
)

// capabilities reads the slugs of the capabilities in the repo's map; nil
// when the repo has no map, which plan.Parse then reports.
func capabilities(repo string) []string {
	root := repo
	if dir, err := repos.StoreDir(repo); err == nil {
		root = filepath.Dir(filepath.Dir(dir))
	}
	m, _, err := mapfile.Read(root)
	if err != nil || m == nil {
		return nil
	}
	var out []string
	for _, f := range m.Capabilities {
		out = append(out, f.Name)
	}
	return out
}

func planOut(d stateplan.Detail) PlanOut {
	o := PlanOut{Bet: d.Bet, N: d.N, Replans: d.Replans, Reason: d.Reason, Person: d.Person, At: stamp(d.At), Changes: d.Changes}
	if o.Changes == nil {
		o.Changes = []plan.Change{}
	}
	for _, r := range d.History {
		o.History = append(o.History, RevisionOut{r.N, r.Reason, r.Person, stamp(r.At), r.Changes})
	}
	return o
}

func progressOut(f stateplan.Facts) (*ProgressOut, []NeedOut) {
	p := &ProgressOut{Planned: orEmpty(f.Planned), InWork: orEmpty(f.InWork), Landed: []LandedOut{}, PerChange: []SpendPerOut{},
		AsksOpen: f.AsksOpen, Spent: f.Spent, Appetite: f.Appetite, Replans: f.Replans}
	for _, l := range f.Landed {
		p.Landed = append(p.Landed, LandedOut{l.Change, l.Commit, stamp(l.At)})
	}
	for _, c := range f.PerChange {
		p.PerChange = append(p.PerChange, SpendPerOut{c.Change, c.Tokens})
	}
	var needs []NeedOut
	for _, n := range f.Needs {
		needs = append(needs, NeedOut{n.Kind, n.Text, stamp(n.At)})
	}
	return p, needs
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// showBet reads a bet and its progress. It first looks at the default branch
// for the bet's Change: trailers and records what landed (and the bet done
// when every planned change has); a version-control error is logged and the
// progress shown is what the store holds.
func showBet(ctx context.Context, st work.Submitter, repo, slug string) (BetOut, error) {
	d, err := work.Bet(ctx, st, slug)
	if err != nil {
		return BetOut{}, err
	}
	var landed map[string]progress.Landing
	if r, err := git.Open(repo); err != nil {
		log.Printf("bet.show %s: progress from version control: %v", slug, err)
	} else if landed, err = progress.Landed(r, slug, d.CreatedAt); err != nil {
		log.Printf("bet.show %s: progress from version control: %v", slug, err)
		landed = nil
	}
	if err := stateplan.Land(ctx, st, slug, landed); err != nil {
		return BetOut{}, err
	}
	if d, err = work.Bet(ctx, st, slug); err != nil {
		return BetOut{}, err
	}
	f, err := stateplan.Progress(ctx, st, slug)
	if err != nil {
		return BetOut{}, err
	}
	o := betDetailOut(d)
	o.Progress, o.NeedsYou = progressOut(f)
	o.Evidence = betEvidence(ctx, st, slug)
	return o, nil
}

func planMethods(g getter) []api.Method {
	const (
		slugNotes = "An unknown slug replies bad_request naming it."
		repo      = `"repo":"/home/me/code/shop"`
		planJSON  = `{"bet":"try-a-quiet-mode","n":1,"replans":0,"person":"ann","at":"2026-11-22T10:00:00Z","changes":[{"name":"quiet-flag","capability":"cli","what":"Add --quiet.","acceptance":["failure case: an unknown flag exits 2"],"must_not":[],"after":[]}]}`
	)
	return []api.Method{
		api.Define("plan.set", "Stores a bet's plan as its next revision.",
			[]string{"The plan is checked against the repo's map; a plan with problems replies bad_request listing each one: a change with no capability or two, a capability not in the map, an after naming an unknown change or making a cycle, acceptance with no failure case and no stated reason.",
				"A revision after the first needs a reason: what shaping missed. Only an active bet takes a plan."},
			`{`+repo+`,"bet":"try-a-quiet-mode","person":"ann","plan":{"changes":[{"name":"quiet-flag","capability":"cli","what":"Add --quiet.","acceptance":["failure case: an unknown flag exits 2"],"must_not":[],"after":[]}]}}`, planJSON,
			func(ctx context.Context, _ *api.Conn, in PlanSetIn) (PlanOut, error) {
				p, err := plan.Parse(in.Plan, capabilities(in.Repo))
				if err != nil {
					return PlanOut{}, &api.Error{Code: api.CodeBadRequest, Message: err.Error()}
				}
				st, err := g.open(ctx, in.Repo)
				if err != nil {
					return PlanOut{}, err
				}
				d, err := stateplan.Set(ctx, st, stateplan.SetIn{Bet: in.Bet, Reason: in.Reason, Person: in.Person, Plan: p})
				return planOut(d), refused(err)
			}),
		api.Define("plan.show", "Shows a bet's current plan, and every revision when asked.",
			[]string{"A bet with no plan replies bad_request saying how to set one.", slugNotes},
			`{`+repo+`,"bet":"try-a-quiet-mode","history":true}`, planJSON,
			func(ctx context.Context, _ *api.Conn, in PlanShowIn) (PlanOut, error) {
				st, err := g.open(ctx, in.Repo)
				if err != nil {
					return PlanOut{}, err
				}
				d, err := stateplan.Show(ctx, st, in.Bet, in.History)
				return planOut(d), refused(err)
			}),
		api.Define("bet.work", "Records the change a session works on.",
			[]string{"The change must be planned in the bet's current revision; a session's later call replaces its earlier change.",
				"The prepare-commit-msg hook reads it to add the Change: trailer to the session's commits."},
			`{`+repo+`,"bet":"try-a-quiet-mode","change":"quiet-flag","session":"abc-123"}`, `{"bet":"try-a-quiet-mode","change":"quiet-flag"}`,
			func(ctx context.Context, _ *api.Conn, in WorkIn) (WorkOut, error) {
				st, err := g.open(ctx, in.Repo)
				if err != nil {
					return WorkOut{}, err
				}
				err = stateplan.Work(ctx, st, stateplan.WorkIn{Session: in.Session, Bet: in.Bet, Change: in.Change})
				return WorkOut{in.Bet, in.Change}, refused(err)
			}),
		api.Define("bet.spend", "Reports a session's running token total on a bet and change.",
			[]string{"It is how an adapter reports a session's tokens (step 6's adapters call it). Tokens is the session's running total, not an increase: reporting the same or a lower total changes nothing, so it is safe to repeat.",
				"The increase is added and the appetite checked in one transaction: a Needs you item at 80% of the appetite, and at 100% the circuit breaker stops the bet and it goes back to shaping. The appetite is never raised."},
			`{`+repo+`,"bet":"try-a-quiet-mode","change":"quiet-flag","session":"abc-123","tokens":52000}`, `{"spent":52000,"appetite":200000,"state":"active","warned":false,"broke":false}`,
			func(ctx context.Context, _ *api.Conn, in SpendIn) (SpendOut, error) {
				st, err := g.open(ctx, in.Repo)
				if err != nil {
					return SpendOut{}, err
				}
				o, err := stateplan.Spend(ctx, st, stateplan.SpendIn{Bet: in.Bet, Change: in.Change, Session: in.Session, Tokens: in.Tokens})
				return SpendOut{o.Spent, o.Appetite, o.State, o.Warned, o.Broke}, refused(err)
			}),
	}
}
