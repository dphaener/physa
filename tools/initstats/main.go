// Command initstats measures a run of physa init on a repo whose map and code
// must stay where they are, and prints a record of numbers that can leave it.
//
// It runs init's three phases in the repo (--gather, --propose, --recover,
// each with --json), times each, and keeps their raw output under the repo's
// store (.git/physa/initstats/<time>), which is never committed. Then it prints
// the record: the repo's shape, each phase's time and exit, and init's counts
// and tokens. The record is built from an allowlist: only fields declared
// below are read, strings only from fixed sets (statuses, source names,
// extensions, models), so names, paths, promises, questions and map text never
// reach it. Read it before it leaves.
//
//	go build -o ~/bin/initstats ./tools/initstats   # once, in Physa's clone
//	initstats -label repo-a [-dir REPO] [-physa physa] [-agent NAME] [-since YYYY-MM-DD] [-max-prs N]
//	initstats -label repo-a -from REPO/.git/physa/initstats/<time>
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/vcs/git"
)

// phases are init's phases in run order: the flag that runs each and the file
// its raw output is kept in.
var phases = []struct{ flag, name string }{
	{"--gather", "gather"},
	{"--propose", "proposal"},
	{"--recover", "recovery"},
}

func main() {
	label := flag.String("label", "", "the repo's name in the record, such as repo-a (required)")
	dir := flag.String("dir", ".", "the repo to run init in")
	physa := flag.String("physa", "physa", "the physa binary")
	agent := flag.String("agent", "", "passed to init as --agent")
	from := flag.String("from", "", "a kept run's folder: print its record without running init again")
	since := flag.String("since", "", "passed to init's gather as --since (YYYY-MM-DD): a shorter window of history and PRs")
	maxPRs := flag.String("max-prs", "", "passed to init's gather as --max-prs")
	flag.Parse()
	if !regexp.MustCompile(`^[a-z0-9-]{1,32}$`).MatchString(*label) {
		fail(errors.New("-label is required: lowercase letters, digits and dashes, such as repo-a"))
	}
	kept := *from
	if kept == "" {
		var err error
		if kept, err = run(*dir, *physa, *agent, *since, *maxPRs); err != nil {
			fail(err)
		}
		fmt.Fprintf(os.Stderr, "initstats: raw output kept in %s\n", kept)
	}
	rec, err := build(kept, *label)
	if err == nil {
		rec.Repo, err = shape(*dir)
		warn(os.Stderr, kept, rec)
	}
	if err != nil {
		fail(err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rec); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "initstats:", err)
	os.Exit(2)
}

// meta is what run keeps beside the raw output: when, which physa, and each
// phase's time and exit code.
type meta struct {
	At     string           `json:"at"`
	Physa  string           `json:"physa"`
	Clone  string           `json:"clone"`
	Phases map[string]Phase `json:"phases"`
}

// Phase is one phase's wall time and exit code.
type Phase struct {
	Seconds float64 `json:"seconds"`
	Exit    int     `json:"exit"`
}

// run runs the phases in dir and keeps their output; it returns the folder.
// A phase that fails ends the run, since the next one reads what it stored.
func run(dir, physa, agent, since, maxPRs string) (string, error) {
	ctx := context.TODO()
	repo, err := git.Open(ctx, dir)
	if err != nil {
		return "", err
	}
	store, err := repo.StoreDir(ctx)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	kept := filepath.Join(store, "initstats", now.Format("20060102T150405Z"))
	if err := os.MkdirAll(kept, 0o755); err != nil {
		return "", err
	}
	ver, _ := exec.Command(physa, "version").Output()
	m := meta{At: now.Format(time.RFC3339), Physa: strings.TrimSpace(strings.SplitN(string(ver), "\n", 2)[0]), Clone: clone(), Phases: map[string]Phase{}}
	for _, p := range phases {
		args := []string{"init", p.flag, "--json"}
		if agent != "" {
			args = append(args, "--agent", agent)
		}
		if p.name == "gather" && since != "" {
			args = append(args, "--since", since)
		}
		if p.name == "gather" && maxPRs != "" {
			args = append(args, "--max-prs", maxPRs)
		}
		out, err := os.Create(filepath.Join(kept, p.name+".json"))
		if err != nil {
			return "", err
		}
		errOut, err := os.Create(filepath.Join(kept, p.name+".stderr"))
		if err != nil {
			return "", err
		}
		cmd := exec.Command(physa, args...)
		cmd.Dir, cmd.Stdout, cmd.Stderr = dir, out, io.MultiWriter(errOut, os.Stderr)
		start := time.Now()
		err = cmd.Run()
		ph := Phase{Seconds: time.Since(start).Round(time.Second).Seconds()}
		err = errors.Join(err, out.Close(), errOut.Close())
		var exit *exec.ExitError
		switch {
		case errors.As(err, &exit):
			ph.Exit = exit.ExitCode()
		case err != nil:
			return "", err
		}
		m.Phases[p.name] = ph
		if err := writeJSON(filepath.Join(kept, "meta.json"), m); err != nil {
			return "", err
		}
		// Recovery exits 1 when a capability is not yet recovered; that is a
		// result to record, not a failed phase. Anything else stops here.
		if ph.Exit != 0 && !(p.name == "recovery" && ph.Exit == 1) {
			fmt.Fprintf(os.Stderr, "initstats: %s exited %d; recording what ran\n", p.name, ph.Exit)
			break
		}
	}
	return kept, nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// readJSON decodes path into v; a missing file leaves v as it is.
func readJSON(path string, v any) (bool, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(b, v)
}

// clone is the commit of the Physa clone this program's source is in, the
// clone physa is usually built from, since source builds report "dev". It is
// empty when the source isn't in a clone.
func clone() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	ctx := context.TODO()
	repo, err := git.Open(ctx, filepath.Dir(file))
	if err != nil {
		return ""
	}
	id, ok := repo.Resolve(ctx, "HEAD")
	if !ok || len(id) < 12 {
		return ""
	}
	return id[:12]
}

// warn says on w, which stays on this machine, when the gather was not what a
// measurement needs: a shallow clone, or no PRs. The reason PRs were
// unavailable is read from the raw gather, so it never reaches the record.
func warn(w io.Writer, kept string, rec Record) {
	g := rec.Gather
	if g == nil {
		return
	}
	if g.History.Shallow {
		fmt.Fprintln(w, "initstats: warning: the clone is shallow, so history is only what was fetched; run git fetch --unshallow and measure again")
	}
	if !g.PRs.Available {
		var raw struct {
			PRs struct {
				Reason string `json:"reason"`
			} `json:"prs"`
		}
		var reason string
		if _, err := readJSON(filepath.Join(kept, "gather.json"), &raw); err != nil {
			reason = "the reason could not be read: " + err.Error()
		} else {
			reason = raw.PRs.Reason
		}
		fmt.Fprintf(w, "initstats: warning: no PRs were gathered (%s), so propose and recovery read docs and changes only\n", reason)
	}
}
