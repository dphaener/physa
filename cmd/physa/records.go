package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/mapslice"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs/git"
)

// openStore opens the store of the repository at dir. Tests replace it.
var openStore = gitStore

// gitStore opens the store that lives in the git directory of the repository at dir.
func gitStore(dir string) (*store.Store, error) {
	repo, err := git.Open(dir)
	if err != nil {
		return nil, err
	}
	sd, err := repo.StoreDir()
	if err != nil {
		return nil, err
	}
	return store.Open(sd)
}

// withName names which extras a slice request asked for.
func withName(r mapslice.Request) string {
	switch {
	case r.WithDecisions && r.WithQuestions:
		return "all"
	case r.WithDecisions:
		return "decisions"
	case r.WithQuestions:
		return "questions"
	}
	return ""
}

// recordSlice writes a slice record for req. A store that cannot be used
// costs one warning line on stderr and nothing else.
func recordSlice(dir string, req mapslice.Request, stderr io.Writer) {
	s, err := openStore(dir)
	if err != nil {
		fmt.Fprintf(stderr, "physa slice: not recorded: %v\n", err)
		return
	}
	defer s.Close()
	err = s.PutSliceRecord(store.SliceRecord{
		At:           time.Now().UTC(),
		Capabilities: req.Capabilities,
		With:         withName(req),
		Budget:       req.Budget,
		Session:      os.Getenv("CLAUDE_CODE_SESSION_ID"),
	})
	if err != nil {
		fmt.Fprintf(stderr, "physa slice: not recorded: %v\n", err)
	}
}

// parseRecords reads the arguments after "records": only --json.
func parseRecords(args []string) (asJSON, ok bool) {
	for _, a := range args {
		if a != "--json" {
			return false, false
		}
		asJSON = true
	}
	return asJSON, true
}

// records lists the slice records of the store in dir, newest first: 0 when
// it listed, 2 when the store cannot be read or the list cannot be written.
func records(dir string, asJSON bool, stdout, stderr io.Writer) int {
	s, err := openStore(dir)
	if err != nil {
		fmt.Fprintf(stderr, "physa records: %v\n", err)
		return 2
	}
	defer s.Close()
	recs, err := s.SliceRecords()
	if err != nil {
		fmt.Fprintf(stderr, "physa records: %v\n", err)
		return 2
	}
	if asJSON {
		if recs == nil {
			recs = []store.SliceRecord{}
		}
		if err := json.NewEncoder(stdout).Encode(recs); err != nil {
			fmt.Fprintf(stderr, "physa records: %v\n", err)
			return 2
		}
		return 0
	}
	if len(recs) == 0 {
		fmt.Fprintln(stdout, "No slice records.")
		return 0
	}
	for _, r := range recs {
		session := r.Session
		if session == "" {
			session = "—"
		}
		line := fmt.Sprintf("%s %s %s", r.At.UTC().Format(time.RFC3339), session, strings.Join(r.Capabilities, ","))
		if r.With != "" {
			line += " with " + r.With
		}
		if r.Budget != 0 {
			line += " budget " + strconv.Itoa(r.Budget)
		}
		fmt.Fprintln(stdout, line)
	}
	return 0
}
