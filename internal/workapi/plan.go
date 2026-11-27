package workapi

import (
	"context"
	"log"
	"path/filepath"

	"github.com/dphaener/physa/client"
	"github.com/dphaener/physa/internal/api"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/plan"
	"github.com/dphaener/physa/internal/progress"
	"github.com/dphaener/physa/internal/repos"
	stateplan "github.com/dphaener/physa/internal/state/plan"
	"github.com/dphaener/physa/internal/state/work"
	"github.com/dphaener/physa/internal/vcs/git"
)

// capabilities reads the slugs of the capabilities in the repo's map, with
// git run in env to find its root; nil when the repo has no map, which
// plan.Parse then reports.
func capabilities(repo string, env []string) []string {
	root := repo
	if dir, err := repos.StoreDir(repo, env); err == nil {
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

func planOut(d stateplan.Detail) client.PlanOut {
	o := client.PlanOut{Pursuit: d.Pursuit, N: d.N, Replans: d.Replans, Reason: d.Reason, Person: d.Person, At: stamp(d.At), Changes: d.Changes}
	if o.Changes == nil {
		o.Changes = []plan.Change{}
	}
	for _, r := range d.History {
		o.History = append(o.History, client.RevisionOut{N: r.N, Reason: r.Reason, Person: r.Person, At: stamp(r.At), Changes: r.Changes})
	}
	return o
}

func progressOut(f stateplan.Facts) (*client.ProgressOut, []client.NeedOut) {
	p := &client.ProgressOut{Planned: orEmpty(f.Planned), InWork: orEmpty(f.InWork), Landed: []client.LandedOut{}, PerChange: []client.SpendPerOut{},
		AsksOpen: f.AsksOpen, Spent: f.Spent, Appetite: f.Appetite, Replans: f.Replans}
	for _, l := range f.Landed {
		p.Landed = append(p.Landed, client.LandedOut{Change: l.Change, Commit: l.Commit, At: stamp(l.At)})
	}
	for _, c := range f.PerChange {
		p.PerChange = append(p.PerChange, client.SpendPerOut{Change: c.Change, Tokens: c.Tokens})
	}
	var needs []client.NeedOut
	for _, n := range f.Needs {
		needs = append(needs, client.NeedOut{Kind: n.Kind, Text: n.Text, At: stamp(n.At)})
	}
	return p, needs
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// showPursuit reads a pursuit and its progress. It first looks at the default branch
// for the pursuit's Change: trailers and records what landed (and the pursuit done
// when every planned change has); a version-control error is logged and the
// progress shown is what the store holds. Git runs in env.
func showPursuit(ctx context.Context, st work.Submitter, env []string, repo, slug string) (client.PursuitOut, error) {
	d, err := work.Pursuit(ctx, st, slug)
	if err != nil {
		return client.PursuitOut{}, err
	}
	var landed map[string]progress.Landing
	if r, err := git.Open(ctx, repo, env); err != nil {
		log.Printf("pursuit.show %s: progress from version control: %v", slug, err)
	} else if landed, err = progress.Landed(r, slug, d.CreatedAt); err != nil {
		log.Printf("pursuit.show %s: progress from version control: %v", slug, err)
		landed = nil
	}
	if err := stateplan.Land(ctx, st, slug, landed); err != nil {
		return client.PursuitOut{}, err
	}
	if d, err = work.Pursuit(ctx, st, slug); err != nil {
		return client.PursuitOut{}, err
	}
	f, err := stateplan.Progress(ctx, st, slug)
	if err != nil {
		return client.PursuitOut{}, err
	}
	o := pursuitDetailOut(d)
	o.Progress, o.NeedsYou = progressOut(f)
	o.Evidence = pursuitEvidence(ctx, st, slug)
	return o, nil
}

func planMethods(g getter, env []string) []api.Method {
	const (
		slugNotes = "An unknown slug replies bad_request naming it."
		repo      = `"repo":"/home/me/code/shop"`
		planJSON  = `{"pursuit":"try-a-quiet-mode","n":1,"replans":0,"person":"ann","at":"2026-11-22T10:00:00Z","changes":[{"name":"quiet-flag","capability":"cli","what":"Add --quiet.","acceptance":["failure case: an unknown flag exits 2"],"must_not":[],"after":[]}]}`
	)
	return []api.Method{
		api.Define("plan.set", "Stores a pursuit's plan as its next revision.",
			[]string{"The plan is checked against the repo's map; a plan with problems replies bad_request listing each one: a change with no capability or two, a capability not in the map, an after naming an unknown change or making a cycle, acceptance with no failure case and no stated reason.",
				"A revision after the first needs a reason: what the plan missed. Only an active pursuit takes a plan."},
			`{`+repo+`,"pursuit":"try-a-quiet-mode","person":"ann","plan":{"changes":[{"name":"quiet-flag","capability":"cli","what":"Add --quiet.","acceptance":["failure case: an unknown flag exits 2"],"must_not":[],"after":[]}]}}`, planJSON,
			func(ctx context.Context, _ *api.Conn, in client.PlanSetIn) (client.PlanOut, error) {
				p, err := plan.Parse(in.Plan, capabilities(in.Repo, env))
				if err != nil {
					return client.PlanOut{}, &api.Error{Code: api.CodeBadRequest, Message: err.Error()}
				}
				st, err := g.open(ctx, in.Repo)
				if err != nil {
					return client.PlanOut{}, err
				}
				d, err := stateplan.Set(ctx, st, stateplan.SetIn{Pursuit: in.Pursuit, Reason: in.Reason, Person: in.Person, Plan: p})
				return planOut(d), refused(err)
			}),
		api.Define("plan.show", "Shows a pursuit's current plan, and every revision when asked.",
			[]string{"A pursuit with no plan replies bad_request saying how to set one.", slugNotes},
			`{`+repo+`,"pursuit":"try-a-quiet-mode","history":true}`, planJSON,
			func(ctx context.Context, _ *api.Conn, in client.PlanShowIn) (client.PlanOut, error) {
				st, err := g.open(ctx, in.Repo)
				if err != nil {
					return client.PlanOut{}, err
				}
				d, err := stateplan.Show(ctx, st, in.Pursuit, in.History)
				return planOut(d), refused(err)
			}),
		api.Define("pursuit.work", "Records the change a session works on.",
			[]string{"The change must be planned in the pursuit's current revision; a session's later call replaces its earlier change.",
				"The prepare-commit-msg hook reads it to add the Change: trailer to the session's commits."},
			`{`+repo+`,"pursuit":"try-a-quiet-mode","change":"quiet-flag","session":"abc-123"}`, `{"pursuit":"try-a-quiet-mode","change":"quiet-flag"}`,
			func(ctx context.Context, _ *api.Conn, in client.WorkIn) (client.WorkOut, error) {
				st, err := g.open(ctx, in.Repo)
				if err != nil {
					return client.WorkOut{}, err
				}
				err = stateplan.Work(ctx, st, stateplan.WorkIn{Session: in.Session, Pursuit: in.Pursuit, Change: in.Change})
				return client.WorkOut{Pursuit: in.Pursuit, Change: in.Change}, refused(err)
			}),
		api.Define("pursuit.spend", "Reports a session's running token total on a pursuit and change.",
			[]string{"It is how an adapter reports a session's tokens (step 6's adapters call it). Tokens is the session's running total, not an increase: reporting the same or a lower total changes nothing, so it is safe to repeat.",
				"The increase is added and the appetite checked in one transaction: a Needs you item at 80% of the appetite, and at 100% the circuit breaker stops the pursuit, and the idea may be pursued again. A pursuit with no appetite has neither; its spend is still counted. The appetite is never raised."},
			`{`+repo+`,"pursuit":"try-a-quiet-mode","change":"quiet-flag","session":"abc-123","tokens":52000}`, `{"spent":52000,"appetite":200000,"state":"active","warned":false,"broke":false}`,
			func(ctx context.Context, _ *api.Conn, in client.SpendIn) (client.SpendOut, error) {
				st, err := g.open(ctx, in.Repo)
				if err != nil {
					return client.SpendOut{}, err
				}
				o, err := stateplan.Spend(ctx, st, stateplan.SpendIn{Pursuit: in.Pursuit, Change: in.Change, Session: in.Session, Tokens: in.Tokens})
				return client.SpendOut{Spent: o.Spent, Appetite: o.Appetite, State: o.State, Warned: o.Warned, Broke: o.Broke}, refused(err)
			}),
	}
}
