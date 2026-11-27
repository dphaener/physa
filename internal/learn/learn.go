// Package learn is learning's first two stages (design 08 section 2): it
// collects the merged changes not yet learned from, finds the signals each
// one carries, and triages them into changes with a signal and changes with
// nothing to learn. A signal that needs a source Physa cannot read (no forge,
// no slice records, no session reader, or the engine, not built yet) is
// reported as not checked, never as absent. The signals in a change's sessions come from a Sessions
// reader; learn reads no session log and no store itself.
//
// What is already learned is read from the record learning keeps (design 08
// section 6): the Learned-through watermark, the newest default-branch change
// learned from, read from the learn branch and the default branch's history;
// and the Learned ids on the learn branch's unmerged commits and the
// collected changes. A run computes the next watermark and never moves it
// past a change not fully triaged. It spends no tokens and changes nothing;
// drafting proposals and writing the record happen elsewhere.
package learn

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/history"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
)

// learnBranch is the branch learning writes its proposals to. Its commits
// carry the Learned-through watermark and, in Learned trailers, the changes
// they learned from (design 08 section 6).
const learnBranch = "physa/learn"

// minSharedID is how long a shared prefix two ids need to name one change.
const minSharedID = 7

// Options are what a run reads besides version control.
type Options struct {
	// Since is the change the run starts after; empty means after the
	// watermark, or the newest DefaultChanges changes when there is none.
	Since string
	// Forge is the forge to ask about each change's PR; nil means none.
	Forge forge.Forge
	// ForgeReason says why there is no forge, when Forge is nil.
	ForgeReason string
	// Records are the slice records, in any order.
	Records []store.SliceRecord
	// RecordsReason, when not empty, says why the slice records could not be
	// read; the signals that need them are then not checked.
	RecordsReason string
	// Capabilities are the slugs of the map's capabilities.
	Capabilities []string
	// Sessions finds the signals in each change's sessions; nil means none,
	// and the session kinds are then not checked.
	Sessions Sessions
}

// ResultVersion is the version of Result's JSON form.
const ResultVersion = 2

// Result is one run's triage, newest change first in every list.
type Result struct {
	Version int `json:"version"`
	// Range names the changes collected, as a range of version control.
	Range string `json:"range"`
	// Changes is how many changes are learned from: those collected, less
	// the ones already learned.
	Changes        int `json:"changes"`
	AlreadyLearned int `json:"already_learned"`
	// Forge names the forge asked about every change; empty when there was
	// none or it stopped answering.
	Forge       string        `json:"forge"`
	Unavailable []Unavailable `json:"unavailable"`
	// RecordsMissing is how many changes had no slice record in their window
	// and so do not count toward served-not-loaded.
	RecordsMissing int `json:"records_missing"`
	// SessionsRead is how many distinct sessions had their logs read.
	SessionsRead int `json:"sessions_read"`
	// SessionsUnavailable are the sessions behind a change whose logs could
	// not be read. They never hold the watermark back.
	SessionsUnavailable []SessionGap    `json:"sessions_unavailable"`
	Learn               []ChangeSignals `json:"learn"`
	Nothing             []ChangeRef     `json:"nothing"`
	Incomplete          []Incomplete    `json:"incomplete"`
	// Watermark is the full id of the change learning was read to have
	// learned through; empty when there is none.
	Watermark string `json:"watermark"`
	// Through is the full id of the change the next record learns through:
	// past every change triaged, never past one not fully triaged, and
	// never behind Watermark. With nothing triaged and no Watermark it is the
	// change the run started after; empty only when the run started at the
	// first change of the history.
	Through string `json:"through"`
	// LearnedNow are the run's changes with signals, by short id.
	LearnedNow []string `json:"learned_now"`
}

// Unavailable is a kind of signal that was not checked, and why.
type Unavailable struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

// ChangeRef names a change.
type ChangeRef struct {
	Change  string `json:"change"`
	Short   string `json:"short"`
	Subject string `json:"subject"`
}

// ChangeSignals is a change with the signals it carries.
type ChangeSignals struct {
	ChangeRef
	Signals []Signal `json:"signals"`
	// Message is the change's full message, for drafting; not in the JSON form.
	Message string `json:"-"`
	// At is when the change was made, for drafting; not in the JSON form.
	At time.Time `json:"-"`
	// Capabilities are the values of the change's Capability trailers, for
	// drafting; not in the JSON form.
	Capabilities []string `json:"-"`
}

// Incomplete is a change with no signal found whose forge signals could not
// be checked, so it is not known to have nothing to learn.
type Incomplete struct {
	ChangeRef
	Reason string `json:"reason"`
}

