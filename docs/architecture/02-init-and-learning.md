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

> **Brought up to date on 2026-11-21, decided by Claude as arbiter for the owner to review.** Gather (§2.2), propose (§2.3), recover (§2.4) and check (§2.5) are built as `physa init --gather`, `--propose` and `--recover`, and a bare `physa init` runs them in order, never bounded, after showing its estimate and getting a yes (§2.7). `--write`, `--push`, `--pr` and `--hooks` put the map on `physa/init` and open its PR (§2.6), and `--answers` gives a new codebase's answers (§2.8). §2.6 holds the hook, and §2.8 drops the workflow, which 07 removed.

### 2.1 Phases

```
gather ──▶ propose capabilities ──▶ recover knowledge ──▶ check ──▶ review as a PR
(rules, then agent)  (agent)                 (agent, per capability)  (no agent)   (a person)
```

### 2.2 Gather (fixed rules, then one read-only agent)

Physa collects sources into the local store. Nothing is written to the repo yet. Gather has two parts: the deterministic finds, which are always kept, and discovery, one agent session that names more sources beside them.

| Source | What is kept | How |
|---|---|---|
| Existing agent and contributor docs | AGENTS.md, CLAUDE.md, CONTEXT.md, Cursor rules, CONTRIBUTING, README, any decision-record folders, and plan and overview files (PLAN, ROADMAP, OVERVIEW, ARCHITECTURE, CONTEXT, any case) directly under `docs/` | files in the repo, found by fixed rules |
| Named by discovery | the other docs the agent names, each with its kind and a one-line reason | one read-only agent session (below) |
| Git history | commit messages, trailers, reverts, which files change together | the version-control interface (07 §6) |
| PRs | titles, bodies, review comments (especially requested changes), linked issues | the forge interface (GitHub through `gh` first) (Q1) |

- **Deterministic finds are always kept.** The fixed rules run first and find the docs in the first row, each with a fixed reason. Discovery adds to them and never removes them.
- **Discovery is one agent session, read-only.** It runs in the repo with the Read, Glob and Grep tools only, so it can read and search but never write, run a command or reach the network. Its prompt says what to look for, never where: it is given no paths, no file list and no doc bodies, because finding where the repo keeps its prior art is its job (the owner, 2026-11-23: "The discovery prompt should tell the agent WHAT to look for, not where."). It names sources, each as a path, one of the five existing kinds (`agent`, `contributing`, `readme`, `decisions` or `overview`) and a one-line reason. The first design passed it up to 4,000 paths, and on a repo of 6,108 files that list alone was about 176,000 tokens and left out the docs it needed.
- **Guardrails.** Gather reads a named file only if it passes these checks, in order. A named file that fails one is listed under skipped with its reason, and is not read; only the text check reads a file before it refuses it:
  - `outside the repo`: the path leaves the repo, or its folder resolves through a symlink to outside it;
  - `under .git` or `under an ignored folder`: the path is under `.git`, under a folder the repo ignores, or a file git ignores;
  - `missing`: the file is not there;
  - `not a regular file`: a symlink, a folder or another special file;
  - `over 2 MB`: a file larger than 2 MB;
  - `unknown kind`: a kind that is not one of the five;
  - `a duplicate`: a file that a deterministic find or an earlier named source already holds;
  - `over the limit of 200 named sources`: a 201st source that would be accepted;
  - `not text`: a NUL byte or invalid UTF-8 in its first 8 KB.
