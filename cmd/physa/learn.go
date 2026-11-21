package main

import (
	"context"
	"io"
	"strconv"
	"strings"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/learn"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/propose"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs/git"
)

// learnOptions is a parsed physa map learn command line.
type learnOptions struct {
	signals  bool
	since    string
	json     bool
	appetite int64  // most tokens a draft session may use; zero is the default
	agent    string // agent to draft with; empty is the default
}

// parseLearn reads the arguments after "learn": --signals, --json,
// --since <change>, --appetite <tokens> and --agent <name> (each also as
// --name=value). It reports false for anything else, for --appetite or
// --agent with --signals, and for an appetite that is not a positive integer.
func parseLearn(args []string) (learnOptions, bool) {
	var o learnOptions
	for i := 0; i < len(args); i++ {
		name, val, hasVal := strings.Cut(args[i], "=")
		if (name == "--signals" || name == "--json") && !hasVal {
			if name == "--signals" {
				o.signals = true
			} else {
				o.json = true
			}
			continue
		}
		if !hasVal && i+1 < len(args) {
			i++
			val = args[i]
		}
		if val == "" {
			return o, false
		}
		n, err := strconv.ParseInt(val, 10, 64)
		switch {
		case name == "--since":
			o.since = val
		case name == "--agent":
			o.agent = val
		case name == "--appetite" && err == nil && n > 0:
			o.appetite = n
		default:
			return o, false
		}
	}
	return o, !o.signals || (o.appetite == 0 && o.agent == "")
}

// learnMap is physa map learn: the signals with --signals, else what a draft
// session proposed and the check kept and dropped. It returns 0 when it
// printed, 1 when the circuit breaker stopped the session, and 2 on any other
// failure. It never writes to the map.
func learnMap(dir string, o learnOptions, stdout, stderr io.Writer) int {
	fail := func(err error) int { return failed(stderr, "learn", err) }
	m, _, err := mapfile.Read(dir)
	if err != nil {
		return fail(err)
	}
	var slugs []string
	for _, c := range m.Capabilities {
		slugs = append(slugs, c.Name)
	}
	repo, err := git.Open(dir)
	if err != nil {
		return fail(err)
	}
	opts := learn.Options{Since: o.since, Forge: newForge(dir), Capabilities: slugs}
	opts.Records, opts.RecordsReason = sliceRecords(dir)
	res, err := learn.Run(repo, opts)
	if err != nil {
		return fail(err)
	}
	if o.signals {
		write := learn.WriteText
		if o.json {
			write = learn.WriteJSON
		}
		if err := write(stdout, res); err != nil {
			return fail(err)
		}
		return 0
	}
	report, err := propose.Run(context.Background(), dir, m, res, propose.Session{
		Agent:    func() (adapter.Adapter, error) { return agentFor(o.agent) },
		Appetite: o.appetite,
		Progress: stderr,
		Check:    propose.Options{OpenRepo: git.Open, PRExists: prResolver(dir)},
	})
	if err != nil {
		return fail(err)
	}
	write := propose.WriteMarkdown
	if o.json {
		write = propose.WriteJSON
	}
	if err := write(stdout, report); err != nil {
		return fail(err)
	}
	if report.Session == propose.SessionStopped {
		return 1
	}
	return 0
}

// sliceRecords reads the slice records of the store in dir, or says why not.
func sliceRecords(dir string) ([]store.SliceRecord, string) {
	s, err := openStore(dir)
	if err != nil {
		return nil, err.Error()
	}
	defer s.Close()
	recs, err := s.SliceRecords()
	if err != nil {
		return nil, err.Error()
	}
	return recs, ""
}
