// Package coverage places each gathered PR and change with its nearest
// proposed capability, by the files it touches against the files and
// clusters the capability's own citations touch, so recovery reads them and
// not only what propose cited. A file many capabilities touch counts for
// little. An item is placed only when at least half its files, by weight,
// are near the capability, and chores (dependency bumps, CI,
// formatting) are never placed. It also says what no capability
// got: the gathered PRs placed nowhere, and the clusters of files no
// capability's citations touch. It is deterministic and starts no session.
package coverage

import (
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/reading"
	"github.com/dphaener/physa/internal/store"
)

// Low is the share of gathered PRs, or of clusters, left with no
// capability past which coverage is low, in percent. A first guess, to be
// tuned from real runs.
const Low = 20

// MinShare is the share of an item's files, in percent, that must be in a
// capability's footprint or its directories for the item to be placed with
// it. A first guess, to be tuned from real runs.
const MinShare = 50

// Placement is what each capability reads beyond its citations, by name:
// the PRs and changes placed with it.
type Placement struct {
	PRs     map[string][]int
	Changes map[string][]string // full ids
}

// Report is how far the proposal reaches into the gather. Cited counts the
// gathered PRs some capability cites, Placed those placed by their files,
// and Unplaced those neither: Unfiled of them because no gathered change
// names the PR, so it has no files to place it by, and Chores of them
// because they are chores, which are never placed. Clusters counts the
// clusters of files that change together, and Uncovered lists those no
// capability's citations touch, each with its first reading.ClusterPaths
// paths.
type Report struct {
	PRs       int               `json:"prs"`
	Cited     int               `json:"cited"`
	Placed    int               `json:"placed"`
	Unplaced  int               `json:"unplaced"`
	Unfiled   int               `json:"unfiled"`
	Chores    int               `json:"chores"`
	Clusters  int               `json:"clusters"`
	Uncovered []reading.Cluster `json:"uncovered"`
	Low       bool              `json:"low"`
}

// prRef finds the PR a change came from: GitHub's squash subject, "Title
// (#12)", or its merge subject, "Merge pull request #12 from …".
var prRef = regexp.MustCompile(`\(#(\d+)\)\s*$|^Merge pull request #(\d+)\b`)

// PROf is the number of the PR change ch came from, or 0.
func PROf(ch store.GatheredChange) int {
	m := prRef.FindStringSubmatch(ch.Subject)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1] + m[2])
	return n
}

// choreSubject is a subject that names a chore: a Conventional Commits
// chore, ci, build or style type, or a dependency bot's "Bump x from y".
var choreSubject = regexp.MustCompile(`(?i)^((chore|ci|build|style)(\([^)]*\))?!?:|bump \S+ from )`)

// choreFiles are files only chores touch: dependency manifests and
// lockfiles, and the configuration of CI, formatters and linters.
var choreFiles = map[string]bool{
	"go.mod": true, "go.sum": true, "Gemfile": true, "Gemfile.lock": true, "package.json": true, "package-lock.json": true,
	"yarn.lock": true, "pnpm-lock.yaml": true, "Cargo.toml": true, "Cargo.lock": true, "poetry.lock": true, "requirements.txt": true,
	"composer.lock": true, "mix.lock": true, ".gitlab-ci.yml": true, ".travis.yml": true, ".editorconfig": true,
	".rubocop.yml": true, ".golangci.yml": true, ".prettierrc": true, ".eslintrc.json": true, ".pre-commit-config.yaml": true,
}

// Chore reports whether an item with this subject or title and these
// files is a chore: its subject says so, or every file it touches is a
// dependency file or CI, formatter or linter configuration.
func Chore(subject string, files []string) bool {
	if choreSubject.MatchString(strings.TrimSpace(subject)) {
		return true
	}
	for _, f := range files {
		if !choreFiles[path.Base(f)] && !strings.HasPrefix(f, ".github/") && !strings.HasPrefix(f, ".circleci/") && !strings.HasPrefix(f, ".buildkite/") {
			return false
		}
	}
	return len(files) > 0
}

// footprint is the files a capability's citations touch, with every
// cluster they reach in full, and the directories of those files.
type footprint struct {
	files, dirs map[string]bool
}