- **No cap.** Discovery has no appetite and no circuit breaker, in a full run and with `--gather` alone: init is never bounded (10 Q6). The owner chose this for discovery too, though it spends before init shows its estimate and asks to go on (§2.7). What it spent is shown with the estimate, in weighted tokens.
- **Fallback.** When the agent is not installed or its session fails, gather keeps the deterministic finds and gives the reason in its summary. Init does not fail.
- **Mapped repos.** `physa init --gather` on a repo that already has a map runs only the deterministic finds and starts no agent. The summary says discovery was not started because the repo has a map (§2.7).
- **Flags.** `physa init --gather` accepts `--agent`, which names the agent that runs discovery.
- **History is bounded** by default: the last 12 months or 1,000 PRs, whichever is smaller (the shorter window). Teams can widen it with `--since YYYY-MM-DD` (the date bound) and `--max-prs N` (the PR bound).
- **When the PR limit is reached first**, history starts at the merge time of the oldest gathered PR, so both sources cover the same window.
- **Without a forge**, only the date applies, and PRs are reported unavailable. That is not a failure.
- **Decision-record folders** are found by name or by shape (numbered markdown files). Files over 2 MB are skipped and listed in the summary. Every source gathered reaches the later phases whole (§2.3).
- **Files that change together** are used only as hints for proposing capabilities during init. File paths never enter the map.
- **`physa init --gather`** runs this phase alone. It writes only the store, replacing the previous gather in one transaction, and prints a summary as text or `--json`: each source with its kind and reason, each skipped file with its why, counts by source, the bound used, discovery's outcome (sources named and added and the tokens spent, or why discovery did not run), and an estimated token count of the sources (bytes / 2.3, an estimate).

### 2.3 Propose capabilities (agent)

The customer's agent receives the gathered sources, whole, Physa's mapping skill, and the budgets from 01. It proposes:

- the product's promise for `product/index.md`;
- as many capabilities as the product really has (no fixed count, `measure-before-budget`), each a promise, each with the PRs or changes that best show it and the docs that hold its rules, decisions and terms. Physa keeps only docs it gathered, and drops any other path with its reason.

This is one agent session. It is the step that most shapes the map, so it is the step a person reviews most closely.

It is built as `physa init --propose`, which needs a stored gather and says so when there is none. Settled with the owner (2026-11-23): **nothing is cut.** In the owner's words: "This process is important, nothing should get cut. People might have to just bite the bullet on appetite." The session reads every gathered doc, PR title, PR body, review comment and change subject whole, with no input budget, no shares between sources and no cap on any item. What propose reads is a choice, not a cut: it reads each change's short id and subject, not its message, and the clusters of files that change together, as hints only. The gather is shaped into the prompt deterministically, with no agent involved: docs by kind, then path; PRs newest first; changes newest first; then the clusters. Tokens are estimated at 2.3 bytes a token, measured on #25's real prompts at 2.38 to 2.70 bytes a token, so the estimate errs high.

The proposal is one prompt-only session with no tools, with no appetite and no circuit breaker; a full run shows its prompt's estimate before it asks to go on (§2.7). Uncertain knowledge comes back as questions, never as guesses (Q2). `physa init --propose` prints the proposal as markdown or `--json` and stores it for the next phase; its "Read" section says what the session read: each source with its count and estimated tokens, and the prompt's estimated tokens. It writes nothing to the repo, and a new gather clears the stored proposal.

### 2.3.1 Propose in shares (settled 2026-11-24, built)

> **Designed by Myxo; the owner chose shares and settled the seven answers below (2026-11-24), each as recommended. This is built (`internal/shares`).** The owner chose to split propose after a large repo's propose prompt came to about 1.97M tokens, then, with its PRs read whole, about 1.6M: more than one session holds.

When the whole prompt fits in one session, propose runs as §2.3 says, unchanged. When it doesn't, propose reads its sources in **shares**, then merges what the shares proposed:

1. **Shares.** The sources are shaped as today (docs by kind, then path; PRs newest first; changes newest first; then clusters) and laid into shares in that order, each share's prompt within what one session holds less the reply reserve. Every source reaches exactly one share whole; no source is split or cut (`init-no-cut`). Each share's session gets the same skill and instructions as today's one session, and proposes the product's promise and capabilities from what it read, each with the PRs, changes and docs that show it.
2. **Merge.** One more session reads only the shares' proposals, never the sources: every promise, with its links and questions. It returns one list: the product's promise, and capabilities merged where two shares proposed the same promise, each keeping the union of its links. It may reword a promise; it may not add a link no share gave. Physa checks that, deterministically, as it checks a proposal today.
3. **What follows is unchanged.** The merged proposal is stored like one session's, and recovery reads each capability's linked sources (§2.4).

Nothing is cut, but no single session reads everything: a capability visible only across two shares' sources may come back as two, or as none. That is the trade the owner accepted in choosing shares over a shorter bound.

