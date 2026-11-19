# Architecture 04: the harness interface

Draft 0.2, 2026-11-19. Uses `.physa/glossary.md` terms only. Builds on 01–03 and on `docs/research/harness-spec.md` (cited as "the spec", with its contract items E1–E16). Items marked (Q) are open questions for the owner.

The owner's harness is trusted because of thirteen mechanisms (spec §4). Most of them are not in its prompts: they are gates and records the runtime enforced. This design keeps every one of them, moves them into the engine where the prompts only promised them, and leaves judgment to the harness.

## 1. Constraints

- **The engine never judges; the harness owns judgment** (`engine-no-judgment`). Role prompts, the conductor's choices, what counts as serious, which failure goes to which role: the harness's. Sessions, records, gates, identity, shipping: the engine's.
- **No harness can grade its own work** (`engine-owns-gates`). Gates are pure functions over records and git.
- **What prompts promise, the engine enforces** (`enforce-promises`): isolation, engine-built check briefs, caps.
- **Done needs a record** (03 §7.1).
- **The core never imports a plugin** (`no-plugin-imports`). A harness is files plus calls to the public API.
- **Bring your own agents**: Claude Code first, others through adapters; agents run on the customer's side.
- **Physa's words at the boundary.** A harness names its roles as it likes; the engine sees only role kinds, changes, findings and the records below. "Leaf", "change set" and "Evidence" for a verifier's pass do not cross the boundary.

## 2. What a harness is

A **harness** is a directory:

```
afk/
  harness.toml        the manifest (§4)
  roles/
    conductor.md      one prompt per role, with shared blocks already expanded
    builder.md
    ...
```

- No code. A harness that needs logic puts it in its prompts or in the project's own commands; the engine runs no harness code.
- **Installed per machine** under `~/.config/physa/harnesses/<name>/` from a git URL at a pinned commit. A repo chooses its harness in machine settings (Q12).
- **Versioned by commit.** Every run records the harness commit it used, so rework numbers (03 §8a) can be compared across harness versions. That is how a team sees whether a prompt change saved tokens.

An **adapter** is the other half: what starts and talks to one kind of agent (Claude Code, Codex, opencode). Adapters are part of the core, compiled in, one small package each (Q10).

## 3. The unit of work

| The spec | Physa |
|---|---|
| change set (one PR, many leaves) | **bet**, delivered as its planned changes (03 §10a) |
| leaf (statement, acceptance, files, must_not) | **change** in the bet's plan, with its acceptance (Q3) |
| one PR per change set | **one PR per change** (glossary: a change is usually one commit in its own PR) |
| "Evidence" written by the verifier | a **pass** record (Q4) |
| question to the arbiter | **ask** (03 §9) |

### 3.1 The plan

When a bet starts, the harness's entry role (the conductor, for the owner's harness) writes the plan through the API:

```
physa plan set <<'EOF'
{"changes":[
  {"name":"retry-queue","capability":"delivery",
   "what":"Retries go through the queue",
   "acceptance":["go test ./delivery/... -run TestRetryEnqueue", "failure case: ..."],
   "must_not":["change the attempt limit"],
   "after":[]},
  ...
]}
EOF
```

- Each planned change names exactly one capability. A plan with a change that names two is refused (the glossary: it should have been split).
- **Acceptance** is runnable, scoped to the change's own tests, and holds a failure case or a stated reason why none applies (spec E1).
- `after` orders changes that depend on each other (Q2).
- Revising the plan is `physa plan set` again with a reason; the engine keeps every revision (03 §10a).
- No file lists: agents are good at finding files, and the map holds why, not what. The acceptance is the contract.

### 3.2 A change's path

```
planned ─► building ─► verified ─► checked ─► (sign-off if stakes require) ─► PR ─► CI ─► landed
              ▲  findings │            │ findings
              └───────────┘            ▼
                                  critic ─► fixer ─► checks again
```

- Each change has its own branch off main (or off the change it comes `after`, once that one has landed: Q2), its own worktree under the run's directory, and its own PR.
- **How the PR merges is the team's choice** (squash, merge commit or rebase), a team setting (Q1). The engine anchors the change by its PR number either way, and every commit a session makes carries the change's trailers, so nothing depends on the merge method.

## 4. The manifest

The owner's harness, ported (spec §5.2, in Physa's terms):

