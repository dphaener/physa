# Architecture 16: the engine runs the work

Draft 0.1, 2026-11-27; settled with the owner on a design page on 2026-11-26, question by question (§9), except the signal (§10). Uses `.physa/glossary.md` terms only. Serves Myxo's idea `engine-runs-work`: "The engine runs the work: it starts a pursuit's harness runs change by change, supervises them, records findings and asks, and moves on when a change lands, so no person or orchestrating agent has to push it."

Today the engine keeps records: ideas, pursuits, plans, spend and evidence. Building a pursuit still needs an orchestrating agent to start a harness for each change, watch for its PR and move to the next one, and a person or a watch to merge. The engine's promise is the opposite: "Work keeps moving through its lifecycle without a person or an orchestrating agent pushing it."

## 1. The loop

```
pursuit active, plan set
  └─ for each change whose `after` changes have landed (independent ones in parallel, within the machine's slots):
       start a run of the team's harness on it ──▶ run ends: PR open, or failed, or blocked
       PR open ──▶ people review and merge (the engine never merges)
       merged ──▶ "landed" fact from the forge ──▶ the next change
       blocked ──▶ Needs you, and the run waits
  └─ every change landed ──▶ the pursuit is done
```

A run is one change, as the glossary's Run says. A run ends at an open PR; people keep the merge. The owner: "this changes with earned releases". The engine stays up while any pursuit has work pending (03 §2) and polls the forge with backoff.

## 2. Physa's API: a contract, not a host

The owner, on stepping back: "Let's think about this as an API with strict boundaries. How do we define what Physa's API is and what a harness must do to satisfy the API." Physa doesn't host harnesses or reach inside them. It defines a contract, and whatever meets it is a harness: dharness, a shell script, a person with a checklist.

```
  PHYSA CALLS (the team implements)     PHYSA LISTENS (the team reports)
  start   build this change             state    working · blocked · done · error
  result  where did it end up?          ask      needs a person (blocks)
  stop    end this run                  call     chose X on a person's behalf
                                        finding  a check said this (the harness's word)
                                        outcome  each acceptance item: passed, failed or
                                                 judged, for an exact commit, and who vouched
  spend and session identity come from the agent adapter
```

