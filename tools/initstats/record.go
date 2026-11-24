package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/dphaener/physa/internal/vcs"
	"github.com/dphaener/physa/internal/vcs/git"
)

// Record is what leaves the repo. Every string in it comes from a fixed set or
// passes model's or ext's pattern; the rest are numbers.
type Record struct {
	Version  int              `json:"version"`
	Label    string           `json:"label"`
	Date     string           `json:"date"`
	Physa    string           `json:"physa"`
	Clone    string           `json:"clone,omitempty"`
	Repo     Repo             `json:"repo"`
	Phases   map[string]Phase `json:"phases"`
	Gather   *Gather          `json:"gather,omitempty"`
	Proposal *Proposal        `json:"proposal,omitempty"`
	Recovery *Recovery        `json:"recovery,omitempty"`
}

// Repo is the repo's shape: the files init could read, and the commonest
// extensions among them.
type Repo struct {
	Files      int   `json:"files"`
	Bytes      int64 `json:"bytes"`
	Extensions []Ext `json:"extensions"`
}

// Ext is one extension's share of the files.
type Ext struct {
	Ext   string `json:"ext"`
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
}

// Tokens are a session's tokens, as init reports them.
type Tokens struct {
	Input       int64 `json:"input"`
	CachedInput int64 `json:"cached_input"`
	Output      int64 `json:"output"`
}

// Gather is the gather phase's counts.
type Gather struct {
	EstimatedTokens int `json:"estimated_tokens"`
	Bound           struct {
		Since          string `json:"since"`
		MaxPRs         int    `json:"max_prs"`
		PRLimitReached bool   `json:"pr_limit_reached"`
		HistorySince   string `json:"history_since,omitempty"`
	} `json:"bound"`
	Docs struct {
		Count   int `json:"count"`
		Bytes   int `json:"bytes"`
		Skipped int `json:"skipped"`
	} `json:"docs"`
	History struct {
		Changes       int  `json:"changes"`
		WithTrailers  int  `json:"with_trailers"`
		Reverts       int  `json:"reverts"`
		WithAltered   int  `json:"with_altered_files"`
		DistinctFiles int  `json:"distinct_files"`
		Bytes         int  `json:"bytes"`
		Shallow       bool `json:"shallow"`
	} `json:"history"`
	PRs struct {
		Available         bool `json:"available"`
		Count             int  `json:"count"`
		ReviewComments    int  `json:"review_comments"`
		RequestingChanges int  `json:"requesting_changes"`
		LinkedIssues      int  `json:"linked_issues"`
		Bytes             int  `json:"bytes"`
	} `json:"prs"`
	Discovery struct {
		Started bool   `json:"started"`
		Named   int    `json:"named"`
		Added   int    `json:"added"`
		Model   string `json:"model"`
		Tokens  Tokens `json:"tokens"`
	} `json:"discovery"`
}

// Proposal is the propose phase's counts.
type Proposal struct {
	Capabilities int      `json:"capabilities"`
	Unshown      int      `json:"unshown"`
	Questions    int      `json:"questions"`
	Dropped      int      `json:"dropped"`
	Malformed    int      `json:"malformed"`
	PromptTokens int      `json:"prompt_tokens"`
	SourceTokens int      `json:"source_tokens"`
	Sources      []Source `json:"sources"`
	Model        string   `json:"model"`
	Tokens       Tokens   `json:"tokens"`
}

// Source is what proposing read of one kind of source.
type Source struct {
	Name   string `json:"name"`
	Items  int    `json:"items"`
	Tokens int    `json:"tokens"`
}

// Recovery is the recover phase's counts, one row per capability in run
// order, by position rather than name.
type Recovery struct {
	Used         Tokens         `json:"used"`
	Capabilities []Recovered    `json:"capabilities"`
	Statuses     map[string]int `json:"statuses"`
}

