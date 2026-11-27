package initrun

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/recovery"
	"github.com/dphaener/physa/internal/walk"
)

// usageAgent is an agent that reports its plan's usage: one active weekly
// limit at percent.
type usageAgent struct {
	*fakeAgent
	percent float64
	reads   int
}

func (u *usageAgent) Usage(context.Context) (adapter.Usage, error) {
	u.reads++
	return adapter.Usage{Limits: []adapter.Limit{{Kind: "weekly_all", Percent: u.percent, Active: true, ResetsAt: time.Date(2026, 11, 30, 14, 0, 0, 0, time.UTC)}}}, nil
}

// recoveredRows is what init's record holds of each capability's recovery.
func recoveredRows(t *testing.T, env Env) map[string]string {
	t.Helper()
	st, err := env.Store(".")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := initrecord.Of(st).Recovered()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, r := range rows {
		out[r.Capability] = r.Status
	}
	return out
}

// TestCappedRecoverStopsThenContinues checks acceptance 1 for money: a
// --recover capped at $0.50 starts the first capability's session, whose
// budget fits, and refuses the next, whose budget would pass the cap; it
// says the cap was reached, what init spent in dollars and that physa init
// --recover continues, and exits 1. A second --recover with a larger cap
// recovers only the capability left.
func TestCappedRecoverStopsThenContinues(t *testing.T) {
	a := newRoutedAgent()
	a.priced, a.recovers = true, 300_000 // each recovering session spends about $1.20
	a.replies["delivery"] = recoverReply // checks clean, so no retry starts a second session
	env := gatheredEnv(t, a)
	if code, _, errOut := runInit(env, "--propose"); code != 0 { // priced, so a budget has a price
		t.Fatalf("propose: %d %s", code, errOut)
	}
	code, out, errOut := runInit(env, "--recover", "--cap", "$0.50")
	t.Logf("first run, sessions %v:\n%s", a.started, errOut)
	if code != 1 {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	first := slices.DeleteFunc(slices.Clone(a.started), func(s string) bool { return s == "propose" })
	if len(first) != 1 {
		t.Fatalf("recovering started %v; want one session before the cap", first)
	}
	rows := recoveredRows(t, env)
	rest := "search"
	if first[0] == "search" {
		rest = "delivery"
	}
	if rows[first[0]] != recovery.StatusRecovered || rows[rest] != recovery.StatusNotYet {
		t.Errorf("stored %v", rows)
	}
	for _, want := range []string{
		"physa init: the cap is $0.50: init stops before a session that would pass it; init has spent $0.04 (10k tokens) so far.\n",
		"physa init: " + rest + ": not yet recovered\n",
		"physa init: the cap of $0.50 was reached: nothing more was started and sessions already running finished; init has spent $",
		"To continue, run physa init --recover with a larger --cap, or none.\n",
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr lacks %q:\n%s", want, errOut)
		}
	}
	if !regexp.MustCompile(`init has spent \$1\.2[0-9] \([0-9.]+k tokens\)\. To continue`).MatchString(errOut) {
		t.Errorf("the spend is not in dollars:\n%s", errOut)
	}
	if strings.Contains(errOut, "failed") {
		t.Errorf("a line says something failed:\n%s", errOut)
	}

	n := len(a.started)
	code, out, errOut = runInit(env, "--recover", "--cap", "$5")
	t.Logf("second run, sessions %v:\n%s", a.started[n:], errOut)
	if code != 0 || !slices.Equal(a.started[n:], []string{rest}) {
		t.Fatalf("code %d, sessions %v; want only %s\nstdout %s\nstderr %s", code, a.started[n:], rest, out, errOut)
	}
	if rows := recoveredRows(t, env); rows["delivery"] != recovery.StatusRecovered || rows["search"] != recovery.StatusRecovered {
		t.Errorf("stored %v", rows)
	}
	if !regexp.MustCompile(`physa init: init has spent \$[0-9.]+ \([0-9.]+k tokens\) of its cap of \$5\.00\.\n`).MatchString(errOut) {
		t.Errorf("no end line against the cap:\n%s", errOut)
	}
}

