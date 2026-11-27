package store

import (
	"database/sql"
	"encoding/json"
	"time"
)

// GatherRun describes how the gather was made: when, the bound used, and
// whether a forge answered. Forge is empty when there was none, and
// ForgeReason says why PRs are unavailable.
type GatherRun struct {
	At          time.Time
	Since       time.Time
	MaxPRs      int
	PRsLimited  bool
	Forge       string
	ForgeReason string
	Stopped     bool // true when the forge stopped the gather early
	// Reached is the oldest merge date of the PRs kept; zero when none.
	Reached time.Time
	// StopReason is why the forge stopped the gather.
	StopReason string
}

// Doc is one gathered document. Reason is why gather picked this source,
// shown to the person; it is empty for a gather stored before it was kept.
type Doc struct {
	Path   string
	Kind   string
	Body   string
	Reason string
}

// Trailer is one key and value from a change's message.
type Trailer struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// GatheredChange is one change from history with the files it altered.
// Reverts is the reverted change's id, or empty.
type GatheredChange struct {
	ID       string
	Short    string
	Subject  string
	Message  string
	Author   string
	At       time.Time
	Trailers []Trailer
	Reverts  string
	Files    []string
}

// GatheredComment is one review comment on a PR.
type GatheredComment struct {
	Author          string
	At              time.Time
	Kind            string
	RequestsChanges bool
	Body            string
}

// GatheredPR is one merged PR with its review comments and linked issues.
type GatheredPR struct {
	Number int
	Title  string
	Body   string
	Author string
	// MergedBy is who merged the PR, when the forge said.
	MergedBy string
	MergedAt time.Time
	Issues   []string
	Comments []GatheredComment
}

