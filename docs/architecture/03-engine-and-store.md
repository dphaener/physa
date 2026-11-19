# Architecture 03: the engine and its local store

Draft 0.3, 2026-11-19. Uses `.physa/glossary.md` terms only. Builds on 01 and 02. Items marked (Q) are open questions for the owner.

The engine is where Rhiza failed: "no orchestrator" was its goal six times and never happened, the owner wrote watch, stall and cleanup scripts around it, and its orchestration became a 55,000-line package with about 85% of all fixes. This design starts from those failures.

## 1. Constraints

- **Dumb but durable** (the owner, about Rhiza). The engine enforces and records. It never judges.
- **Work keeps moving without a person or an orchestrating agent**, and that claim is counted, not hoped (`count-interventions`).
- **One owner for run state**: one store, one writer, one state machine per kind of thing.
- **Reading never replays history** (`no-replay`).
- **Run records never enter the map** (`no-run-records`). They expire.
- **Agents run on the customer's side** with their own keys (`agents-customer-side`).
- **Portable**: Linux and macOS, no systemd or other service manager required.
- **Nothing uncommitted on main**: every run works on its own branch (the owner, about Rhiza).
- **The core imports nothing above it**; views and harnesses use the same public interface.

## 2. Rhiza's failures, and what answers each

| Rhiza failure | Answer here |
|---|---|
| `watch.sh` scraping panes for usage-limit and permission prompts | Adapters report a session blocked on a prompt as a structured event (§7.3) |
| `runwatch.sh` finding a child idle for 64 hours | Stall detection in the engine (§7.2) |
| `closer.sh` sending `/exit` 127 times | The engine ends sessions itself when the role says done (§7.1) |
| Machine rules pasted into every brief after the out-of-memory crash | Machine limits enforced by the engine on every session it starts (§7.4) |
| tmp sweeps, leftover folders, 15 GB of dead worktree indexes | Each run owns one directory the engine removes at its end (§6) |
| `maxRuns` sized by eye | Slots computed from the machine, adjustable (§7.4) |
| A 51-event journal plus about 10 loose JSON files plus three lock copies | One SQLite store, one writer, state tables (§4) |
| `clean_streak: 0`, measured only at the end | Interventions counted from the first run (§8) |
| A 9-hour stall because a fixer never said done | Done needs a record, and stalls go to Needs you (§7.1, §7.2) |

## 3. Processes

- **One engine per machine** (Q1). It serves every repo registered on that machine, and is the only process that writes engine state.
- **The CLI talks to it** over a local socket. If the engine is not running, the CLI starts it.
- **Foreground mode** for CI and one-off use: `physa run --foreground` runs the engine in the current process for a single piece of work and exits.
- **Views** (the CLI, the Rex plugin, the single-user phone web) read and act only through the engine's API. The engine serves the phone web itself.
- **Watching a live session** is part of the API, not of any one view (§7.5): the CLI, the Rex plugin and the phone web can all follow a session as it works.
- **Harnesses and agents** are child processes the engine starts. They talk back through the same API.

```
 CLI   Rex plugin   phone web            agents (customer's own)
   \       |        /                         ▲   │ done, records,
    \      |       /   one API                 │   │ asks
     ▼     ▼      ▼                            │   ▼
   ┌──────────────────────────────┐  spawns  ┌───────────┐
   │ engine (one per machine)     │─────────▶│ harness    │
   │  one writer · intents · timers│◀─────────│ sessions  │
   └──────────────┬───────────────┘          └───────────┘
                  │ one transaction per change
                  ▼
   .git/physa/physa.db   (one per repo)       ~/.config/physa/  (machine)
```

## 4. The store

### 4.1 Where

