// Package learnbet finds the learning signals a bet carries: a pitch's rabbit
// holes and no-gos, read from the store, so learning drafts them into the
// map. It only reads: the store's tables are the engine's to write, and the
// map is written by nobody but learning's reviewed proposals. A bet is
// learned from once; the record is a Learned trailer naming it as
// bet/<slug>, read from the history made since the bet.
package learnbet

import (
	"fmt"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/history"
	"github.com/dphaener/physa/internal/learn"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
)

// Prefix starts the id a bet is learned under: bet/<slug>.
const Prefix = "bet/"

// learnedKey is the trailer of a learning commit that names what it learned
// from (design 08 section 6).
const learnedKey = "Learned"

// ID returns the id a bet is learned under.
func ID(slug string) string { return Prefix + slug }

// IsID reports whether id names a bet and not a change.
func IsID(id string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(id)), Prefix)
}

// Bet is a bet made on a pitch, with the two parts of the pitch learning
// reads.
type Bet struct {
	Slug   string
	Idea   string // the idea's text
	Person string // who made the bet
	At     time.Time
	// RabbitHoles and NoGos are the pitch's parts, trimmed; either may be
	// empty.
	RabbitHoles, NoGos string
}

// Read returns the bets made on a pitch, newest first. A bet made on an
// idea's text alone has no pitch and is left out.
func Read(st *store.Store) ([]Bet, error) {
	rows, err := st.DB().Query(`SELECT b.slug, i.text, b.person, b.created_at, p.rabbit_holes, p.no_gos
		FROM bets b JOIN ideas i ON i.id = b.idea_id JOIN pitches p ON p.id = b.pitch_id
		ORDER BY b.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Bet
	for rows.Next() {
		var b Bet
		var at string
		if err := rows.Scan(&b.Slug, &b.Idea, &b.Person, &at, &b.RabbitHoles, &b.NoGos); err != nil {
			return nil, err
		}
		if b.At, err = time.Parse(time.RFC3339, at); err != nil {
			return nil, fmt.Errorf("bet %s: %w", b.Slug, err)
		}
		b.RabbitHoles, b.NoGos = strings.TrimSpace(b.RabbitHoles), strings.TrimSpace(b.NoGos)
		out = append(out, b)
	}
	return out, rows.Err()
}

// Source finds the bets' signals for a learning run (learn.Bets).
type Source struct {
	open func() (*store.Store, error)
}

// From returns the Source that reads the store open opens for dir. The
// store is opened for each read and closed after it.
func From(open func(dir string) (*store.Store, error), dir string) *Source {
	return &Source{open: func() (*store.Store, error) { return open(dir) }}
}

// Signals returns one signal for each bet whose pitch has rabbit holes or
// no-gos and that no Learned trailer names: none in learned, and none on a
// change made on ref since the bet. Each comes as a ChangeSignals named
// bet/<slug>, and its signal is about the change the bet was made on: the
// newest one on ref no later than the bet, which is what an entry drafted
// from it is anchored to. A store or a history that cannot be read gives no
// signal and a gap saying why, never an error.
func (s *Source) Signals(repo vcs.Repo, ref string, learned []string) ([]learn.ChangeSignals, []learn.Unavailable) {
	gap := func(format string, args ...any) ([]learn.ChangeSignals, []learn.Unavailable) {
		return nil, []learn.Unavailable{{Kind: learn.KindBetPitch, Reason: fmt.Sprintf(format, args...)}}
	}
	if s == nil || s.open == nil {
		return gap("no store")
	}
	st, err := s.open()
	if err != nil {
		return gap("no store: %v", err)
	}
	bets, err := Read(st)
	st.Close()
	if err != nil {
		return gap("the store's bets cannot be read: %v", err)
	}
	bets = unlearned(bets, learned)
	if len(bets) == 0 {
		return nil, nil
	}
	// A Learned trailer naming a bet can be older than the changes a run
	// collects, which the watermark bounds; it is never older than the bet.
	oldest := bets[0].At
	for _, b := range bets {
		if b.At.Before(oldest) {
			oldest = b.At
		}
	}
	since, err := repo.Since(ref, oldest)
	if err != nil {
		return gap("the history since the oldest bet cannot be read: %v", err)
	}
	var named []string
	for _, c := range since {
		named = append(named, history.Trailers(c.Message, learnedKey)...)
	}
	if bets = unlearned(bets, named); len(bets) == 0 {
		return nil, nil
	}
	changes, err := repo.Latest(ref, len(since)+1)
	if err != nil {
		return gap("the change a bet was made on cannot be read: %v", err)
	} else if len(changes) == 0 {
		return gap("the history holds no change a bet was made on")
	}
	out := make([]learn.ChangeSignals, 0, len(bets))
	for _, b := range bets {
		out = append(out, signal(b, madeOn(changes, b.At)))
	}
	return out, nil
}

// unlearned returns the bets with rabbit holes or no-gos that none of
// learned names. A bet's id is matched whole, never by prefix: a second bet
// on an idea has the first one's slug with a number after it.
func unlearned(bets []Bet, learned []string) []Bet {
	done := map[string]bool{}
	for _, id := range learned {
		done[strings.ToLower(strings.TrimSpace(id))] = true
	}
	var out []Bet
	for _, b := range bets {
		if (b.RabbitHoles != "" || b.NoGos != "") && !done[strings.ToLower(ID(b.Slug))] {
			out = append(out, b)
		}
	}
	return out
}

// madeOn returns the newest of changes, newest first, made no later than
// at, or the oldest of them when every one is later.
func madeOn(changes []vcs.Change, at time.Time) vcs.Change {
	for _, c := range changes {
		if !c.Time.After(at) {
			return c
		}
	}
	return changes[len(changes)-1]
}

// signal is the signal bet b carries. A bet is no change: on is only the
// change that entries drafted from it anchor to, and the text says so.
func signal(b Bet, on vcs.Change) learn.ChangeSignals {
	id := ID(b.Slug)
	idea := strings.Join(strings.Fields(b.Idea), " ")
	var parts []string
	var quotes []learn.Quote
	for _, p := range []struct{ name, text string }{{"Rabbit holes", b.RabbitHoles}, {"No-gos", b.NoGos}} {
		if p.text == "" {
			continue
		}
		parts = append(parts, strings.ToLower(p.name))
		quotes = append(quotes, learn.Quote{Author: b.Person, At: b.At, Text: p.name + ":\n" + p.text})
	}
	return learn.ChangeSignals{
		ChangeRef: learn.ChangeRef{Change: id, Short: id, Subject: idea},
		At:        b.At,
		Signals: []learn.Signal{{
			Kind: learn.KindBetPitch, Rule: learn.RuleBetPitch, Changes: []string{on.Short},
			Evidence: []string{
				fmt.Sprintf("bet %s on the idea: %s", b.Slug, idea),
				fmt.Sprintf("made by %s at %s; entries from it anchor to change %s", b.Person, b.At.UTC().Format(time.RFC3339), on.Short),
				"its pitch names " + strings.Join(parts, " and "),
			},
			Quotes: quotes,
		}},
	}
}
