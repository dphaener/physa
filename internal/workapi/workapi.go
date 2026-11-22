// Package workapi is the engine's API for ideas, pitches and bets: eight methods, typed once, each change one command with its event.
package workapi

import (
	"context"
	"errors"
	"time"

	"github.com/dphaener/physa/internal/api"
	"github.com/dphaener/physa/internal/pitch"
	"github.com/dphaener/physa/internal/repos"
	"github.com/dphaener/physa/internal/state"
	"github.com/dphaener/physa/internal/state/work"
)

type getter func(ctx context.Context, repo string) (*state.State, error)

// PersonOnly reports whether method is one only a person may call: a bet is
// made and stopped by people (people-bet).
func PersonOnly(method string) bool { return method == "bet.start" || method == "bet.stop" }

// Refusal is what a command says when method is called inside agent's
// session.
func Refusal(method, agent string) string {
	verb := "make"
	if method == "bet.stop" {
		verb = "stop"
	}
	return "A person must " + verb + " this bet: physa is running inside a " + agent + " session."
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

func pitchOut(s *work.Shaped) *PitchOut {
	if s == nil {
		return nil
	}
	return &PitchOut{s.Problem, s.Appetite, s.Solution, s.Scope, s.Signal, s.RabbitHoles, s.NoGos, s.Person, stamp(s.At)}
}

func ideaOut(f work.IdeaFacts) IdeaOut {
	return IdeaOut{Slug: f.Slug, Text: f.Text, Person: f.Person, CreatedAt: stamp(f.CreatedAt), Pitches: f.Pitches, Bet: f.Bet, BetState: f.BetState}
}

func betOut(f work.BetFacts) BetOut {
	return BetOut{Slug: f.Slug, Idea: f.Idea, IdeaText: f.IdeaText, Person: f.Person, Appetite: f.Appetite, Spent: f.Spent, State: f.State,
		StopReason: f.StopReason, CreatedAt: stamp(f.CreatedAt), StartedAt: stamp(f.StartedAt), EndedAt: stamp(f.EndedAt)}
}

func ideaDetailOut(d work.IdeaDetail) IdeaOut {
	o := ideaOut(d.IdeaFacts)
	o.Pitch = pitchOut(d.Pitch)
	for _, b := range d.Bets {
		o.Bets = append(o.Bets, betOut(b))
	}
	return o
}

func betDetailOut(d work.BetDetail) BetOut {
	o := betOut(d.BetFacts)
	o.Pitch = pitchOut(d.Pitch)
	return o
}

// Methods returns the eight methods, reading and changing the state get
// opens for the repo each call names.
func Methods(get func(ctx context.Context, repo string) (*state.State, error)) []api.Method {
	g := getter(get)
	const (
		repo      = `"repo":"/home/me/code/shop"`
		ideaJSON  = `{"slug":"try-a-quiet-mode","text":"Try a quiet mode","person":"ann","created_at":"2026-11-22T10:00:00Z","pitches":0}`
		betJSON   = `{"slug":"try-a-quiet-mode","idea":"try-a-quiet-mode","idea_text":"Try a quiet mode","person":"ann","appetite":200000,"spent":0,"state":"active","created_at":"2026-11-22T10:00:00Z","started_at":"2026-11-22T10:00:00Z"}`
		slugNotes = "An unknown slug replies bad_request naming it."
	)
	return []api.Method{
		api.Define("idea.add", "Adds an idea, owned by the person who adds it.",
			[]string{"The slug is made from the text (up to five words) and numbered when taken; a slug that is given and taken replies bad_request.",
				"An idea has no named states and cannot be deleted."},
			`{`+repo+`,"text":"Try a quiet mode","person":"ann"}`, ideaJSON,
			func(ctx context.Context, _ *api.Conn, in AddIn) (IdeaOut, error) {
				st, err := g.open(ctx, in.Repo)
				if err != nil {
					return IdeaOut{}, err
				}
				f, err := work.AddIdea(ctx, st, work.IdeaIn{Text: in.Text, Slug: in.Slug, Person: in.Person})
				return ideaOut(f), refused(err)
			}),
		api.Define("idea.list", "Lists the repo's ideas, newest first, each with its facts.",
			[]string{"The result is [] when there are none."},
			`{`+repo+`}`, `[`+ideaJSON+`]`,
			func(ctx context.Context, _ *api.Conn, in RepoIn) ([]IdeaOut, error) {
				st, err := g.open(ctx, in.Repo)
				if err != nil {
					return nil, err
				}
				fs, err := work.Ideas(ctx, st)
				out := make([]IdeaOut, 0, len(fs))
				for _, f := range fs {
					out = append(out, ideaOut(f))
				}
				return out, refused(err)
			}),
		api.Define("idea.show", "Shows an idea with its latest pitch and its bets.",
			[]string{slugNotes, "pitch is left out when the idea was never shaped."},
			`{`+repo+`,"slug":"try-a-quiet-mode"}`, ideaJSON,
			func(ctx context.Context, _ *api.Conn, in SlugIn) (IdeaOut, error) {
				st, err := g.open(ctx, in.Repo)
				if err != nil {
					return IdeaOut{}, err
				}
				d, err := work.Idea(ctx, st, in.Slug)
				return ideaDetailOut(d), refused(err)
			}),
		api.Define("idea.shape", "Shapes an idea into a pitch of seven parts, or shapes it again.",
			[]string{"The markdown is checked: every part must be there and filled, and the appetite must parse; a pitch with problems replies bad_request listing each one.",
				"Shaping again adds a pitch; the earlier ones stay as events.", slugNotes},
			`{`+repo+`,"slug":"try-a-quiet-mode","markdown":"## Problem\n...\n## Appetite\n200k\n...","person":"ann"}`, ideaJSON,
			func(ctx context.Context, _ *api.Conn, in ShapeIn) (IdeaOut, error) {
				p, err := pitch.Parse(in.Markdown)
				if err != nil {
					return IdeaOut{}, &api.Error{Code: api.CodeBadRequest, Message: err.Error()}
				}
				st, err := g.open(ctx, in.Repo)
				if err != nil {
					return IdeaOut{}, err
				}
				d, err := work.Shape(ctx, st, work.ShapeIn{Slug: in.Slug, Pitch: p, Person: in.Person})
				return ideaDetailOut(d), refused(err)
			}),
		api.Define("bet.start", "Makes a bet on an idea, with an appetite in tokens.",
			[]string{"The appetite is the one given or the latest pitch's; with neither the call replies bad_request.",
				"An idea with an active bet replies bad_request. Only people bet: the CLI refuses inside an agent's session.",
				"The pitch's rabbit holes and no-gos are recorded for learning.", slugNotes},
			`{`+repo+`,"slug":"try-a-quiet-mode","appetite":"200k","person":"ann"}`, betJSON,
			func(ctx context.Context, _ *api.Conn, in StartIn) (BetOut, error) {
				var tokens int64
				if in.Appetite != "" {
					var err error
					if tokens, err = pitch.ParseTokens(in.Appetite); err != nil {
						return BetOut{}, &api.Error{Code: api.CodeBadRequest, Message: err.Error()}
					}
				}
				st, err := g.open(ctx, in.Repo)
				if err != nil {
					return BetOut{}, err
				}
				d, err := work.StartBet(ctx, st, work.BetIn{Slug: in.Slug, Appetite: tokens, Person: in.Person})
				return betDetailOut(d), refused(err)
			}),
		api.Define("bet.list", "Lists the repo's bets, newest first.",
			[]string{"The result is [] when there are none."},
			`{`+repo+`}`, `[`+betJSON+`]`,
			func(ctx context.Context, _ *api.Conn, in RepoIn) ([]BetOut, error) {
				st, err := g.open(ctx, in.Repo)
				if err != nil {
					return nil, err
				}
				fs, err := work.Bets(ctx, st)
				out := make([]BetOut, 0, len(fs))
				for _, f := range fs {
					out = append(out, betOut(f))
				}
				return out, refused(err)
			}),
		api.Define("bet.show", "Shows a bet with the pitch it was made on.",
			[]string{slugNotes}, `{`+repo+`,"slug":"try-a-quiet-mode"}`, betJSON,
			func(ctx context.Context, _ *api.Conn, in SlugIn) (BetOut, error) {
				st, err := g.open(ctx, in.Repo)
				if err != nil {
					return BetOut{}, err
				}
				d, err := work.Bet(ctx, st, in.Slug)
				return betDetailOut(d), refused(err)
			}),
		api.Define("bet.stop", "Stops an active bet.",
			[]string{"Only an active bet stops; any other replies bad_request.", slugNotes},
			`{`+repo+`,"slug":"try-a-quiet-mode","reason":"not worth it","person":"ann"}`, betJSON,
			func(ctx context.Context, _ *api.Conn, in StopIn) (BetOut, error) {
				st, err := g.open(ctx, in.Repo)
				if err != nil {
					return BetOut{}, err
				}
				d, err := work.StopBet(ctx, st, work.StopIn{Slug: in.Slug, Reason: in.Reason, Person: in.Person})
				return betDetailOut(d), refused(err)
			}),
	}
}
