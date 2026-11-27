package main

import (
	"context"
	"io"
	"path/filepath"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/adapter/claude/claudelog"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/learn"
	"github.com/dphaener/physa/internal/learnbranch"
	"github.com/dphaener/physa/internal/learnreport"
	"github.com/dphaener/physa/internal/learnsession"
	"github.com/dphaener/physa/internal/person"
	"github.com/dphaener/physa/internal/propose"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs/git"
)

// learnMap is physa map learn: the signals with --signals, else what a draft
// session proposed and the check kept and dropped; --write commits the kept
// ones to physa/learn, --push pushes it, --pr opens its PR. It returns 0 when
// it printed, 1 when the breaker stopped the session, 2 on failure.
func learnMap(ctx context.Context, dir string, o learnbranch.Flags, stdout, stderr io.Writer) int {
	session := propose.Session{Agent: func() (adapter.Adapter, error) { return agentFor(o.Agent) }, Appetite: o.Appetite,
		Progress: stderr, Check: propose.Options{OpenRepo: openRepo, PRExists: prResolver(dir)}}
	if !o.Signals {
		session.Person = resolvePerson(ctx, dir, newForge(dir), stderr)
	}
	var out learnbranch.Outcome
	var err error
	if o.Write {
		out, err = writeBranch(ctx, dir, o, session)
	} else if out.Report, err = draftOnly(ctx, dir, o, session, stdout); err == nil && o.Signals {
		return 0
	}
	if err == nil {
		err = learnbranch.WriteRun(stdout, stderr, out, o.JSON, o.Write)
	}
	if err != nil {
		return failed(stderr, "learn", err)
	}
	if out.Report.Session == propose.SessionStopped {
		return 1
	}
	return 0
}

var resolvePerson = person.ForRepo

func writeBranch(ctx context.Context, dir string, o learnbranch.Flags, s propose.Session) (learnbranch.Outcome, error) {
	w, err := git.OpenWriter(ctx, dir)
	if err != nil {
		return learnbranch.Outcome{}, err
	}
	lo := learnbranch.Options{Push: o.Push, PR: o.PR, Since: o.Since, Session: s, Forge: newForge(dir), Sessions: sessionLogs(dir)}
	lo.PRs, _ = lo.Forge.(forge.PRs)
	lo.Records, lo.RecordsReason = sliceRecords(ctx, dir)
	return learnbranch.Run(ctx, w, lo)
}

// draftOnly drafts from the map and history in dir, or prints the signals.
func draftOnly(ctx context.Context, dir string, o learnbranch.Flags, s propose.Session, stdout io.Writer) (propose.Report, error) {
	repo, err := git.Open(ctx, dir)
	if err != nil {
		return propose.Report{}, err
	}
	lo := learn.Options{Since: o.Since, Forge: newForge(dir), Sessions: sessionLogs(dir)}
	lo.Records, lo.RecordsReason = sliceRecords(ctx, dir)
	m, res, err := learnbranch.Read(ctx, dir, repo, lo)
	if err != nil {
		return propose.Report{}, err
	}
	if o.Signals {
		write := learnreport.WriteText
		if o.JSON {
			write = learnreport.WriteJSON
		}
		return propose.Report{}, write(stdout, res)
	}
	return propose.Run(ctx, dir, m, res, s)
}

// sessionLogs reads the logs of the sessions that worked in dir, whoever drafts.
func sessionLogs(dir string) learn.Sessions {
	abs, _ := filepath.Abs(dir) // fails only with no working directory, then nothing matches
	return learnsession.New(claudelog.New(abs))
}

// sliceRecords reads the slice records of the store in dir, or says why not.
func sliceRecords(ctx context.Context, dir string) (recs []store.SliceRecord, reason string) {
	s, err := openStore(ctx, dir)
	if err == nil {
		defer s.Close()
		recs, err = s.SliceRecords(ctx)
	}
	if err != nil {
		return nil, err.Error()
	}
	return recs, ""
}
