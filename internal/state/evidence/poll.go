package evidence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	observe "github.com/dphaener/physa/internal/evidence"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/state"
	"github.com/dphaener/physa/internal/vcs/git"
)

// mark is how far the poll has read: version control's head and its time,
// and the newest merged-at the forge showed.
type mark = observe.Watermark

// What a poll did with the forge. Empty is that it did not ask one: there is
// none, or its backoff had not run out.
const (
	forgeOK     = "ok"
	forgeFailed = "failed"
)

// pollOut is a poll intent's result: the facts it observed, the watermark to
// move to, and what the forge did.
type pollOut struct {
	Facts      []observe.Fact `json:"facts"`
	Next       mark           `json:"next"`
	Forge      string         `json:"forge,omitempty"`
	ForgeError string         `json:"forge_error,omitempty"`
}

// ts writes a watermark or fact time as RFC 3339 UTC, which sorts as text and
// reads back the same; the zero time is "".
func ts(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parse(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, s)
}

// factAt writes a fact's time as RFC 3339 UTC in whole seconds, so facts
// sort by time as text.
func factAt(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// intentStamp is the format of the intents table's times (internal/state).
const intentStamp = "2006-01-02T15:04:05.000Z"

// firePoll is the poll timer's Fire. Unless a poll is already pending it
// writes one from the stored watermark; then it sets itself 5 minutes out
// while any pursuit is active, and a day out otherwise. It reads nothing outside
// the store.
func firePoll(tx *state.Tx, t state.Timer) error {
	var pending, active int
	if err := tx.QueryRow(`SELECT count(*) FROM intents WHERE kind = ? AND state = 'pending'`, PollKind).Scan(&pending); err != nil {
		return err
	}
	if pending == 0 {
		in, err := readPoll(tx)
		if err != nil {
			return err
		}
		in.Store, in.At = tx.StoreDir(), tx.Now()
		if _, err := tx.Intent(PollKind, in); err != nil {
			return err
		}
	}
	if err := tx.QueryRow(`SELECT count(*) FROM pursuits WHERE state = 'active'`).Scan(&active); err != nil {
		return err
	}
	every := idleEvery
	if active > 0 {
		every = activeEvery
	}
	return tx.SetTimer(t.Kind, t.Key, tx.Now().Add(every), nil)
}

// readPoll reads the watermark row; with none yet it is a first look.
func readPoll(tx *state.Tx) (pollIn, error) {
	var in pollIn
	var at, forgeAt, next string
	err := tx.QueryRow(`SELECT vcs_commit, vcs_at, forge_merged_at, forge_next_at FROM evidence_polls WHERE id = 1`).
		Scan(&in.Mark.Commit, &at, &forgeAt, &next)
	if errors.Is(err, sql.ErrNoRows) {
		return in, nil
	} else if err != nil {
		return in, err
	}
	var errs [3]error
	in.Mark.Time, errs[0] = parse(at)
	in.Mark.Forge, errs[1] = parse(forgeAt)
	in.ForgeNext, errs[2] = parse(next)
	return in, errors.Join(errs[:]...)
}

// doPoll is the poll intent's Do, run with no transaction open. The repo
// root is the store directory's grandparent (.git/physa, design 03 section
// 18). The forge is asked only when its backoff has run out. A failure of
// version control fails the poll, so it is tried again; the forge's failure
// is part of the result.
func doPoll(ctx context.Context, in state.Intent, newForge func(string) forge.Forge) (string, error) {
	var p pollIn
	if err := json.Unmarshal(in.Payload, &p); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	root := filepath.Dir(filepath.Dir(p.Store))
	repo, err := git.Open(ctx, root)
	if err != nil {
		return "", fmt.Errorf("open the repo at %s: %w", root, err)
	}
	var merged observe.Merged
	if newForge != nil && !p.ForgeNext.After(p.At) {
		if m, ok := newForge(root).(forge.Merged); ok {
			merged = m
		}
	}
	facts, next, forgeErr, err := observe.Observe(repo, merged, p.Mark, p.At)
	if err != nil {
		return "", fmt.Errorf("read version control: %w", err)
	}
	out := pollOut{Facts: facts, Next: next}
	switch {
	case merged == nil:
	case forgeErr != nil:
		out.Forge, out.ForgeError = forgeFailed, forgeErr.Error()
	default:
		out.Forge = forgeOK
	}
	body, err := json.Marshal(out)
	return string(body), err
}

// recordPoll is the poll intent's Record, in the transaction of its
// intent.done. Each fact is kept once, with its pursuit's idea when the pursuit is
// known. The watermarks and the forge's backoff move only from the values
// the poll started at, so a poll recorded twice moves nothing twice.
func recordPoll(tx *state.Tx, in state.Intent, result string) error {
	var p pollIn
	var out pollOut
	if err := json.Unmarshal(in.Payload, &p); err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		return err
	}
	added, total := map[string]int64{}, int64(0)
	for _, f := range out.Facts {
		res, err := tx.Exec(`INSERT OR IGNORE INTO evidence (kind, commit_id, pr, capability, pursuit, change, idea, at, source, seconds, by_commit, recorded_at)
			VALUES (?1, ?2, ?3, ?4, ?5, ?6, coalesce((SELECT i.slug FROM pursuits b JOIN ideas i ON i.id = b.idea_id WHERE b.slug = ?5 AND ?5 != ''), ''), ?7, ?8, ?9, ?10, ?11)`,
			f.Kind, f.Commit, f.PR, f.Capability, f.Pursuit, f.Change, factAt(f.At), f.Source, nullInt(f.Seconds), f.ByCommit, ts(tx.Now()))
		if err != nil {
			return fmt.Errorf("store a %s fact: %w", f.Kind, err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added[f.Kind] += n
			total += n
		}
	}
	if err := movePoll(tx, p, out); err != nil {
		return err
	}
	if total == 0 {
		return nil
	}
	return tx.Event("evidence.recorded", "evidence", 0, "", map[string]any{"count": total, "kinds": added})
}

func nullInt(p *int64) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *p, Valid: true}
}

