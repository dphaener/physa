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

> **Brought up to date on 2026-11-21, decided by Claude as arbiter for the owner to review.** Gather (§2.2), propose (§2.3), recover (§2.4) and check (§2.5) are built as `physa init --gather`, `--propose` and `--recover`, and a bare `physa init` runs them in order under one appetite (§2.7). `--write`, `--push`, `--pr` and `--hooks` put the map on `physa/init` and open its PR (§2.6), and `--answers` gives a new codebase's answers (§2.8). §2.6 holds the hook, and §2.8 drops the workflow, which 07 removed.

### 2.1 Phases

```
gather ──▶ propose capabilities ──▶ recover knowledge ──▶ check ──▶ review as a PR
(no agent)    (agent)                 (agent, per capability)  (no agent)   (a person)
```

### 2.2 Gather (deterministic, no agent)

Physa collects sources into the local store. Nothing is written to the repo yet.

| Source | What is kept | How |
|---|---|---|
| Existing agent and contributor docs | AGENTS.md, CLAUDE.md, CONTEXT.md, Cursor rules, CONTRIBUTING, README, any decision-record folders, and plan and overview files (PLAN, ROADMAP, OVERVIEW, ARCHITECTURE, CONTEXT, any case) directly under `docs/` | files in the repo |
| Git history | commit messages, trailers, reverts, which files change together | the version-control interface (07 §6) |
| PRs | titles, bodies, review comments (especially requested changes), linked issues | the forge interface (GitHub through `gh` first) (Q1) |

- **History is bounded** by default: the last 12 months or 1,000 PRs, whichever is smaller (the shorter window). Teams can widen it with `--since YYYY-MM-DD` (the date bound) and `--max-prs N` (the PR bound).
- **When the PR limit is reached first**, history starts at the merge time of the oldest gathered PR, so both sources cover the same window.
- **Without a forge**, only the date applies, and PRs are reported unavailable. That is not a failure.
- **Decision-record folders** are found by name or by shape (numbered markdown files). Files over 2 MB are skipped and listed in the summary; fitting the sources to an agent is the next phase's job (§2.3).
- **Files that change together** are used only as hints for proposing capabilities during init. File paths never enter the map.
- **`physa init --gather`** runs this phase alone. It writes only the store, replacing the previous gather in one transaction, and prints a summary as text or `--json`: counts by source, the bound used, and an estimated token count (bytes / 2.3, an estimate).

### 2.3 Propose capabilities (agent)

The customer's agent receives the gathered sources (summarised to fit), Physa's mapping skill, and the budgets from 01. It proposes:

- the product's promise for `product.md`;
- as many capabilities as the product really has (no fixed count, `measure-before-budget`), each a promise, each with the PRs or changes that best show it.

This is one agent session. It is the step that most shapes the map, so it is the step a person reviews most closely.

It is built as `physa init --propose`, which needs a stored gather and says so when there is none. The gathered sources are fitted deterministically to an input budget before the agent sees them, with no agent involved. The budget is a named constant, a first guess tuned from runs (`measure-before-budget`). Fitting estimates a token as 2.3 bytes, measured on #25's real prompts at 2.38 to 2.70 bytes a token, so the estimate errs high. It charges each item its own wrapping (labels and fences) as well as its text, and keeps a share of the PR budget for review comments, so long PR bodies cannot push them all out. Sources are cut in this order: docs (long ones cut by section, their headings kept), PRs (titles first, then bodies, then review comments, those requesting changes first), commit subjects, and finally clusters of files that change together, used as hints only. The summary says what was cut and how much. The proposal is one prompt-only session with no tools, under the appetite (§2.7), shown before it starts. Uncertain knowledge comes back as questions, never as guesses (Q2). `physa init --propose` prints the proposal as markdown or `--json` and stores it for the next phase. It writes nothing to the repo, and a new gather clears the stored proposal.

### 2.4 Recover knowledge (agent, one session per capability)

For each capability, a fresh agent session receives that capability's promise and the sources linked to it, and writes `capabilities/<name>.md`:

- **rules** from review corrections, docs and reverts, each with `Because:` and an anchor;
- **decisions** from PR discussions, with who, when and the options rejected;
- **terms** for the glossary;
- **questions** for anything uncertain (Q2).

Sessions are independent, so they can run in parallel.

**Uncertain knowledge becomes a question, never a guess.** If the sources suggest a rule but do not establish it, the agent writes a question ("Is it true that retries must never run in-process? Suggested by #1290.") instead of a rule. There are no confidence scores in the map.

Settled as built with `physa init --recover` (decided by Claude as arbiter for the owner to review):

- A session receives the product's promise, the capability's promise and its questions from the proposal, the recovering skill, and the capability's linked sources fitted to a 30,000-token input budget, with no clusters. It has no tools and runs in a fresh directory.
- Its reply is the capability file and its glossary terms. Physa writes the front matter, with the proposal's promise on one line, and adds a title when the session wrote none. Questions Physa records carry "raised by physa", the date and their anchors.
- A decision whose person or date the sources do not show becomes a question.
- At most three sessions run at once. Each has its session id assigned before it starts, so a retry resumes the same session (§2.5).
- The store keeps each recovered file and its terms until `physa init --write` writes them to the repo. A new gather or proposal clears them.

### 2.5 Check (deterministic)

`physa check map` (01 §8.3) validates every file. An agent's output that fails is sent back to the same session with the errors, up to two times. After that, the failing entries are dropped and listed in the review summary.

Settled as built:

- Each file is checked with the map check's rules on a scratch copy of the map, so it passes as it will be stored. Duplicate slugs and terms across capabilities are found in run order: the later capability drops them.
- A retry is the same session, resumed, with its problems named by the text of their line.
- Dropping repeats: after each drop the file is checked again, until it passes or only problems that dropping cannot reach remain. Those fail the capability, and its file is not stored.
- A glossary problem outside any term drops every term of that capability and keeps the file.
- An agent error on any attempt fails the capability, even on a retry.

### 2.6 Review as a PR (a person)

Init commits to a branch `physa/init` and opens a PR (Q3) containing exactly:

| File | Purpose |
|---|---|
| `.physa/README.md`, `product.md`, `glossary.md`, `capabilities/*.md` | the map |
| 2–3 lines in `AGENTS.md`, between markers | the pointer (01 §7) |

With `--hooks`, init installs the `Session:` hook after its commit and before its PR, the way `physa map hooks` does (01 §8, 08 §5). In a repo that ships no hook, that is `.git/hooks/prepare-commit-msg`, written when `core.hooksPath` is unset and no such hook exists, with `core.hooksPath` left alone so every hook already there keeps running. Otherwise init refuses and says how to add the hook by hand. The branch is kept and the PR still opens, its body naming the refusal, and the run exits 2. The hook is local, so it is not part of the PR.

The PR description summarises: capabilities proposed, entries recovered with their sources, questions raised, entries dropped, and the tokens spent. The person edits, deletes or merges it like any PR.

Settled as built with `physa init --write` (decided by Claude as arbiter for the owner to review):

- The owner's answers (2026-11-21): changes made before the map are exempt from the change check (07 §5), so init's commit carries no `Capability:` trailer. A bare `physa init` starts fresh, replacing the stored gather and proposal. `physa init --recover --write` continues from what is stored, recovering what is not yet done and writing the branch.
- The owner's answer on the hook: it is written to `.git/hooks` without touching `core.hooksPath`, else refused. No hook that is already there is ever disabled.

Calls made, for the owner to check:

- Propose gets half the appetite, at most its default (§2.7); recovery gets the rest, less what proposing used.
- A capability not yet recovered, or failed, is written with its promise only, and the PR body says which.
- A repo whose default branch already holds a map is refused, and `physa map learn` keeps it learning.
- An existing `physa/init` is replaced by one fresh commit. The push is leased to the fetched remote branch: a lost lease stops the run without a retry, and the local branch moves only after the push succeeds.
- The commit carries one `Session:` trailer for each session Physa started (the proposal and each recovery).
- A refused hook exits 2 with the branch kept (above).