// unit is the weight of a file no other footprint holds.
const unit = 1_000_000

// weights is what each file counts for when placing: unit divided by the
// number of footprints that hold it, so a file many capabilities touch, such
// as a schema dump or the routes, says little about which one an item
// belongs to; a file no footprint holds counts unit. A file held by most
// footprints, more than half and more than two, counts nothing.
type weights map[string]int

func (w weights) of(file string) int {
	if n, ok := w[file]; ok {
		return n
	}
	return unit
}

// score is how near files are to f, weighed: those in it, then those in it
// or in one of its directories. The repo's root is no directory of a
// footprint, so files there are near only when the footprint holds them.
func (f footprint) score(files []string, w weights) [2]int {
	var s [2]int
	for _, p := range files {
		if f.files[p] {
			s[0] += w.of(p)
		}
		if d := path.Dir(p); f.files[p] || d != "." && f.dirs[d] {
			s[1] += w.of(p)
		}
	}
	return s
}

// Place places each gathered PR and change no capability cites with the
// capability whose footprint holds the most of its files, then the most of
// its files' directories, each file weighed by how many footprints hold it
// (weights); a tie goes to the capability proposed first. An item is placed
// nowhere when less than MinShare percent of its files' weight is near that
// capability, when all its files are held by most footprints, or when it is
// a Chore. A PR's files are those of the
// changes that name it. It returns the placement and the report.
func Place(g store.Gather, caps []mapping.Capability) (Placement, Report) {
	pl := Placement{PRs: map[string][]int{}, Changes: map[string][]string{}}
	byPR, choreOnly := map[int][]string{}, map[int]bool{}
	for _, ch := range g.Changes {
		if n := PROf(ch); n > 0 {
			_, seen := choreOnly[n]
			choreOnly[n] = (!seen || choreOnly[n]) && Chore(ch.Subject, ch.Files)
			byPR[n] = append(byPR[n], ch.Files...)
		}
	}
	// prChore is whether PR n is a chore: by its title, its files, or every
	// change that names it.
	prChore := func(p store.GatheredPR) bool { return choreOnly[p.Number] || Chore(p.Title, byPR[p.Number]) }
	citedPR, citedChange := map[int]bool{}, map[string]bool{}
	feet := make([]footprint, len(caps))
	all := reading.Clusters(g.Changes)
	touched := make([]bool, len(all))
	for i, c := range caps {
		own := map[string]bool{}
		for _, r := range refs(c) {
			if n, err := strconv.Atoi(strings.TrimPrefix(r, "#")); strings.HasPrefix(r, "#") && err == nil {
				citedPR[n] = true
				for _, f := range byPR[n] {
					own[f] = true
				}
				continue
			}
			for _, ch := range g.Changes {
				if r == ch.Short || strings.HasPrefix(ch.ID, r) {
					citedChange[ch.ID] = true
					for _, f := range ch.Files {
						own[f] = true
					}
				}
			}
		}
		fp := footprint{files: map[string]bool{}, dirs: map[string]bool{}}
		for f := range own {
			fp.files[f] = true
		}
		for k, cl := range all {
			for _, p := range cl.Paths {
				if own[p] {
					touched[k] = true
					for _, q := range cl.Paths {
						fp.files[q] = true
					}
					break
				}
			}
		}
		for f := range fp.files {
			fp.dirs[path.Dir(f)] = true
		}
		feet[i] = fp
	}
	held := map[string]int{}
	for _, fp := range feet {
		for f := range fp.files {
			held[f]++
		}
	}
	w := weights{}
	for f, n := range held {
		if w[f] = unit / n; n*2 > len(feet) && n > 2 {
			w[f] = 0
		}
	}
	nearest := func(subject string, files []string) (string, bool) {
		if Chore(subject, files) {
			return "", false
		}
		best, at, total := [2]int{}, -1, 0
		for _, f := range files {
			total += w.of(f)
		}
		for i, fp := range feet {
			if s := fp.score(files, w); s[0] > best[0] || s[0] == best[0] && s[1] > best[1] {
				best, at = s, i
			}
		}
		if at < 0 || total == 0 || best[1]*100 < total*MinShare {
			return "", false
		}
		return caps[at].Name, true
	}
	var rep Report
	for _, ch := range g.Changes {
		if !citedChange[ch.ID] {
			if name, ok := nearest(ch.Subject, ch.Files); ok {
				pl.Changes[name] = append(pl.Changes[name], ch.ID)
			}
		}
	}
	for _, p := range g.PRs {
		rep.PRs++
		name, ok := "", false
		if !prChore(p) {
			name, ok = nearest(p.Title, byPR[p.Number])
		}
		switch {
		case citedPR[p.Number]:
			rep.Cited++
		case ok:
			rep.Placed++
			pl.PRs[name] = append(pl.PRs[name], p.Number)
		default:
			rep.Unplaced++
			switch {
			case prChore(p):
				rep.Chores++
			case len(byPR[p.Number]) == 0:
				rep.Unfiled++
			}
		}
	}
	rep.Clusters, rep.Uncovered = len(all), []reading.Cluster{}
	for k, cl := range all {
		if !touched[k] {
			cl.Paths = cl.Paths[:min(len(cl.Paths), reading.ClusterPaths)]
			rep.Uncovered = append(rep.Uncovered, cl)
		}
	}
	// Chores reach no capability by design, so they do not lower coverage.
	rep.Low = (rep.Unplaced-rep.Chores)*100 > (rep.PRs-rep.Chores)*Low || len(rep.Uncovered)*100 > rep.Clusters*Low
	return pl, rep
}