// TestCappedWalkStopsCleanly checks that a --walk capped at 1k tokens, less
// than any session's budget, starts no session, stores no walk, keeps the
// stored areas, says no phase failed and gives physa init --walk to continue.
func TestCappedWalkStopsCleanly(t *testing.T) {
	env, a := walkEnv(t)
	code, out, errOut := runInit(env, "--walk", "--cap", "1k")
	if code != 1 || a.starts != 0 || out != "" {
		t.Fatalf("code %d, %d sessions\nstdout %s\nstderr %s", code, a.starts, out, errOut)
	}
	for _, want := range []string{
		"physa init: the cap is 1k tokens: init stops before a session that would pass it;",
		"physa init: the cap of 1k tokens was reached: nothing more was started and sessions already running finished; init has spent ",
		"To continue, run physa init --walk with a larger --cap, or none.\n",
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr lacks %q:\n%s", want, errOut)
		}
	}
	if strings.Contains(errOut, "did not end whole") || strings.Contains(errOut, "not started:") {
		t.Errorf("a line says the walk failed:\n%s", errOut)
	}
	st, err := env.Store(".")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, ok, err := walk.Load(initrecord.Of(st)); ok || err != nil {
		t.Errorf("a walk was stored: %v %v", ok, err)
	}
	if _, ok, err := initrecord.Of(st).Areas(); !ok || err != nil {
		t.Errorf("the areas were not kept: %v %v", ok, err)
	}
}

// TestShareCapAtThePlanUsageStartsNoSession checks that a share cap at or
// under the plan's usage starts no session and says when to continue, that
// one above it reads usage before each session and runs, and that a run with
// no --cap says nothing of a cap.
func TestShareCapAtThePlanUsageStartsNoSession(t *testing.T) {
	u := &usageAgent{fakeAgent: recoverAgent(100), percent: 60}
	env := proposedEnv(t, u)
	code, _, errOut := runInit(env, "--recover", "--cap", "60%")
	if code != 1 || u.starts != 0 {
		t.Fatalf("code %d, %d sessions\nstderr %s", code, u.starts, errOut)
	}
	for _, want := range []string{
		"physa init: the cap of 60% was reached: nothing more was started and sessions already running finished; the plan's usage is 60% (weekly_all resets ",
		"To continue, run physa init --recover once the plan's usage is under 60% (weekly_all resets ",
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr lacks %q:\n%s", want, errOut)
		}
	}
	u.reads = 0
	code, _, errOut = runInit(env, "--recover", "--cap", "80%")
	if code != 0 || u.starts != 2 || u.reads < 3 { // once to check, then before each session
		t.Fatalf("code %d, %d sessions, %d reads\nstderr %s", code, u.starts, u.reads, errOut)
	}
	if !strings.Contains(errOut, "physa init: the cap is 80% of the plan's usage: init starts no session while it is at or over that; the plan's usage is 60% (weekly_all resets ") {
		t.Errorf("stderr:\n%s", errOut)
	}
	code, _, errOut = runInit(env, "--recover", "--only", "delivery")
	if code != 0 || strings.Contains(errOut, "the cap") {
		t.Errorf("an uncapped run: code %d\nstderr %s", code, errOut)
	}
}

// TestUsageCapWithoutUsageRefusesToStart checks acceptance 4: a share cap
// with an agent that cannot report its plan's usage refuses the run before
// anything starts, says why and exits 2, with nothing gathered.
func TestUsageCapWithoutUsageRefusesToStart(t *testing.T) {
	newRepo(t)
	f := oneAgent()
	env := testEnv(t, onePR(), f)
	code, out, errOut := runInit(env, "--cap", "80%")
	want := "physa init: --cap 80% needs the agent's plan usage, and fake cannot report its plan's usage: it has no way to read it\n"
	if code != 2 || f.starts != 0 || out != "" || errOut != want {
		t.Fatalf("code %d, %d sessions\nstdout %s\nstderr %q\nwant %q", code, f.starts, out, errOut, want)
	}
	st, err := env.Store(".")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, ok, err := st.Gathered(); ok || err != nil {
		t.Errorf("gathered: %v %v", ok, err)
	}
}