```toml
[harness]
name  = "afk"
entry = "conductor"               # started once per bet; spawns the rest
spike = "spiker"                  # started once per spike (optional)

[roles]                           # every role, with its kind (Q5)
conductor = { kind = "conduct" }
spiker    = { kind = "build" }
builder   = { kind = "build" }
verifier  = { kind = "check", workspace = "readonly@change" }
plan      = { kind = "check", workspace = "readonly@change", check = true }
code      = { kind = "check", workspace = "readonly@change", check = true }
qa        = { kind = "check", workspace = "readonly@change", check = true }
critic    = { kind = "check", workspace = "readonly@change" }
fixer     = { kind = "fix" }
ci-fix    = { kind = "fix" }
gate-fix  = { kind = "fix" }
conflict-fix = { kind = "fix" }
arbiter   = { kind = "conduct" }
plan-reviewer = { kind = "check" }       # optional plan review (Q11)
plan-critic   = { kind = "check" }
plan-reviser  = { kind = "fix" }

[findings]                        # the harness's grading (Q6)
levels  = ["blocker","major","minor","nit"]
serious = ["blocker","major"]     # what critics judge and gates count

[done]                            # the engine refuses done without these (03 §7.1)
verifier = "pass | findings"
check    = "round"                # every role with check = true
critic   = "verdicts"
builder  = "commit"
fixer    = "commit"

[gates]                           # all must hold before a change's PR opens
before_pr = ["passed", "reviewed", "covered", "checks-block", "command:local_gate"]

[routes]                          # failure -> role; the engine hands the failure to the entry role
passed       = "verifier|builder"
reviewed     = "critic,fixer"
local_gate   = "gate-fix"
ci           = "ci-fix"
conflict     = "conflict-fix"
signoff      = "fixer"

[caps]                            # enforced by the engine (Q7)
build_attempts = 3                # per change
fix_cycles     = 2                # per change
handbacks      = 3                # per change
command_budget = "4m"
```

## 5. Sessions

### 5.1 Spawning

```
physa spawn <role> --change <name> [--check <name>] [--brief -] [--model <m>]
```

- Only a `conduct` role can spawn. The engine starts the session through the adapter, in the workspace the role declares, and returns at once. **No sleeping, no polling** (spec §2.10).
- When a child ends, the engine sends its parent a message: role, change, outcome, and what it recorded (spec E11).
- **The entry role is one session per bet**, woken by messages, as today. If it is lost, the engine starts a fresh one with the bet's state from the store, so nothing depends on a session's memory (03 §5.2).

### 5.2 Identity

- Every session gets a **session token** in its environment (`PHYSA_SESSION`). Every CLI call a session makes carries it, so the engine knows which session, role and change is calling, and records as that session only (spec E3).
- The engine also credits commits by watching each session's worktree, not by trusting trailers (03 §8a). Trailers stay for people reading history and for CI.
- A commit on a run's branch made by no session is an intervention (03 §8).

### 5.3 What each role sees

Enforced by the engine, not asked of the prompt (`enforce-promises`, spec §3.4):

| Role kind | Workspace | Can read |
|---|---|---|
| conduct | the bet's worktrees | everything about its bet |
| build | its change's worktree, writable | its change's plan entry, slice, earlier findings on this change, run notes |
| check | a **read-only** checkout at the change's commit | its change's plan entry, slice, diff, its own check's rounds. Never build records, notes or other checks' rounds |
| fix | its change's worktree, writable | the findings it was given, slice, diff |

- **Read-only** means a separate checkout with no write permission and no branch to push. A check cannot change the code it judges.
- **Check briefs are built by the engine** from the plan entry, slice and diff. The conductor can add a line; it cannot see or shape the rest (`enforce-promises`).
- Reads go through the token, so "never read the build history" is a refused call, not a request.

### 5.4 Briefs

A session's prompt is: the role's prompt from the harness, then the brief. A brief is assembled by the engine in fixed sections: the change's plan entry, the slice for its capability (`physa slice --change`), the role's inputs from §5.3, and the conductor's line. Team and machine notes are appended last (spec port note 5).

## 6. Records

Seven records, in one shape for every harness. Each is a CLI call with JSON on stdin, carried to the API with the session token.

| Record | Who | Shape | Used by |
|---|---|---|---|
| **commit** | build, fix | git itself; the engine credits it (§5.2) and reads its `Checks:` and `Unfixed:` blocks | `passed`, `checks-block`, rework |
| **pass** | a check role, on a change | the change, what it ran, with results | `passed` (Q4) |
| **findings** | any check role | `{check?, round, findings:[{level, place, what}]}` | `reviewed`, retries, rework |
| **round coverage** | check | one line per acceptance point: what was run or read, and the result | `covered` |
| **verdicts** | critic | `{finding, verdict: confirmed\|downgraded\|dropped, level?, reason}` | `reviewed` |
| **note** | build | one line of learning | later builders only |
| **ask** | any | text, options, a recommendation | 03 §9 |

- Every record is tied to the session, role, change and harness version that wrote it.
- Findings are numbered by the engine (f1, f2…). A later builder or fixer gets them **word for word** (spec: verbatim retry).
- A critic can only confirm, downgrade or drop; the engine refuses a verdict that adds a finding or raises a level (spec §2.7).

## 7. Gates

Pure functions over records and git. A harness chooses which apply; it cannot change what they compute.

| Gate | Holds when |
|---|---|
| `passed` | the change has a pass from a session whose role kind is check and that wrote none of the change's commits |
| `reviewed` | every role with `check = true` has a round on the change's latest commit, and no finding at a `serious` level is still standing |
| `covered` | each round has a finding or coverage line for every acceptance point |
| `checks-block` | the engine re-ran every command in the latest commit's `Checks:` block and the acceptance, in a clean checkout, with the claimed results (`rerun-checks`) |
| `command:<name>` | the project's command (local gate, pre-push) exits 0 |

