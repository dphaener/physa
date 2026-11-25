// Package workapi is the engine's API for ideas, pursuits and evidence: twelve methods, typed once, each change one command with its event.
package workapi

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/dphaener/physa/client"
	"github.com/dphaener/physa/internal/api"
	"github.com/dphaener/physa/internal/repos"
	"github.com/dphaener/physa/internal/signal"
	"github.com/dphaener/physa/internal/state"
	stateevidence "github.com/dphaener/physa/internal/state/evidence"
	"github.com/dphaener/physa/internal/state/work"
	"github.com/dphaener/physa/internal/tokens"
)

type getter func(ctx context.Context, repo string) (*state.State, error)

// PersonOnly reports whether method is one only a person may call: a pursuit is
// started and stopped by people (people-pursue).
func PersonOnly(method string) bool { return method == "pursuit.start" || method == "pursuit.stop" }

// Refusal is what a command says when method is called inside agent's
// session.
func Refusal(method, agent string) string {
	verb := "start"
	if method == "pursuit.stop" {
		verb = "stop"
	}
	return "A person must " + verb + " this pursuit: physa is running inside a " + agent + " session."
}

// open gets the repo's state; a repo the engine cannot serve is bad_request.
func (g getter) open(ctx context.Context, repo string) (*state.State, error) {
	st, err := g(ctx, repo)
	return st, refused(err)
}

