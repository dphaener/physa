package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/dphaener/physa/internal/mapslice"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs/git"
)

// openStore opens the store of the repository at dir. Tests replace it.
var openStore = gitStore

// gitStore opens the store that lives in the git directory of the repository at dir.
func gitStore(dir string) (*store.Store, error) {
	ctx := context.TODO()
	repo, err := git.Open(ctx, dir)
	if err != nil {
		return nil, err
	}
	sd, err := repo.StoreDir(ctx)
	if err != nil {
		return nil, err
	}
	return store.Open(ctx, sd)
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
		fmt.Fprintf(stderr, "physa map slice: not recorded: %v\n", err)
		return
	}
	defer s.Close()
	rec := store.SliceRecord{
		At:      time.Now().UTC(),
		Session: os.Getenv("CLAUDE_CODE_SESSION_ID"),
	}
	if len(req.Entries) > 0 {
		rec.Entries = req.Entries
	} else {
		rec.Capabilities, rec.With = req.Capabilities, withName(req)
	}
	err = s.PutSliceRecord(context.TODO(), rec)
	if err != nil {
		fmt.Fprintf(stderr, "physa map slice: not recorded: %v\n", err)
	}
}

// records lists the slice records of the store in dir, newest first: 0 when
// it listed, 2 when the store cannot be read or the list cannot be written.
func records(dir string, asJSON bool, stdout, stderr io.Writer) int {
	s, err := openStore(dir)
	if err != nil {
		fmt.Fprintf(stderr, "physa map records: %v\n", err)
		return 2
	}
	defer s.Close()
	recs, err := s.SliceRecords(context.TODO())
	if err != nil {
		fmt.Fprintf(stderr, "physa map records: %v\n", err)
		return 2
	}
	if err := store.WriteRecords(stdout, recs, asJSON); err != nil {
		fmt.Fprintf(stderr, "physa map records: %v\n", err)
		return 2
	}
	return 0
}