- **Standing** is computed: a serious finding stands unless it was dropped or downgraded below serious. An unjudged one stands (spec §3.4.4).
- **Unfixed findings are carried**, never dropped: an `Unfixed:` line keeps its finding standing.
- These gates feed the change's **case**: what was checked, by whom, with the engine's own re-run.

## 8. Failures, caps and hand-back

- When a gate fails, or CI is red, or the PR conflicts, the engine **hands the failure back** to the entry role with a structured failure (spec E13): which gate, its output, the failing commands and the files in conflict. The harness's `[routes]` tell the conductor which role to spawn; the engine does not choose.
- **Caps are the engine's** (Q7). A change that reaches `build_attempts`, `fix_cycles` or `handbacks` stops and goes to Needs you with its findings. The spec's gap ("no cap on conductor retries") is closed here.
- **Appetite still bounds everything**: caps stop one change looping; the appetite's circuit breaker stops the bet.
- **The engine ships**: push, PR, CI wait, merge. No role pushes, rebases, forces or merges (spec §2.10).

## 9. Asks and autonomy

- Every role asks through `physa ask` (03 §9). Adapters turn the agent's own question tool into an ask where the agent has one (Claude Code's AskUserQuestion, through its hook).
- The team's autonomy policy routes each ask: to a person's Needs you, or to the harness's arbiter role. The arbiter's answer is recorded as `accepting a proposal` from an agent, never as a person's words.
- An answer becomes a map decision only if it passes the decision test (03 §9).

## 10. Adapters

What the engine needs from an agent, and nothing more (Q10):

| Adapter call | Claude Code |
|---|---|
| start a headless session with a prompt, in a directory, with an environment | `claude -p` with streamed JSON in and out |
| send a message to a running session | streamed JSON input |
| stream output, for watching (03 §7.5) | streamed JSON output |
| report progress (tool activity), for stall detection | from the stream |
| report tokens used | from the stream's usage |
| report blocked (usage limit, permission) | from the stream and exit status |
| turn the agent's question into an ask | its hook |
| end the session | close input; signal the process group |

An adapter is one Go package behind one interface. A second adapter (Codex or opencode) is the test that the interface is not Claude-shaped.

## 11. Spikes

- A harness may declare a `spike` role. A spike is one session with a writable worktree, the idea's text, a very small appetite (03 Q18), and no gates. It ends with a commit and a note of what it learned.
- Its branch stays with the idea (glossary: Spike). A bet can start from it: the conductor sees it in its brief.
- A harness without a spike role cannot run spikes; nothing else changes.

## 12. Porting the owner's harness

Changes to the role prompts, in order:

1. Replace Rhiza's words with Physa's: change, bet, plan, pass, ask, findings levels. Keep the content: the six-section briefs, the severity block, the never-list, the command budget, test scope, the flake rule.
2. Replace `rhiza ...` calls with the §6 records and `physa spawn`, `physa plan`, `physa ask`, `physa done`.
3. Remove what the engine now enforces: "never read the run status" (refused), "detached checkout" (given), retry counting (capped), pushing and landing (the engine's).
4. Drop what no longer exists: `.rhiza` model writes, render, land, overlaps, model checks, lint-fix, model conflicts. The map is written only through learning (02).
5. Plan review (reviewer, critic, reviser) stays as optional roles (Q11). When a harness declares them, they review the bet's plan after the conductor writes it and before building starts, against the pitch and the slice, not a model.

## 13. Settled with the owner (2026-11-19)

- **Q1** How a change's PR merges is the team's choice (the owner: "a very personal decision, the developer/team should decide this"). The engine anchors by PR number and puts trailers on every session's commits, so it works with any method.
- **Q2** Changes that depend on each other land in order: a change marked `after` another branches from main once that one has landed. No stacked PRs.
- **Q3** **Acceptance** is a glossary term.
- **Q4** The verifier's record is a **pass**, not Evidence.
- **Q5** Four role kinds: conduct, build, check, fix.
- **Q6** Findings have **levels** the harness names; the harness says which are serious. Not "severity".
- **Q7** Caps per change in the manifest, enforced by the engine; reaching one goes to Needs you.
- **Q8** Check roles get a read-only checkout and engine-built briefs; their reads of build records are refused.
- **Q9** The engine re-runs the `Checks:` block and acceptance itself; the verifier still judges the diff.
- **Q10** Adapters compiled into the core, one package each; Claude Code first; a second adapter early.
- **Q11** Plan review stays, as optional roles that review the bet's plan before building starts.
- **Q12** Harnesses are installed per machine. (Which harness a repo uses is still open: Q13.)

- **Q13** One harness per machine, the developer's choice; every repo on the machine uses it and nothing about it enters the repo. A team's bar lives in its stakes policy and the PR check, not in the harness. A per-repo override waits until someone needs it; a team default and harness comparison belong to the hosted layer.
