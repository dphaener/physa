# Architecture 07: records and verification

Draft 0.1, 2026-11-20. Uses `.physa/glossary.md` terms only. Builds on 01–05; revises 02 §3. Items marked (Q) are open questions for the owner.

This design says what Physa must know about the work done on a product, where each fact comes from, how it travels, and the little that must be verified. It was worked backwards from what Physa needs (the owner: "Let's work backwards from what we NEED"), using the facts found in the session logs of plan steps 1 and 2 (§2).

It replaces the reviewer agent of the PR check (02 §3.2 step 3), the `Loaded:` trailer (02 §3.4) and the results kept in GitHub check runs (02 §3.6). The owner: "We shouldn't care about reviewing someone else's code, that's their problem."

## 1. Constraints

- **Hard requirements are enforced structurally or deterministically, never by an agent's judgment.** Structurally: the engine delivers the slice, writes the trailers and runs the gates. Deterministically: a check anyone can rerun from the repo. What needs judgment is measured after merge by learning, never enforced at the boundary.
- **Checks run here, before or after a PR, not in a remote environment Physa doesn't control** (the owner: "I feel like that piece and the sync need to happen here, either before or after a PR"). A team may still run the same commands in its own CI.
- **Physa needs neither GitHub nor CI.** A forge is an optional extra (02 Q1 stays: GitHub first, not only).
- **Physa needs version control, not git.** What it needs is one interface; git is its first adapter (§6). The owner: "is there any way to make that part pluggable?"
- **Record what happened; never ask an agent to report what Physa can observe.** The owner: "Physa has access to every agent log correct? It knows what the agent did, right? That can be used can it not?"
- **Unknown is said, never guessed.** Work done outside the engine has fewer facts, and the gaps show as unknown.

## 2. What the session logs showed

The five dharness sessions that built #9–#13 (each a conductor and 9–18 subagents) were read on 2026-11-20 with a throwaway script.

