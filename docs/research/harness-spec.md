# The owner's agent harness: the spec to port it

Status: a read-only survey, 2026-11-19. It describes the harness as it runs today in Rhiza (`ship-change` conductor default plus the plan workflows). The aim is to lift it out as the first pluggable harness of a generic engine.

Sources (all read for this survey):
- Role skills: `~/.claude/skills/rhiza/skills/rhiza-*/SKILL.md`. These are installed copies of `/home/rhiza/code/rhiza/internal/guide/text/*.md`, with the `shared/*` blocks expanded (`internal/api/guide_install.go`).
- Workflows: `/home/rhiza/code/rhiza/internal/workflow/builtin/{ship-change,ship-change-classic,plan,plan-milestone,plan-review,build,review}.toml`
- Engine code: `internal/api/{run_spawn,run_review,run_checks,run_build,session_done,session_done_check,land_commit,run_handcommit}.go`, `internal/run/journal.go`, `internal/adapter/adapter.go`, `internal/adapter/claudecode/claudecode.go`
- Decisions in `.rhiza/rendered/decision/`: `conductor-milestone-direction`, `afk-prompts-shipped-default`, `conductor-three-checks`, `conductor-spawned-sessions`, `conductor-plans-leaves`, `conductor-questions-to-arbiter`, `conductor-land-commit-trailers`, `engine-is-conductor` (superseded in practice), `shipit-as-agent-step`, `byo-levels-skill-conductor-workflow`
- Sources: `conductor-workflow-interview`, `efficient-build-cycle-interview`, `founding-conversation`
- Orchestrator: `~/orchestrator/{conductor-rules,machine-rules,hands-off-answers,HANDOFF}.md` and `~/orchestrator/perf-2026-11-12/prompt-gap.md`, the role-by-role comparison of afk and Rhiza
- afk source, cloned read-only earlier at `/home/rhiza/tmp/claude-1000/-home-rhiza-code-rhiza/823cd4a5-db47-4770-9e31-1f8134228fec/scratchpad/afk-src`: `docs/PLAN.md` §1, §2, §5.5 and §5.7, `plugins/afk/skills/*`, `plugins/afk-opencode/PROVENANCE.md`

No afk, forge or shipit plugin is installed under `~/.claude`. The only installed harness is the `rhiza` skills plugin.

---

## 1. Lineage: forge and shipit → afk → Rhiza

The owner says "afk → forge → Rhiza". The evidence shows two parents merged into afk, and afk's prompts then imported into Rhiza.

| Stage | What it was | What it gave the harness (evidence) |
|---|---|---|
| **forge** (`~/code/forge-cli` plus the claude-marche `plugins/forge` Claude plugin, upstream SHA `bceffe7b`) | A Go binary: detached PTY hosts per session, a Bubble Tea dashboard, a worktree per session, three agent providers and an autonomous mode. Its plugin's workflow scripts (`execute.js`, `review.js`) scheduled tasks by dependency. | **The workflow skeleton**: a conductor, then the planner (a "grill" interview with codebase research and the Crucible plan critique), then the orchestrator (fresh sub-agents per task using the six-section prompt: TASK / EXPECTED OUTCOME / REQUIRED TOOLS / MUST DO / MUST NOT DO / CONTEXT), then a tiered verifier, then independent review checks with an adversarial critic, then fix-rejections. It also gave the "disk is the only state channel" rule. afk PLAN §2: "forge's Go code and forge's plugin are the base." In the owner's words (afk PLAN §11): "The owner has run forge's autonomous mode many times and it works well." |
| **shipit** (`flow` marketplace plugin v2.0.2, `/shipit:go` per task) | A Claude Code plugin that built 21 nebari tasks "with no task re-planned or restarted". | **QA scenarios with evidence**: every task has happy and failure scenarios. **The hands-on QA check** ("F3": run the app, build to scratch, exercise the change). afk T0.2 and T0.3 grafted these onto forge. shipit's Claude-only mods (write-guard, continuation enforcer) were not carried over; afk enforces them from its daemon. Rhiza milestones 1–2 were built with shipit as the agent step (Decision `shipit-as-agent-step`). |
| **afk** (`github.com/dphaener/afk`; its plugin `plugins/afk` "merged the shipit and forge prompts") | Runs unattended on a VM. A daemon ships each task (push, PR, CI, merge, deploy check). A usage governor. An inbox for owner-only items. | **The lean cycle (PLAN §5.7)**, each rule tied to a measurement: no sleeping or polling and no command over about 4 minutes; three checks, not four (scope folded into compliance); a critic only on blocker or major; re-review only the failed check; a tiered verifier; no agent does a script's job; tasks carry exact files; research is opt-in in autonomous mode. **The autonomous policy (§5.5)**: never wait on a rubber stamp; take the recommended option and record it as assumed; park on anything irreversible; fix every confirmed blocker or major. **The shared severity block** (`<!-- severity-defs -->`, the same bytes in every prompt). **Critic downgrade.** **UNFIXED carried, never dropped.** **Verbatim retry.** **Notepads**, which became run notes. **Per-agent tool allow-lists.** |
| **Rhiza** (first named Genesis; founding conversation 2026-10-23: "I already have something I've built called afk … build on those principles but start something new") | A product model (`.rhiza/`), change sets and leaves, a durable daemon and Rex panes. | It kept afk's role content in Rhiza's own words. Milestone 35 (Decision `afk-prompts-shipped-default`, 2026-11-12): "Those should ship as the default build tools, and we also want to use them as our own." Rhiza added: **the model is the plan** (leaves with acceptance, files and must_not, Decisions with provenance, Sources); **a deterministic build-check** (Evidence written by a non-author actor); **commit trailers**; **the model check and overlaps**; **plan review on every plan** (reviewer, critic, reviser); **the arbiter and advisor**; **ship fixers** (ci-fix, gate-fix, conflict-fix). Milestone 36 (Decision `conductor-milestone-direction`, 2026-11-13) replaced the ~56-step daemon workflow (now `ship-change-classic.toml`, 497 lines) with **one conductor agent per change set** over "a dumb, durable daemon". |