// refs is what c cites: the PRs and changes that show it and that
// suggested its questions, trimmed, each once.
func refs(c mapping.Capability) []string {
	seen := map[string]bool{"": true}
	var out []string
	for _, r := range append(append([]string{}, c.ShownBy...), suggested(c)...) {
		if r = strings.TrimSpace(r); !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}

func suggested(c mapping.Capability) []string {
	var out []string
	for _, q := range c.Questions {
		out = append(out, q.SuggestedBy...)
	}
	return out
}

// shown is how many uncovered clusters WriteText lists.
const shown = 10

// Text is r for a person: what propose cited, what was placed by files
// and what was left, the clusters no capability touches, the largest first,
// and, when coverage is low, that it is.
func Text(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Coverage: the capabilities cite %d of %d gathered PRs; %d more are placed with the nearest by their files, so recovery reads them, and %d reach none", r.Cited, r.PRs, r.Placed, r.Unplaced)
	var why []string
	if r.Chores > 0 {
		why = append(why, fmt.Sprintf("%d %s, such as dependency bumps, CI and formatting, which are never placed", r.Chores, map[bool]string{true: "chore", false: "chores"}[r.Chores == 1]))
	}
	if r.Unfiled > 0 {
		why = append(why, fmt.Sprintf("%d with no change naming them, so no files to place them by", r.Unfiled))
	}
	if len(why) > 0 {
		fmt.Fprintf(&b, " (%s)", strings.Join(why, "; "))
	}
	fmt.Fprintf(&b, ".\nClusters: %d of %d touch no capability.\n", len(r.Uncovered), r.Clusters)
	for i, c := range r.Uncovered {
		if i == shown {
			fmt.Fprintf(&b, "  and %d more\n", len(r.Uncovered)-shown)
			break
		}
		fmt.Fprintf(&b, "  %s (%d changes)\n", strings.Join(c.Paths, ", "), c.Changes)
	}
	if r.Low {
		fmt.Fprintf(&b, "Coverage is low: more than %d%% of the PRs or clusters reach no capability, so areas of the product may have none. Review the clusters above; physa init --propose proposes again.\n", Low)
	}
	return b.String()
}

// Warn says on w, as init's warning, when r's coverage is low; init goes on.
func Warn(w io.Writer, r Report) {
	if r.Low {
		fmt.Fprintf(w, "physa init: warning: coverage is low: %d of %d gathered PRs and %d of %d clusters reach no capability; init goes on, and the proposal's coverage says which.\n", r.Unplaced, r.PRs, len(r.Uncovered), r.Clusters)
	}
}
