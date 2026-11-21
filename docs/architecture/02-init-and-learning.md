# Architecture 02: creating the map and keeping it learning

Draft 0.1, 2026-11-19. Uses `.physa/glossary.md` terms only. Builds on `01-map-and-slice.md`. Items marked (Q) are open questions for the owner.

The map is only worth something if it gets created on a real codebase and keeps learning without anyone curating it. This doc covers both: `physa init`, and what happens around every PR.

## 1. Constraints

From the vision and 01:

- Agents always run on the customer's side, with the customer's own agent and keys. Physa never runs an agent of its own.
- Agents judge; Physa gathers, checks and records. Nothing deterministic guesses, and nothing an agent produces is trusted without `physa check map`.
- Nothing changes silently: every map change arrives as a proposal a person accepts.
- Small footprint: what init adds to the repo is listed exactly (§2.6).
- The open-source core must work without the hosted layer, including measuring slice misses and excess.

## 2. `physa init`

> **Brought up to date on 2026-11-21, decided by Claude as arbiter for the owner to review.** Gather (§2.2) is built as `physa init --gather`. §2.6 adds the hook, and §2.8 drops the workflow, which 07 removed.

### 2.1 Phases

```
gather ──▶ propose capabilities ──▶ recover knowledge ──▶ check ──▶ review as a PR
(no agent)    (agent)                 (agent, per capability)  (no agent)   (a person)
```

### 2.2 Gather (deterministic, no agent)

Physa collects sources into the local store. Nothing is written to the repo yet.

| Source | What is kept | How |
|---|---|---|
| Existing agent and contributor docs | AGENTS.md, CLAUDE.md, Cursor rules, CONTRIBUTING, README, any decision-record folders | files in the repo |
| Git history | commit messages, trailers, reverts, which files change together | the version-control interface (07 §6) |
| PRs | titles, bodies, review comments (especially requested changes), linked issues | the forge interface (GitHub through `gh` first) (Q1) |

- **History is bounded** by default: the last 12 months or 1,000 PRs, whichever is smaller (the shorter window). Teams can widen it with `--since YYYY-MM-DD` (the date bound) and `--max-prs N` (the PR bound).
- **When the PR limit is reached first**, history starts at the merge time of the oldest gathered PR, so both sources cover the same window.
- **Without a forge**, only the date applies, and PRs are reported unavailable. That is not a failure.
- **Decision-record folders** are found by name or by shape (numbered markdown files). Files over 2 MB are skipped and listed in the summary; fitting the sources to an agent is the next phase's job (§2.3).
- **Files that change together** are used only as hints for proposing capabilities during init. File paths never enter the map.
- **`physa init --gather`** runs this phase alone. It writes only the store, replacing the previous gather in one transaction, and prints a summary as text or `--json`: counts by source, the bound used, and an estimated token count (bytes/4, an estimate).

### 2.3 Propose capabilities (agent)

The customer's agent receives the gathered sources (summarised to fit), Physa's mapping skill, and the budgets from 01. It proposes:

- the product's promise for `product.md`;
- up to ~50 capabilities, each a promise, each with the PRs that best show it.

This is one agent session. It is the step that most shapes the map, so it is the step a person reviews most closely.

### 2.4 Recover knowledge (agent, one session per capability)

For each capability, a fresh agent session receives that capability's promise and the sources linked to it, and writes `capabilities/<name>.md`:

- **rules** from review corrections, docs and reverts, each with `Because:` and an anchor;
- **decisions** from PR discussions, with who, when and the options rejected;
- **terms** for the glossary;
- **questions** for anything uncertain (Q2).

Sessions are independent, so they can run in parallel.

**Uncertain knowledge becomes a question, never a guess.** If the sources suggest a rule but do not establish it, the agent writes a question ("Is it true that retries must never run in-process? Suggested by #1290.") instead of a rule. There are no confidence scores in the map.

### 2.5 Check (deterministic)

`physa check map` (01 §8.3) validates every file. An agent's output that fails is sent back to the same session with the errors, up to two times. After that, the failing entries are dropped and listed in the review summary.

### 2.6 Review as a PR (a person)

Init commits to a branch `physa/init` and opens a PR (Q3) containing exactly:

| File | Purpose |
|---|---|
| `.physa/README.md`, `product.md`, `glossary.md`, `capabilities/*.md` | the map |
| 2–3 lines in `AGENTS.md`, between markers | the pointer (01 §7) |

Init also installs the `Session:` hook, which `physa map hooks` does in a repo that ships one. Init takes that over (08 §5). The hook is local git config (`core.hooksPath`), so it is not part of the PR.

The PR description summarises: capabilities proposed, entries recovered with their sources, questions raised, entries dropped, and the tokens spent. The person edits, deletes or merges it like any PR.

### 2.7 Appetite for init

Init spends the customer's tokens, so it has an appetite (Q4): a token budget shown before it starts, with a circuit breaker. If recovery runs out, the capabilities already done are kept and the rest are listed as "not yet recovered", to be filled from future PRs.

### 2.8 A new codebase

With nothing to recover, init asks the person a short round of questions (numbered, each with a suggested answer) to write the product's promise and first terms, creates `product.md` and `glossary.md`, and adds the pointer and the hook. Capabilities arrive with the first PRs.