- **Exact and cheap**: tokens, tool calls, test runs and their failures, questions asked, human messages mid-run, and which agents ran with which role and model. Each subagent has its own transcript, linked to its parent.
- **`Loaded:` overstated.** In #12 the commit said `Loaded: engine, learning, map`; only the conductor ran `physa slice`, and none of the seven agents that wrote the code did. In #13, three of six did.
- **Edits are not visible as edits.** Most changes went through the shell (heredocs, `sed -i`), not the editing tools: in #12, 12 shell edits and none through the tools. The diff is the only true record of what changed. Reads are as approximate.
- **`Capability:` was right every time.** Intent is the one thing the log doesn't hold.
- **Capabilities from history are noise on a young repo.** Over #12's files, past trailers pointed at `engine`; the change served `learning`.
- **A command run by Claude Code knows its session** (`CLAUDE_CODE_SESSION_ID`, the transcript's name). A subagent's commands carry its parent's session, not its own.

## 3. What Physa uses the facts for

| Use | Designed in | It answers |
|---|---|---|
| U1 Strength | 05 §6 | Which capabilities and ideas hold up, and which fade? |
| U2 Map usefulness | 01, here | Did the map give agents what they needed? What is dead weight, what is missing? |
| U3 Appetite and bet progress | 03 §10, §10a | What has this bet cost in tokens, and what of the plan has landed? |
| U4 Needs you | 03 §7.5, §9 | What is stuck or waiting on a person now? |
| U5 Trust in the work | 03 §8a, 04 | Where did agents decide alone, get corrected, or rework? |
| U6 Keeping the map true | 02 §3.3 | What did this change make true that the map doesn't say? |

Earned release has its own production signals (05 §3–§4) and is not part of this design.

## 4. The facts

| Fact | Uses | Recorded by | Lives in | Travels by |
|---|---|---|---|---|
| F1 the capability a change serves | U1 U2 U3 U6 | the `Capability:` trailer; the engine writes it on work it runs | version control | version control |
| F2 landed, reverted, rolled back | U1 U3 U5 U6 | version control (rollbacks: 05) | version control | version control |
| F3 the slices each agent had | U2 U5 | `physa map slice` recording each call; the engine recording each brief | the store | a digest (§7) |
| F4 tokens per session, tied to its bet | U3 | the engine, from the session | the store | a digest |
| F5 rework: findings, fix rounds, failing tests | U1 U2 U5 | the engine and its records (03 §8a) | the store | a digest |
| F6 asks and human interventions | U4 U5 | the engine (03 §9) | the store | a digest |
| F7 stalls and current state | U4 | the engine, live | the store | never |
| F8 what changed | U6 | version control (the diff) | version control | version control |
| F9 calls an agent made alone | U5 U6 | after-merge learning, from the log | the store; proposals on `physa/learn` | the proposals PR |

Left out on purpose, because no use needs them: which files were read, capabilities from history, rules at stake, and any agent's judgment of what a change touched.

### 4.1 Slice records (F3)

- `physa map slice` writes a slice record to the store on every call: the capabilities, `--with`, the entries taken with `--entry`, the time, and the calling session when the agent says it (Claude Code: `CLAUDE_CODE_SESSION_ID`). This works before the engine exists.
- Inside a session, which subagent made a call is found by joining the record with that session's logs on time and arguments. Under the engine every role is its own session (04 §5), so a role's slice records are exact without the join.
- **Every change carries `Session: <id>`**, added by a `prepare-commit-msg` hook from `CLAUDE_CODE_SESSION_ID` before the engine exists, never by the agent. A person's own commit has none. The owner: "feels like we should just record session ids to make everything more deterministic." Whoever launches a session assigns its id when it can (the engine always will; the owner: "when a session starts it gets an id. why wait? you can also give a session a pre determined id when you start it."). The hook still stamps sessions Physa did not start. The join on time above is the fallback.
- **A commit made while its session works on a planned change carries `Change: <bet>/<change>`**, added by the same hook from the store, never by the agent. The store names the change when the session runs `physa bet work` (03 §16, §20); a session it names no change for gets no `Change:` line. Progress counts landed changes by this trailer (03 §10a).
- **Map usefulness** (U2) compares the slices an agent had with the capability its change served (F1): served but never loaded, or loaded and never served. Over many changes, a capability that keeps being served without being loaded is a sign its index line or promise doesn't draw agents in. It is a signal about the map, never a judgment of an agent.

### 4.2 Outside the engine

Work done outside the engine (a person's commits, someone's own agent) has F1, F2 and F8 from version control, and F3 where `physa map slice` ran on that machine. F4–F7 and F9 are unknown and shown as unknown.

## 5. What is verified

Only F1 and the map. Both are deterministic and rerun from the repo:

- every change names exactly one capability, and the slug exists in the map as the change leaves it (a change that adds a capability names it);
- the map passes `physa check map`.

Changes made before the map existed are exempt from the first rule: a change whose parent on the default branch holds neither `.physa/product.md` nor `.physa/product/index.md`, the root change included, needs no `Capability:` trailer and is not reported. Init's commit that adds the map is one of them (02 §2.6). Decided by the owner (2026-11-21).

`physa check changes` runs both on a range. It runs as an engine gate before a PR, and by hand (or in a team's own CI) for work outside the engine. It reports; it blocks only where a team's policy says so (02 Q5).

Everything else is recorded (F2–F8) or learning's judgment (F9).

## 6. Version control is an adapter

What Physa needs, as one interface. Git is the only adapter built now; the interface is checked against Mercurial on paper, so it is not git-shaped. A second adapter (Jujutsu is the likely first; Mercurial is the harder test) proves it.

| Operation | Serves | Git | Mercurial |
|---|---|---|---|
| list the changes in a range | F1 F2 | `git log A..B` | revsets |
| read a change's message and its trailers | F1 | the message; Physa's own parser (02 §3.2) | the description; the same parser |
| a stable change id, for anchors | the map | commit hash | changeset hash |
| what a change altered | F8 | diff | `hg diff -c` |
| detect reverts | F2 | revert messages | backout messages |
| where the store lives | the store | `.git/physa/` in the common directory | `.hg/physa/` |
| an isolated working copy for a run | the engine (03) | worktree | share, or a clone |
| attach a digest to a change after the fact | §7 | notes on `refs/notes/physa` | none: a fallback (§7) |
| list the changes since a time | init's gather (02 §2.2) | `git log --since` | `hg log -d '>DATE'` |
| the files a change altered | init's gather | `git diff-tree --name-only` | the `{files}` template |

An adapter may lack an operation; the design names the fallback for each one it may lack.

## 7. Team transport

F3–F6 begin on the machine that did the work. A solo developer's store is that machine. For a team, a small digest per landed change (its slice records, tokens, rework counts, asks and interventions) travels through the version-control adapter: git attaches it as a note on `refs/notes/physa`, pushed and fetched like any ref, with nothing in the working tree. An adapter without notes falls back to the hosted layer, which ingests the same digest. Built after the engine (plan step 5).

`physa sync` becomes: fetch, then recompute F1, F2 and F8 from version control and read the digests. No forge API is needed for any fact. `#N` PR anchors use the forge when one is present (`gh` for GitHub) and are checked for form when not.

## 8. What changes in what has landed

- **Removed**: the reviewer agent, the `reviewing` skill and `physa review` (#13); check-run output, the `pr_checks` table, sync through `gh` and `.github/workflows/physa.yml` (#12); reading `Loaded:` and the slice's request for it (#11).
- **Kept**: the agent adapter (#13), which the engine and learning build on; the store, its migrations and its driver (#12); `physa trailers` for `Capability:` (#11).
- **Added**: slice records (§4.1); the version-control interface in front of `internal/history` (§6).
- **Revised**: 02 §3.2–§3.7, §4 (no `reviewing` skill), Q6 and Q8 point here.

## 9. Open questions

- **Q1** How a capability is born from building work, not from a bet: an agent builds something that deserves its own capability. Version control can check that a named new slug is added; nothing deterministic can tell that one should have been. Is it learning's to propose after merge, or the shaping lifecycle's?
- **Q2** Glossary: is "slice record" a term? The comparison in §4.1 needs names if it is shown in a view.
- **Q3** The digest's exact contents, and whether a team can opt out of sharing tokens.
- **Q4** F9 (calls an agent made alone) needs reading the log with judgment. Does learning do it for every merged change, or only when asked?

## 10. Settled with the owner (2026-11-20)

- The PR check does not review code. "We shouldn't care about reviewing someone else's code, that's their problem."
- Checks and sync run here, before or after a PR, not in CI that Physa doesn't control.
- Physa records what it observes in the session logs rather than asking agents to report it; the agent declares only intent (`Capability:`).
- The uses U1–U6 and facts F1–F9 are what Physa needs; only F1 and the map are verified.
- Version control is an adapter, git first, checked against Mercurial.
- Team transport: notes on `refs/notes/physa` through the version-control adapter, built after the engine.
