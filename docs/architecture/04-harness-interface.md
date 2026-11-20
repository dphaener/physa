# Architecture 04: the harness interface

Draft 0.3, 2026-11-20. Uses `.physa/glossary.md` terms only. Builds on 01–03 and on `docs/research/harness-spec.md` (cited as "the spec", with its contract items E1–E16). Items marked (Q) are open questions for the owner.

The owner's harness is trusted because of thirteen mechanisms (spec §4). Most of them are not in its prompts: they are gates and records the runtime enforced. This design keeps every one of them, moves them into the engine where the prompts only promised them, and leaves judgment to the harness.

## 1. Constraints

- **The engine never judges; the harness owns judgment** (`engine-no-judgment`). Role prompts, the conductor's choices, what counts as serious, which failure goes to which role: the harness's. Sessions, records, gates, identity, shipping: the engine's.
- **No harness can grade its own work** (`engine-owns-gates`). Gates are pure functions over records and git.
- **What prompts promise, the engine enforces** (`enforce-promises`): isolation, engine-built check briefs, caps.
- **Done needs a record** (03 §7.1).
- **The core never imports a plugin** (`no-plugin-imports`). A harness is files plus calls to the public API.
- **Bring the harness you already have.** Developers package workflows today as skills and plugins for their agent (Agent Skills, Agent Plugins, Claude Code plugins). Physa installs those as they are; nobody rewrites a harness into Physa's format.
- **Bring your own agents**: Claude Code first, others through adapters; agents run on the customer's side.
- **Physa's words at the boundary.** A harness names its roles as it likes; the engine sees only role kinds, changes, findings and the records below. "Leaf", "change set" and "Evidence" for a verifier's pass do not cross the boundary.

## 2. What a harness is

### 2.1 The skeleton

Every harness runs in one skeleton the engine owns:

- the four role kinds (conduct, build, check, fix);
- **every role is its own session started by the engine**, in the workspace its kind gets (§5.3);
- done needs a record, caps, gates and shipping (§6–§8).

**Physa's default harness** is that skeleton with Physa's own prompts in every role: a conductor, a builder, one check and a fixer. It works on day one for a developer with no harness at all.

### 2.2 Installing a harness

A developer's existing plugin fills the skeleton (Q14):

```
physa harness install <git URL or path>
```

1. **Read the plugin**: its subagent definitions (name, prompt, tools, model), skills, commands and hooks. A subagent without edit or write tools is already shaped like a check.
2. **An agent proposes the harness settings**: which subagents are roles, each role's kind, the record it must leave before done, the caps. Roles the plugin does not provide are filled by Physa's default prompts. A plugin with only skills fills fewer roles, and its skills are loaded into the roles that use them.
3. **The developer reviews the harness settings once**: each mapping, and every prompt line that spawns a role natively (§5.1), so a prompt that relies on it is visible before it stumbles.
4. **The plugin's prompts are never edited.** Upstream updates still apply: updating the plugin proposes the settings' diff for review.

On disk, one harness per machine (Q13):

```
~/.config/physa/harness/
  plugin/             the developer's plugin, at a pinned commit, untouched
  harness.toml        the harness settings, reviewed (§4)
```

- **No harness code runs in the engine.** Logic lives in prompts or in the project's own commands.
- **Versioned by the plugin's commit and the settings'.** Every run records both, so rework numbers (03 §8a) can be compared across harness versions. That is how a developer sees whether a prompt change saved tokens.
- **The owner's harness is installed the same way as anyone's**, with no special path. It is the first test of the installer.

An **adapter** is the other half: what starts and talks to one kind of agent (Claude Code, Codex, opencode). Adapters are part of the core, compiled in, one small package each (Q10).

## 3. The unit of work