One note on how direction changed. Milestone 3's Decision `engine-is-conductor` said "no LLM conductor; deterministic engine". Milestone 36 reversed that, after the measurements (prompt-gap.md §7) showed the daemon-scripted cycle was slower and needed more rework than afk's agent-conducted cycle. The figures: median 121m against 39m per shipped unit; build rework 31% against 12%; review rework 58% against 7%. The owner, on 2026-11-13: "in afk, the majority of these things were handled by agents … the daemon should be dumb but durable … determinism means durable decisions and deterministic gates, not scripting each agent judgment."

---

## 2. The workflow end to end

### 2.0 Shape

```
plan (planner) ─► plan review loop (reviewer → critic → reviser, ≤3 rounds) ─► gate: accept
   │                       [plan.toml / plan-milestone.toml; or conductor phase 1 if no leaves]
   ▼
conduct (ONE conductor session per change set; spawns every other role as a visible session)
   ├─ per leaf: builder ─► verifier ─► (fail) builder retry with verbatim findings ─► verifier …
   ├─ checks: check-plan ∥ check-code ∥ check-qa  (one "round" each, per cycle)
   ├─ critic per check, only if that check's round has a blocker/major
   ├─ fixer for standing confirmed findings ─► checks again
   ├─ gate it must see pass: rhiza run review-check
   └─ rhiza land --commit ; rhiza session done
   ▼
engine gate (deterministic commands): build-check, review-check, rhiza check, ship gate
   ▼ signoff (owner or auto) ─► push/PR ─► wait CI ─► pr-green ─► merge ─► main CI ─► close-out ─► ship ─► deploy-check
   (any red before merge → hand-back to the conductor with handback.md, ≤ handback_cap=3)
```

### 2.1 Plan phase (planner): `rhiza-planner`, `rhiza-plan`, plan-recipes, plan-interview

- **Job.** Plan one change set (or a milestone) straight into the model. There is no plan file, no task list and no checkboxes ("Keep a plan file … outside the model" is on the never-list).
- **Inputs.** The change set ID (`RHIZA_CHANGE`), `rhiza change show`, `rhiza slice`, `rhiza why`, the code, and the owner when present.
- **Steps.**
  1. A research pass before any interview or decision. Read the slice and the intent's nodes. Search the code for the names the intent uses. Find the patterns a builder would follow and the files each leaf touches.
  2. Interview the owner. Never ask a fact the code or model answers. One topic per message, each with a recommendation. Unsettled points become questions linked `blocks`.
  3. Scrub the interview into a Source with `rhiza-scrub` (summary, the owner's verbatim typed words, no secrets).
  4. Write the leaves with `rhiza apply`.
  5. Self-check: re-read the slice, run `rhiza check`, fix what they find.
- **Output: the leaf contract.** Each leaf is a capability node with:
  - `statement`: one sentence.
  - `acceptance`: runnable, scoped to its own tests (never the full suite), and holding at least one point marked with the words `failure case`, or else a `failure_waiver` with a reason.
  - `files`: the paths it touches; line ranges are allowed.
  - `must_not`.
  - A `decided-by` link to a Decision. Each Decision has provenance: a `quote` of the owner's typed words, or `wording` (for an agent's option the owner picked, or `(assumed, would have escalated)`), or a `derived-from` Source.
  - Sizing: one concern, small enough for one fresh session.
- **Provenance rules.** Never put words in the owner's mouth: a picked option is never a `quote`. The engine captures AskUserQuestion picks from the transcript itself (`Adapter.Answers`).
- **Autonomous** (`RHIZA_AUTONOMOUS=1`). AskUserQuestion is denied by the PreToolUse hook and becomes a question node. The arbiter answers it. The planner waits at the prompt for the answer message.

### 2.2 Plan review: `plan.toml` steps review → has-findings → critic → fresh → check → revise / rulings

| Role | Job | Records | Must not |
|---|---|---|---|
| **plan-reviewer** (fresh; told only the change set ID) | Records one finding per problem, in 8 kinds: `missing-acceptance`, `missing-provenance`, `unreal-depends-on`, `outside-intent`, `contradiction` (including against the code and against in-flight change sets via `rhiza overlaps`), `uncovered-intent`, `oversized-leaf`, `no-failure-case`. Never records what `rhiza run review-check` or `rhiza check` already checks deterministically. | `rhiza run findings - --json` → `{"findings":[{"node","kind","reason"}]}`, numbered f1…; `{"findings":[]}` when clean | Change anything |
| **plan-critic** | Re-checks each finding against the model and the code. Gives `confirmed`, `downgraded` (with a lower severity) or `dropped`, each with a reason. | `rhiza run verdict - --json` → `{"verdicts":[{"finding","verdict","severity?","reason"}]}` | Add a finding, raise a severity, rewrite a finding, change anything |
| **plan-reviser** | Fixes only the findings that are confirmed and major, in the model (split an oversized leaf, add a leaf for an uncovered clause, …). Never invents owner words. | `rhiza apply` (or `--record` for a landed node), then `rhiza check` | Touch anything a finding does not name |