// movePoll moves the watermarks and the forge's backoff by compare-and-set
// against the payload: reset on the forge's success, and on its failure a
// next try 5 minutes out, doubling per failure to at most 6 hours.
func movePoll(tx *state.Tx, p pollIn, out pollOut) error {
	exec := func(q string, args ...any) error {
		if _, err := tx.Exec(q, args...); err != nil {
			return fmt.Errorf("move the evidence watermark: %w", err)
		}
		return nil
	}
	err := errors.Join(
		exec(`INSERT INTO evidence_polls (id) VALUES (1) ON CONFLICT (id) DO NOTHING`),
		exec(`UPDATE evidence_polls SET vcs_commit = ?, vcs_at = ? WHERE id = 1 AND vcs_commit = ? AND vcs_at = ?`,
			out.Next.Commit, ts(out.Next.Time), p.Mark.Commit, ts(p.Mark.Time)),
		exec(`UPDATE evidence_polls SET forge_merged_at = ? WHERE id = 1 AND forge_merged_at = ?`, ts(out.Next.Forge), ts(p.Mark.Forge)))
	if err != nil {
		return err
	}
	switch out.Forge {
	case forgeOK:
		return exec(`UPDATE evidence_polls SET forge_failures = 0, forge_next_at = '' WHERE id = 1 AND forge_next_at = ?`, ts(p.ForgeNext))
	case forgeFailed:
		var n int
		if err := tx.QueryRow(`SELECT forge_failures FROM evidence_polls WHERE id = 1`).Scan(&n); err != nil {
			return err
		}
		next := tx.Now().Add(min(forgeFirst<<min(n, 10), forgeMost))
		return exec(`UPDATE evidence_polls SET forge_failures = ?, forge_next_at = ? WHERE id = 1 AND forge_next_at = ?`, n+1, ts(next), ts(p.ForgeNext))
	}
	return nil
}

// expire is the retention timer's Fire: it deletes facts observed more than
// a year ago and done polls older than a week, appends evidence.expired with
// the counts when it deleted anything, and sets itself a day out.
func expire(tx *state.Tx, t state.Timer) error {
	now := tx.Now()
	facts, err := tx.Exec(`DELETE FROM evidence WHERE at < ?`, factAt(now.Add(-keepFacts)))
	if err != nil {
		return err
	}
	polls, err := tx.Exec(`DELETE FROM intents WHERE kind = ? AND state = 'done' AND done_at < ?`, PollKind, now.Add(-keepPolls).UTC().Format(intentStamp))
	if err != nil {
		return err
	}
	nf, _ := facts.RowsAffected()
	np, _ := polls.RowsAffected()
	if nf+np > 0 {
		if err := tx.Event("evidence.expired", "evidence", 0, "", map[string]int64{"facts": nf, "polls": np}); err != nil {
			return err
		}
	}
	return tx.SetTimer(t.Kind, t.Key, now.Add(expireEvery), nil)
}
