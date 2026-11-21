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
- **`Learned: <change>, …`** trailers on those commits record every change learned from, including those triage found nothing in. The record lives in version control, so no change is learned from twice, whichever machine or runner ran it.
- **Two runners at once**: each pushes `physa/learn` with a lease; the one that loses re-reads the branch, drops what is already learned, and tries again.
- **With a forge**, the branch is opened as one batched PR, refreshed as runs add to it. **Without one**, it stays a branch the steward reviews.
- **It never merges itself** (`no-self-merge`). Stewards accept, edit or drop proposals like any PR.
- **A dropped proposal is remembered**, so it is not proposed again for the same signal (Q2).

## 7. Cost

- An appetite in tokens, per run, shown before the draft starts; the circuit breaker stops the run at the limit and keeps what was drafted.
- The default is the smallest that covers one batch, tuned from real runs.
- Most runs cost nothing, because most changes carry no signal.

## 8. New capabilities

Born when the work is done, not learned later (07 Q1): if a change deserves its own capability, the change adds the capability file and anchors to it, and `physa check` verifies the slug exists. Learning proposes a new capability only when a signal says so.

## 9. The example workflow

A documented recipe, not something Physa installs: a workflow that, on pushes to the default branch, installs the agent, runs `physa learn --appetite <tokens>` with the credential from the team's secrets, and pushes `physa/learn`. It uses only what any CI with an agent installed can do.

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
