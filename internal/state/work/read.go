package work

import (
	"context"
	"database/sql"
	"time"

	"github.com/dphaener/physa/internal/signal"
	"github.com/dphaener/physa/internal/state"
)

// Reads run as commands that write nothing: one consistent view, taken
// between two changes, with no event (a read changes no row).

// IdeaFacts is an idea and the facts that say where it stands. Pursuit and
// PursuitState are its latest pursuit's, empty when it has none.
type IdeaFacts struct {
	Slug, Text, Person    string
	CreatedAt             time.Time
	Pursuit, PursuitState string
}

// IdeaDetail is an idea with its pursuits, newest first.
type IdeaDetail struct {
	IdeaFacts
	Pursuits []PursuitFacts
}

// PursuitFacts is a pursuit: its idea, appetite and spend in tokens, state
// and times. Appetite is 0 when the pursuit has none. StopReason, StartedAt
// and EndedAt are empty or zero when unset.
type PursuitFacts struct {
	Slug, Idea, IdeaText, Person string
	Appetite, Spent              int64
	State, StopReason            string
	CreatedAt, StartedAt         time.Time
	EndedAt                      time.Time
}

// PursuitDetail is a pursuit with its signal, nil for a pursuit started
// before signals were recorded.
type PursuitDetail struct {
	PursuitFacts
	Signal *signal.Signal
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

// Idea returns one idea with its pursuits, or refuses naming an unknown
// slug.
func Idea(ctx context.Context, s Submitter, slug string) (IdeaDetail, error) {
	var out IdeaDetail
	err := s.Submit(ctx, func(tx *state.Tx) (err error) {
		out, err = ideaDetail(tx, slug)
		return err
	})
	return out, err
}

// Pursuits returns every pursuit, newest first.
func Pursuits(ctx context.Context, s Submitter) ([]PursuitFacts, error) {
	var out []PursuitFacts
	err := s.Submit(ctx, func(tx *state.Tx) (err error) {
		out, err = pursuitFacts(tx, "")
		return err
	})
	return out, err
}

// Pursuit returns one pursuit with its signal, or refuses naming an unknown
// slug.
func Pursuit(ctx context.Context, s Submitter, slug string) (PursuitDetail, error) {
	var out PursuitDetail
	err := s.Submit(ctx, func(tx *state.Tx) (err error) {
		out, err = pursuitDetail(tx, slug)
		return err
	})
	return out, err
}

func ideaFacts(tx *state.Tx, where string, args ...any) ([]IdeaFacts, error) {
	rows, err := tx.Query(`SELECT i.slug, i.text, i.person, i.created_at,
		coalesce((SELECT p.slug FROM pursuits p WHERE p.idea_id = i.id ORDER BY p.id DESC LIMIT 1), ''),
		coalesce((SELECT p.state FROM pursuits p WHERE p.idea_id = i.id ORDER BY p.id DESC LIMIT 1), '')
		FROM ideas i `+where+` ORDER BY i.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IdeaFacts{}
	for rows.Next() {
		var f IdeaFacts
		var at string
		if err := rows.Scan(&f.Slug, &f.Text, &f.Person, &at, &f.Pursuit, &f.PursuitState); err != nil {
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
	d.Pursuits, err = pursuitFacts(tx, `WHERE i.slug = ?`, slug)
	return d, err
}

func pursuitFacts(tx *state.Tx, where string, args ...any) ([]PursuitFacts, error) {
	rows, err := tx.Query(`SELECT p.slug, i.slug, i.text, p.person, coalesce(p.appetite_tokens, 0), p.spent_tokens, p.state,
		coalesce(p.stop_reason, ''), p.created_at, coalesce(p.started_at, ''), coalesce(p.ended_at, '')
		FROM pursuits p JOIN ideas i ON i.id = p.idea_id `+where+` ORDER BY p.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PursuitFacts{}
	for rows.Next() {
		var f PursuitFacts
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

func pursuitDetail(tx *state.Tx, slug string) (PursuitDetail, error) {
	facts, err := pursuitFacts(tx, `WHERE p.slug = ?`, slug)
	if err != nil {
		return PursuitDetail{}, err
	} else if len(facts) == 0 {
		return PursuitDetail{}, refuse(ErrUnknownPursuit, "no pursuit has the slug %q", slug)
	}
	d := PursuitDetail{PursuitFacts: facts[0]}
	var claim, forText, against, window, resolver sql.NullString
	if err := tx.QueryRow(`SELECT signal_claim, signal_for, signal_against, signal_window, signal_resolver FROM pursuits WHERE slug = ?`, slug).
		Scan(&claim, &forText, &against, &window, &resolver); err != nil {
		return PursuitDetail{}, err
	}
	if claim.Valid {
		d.Signal = &signal.Signal{Claim: claim.String, For: forText.String, Against: against.String, Window: window.String, Resolver: resolver.String}
	}
	return d, nil
}

// parse reads a stored time; an empty one is the zero time.
func parse(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, s)
}