## 3. Around every PR

> **Revised 2026-11-20 by [07](07-records-and-verification.md).** The PR check no longer runs a reviewer agent, the `Loaded:` trailer is replaced by slice records, results are no longer kept in GitHub check runs, and checks and sync run on the developer's machine. Where this section and 07 disagree, 07 holds. §3.3 and §3.5 are replaced by [08](08-learning.md).

### 3.1 Two moments

| Moment | Purpose | Output |
|---|---|---|
| **While the PR is open** (opened or updated) | check the change against the map | one comment, kept up to date; suggested map edits on this PR |
| **After it merges** (and when a revert merges) | learn from what happened, including the review | proposed map updates, batched (§3.5) |

### 3.2 While the PR is open

Deterministic first, then one agent session:

1. **Trailers**: every commit names exactly one capability, and the slug exists. A commit with none gets a suggested trailer from the reviewer. Settled with the owner (2026-11-20): trailers are read from every final paragraph made only of `Key: value` lines, so a squash merge that adds its own trailers (such as `Co-authored-by:`) after a blank line reads like the branch commit, and `physa trailers` reports these findings.
2. **Map changes in the PR** pass `physa check map`.
3. **The reviewer agent** receives the diff, the PR title and body, the index, and the slices for the capabilities the trailers name. It returns structured output:
   - the capabilities the change actually touches;
   - the rules at stake, and whether each looks respected;
   - contradictions with existing decisions;
   - suggested map edits that this change itself makes true (for example, a rule this PR introduces).
4. **Miss and excess** are computed (§3.4).

Decided by Claude as arbiter (2026-11-20), for the owner to review: the reviewer runs only when a credential is available; in the Action, that is a `CLAUDE_CODE_OAUTH_TOKEN` or `ANTHROPIC_API_KEY` secret. Without one, the check run says the reviewer was skipped and why, and the deterministic results stand. Invalid reviewer output is a reported failure, never silently dropped. Misses and excess are null unless the reviewer ran.

Physa posts one comment, edited in place on every update, plus GitHub suggested changes for the map edits, which the author can accept with one click.

**Non-blocking by default** (Q5): the check reports a neutral status. A team can make specific findings blocking by policy, for example a violated rule on a capability its stakes call established.

### 3.3 After it merges

One agent session per merged PR reads what the review added:

- review comments that corrected the change become proposed rules, with the PR as anchor;
- choices argued in the discussion become proposed decisions, with the options rejected;
- a merged revert weakens what it reverted and proposes a `Rejected:` item or a question.

### 3.4 Measuring slices without a store in CI

CI has no access to a developer's local store, so the slice record travels with the work (Q6):

- When an agent loads a slice, the skill tells it to add a trailer listing what it loaded, comma-separated: `Loaded: delivery, queue`. Harnesses add it themselves.
- The PR check compares `Loaded:` with the capabilities the reviewer found touched:
  - **miss**: touched but not loaded;
  - **excess**: loaded but not touched.

### 3.5 How proposals reach the map (Q7)

- **While the PR is open**: as suggested changes on that PR. The author accepts or ignores them.
- **After merge**: learnings, decay proposals and over-budget merges are batched into one **map update PR** (`physa/learn`), refreshed as new proposals arrive. Stewards review it like any PR. It never merges itself.

### 3.6 Where the results are kept in the open-source core (Q8)

Results that feed strength (misses, excess, rules at stake, reverts) must not live in the repo. Without the hosted layer, Physa keeps them in GitHub itself:

- each PR check writes its structured results into the check run's output;
- `physa sync` reads them back through `gh` into the local store, where strength and decay are computed.

The hosted layer ingests the same results directly.

### 3.7 Locally, before pushing

`physa review` runs the same open-PR check on the current branch against the default branch, with the developer's own agent. No CI needed.

Decided by Claude as arbiter (2026-11-20), for the owner to review: `physa review` exits 1 when the reviewer fails, and prints the tokens it used when it ends.

## 4. Skills Physa ships

Each agent step uses a skill Physa ships and teams can replace:

| Skill | Used by |
|---|---|
| mapping | init, proposing capabilities |
| recovering | init, one capability at a time |
| learning | the after-merge step |

They use only glossary terms and return structured output that `physa check map` validates.

## 5. Settled with the owner (2026-11-19)

- **Q1** GitHub first, through `gh`; other hosts later.
- **Q2** Uncertain recovered knowledge becomes a question, never a rule with a confidence score.
- **Q3** Init's output arrives as a PR on `physa/init`.
- **Q4** Init has a token appetite with a circuit breaker, shown before it starts.
- **Q5** The PR check is non-blocking by default; teams make findings blocking by policy.
- **Q6** A `Loaded:` trailer records what each change's agent loaded.
- **Q7** Suggested changes while a PR is open; one batched map update PR after merge, which never merges itself.
- **Q8** In the open-source core, GitHub check runs keep the results and `physa sync` pulls them into the local store. Superseded by 07 (2026-11-20): checks and sync run on the developer's machine.
- **Q9** Init adds `.github/workflows/physa.yml`. Superseded by 07 (2026-11-20): there is no workflow to add.
