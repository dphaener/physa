# Architecture 03: the engine and its local store

Draft 0.8, 2026-11-22. Uses `.physa/glossary.md` terms only. Builds on 01, 02, 07 and 08; §16 records the step 5 round and §17 what step 5D built. Items marked (Q) are open questions for the owner.

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
- **The CLI talks to it** over a local socket. If the engine is not running, the CLI starts it. It stays up while any work is pending (runs, asks, intents, timers) and exits after 30 minutes with nothing pending (§16).
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
- **The driver is `modernc.org/sqlite`**, pure Go with no cgo, so the core ships as a single binary (decided by Claude as arbiter, 2026-11-20).
- **Machine settings** (registered repos, slots, limits, adapters and their keys' locations) live in `~/.config/physa/`.

### 4.2 Shape

- **State tables are the truth** (Q3): the current state of every idea, pitch, bet, change, run and session, read directly.
- **Every state change appends an event in the same transaction.** Events are an audit trail and the input to strength. They are never replayed to answer a read.
- **Retention**: agent output and logs expire after 30 days; events after 90 days; the aggregates strength needs are kept, small.
- **Schema changes** are numbered migrations applied by the engine at start.

### 4.3 What it holds

| Kind | Holds | Never |
|---|---|---|
| Work | ideas, spikes, pitches, bets, changes, their appetites | in the map |
| Runs | runs, sessions, their outcomes, harness records (findings, evidence, rounds, verdicts, notes) | in the map |
| People | asks (§9), sign-offs, answers, interventions | in the map |
| Measures | slice records, facts read from version control and the forge (07), signal and flag results, rollbacks | in the map |
| Derived | strength per idea and capability | stored as truth; recomputed from measures |

## 5. How the engine changes state

### 5.1 One writer, one loop

Every change to state, whatever its source (a CLI command, a session's record, a finished process, a timer, a forge update), becomes a command handled one at a time in a single transaction. No two code paths write the same table.

The engine is the only writer of work and run state: ideas, pitches, bets, plans, runs, sessions, asks, intents and their events. The map commands keep writing their own records (slice records, learning's and init's state) directly, in short transactions, so `physa map` never needs the engine running (§16).

### 5.2 Side effects are durable intents (Q4)

Anything that touches the outside world (start a session, push a branch, open a PR, merge, turn a flag off) is first written as an **intent** in the same transaction as the state change that requires it. Workers carry intents out and record the result. After a crash or a restart, unfinished intents are retried. Every intent is safe to repeat.

This is what "durable" means: the engine can be killed at any moment and picks up exactly where it was, with nothing to fix by hand.

### 5.3 Inputs

| Input | How it arrives |
|---|---|
| People | CLI, Rex plugin, phone web, through the API |
| Sessions | API calls from the harness and agents (records, asks, done), and the process ending |
| Time | timers held in the store: stall checks, retries |
| The forge | polled through the forge adapter (GitHub first, through `gh`) with backoff in the open-source core (Q5); webhooks in the hosted layer |

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

A note, with no design: because session ids are assigned at launch (04 §10), the engine can resume a session (`--resume`) after a crash, a usage limit or an answered ask, or fork it (`--fork-session`) for a fix round. The owner: "this will also give us resumable sessions." Resuming needs the machine that has the log. The engine designs it in step 5.

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

## 8a. Rework

Rework is what a change spends after its first build to fix findings. Recording it lets a team tune its harness and spend fewer tokens after builds (R1–R5).

- **Every commit and every token is credited to the session that produced it**, and each session to a role the harness declares as build, check or fix (R1). This holds for any harness: the engine starts every session in its own worktree and sees the commit before and after it, so it does not depend on agents' trailers.
- **Harnesses can report findings through the API** in one shape: the check, severity, the change, what is wrong, and later which fix addressed it or that a critic dropped it (R2). Optional: a harness that reports none still gets everything in the first point.
- **Per change, the engine records rework** (R3): review rounds, fix tokens against build tokens, findings per check, findings dropped. A check whose findings are often dropped is noise; one whose findings always lead to a fix earns its tokens.
- **Findings that recur become proposed rules** (R4). When the same kind of finding recurs on a capability, the learning step proposes a rule on the next `physa/learn` PR, so builders get it in their slice instead of paying for it in review.
- **Agents only** (R5). Physa never tracks how people review.

## 9. Asks

An **ask** is what an agent needs from a person during a run: an answer, a choice, a permission. It is not a map **question**: an ask is short-lived and stays with its run.

Agents ask through the engine:

- The ask is stored with the run and routed by the team's autonomy policy: to the person's Needs you, or to an arbiter role the harness provides.
- The answer goes back to the session that asked, recorded with who answered: the person, or the arbiter on their behalf ("Claude as arbiter, on behalf of dphaener"). Learning credits each answer to whoever gave it.
- An answer becomes a **decision** in the map only if it passes the decision test (hard to reverse, surprising without context, a real trade-off), recorded with how it was reached. Everything else stays with the run and expires. (Rhiza turned 322 automatic answers into decisions.)

## 10. Appetites and circuit breakers

- A bet's appetite is tokens (Q12), counted from when the bet is accepted and building starts (Q14). Time plays no part (Q16). A spike has its own very small appetite, set when it starts (Q18).
- Adapters report each session's tokens; the engine adds them to the bet after every session record and checks the appetite in the same transaction.
- At 80% the person who made the bet is told through Needs you. At 100% the circuit breaker stops the bet's runs, records why, and the bet goes back to shaping. It never extends itself.

## 10a. Progress of a bet

What the engine records so views can show a bet's progress without hills (Q17):

- **The plan is recorded and replanning is counted.** When building starts, the conductor names the changes it plans for the bet. The engine stores the plan and every revision with its reason. A plan that keeps changing is the new uphill: it shows what shaping missed.
- **Progress is facts, never a percentage.** Per bet: changes landed, in a run, waiting on a person; tokens spent against the appetite; asks open. Never folded into one number.
- **A bet is done when its planned changes have landed.** When the team's process asks for it to be shown working, that is a sign-off the engine asks for, and the bet is not done until it is given.
- **Spend is recorded per change**, so a view can show where the appetite went and a change eating a large share of it stands out while it happens.

## 11. Lifecycles

Ideas have no named states (`strength-not-mode`). Everything else the engine runs has a small state machine, each owned by one part of the engine:

| Thing | States |
|---|---|
| Bet | active → done, or stopped by its circuit breaker |
| Change | open → landed → exposed (its release widening, with earned release on) → established, or reverted |
| Run (for a spike or a change) | queued → running → waiting on a person → ended (outcome) |
| Session | running → done, exited, lost, or stalled |

## 12. The API

- One local API, typed once in Go, for local clients: a WebSocket on a Unix socket under `~/.config/physa/`, the shape Rex's own API has (§16): methods called by name with JSON in and out, a JSON schema per method, a versioned subprotocol (`physa.v1`), and `physa api list|describe|call` for debugging. Streaming (following a live session, Needs you) uses the same connection.
- The phone web is server-rendered by the engine and calls the same Go operations directly; only its live parts stream to the browser. The hosted layer never connects to the engine: the engine pushes records out to it, opt-in (06 §10).
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

- **Q13** Scope is the breadth of a pitch (the owner: "Scope is the breadth of the feature"), set during shaping. There is no countable piece between a bet and its changes; a pitch too broad for one bet becomes more than one idea. Spikes (optional) are runs during shaping whose working or partial code feeds the pitch. Hill is removed from the glossary.
- **Q14** A bet's appetite counts from when it is accepted and building starts.
- **Q15** Init's token limit is an appetite, consistent with Q12.
- **Q18** A spike has its own appetite, very small, set when it starts.
- **Q17** What the engine records for a bet's progress, as in §10a: the plan and its revisions, facts never a percentage, a bet done when its planned changes land (or at a sign-off when the team asks for one), spend per change. How it is shown is for the views design.
- **R1–R5** Rework recorded as in §8a; Role, Finding and Rework added to the glossary.
- **Q16** Time plays no part in appetite (the owner: "time doesn't matter at all anymore"). Stalls and unanswered asks are supervision and Needs you, not appetite.

## 15. Open questions


## 16. Settled with the owner (2026-11-21): the step 5 round

- **Ideas, pitches and bets live in the local store only, for now.** One clone holds them; mirroring to `refs/notes/physa` waits for team digests (07 §7) and the hosted layer. Chosen because it is the simplest place, and enough while Physa is built from one machine.
- **A person is named by their forge login** (GitHub: `dphaener`), read once and kept in `~/.config/physa/`, with a setting to change it. With no forge, the part of git's email before the `@`. The map, the engine and learning all use this name; an arbiter's answer is "Claude as arbiter, on behalf of dphaener". The name is kept in `~/.config/physa/person` (under `$XDG_CONFIG_HOME` when set) as one line, and editing that file is the setting; a name taken from git's email is not kept, and a settings command can replace the file later.
- **Writers are split by table** (§5.1): the engine writes work and run state; the map commands write their own records.
- **The engine exits when idle**: up while work is pending, gone after 30 minutes with none; the next command that needs it starts it again.
- **Pitches are written in `$EDITOR` or from a file**: `physa idea shape <slug>` opens a markdown template of the seven parts (problem, appetite, solution, scope, signal, rabbit holes, no-gos); `--from <file>` or stdin is how agents write one. Physa checks the parts and stores it.
- **Ideas and bets are named by slugs**: "use the slugs. they're user friendly and filterable so a cli user doesn't need to type it up front, but they can."
- **The CLI is nice**: "i think the cli needs to be NICE. IE use charm libraries." On a terminal, Charm's libraries (Bubble Tea, Lip Gloss, Huh, Bubbles) style output, give a filterable picker when a slug is left out, and drive forms. In a pipe, in CI or for an agent: plain text or `--json`, and a missing slug is an error listing the choices. Map output (slice, `--entry`) is plain everywhere.
- **The local API is a WebSocket, like Rex's** (§12): "the rex api communicates over websockets i believe? check that. we should do the same." (Checked: Rex upgrades `GET /api/control` on its Unix socket to a WebSocket with subprotocol `rex.control.v1`.)
- **Step 5 is built as five PRs**, in order: D the engine process and its API; E state tables, events and durable intents; F ideas, pitches and bets from the CLI; G the plan, appetite and its circuit breaker; H evidence from version control and from CI where a team runs one.
- **Which change a commit belongs to is a `Change: <bet>/<change>` trailer, added by the prepare-commit-msg hook and never by the agent, as `Session:` is.** The hook asks the store, read-only, which planned change the session works on, and adds nothing when there is none. The engine learns a session's change when the session starts: `physa bet work <bet>/<change>` records it now, and the engine itself records it in step 6. Decided by dphaener, choosing an option (2026-11-22). The two options not chosen: the agent writing the trailer, which is a claim, like the dropped `Loaded:`; and waiting for step 6, since harnesses outside the engine would never get progress.

## 17. Built in step 5D

Build D, the engine process and its API, had to settle the points below. They are calls made by Claude as arbiter on behalf of dphaener during planning, for the owner to check. They were not settled with the owner.

- **Files.** The engine keeps three files in the config directory: the socket `engine.sock` (mode 0600), the lock `engine.lock` and the log `engine.log`. The directory is mode 0700. The lock is an flock held for the engine's life, with its pid inside as text for messages only. The engine takes the lock first. Holding it proves no engine is alive, so an old socket file is then removed. The kernel releases the lock when the engine is killed, so a lock is never stale. An OS pid shown on screen is not an internal identifier.
- **Short socket paths.** A socket path of 104 bytes or more is refused on every platform, with an error naming the path and the limit. It is the macOS limit, kept on Linux so both behave alike. There is no fallback directory.
- **The API.** `GET /api` on the socket upgrades to a WebSocket with the subprotocol `physa.v1`, which both sides require. A request is `{"id", "method", "params"}`. A reply carries the same `id` and either a `result` or an `error` with a code (`bad_request`, `unknown_method` or `internal`) and a message. The engine may send an `event` with a name and data and no id. The API revision is an integer, now 1. Its first methods are `api.list`, `api.describe`, `engine.status` and `engine.stop`.
- **Schemas.** Each method's schema is derived from its Go input and output types, from a small subset of JSON schema: json names, a `desc` tag, required unless `omitempty`, no unknown properties. Input is checked against the schema before it is decoded.
- **Starting.** A start runs the same binary as `physa engine run`, detached, with its output appended to `engine.log`, and waits up to 5 seconds for the engine to answer. `engine status` and every `api` command start the engine when none runs. `engine stop` never does: with none running it says `No engine running` and exits 0. `engine run` exits 3 when an engine already runs and 2 on any other failure.
- **Idle.** `--idle <duration>`, default 30 minutes, on `engine run` and `engine start`. The engine exits when no client has been connected and no call has come for that long.
- **`engine run` and `run --foreground`.** `physa engine run` serves the engine in the foreground. `physa run --foreground` (§7.5) stays planned and is a different thing: it runs a bet.
- **Upgrades.** An older engine keeps serving after an upgrade until it exits idle or is stopped. There is no version check yet.
- **Platforms.** The engine runs on Linux and macOS only. On other platforms the rest of the binary still builds, and the engine commands refuse.
- **Libraries.** coder/websocket is the one WebSocket library. Lip Gloss was pinned at v2.0.6 while the go line stayed at Go 1.25. Build F raised the go line to 1.26.0, which Bubble Tea and Bubbles v2 need, and unpinned Lip Gloss (§19).
- **Budgets.** `tools/codecheck` holds a budget per package: 400 non-blank lines each for `internal/engine`, `internal/api`, `internal/apiclient` (300), `internal/apischema`, `internal/enginestart` and `internal/enginecli`, and 200 for `internal/output` and `internal/configdir`.

## 18. Built in step 5E

Build E, the engine's state, had to settle the points below. They are calls made by Claude as arbiter on behalf of dphaener during planning, for the owner to check. They were not settled with the owner.

- **Registered repos.** The engine reaches a repo's store through the repos file, `repos` in Physa's settings directory: one absolute repo root per line, the grandparent of a `.git/physa` store directory. Other shapes are refused for now. A call that names a repo gives its absolute path; the engine finds the store directory through version control, registers the repo if it is new and opens its store. A repo, its subdirectories and its worktrees share one open store. On start the engine opens every registered repo; a path that is no longer a repo is skipped with one log line and kept in the file. The engine listens before it opens them, so the CLI's 5 second start wait is never spent on stores. The file only grows in this build: nothing removes a repo.
- **Tables.** Migration 0008 adds `ideas` (slug, text, person), `pitches` (idea, appetite in tokens, problem, solution, scope, signal, rabbit holes, no-gos), `bets` (slug, idea, pitch, person, appetite and spent tokens, state `active`, `done` or `stopped`, stop reason, start and end), `plans` (one per bet), `plan_revisions` (numbered, with the changes as JSON, a reason and a person), `events`, `intents` and `timers`. A pitch's idea is not unique: a stopped bet goes back to shaping, and reshaping adds a pitch. Builds F and G may add columns.
- **One loop per store.** Each open store has one goroutine that applies commands one at a time. A command runs in one write transaction that takes the lock at BEGIN (`_txlock=immediate` in the store's connection string), so a second process cannot slip a write in between. `Submit` returns when the commit is done. A command that is taken is applied; one that is not taken is not. A first open that meets a busy store retries for up to 5 seconds.
- **Every write appends its event.** A command that changes a state table and appends no event is refused at commit. Events are never replayed to answer a read (`no-replay`); the state tables hold the current answer. `engine.events {repo, limit}` returns the newest first, 50 by default and 500 at most, and registers the repo. A repo-scoped call whose path is not an absolute repo path replies `bad_request`; a store that will not open replies `internal`. `engine.status` lists the open repos.
- **Intents.** A kind is registered by name with a `Do` and a `Repeat` text: the plain-words statement of what happens when the intent runs twice after a crash. One worker per repo carries out pending intents in id order, outside any transaction, and records each result through the loop with an event. A failure retries with backoff from 1 second, doubling up to 1 minute. After 10 attempts the intent is `failed`, with an `intent.failed` event, and no longer counts as pending. On start every pending intent is tried at once, whatever its backoff. An intent of a kind this binary does not register stays pending and uncounted, so a newer engine carries it out later.
- **`log.append`.** The first real kind appends `<intent id> <text>` to a file under `<store dir>/logs/`. Repeating it is safe: the worker skips an id already in the file.
- **Timers and retention.** A timer is a row in `timers`, fired by the loop as a command, with the fire and the reschedule in one transaction. Overdue timers fire after a start. The retention timer (`events.expire`, key `retention`) is ensured when a store opens and fires daily. It deletes events older than 90 days and appends one `events.expired` event with the count when it deleted any. A timer of a kind this binary does not register is left alone, out of the due scan, so a newer timer never holds an older engine up. A failed fire is pushed out with a backoff.
- **Idle.** The engine counts as busy while a client is connected, while a repo is opening, while any repo has a pending intent, or while a timer is due before now plus the idle window. The counts are kept in memory and never queried under the engine's lock. Under the default 30 minute window the daily retention timer keeps nothing up.
- **One writer.** `tools/codecheck` has the rule `engine-tables-writer`: a string literal that writes an engine table, in non-test Go outside `internal/state` and its subpackages, is a violation. It is a check on string literals, not a proof. Subpackages are allowed so build F can add its commands without reversing the rule.
- **The kill test.** `TestKillAnyMoment` in `internal/engine` re-runs the test binary as the engine, writes through it, kills it with SIGKILL at a random moment, restarts it and checks the store: no event lost or doubled, no intent stuck, no line written twice. `PHYSA_KILL_RUNS` sets the rounds (default 20) and `PHYSA_KILL_SEED` the seed, which is logged and named on failure. A kill counts as mid-flight only when the store shows it: a write cut off or an intent the restarted engine found pending. `TestKillCheckCatches` proves each check fails on a store broken on purpose.
- **Budgets.** `tools/codecheck` holds `internal/state` to 700 non-blank lines and `internal/repos` to 350.
- **Upgrades.** An older physa refuses a store this one has migrated to version 8. The files under `<store dir>/logs/` get no retention yet, and `log.append` reads its whole file for each intent. That is fine for now and is to revisit before run logs use it.

## 19. Built in step 5F

Build F, ideas, pitches and bets from the CLI, had to settle the points below. They are calls made by Claude as arbiter on behalf of dphaener during planning, for the owner to check. They were not settled with the owner.

- **Slugs.** A slug made from an idea's text is its first five words in lower-case ASCII letters and digits, joined by `-`, at most 40 characters; text with no such word gives `idea`. A made slug that is taken, or is the name of a subcommand (`add`, `list`, `show`, `shape`, `stop`, `plan`), takes `-2`, `-3` and so on, cut short so the whole stays within 40 characters. A slug chosen with `--slug` must match `^[a-z0-9]+(-[a-z0-9]+)*$`, be at most 40 characters, not be a subcommand's name and not be taken; otherwise it is refused, naming why. A bet's slug is its idea's slug, and a later bet on the same idea takes `-2`, `-3`.
- **The pitch.** A pitch is a markdown file: `# <idea text>`, then the seven `## ` parts in order (Problem, Appetite, Solution, Scope, Signal, Rabbit holes, No-gos), matched case-insensitively. HTML comments are guidance and are stripped; a line starting `## ` always starts a part. Every part must be non-empty and the appetite more than 0. An unknown or repeated heading is an error. Every problem is reported together, on one line, as `the pitch has problems: ...`. Shaping again stores a new pitch and keeps the earlier one, with a `pitch.shaped` event; the idea's pitch is its latest, and the template is filled with it.
- **Appetite syntax.** Tokens are a whole number with an optional `k` (thousand) or `m` (million) suffix: `200k`, `1.5m` or `50000`. Commas and underscores are separators and are dropped. A decimal needs a `k` or `m` suffix and at most three places after `k` or six after `m`. Written back, a multiple of 100,000 at or above a million reads as `m`, another multiple of a thousand as `k`, and anything else as a plain count.
- **Bets.** `physa bet <slug>` makes a bet. Its appetite is `--appetite` when given, else the latest pitch's; with neither, the command asks on a terminal and is bad usage off one. A bet on an idea with no pitch needs an appetite. A bet links the latest pitch when there is one. An idea with an active bet refuses another, and the engine says so: `the idea "x" already has the active bet "x"; stop it first`. Choices on a terminal: `bet` picks among ideas whose latest bet is not active, `bet show` among all bets and `bet stop` among active bets.
- **States.** A bet is `active` until it is stopped, then `stopped`, with its end time. `physa bet stop <slug>` takes `--reason`; with none the reason is `stopped by <person>`. Only an active bet stops, and stopping one twice is refused. `done` is in the table, but nothing sets it until build G, when the plan says a bet's changes have landed.
- **Stopping on a terminal.** `bet stop` asks first with a Huh confirm, unless `--yes` is given or Physa is not on a terminal. A "no", or an abort, leaves the bet active and prints `physa bet stop: not stopped`, exit 2.
- **Only people bet.** `physa bet` and `physa bet stop` refuse inside an agent session, before anything else is checked, with exit 2 and the message `physa bet: A person must make this bet: physa is running inside a Claude Code session.` (`physa bet stop` says `stop` in place of `make`). The marker is Claude Code's: `CLAUDECODE` or `CLAUDE_CODE_SESSION_ID` set and not empty, asked through `agentenv.Detect`, which lists the one agent today. `physa api call bet.start` and `physa api call bet.stop` refuse the same way before any engine is started, because the engine cannot see the client's environment. Ideas, `bet list` and `bet show`, and every API read, work for agents.
- **The person.** The CLI names the person with `person.ForRepo`: their setting in `~/.config/physa/person`, else their forge login, else the part of their git email before `@`. When none can be had, the reason goes to stderr and the command exits 2 before the engine is called. A person is resolved only where a change needs one (`idea add`, `idea shape`, `bet` and `bet stop`); `list` and `show` never ask the forge. Each changing call sends `person`, and the engine refuses an empty one.
- **The eight methods.** `idea.add`, `idea.list`, `idea.show`, `idea.shape`, `bet.start`, `bet.list`, `bet.show` and `bet.stop`, with JSON schemas derived from their types (§17). Each change is one command in one transaction with its event: `idea.added`, `pitch.shaped`, `bet.started` and `bet.stopped`. Events carry slugs, since a subject's internal id is not a name people use. A read runs as a read-only command through the same loop, so it sees one consistent view. A refusal from the store, a pitch that does not parse, a bad appetite and a repo the engine does not serve all reply `bad_request`.
- **Ideas.** `idea add "<text>"` takes its slug from the text unless `--slug` chooses it, and prints it. `idea list` lists the ideas newest first, each with who added it and its facts: `not shaped` or `shaped` (or `shaped N times`), then `bet <state>`, or `bet <slug> <state>` when the bet's slug differs from the idea's. `idea show` gives the text, who added it, the latest pitch and the bets.
- **A missing slug off a terminal.** It is bad usage, exit 2: the command names what is missing, lists the choices, newest first, and prints its help. With no ideas it says there are none yet.
- **The editor.** `idea shape <slug>` on a terminal, without `--from`, opens a temporary `physa-pitch-*.md` file with the template in `$VISUAL`, else `$EDITOR`, else `vi`. The variable is split on spaces and run without a shell, so `code --wait` works. A save that does not parse shows the problems and asks `Edit the pitch again?`. An editor that fails, a fault from the engine, or a "no" keeps the file, prints its path and exits 2; so does an abort of the question. Off a terminal, `--from <file>` (or `--from -` for stdin) is the only way in, and without it `idea shape` is bad usage before the slug is chosen or the repo is found.
- **One test for the terminal.** `output.Interactive(in, out, agent)` decides how every idea and bet command behaves: it is true only when stdin and stdout are both terminals, `CI` is empty and no agent session is detected. Styled output, pickers, confirms and inputs follow it. Otherwise a command prints plain text or `--json`. The rule is tested without a terminal, and `/dev/null` is not a terminal.
- **Pickers, confirms and inputs.** A missing slug on a terminal is chosen from a filterable list (a Bubbles list in a Bubble Tea model, filtered on slug, text and detail). Esc clears a filter first and aborts only when none is applied; Ctrl+C always aborts. A confirm and an input are Huh forms. A missing appetite is asked with an input that the appetite syntax checks. The picker's tests drive the model with teatest, never a real terminal.
- **Styles.** Titles are bold, slugs take the design's accent colour, and keys and facts are faint. There are no borders and no marks. The output package keeps its rule: styled only on a terminal, and never with `NO_COLOR` set.
- **Packages.** `internal/pitch` (the template, the parsing, the checks and the appetite syntax; no I/O), `internal/agentenv` (the agent markers), `internal/state/work` (the commands, reads, slugs and events, behind the engine's one writer), `internal/workapi` (the eight methods and their types), `internal/picker` (the list, confirm and input), `internal/ideacli` (the idea group and the helpers the bet group shares), `internal/betcli` (the bet group) and `internal/learnbet` (the bet signals, 08 §3). `cmd/physa` only wires them in. Non-blank lines at the end of this build: `internal/state/work` 573, `internal/workapi` 271, `internal/picker` 144, `internal/pitch` 185, `internal/agentenv` 26, `internal/ideacli` 745, `internal/betcli` 277, `internal/learnbet` 183. Packages already near their budgets got only wiring edits: `internal/state` 698 of 700, `internal/enginecli` 398 of 400, `internal/learn` 800 of 800, `cmd/physa` 789 of 800.
- **Go and the libraries.** The go line is `go 1.26.0`, which Bubble Tea v2 and current Charm releases need (§17). Lip Gloss is unpinned at v2.1.1. Bubble Tea v2.1.2, Bubbles v2.2.2 and Huh v2.0.3 are added, and teatest is at its pseudo-version, since no tagged v2 release exists.

## 20. Built in step 5G

Build G, a bet's plan, its progress and its appetite with the circuit breaker, had to settle the points below. They are calls made by Claude as arbiter on behalf of dphaener during planning, for the owner to check. They were not settled with the owner.

- **The plan.** `physa bet plan set [<bet>] [--reason <text>] [--from <file>|-]` stores the plan's JSON, read from `--from` or from stdin, as the bet's next revision (`plan.set`). A plan with problems is refused as a whole, and every problem is listed on one line as `the plan has problems: ...`. A plan with unknown fields, or with invalid JSON, is refused as not valid before anything else is checked. Each change names one capability, a slug in the map: a string with a comma, space or slash, or an array of two, names two capabilities and is refused. A change's `after` names only other changes in the plan and makes no cycle. Each acceptance list holds an entry starting `failure case:`, or `no failure case:` followed by the reason; both need text after the colon.
- **Revisions and replans.** The first revision's reason is optional. Every later one needs `--reason`, which says what shaping missed, or it is refused. Only an active bet takes a plan. Every revision is kept with its reason, its person and its time, and `physa bet plan show <bet> [--history]` lists them, oldest first. Replans are the revisions after the first, counted in `plan show` and `bet show`.
- **The tables.** Migration 0009 adds four: `changes` (per bet and change name: its capability, whether it is planned, `open` or `landed` with the landed commit and time, and its spent tokens; a change that a later revision drops keeps its row with `planned` 0, so its spend stays), `session_changes` (the change each session works on, one row per session), `spends` (tokens per bet, change and session, as the session's running total) and `needs_you` (the person it is for, its kind `appetite` or `breaker`, its bet, its text and when it is done). Only the engine writes them, and codecheck's `engine-tables-writer` rule names all four. `plan_revisions` keeps each revision's changes as JSON, the plan's history.
- **Working on a change.** `physa bet work <bet>/<change>` records, through the engine (`bet.work`), the change the current session works on, replacing the one it worked on before. The bet must be active and the change planned in its current revision. With no `CLAUDE_CODE_SESSION_ID` it exits 2. Agent sessions may call it. `physa bet work --current` prints `<bet>/<change>` for the session, read from the store read-only, or prints nothing. It always exits 0, starts no engine and migrates nothing; the commit hook is its caller. `work` is reserved as a slug, with the names of the other subcommands.
- **Progress and landing.** `physa bet show` (and `bet.show`) reads the default branch's commits since the bet was made, through the version-control interface, and matches their `Change:` trailers. Each planned change found there is recorded once as landed (`change.landed`). When every planned change has landed, an active bet is set `done` (`bet.done`). Both writes repeat safely. A version-control error is logged, and the progress shown is what the store holds. Progress is facts: changes landed, changes in work (`change.in_work`), asks open (none yet), tokens spent of the appetite, replans, and spend per change, largest first. There is no percentage.
- **Spend and the circuit breaker.** `bet.spend` takes a session's running total on a bet and, optionally, on one of its changes (an empty change is the bet itself). Only the increase over what the store holds for that session is added, so a repeated or lower total changes nothing. The increase, the appetite check and any Needs you item are one transaction. At 80% of the appetite the bet's person gets one `appetite` item, with `appetite.warned`. At the appetite the circuit breaker stops the bet with the reason `circuit breaker: spent N of M tokens`, gives its person a `breaker` item, and writes `bet.stopped`; the bet goes back to shaping. A bet has no steward, so the items go to the bet's person; dphaener settled that wording on 2026-11-22. Tokens read "N of M", never a percentage. Spend on a bet that is not active is recorded and raises nothing. Nothing raises an appetite: only a person making a new bet sets one.
- **Methods and events.** The methods are `plan.set`, `plan.show`, `bet.work` and `bet.spend`; `bet.show` also lands changes and reports progress. The events are `plan.revised`, `change.in_work`, `change.landed`, `spend.reported`, `appetite.warned`, `bet.stopped` and `bet.done`. The API revision stays 1.
- **The hook.** `prepare-commit-msg` adds `Change: <bet>/<change>` after `Session:`, and only when the store names a change for the session. It adds nothing when `physa` is not on PATH or the store names none, never starts the engine, and never fails a commit. `physa map hooks` replaces the earlier Physa hook (the one that stamped `Session:` alone, found by its SHA-256) in place; any other hook file is still refused. A repo that ran `physa map hooks` before this build is upgraded, not refused.
- **Packages.** `internal/plan` (the plan's checks, with no I/O), `internal/progress` (which planned changes have landed, read through version control), `internal/state/plan` (the commands and reads behind the engine's one writer), `store.OpenReadOnly` (the store for `--current`), the four methods in `internal/workapi`, the `bet plan` and `bet work` commands in `internal/betcli`, and two flags in `internal/ideacli`. Non-blank lines at the end of this build: `internal/plan` 209, `internal/progress` 69, `internal/state/plan` 620, `internal/state/work` 573 (its reserved words only), `internal/state` 698 of 700 (unchanged), `internal/workapi` 520, `internal/betcli` 608, `internal/ideacli` 748 of 800 (the two flags), `internal/sessionhook` 155 and `internal/store` 715.

## 21. Built in step 5H

Build H, evidence from version control and CI, had to settle the points below. They are calls made by Claude as arbiter on behalf of dphaener during planning, for the owner to check. They were not settled with the owner.

- **Tables.** Migration 0010 adds `evidence`, one row per fact: kind, commit, PR, capability, bet, change (the plan change's name), idea, time, source (`vcs` or `forge`), lead time in seconds (lead time only), the reverting commit (`reverted` only) and the time it was recorded. It is unique on (kind, commit, PR), and a fact is written with INSERT OR IGNORE, so each is recorded once. It has no weight or strength column. `evidence_polls` holds one row: the version-control watermark (the default branch's head and its time), the forge's watermark (the newest merged-at seen), and the forge's failure count and next try. The store is at version 10, and an older physa refuses it.
- **Kinds.** `landed` is a change on the default branch. `reverted` is the reverted change, with `by_commit` the revert; the revert also has its own `landed`, and a revert whose target the repo lacks gives no `reverted`. `lead_time` runs from the PR's opened time to its merge. `ci_passed`, `ci_failed` and `ci_passed_after_failing` are the CI results. Capability, bet and change come from the `Capability:` and `Change:` trailers, and idea from the bet. A forge fact's commit is its PR's merge commit, empty when the forge names none; the PR number keeps it unique.
- **The poll.** The timer `evidence.poll` (key `poll`) writes one `evidence.poll` intent while none is pending. The intent's Do reads version control and the forge with no transaction open. Its `Record` hook stores the facts and moves both watermarks in the transaction that marks the intent done, so a kill before that loses nothing and doubles nothing (the kill test, §18). When new facts are stored, `evidence.recorded` is appended in the same transaction with the counts. The watermarks and the forge's backoff move only from the values the poll started at, so a poll recorded twice moves nothing twice.
- **Version-control reads.** A first look reads the default branch's changes from the last year. After that it reads `<watermark>..<default>`. When the watermark is no longer an ancestor of the default branch, the look restarts a day before the watermark's time.
- **Forge reads.** The forge is asked for merged PRs since its watermark, at most 500, and only when its backoff has run out. A first look reaches back a year. A look that reports more than 500 PRs is not followed: the watermark moves to the newest one seen, so older PRs in that window are never read. A busy first look may miss PRs; for the owner to check.
- **Fetching.** The engine never fetches. Facts appear once the clone has the changes.
- **CI.** Each merged PR's first 100 commits are read with their check rollup. The last commit with a SUCCESS, FAILURE or ERROR result decides, so a trailing commit with no result does not hide an earlier one, and a pass after an earlier failure or error is `ci_passed_after_failing`. A rerun on one commit is invisible, since only the rollup's state is read. A PR with more than 100 commits is read at 100.
- **Cadence and idle.** The poll timer is due at once when the store opens, then every 5 minutes while any bet is active, else daily. `bet.start` makes it due now. A timer that fires while a poll is pending writes no second one, but still sets its next time. Only the 5-minute timer falls inside the 30-minute idle window, so an active bet keeps the engine up and the daily timers do not. The idle rule is unchanged.
- **Forge backoff and deadlines.** A forge failure never fails the poll; the version-control facts are still stored. The forge's next try is 5 minutes out, doubling per failure to at most 6 hours, and a success clears it. Every gh call has a 30-second deadline in the default runner. A timed-out call is a plain error, not a forge class.
- **Retention.** The timer `evidence.expire` (key `retention`) runs daily. It deletes facts observed more than a year ago, and done poll intents finished more than 7 days ago. Failed poll intents stay, since they say what went wrong. When it deletes anything it appends `evidence.expired` with both counts.
- **Reads.** `evidence.list` takes `repo`, `capability`, `bet` and `limit`, and returns facts newest first: 50 by default, at most 500, with commits as their first eight characters. It reads what the engine stored and starts no poll. `bet.show` adds `Evidence`, at most 50 facts, newest first; when reading them fails, the failure is logged, no evidence is shown, and the rest of the bet is still shown. `physa evidence [--capability <slug>] [--bet <slug>] [--limit N] [--json]` is top-level, because evidence spans capabilities and bets, so it belongs to neither the `bet` group nor `map`.
- **Production.** `workapi.StateOptions` registers the poll and retention kinds, with GitHub as the forge, and `enginecli` passes it to the engine.
- **Packages.** `internal/evidence` holds the facts, read through the `Repo` and `Merged` interfaces, with no store. `internal/state/evidence` holds the poll, its `Record`, the retention and the reads, registered through `state.Options`. `internal/evidencecli` is the top-level command, and `internal/workapi/evidence.go` the method and `bet.show`'s Evidence. The state seams are `IntentKind.Record`, `TimerKind.Key` with `Delay`, and `Tx.StoreDir`. Non-blank lines at the end of this build: `internal/evidence` 226, `internal/state/evidence` 365, `internal/evidencecli` 161, `internal/state` 700 of 700, `internal/engine` 397 of 400, `internal/enginecli` 398 of 400, `internal/repos` 336 of 350, `internal/workapi` 611, `internal/forge` 142, `internal/forge/github` 524, `internal/store` 715 and `cmd/physa` 791 of 800.
