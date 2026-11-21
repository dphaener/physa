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
}

// Doc is one gathered document.
type Doc struct {
	Path string
	Kind string
	Body string
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
	Number   int
	Title    string
	Body     string
	Author   string
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

func jsonText(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

// PutGather replaces the stored gather with g in one transaction: on any
// error the earlier gather is left intact. It also deletes the stored init
// proposal, which belongs to one gather.
func (s *Store) PutGather(g Gather) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range []string{"gather_comments", "gather_prs", "gather_changes", "gather_docs", "gather_runs", "init_proposal"} {
		if _, err := tx.Exec("DELETE FROM " + t); err != nil {
			return err
		}
	}
	r := g.Run
	if _, err := tx.Exec(`INSERT INTO gather_runs (id, at, since, max_prs, prs_limited, forge, forge_reason) VALUES (1, ?, ?, ?, ?, ?, ?)`,
		ts(r.At), ts(r.Since), r.MaxPRs, b2i(r.PRsLimited), r.Forge, r.ForgeReason); err != nil {
		return err
	}
	for _, d := range g.Docs {
		if _, err := tx.Exec(`INSERT INTO gather_docs (path, kind, body) VALUES (?, ?, ?)`, d.Path, d.Kind, d.Body); err != nil {
			return err
		}
	}
	for i, c := range g.Changes {
		trailers := c.Trailers
		if trailers == nil {
			trailers = []Trailer{}
		}
		files := c.Files
		if files == nil {
			files = []string{}
		}
		tj, err := jsonText(trailers)
		if err != nil {
			return err
		}
		fj, err := jsonText(files)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO gather_changes (id, short, subject, message, author, at, trailers, reverts, files, seq) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.ID, c.Short, c.Subject, c.Message, c.Author, ts(c.At), tj, c.Reverts, fj, i); err != nil {
			return err
		}
	}
	for i, p := range g.PRs {
		issues := p.Issues
		if issues == nil {
			issues = []string{}
		}
		ij, err := jsonText(issues)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO gather_prs (number, title, body, author, merged_at, issues, seq) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			p.Number, p.Title, p.Body, p.Author, ts(p.MergedAt), ij, i); err != nil {
			return err
		}
		for j, c := range p.Comments {
			if _, err := tx.Exec(`INSERT INTO gather_comments (pr, author, at, kind, requests_changes, body, seq) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				p.Number, c.Author, ts(c.At), c.Kind, b2i(c.RequestsChanges), c.Body, j); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
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
	var at, since string
	var limited int
	err := s.db.QueryRow(`SELECT at, since, max_prs, prs_limited, forge, forge_reason FROM gather_runs WHERE id = 1`).
		Scan(&at, &since, &g.Run.MaxPRs, &limited, &g.Run.Forge, &g.Run.ForgeReason)
	if err == sql.ErrNoRows {
		return Gather{}, false, nil
	}
	if err != nil {
		return Gather{}, false, err
	}
	g.Run.PRsLimited = limited != 0
	if g.Run.At, err = time.Parse(time.RFC3339, at); err != nil {
		return Gather{}, false, err
	}
	if g.Run.Since, err = time.Parse(time.RFC3339, since); err != nil {
		return Gather{}, false, err
	}
	if g.Docs, err = s.gatheredDocs(); err != nil {
		return Gather{}, false, err
	}
	if g.Changes, err = s.gatheredChanges(); err != nil {
		return Gather{}, false, err
	}
	if g.PRs, err = s.gatheredPRs(); err != nil {
		return Gather{}, false, err
	}
	return g, true, nil
}

func (s *Store) gatheredDocs() ([]Doc, error) {
	rows, err := s.db.Query(`SELECT path, kind, body FROM gather_docs ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Doc
	for rows.Next() {
		var d Doc
		if err := rows.Scan(&d.Path, &d.Kind, &d.Body); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) gatheredChanges() ([]GatheredChange, error) {
	rows, err := s.db.Query(`SELECT id, short, subject, message, author, at, trailers, reverts, files FROM gather_changes ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GatheredChange
	for rows.Next() {
		var c GatheredChange
		var at, tj, fj string
		if err := rows.Scan(&c.ID, &c.Short, &c.Subject, &c.Message, &c.Author, &at, &tj, &c.Reverts, &fj); err != nil {
			return nil, err
		}
		if c.At, err = time.Parse(time.RFC3339, at); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(tj), &c.Trailers); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(fj), &c.Files); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) gatheredPRs() ([]GatheredPR, error) {
	rows, err := s.db.Query(`SELECT number, title, body, author, merged_at, issues FROM gather_prs ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	var out []GatheredPR
	for rows.Next() {
		var p GatheredPR
		var at, ij string
		if err := rows.Scan(&p.Number, &p.Title, &p.Body, &p.Author, &at, &ij); err != nil {
			rows.Close()
			return nil, err
		}
		if p.MergedAt, err = time.Parse(time.RFC3339, at); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal([]byte(ij), &p.Issues); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i := range out {
		cs, err := s.gatheredComments(out[i].Number)
		if err != nil {
			return nil, err
		}
		out[i].Comments = cs
	}
	return out, nil
}

func (s *Store) gatheredComments(pr int) ([]GatheredComment, error) {
	rows, err := s.db.Query(`SELECT author, at, kind, requests_changes, body FROM gather_comments WHERE pr = ? ORDER BY seq`, pr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GatheredComment
	for rows.Next() {
		var c GatheredComment
		var at string
		var rc int
		if err := rows.Scan(&c.Author, &at, &c.Kind, &rc, &c.Body); err != nil {
			return nil, err
		}
		c.RequestsChanges = rc != 0
		if c.At, err = time.Parse(time.RFC3339, at); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