- **One SQLite database per repo** at `.git/physa/physa.db` (Q2). It lives in git's common directory, so every worktree of the repo shares it, and it never enters the working tree or a commit.
- **Machine settings** (registered repos, slots, limits, adapters and their keys' locations) live in `~/.config/physa/`.

### 4.2 Shape

- **State tables are the truth** (Q3): the current state of every idea, pitch, bet, scope, change, run and session, read directly.
- **Every state change appends an event in the same transaction.** Events are an audit trail and the input to strength. They are never replayed to answer a read.
- **Retention**: agent output and logs expire after 30 days; events after 90 days; the aggregates strength needs are kept, small.
- **Schema changes** are numbered migrations applied by the engine at start.

### 4.3 What it holds

| Kind | Holds | Never |
|---|---|---|
| Work | ideas, spikes, pitches, bets, scopes, changes, their appetites | in the map |
| Runs | runs, sessions, their outcomes, harness records (findings, evidence, rounds, verdicts, notes) | in the map |
| People | asks (§9), sign-offs, answers, interventions | in the map |
| Measures | slice records, PR check results pulled by `physa sync`, signal and flag results, rollbacks | in the map |
| Derived | strength per idea and capability | stored as truth; recomputed from measures |

## 5. How the engine changes state

### 5.1 One writer, one loop

Every change to state, whatever its source (a CLI command, a session's record, a finished process, a timer, a GitHub update), becomes a command handled one at a time in a single transaction. No two code paths write the same table.

### 5.2 Side effects are durable intents (Q4)

Anything that touches the outside world (start a session, push a branch, open a PR, merge, turn a flag off) is first written as an **intent** in the same transaction as the state change that requires it. Workers carry intents out and record the result. After a crash or a restart, unfinished intents are retried. Every intent is safe to repeat.

This is what "durable" means: the engine can be killed at any moment and picks up exactly where it was, with nothing to fix by hand.

### 5.3 Inputs

| Input | How it arrives |
|---|---|
| People | CLI, Rex plugin, phone web, through the API |
| Sessions | API calls from the harness and agents (records, asks, done), and the process ending |
| Time | timers held in the store: stall checks, retries |
| GitHub | polled through `gh` with backoff in the open-source core (Q5); webhooks in the hosted layer |

## 6. Workspaces

- **Every run works in its own worktree and branch** (Q6) under `.git/physa/work/<run>/`, never in the person's checkout and never on main.
- **Read-only checkouts** for roles that must not see or touch the author's work in progress (verifiers, checks) are made by the engine at a given commit, in the same run directory.
- **Each run's directory holds everything it creates**: worktrees, temporary files, agent scratch. The engine points the agent's temporary directory at it.
- **At the run's end the directory is removed**, after its outcome is recorded. No sweepers.

## 7. Supervision

### 7.1 Sessions end on purpose

- A session ends when its role says done, and **done needs a record**: the engine refuses it until the role's required record exists (the harness declares which, as in the harness spec).
- After done, the engine ends the process itself. Nobody sends `/exit`.
- A process that exits without saying done is recorded as **lost**, and the run goes to its harness's route for that case.

### 7.2 Stalls

- A session that makes no progress (no output, no record, no tool activity reported by its adapter) for a set time is **stalled**. Default: 15 minutes (Q8).
- A stalled session goes to **Needs you** by default. A team can make the engine restart it once first, by policy.
- Stalls are counted per harness role, so a role that stalls often shows up in the numbers.

### 7.3 Prompts an agent is stuck on

- Sessions run headless by default (Q7), so agents don't sit on interactive dialogs.
- When an agent still waits on something (a usage limit, a permission it lacks), its adapter reports it as a structured event: **blocked**, with what it waits for.
- A usage limit becomes a **hold**: the engine pauses starting new sessions until the limit resets, and shows it.
- Anything else blocked goes to Needs you.

### 7.4 Machine limits

- **Slots**: how many sessions run at once on the machine. The default comes from its cores and memory; people can change it (Q9).
- **Per-session limits**: memory, processes and a command time budget, applied by the engine to every session it starts, using what the operating system offers portably (process groups, resource limits). Nothing is pasted into a brief.
- **Holds**: when the machine is under pressure (load, memory, disk), the engine waits to start new sessions, and the hold is recorded and visible.
- **Disk**: each run's directory is the only place its files go, and it is removed at the end.

### 7.5 Watching a live session

- Headless does not mean invisible. The engine keeps each running session's output as a stream (kept with the run, expiring with its logs) and offers it through the API.
- Any view can follow it: `physa watch <run>` in a terminal, a Rex pane through the Rex plugin, or the phone web.
- Watching is read-only. Typing into a session by hand is an intervention (§8) and goes through the API so it is counted.

## 8. Counting "needed a person"

An **intervention** is anything a person does to keep work moving that the engine did not ask for (Q10):

- restarting, resuming or abandoning a run or session by hand;
- committing by hand on a run's branch (a commit without an agent's trailers);
- changing a run's state, limits or brief while it runs;
- answering something the engine did not route to them;
- typing into a running session.

What the engine asks for (sign-offs, bets, asks routed to Needs you) are not interventions: they are the design.

Every run records its interventions. The engine reports runs finished with none, and the current streak, from the first run.

## 9. Asks

An **ask** is what an agent needs from a person during a run: an answer, a choice, a permission. It is not a map **question**: an ask is short-lived and stays with its run.

Agents ask through the engine:

- The ask is stored with the run and routed by the team's autonomy policy: to the person's Needs you, or to an arbiter role the harness provides.
- The answer goes back to the session that asked.
- An answer becomes a **decision** in the map only if it passes the decision test (hard to reverse, surprising without context, a real trade-off), recorded with how it was reached. Everything else stays with the run and expires. (Rhiza turned 322 automatic answers into decisions.)

## 10. Appetites and circuit breakers

- A bet's appetite is tokens (Q12), counted from when the bet is accepted and building starts (Q14). Time plays no part (Q16). A spike has its own very small appetite, set when it starts (Q18).
- Adapters report each session's tokens; the engine adds them to the bet after every session record and checks the appetite in the same transaction.
- At 80% the bet's steward is told through Needs you. At 100% the circuit breaker stops the bet's runs, records why, and the bet goes back to shaping. It never extends itself.

## 11. Lifecycles

Ideas have no named states (`strength-not-mode`). Scopes have none either: they are set during shaping and a bet is delivered through them (Q13). Everything else the engine runs has a small state machine, each owned by one part of the engine:

| Thing | States |
|---|---|
| Bet | active → done, or stopped by its circuit breaker |
| Change | open → landed → exposed (its flag widening) → established, or reverted |
| Run (for a spike, change or scope) | queued → running → waiting on a person → ended (outcome) |
| Session | running → done, exited, lost, or stalled |

## 12. The API

- One local API over a socket, JSON in and out, typed once in Go.
- The CLI, the Rex plugin, the phone web and harness sessions all use it. The hosted layer receives what it needs from it by push.
- Its operations are budgeted like everything else: Rhiza had 214; the target here is a few dozen.

## 13. Size

The engine is the part of Physa most likely to grow by accretion. Its packages get size budgets from day one, and the supervision rules above replace the seven cleanup packages and the nine-file watchdog Rhiza grew.

## 14. Settled with the owner (2026-11-19)

- **Q1** One engine per machine, the only writer, started by the CLI on demand, with a foreground mode for CI.
- **Q2** One store per repo at `.git/physa/physa.db`, shared by all its worktrees; machine settings in `~/.config/physa/`.
- **Q3** State tables are the truth; events are appended in the same transaction, never replayed, and expire.
- **Q4** Side effects are durable intents, retried after a crash and safe to repeat.
- **Q5** GitHub polled through `gh` in the open-source core; webhooks only in the hosted layer.
- **Q6** Every run in its own hidden worktree under `.git/physa/work/`, removed at its end.
- **Q7** Headless sessions by default. Watching a live session is offered by the engine's API to every view, not only the Rex plugin (the owner: "I'm not certain it HAS to be a Rex plugin to be able to watch live sessions").
- **Q8** Stalls after 15 minutes without progress go to Needs you; teams can allow one automatic restart.
- **Q9** Slots defaulted from the machine's cores and memory, and adjustable.
- **Q10** An intervention is what a person does to keep work moving that the engine did not ask for.
- **Q11** **Ask** is its own term, distinct from a map question (added to the glossary).
- **Q12** Appetite is tokens, warned at 80%, stopped at 100%. The owner first chose time, then: "Time doesn't really matter as much. Agents are fast." and "we should call it tokens. Call it what it is."

- **Q13** Scopes are set during shaping and listed in the pitch, with no lifecycle. Showing a scope working on its own can be part of the process; it is not required. Spikes (optional) are runs during shaping whose working or partial code feeds the pitch. Hill is removed from the glossary.
- **Q14** A bet's appetite counts from when it is accepted and building starts.
- **Q15** Init's token limit is an appetite, consistent with Q12.
- **Q18** A spike has its own appetite, very small, set when it starts.
- **Q16** Time plays no part in appetite (the owner: "time doesn't matter at all anymore"). Stalls and unanswered asks are supervision and Needs you, not appetite.

## 15. Open questions

- **Q17** How a bet's progress is described and shown, now that hills are gone. Showing it is for the views design; what the engine must record for it is decided here (owner: "there are underlying modeling decisions that may need to be considered").