// entry is one change being learned from while a run works on it.
type entry struct {
	change   vcs.Change
	from     time.Time // the window's start, exclusive; zero means open
	signals  []Signal
	forgeGap string // why the forge signals were not checked; empty if they were
	records  []store.SliceRecord
	loaded   map[string]bool
}

// Run collects the changes on the default branch not yet learned from, finds
// their signals and triages them. The error is for a history that cannot be
// read; a source of signals that cannot be read is reported in the Result.
func Run(repo vcs.Repo, o Options) (Result, error) {
	ctx := context.TODO()
	ref, err := repo.DefaultBranch(ctx)
	if err != nil {
		return Result{}, err
	}
	watermark, err := readWatermark(ctx, repo, ref)
	if err != nil {
		return Result{}, err
	}
	collected, older, rng, err := collect(ctx, repo, ref, o.Since, watermark)
	if err != nil {
		return Result{}, err
	}
	learned, err := learnedIDs(ctx, repo, ref, collected)
	if err != nil {
		return Result{}, err
	}
	res := Result{Version: ResultVersion, Range: rng, Unavailable: []Unavailable{},
		SessionsUnavailable: []SessionGap{}, Learn: []ChangeSignals{}, Nothing: []ChangeRef{}, Incomplete: []Incomplete{},
		Watermark: watermark, LearnedNow: []string{}}
	var entries []*entry
	for i, c := range collected {
		var from time.Time
		if i+1 < len(collected) {
			from = collected[i+1].Time
		} else if older != nil {
			from = older.Time
		}
		if isRecord(c) {
			continue
		}
		if isLearned(c.ID, learned) {
			res.AlreadyLearned++
			continue
		}
		entries = append(entries, &entry{change: c, from: from})
	}
	res.Changes = len(entries)

	res.Forge, res.Unavailable = askForge(entries, o, res.Unavailable)
	for _, e := range entries {
		if s, ok := revertSignal(ctx, repo, e.change, collected); ok {
			e.signals = append(e.signals, s)
		}
	}
	if o.RecordsReason != "" {
		res.Unavailable = append(res.Unavailable, Unavailable{KindServedNotLoaded, o.RecordsReason})
	} else {
		res.RecordsMissing = placeRecords(entries, o.Records)
		servedNotLoaded(entries, o.Capabilities)
	}
	res.Unavailable = append(res.Unavailable, Unavailable{KindRecurringFinding, reasonRecurringFinding})
	if o.Sessions == nil {
		for _, k := range sessionKinds {
			res.Unavailable = append(res.Unavailable, Unavailable{k, reasonNoSessions})
		}
	} else {
		res.SessionsRead, res.SessionsUnavailable = askSessions(entries, o.Sessions)
	}

	incomplete := map[string]bool{}
	for _, e := range entries {
		ref := ChangeRef{Change: e.change.ID, Short: e.change.Short, Subject: e.change.Subject}
		switch {
		case len(e.signals) > 0:
			res.Learn = append(res.Learn, ChangeSignals{ChangeRef: ref, Signals: e.signals,
				Message: e.change.Message, At: e.change.Time, Capabilities: history.Trailers(e.change.Message, "Capability")})
			res.LearnedNow = append(res.LearnedNow, e.change.Short)
		case e.forgeGap != "":
			res.Incomplete = append(res.Incomplete, Incomplete{ChangeRef: ref,
				Reason: KindCorrection + " and " + KindArguedOptions + " not checked: " + e.forgeGap})
			incomplete[e.change.ID] = true
		default:
			res.Nothing = append(res.Nothing, ref)
		}
	}
	res.Through = nextWatermark(ctx, repo, watermark, collected, incomplete)
	if res.Through == "" && older != nil {
		res.Through = older.ID
	}
	return res, nil
}

// collect returns the changes to learn from on ref, newest first, the change
// just older than the oldest of them when there is one, and the range they
// span: those after since when it is given, else every change after the
// watermark when there is one, else the newest DefaultChanges.
func collect(ctx context.Context, repo vcs.Repo, ref, since, watermark string) ([]vcs.Change, *vcs.Change, string, error) {
	if since == "" && watermark != "" {
		changes, err := repo.Changes(ctx, watermark+".."+ref)
		if err != nil {
			return nil, nil, "", err
		}
		bound, err := repo.Latest(ctx, watermark, 1)
		if err != nil {
			return nil, nil, "", err
		}
		rng := shortID(watermark) + ".." + ref
		var older *vcs.Change
		if len(bound) > 0 {
			older = &bound[0]
			rng = older.Short + ".." + ref
		}
		return changes, older, rng, nil
	}
	if since != "" {
		rng := since + ".." + ref
		changes, err := repo.Changes(ctx, rng)
		if err != nil {
			return nil, nil, "", err
		}
		bound, err := repo.Latest(ctx, since, 1)
		if err != nil {
			return nil, nil, "", err
		}
		var older *vcs.Change
		if len(bound) > 0 {
			older = &bound[0]
		}
		return changes, older, rng, nil
	}
	changes, err := repo.Latest(ctx, ref, DefaultChanges+1)
	if err != nil {
		return nil, nil, "", err
	}
	if len(changes) <= DefaultChanges {
		return changes, nil, ref, nil
	}
	older := changes[DefaultChanges]
	return changes[:DefaultChanges], &older, older.Short + ".." + ref, nil
}