### 2.7 Appetite for init

Init spends the customer's tokens, so it has an appetite (Q4): a token budget shown before it starts, with a circuit breaker. If recovery runs out, the capabilities already done are kept and the rest are listed as "not yet recovered", to be filled from future PRs. `--appetite` sets it. Propose's input budget (§2.3) leaves room in the appetite for the reply, and an appetite too small for any input is refused, naming the smallest that works.

Settled as built with `physa init --recover`:

- The default appetite is 300,000 tokens, shared by all of a run's sessions. Input and output both count.
- Before its first turn a session reserves its prompt plus 12,000 tokens for the reply. Before each retry it reserves what it has used so far, plus the retry's message and another 12,000. A reservation that cannot fit beside what is already spent is refused. One that waits only on other sessions' reservations waits for them to settle.
- A session that reports more tokens than its reservation is stopped by the circuit breaker.
- Bare `physa init`, `--propose` and `--recover` refuse a repo whose default branch already holds a map before gathering or running any agent, saying `physa map learn` keeps it current. When no default branch can be found they check the working tree for `.physa/product.md` instead, and `--write` reports the missing default branch as before; `--gather` alone still runs, since it spends no tokens and writes only the store.
- When the appetite ends a session, a capability with a checked answer keeps it, with its entries settled by dropping. A capability with no checked answer is "not yet recovered", as are those not started. A capability whose retries run out with no readable answer fails, with the read problem.

Settled as built with a bare `physa init` (decided by Claude as arbiter for the owner to review): the whole run spends one appetite, given by `--appetite` or the default of 300,000 tokens. Before any token is spent, the run prints its plan as one line, for example `physa init: appetite 300000 tokens: gather 0 (no agent); propose up to 150000 (its prompt about P plus R for the reply); recover the rest, about X, shared by its sessions; check and write 0.` The figures are estimates: the propose prompt is fitted as §2.3 describes, and the reply reserve is propose's own.

### 2.8 A new codebase

With nothing to recover, init asks the person a short round of questions (numbered, each with a suggested answer) to write the product's promise and first terms, creates `product.md` and `glossary.md`, and adds the pointer and, with `--hooks`, the hook. Capabilities arrive with the first PRs.

Built as the new-codebase path of a bare `physa init` (decided by Claude as arbiter for the owner to review):

- **Detection**: a gather is of a new codebase when it has no agent doc, contributing guide or decision record (a README alone does not count, since generators write one), no PRs, and at most five changes. Any other gather runs the phases instead.
- **The three questions**: (1) what the product is called, suggested from the repo's directory name; (2) what it promises its users in one sentence, suggested from the README's first sentence, else "<Name> lets its users …"; (3) its first terms, each written `Term: meaning` and separated by `;`, suggested as none.
- **`--answers FILE`**: one line per question, `1: <name>`, `2: <promise>` and `3: <terms>`. A missing number takes its suggestion. A name is one non-empty line, a promise is not empty and holds no `…`, and a term has the form `Name: meaning`. `none` or `none for now` means no terms. A line that fails names its number.
- **Terminal only**: without `--answers`, the questions are asked on a terminal, on stderr so that `--json` output stays one document. An empty line or `yes` accepts the suggestion, and a bad answer is asked again at most twice before the run fails. Without a terminal and without `--answers`, the run exits 2 and names `--answers`. An answers file is read even on a terminal.

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

Each agent step uses a skill Physa ships:

| Skill | Used by |
|---|---|
| mapping | init, proposing capabilities |
| recovering | init, one capability at a time |
| learning | the after-merge step |

They use only glossary terms and return structured output that `physa check map` validates. The skills are embedded in the binary, and teams can't replace them (decided by dphaener, 2026-11-21: "That steps on our core functionality that we own and could end up with loss of trust in the product due to users making bad decisions.").

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
