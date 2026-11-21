# Architecture 08: learning

Draft 0.1, 2026-11-21. Uses `.physa/glossary.md` terms only. Builds on 01, 02 and 07; replaces 02 §3.3 and §3.5. Items marked (Q) are open questions for the owner.

Learning is how the map keeps its promise: "The map gets better with every PR, without anyone curating it." This design says what learning learns from, when an agent is involved, where it runs, and how proposals reach the map.

It follows 07: Physa does not review code (`no-code-review`). Learning never reads a diff looking for lessons; it spends tokens only where something shows a lesson was learned.

## 1. Constraints

- **Signals only, never the bare diff.** A merged change with no signal costs nothing.
- **Nothing changes silently** (`no-self-merge`). Learning proposes; stewards accept.
- **Runs wherever `physa learn` can run**: a developer's machine, the engine after it lands a change, a scheduled job, or a team's CI with an agent installed (the owner: "A team may be able to run that elsewhere. They may have claude installed in their github instance and it could run as a CI action. We should build in that flexibility"). Physa requires none of them.
- **Spends tokens under an appetite**, shown before it starts, with a circuit breaker (as init, 02 Q4).
- **Measure before setting budgets** (`measure-before-budget`): defaults are tuned from real runs.

## 2. The pipeline

Four stages. Only the third spends tokens.

| Stage | Tokens | What it does |
|---|---|---|
| 1. Collect | none | For each merged change not yet learned from, gather its signals (§3) |
| 2. Triage | none | Deterministic rules decide whether the change carries a signal; a change with none is marked learned |
| 3. Draft | the agent | One session per run, never one per change: turns the batch's signals into proposed map entries |
| 4. Check and propose | none | `physa check map` validates every draft; malformed drafts are dropped and reported; the rest go to `physa/learn` (§6) |

## 3. Signals

| Signal | From | Proposes |
|---|---|---|
| A review comment that led to a fix in the same change | the forge | a rule |
| A discussion where options were argued | the forge | a decision, with the options rejected |
| A revert or a rollback | version control; rollbacks (05) | a `Rejected:` item or a question on what it undid |
| The same kind of finding recurring on one capability (03 R4) | the store or team digests (07 §7) | a rule |
| Asks and human interventions in a session | session logs; digests | a rule, a decision or a question |
| A capability served without its slice being loaded, repeatedly (07 §4.1) | slice records; trailers | a sharper index line or promise |
| A comment saying work deserves its own capability | the forge | a new capability (§8) |

Triage thresholds (how many recurrences, how long a thread) start small and are tuned from real runs.

Drift, a change quietly undoing a decision, is caught only when a signal points at it. That is the price of not reviewing code.

## 4. The draft

- **Physa's own role**, through the agent adapter (04 §10, `internal/adapter`), with no tools: its inputs are written by others and untrusted.
- **The `learning` skill**, embedded in Physa and replaceable by a team, in glossary terms only. It receives the batch's signals, each with its change, and the slices for the capabilities they bear on.
- **Structured output**: proposed entries in the map's own form, each anchored to the change or changes it came from. Anything that names a slug the map lacks, or fails `physa check map`, is dropped and reported, never silently.
- **Judgment it makes**: whether a comment was a lesson or a nit, the wording of the entry, and its `Because:`.

## 5. Where it runs

`physa learn` is self-contained, so the same command runs anywhere:

- **Inputs from shared places first**: version control, the forge when there is one, and team digests on `refs/notes/physa` once they exist (07 §7). Local session logs are an extra, added when it runs on the machine that did the work.
- **Non-interactive**: the appetite comes from a flag or the settings, never a prompt; exit codes mean something; output is plain or `--json`.
- **The agent is a setting**: the adapter (Claude Code first) and its credentials come from settings and the environment.
- **Safe to run anywhere, any number of times**: see §6.

Where it runs, stage by stage:

- **Now (plan step 3)**: whoever runs `physa learn`. For a solo developer, their machine; for a team, any teammate, often a steward.
- **With the engine (plan step 6)**: the machine that landed a change learns from it right after landing, while its session logs are local.
- **In a team's CI, if it wants**: Physa documents an example workflow that runs `physa learn` after merges (§9). Init does not add it.

Signals that arrive later are still caught: a revert is a merged change of its own, picked up by the next run anywhere. Review comments added after a merge are not learned from for now (Q3).

## 6. Proposals and their record