// isLearned reports whether a learned id names change id.
func isLearned(id string, learned []string) bool {
	for _, l := range learned {
		if sameChange(id, l) {
			return true
		}
	}
	return false
}

// sameChange reports whether ids a and b, full or abbreviated, share a prefix
// long enough to name one change: one is a prefix of the other, and the
// shorter has at least minSharedID characters.
func sameChange(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	if len(a) > len(b) {
		a, b = b, a
	}
	return len(a) >= minSharedID && strings.HasPrefix(b, a)
}

// askForge finds each change's correction and argued-options signals from
// its PR. A forge that is missing or cannot log in stops being asked, and
// every change after that has its forge signals not checked. It returns the
// forge's name when it answered for every change, and unavailable with the
// forge's kinds added when it did not.
func askForge(entries []*entry, o Options, unavailable []Unavailable) (string, []Unavailable) {
	f, reason := o.Forge, o.ForgeReason
	if f == nil && reason == "" {
		reason = "no forge"
	}
	for _, e := range entries {
		if f == nil {
			e.forgeGap = reason
			continue
		}
		pr, ok, err := f.ChangePR(e.change.ID)
		switch {
		case errors.Is(err, forge.ErrUnavailable) || errors.Is(err, forge.ErrUnauthenticated):
			f, reason = nil, forge.Reason(err)
			e.forgeGap = reason
		case err != nil:
			e.forgeGap = fmt.Sprintf("the forge could not read the PR of %s: %v", e.change.Short, err)
		case ok:
			if s, found := correctionSignal(e.change, pr); found {
				e.signals = append(e.signals, s)
			}
			if s, found := arguedSignal(e.change, pr); found {
				e.signals = append(e.signals, s)
			}
		}
	}
	if f == nil {
		return "", append(unavailable, Unavailable{KindCorrection, reason}, Unavailable{KindArguedOptions, reason})
	}
	return f.Name(), unavailable
}

// askSessions adds to each change the signals in its sessions. It returns
// how many distinct sessions were read and the sessions whose logs were
// unavailable; those leave the change's triage as it is.
func askSessions(entries []*entry, s Sessions) (int, []SessionGap) {
	read := map[string]bool{}
	gaps := []SessionGap{}
	for _, e := range entries {
		signals, ids, missing := s.Signals(e.change, sessionsOf(e.records), e.from)
		e.signals = append(e.signals, signals...)
		for _, id := range ids {
			read[id] = true
		}
		for _, g := range missing {
			g.Change = e.change.Short
			gaps = append(gaps, g)
		}
	}
	return len(read), gaps
}

// sessionsOf returns the distinct sessions of records, sorted.
func sessionsOf(records []store.SliceRecord) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range records {
		if r.Session != "" && !seen[r.Session] {
			seen[r.Session] = true
			out = append(out, r.Session)
		}
	}
	sort.Strings(out)
	return out
}

// placeRecords gives each change the slice records of its window, the time
// after the next older change up to and including its own, and the
// capabilities its sessions loaded: every record of each session with a
// record in the window, wherever it falls, plus the window's records with no
// session. It returns how many changes had no record in their window.
func placeRecords(entries []*entry, records []store.SliceRecord) int {
	bySession := map[string][]store.SliceRecord{}
	for _, r := range records {
		if r.Session != "" {
			bySession[r.Session] = append(bySession[r.Session], r)
		}
	}
	missing := 0
	for _, e := range entries {
		sessions := map[string]bool{}
		for _, r := range records {
			if r.At.After(e.change.Time) || (!e.from.IsZero() && !r.At.After(e.from)) {
				continue
			}
			e.records = append(e.records, r)
			if r.Session != "" {
				sessions[r.Session] = true
			}
		}
		if len(e.records) == 0 {
			missing++
			continue
		}
		e.loaded = map[string]bool{}
		load := func(r store.SliceRecord) {
			for _, c := range r.Capabilities {
				e.loaded[c] = true
			}
		}
		for _, r := range e.records {
			if r.Session == "" {
				load(r)
			}
		}
		for s := range sessions {
			for _, r := range bySession[s] {
				load(r)
			}
		}
	}
	return missing
}