// Gather is the one gather the store keeps.
type Gather struct {
	Run     GatherRun
	Docs    []Doc
	Changes []GatheredChange
	PRs     []GatheredPR
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// PutGather replaces the stored gather with g in one transaction: on any
// error the earlier gather is left intact. It also deletes the stored init
// areas, proposal, recoveries and spend, which belong to one gather.
func (s *Store) PutGather(g Gather) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// ex runs a statement unless an earlier one failed; err keeps the first failure.
	ex := func(q string, args ...any) {
		if err == nil {
			_, err = tx.Exec(q, args...)
		}
	}
	js := func(v any) string {
		b, e := json.Marshal(v)
		if err == nil {
			err = e
		}
		return string(b)
	}
	for _, t := range []string{"gather_comments", "gather_prs", "gather_changes", "gather_docs", "gather_runs", "init_areas", "init_walk", "init_proposal", "init_recovery", "init_spend"} {
		ex("DELETE FROM " + t)
	}
	r := g.Run
	reached := ""
	if !r.Reached.IsZero() {
		reached = ts(r.Reached)
	}
	ex(`INSERT INTO gather_runs (id, at, since, max_prs, prs_limited, forge, forge_reason, stopped, stopped_reached, stopped_reason) VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ts(r.At), ts(r.Since), r.MaxPRs, b2i(r.PRsLimited), r.Forge, r.ForgeReason, b2i(r.Stopped), reached, r.StopReason)
	for _, d := range g.Docs {
		ex(`INSERT INTO gather_docs (path, kind, body, reason) VALUES (?, ?, ?, ?)`, d.Path, d.Kind, d.Body, d.Reason)
	}
	for i, c := range g.Changes {
		ex(`INSERT INTO gather_changes (id, short, subject, message, author, at, trailers, reverts, files, seq) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.ID, c.Short, c.Subject, c.Message, c.Author, ts(c.At), js(orEmpty(c.Trailers)), c.Reverts, js(orEmpty(c.Files)), i)
	}
	for i, p := range g.PRs {
		ex(`INSERT INTO gather_prs (number, title, body, author, merged_by, merged_at, issues, seq) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			p.Number, p.Title, p.Body, p.Author, p.MergedBy, ts(p.MergedAt), js(orEmpty(p.Issues)), i)
		for j, c := range p.Comments {
			ex(`INSERT INTO gather_comments (pr, author, at, kind, requests_changes, body, seq) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				p.Number, c.Author, ts(c.At), c.Kind, b2i(c.RequestsChanges), c.Body, j)
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// orEmpty makes a nil slice empty, so it stores as [] and not null.
func orEmpty[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Gathered returns the stored gather, false when nothing was gathered.
func (s *Store) Gathered() (Gather, bool, error) {
	var g Gather
	var at, since, reached string
	var limited, stopped int
	err := s.db.QueryRow(`SELECT at, since, max_prs, prs_limited, forge, forge_reason, stopped, stopped_reached, stopped_reason FROM gather_runs WHERE id = 1`).
		Scan(&at, &since, &g.Run.MaxPRs, &limited, &g.Run.Forge, &g.Run.ForgeReason, &stopped, &reached, &g.Run.StopReason)
	if err != nil {
		if err == sql.ErrNoRows {
			err = nil
		}
		return Gather{}, false, err
	}
	g.Run.PRsLimited = limited != 0
	g.Run.Stopped = stopped != 0
	parse := func(t *time.Time, v string) {
		if err == nil && v != "" {
			*t, err = time.Parse(time.RFC3339, v)
		}
	}
	parse(&g.Run.Reached, reached)
	parse(&g.Run.At, at)
	parse(&g.Run.Since, since)
	if err == nil {
		g.Docs, err = s.gatheredDocs()
	}
	if err == nil {
		g.Changes, err = s.gatheredChanges()
	}
	if err == nil {
		g.PRs, err = s.gatheredPRs()
	}
	if err != nil {
		return Gather{}, false, err
	}
	return g, true, nil
}

// each runs a query and calls scan for every row, closing the rows.
func (s *Store) each(q string, scan func(*sql.Rows) error, args ...any) error {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Store) gatheredDocs() (out []Doc, err error) {
	err = s.each(`SELECT path, kind, body, reason FROM gather_docs ORDER BY path`, func(r *sql.Rows) error {
		var d Doc
		err := r.Scan(&d.Path, &d.Kind, &d.Body, &d.Reason)
		out = append(out, d)
		return err
	})
	return out, err
}

func (s *Store) gatheredChanges() (out []GatheredChange, err error) {
	err = s.each(`SELECT id, short, subject, message, author, at, trailers, reverts, files FROM gather_changes ORDER BY seq`, func(r *sql.Rows) error {
		var c GatheredChange
		var at, tj, fj string
		if err := r.Scan(&c.ID, &c.Short, &c.Subject, &c.Message, &c.Author, &at, &tj, &c.Reverts, &fj); err != nil {
			return err
		}
		var err error
		if c.At, err = time.Parse(time.RFC3339, at); err == nil {
			err = json.Unmarshal([]byte(tj), &c.Trailers)
		}
		if err == nil {
			err = json.Unmarshal([]byte(fj), &c.Files)
		}
		if err != nil {
			return err
		}
		out = append(out, c)
		return nil
	})
	return out, err
}

func (s *Store) gatheredPRs() (out []GatheredPR, err error) {
	err = s.each(`SELECT number, title, body, author, merged_by, merged_at, issues FROM gather_prs ORDER BY seq`, func(r *sql.Rows) error {
		var p GatheredPR
		var at, ij string
		if err := r.Scan(&p.Number, &p.Title, &p.Body, &p.Author, &p.MergedBy, &at, &ij); err != nil {
			return err
		}
		var err error
		if p.MergedAt, err = time.Parse(time.RFC3339, at); err == nil {
			err = json.Unmarshal([]byte(ij), &p.Issues)
		}
		if err != nil {
			return err
		}
		out = append(out, p)
		return nil
	})
	for i := 0; err == nil && i < len(out); i++ {
		out[i].Comments, err = s.gatheredComments(out[i].Number)
	}
	return out, err
}

func (s *Store) gatheredComments(pr int) (out []GatheredComment, err error) {
	err = s.each(`SELECT author, at, kind, requests_changes, body FROM gather_comments WHERE pr = ? ORDER BY seq`, func(r *sql.Rows) error {
		var c GatheredComment
		var at string
		var rc int
		if err := r.Scan(&c.Author, &at, &c.Kind, &rc, &c.Body); err != nil {
			return err
		}
		c.RequestsChanges = rc != 0
		var err error
		if c.At, err = time.Parse(time.RFC3339, at); err != nil {
			return err
		}
		out = append(out, c)
		return nil
	}, pr)
	return out, err
}