- **The `physa/learn` branch** holds the batch: proposed map edits, one commit per run.
- **`Learned-through: <change id>`** is the record, a watermark: the newest default-branch change learned from, with every change before it learned. Each learning commit carries one. Physa reads it from the newest such trailer on `physa/learn` (local or remote) and on the default branch's history, and takes the later one. A squash merge of the learn PR carries the trailer onto the default branch, so the record survives the branch being deleted. A change not fully triaged (one whose signals could not all be found) holds the watermark back, so it is looked at again. The record lives in version control, so no change is learned from twice, whichever machine or runner ran it. Closed by Claude as arbiter (2026-11-21, for the owner to review): per-change `Learned:` trailers alone are lost when the learn PR is squash-merged and its branch deleted.
- **`Learned: <change>, …`** stays too, listing the run's changes that had signals.
- **Two runners at once**: each pushes `physa/learn` with a lease; the one that loses re-reads the branch, drops what is already learned, and tries again.
- **With a forge**, the branch is opened as one batched PR, refreshed as runs add to it. **Without one**, it stays a branch the steward reviews.
- **It never merges itself** (`no-self-merge`). Stewards accept, edit or drop proposals like any PR.
- **A dropped proposal is remembered**, so it is not proposed again for the same signal (Q2).

**Built in step 3c.** Calls made while building the branch, for the owner to check:

- **Flags**: `--write` commits the run's proposals to `physa/learn`; `--push` (needs `--write`) pushes it with a lease; `--pr` (needs `--push`) opens or refreshes the PR. None of them goes with `--signals`.
- **Rebuilt on the default branch**: each run starts from the default branch, replays the branch's earlier unmerged commits onto it (dropping any that add nothing now), and adds one commit. A replay that conflicts stops the run, naming the change. A `physa/learn` checked out anywhere is refused.
- **An isolated working copy under the store dir**: the user's working copy, index and checked-out branch are never touched. With `--write`, drafting and checks use that copy's map (the default branch plus earlier proposals), and kept proposals are applied one by one, each checked again.
- **Fetch and base**: with `--push`, the default branch and `physa/learn` are fetched from origin first and the run is based on the remote's `physa/learn`. Without `--push` nothing is fetched and the local branch is preferred.
- **Commit shape**: subject `docs(map): Learn from <n> change(s)`; a body with one line per proposal, or `No proposals; the watermark advances.`; trailers `Learned-through`, `Learned` when any change had signals, then `Capability: learning`. The committer is git's configured identity. Every learning commit carries `Learned-through`: the newest fully triaged change, else the change the run started after, else `none` when the run started at the repo's first change. A run that neither advances the watermark nor has proposals writes nothing.
- **A lost lease redoes the record and the write, not the draft**: there is no second agent session. Proposals whose changes are now learned are dropped and the later watermark is taken. A second loss stops with a clear message.
- **The PR**: found by its head `physa/learn` among open PRs, refreshed from every unmerged commit's proposal lines. There is no PR for a run with no proposals. Without a forge, the branch stays for the steward to review.
- **Exit codes**: 0 written, or nothing to write; 1 the circuit breaker stopped and nothing was written; 2 failure.

## 7. Cost

- An appetite in tokens, per run, shown before the draft starts; the circuit breaker stops the run at the limit and keeps nothing (step 3b's call, below).
- The default is the smallest that covers one batch, tuned from real runs.
- Most runs cost nothing, because most changes carry no signal.

**Built in step 3b.** Calls made while building the draft, for the owner to check:

- The appetite counts every token in and out, cached input included.
- The circuit breaker keeps nothing when it stops a session: no partial text is drafted or proposed, and the stopped run says so.
- The agent is chosen with `--agent` or `PHYSA_AGENT`, `claude` by default; credentials come from the environment.
- Each proposal is checked alone on a scratch copy of the map, so one bad proposal cannot take the others down with it.

## 8. New capabilities

Born when the work is done, not learned later (07 Q1): if a change deserves its own capability, the change adds the capability file and anchors to it, and `physa check` verifies the slug exists. Learning proposes a new capability only when a signal says so.

## 9. The example workflow

A documented recipe, not something Physa installs: `docs/recipes/learn-on-github.md`, a GitHub Actions workflow that, on pushes to the default branch, installs the agent and runs `physa map learn --write --push --pr --appetite <tokens>` with the credential from the team's secrets. It uses only what any CI with an agent installed can do.

## 10. What changes

- **Replaces** 02 §3.3 and §3.5; 02 §3 carries a note pointing here. Vision §4.4 is rewritten to match 07 and this design.
- **Plan step 3** builds §2–§7 and §9.
- **Skills Physa ships** (02 §4): mapping, recovering, learning.

## 11. Open questions

- **Q1** Who is notified when the learn PR has new proposals: the capability's stewards (glossary: Steward), or whoever ran it?
- **Q2** How a dropped proposal is remembered: in the store, or on the `physa/learn` branch so it travels with the team.
- **Q3** Whether review comments added after a merge are worth rechecking recently learned changes for.

## 12. Settled with the owner (2026-11-21)

- Learning learns from signals only; a change with none costs nothing; an agent is used only to draft entries from signals ("that makes sense, and is necessary").
- `physa learn` runs wherever a team chooses, including CI with an agent installed; Physa requires none of them.
- Proposals batch on `physa/learn`, recorded with `Learned:` trailers; it never merges itself.
- New capabilities are born when the work is done; learning proposes one only on a signal.