The estimate (§2.7) counts each share's prompt and the merge's; the "Read" section lists each share and what it read.

**No pull toward a count** (2026-11-25, proposed by Myxo for the owner to review). On a large repo, three shares that read different parts of the sources each proposed about twenty capabilities, and the merge returned about the same number, dropping areas only one share saw. Both skills had pulled toward fewer, larger capabilities. Now they size a capability by its `rules.md` alone: a capability is too large when its rules wouldn't fit in one `rules.md`, and too small when a change or two would exhaust it. The propose skill asks the agent to account for every area the sources show, and never to merge or leave out a candidate to shorten the list. The merge skill keeps a capability only one share proposed, unless another share's promise clearly contains it and the same changes keep both.

Settled with the owner (2026-11-24):

1. **Docs in every share.** When the docs take no more than half a session, every share also reads all of them, so each share sees the product's frame and only PRs and changes are divided; otherwise the docs are divided too. It costs a share's worth of docs per share.
2. **The adapter states the window, and a refusal halves the share.** The adapter states the model's window; when a session is refused as too long anyway, propose halves that share and tries again, so a wrong figure costs one refused call, never a failed run.
3. **A source larger than a share stops init.** Init stops before spending on propose, names the source and its size, and says how to leave it out of the gather. Cutting it would break `init-no-cut`, and leaving it out silently would too.
4. **A source that fits an empty share but not beside the docs divides the docs.** The owner's choice of the recommended answer: that run divides the docs too, and init stops only when a source cannot fit even an empty share.
5. **How to leave a source out.** The owner's choice of the recommended answer: the stop tells the person to leave the source out of the gather in ways that already exist: a doc by ignoring it in git, a PR or change by gathering again with `--since` after its date, or a smaller `--max-prs`. No new flag is added.
6. **Shares run one at a time.** The owner's choice of the recommended answer: a refusal halves the room for that share and every share not yet run, so a wrong figure costs one refused call.
7. **The window is 1,000,000 tokens.** The owner's choice of the recommended answer: the Claude Code adapter states a window of 1,000,000 tokens.

Settled as built (decided by Claude for the owner to review):

- A PR travels with its review comments as one source, so it is never split between shares.
- The merge prompt's estimate before shares run is its skill plus a reply reserve per share.
- A merge refused as too long fails rather than halving, since it reads no source.
- Drops list the merge's first, then each share's, each share's reason prefixed with its place.
- A run whose prompt fits one session is unchanged in prompt, output and stored proposal.
- A source ignored in git is skipped only when the agent's search found it; the fixed docs are read from the working tree, so the stop also says to move such a doc out of it.

### 2.4 Recover knowledge (agent, one session per capability)