- **start** runs with `PHYSA_RUN` (the run), `PHYSA_CHANGE` (the change, as the `Change:` trailer names it) and `PHYSA_SPEC` (the absolute path of the change's spec file: what, acceptance, must not, capability, slice).
- **result** prints one JSON object: `{"branch": …, "commit": <full sha>, "pr": <url>}`. `pr` is required, since a run ends at an open PR. Exit 0 means the JSON is valid; any other code is an error, with stderr kept on the run's record.
- **state** comes free where the agent emits OSC 7501 (§5). The rest a harness reports with `physa` commands or through a `report` command of its own.
- Everything is files, environment, JSON on stdout and exit codes, so any language and any agent can meet it.

A TOML file in the team's repo binds each verb to the team's own commands. For dharness, unchanged:

```toml
[run]
start  = "claude --plugin-dir ~/code/dharness '/dharness:go --autonomous {change}'"
result = "bin/physa-result"      # reads .dharness state and the draft PR
[report]
asks    = "bin/physa-inbox"      # .dharness inbox lines → asks and calls
outcome = "bin/physa-outcomes"   # dharness's verdict → an outcome per acceptance item
```

## 3. Levels, not one install path

| Level | The team provides | Physa gives |
|---|---|---|
| 0 | nothing: work by hand, `Change:` from the hook | records and evidence (today) |
| 1 | `start`, `result`, an outcome per acceptance item | the engine drives the loop and holds a change until its record is complete |
| 2 | `report` | asks and calls in Needs you, findings recorded |
| 3 | a role per kind (conduct, build, check, fix) | the engine runs each role, with isolation between them |

A team takes only what it wants. Each run records its level, so stakes can later require a higher one.

## 4. Structure, not functionality

The owner: "Why is physa bothering verifying what a harness has already verified? That's the harness's business. … We're responsible for the structure not functionality of their code." And: "We just need to enforce things that ensure Physa can continue to function as advertised. If people want to produce crappy software that's up to them, we can't stop that."

**Physa enforces** what its own records depend on:

- the commit exists and belongs to the change, with its `Change:` trailer;
- every acceptance item has a reported outcome for that exact commit, and an outcome for another commit is stale;
- every outcome says who vouched for it.

A change missing any of that isn't ready: the engine holds it, out of draft or unmerged as the team's rules say. **Physa never runs** anyone's code or tests, in CI or anywhere else. Worktrees, databases, branches, PR style and draft-or-not belong to the harness and the team, never to Physa. Trust comes afterwards, from the evidence Physa already measures: reverts, defects, rework.

Acceptance items name a command or a test (the owner's Q5); prose items stay allowed and are reported as judgment, not as passed. Reporting is what's strict.

## 5. Running agents

The engine starts an agent interactively in a terminal it owns, or in a Rex tab where Rex runs, not with `claude -p`. Tested on 2026-11-26: `claude -p` emits no OSC 7501; an interactive Claude Code reports `working` then `done` when its terminal answers the protocol's query. That terminal also lets a person watch a run (03 `headless-watchable`). Blocked states are tested before this ships.

Physa may emit OSC 7501 itself, so its runs and Needs you show where a terminal shows program status.

## 6. What changes in the core

These are the owner's calls, and this design carries them into the core:

- **Vision §5, item 7** becomes: "**The engine enforces structure and never judges.** It owns gates, identity and records, and labels every claim with who made it; it never runs a team's code."
- **Vision §8**: "The engine re-runs the checks itself and never takes an agent's word for them" becomes "The engine checks that every acceptance item has an outcome for the exact commit, and records who vouched for it; trust comes from the evidence that follows."
- **Vision §9**: "so a harness can never grade its own work" becomes "so every claim a harness makes is recorded as its own".
- **The engine rule `engine-owns-gates`** becomes "The engine owns the gates on structure: a change is ready only when its record is complete, and every claim carries who made it."
- **The harnesses decision `install-harness`** is superseded by `harness-contract`, with the old decision kept as a rejected option.
- **The glossary's Harness** becomes: "A team's workflow for building and checking changes, whatever meets Physa's harness contract (start, result, and what it reports), bound by a file in the repo. A harness may be run role by role (conduct, build, check, fix), or as a whole."

## 7. What the engine needs that it doesn't have

Runs, sessions, asks, calls, findings and outcomes in the store; the loop and its timers; the terminal for agents; the TOML file and its loader; supervision (03 §7: stalls, slots, cleanup). Most of 03 already designs these; this design narrows what a run is and what the engine checks.

## 8. What stays out

Merging (people keep it until earned release), running a team's tests, owning environments, and grading code. A harness that wants Physa to run its roles opts into level 3.

## 9. Settled with the owner (2026-11-26)

On the design page, each answered on its card:

1. **A contract replaces "harness as plugin"**: the glossary's Harness becomes whatever meets Physa's contract, bound by the TOML; roles are level 3. "Yes".
2. **Reopen `install-harness`**: two paths, boundary enforcement for an unchanged harness and roles for full isolation, each run recording which. "Yes".
3. **Files, JSON and exit codes**, with the names and shapes in §2. "Yes", then "No preference" on names.
4. **Structure, not functionality** (§4). "1 sounds right."
5. **Acceptance names a command or a test**; prose is judgment. "Yes".
6. **Agents run in a terminal the engine owns**, after testing blocked states. "Yes".

Round 1 (the loop, §1): "1. Yes 2. Yes, but this changes with earned releases 3. Yes".

## 10. Question for the owner

1. **The signal for `engine-runs-work`**: claim, a pursuit with a plan moves from its first change to its last with no person or orchestrating agent starting runs or moving it on; for, pursuits whose changes all ran and landed through the engine, with people only reviewing; against, pursuits that still need an agent or a person to start runs or move to the next change; window 2027-06-01; resolver, Darin, reading the engine's run records. Yes?