// TestBareCappedRunNamesTheCapFirst checks that a bare capped run's first
// line names the cap beside what a token is worth, that it shows its spend
// in dollars, and that it ends with what init spent against the cap.
func TestBareCappedRunNamesTheCapFirst(t *testing.T) {
	remoteRepo(t)
	a := newRoutedAgent()
	a.priced = true
	env := fullEnv(t, onePR(), a)
	code, out, errOut := runInit(env, "--cap", "$5")
	if code != 0 || !strings.Contains(out, "## delivery: recovered") {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	if want := "physa init: the cap is $5.00: init stops before a session that would pass it; init has spent $0.00 (0 tokens) so far. Spend is shown in tokens;"; !strings.HasPrefix(errOut, want) {
		t.Errorf("stderr starts %q, want %q", errOut, want)
	}
	if !regexp.MustCompile(`physa init: this run spent \$0\.[0-9]{2} \([0-9.]+k tokens\)\.\n`).MatchString(errOut) ||
		!regexp.MustCompile(`physa init: init has spent \$0\.[0-9]{2} \([0-9.]+k tokens\) of its cap of \$5\.00\.\n`).MatchString(errOut) {
		t.Errorf("stderr:\n%s", errOut)
	}
}

// TestBareCappedRunStoppedWhileRecoveringContinuesWithRecover checks that a
// bare run the cap stops once proposing is done gives physa init --recover,
// with the words of the line --recover takes, to continue.
func TestBareCappedRunStoppedWhileRecoveringContinuesWithRecover(t *testing.T) {
	remoteRepo(t)
	a := newRoutedAgent()
	a.priced, a.propose = true, 200_000 // proposing spends about $0.80
	env := fullEnv(t, onePR(), a)
	code, out, errOut := runInit(env, "--write", "--since", "2025-01-01", "--cap=$0.80")
	if code != 1 || slices.Contains(a.started, "delivery") || !slices.Contains(a.started, "propose") {
		t.Fatalf("code %d, sessions %v\nstdout %s\nstderr %s", code, a.started, out, errOut)
	}
	if !strings.Contains(errOut, "To continue, run physa init --recover --write with a larger --cap, or none.\n") {
		t.Errorf("stderr:\n%s", errOut)
	}
}

// TestBadCapIsBadUsage checks that a --cap init cannot read, or one with
// --place, is bad usage that says why, before anything starts.
func TestBadCapIsBadUsage(t *testing.T) {
	newRepo(t)
	for args, want := range map[string]string{
		"--gather --cap 120%":           `physa init: --cap "120%" must be above 0% and at most 100%: give an amount of money ($5 or 5usd), a share of the plan's usage (80%) or tokens (2m)`,
		"--place --cap 5usd":            "physa init: --place starts no session, so it takes no --cap",
		"--recover --cap lots":          `physa init: --cap "lots" is not a cap: give`,
		"--walk --cap $0":               `physa init: --cap "$0" must be more than zero`,
		"--propose --cap=-5":            `physa init: --cap "-5" is not a cap`,
		"--areas --cap 0%":              `physa init: --cap "0%" must be above 0% and at most 100%`,
		"--recover --cap 12.5":          `physa init: --cap "12.5" is not a cap`,
		"--write --cap 5usd x":          "usage: physa init",
		"--gather --cap 5usd --propose": "usage: physa init",
	} {
		f := oneAgent()
		code, _, errOut := runInit(testEnv(t, onePR(), f), strings.Fields(args)...)
		if code != command.BadUsage || f.starts != 0 || !strings.Contains(errOut, want) {
			t.Errorf("%s: code %d, %d sessions, stderr %q", args, code, f.starts, errOut)
		}
	}
}

// TestCappedGatherStoresNothing checks that a --gather whose discovery
// session the cap refuses stores no gather, so the earlier one stays whole,
// and gives physa init --gather to continue.
func TestCappedGatherStoresNothing(t *testing.T) {
	env := gatheredEnv(t, oneAgent())
	before := storedGather(t, env).Run.At
	code, _, errOut := runInit(env, "--gather", "--cap", "1k")
	if code != 1 || !strings.Contains(errOut, "To continue, run physa init --gather with a larger --cap, or none.\n") {
		t.Fatalf("code %d\nstderr %s", code, errOut)
	}
	if after := storedGather(t, env).Run.At; !after.Equal(before) {
		t.Errorf("the gather was replaced: %v, was %v", after, before)
	}
	if strings.Contains(errOut, "Gathered into") || strings.Contains(errOut, "not started:") {
		t.Errorf("stderr:\n%s", errOut)
	}
}