For each capability, a fresh agent session receives that capability's promise and the sources linked to it: the PRs and changes propose named for it, those placed with it by the files they touch (below), the changes those revert or are reverted by, the docs propose linked to it, and the repo's agent instructions, readme and contributing guide, never the other docs. On nebari, giving every session every doc meant each read about 570,000 tokens; linking them brought recovery from 10.9M tokens to 6.1M with a map at least as rich (2026-11-23). Propose is told that a doc of kind `decisions` goes under every capability whose choices it records, usually every capability for a product-wide decision log (the owner's choice, 2026-11-23): in run 8 on nebari it linked its 423-entry `docs/decisions.md` to none, and recovered decisions fell from 72 to 28. The session writes one text for `capabilities/<name>/` (09 §7): front matter, title, optional overview, then its Rules, Decisions and Questions sections. Physa splits it into the folder's files:

- **rules** from review corrections, docs and reverts, each with `Because:` and an anchor;
- **decisions** from PR discussions, with who, when and the options rejected;
- **terms** for the glossary;
- **questions** for anything uncertain (Q2).

Sessions are independent, so they can run in parallel.

**Who decided, when an agent wrote the words.** Most PRs and changes in a repo built with agents are written by an agent and merged or committed by a person, so their author is not who wrote their words. A PR whose body, or a change whose message, carries an agent's mark (a "Generated with" footer, or a co-author trailer naming Claude, Codex, Cursor or Copilot) is marked `written by: an agent` in the session's sources. A decision drawn from it is `accepting a proposal` by whoever accepted it: the PR's merger, its author when the forge names no merger, or the change's author. `in their words` is kept for words the person wrote themselves (the owner, 2026-11-23, choosing "Merger, accepting a proposal"). Gather records each PR's merger and names each change's author by forge login where the forge can say, so a person is named the same way in every entry.

**Uncertain knowledge becomes a question, never a guess.** If the sources suggest a rule but do not establish it, the agent writes a question ("Is it true that retries must never run in-process? Suggested by #1290.") instead of a rule. There are no confidence scores in the map.

Settled as built with `physa init --recover` (decided by Claude as arbiter for the owner to review):

- A session receives the product's promise, the capability's promise and its questions from the proposal, the recovering skill, and the capability's linked sources whole: every doc, and each linked PR's title, body and review comments and each linked change's message, with no input budget and no cap on any item (§2.3, the owner, 2026-11-23). It reads no clusters. It has no tools and runs in a fresh directory.
- Its reply is the capability text and its glossary terms. Physa writes the front matter, with the proposal's promise on one line, and adds a title when the session wrote none. Questions Physa records carry "raised by physa", the date and their anchors.
- A decision whose person or date the sources do not show becomes a question.
- At most three sessions run at once. Each has its session id assigned before it starts, so a retry resumes the same session (§2.5).
- The store keeps each recovered file and its terms until `physa init --write` writes them to the repo. A new gather or proposal clears them.

**Every PR and change reaches a capability** (built 2026-11-25, proposed by Myxo for the owner to review). Propose cites only a few PRs and changes for each capability; on a large repo the citations reached about a quarter of the PRs gathered, and recovery read nothing else. After propose, `internal/coverage` places each PR and change no capability cites with its nearest capability: the one whose footprint holds most of its files, then most of its files' directories, a tie going to the capability proposed first. An item is placed only when at least half its files are in that footprint or its directories, the repo's root not counting as one, and a chore is never placed: a subject of type `chore`, `ci`, `build` or `style`, a dependency bot's `Bump x from y`, or files that are all dependency manifests and lockfiles or CI, formatter and linter configuration. What fails the bar reaches no capability; chores are counted apart and do not lower coverage. The owner, reviewing #8: forcing every PR to a capability "could get messy" and land capabilities that don't make sense (2026-11-25). A capability's footprint is the files its cited PRs and changes touch, with every cluster of files that change together that they reach, in full. A PR's files are those of the changes that name it in a squash subject (`Title (#12)`) or a merge subject (`Merge pull request #12`), so a PR no gathered change names has none and is placed nowhere. The placement is deterministic and computed again from the stored gather and proposal when recovery runs, over every capability proposed, so `--recover --only` reads what a full run would. Each item goes to one capability, so recovery's sources together come to about the size of the gather, read once. Before, recovery read only what was cited, so ratios recorded by earlier runs (§2.7) are lower than a run reads now, and estimate low until newer runs are recorded.

**Coverage.** After propose, init prints the proposal's coverage beside it, and under `--json` as `coverage`: how many gathered PRs the capabilities cite, how many more were placed by their files, how many reach none and why, and the clusters no capability's citations touch, largest first. More than 20% of the PRs or of the clusters reaching none is low coverage (a first guess, to be tuned from real runs), and init then warns on stderr and goes on. Proposing again on its own, with the uncovered clusters named in the prompt, and asking on a terminal before recovering are the other options; warning was chosen because a second propose costs a share of the run and may not do better, and a person can rerun `physa init --propose` when the clusters show a real gap.

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
| `.physa/README.md`, `product/`, `glossary.md`, `capabilities/<name>/` | the map |
| 2–3 lines in `AGENTS.md`, between markers | the pointer (01 §7) |

With `--hooks`, init installs the `Session:` hook after its commit and before its PR, the way `physa map hooks` does (01 §8, 08 §5). In a repo that ships no hook, that is `.git/hooks/prepare-commit-msg`, written when `core.hooksPath` is unset and no such hook exists, with `core.hooksPath` left alone so every hook already there keeps running. Otherwise init refuses and says how to add the hook by hand. The branch is kept and the PR still opens, its body naming the refusal, and the run exits 2. The hook is local, so it is not part of the PR.

The PR description summarises: capabilities proposed, entries recovered with their sources, questions raised, entries dropped, and the tokens spent. The person edits, deletes or merges it like any PR.

Settled as built with `physa init --write` (decided by Claude as arbiter for the owner to review):

- The owner's answers (2026-11-21): changes made before the map are exempt from the change check (07 §5), so init's commit carries no `Capability:` trailer. A bare `physa init` starts fresh, replacing the stored gather and proposal. `physa init --recover --write` continues from what is stored, recovering what is not yet done and writing the branch.
- The owner's answer on the hook: it is written to `.git/hooks` without touching `core.hooksPath`, else refused. No hook that is already there is ever disabled.

Calls made, for the owner to check:

- A capability not yet recovered, or failed, is written with its promise only, and the PR body says which.
- A repo whose default branch already holds a map is refused, and `physa map learn` keeps it learning.
- An existing `physa/init` is replaced by one fresh commit. The push is leased to the fetched remote branch: a lost lease stops the run without a retry, and the local branch moves only after the push succeeds.
- The commit carries one `Session:` trailer for each session Physa started (the proposal and each recovery).
- A refused hook exits 2 with the branch kept (above).

### 2.7 Init's cost, up front

Settled with the owner on 2026-11-23 (10 Q6, Q10, Q10a), replacing Q4 for init. The owner: "I think init is a special case. We should be up front about it but I don't think it should be bounded at all or we won't get good results." Init has no appetite and no circuit breaker, in a full run and in `--gather`, `--propose` and `--recover` alone, and there is no `--appetite`. No source is cut either (§2.3). Instead init is up front about what it will cost:

- **What a token is worth.** Before any token is spent, a bare run says it is never bounded and what a token of its spend is worth: an input token of the agent's default model at its cached input price (10 Q5), or a raw token, with no cost shown, while no price is known (10 Q9). For example `physa init: no appetite and no circuit breaker: init is never bounded, and is up front about its cost instead: once it has gathered it estimates the rest of the run and asks before going on. Spend is shown in tokens weighted by price, each worth an input token of claude-opus-5-5 ($4.00 a million), with the cost estimate beside it.`
- **The estimate.** Once gather and discovery have run, init estimates the rest of the run in weighted tokens, as a range measured from past runs, never assumed: propose's prompt and its reply reserve of 16,000, which are known, times propose's weighted-to-raw factor, plus the gathered sources' estimated tokens times recovery's weighted-to-sources ratio. Both are measured because a one-turn session writes its whole prompt to Claude Code's 1-hour cache at twice input (10 §3). Each bare run that finishes recovering, with prices known, records both in the machine's spend cache: recovery's weighted tokens ÷ the sources' estimated tokens, and propose's weighted ÷ raw tokens. With none recorded, the ratio is nebari's 12.5 from run 9, with a range of ×0.5 to ×1.5, and propose's factor is 2.05; with one, that run's own ratio, ×0.5 to ×1.5, and its factor (proposed by Myxo on 2026-11-25 for the owner to review, after a large repo's run spent 3 to 8 times under an estimate drawn from nebari's ratio); with two or more each runs from the smallest recorded to the largest. The range is shown in weighted tokens with the cost estimate beside it (10 Q7), or with no cost when no price is known: `physa init: discovery spent 74k tokens (about $0.30). The rest of the run should spend about 3.2M to 9.1M tokens (about $13 to $36): propose's prompt of about 100k tokens plus 16k for its reply, at 2.05 times as weighed, and recovering at 6.25 to 18.75 times the sources' 470k tokens (nebari's measured ratio of 12.5, until a run is recorded on this machine). An estimate, not a limit.` When propose runs in shares (§2.3.1), the estimate counts every share's prompt and the merge's, each with its reply reserve. For run 9's figures (a prompt and reply of 682,510 and sources of 998k) that is about 7.6M to 20.1M, which holds the 14M it spent.
- **The yes.** On a terminal init then asks `Go on to propose and recover, about 3.2M to 9.1M tokens (about $13 to $36)?` A no stops before proposing, with nothing spent on proposing or recovering and the gather stored, and exits 1. Off a terminal it stops there too, names the estimate and says to rerun with `--yes`, which goes on without asking: `physa init: stopped before proposing: the rest of the run is estimated at about … tokens, and with no terminal to ask on, init goes on only when told to. The gather is stored; rerun with --yes to go on without asking: physa init --write --yes`. The person sees the cost before the large spend, at the price of stopping a run nobody is watching (10 Q10).
- **Past the estimate.** While it runs, when what proposing and recovering have spent passes the top of the range, init says so once and goes on: `physa init: past the estimate: proposing and recovering have spent 9.8M tokens (about $39), more than its top of 9.1M tokens (about $36); init is never bounded, so it goes on.`
- **Warnings before the spend** (built 2026-11-25, proposed by Myxo for the owner to review). Before the agent's first session, init says what in the machine's setup may fail its sessions or bill someone unexpected: for Claude Code, an `ANTHROPIC_API_KEY` in the environment, which claude uses over the person's login (`physa init: warning: ANTHROPIC_API_KEY is set, so claude uses that key instead of your login, …`). A session that then fails with claude's "Invalid API key" names the variable in its error. Once gathered, and before the estimate's question, init warns when the gather is thin: a shallow clone, or no PRs for want of the forge (`gh auth login`). It warns and goes on; refusing is the other option, set aside because a person may want the thin map anyway.
- **What it spent.** After recovering, a bare run prints `physa init: this run spent … tokens (about $…).`, and the PR body's "Tokens spent" table keeps the raw input, cached input and output columns and adds weighted tokens and the cost estimate for discovery, propose and recover and in total, which is the same figure.

Settled as built (decided by Claude for the owner to review):

- Recovery still runs at most three sessions at once (§2.4); nothing is reserved for them. A run that is interrupted keeps the capabilities already recovered and lists the rest as "not yet recovered", and `physa init --recover` continues.
- Only a bare run records the ratio: `--recover` alone may recover some capabilities only. `--gather`, `--propose` and `--recover` alone show no estimate and ask nothing.
- The table's weighted tokens and cost come from what the run's meter counted per session, from the adapter's real dollars, never from re-weighing stored tokens: Claude Code writes its cache with a 1-hour lifetime at twice input, so re-weighing them as plain input had shown about half the cost. The store keeps each phase's count (`init_spend`), adding each run of a phase to it, so a later `--recover --write` shows what was really spent; a new gather clears them all and a new proposal clears propose's and recover's.
- The spend cache is `spend.json` in Physa's settings directory, beside the person setting.
- Bare `physa init`, `--propose` and `--recover` refuse a repo whose default branch already holds a map before gathering or running any agent, saying `physa map learn` keeps it current. When no default branch can be found they check the working tree for `.physa/product.md` instead, and `--write` reports the missing default branch as before; `--gather` alone still runs: its only spend is discovery's, which a mapped repo never starts, and it writes only the store.

### 2.8 A new codebase

With nothing to recover, init asks the person a short round of questions (numbered, each with a suggested answer) to write the product's promise and first terms, creates `product/` and `glossary.md`, and adds the pointer and, with `--hooks`, the hook. Capabilities arrive with the first PRs.

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
- **Q4** Init has a token appetite with a circuit breaker, shown before it starts. Replaced on 2026-11-23 by 10 Q6: init is never bounded, and shows its estimate and asks before its large spend (§2.7).
- **Q5** The PR check is non-blocking by default; teams make findings blocking by policy.
- **Q6** A `Loaded:` trailer records what each change's agent loaded.
- **Q7** Suggested changes while a PR is open; one batched map update PR after merge, which never merges itself.
- **Q8** In the open-source core, GitHub check runs keep the results and `physa sync` pulls them into the local store. Superseded by 07 (2026-11-20): checks and sync run on the developer's machine.
- **Q9** Init adds `.github/workflows/physa.yml`. Superseded by 07 (2026-11-20): there is no workflow to add.
