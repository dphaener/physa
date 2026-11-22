package work

import (
	"context"
	"database/sql"
	"time"

	"github.com/dphaener/physa/internal/pitch"
	"github.com/dphaener/physa/internal/state"
)

// Reads run as commands that write nothing: one consistent view, taken
// between two changes, with no event (a read changes no row).

// IdeaFacts is an idea and the facts that say where it stands. Bet and
// BetState are its latest bet's, empty when it has none.
type IdeaFacts struct {
	Slug, Text, Person string
	CreatedAt          time.Time
	Pitches            int
	Bet, BetState      string
}

// Shaped is a stored pitch: its parts, who shaped it and when.
type Shaped struct {
	pitch.Pitch
	Person string
	At     time.Time
}

// IdeaDetail is an idea with its latest pitch (nil when never shaped) and
// its bets, newest first.
type IdeaDetail struct {
	IdeaFacts
	Pitch *Shaped
	Bets  []BetFacts
}

// BetFacts is a bet: its idea, appetite and spend in tokens, state and
// times. StopReason, StartedAt and EndedAt are empty or zero when unset.
type BetFacts struct {
	Slug, Idea, IdeaText, Person string
	Appetite, Spent              int64
	State, StopReason            string
	CreatedAt, StartedAt         time.Time
	EndedAt                      time.Time
}

// BetDetail is a bet with the pitch it was made on (nil when none).
type BetDetail struct {
	BetFacts
	Pitch *Shaped
}

// Ideas returns every idea, newest first.
func Ideas(ctx context.Context, s Submitter) ([]IdeaFacts, error) {
	var out []IdeaFacts
	err := s.Submit(ctx, func(tx *state.Tx) (err error) {
		out, err = ideaFacts(tx, "")
		return err
	})
	return out, err
}

// Idea returns one idea with its latest pitch and its bets, or refuses
// naming an unknown slug.
func Idea(ctx context.Context, s Submitter, slug string) (IdeaDetail, error) {
	var out IdeaDetail
	err := s.Submit(ctx, func(tx *state.Tx) (err error) {
		out, err = ideaDetail(tx, slug)
		return err
	})
	return out, err
}

// Bets returns every bet, newest first.
func Bets(ctx context.Context, s Submitter) ([]BetFacts, error) {
	var out []BetFacts
	err := s.Submit(ctx, func(tx *state.Tx) (err error) {
		out, err = betFacts(tx, "")
		return err
	})
	return out, err
}

// Bet returns one bet with its pitch, or refuses naming an unknown slug.
func Bet(ctx context.Context, s Submitter, slug string) (BetDetail, error) {
	var out BetDetail
	err := s.Submit(ctx, func(tx *state.Tx) (err error) {
		out, err = betDetail(tx, slug)
		return err
	})
	return out, err
}

func ideaFacts(tx *state.Tx, where string, args ...any) ([]IdeaFacts, error) {
	rows, err := tx.Query(`SELECT i.slug, i.text, i.person, i.created_at,
		(SELECT count(*) FROM pitches p WHERE p.idea_id = i.id),
		coalesce((SELECT b.slug FROM bets b WHERE b.idea_id = i.id ORDER BY b.id DESC LIMIT 1), ''),
		coalesce((SELECT b.state FROM bets b WHERE b.idea_id = i.id ORDER BY b.id DESC LIMIT 1), '')
		FROM ideas i `+where+` ORDER BY i.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IdeaFacts{}
	for rows.Next() {
		var f IdeaFacts
		var at string
		if err := rows.Scan(&f.Slug, &f.Text, &f.Person, &at, &f.Pitches, &f.Bet, &f.BetState); err != nil {
			return nil, err
		}
		if f.CreatedAt, err = parse(at); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func ideaDetail(tx *state.Tx, slug string) (IdeaDetail, error) {
	facts, err := ideaFacts(tx, `WHERE i.slug = ?`, slug)
	if err != nil {
		return IdeaDetail{}, err
	} else if len(facts) == 0 {
		return IdeaDetail{}, refuse(ErrUnknownIdea, "no idea has the slug %q", slug)
	}
	d := IdeaDetail{IdeaFacts: facts[0]}
	if d.Pitch, err = shaped(tx, `idea_id = (SELECT id FROM ideas WHERE slug = ?) ORDER BY id DESC LIMIT 1`, slug); err != nil {
		return IdeaDetail{}, err
	}
	d.Bets, err = betFacts(tx, `WHERE i.slug = ?`, slug)
	return d, err
}

func betFacts(tx *state.Tx, where string, args ...any) ([]BetFacts, error) {
	rows, err := tx.Query(`SELECT b.slug, i.slug, i.text, b.person, b.appetite_tokens, b.spent_tokens, b.state,
		coalesce(b.stop_reason, ''), b.created_at, coalesce(b.started_at, ''), coalesce(b.ended_at, '')
		FROM bets b JOIN ideas i ON i.id = b.idea_id `+where+` ORDER BY b.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BetFacts{}
	for rows.Next() {
		var f BetFacts
		var created, started, ended string
		if err := rows.Scan(&f.Slug, &f.Idea, &f.IdeaText, &f.Person, &f.Appetite, &f.Spent, &f.State,
			&f.StopReason, &created, &started, &ended); err != nil {
			return nil, err
		}
		for _, t := range []struct {
			s   string
			dst *time.Time
		}{{created, &f.CreatedAt}, {started, &f.StartedAt}, {ended, &f.EndedAt}} {
			if *t.dst, err = parse(t.s); err != nil {
				return nil, err
			}
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func betDetail(tx *state.Tx, slug string) (BetDetail, error) {
	facts, err := betFacts(tx, `WHERE b.slug = ?`, slug)
	if err != nil {
		return BetDetail{}, err
	} else if len(facts) == 0 {
		return BetDetail{}, refuse(ErrUnknownBet, "no bet has the slug %q", slug)
	}
	d := BetDetail{BetFacts: facts[0]}
	d.Pitch, err = shaped(tx, `id = (SELECT pitch_id FROM bets WHERE slug = ?)`, slug)
	return d, err
}

// shaped reads the one pitch the condition picks, or nil when it picks none.
func shaped(tx *state.Tx, cond string, args ...any) (*Shaped, error) {
	var p Shaped
	var at string
	err := tx.QueryRow(`SELECT problem, appetite_tokens, solution, scope, signal, rabbit_holes, no_gos, person, created_at
		FROM pitches WHERE `+cond, args...).Scan(&p.Problem, &p.Appetite, &p.Solution, &p.Scope, &p.Signal,
		&p.RabbitHoles, &p.NoGos, &p.Person, &at)
	if err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if p.At, err = parse(at); err != nil {
		return nil, err
	}
	return &p, nil
}

// parse reads a stored time; an empty one is the zero time.
func parse(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, s)
}
