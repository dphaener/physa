// Package work is the engine's commands and reads on ideas and pursuits:
// each change one command in one transaction with its event. An idea belongs
// to the person who adds it and has no named states; a pursuit is started
// against a signal, with an appetite or none, is active, done or stopped, and
// an idea has at most one active pursuit.
package work

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/signal"
	"github.com/dphaener/physa/internal/state"
)

// The kinds of Refusal. Test one with errors.Is; the API replies bad_request
// to every Refusal, with its message.
var (
	ErrUnknownIdea    = errors.New("unknown idea")
	ErrUnknownPursuit = errors.New("unknown pursuit")
	ErrSlugTaken      = errors.New("slug taken")
	ErrSlugInvalid    = errors.New("slug invalid")
	ErrActivePursuit  = errors.New("idea already has an active pursuit")
	ErrNotActive      = errors.New("pursuit not active")
	ErrBadAppetite    = errors.New("bad appetite")
	ErrNoPerson       = errors.New("no person")
	ErrNoText         = errors.New("no text")
	ErrSignal         = errors.New("signal incomplete")
)

// Refusal is a command or read refused for what it was asked, not for a
// fault: its message says why in plain words, naming the slug.
type Refusal struct {
	Kind error
	Msg  string
}

func (r *Refusal) Error() string { return r.Msg }
func (r *Refusal) Unwrap() error { return r.Kind }

func refuse(kind error, format string, args ...any) error {
	return &Refusal{Kind: kind, Msg: fmt.Sprintf(format, args...)}
}

// Submitter runs a command in one transaction; *state.State is one.
type Submitter interface {
	Submit(ctx context.Context, c state.Command) error
}

// IdeaIn adds an idea. Slug is optional: left empty, one is made from Text.
type IdeaIn struct {
	Text, Slug, Person string
}

// PursueIn starts a pursuit of an idea against Signal. Appetite 0 is none.
type PursueIn struct {
	Slug     string
	Appetite int64
	Signal   signal.Signal
	Person   string
}

// StopIn stops an active pursuit. Reason empty is "stopped by <person>".
type StopIn struct {
	Slug, Reason, Person string
}

const (
	stateActive  = "active"
	stateStopped = "stopped"
)

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func person(p string) (string, error) {
	if p = strings.TrimSpace(p); p == "" {
		return "", refuse(ErrNoPerson, "a person must be named for this change")
	}
	return p, nil
}