- **Severity by kind.** `missing-acceptance`, `contradiction`, `outside-intent`, `uncovered-intent`, `oversized-leaf` and `no-failure-case` are major; the rest are minor. Minors are never fixed.
- **Loop limits.** Review, has-findings, critic, fresh and check each have `max_visits=3`; revise has `max_visits=2`; any failure sets `retry_from = "interview"`. `review-check --fresh` sends a stale round back to a fresh review (never straight to revise).
- **From round 3.** A confirmed major becomes a question node. Autonomously, the arbiter decides it as a Decision. `review-check --rulings` then needs each ruling applied (a node linked `decided-by` it), with one `apply-rulings` visit allowed, before the gate.
- **Gate.** `rhiza run gate` proposes the change sets. The owner accepts them, or the gate accepts them under `autonomy.accept=auto`. Then the plan PR goes through the ship steps.
- **Conductor variant.** A change set with no leaves gets them from the conductor's first phase, under the autonomous rules (Decision `conductor-plans-leaves`). No separate plan review runs there; the deterministic leaf-contract check and the later checks cover it.

### 2.3 Conduct: `rhiza-conduct`, conduct-roles, conduct-handback

- **Job.** One session per change set (`RHIZA_CHANGE`, `RHIZA_RUN`). It plans missing leaves, then spawns every other role. It never builds, checks or fixes anything itself. It never merges or pushes, never edits `.rhiza/log`, and never passes its own gate.
- **Spawn.** `rhiza run spawn --role <role> --brief <file> [--leaf <l>] [--check <c>] [--model <m>] --no-wait`.
  - The conductor writes each child's brief: "the leaf, the one thing to do, nothing else".
  - The engine writes the child's `brief.md` as the contract section followed by that brief. Only builders and verifiers also get the run notes.
  - After spawning, the conductor ends its turn with no sleep and no poll.
  - When a child ends, the engine wakes the conductor with a message: `Your spawned child session <s> ended.` plus the role, leaf, outcome, detail and a pointer to `rhiza run status --json`.
- **Phase order.** Plan when there are no leaves → for each leaf, builder then verifier (retry on findings) → the three checks together → a critic per check with a blocker or major → a fixer → the checks again → `rhiza run review-check` must pass → `rhiza land --commit` (lands, renders and commits `.rhiza` with `Change:` and `Actor:` trailers) → `rhiza session done`.
- **Questions.** A question from the conductor, or from any child it spawned, goes to the arbiter whatever the run's autonomy setting (Decision `conductor-questions-to-arbiter`). Owner sign-off is still governed by `autonomy.signoff`.
- **Hand-back.** A red engine step re-enters the conductor with `handback.md`. Its sections: Failing step, Outcome, `## Failing commands` (with kept-output paths), `## Output tail`, `## Cause`, `## Failed CI runs`, `## Conflicted files`. The conductor routes it as follows:

| Failing thing | Role spawned |
|---|---|
| `rhiza run build-check` | A verifier for each leaf without independent Evidence; a builder retry if one fails |
| `rhiza run review-check` | A critic, then a fixer, then the checks again |
| `rhiza check` | A fixer, with the output quoted as its findings |
| `rhiza ship gate`, the pre-push check, or the gate on a merged head | gate-fix |
| signoff `changes` | A fixer, with the owner's requested changes as findings, then the checks |
| settled red CI at pr-green | ci-fix |
| An unresolved merge of main | conflict-fix |
| wait-ci timeout, or a git/gh error in rebase | Nothing: change nothing and finish |

- **Cap.** `handback_cap=3`. The next red after that parks the run, stopped, with an inbox item. Steps after the merge never hand back.

### 2.4 Builder: `rhiza-builder` (plus `rhiza-build-retry` when `RHIZA_ATTEMPT>1`)