| The spec | Physa |
|---|---|
| change set (one PR, many leaves) | **bet**, delivered as its planned changes (03 §10a) |
| leaf (statement, acceptance, files, must_not) | **change** in the bet's plan, with its acceptance (Q3) |
| one PR per change set | **one PR per change** (glossary: a change is usually one commit in its own PR) |
| "Evidence" written by the verifier | a **pass** record (Q4) |
| question to the arbiter | **ask** (03 §9) |
| a harness written for Physa | the developer's own plugin plus reviewed **harness settings** (§2.2) |

### 3.1 The plan

When a bet starts, the harness's conduct role (the conductor, for the owner's harness) writes the plan through the API:

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

## 4. The harness settings

What install proposes and the developer reviews. The owner's harness, as its `harness.toml` would read:

```toml
[harness]
plugin = "github.com/dphaener/afk-harness@<commit>"
conduct = "conductor"             # started once per bet; its prompt is the plugin's, unedited
spike   = "spiker"                # started once per spike (optional)

[roles]                           # plugin subagents that are roles; everything else stays native (§5.1)
conductor = { kind = "conduct" }
builder   = { kind = "build", done = "commit" }
verifier  = { kind = "check", done = "pass | findings" }
plan      = { kind = "check", check = true, done = "round" }
code      = { kind = "check", check = true, done = "round" }
qa        = { kind = "check", check = true, done = "round" }
critic    = { kind = "check", done = "verdicts" }
fixer     = { kind = "fix",   done = "commit" }
ci-fix    = { kind = "fix",   done = "commit" }
gate-fix  = { kind = "fix",   done = "commit" }
conflict-fix = { kind = "fix", done = "commit" }
arbiter   = { kind = "conduct" }
plan-reviewer = { kind = "check", done = "round" }   # optional plan review (Q11)
plan-critic   = { kind = "check", done = "verdicts" }
plan-reviser  = { kind = "fix" }
spiker    = { kind = "build", done = "commit" }
# filled by Physa's default prompts: none

[findings]                        # the harness's grading (Q6)
levels  = ["blocker","major","minor","nit"]
serious = ["blocker","major"]     # what critics judge and gates count

[gates]                           # all must hold before a change's PR opens
before_pr = ["passed", "reviewed", "covered", "checks-block", "command:local_gate"]

[routes]                          # failure -> role; the engine hands the failure to the conduct role
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

[review]                          # what the developer saw when accepting
native_spawn_lines = []           # prompt lines that spawn a role natively
```

## 5. Sessions

### 5.1 Spawning

```
physa spawn <role> --change <name> [--check <name>] [--brief -] [--model <m>]
```