// Recovered is one capability's outcome, without its name or text.
type Recovered struct {
	Status   string `json:"status"`
	Attempts int    `json:"attempts"`
	Model    string `json:"model"`
	Tokens   Tokens `json:"tokens"`
	Counts   struct {
		Rules     int `json:"rules"`
		Decisions int `json:"decisions"`
		Questions int `json:"questions"`
		Terms     int `json:"terms"`
	} `json:"counts"`
	Dropped  int  `json:"dropped"`
	Problems int  `json:"problems"`
	Errored  bool `json:"errored"`
}

var (
	modelPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,63}$`)
	extPattern    = regexp.MustCompile(`^\.[a-z0-9]{1,8}$`)
	commitPattern = regexp.MustCompile(`^[0-9a-f]{12}$`)
	datePattern   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)
	sourceNames   = map[string]bool{"docs": true, "pull requests": true, "review comments": true, "changes": true, "clusters": true}
	statuses      = map[string]bool{"recovered": true, "not yet recovered": true, "failed": true}
)

// model keeps a model's name when it looks like one, else "other".
func model(s string) string {
	if modelPattern.MatchString(s) {
		return s
	}
	if s == "" {
		return ""
	}
	return "other"
}

func oneOf(set map[string]bool, s string) string {
	if set[s] {
		return s
	}
	return "other"
}

// build reads a kept run's folder into a record. Each phase's output is
// decoded into the types above, so a field they don't declare is never read.
func build(kept, label string) (Record, error) {
	var m meta
	if _, err := readJSON(filepath.Join(kept, "meta.json"), &m); err != nil {
		return Record{}, err
	}
	rec := Record{Version: 1, Label: label, Phases: m.Phases}
	if len(m.At) >= 10 {
		rec.Date = m.At[:10]
	}
	if strings.HasPrefix(m.Physa, "physa ") {
		rec.Physa = model(strings.TrimPrefix(m.Physa, "physa "))
	}
	if commitPattern.MatchString(m.Clone) {
		rec.Clone = m.Clone
	}
	// Gather's docs list their paths: the record keeps only how many were skipped.
	var g struct {
		Gather
		Docs struct {
			Count   int               `json:"count"`
			Bytes   int               `json:"bytes"`
			Skipped []json.RawMessage `json:"skipped"`
		} `json:"docs"`
	}
	if ok, err := readJSON(filepath.Join(kept, "gather.json"), &g); err != nil {
		return rec, err
	} else if ok {
		out := g.Gather
		out.Docs.Count, out.Docs.Bytes, out.Docs.Skipped = g.Docs.Count, g.Docs.Bytes, len(g.Docs.Skipped)
		out.Discovery.Model = model(out.Discovery.Model)
		out.Bound.Since, out.Bound.HistorySince = date(out.Bound.Since), date(out.Bound.HistorySince)
		rec.Gather = &out
	}
	var p struct {
		Capabilities []struct {
			Unshown   bool              `json:"unshown"`
			Questions []json.RawMessage `json:"questions"`
		} `json:"capabilities"`
		Questions []json.RawMessage `json:"questions"`
		Dropped   []json.RawMessage `json:"dropped"`
		Malformed []json.RawMessage `json:"malformed"`
		Read      struct {
			PromptTokens int      `json:"prompt_tokens"`
			SourceTokens int      `json:"source_tokens"`
			Sources      []Source `json:"sources"`
		} `json:"read"`
		Session struct {
			Model  string `json:"model"`
			Tokens Tokens `json:"tokens"`
		} `json:"session"`
	}
	if ok, err := readJSON(filepath.Join(kept, "proposal.json"), &p); err != nil {
		return rec, err
	} else if ok {
		pr := Proposal{Capabilities: len(p.Capabilities), Questions: len(p.Questions), Dropped: len(p.Dropped),
			Malformed: len(p.Malformed), PromptTokens: p.Read.PromptTokens, SourceTokens: p.Read.SourceTokens,
			Sources: []Source{}, Model: model(p.Session.Model), Tokens: p.Session.Tokens}
		for _, c := range p.Capabilities {
			pr.Questions += len(c.Questions)
			if c.Unshown {
				pr.Unshown++
			}
		}
		for _, s := range p.Read.Sources {
			pr.Sources = append(pr.Sources, Source{Name: oneOf(sourceNames, s.Name), Items: s.Items, Tokens: s.Tokens})
		}
		rec.Proposal = &pr
	}
	var r struct {
		Used         Tokens `json:"used"`
		Capabilities []struct {
			Status   string            `json:"status"`
			Attempts int               `json:"attempts"`
			Model    string            `json:"model"`
			Tokens   Tokens            `json:"tokens"`
			Counts   json.RawMessage   `json:"counts"`
			Dropped  []json.RawMessage `json:"dropped"`
			Problems []json.RawMessage `json:"problems"`
			Error    string            `json:"error"`
		} `json:"capabilities"`
	}
	if ok, err := readJSON(filepath.Join(kept, "recovery.json"), &r); err != nil {
		return rec, err
	} else if ok {
		rv := Recovery{Used: r.Used, Capabilities: []Recovered{}, Statuses: map[string]int{}}
		for _, c := range r.Capabilities {
			o := Recovered{Status: oneOf(statuses, c.Status), Attempts: c.Attempts, Model: model(c.Model), Tokens: c.Tokens,
				Dropped: len(c.Dropped), Problems: len(c.Problems), Errored: c.Error != ""}
			if len(c.Counts) > 0 {
				if err := json.Unmarshal(c.Counts, &o.Counts); err != nil {
					return rec, err
				}
			}
			rv.Statuses[o.Status]++
			rv.Capabilities = append(rv.Capabilities, o)
		}
		rec.Recovery = &rv
	}
	return rec, nil
}

// topExts is how many extensions the record lists; the rest are summed as
// "other".
const topExts = 10

// shape lists the files init could read in dir: tracked, or untracked and not
// ignored.
func shape(dir string) (Repo, error) {
	repo, err := git.Open(dir)
	if err != nil {
		return Repo{}, err
	}
	l, ok := repo.(vcs.Lister)
	if !ok {
		return Repo{}, nil
	}
	files, err := l.Listed()
	if err != nil {
		return Repo{}, err
	}
	root, err := topLevel(dir)
	if err != nil {
		return Repo{}, err
	}
	byExt := map[string]*Ext{}
	out := Repo{Files: len(files), Extensions: []Ext{}}
	for _, f := range files {
		var size int64
		if fi, err := os.Lstat(filepath.Join(root, filepath.FromSlash(f))); err == nil {
			size = fi.Size()
		}
		out.Bytes += size
		e := strings.ToLower(path.Ext(f))
		if !extPattern.MatchString(e) {
			e = "none"
		}
		if byExt[e] == nil {
			byExt[e] = &Ext{Ext: e}
		}
		byExt[e].Files++
		byExt[e].Bytes += size
	}
	all := make([]Ext, 0, len(byExt))
	for _, e := range byExt {
		all = append(all, *e)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Files != all[j].Files {
			return all[i].Files > all[j].Files
		}
		return all[i].Ext < all[j].Ext
	})
	other := Ext{Ext: "other"}
	for i, e := range all {
		if i < topExts {
			out.Extensions = append(out.Extensions, e)
			continue
		}
		other.Files += e.Files
		other.Bytes += e.Bytes
	}
	if other.Files > 0 {
		out.Extensions = append(out.Extensions, other)
	}
	return out, nil
}

// topLevel is the working tree's root: dir or the nearest parent holding .git.
func topLevel(dir string) (string, error) {
	d, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return d, nil
		}
		up := filepath.Dir(d)
		if up == d {
			return "", fmt.Errorf("%s is not in a git working tree", dir)
		}
		d = up
	}
}

// date keeps a value's leading YYYY-MM-DD and nothing else.
func date(s string) string {
	return datePattern.FindString(s)
}
