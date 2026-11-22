// Package work is the engine's commands and reads on ideas, pitches and
// bets: each change one command in one transaction with its event. An idea
// belongs to the person who adds it and has no named states; a pitch shaped
// again is a new pitch, so its history stays; a bet is active, done or
// stopped, and an idea has at most one active bet.
package work

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/pitch"
	"github.com/dphaener/physa/internal/state"
)

// The kinds of Refusal. Test one with errors.Is; the API replies bad_request
// to every Refusal, with its message.
var (
	ErrUnknownIdea     = errors.New("unknown idea")
	ErrUnknownBet      = errors.New("unknown bet")
	ErrSlugTaken       = errors.New("slug taken")
	ErrSlugInvalid     = errors.New("slug invalid")
	ErrActiveBet       = errors.New("idea already has an active bet")
	ErrNotActive       = errors.New("bet not active")
	ErrNoAppetite      = errors.New("no appetite")
	ErrNoPerson        = errors.New("no person")
	ErrNoText          = errors.New("no text")
	ErrPitchIncomplete = errors.New("pitch incomplete")
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

// ShapeIn shapes an idea into a pitch, or shapes it again.
type ShapeIn struct {
	Slug   string
	Pitch  pitch.Pitch
	Person string
}

// BetIn makes a bet on an idea. Appetite 0 takes the latest pitch's.
type BetIn struct {
	Slug     string
	Appetite int64
	Person   string
}

// StopIn stops an active bet. Reason empty is "stopped by <person>".
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

// incomplete names the parts of p that are empty, in the order written.
func incomplete(p pitch.Pitch) []string {
	var missing []string
	for _, part := range []struct {
		name string
		ok   bool
	}{
		{"Problem", strings.TrimSpace(p.Problem) != ""}, {"Appetite", p.Appetite > 0},
		{"Solution", strings.TrimSpace(p.Solution) != ""}, {"Scope", strings.TrimSpace(p.Scope) != ""},
		{"Signal", strings.TrimSpace(p.Signal) != ""}, {"Rabbit holes", strings.TrimSpace(p.RabbitHoles) != ""},
		{"No-gos", strings.TrimSpace(p.NoGos) != ""},
	} {
		if !part.ok {
			missing = append(missing, part.name)
		}
	}
	return missing
}

// Shape stores in.Pitch as the idea's latest pitch, with event pitch.shaped.
// Earlier pitches are kept: shaping again adds one. Every part must be there.
func Shape(ctx context.Context, s Submitter, in ShapeIn) (IdeaDetail, error) {
	who, err := person(in.Person)
	if err != nil {
		return IdeaDetail{}, err
	}
	if missing := incomplete(in.Pitch); len(missing) > 0 {
		return IdeaDetail{}, refuse(ErrPitchIncomplete, "the pitch for %q has empty parts: %s", in.Slug, strings.Join(missing, ", "))
	}
	p := in.Pitch
	var out IdeaDetail
	err = s.Submit(ctx, func(tx *state.Tx) error {
		ideaID, _, err := lookupIdea(tx, in.Slug)
		if err != nil {
			return err
		}
		at := stamp(tx.Now())
		res, err := tx.Exec(`INSERT INTO pitches (idea_id, problem, appetite_tokens, solution, scope, signal, rabbit_holes, no_gos, person, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, ideaID, p.Problem, p.Appetite, p.Solution, p.Scope, p.Signal, p.RabbitHoles, p.NoGos, who, at, at)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE ideas SET updated_at = ? WHERE id = ?`, at, ideaID); err != nil {
			return err
		}
		if err := tx.Event("pitch.shaped", "pitch", id, who, map[string]any{"idea": in.Slug, "appetite": p.Appetite}); err != nil {
			return err
		}
		out, err = ideaDetail(tx, in.Slug)
		return err
	})
	return out, err
}

// StartBet makes a bet on an idea, with event bet.started carrying the
// pitch's rabbit holes and no-gos for learning. in.Appetite wins; without it
// the latest pitch's is used; with neither the bet is refused. An idea with
// an active bet is refused another. The bet's slug is the idea's, numbered
// when an earlier bet has it.
func StartBet(ctx context.Context, s Submitter, in BetIn) (BetDetail, error) {
	who, err := person(in.Person)
	if err != nil {
		return BetDetail{}, err
	}
	if in.Appetite < 0 {
		return BetDetail{}, refuse(ErrNoAppetite, "an appetite must be more than 0 tokens")
	}
	var out BetDetail
	err = s.Submit(ctx, func(tx *state.Tx) error {
		ideaID, _, err := lookupIdea(tx, in.Slug)
		if err != nil {
			return err
		}
		var active string
		switch err := tx.QueryRow(`SELECT slug FROM bets WHERE idea_id = ? AND state = 'active'`, ideaID).Scan(&active); {
		case err == nil:
			return refuse(ErrActiveBet, "the idea %q already has the active bet %q; stop it first", in.Slug, active)
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		var pitchID sql.NullInt64
		var p pitch.Pitch
		err = tx.QueryRow(`SELECT id, appetite_tokens, rabbit_holes, no_gos FROM pitches WHERE idea_id = ? ORDER BY id DESC LIMIT 1`, ideaID).
			Scan(&pitchID, &p.Appetite, &p.RabbitHoles, &p.NoGos)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		appetite := in.Appetite
		if appetite == 0 {
			appetite = p.Appetite
		}
		if appetite <= 0 {
			return refuse(ErrNoAppetite, "the idea %q has no pitch with an appetite; give the bet one", in.Slug)
		}
		slug, err := unique(in.Slug, func(slug string) (bool, error) { return exists(tx, `SELECT 1 FROM bets WHERE slug = ?`, slug) })
		if err != nil {
			return err
		}
		at := stamp(tx.Now())
		res, err := tx.Exec(`INSERT INTO bets (slug, idea_id, pitch_id, person, appetite_tokens, state, created_at, started_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			slug, ideaID, pitchID, who, appetite, stateActive, at, at)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		data := map[string]any{"slug": slug, "idea": in.Slug, "appetite": appetite, "pitch": pitchID.Valid, "rabbit_holes": p.RabbitHoles, "no_gos": p.NoGos}
		if err := tx.Event("bet.started", "bet", id, who, data); err != nil {
			return err
		}
		out, err = betDetail(tx, slug)
		return err
	})
	return out, err
}

// StopBet stops an active bet, with event bet.stopped. Only an active bet
// stops; the reason is kept, or "stopped by <person>" when none is given.
func StopBet(ctx context.Context, s Submitter, in StopIn) (BetDetail, error) {
	who, err := person(in.Person)
	if err != nil {
		return BetDetail{}, err
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		reason = "stopped by " + who
	}
	var out BetDetail
	err = s.Submit(ctx, func(tx *state.Tx) error {
		var id int64
		var now, idea string
		err := tx.QueryRow(`SELECT b.id, b.state, i.slug FROM bets b JOIN ideas i ON i.id = b.idea_id WHERE b.slug = ?`, in.Slug).Scan(&id, &now, &idea)
		if errors.Is(err, sql.ErrNoRows) {
			return refuse(ErrUnknownBet, "no bet has the slug %q", in.Slug)
		} else if err != nil {
			return err
		}
		if now != stateActive {
			return refuse(ErrNotActive, "the bet %q is %s, not active; only an active bet stops", in.Slug, now)
		}
		if _, err := tx.Exec(`UPDATE bets SET state = ?, ended_at = ?, stop_reason = ? WHERE id = ?`, stateStopped, stamp(tx.Now()), reason, id); err != nil {
			return err
		}
		if err := tx.Event("bet.stopped", "bet", id, who, map[string]any{"slug": in.Slug, "idea": idea, "reason": reason}); err != nil {
			return err
		}
		out, err = betDetail(tx, in.Slug)
		return err
	})
	return out, err
}