- **Starting a role always goes through the engine.** Only a conduct role can spawn. The engine starts the session through the adapter, in the workspace its kind gets, and returns at once. **No sleeping, no polling** (spec §2.10).
- **A role's subagent definition is not loaded natively** in any session: the verifier, the checks and the critic exist only through `physa spawn`. No hook is needed; the engine decides which subagent definitions each session loads.
- **Native subagents are normal inside a role** (Q15). A planner sending out research agents, or a builder sending one to read the code, is part of that role's work. They run inside its session and inherit its workspace (a check's subagent reads the same read-only checkout), its identity (their tokens are credited to the role, from the agent's stream) and its records (only the role itself can record findings, a pass or done).
- **A native subagent can never stand in for a role.** If a conductor tells a generic subagent to verify the build, its output is part of the conductor's session and cannot be a pass: the `passed` gate counts only a pass from an engine-started check session that wrote no commits. Trust comes from records and gates, not from policing what agents do.
- When a child ends, the engine sends its parent a message: role, change, outcome, and what it recorded (spec E11).
- **The conduct role is one session per bet**, woken by messages, as today. If it is lost, the engine starts a fresh one with the bet's state from the store, so nothing depends on a session's memory (03 §5.2).

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

- When a gate fails, or CI is red, or the PR conflicts, the engine **hands the failure back** to the conduct role with a structured failure (spec E13): which gate, its output, the failing commands and the files in conflict. The harness's `[routes]` tell the conductor which role to spawn; the engine does not choose.
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
| load only the subagent definitions the engine allows (§5.1) | `--plugin-dir` for the installed plugin, `--setting-sources` limited so no other copy loads, and a deny rule `Agent(<role>)` per role subagent. Verified 2026-11-20 (Claude Code 2.1.323), for subagents given with `--agents` and for subagents from a plugin, where the rule uses the plugin's namespaced name (`Agent(<plugin>:<role>)`): a denied subagent is not offered, a direct attempt is refused with "denied by permission rule", and other subagents run normally |
| end the session | close input; signal the process group |

An adapter is one Go package behind one interface. A second adapter (Codex or opencode) is the test that the interface is not Claude-shaped.

## 11. Spikes

- A harness may declare a `spike` role. A spike is one session with a writable worktree, the idea's text, a very small appetite (03 Q18), and no gates. It ends with a commit and a note of what it learned.
- Its branch stays with the idea (glossary: Spike). A bet can start from it: the conductor sees it in its brief.
- A harness without a spike role cannot run spikes; nothing else changes.

## 12. Installing the owner's harness

The owner's harness is installed like anyone's (§2.2). What install has to deal with:

1. **Its Rhiza calls.** Its prompts call `rhiza run findings`, `rhiza run spawn` and the like. The engine adds a short "how to record" section to every role's brief naming Physa's calls, and the settings review lists every prompt line that still names a Rhiza call. Lines that cannot work (writes to `.rhiza`, render, land, overlaps, model checks, lint-fix) are shown so the owner can update the plugin upstream, in its own repo, not through Physa.
2. **What the engine now does.** "Never read the run status" is a refused call, "detached checkout" is given, retries are capped, pushing and landing are the engine's. The prompts can keep saying so; it costs a few lines.
3. **Plan review** (reviewer, critic, reviser) stays as optional roles (Q11): they review the bet's plan after the conductor writes it and before building starts.

Because the plugin is never edited by Physa, the owner's harness and Rhiza can run side by side until the switch.

## 13. Settled with the owner (2026-11-19)

- **Q1** How a change's PR merges is the team's choice (the owner: "a very personal decision, the developer/team should decide this"). The engine anchors by PR number and puts trailers on every session's commits, so it works with any method.
- **Q2** Changes that depend on each other land in order: a change marked `after` another branches from main once that one has landed. No stacked PRs.
- **Q3** **Acceptance** is a glossary term.
- **Q4** The verifier's record is a **pass**, not Evidence.
- **Q5** Four role kinds: conduct, build, check, fix.
- **Q6** Findings have **levels** the harness names; the harness says which are serious. Not "severity".
- **Q7** Caps per change in the harness settings, enforced by the engine; reaching one goes to Needs you.
- **Q8** Check roles get a read-only checkout and engine-built briefs; their reads of build records are refused.
- **Q9** The engine re-runs the `Checks:` block and acceptance itself; the verifier still judges the diff.
- **Q10** Adapters compiled into the core, one package each; Claude Code first; a second adapter early.
- **Q11** Plan review stays, as optional roles that review the bet's plan before building starts.
- **Q12** Harnesses are installed per machine. (Which harness a repo uses is still open: Q13.)

- **Q14** Every harness runs in Physa's skeleton. Install maps an existing plugin's subagents and skills onto roles; Physa's default prompts fill any role the plugin lacks; the developer reviews the harness settings once; the plugin's prompts are never edited. (The owner: "Why not just do that for everything?")
- **Q15** Native subagents are allowed inside any role and share its workspace, identity and records (the owner: research agents when planning "should 100% be a normal thing that is allowed"). Only starting a role goes through `physa spawn`; a role's subagent definition is not loaded natively. No hooks.
- **Q13** One harness per machine, the developer's choice; every repo on the machine uses it and nothing about it enters the repo. A team's bar lives in its stakes policy and the PR check, not in the harness. A per-repo override waits until someone needs it; a team default and harness comparison belong to the hosted layer.
