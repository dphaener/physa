package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/dphaener/physa/internal/learn"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs/git"
)

// learnOptions is a parsed physa map learn command line.
type learnOptions struct {
	signals bool
	since   string
	json    bool
}

// parseLearn reads the arguments after "learn": --signals, --json and
// --since <change> (or --since=<change>). It reports false for anything else.
func parseLearn(args []string) (learnOptions, bool) {
	var o learnOptions
	for i := 0; i < len(args); i++ {
		name, val, hasVal := strings.Cut(args[i], "=")
		switch name {
		case "--signals", "--json":
			if hasVal {
				return o, false
			}
			if name == "--signals" {
				o.signals = true
			} else {
				o.json = true
			}
		case "--since":
			if !hasVal {
				if i+1 >= len(args) {
					return o, false
				}
				i++
				val = args[i]
			}
			if val == "" {
				return o, false
			}
			o.since = val
		default:
			return o, false
		}
	}
	return o, true
}

// learnMap is physa map learn: 0 when it printed, whatever it found, and 2
// when drafting is asked for (not built), or the map or the history cannot be
// read or the output cannot be written.
func learnMap(dir string, o learnOptions, stdout, stderr io.Writer) int {
	if !o.signals {
		fmt.Fprintln(stderr, "physa map learn: drafting is planned; run physa map learn --signals")
		return 2
	}
	m, _, err := mapfile.Read(dir)
	if err != nil {
		fmt.Fprintf(stderr, "physa map learn: %v\n", err)
		return 2
	}
	var slugs []string
	for _, c := range m.Capabilities {
		slugs = append(slugs, c.Name)
	}
	repo, err := git.Open(dir)
	if err != nil {
		fmt.Fprintf(stderr, "physa map learn: %v\n", err)
		return 2
	}
	opts := learn.Options{Since: o.since, Forge: newForge(dir), Capabilities: slugs}
	opts.Records, opts.RecordsReason = sliceRecords(dir)
	res, err := learn.Run(repo, opts)
	if err != nil {
		fmt.Fprintf(stderr, "physa map learn: %v\n", err)
		return 2
	}
	write := learn.WriteText
	if o.json {
		write = learn.WriteJSON
	}
	if err := write(stdout, res); err != nil {
		fmt.Fprintf(stderr, "physa map learn: %v\n", err)
		return 2
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
