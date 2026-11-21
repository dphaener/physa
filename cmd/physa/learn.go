package main

import (
	"context"
	"io"
	"strconv"
	"strings"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/learn"
	"github.com/dphaener/physa/internal/learnbranch"
	"github.com/dphaener/physa/internal/propose"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs/git"
)

// learnOptions is a parsed physa map learn command line.
type learnOptions struct {
	signals  bool
	since    string
	json     bool
	write    bool   // commit the kept proposals to physa/learn
	push     bool   // push physa/learn; needs write
	pr       bool   // open or refresh the PR; needs push
	appetite int64  // most tokens a draft session may use; zero is the default
	agent    string // agent to draft with; empty is the default
}

// parseLearn reads the arguments after "learn": the flags --signals, --json,
// --write, --push, --pr and the values --since, --appetite and --agent (each
// also as --name=value). It reports false for anything else, --appetite,
// --agent or --write with --signals, --push without --write, --pr without
// --push, and an appetite that is not a positive integer.
func parseLearn(args []string) (learnOptions, bool) {
	var o learnOptions
	flags := map[string]*bool{"--signals": &o.signals, "--json": &o.json, "--write": &o.write, "--push": &o.push, "--pr": &o.pr}
	strs := map[string]*string{"--since": &o.since, "--agent": &o.agent}
	for i := 0; i < len(args); i++ {
		name, val, hasVal := strings.Cut(args[i], "=")
		if b, ok := flags[name]; ok && !hasVal {
			*b = true
			continue
		}
		if !hasVal && i+1 < len(args) {
			i++
			val = args[i]
		}
		if val == "" {
			return o, false
		}
		if p, ok := strs[name]; ok {
			*p = val
		} else if n, err := strconv.ParseInt(val, 10, 64); name == "--appetite" && err == nil && n > 0 {
			o.appetite = n
		} else {
			return o, false
		}
	}
	valid := !o.signals || (o.appetite == 0 && o.agent == "" && !o.write)
	return o, valid && (!o.push || o.write) && (!o.pr || o.push)
}

// learnMap is physa map learn: the signals with --signals, else what a draft
// session proposed and the check kept and dropped; --write commits the kept
// ones to physa/learn, --push pushes it and --pr opens its PR. It returns 0
// when it printed, 1 when the circuit breaker stopped the session, 2 on any
// failure. Without --write it never writes to the repo.
func learnMap(dir string, o learnOptions, stdout, stderr io.Writer) int {
	session := propose.Session{Agent: func() (adapter.Adapter, error) { return agentFor(o.agent) }, Appetite: o.appetite,
		Progress: stderr, Check: propose.Options{OpenRepo: git.Open, PRExists: prResolver(dir)}}
	var out learnbranch.Outcome
	var err error
	if o.write {
		out, err = writeBranch(dir, o, session)
	} else if out.Report, err = draftOnly(dir, o, session, stdout); err == nil && o.signals {
		return 0
	}
	if err == nil {
		err = learnbranch.WriteRun(stdout, stderr, out, o.json, o.write)
	}
	if err != nil {
		return failed(stderr, "learn", err)
	}
	if out.Report.Session == propose.SessionStopped {
		return 1
	}
	return 0
}

func writeBranch(dir string, o learnOptions, s propose.Session) (learnbranch.Outcome, error) {
	w, err := git.OpenWriter(dir)
	if err != nil {
		return learnbranch.Outcome{}, err
	}
	lo := learnbranch.Options{Push: o.push, PR: o.pr, Since: o.since, Session: s, Forge: newForge(dir)}
	lo.PRs, _ = lo.Forge.(forge.PRs)
	lo.Records, lo.RecordsReason = sliceRecords(dir)
	return learnbranch.Run(context.Background(), w, lo)
}

// draftOnly drafts from the map and history in dir, or prints the signals.
func draftOnly(dir string, o learnOptions, s propose.Session, stdout io.Writer) (propose.Report, error) {
	repo, err := git.Open(dir)
	if err != nil {
		return propose.Report{}, err
	}
	lo := learn.Options{Since: o.since, Forge: newForge(dir)}
	lo.Records, lo.RecordsReason = sliceRecords(dir)
	m, res, err := learnbranch.Read(dir, repo, lo)
	if err != nil {
		return propose.Report{}, err
	}
	if o.signals {
		write := learn.WriteText
		if o.json {
			write = learn.WriteJSON
		}
		return propose.Report{}, write(stdout, res)
	}
	return propose.Run(context.Background(), dir, m, res, s)
}

// sliceRecords reads the slice records of the store in dir, or says why not.
func sliceRecords(dir string) (recs []store.SliceRecord, reason string) {
	s, err := openStore(dir)
	if err == nil {
		defer s.Close()
		recs, err = s.SliceRecords()
	}
	if err != nil {
		return nil, err.Error()
	}
	return recs, ""
}