// refused maps what a caller asked wrongly to bad_request and leaves faults
// as they are; nil stays nil.
func refused(err error) error {
	if errors.As(err, new(*repos.NotServedError)) || errors.As(err, new(*work.Refusal)) {
		return &api.Error{Code: api.CodeBadRequest, Message: err.Error()}
	}
	return err
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func signalOut(sig *signal.Signal) *client.SignalOut {
	if sig == nil {
		return nil
	}
	return &client.SignalOut{Claim: sig.Claim, For: sig.For, Against: sig.Against, Window: sig.Window, Resolver: sig.Resolver}
}

func ideaOut(f work.IdeaFacts) client.IdeaOut {
	return client.IdeaOut{Slug: f.Slug, Text: f.Text, Person: f.Person, CreatedAt: stamp(f.CreatedAt), Pursuit: f.Pursuit, PursuitState: f.PursuitState}
}

func pursuitOut(f work.PursuitFacts) client.PursuitOut {
	return client.PursuitOut{Slug: f.Slug, Idea: f.Idea, IdeaText: f.IdeaText, Person: f.Person, Appetite: f.Appetite, Spent: f.Spent, State: f.State,
		StopReason: f.StopReason, CreatedAt: stamp(f.CreatedAt), StartedAt: stamp(f.StartedAt), EndedAt: stamp(f.EndedAt)}
}

func ideaDetailOut(d work.IdeaDetail) client.IdeaOut {
	o := ideaOut(d.IdeaFacts)
	for _, b := range d.Pursuits {
		o.Pursuits = append(o.Pursuits, pursuitOut(b))
	}
	return o
}

func pursuitDetailOut(d work.PursuitDetail) client.PursuitOut {
	o := pursuitOut(d.PursuitFacts)
	o.Signal = signalOut(d.Signal)
	return o
}

// Methods returns the twelve methods, reading and changing the state get
// opens for the repo each call names.
func Methods(get func(ctx context.Context, repo string) (*state.State, error)) []api.Method {
	g := getter(get)
	const (
		repo        = `"repo":"/home/me/code/shop"`
		ideaJSON    = `{"slug":"try-a-quiet-mode","text":"Try a quiet mode","person":"ann","created_at":"2026-11-22T10:00:00Z"}`
		pursuitJSON = `{"slug":"try-a-quiet-mode","idea":"try-a-quiet-mode","idea_text":"Try a quiet mode","person":"ann","appetite":200000,"spent":0,"state":"active","created_at":"2026-11-22T10:00:00Z","started_at":"2026-11-22T10:00:00Z"}`
		slugNotes   = "An unknown slug replies bad_request naming it."
	)
	return append(append(planMethods(g), evidenceMethods(g)...), []api.Method{
		api.Define("idea.add", "Adds an idea, owned by the person who adds it.",
			[]string{"The slug is made from the text (up to five words) and numbered when taken; a slug that is given and taken replies bad_request.",
				"An idea has no named states and cannot be deleted."},
			`{`+repo+`,"text":"Try a quiet mode","person":"ann"}`, ideaJSON,
			func(ctx context.Context, _ *api.Conn, in client.AddIn) (client.IdeaOut, error) {
				st, err := g.open(ctx, in.Repo)
				if err != nil {
					return client.IdeaOut{}, err
				}
				f, err := work.AddIdea(ctx, st, work.IdeaIn{Text: in.Text, Slug: in.Slug, Person: in.Person})
				return ideaOut(f), refused(err)
			}),
		api.Define("idea.list", "Lists the repo's ideas, newest first, each with its facts.",
			[]string{"The result is [] when there are none."},
			`{`+repo+`}`, `[`+ideaJSON+`]`,
			func(ctx context.Context, _ *api.Conn, in client.RepoIn) ([]client.IdeaOut, error) {
				st, err := g.open(ctx, in.Repo)
				if err != nil {
					return nil, err
				}
				fs, err := work.Ideas(ctx, st)
				out := make([]client.IdeaOut, 0, len(fs))
				for _, f := range fs {
					out = append(out, ideaOut(f))
				}
				return out, refused(err)
			}),
		api.Define("idea.show", "Shows an idea with its pursuits.",
			[]string{slugNotes, "pursuits is left out when the idea was never pursued."},
			`{`+repo+`,"slug":"try-a-quiet-mode"}`, ideaJSON,
			func(ctx context.Context, _ *api.Conn, in client.SlugIn) (client.IdeaOut, error) {
				st, err := g.open(ctx, in.Repo)
				if err != nil {
					return client.IdeaOut{}, err
				}
				d, err := work.Idea(ctx, st, in.Slug)
				return ideaDetailOut(d), refused(err)
			}),
		api.Define("pursuit.start", "Starts a pursuit of an idea against a signal, with an appetite in tokens or none.",
			[]string{"The signal is required, every part of it: claim, for, against, window and resolver. A window that is not a date (YYYY-MM-DD) or is before today, or any part left empty, replies bad_request listing every problem on one line.",
				"The appetite is optional. With none there is no circuit breaker; spend is still counted.",
				"An idea with an active pursuit replies bad_request. Only people pursue: the CLI refuses inside an agent's session.", slugNotes},
			`{`+repo+`,"slug":"try-a-quiet-mode","appetite":"200k","signal":{"claim":"People turn on quiet mode in their first week.","for":"most new users turn it on","against":"few find it","window":"2026-12-15","resolver":"ann"},"person":"ann"}`, pursuitJSON,
			func(ctx context.Context, _ *api.Conn, in client.StartIn) (client.PursuitOut, error) {
				var appetite int64
				if in.Appetite != "" {
					var err error
					if appetite, err = tokens.Parse(in.Appetite); err != nil {
						return client.PursuitOut{}, &api.Error{Code: api.CodeBadRequest, Message: err.Error()}
					}
				}
				st, err := g.open(ctx, in.Repo)
				if err != nil {
					return client.PursuitOut{}, err
				}
				sig := signal.Signal{Claim: in.Signal.Claim, For: in.Signal.For, Against: in.Signal.Against, Window: in.Signal.Window, Resolver: in.Signal.Resolver}
				d, err := work.StartPursuit(ctx, st, work.PursueIn{Slug: in.Slug, Appetite: appetite, Signal: sig, Person: in.Person})
				if err == nil {
					if werr := stateevidence.Wake(ctx, st); werr != nil {
						log.Printf("pursuit.start %s: waking the evidence poll: %v", in.Slug, werr)
					}
				}
				return pursuitDetailOut(d), refused(err)
			}),
		api.Define("pursuit.list", "Lists the repo's pursuits, newest first.",
			[]string{"The result is [] when there are none."},
			`{`+repo+`}`, `[`+pursuitJSON+`]`,
			func(ctx context.Context, _ *api.Conn, in client.RepoIn) ([]client.PursuitOut, error) {
				st, err := g.open(ctx, in.Repo)
				if err != nil {
					return nil, err
				}
				fs, err := work.Pursuits(ctx, st)
				out := make([]client.PursuitOut, 0, len(fs))
				for _, f := range fs {
					out = append(out, pursuitOut(f))
				}
				return out, refused(err)
			}),
		api.Define("pursuit.show", "Shows a pursuit with its signal, its progress and its Needs you items.",
			[]string{slugNotes, "signal is left out for a pursuit started before signals were recorded.", "progress holds facts, never a percentage: planned, landed, in work, asks open, spent against the appetite, replans and spend per change.",
				"It reads the default branch for the pursuit's Change: trailers and records what landed; the pursuit is done when every planned change has. A version-control error leaves progress as the store holds it."},
			`{`+repo+`,"slug":"try-a-quiet-mode"}`, pursuitJSON,
			func(ctx context.Context, _ *api.Conn, in client.SlugIn) (client.PursuitOut, error) {
				st, err := g.open(ctx, in.Repo)
				if err != nil {
					return client.PursuitOut{}, err
				}
				o, err := showPursuit(ctx, st, in.Repo, in.Slug)
				return o, refused(err)
			}),
		api.Define("pursuit.stop", "Stops an active pursuit.",
			[]string{"Only an active pursuit stops; any other replies bad_request.", slugNotes},
			`{`+repo+`,"slug":"try-a-quiet-mode","reason":"not worth it","person":"ann"}`, pursuitJSON,
			func(ctx context.Context, _ *api.Conn, in client.StopIn) (client.PursuitOut, error) {
				st, err := g.open(ctx, in.Repo)
				if err != nil {
					return client.PursuitOut{}, err
				}
				d, err := work.StopPursuit(ctx, st, work.StopIn{Slug: in.Slug, Reason: in.Reason, Person: in.Person})
				return pursuitDetailOut(d), refused(err)
			}),
	}...)
}