- **Inputs.** `RHIZA_CHANGE`, `RHIZA_LEAF`, `RHIZA_ATTEMPT`; `rhiza slice`, `rhiza show <leaf>`; the existing code and tests "that follow the same pattern", read first.
- **The contract.** "The leaf's `acceptance` is the contract: when it holds, you are done." Raise any owner question **before** starting, not midway.
- **Pre-handoff checklist.** Run the acceptance commands, `rhiza ship generate` (the project's generator) and `rhiza render`, and fold in what they change.
- **Output.** Exactly **one** new commit, shaped like this:
  ```
  <subject>

  Checks:
  - <acceptance command>: exit 0
  - rhiza ship generate: exit 0
  - rhiza render: exit 0

  Change: <change set ID>
  Actor: <actor handle>
  Leaf: <leaf handle>
  Co-Authored-By: …
  ```
  All trailers sit in the one final paragraph. The commit also stages the session's own untracked actor record from `.rhiza/log`, never another actor's.
- **Other duties.**
  - Keep the model in step: if the leaf changes what a landed node says, `rhiza set` it in the change set, or raise a question.
  - Before done, write one line of learning: `rhiza run note "<learning>"`.
  - Outside work that is **needed** becomes a question linked `blocks` to the leaf, and the builder stops. Outside work that is only **noticed** becomes a `discovered-from` draft change set and is never built in this run.
- **Retry.** Inspect the working tree and the earlier attempts' commits first. Read the previous findings (quoted verbatim in the brief). Address every finding exactly as written. Add one fix commit with the same `Leaf:` trailer. Never amend, rebase or force.

### 2.5 Verifier: `rhiza-verifier`

- **Isolation.** The verifier never wrote the code. The build-check enforces that by actor. The text says "a detached checkout at the leaf's commit" (see the caveat in §3.4). It never edits code.
- **Job.**
  1. Re-run **every** command in the commit's `Checks:` block and every acceptance command, as written. A different exit code, or a missing `Checks:` block, is a finding.
  2. Read the diff. Confirm each acceptance point is met **and tested**, nothing outside the leaf changed, and there are no stubs or TODOs.
  3. Tests use fakes only, never real agents, Rex or `gh`.
- **Output: exactly one of two.**
  - Evidence written as itself: `rhiza apply` with `{op:add, kind:evidence, handle:<leaf>-verified, fields:[statement=…, result=PASS]}` and a `verifies` link to the leaf.
  - Findings: `rhiza run findings --leaf <leaf> -` with `{"findings":[{"reason":"<file, command or acceptance point: what failed and what to change>"}]}`.
- **Guard.** `session done` is refused while the attempt has neither its own Evidence nor findings (`session_done.go: verifierRecordMissing`). Evidence written by another actor does not count.

### 2.6 Checks (the conductor's three): `rhiza-check-plan`, `rhiza-check-code`, `rhiza-check-qa`

**The isolation clause, word for word in all three:** "You know nothing of how the work was built. Never run `rhiza run status` or rhiza run show (or tail) on the build run, and never read its findings, its run notes or its session transcripts. You judge only the contract from the slice, the diff from the merge-base and the model's Evidence."

Every check reads `rhiza slice --change $RHIZA_CHANGE --json` and `git diff $(git merge-base origin/main HEAD)`.

| Check | Judges | Grading | Must not |
|---|---|---|---|
| **plan** (compliance + scope + model) | Each leaf's acceptance, point by point; runs the tests it names. Then the diff against the intent, the constraints and the Decisions. | Leaf with nothing in the diff: **blocker**. Acceptance point unmet or partly met: **major**. Scope creep: **major**. Unaccounted file: **major** if it changes behaviour, else **minor**. Code contradicts a leaf, Decision or constraint: **major**. A rule with no constraint stating it: **minor**. | Judge style or tests; edit code or model |
| **code** (quality) | Finds the test commands in the contributor guide (never guesses them). Maps each changed file to its direct, integration and downstream tests and runs them scoped; never silently falls back to the full suite. Then a 7-item defect checklist: swallowed errors, debug output, commented-out code, unused imports, TODOs or stubs, generic names, missing error handling. | Failing build or test: **blocker**. Defect: **major**. Convention: **minor**. | Judge acceptance; edit code |
| **qa** (hands-on) | Starts clean. Builds fresh to a scratch path **outside `$PATH`**, never installs. Runs each leaf's happy and failure scenarios with the **built program**. Then bounded exploration: pairs of commands that feed each other, then edge cases. Writes no new test files. Reports only broken behaviour. | Breakage: **blocker**. Rough edge: **minor**. | Write tests; edit code |

- **Output: one round.**
  ```
  rhiza run findings --check "$RHIZA_CHECK" - <<'EOF'
  {"findings":[{"severity":"major","leaf":"<leaf>","place":"path:line","reason":"…"}],
   "evidence":[{"leaf":"<leaf>","line":"<what was run or read for one point, with the result>"}]}
  EOF
  ```
  - There is one evidence line per acceptance point of every leaf.
  - In a conductor step the engine **refuses a round that leaves any leaf uncovered** (no finding and no evidence line for it).
  - QA also writes model Evidence `<leaf>-qa-<cycle>` with `verifies`.
- **Guard.** A check's `session done` is refused until its round is journaled.
- **Classic workflows** (`review.toml`, `ship-change-classic.toml`) run four checks instead: compliance, quality, qa and model. Each runs in a throwaway detached checkout. Fixes go one leaf at a time, each followed by that leaf's verifier. At most 2 fix cycles; what still fails becomes questions.

### 2.7 Critic: `rhiza-critic` (also `rhiza-plan-critic`)

- **When.** Only for a check whose latest round has a blocker or major (Decision `conductor-three-checks`).
- **What it reads.** Its own check's last round, and nothing else of the run, plus the contract and the diff.
- **Verdicts.** For each blocker and major: `confirmed` (real, right severity); `downgraded` (real, but give the lower `minor` or `nit` and why); `dropped` (not real: the contract doesn't ask for it, the code is right, or the finding misreads the diff). Each reason names the leaf, the acceptance point or `path:line`. "When unsure, confirm: only drop or downgrade what you can show."
- **Must not.** Never add a finding, raise a severity or rewrite one. Minors and nits get no verdict.
- **Output.** `rhiza run verdict --check "$RHIZA_CHECK" -` with `{"verdicts":[{"finding":"f1","verdict":"confirmed|downgraded|dropped","severity?":"minor","reason":"…"}]}`.
- **Guard.** `session done` is refused until every serious finding of the round has a verdict.
- **What stays open** (`run.Round.Standing`). A blocker or major stays open when it has **no verdict**, or a verdict other than dropped or downgraded-below-major. An unjudged finding counts as confirmed.

### 2.8 Fixer: `rhiza-fixer` (+ ci-fix, gate-fix, conflict-fix)

- **Fixer.** Fixes **only** the findings in its prompt, each exactly as written and in place.
  - Reproduce each finding first with the scoped command and see it fail.
  - Commit only once those commands and the touched packages pass.
  - Run `rhiza ship generate` and `rhiza render`, then make one commit. It has an `Unfixed:` block (`- f3: <why>`) and a `Checks:` block, and its trailers are `Change:`, `Actor:`, and **either** `Leaf:` **or** `Review: <run>`.
  - A finding it cannot fix is carried as unfixed, never dropped.
  - Never push, rebase, force or use `--no-verify`.
  - Lint-fix variant: works from `rhiza run lint-check --json` hits, using `rhiza set` or by retiring capabilities.
- **ci-fix.** Reads `rhiza ship check --json` (failing tests and the recognised non-test causes). Reproduces locally and never pushes to see.
  - A flake is fixed, or quarantined (skipped) with a bug filed by `rhiza request --kind bug` and named in a comment at the skip.
  - One commit trailed `Review:`.
  - At most 2 visits: a third red head fails the run.
- **gate-fix.** Reads the failed gate's kept output and its `Full log:` from `rhiza run status --json`. Never reruns the whole gate; reproduces with the scoped command. Same flake rule, same `Review:` commit, at most 2 visits.
- **conflict-fix.**
  - Code conflict (a merge left in progress): resolve so both sides hold, regenerate, render, stage `.rhiza/rendered` and `.rhiza/log`, and commit the merge with `Checks:` and `Review:`.
  - Model conflict (the land was refused): `rhiza set <node> --rebase`, then a `Model-Conflict: <other cs>` trailer.
  - A real clash: commit nothing and ask **one** blocking question naming both change sets.

### 2.9 Arbiter and advisor (owner proxies)

- **Arbiter.** A fresh session per question. It reads the question, `slice` and `why`, decides, and writes **one** Decision: `wording=(assumed, would have escalated)`, a required `reasoning`, and an `answers` link to the question. Nothing else.
- **Advisor.** Runs in non-autonomous runs while the owner is asked. It recommends with `rhiza run recommend --recommendation … --reasons … [--restatement …]` and never answers.
- **Origin.** These come from afk §5.5 (take the recommended option, record it as assumed, park the irreversible). The nebari data behind that: the owner took the recommended option 205 of 241 times.

### 2.10 Rules shared by every role

These are blocks shared across `internal/guide/text/shared/`:

- **Command budget.** Never sleep and never poll. No command over about 4 minutes; longer work runs once in the background and is waited on once.
- **Never-list.** Sleep or poll; leave stubs, TODOs or placeholders; add a dependency; rewrite history; chain `session done` after another command.
- **Session end.** `rhiza session done` runs as its own command, only after the recording command succeeded. Never `;` or `&&`.
- **Test scope.** Run only the tests covering the change, with the repo's scoped commands, never the full suite: "the PR's CI runs it."
- **Unstable CI.** Don't push or rerun CI to see. Reproduce locally. Reproduce flakes by injecting a delay at the suspected race, never by CPU stress.
- **Severities.** `blocker` (behaviour broken, build or test failing, leaf missing); `major` (acceptance point unmet, real defect); `minor` (style, naming, docs, follow-up); `nit` (taste). **Minor and nit are never fixed.**
- **Outside work.** Needed: a question linked `blocks`, then stop. Noticed: a `discovered-from` draft change set.

### 2.11 Limits and retries

| Limit | Where | Value |
|---|---|---|
| Hand-backs to the conductor | `handback_cap` | 3, then the run parks with an inbox item |
| Plan review rounds | plan.toml `max_visits` | 3. From round 3, confirmed majors become questions (the arbiter rules); reviser ≤2; apply-rulings 1 |
| Builder attempts (classic `leaves` step) | build.toml `models` ladder | sonnet → opus → opus (3) |
| Builder attempts and fix cycles (conductor) | **no explicit cap** in conduct.md | Bounded only by the stall watchdog and the hand-back cap. **A gap to close in the port.** |
| Classic review fix cycles | review.toml checks step | 2, then questions; `max_visits` 3 for owner re-entry |
| ci-fix / gate-fix | ship-change-classic | at most 2 each |
| Wait CI | ship-change.toml | 2h timeout, `max_visits` 6 |
| Failed run retry | owner, hands-off answers #2 | `retry_from` step plus findings in the next brief; one retry, then the inbox |

### 2.12 Owner gates and autonomous mode

- **Autonomy stages.** `rhiza.autonomy.{plan,accept,ship,signoff}` = `auto|owner`, or `autonomous=true` per workflow.
- **Where the owner can be asked:**
  - The plan interview, when the owner is present.
  - Plan acceptance (`run gate`).
  - Sign-off (`signoff` step: `rhiza run approve` or `rhiza run request-changes`).
  - Inbox items, for a parked run and for open questions that block.
- **Autonomous behaviour.** AskUserQuestion is denied by the PreToolUse hook (`Adapter.AskReply`), and the call becomes a question node. The arbiter answers it, and the session is messaged the answer. In a conductor step this always applies, whatever the stage settings.
- **The orchestrator layer** sits above the harness (`~/orchestrator`). It acts as the owner's proxy: it relays owner words verbatim (`hands-off-answers.md`) and appends machine rules to every child's brief (`conductor-rules.md`: systemd-run memory and task caps, one test command at a time, checks run one after another, 4-minute and 9-minute caps). These are environment overlays, not part of the harness.

---

## 3. The harness's contract with the engine

### 3.1 What is essential to the harness (port this)

| # | Contract item | Today in Rhiza | Why it's essential |
|---|---|---|---|
| E1 | **A unit of work with a leaf contract**: an intent, plus leaves each holding `statement`, `acceptance` (runnable, scoped, with a failure case or waiver), `files`, `must_not`, and Decisions with provenance | Change set, capability nodes, `rhiza slice --json`, `rhiza show <leaf>` | The acceptance is the contract that the builder, verifier, plan check and QA all judge against |
| E2 | **A session per role**, fresh context, launched with a role prompt, a brief file and env (change, leaf, attempt, role, check, cycle, run, actor) | `rhiza run spawn`; `RHIZA_*` env; `brief.md`, `slice.json`, `handback.md` | Independence between roles comes from separate sessions |
| E3 | **A stable actor identity per session**, recorded on commits | `Actor:` trailer; actor records in `.rhiza/log` | Without it there is no "the verifier never wrote the code" check |
| E4 | **Commit attribution trailers**: `Change:`, `Actor:`, `Leaf:` (builder or fixer) or `Review:` (run-level fixer), `Model-Conflict:`; plus a `Checks:` block (command: exit code) and an `Unfixed:` block | Git commit messages; build gate; hand-commit scanner | They link code to leaves; they let the verifier re-run the claimed checks; they let the engine count hand interventions |
| E5 | **Leaf findings**: `{reason}` per finding on (leaf, attempt) | `rhiza run findings --leaf` | Drives the builder retry, quoted verbatim |
| E6 | **Verification Evidence**: a record by actor X `verifies` leaf L, `result=PASS` | Evidence node and link via `rhiza apply` | build-check's input |
| E7 | **Check rounds**: per check, per cycle, `findings[{severity,leaf,place,reason}]` plus `evidence[{leaf,line}]`, numbered f1… | `rhiza run findings --check` | review-check's input; coverage rule |
| E8 | **Critic verdicts** on serious findings: `confirmed|downgraded(severity)|dropped` with a reason; append-only, latest wins | `rhiza run verdict --check` | Standing-finding computation |
| E9 | **Deterministic gates** computed from the records, never from what an agent says: (a) build-check: every leaf has live Evidence whose creator is named in no `Actor:` trailer of any commit trailed `Leaf: <leaf>`; (b) review-check: every declared check has a round, and none has a standing blocker or major; (c) the contract check: each leaf has its fields; (d) round coverage: a check round covers every leaf | `rhiza run build-check`, `rhiza run review-check`, `rhiza check`, `checkLeafCoverage` | This is where trust lives (§4) |
| E10 | **Done needs a record**: refuse a role's end while its required record is missing (verifier: Evidence or findings; check: a round; critic: verdicts on every serious finding; reviewer: a round) | `session_done*.go` | Removed the 4.7% of void verifier attempts |
| E11 | **Wake the parent on a child's end**, with role, leaf, outcome and detail; no polling | Engine message to the conductor pane | The conductor has to work without sleeping or polling |
| E12 | **Question routing**: a role can ask; the engine routes the question to the owner or to an arbiter session, records a Decision with provenance, and delivers the answer | AskUserQuestion deny hook → question node → arbiter/advisor → message | Autonomous mode |
| E13 | **Hand-back**: a red deterministic step re-enters the conductor with a structured failure file; capped | `handback:conduct`, `handback.md`, `handback_cap` | The conductor routes the failure to a fixer role |
| E14 | **A per-run workspace** (branch and worktree) and the means to give a role a **separate checkout** at a commit or the branch head | Run worktree; `.git/rhiza/checkouts/<run>/…` (classic only) | Lets verifiers and checks work apart from the code's author |
| E15 | **Run notes** (append-only one-liners), given only to builders and verifiers | `rhiza run note`; `spawnNotes` | Carries learnings forward without contaminating checks |
| E16 | **The project's commands** as settings: scoped test discovery (from the contributor guide), generate, render, local gate, pre-push check | `rhiza ship generate`, `rhiza.ship.{generate,localGate,prePushCheck}` | Pre-handoff checklist; gate-fix |

### 3.2 Rhiza-v1-specific plumbing (replaceable)

- The `.rhiza/` product model, `rhiza apply` JSON write ops, the fold and `.rhiza/log`, the render step `rhiza render`, `.rhiza/rendered` and its staging, staging of actor records, `rhiza land` and `land --commit`, model conflicts (`set --rebase`), the model check, `rhiza overlaps`, Sources and `rhiza source add --raw-session`, the lint-check and lint-fix, `rhiza check` canonical form.
- Rex panes, closing panes, `rhiza session done` as the end signal (any explicit "role finished" call works), and the Claude Code hook settings JSON: `SessionStart`, `UserPromptSubmit`, `PreToolUse` (ask-deny and `.rhiza/log` edit guard), `PostToolUse`, `Notification`, `Stop`, `SessionEnd`.
- The TOML workflow DSL (step kinds `agent`, `command`, `wait`, `leaves`, `checks`, `signoff`, `workflow`, `gate`, `arbiter`; `max_visits`, `retry_from`, `until`, `until_when`).
- The ship pipeline: `rhiza ship open-pr|check|merge|rebase|close-out|command|deploy|gate`, CI triage, the plan PR.
- The usage and machine holds, the stall watchdog, the token ledger, the model ladder and `risky_paths` tiering.
- The `/rhiza-<role>` skill install mechanism, and `rhiza guide <topic>` as the text server.

### 3.3 The CLI surface the roles call today (a complete list)

| Area | Commands |
|---|---|
| Reading | `rhiza slice [--change] --json`, `rhiza show <ref> --json`, `rhiza why <ref> --json`, `rhiza change show --json`, `rhiza run status [--json]`, `rhiza ship check --json` (`--model-conflict`), `rhiza run lint-check --json`, `rhiza overlaps --change --json`, `rhiza guide <topic>`, `rhiza describe run.findings` / `run.verdict` |
| Recording | `rhiza run findings [--leaf L | --check C] -`, `rhiza run verdict [--check C] -`, `rhiza run note`, `rhiza run recommend`, `rhiza apply <file> [--change|--record] --json`, `rhiza set`, `rhiza link`, `rhiza change new`, `rhiza change use`, `rhiza source add`, `rhiza request --kind bug` |
| Orchestration (conductor only) | `rhiza run spawn --role --brief [--leaf --check --model --no-wait]`, `rhiza run review-check`, `rhiza land --commit` |
| Project hooks | `rhiza ship generate`, `rhiza render`, `rhiza check` |
| End | `rhiza session done` |
| Git | `git show --stat HEAD`, `git diff $(git merge-base origin/main HEAD)`, `git add` (actor records, `.rhiza/rendered`, `.rhiza/log`), `git commit`. **Never** push, rebase, force or `--no-verify`; the engine pushes. No `gh` in roles (tests must use fakes). |

### 3.4 Caveats found in this survey (decide these before the port)

1. **Isolation in the conductor path is weaker than the text says.**
   - The verifier and check skills say "detached checkout" and "throwaway checkout". Those come from the classic `leaves` and `checks` steps (`StartLeaf` sets `dir`; `OpenCheckCheckout`).
   - Children started by `rhiza run spawn` start **in the run's shared worktree**: `run_spawn.go` sets no `dir`; capability `run-spawn` says "starts the child in the run's worktree".
   - So in today's default, check isolation is enforced by (a) a separate session, (b) the instruction not to read run status, (c) the engine withholding run notes from checks, critics and fixers, and (d) build-check's actor rule. It is not enforced by the filesystem.
   - The conductor also writes each check's brief while knowing the build history, which could contaminate it.
   - **Recommendation:** the port should give verifiers, checks and critics a separate read-only checkout, and engine-built briefs for checks.
2. **No cap on conductor build retries or fix cycles.** Classic workflows cap these (ladder of 3; 2 fix cycles). The conductor relies on its own judgment, the stall watchdog and `handback_cap`.
3. **"Minor and nit are never fixed" against "fixers act on what you confirm or downgrade"** (critic.md). In code, only blocker or major still standing after the verdicts reaches the fixer (`Round.Standing` / `Blocking`). A downgrade below major therefore removes a finding from fixing.
4. **Unjudged serious findings stand** (R8). If the conductor skips the critic, every blocker or major counts as confirmed. That is the safe default.

---

## 4. Why the owner trusts it

These are the mechanisms, each traced to its source.

1. **The acceptance is the contract, written before the code and judged by others.** Every leaf carries a runnable acceptance with a failure case, a `files` list and a `must_not`. The builder works to it; the verifier, the plan check and QA judge it point by point; and the evidence lines must cover every leaf (the engine refuses rounds that leave one uncovered).
2. **The verifier never wrote the code, and the engine checks that with git.** build-check passes a leaf only when its Evidence comes from an actor that appears in no `Actor:` trailer of the leaf's commits. Evidence written by someone else is refused at `session done`. This is a hard guarantee that afk did not have (its verifier was optional per task).
3. **Re-run, don't trust the self-report.** The builder must list every command and its exit code in `Checks:`. The verifier re-runs each one and the acceptance; a mismatch or a missing block is a finding. (afk `execute.js`: "Re-run every reported check command and every Acceptance Criteria command".)
4. **Independent, isolated judges.** Checks "know nothing of how the work was built". Each judges only the contract, the diff and the Evidence, so none can anchor on the builder's story. Three different lenses: contract (plan), code health (code), real behaviour with the built binary (QA). The QA check ran with real binaries because "fakes alone do not count". In nebari, QA was "the one that found functional defects".
5. **An adversarial critic that can only subtract.** It confirms, downgrades or drops, and can never add or raise. "When unsure, confirm." This cuts fix loops on findings that are real but over-graded (Rhiza had confirmed 242 of 246 before downgrade existed). It never hides a real blocker, because an unjudged finding stands.
6. **Verdicts are computed from records, never stated by agents.** review-check and build-check read only the journal and git. A check with no round fails the gate, and the conductor "never passes its own gate". (afk `review.js`: "Agents' own verdicts are never passed through.")
7. **Fixers fix exactly what was confirmed, reproduce it first, and carry what they couldn't fix** (`Unfixed:`). No silent drops, no drive-by changes. Then the checks run again.
8. **Done needs a record; no chaining.** Removes "ended with nothing" attempts and false passes.
9. **Provenance and no invented words.** Every choice is a Decision with the owner's quote, the picked option's wording, or `(assumed, would have escalated)` with reasoning. A reviser may not invent a quote. The owner can audit every autonomous call afterwards.
10. **Scope discipline.** Outside work becomes a question or a discovered-from draft, never silent scope creep. Plan review checks oversized leaves, uncovered intent clauses and contradictions with in-flight work.
11. **Mechanical guardrails from measured failures.** The command budget (idle gaps once cost 14% of tokens), scoped tests only, no push or force or rebase or `--no-verify`, never installing onto `$PATH`, flake quarantine with a filed bug, and a guard against edits to `.rhiza/log`.
12. **Visibility.** Every role is a separate, visible, journaled session. The owner: "I like the visibility of being able to see what is happening on a run easily."
13. **Measured track record.** afk shipped tasks at a median of 39m with 12% build rework and 7% review rework; nebari's reviews "caught real bugs" (8 of 21 rejected). The owner trusted forge's autonomous mode "many times". Rhiza adopted afk's content on the owner's word: "we may have lost something by not including the shipit/forge prompts."

---

## 5. A draft of the minimal plugin interface

The design principle comes from the owner: **the engine is "dumb but durable"**. It owns sessions, records, gates, routing and shipping. **The harness owns judgment**: its role prompts, the conductor's policy, its taxonomies and its failure routes. Determinism means durable records and deterministic gates, not scripted judgment.

### 5.1 What the engine must provide

```
# Workspace
workspace.open(unit) -> {branch, worktree}
workspace.checkout(at: commit|branch_head, mode: shared|detached_ro) -> path   # verifier/check isolation

# Sessions
session.spawn(role, brief_text, {leaf?, check?, cycle?, attempt?, model?, workspace_mode}, parent?)
    -> session_id         # visible, journaled; env carries unit/run/role/leaf/check/cycle/attempt/actor + brief path
session.end(session_id, outcome)    # called by the role ("done"); refused unless harness.done_requirement(role) holds
event child.ended -> parent         # {session, role, leaf, outcome: pass|fail, detail}; push, never poll

# Identity
actor_of(session) -> actor_id ; commit trailers parsed from git: Unit/Change, Actor, Leaf, Review

# Records (append-only journal; schemas validated)
record.leaf_findings(leaf, attempt, [{reason}])
record.evidence(leaf, actor, statement, result)         # "verifies"
record.round(check, cycle, findings[{id?, severity, leaf?, place, reason}], evidence[{leaf, line}])
record.verdicts(check, round, [{finding, verdict: confirmed|downgraded|dropped, severity?, reason}])
record.note(text)                                       # visible to roles the harness lists
record.question(text, blocks?) -> routed to owner | arbiter ; record.decision(provenance)

# Gates (pure functions over records + git; the harness picks which to require)
gate.build_check(unit)     # every leaf has Evidence by a non-author actor
gate.review_check(unit)    # every declared check has a round; none has a standing serious finding
gate.contract_check(unit)  # leaf fields present
gate.coverage(round)       # every leaf named by a finding or evidence line
gate.command(cmd)          # project's local gate / pre-push

# Flow
handback(conductor, failure_file{step, outcome, failing_commands[+output paths], tail, cause, ci_runs, conflicts}), cap N
signoff(unit) -> pass | changes(text)      # owner or auto
ship(unit)                                 # push, PR, CI wait, merge, post-merge checks; reds before merge -> handback
holds: usage / machine load -> spawn waits (journaled)
```

### 5.2 What the harness declares (a manifest)

```toml
[harness]
name = "afk-forge-rhiza"
roles = ["planner","plan-reviewer","plan-critic","plan-reviser","conductor","builder","verifier",
         "check-plan","check-code","check-qa","critic","fixer","ci-fix","gate-fix","conflict-fix","arbiter","advisor"]
prompts = "roles/<role>.md"          # the skill texts, with shared blocks
entry = "conductor"                  # session the engine starts per unit; it spawns the rest

[checks]                             # declared check names; review_check counts only these
names = ["plan","code","qa"]

[severities]
order = ["blocker","major","minor","nit"]
serious = ["blocker","major"]        # what critics judge and gates count
fixable = ["blocker","major"]

[done_requirements]                  # engine refuses session.end without these
verifier   = "evidence_by_self | leaf_findings(attempt)"
"check-*"  = "round(check, cycle)"
critic     = "verdict_for_each_serious(check, latest_round)"
plan-reviewer = "round"
plan-critic   = "verdicts(latest_round)"

[visibility]                         # what records each role may read (engine enforces where it can)
notes      = ["builder","verifier"]
run_status = ["conductor","critic(own check only)","fixer","plan-*"]
workspace  = { verifier = "detached_ro@leaf_commit", "check-*" = "detached_ro@head", critic = "detached_ro@head" }

[gates]                              # required before land/signoff
before_land = ["build_check","review_check","contract_check","command:local_gate"]

[trailers]
required = { builder = ["Change","Actor","Leaf"], fixer = ["Change","Actor","Leaf|Review"], "*-fix" = ["Change","Actor","Review"] }
body_blocks = ["Checks:","Unfixed:"]

[handback_routes]                    # failure -> role (the conductor reads these; the engine passes the failure file)
build_check = "verifier|builder"
review_check = "critic,fixer,checks"
contract_check = "fixer"
local_gate = "gate-fix"
signoff_changes = "fixer,checks"
ci_red = "ci-fix"
merge_conflict = "conflict-fix"
ci_timeout = "none"

[limits]                             # make the conductor's caps explicit (gap today)
builder_attempts = 3
fix_cycles = 2
plan_review_rounds = 3
handback_cap = 3
command_budget = "4m"
```

### 5.3 Events across the boundary

| Direction | Event | Payload |
|---|---|---|
| engine → harness | `unit.started` | unit ID, slice (intent, leaves with their contract, constraints, Decisions), brief, autonomy |
| engine → harness | `child.ended` | session, role, leaf, check, outcome, detail |
| engine → harness | `handback` | failure file (see 5.1) |
| engine → harness | `question.answered` | question, Decision, provenance |
| engine → harness | `signoff.changes` | owner's text |
| harness → engine | `spawn`, `record.*`, `ask`, `land`, `end(outcome)` | as in 5.1 |

### 5.4 States the engine tracks (the harness reads them)

- **Unit:** `planning → conducting → gating → signoff → shipping → done | failed | parked(inbox)`
- **Leaf:** `open → built(attempt n, commit) → verified(evidence by actor ≠ authors) | rejected(findings) → built(n+1) …`
- **Check:** for each `cycle`, `pending → round_recorded → judged(verdicts) → fixed → (next cycle)`. A finding is *standing* = serious and not (dropped, or downgraded below serious).
- **Session:** `running → said_done | exited(code) | lost`; holds while usage or load is high.

### 5.5 What the harness owns

- Role texts and the conductor's phase policy.
- The severity taxonomy and the per-check grading.
- The plan-review kinds.
- The brief contents.
- The failure-to-role routes.
- The model per role.
- The rule that outside work becomes a question or a draft.

It does **not** own pushing, merging, gate evaluation, identity or the journal: those are the engine's, so a harness can never grade itself.

### 5.6 Port notes, in priority order

1. Keep the four deterministic gates and done-needs-a-record exactly as they are. They carry the trust.
2. Make isolation structural: read-only detached checkouts for the verifier, the checks and the critic, and check briefs built by the engine from the slice and diff only.
3. Make the conductor's caps explicit (builder attempts, fix cycles) and enforce them in the engine.
4. Rename `.rhiza` specifics to the new model: leaf, Evidence and Decision become generic record types. Keep the trailer names, or map them one-to-one.
5. Keep machine and owner overlays (orchestrator rules, project commands) as brief appendices, outside the harness text.