func exists(tx *state.Tx, query, slug string) (bool, error) {
	var one int
	err := tx.QueryRow(query, slug).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// AddIdea adds an idea owned by in.Person, with event idea.added. A slug
// made from the text gets a number when it is taken or reserved; a chosen
// slug that is taken is refused.
func AddIdea(ctx context.Context, s Submitter, in IdeaIn) (IdeaFacts, error) {
	who, err := person(in.Person)
	if err != nil {
		return IdeaFacts{}, err
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return IdeaFacts{}, refuse(ErrNoText, "an idea needs its text")
	}
	chosen := strings.TrimSpace(in.Slug)
	if chosen != "" {
		if err := ValidSlug(chosen); err != nil {
			return IdeaFacts{}, err
		}
	}
	var out IdeaFacts
	err = s.Submit(ctx, func(tx *state.Tx) error {
		taken := func(slug string) (bool, error) { return exists(tx, `SELECT 1 FROM ideas WHERE slug = ?`, slug) }
		slug, err := chosen, error(nil)
		if slug != "" {
			if used, err := taken(slug); err != nil {
				return err
			} else if used {
				return refuse(ErrSlugTaken, "an idea already has the slug %q", slug)
			}
		} else if slug, err = unique(MakeSlug(text), taken); err != nil {
			return err
		}
		at := stamp(tx.Now())
		res, err := tx.Exec(`INSERT INTO ideas (slug, text, person, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, slug, text, who, at, at)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if err := tx.Event("idea.added", "idea", id, who, map[string]any{"slug": slug, "text": text}); err != nil {
			return err
		}
		facts, err := ideaFacts(tx, `WHERE i.slug = ?`, slug)
		if err == nil {
			out = facts[0]
		}
		return err
	})
	return out, err
}

// lookupIdea finds an idea's id and text by slug, or refuses naming it.
func lookupIdea(tx *state.Tx, slug string) (int64, string, error) {
	var id int64
	var text string
	err := tx.QueryRow(`SELECT id, text FROM ideas WHERE slug = ?`, slug).Scan(&id, &text)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", refuse(ErrUnknownIdea, "no idea has the slug %q", slug)
	}
	return id, text, err
}

// StartPursuit starts a pursuit of an idea, with event pursuit.started
// carrying its appetite and signal. The signal is required whole and checked
// on the engine's day: its window is today or later. An appetite is optional;
// with none there is no circuit breaker. An idea with an active pursuit is
// refused another. The pursuit's slug is the idea's, numbered when an earlier
// pursuit has it.
func StartPursuit(ctx context.Context, s Submitter, in PursueIn) (PursuitDetail, error) {
	who, err := person(in.Person)
	if err != nil {
		return PursuitDetail{}, err
	}
	if in.Appetite < 0 {
		return PursuitDetail{}, refuse(ErrBadAppetite, "an appetite must be more than 0 tokens")
	}
	sig := signal.Trim(in.Signal)
	var out PursuitDetail
	err = s.Submit(ctx, func(tx *state.Tx) error {
		if problems := signal.Check(sig, tx.Now().Local()); len(problems) > 0 {
			return refuse(ErrSignal, "%s", signal.Problems(problems).Error())
		}
		ideaID, _, err := lookupIdea(tx, in.Slug)
		if err != nil {
			return err
		}
		var active string
		switch err := tx.QueryRow(`SELECT slug FROM pursuits WHERE idea_id = ? AND state = 'active'`, ideaID).Scan(&active); {
		case err == nil:
			return refuse(ErrActivePursuit, "the idea %q already has the active pursuit %q; stop it first", in.Slug, active)
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		slug, err := unique(in.Slug, func(slug string) (bool, error) { return exists(tx, `SELECT 1 FROM pursuits WHERE slug = ?`, slug) })
		if err != nil {
			return err
		}
		appetite := sql.NullInt64{Int64: in.Appetite, Valid: in.Appetite > 0}
		at := stamp(tx.Now())
		res, err := tx.Exec(`INSERT INTO pursuits (slug, idea_id, person, appetite_tokens, state, created_at, started_at,
			signal_claim, signal_for, signal_against, signal_window, signal_resolver) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			slug, ideaID, who, appetite, stateActive, at, at, sig.Claim, sig.For, sig.Against, sig.Window, sig.Resolver)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		var tokens any // none is null
		if appetite.Valid {
			tokens = appetite.Int64
		}
		data := map[string]any{"slug": slug, "idea": in.Slug, "appetite": tokens, "signal": map[string]string{
			"claim": sig.Claim, "for": sig.For, "against": sig.Against, "window": sig.Window, "resolver": sig.Resolver}}
		if err := tx.Event("pursuit.started", "pursuit", id, who, data); err != nil {
			return err
		}
		out, err = pursuitDetail(tx, slug)
		return err
	})
	return out, err
}

// StopPursuit stops an active pursuit, with event pursuit.stopped. Only an
// active pursuit stops; the reason is kept, or "stopped by <person>" when
// none is given.
func StopPursuit(ctx context.Context, s Submitter, in StopIn) (PursuitDetail, error) {
	who, err := person(in.Person)
	if err != nil {
		return PursuitDetail{}, err
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		reason = "stopped by " + who
	}
	var out PursuitDetail
	err = s.Submit(ctx, func(tx *state.Tx) error {
		var id int64
		var now, idea string
		err := tx.QueryRow(`SELECT p.id, p.state, i.slug FROM pursuits p JOIN ideas i ON i.id = p.idea_id WHERE p.slug = ?`, in.Slug).Scan(&id, &now, &idea)
		if errors.Is(err, sql.ErrNoRows) {
			return refuse(ErrUnknownPursuit, "no pursuit has the slug %q", in.Slug)
		} else if err != nil {
			return err
		}
		if now != stateActive {
			return refuse(ErrNotActive, "the pursuit %q is %s, not active; only an active pursuit stops", in.Slug, now)
		}
		if _, err := tx.Exec(`UPDATE pursuits SET state = ?, ended_at = ?, stop_reason = ? WHERE id = ?`, stateStopped, stamp(tx.Now()), reason, id); err != nil {
			return err
		}
		if err := tx.Event("pursuit.stopped", "pursuit", id, who, map[string]any{"slug": in.Slug, "idea": idea, "reason": reason}); err != nil {
			return err
		}
		out, err = pursuitDetail(tx, in.Slug)
		return err
	})
	return out, err
}
