# Rhiza (formerly Genesis): lessons ledger

Covers 2026-10-23 to 2026-11-19. Compiled 2026-11-19, read-only.

## Sources

- **Orchestrator notes** in `~/orchestrator/`: STATE.md, HANDOFF.md, hands-off-*.md, conductor-rules.md, machine-rules.md, nebari-brief.md, nebari-log.md, planner-notes/, perf-2026-11-12/, architecture-survey-2026-11-15.md, and the watcher scripts with their logs.
- **The rhiza repo:** its git log of 674 commits and `.rhiza/rendered/`.
  - 251 sources; 73 of them contain the owner's quotes. All were extracted to `source-quotes.txt` in this folder.
  - 1,247 decisions, 323 questions, 152 intents, 42 notes and 14 constraints.
- **Nebari:** its git log and `.rhiza/`.
- **Two earlier audits:** `../arch/architecture-audit.md` and `../rhiza-audit/report.md`.
- **Supporting notes in this folder:** `git-notes.md` (timeline and churn) and `decisions-notes.md` (reversals, statistics, quotes by theme).
- **Not on disk:** the founding records under `~/code/_genesis/conversations/` were not found anywhere on this VM (searched `/`). They survive only as the scrubbed Source `founding-conversation.md` and the milestone sources.

In quotes, "owner" means words the owner typed. Picked options from the agent's menus are marked as picks. The owner ruled on 2026-10-25 that they are not his words: "Those are from the ask user question tool, so none of them are my own words."

---

## 1. Timeline

### Phase A: Founding and the knowledge core (Oct 23–25)

- **Oct 23:** "Start the Genesis repo". The founding conversation set the direction:
  - a product as "a living, breathing, ever changing piece of software, that has records of how it has changed, how the decisions were made, and how all of it is connected. More of a interconnected web than a linear page of data"
  - an append-only log in each repo
  - a CLI that ships with agent skills
  - Rex required
  - six layers
- **Oct 24–25, M1:** eleven tasks built with shipit in Rex tabs (#1–#11). The tasks covered the log, change sets, the projection (as-is, to-be and what-if), the CLI, check, the SQLite index with a 50 ms read budget, the slice and the skills.
- **Oct 25:** Genesis imported its own founding plan (#13), then planned milestone 2 inside itself (#14).

### Phase B: Genesis becomes Rhiza; workflows built into the tool (Oct 25–28)

- **Oct 25 22:09, the rename** (#23, b0eb080): 1,106 files changed, "a clean break: no alias, fallback or GENESIS_* variable".
  - The new name comes from mycorrhiza. It was picked over Hypha and Myco because neither of those collides with an agent tool.
  - Owner: "Ok I think I like the name. The designs are going to need some work..."
- **Oct 26–27, M2 and M3:** runs and the journal, the ship-change workflow, the autonomous mode and arbiter, then the plan, build and review workflows (#27–#45).
  - Owner, M3: "Right now I'm not seeing the benefit of having an LLM conductor". The decision was engine-is-conductor: deterministic and journaled.
- **Oct 28, M4: "Rhiza orchestrates… with no orchestrator session"** (#48), and the daemon (#52). This was the first of six "no orchestrator" declarations.

### Phase C: Autonomy ramp and milestone sprawl (Oct 29–Nov 2)

- **Oct 29, owner:** "I'm stopping for the night, let's just run autonomously from here on out, I'm just accepting the recommended answers anyway".
- **Oct 28–29:** the TUI shell, then native Rex actions. Owner: "That works, but that isn't what I was looking for."
- **Oct 30–Nov 1:** M5–M23 planned in about 48 hours. The orchestrator picked most of them under "if you run out of work keep picking high value tasks. don't stop and wait for my input."
- **Oct 29:** GitHub billing stopped CI, and the daemon was stopped by hand.
- **Oct 31:** the disk filled (ENOSPC) and every session stalled from about 09:16 to 13:50Z.

### Phase D: A headless VM, many projects, the web UI and the first outside repo (Nov 2–7)

- **Nov 3, M24:** a headless daemon under systemd on a Linux VM. The orchestrator was handed from the Mac to the VM (HANDOFF.md).
  - Owner: "act on my behalf. i want an empty backlog".
  - Owner: "No need to see anything in a Rex tab that isn't doing agent work."
- **Nov 4:** CPU overload, and the daemon was paused. Owner: "We are just killing the CPU on this machine… I think we need to pause and figure this out."
- **Nov 5:**
  - The TUI was reset (M26, #301 deleted 4,723 lines).
  - Web UI v1 shipped (M28).
  - New constraint: no IDs or handles on owner surfaces.
- **Nov 6:**
  - M29 added a root daemon for many projects.
  - M27 imported Nebari, the first brownfield repo. The `rhiza import` command itself was never built.
- **Nov 7:** two principles recorded (#353): "workflows are the rules" and "warn, don't safeguard".

### Phase E: Going public, releases, CI pain (Nov 7–12)

- **M30–M34:** docs, the owner's voice, releases (v0.1.0 cut by the orchestrator, #477), and AGENTS.md.
- **Nov 8, owner:** "the amount of failed CI runs is way out of control… if its not green here IT should never go too Ci".
- **Nov 12:** the performance report: 233 run journals and 699 CI runs.
  - Owner: "I'm disappointed with the overall performance of a full cycle of build runs. It takes a long time, and seems to be error prone."
  - M35 brought afk's prompts back.

### Phase F: The conductor pivot (Nov 12–14)

- **Nov 13, owner:**
  - "I'm dumbfounded as to how things are still taking so long and how we still have to babysit this process."
  - "the daemon should be dumb but durable. the original ask for orchestrating from the daemon was determinism but we're not getting that."
- **M36 (#526):** one conductor agent per change set over a daemon with eight primitives. Owner: "Promote immediately, it's not going to get worse, I'm tired of this."
- **#578:** the conductor became the default. The old flow survives as ship-change-classic.
- **M37:** the web UI redesign, 7 days after v1. Owner: "The web UI is mostly useless to me right now."

### Phase G: The daemon off, then performance and architecture (Nov 14–16)

- **Owner (quoted in nebari-brief.md):** "I do not want the daemon involved at all, I want it shut down."
- **M38–M39 were run daemonless.** An orchestrator spawned the runs by hand, with watcher, closer and runwatch scripts around them.
- **M38, the fast test suite:**
  - Owner: "We really need to focus on Rhiz performance. It's TERRIBLE."
  - It closed without meeting the cold target: 423 s against a 240 s target.
- **M39:** reads come from the projection. The trigger was the architecture survey: check took 15 s, status 2 s, and the log had grown to 127 MB.
- **Nov 15:** parallel tests spawned about 1,600 processes and the VM ran out of memory, killing every session. machine-rules.md was written that day.

### Phase H: "The daemon runs any workflow without intervention" (Nov 17–19)

- **Nov 17, M40, owner:** "intervention should be the exception. even for bring your own workflows. so having an orchestrator like this should not be necessary."
- **Nov 18:** shipped:
  - spawn holds
  - stall and runaway detection
  - retry from the right step
  - interventions journaled
  - a clean-streak measure
- **Nov 19:**
  - The 11 remaining founding "Area" change sets and the landing page were withdrawn.
  - `rhiza daemon status` shows `clean_streak: 0`.
  - The orchestrator now runs a watch-only `wait-trouble.sh`.

### Pivot summary

| Date | From | To | Driver |
|---|---|---|---|
| Oct 25 | Genesis | Rhiza (clean break) | owner |
| Oct 26 → Nov 13 | Deterministic engine and daemon as conductor | LLM conductor agent over a "dumb but durable" daemon | owner, after measuring |
| Oct 27 → Nov 12 | shipit as an agent step | shipit replaced by Rhiza prompts, then afk/shipit prompts brought back | owner |
| Oct 29 → Nov 5 | Many TUI mini-apps | 5 small modal apps; the rest deleted | owner |
| Nov 5 → Nov 13 | Web UI v1 | Redesign, phone-first, "does anything need me?" | owner |
| Founding → Nov 3 | Supervised Mac, never bypass | Dedicated VM with bypassPermissions | owner, via machine trust |
| Nov 3 → Nov 6 | Per-repo daemon | One root daemon, root required | owner |
| Nov 14 → Nov 17 | Daemon on | Daemon off, then back on with hands-off as the goal | owner |

---

## 2. Wins: what worked and what the owner trusted

### W1. The knowledge-model core is sound engineering

- The core is about 17.6k lines: id, canon, key, textref, op, entry, store, changeset, model, index, slice, render and check.
- The architecture audit says it "is layered with no upward dependencies, made of pure functions, documented, property-tested and fuzzed, and it barely churns".
- Only about 13 of 129 fix commits touched core packages.
- The fix share of commits was 4% from Oct 23 to Nov 1, while work was mostly core. It rose to 43% when the daemon and runs came online.
- The audit's salvage list names `canon`, `id`, `key`, `textref`, `usage`, `adapter`, and the patterns behind `slice`, `index`, `store` and the api registry. That registry gives typed operations, and `--json` output identical to the API.

### W2. The append-only log in the repo, with commit trailers

- The owner chose this himself: "This has to be portable to many repositories and live inside of them… I don't think a database is the right choice here. I think an append-only log is the right choice."
- Trailers (Change:, Leaf:, Actor:, Review:, later Checks:) linked commits to the model. Every audit and reversal analysis in this ledger was possible because of them.
- The weakness was the never-prune rule, not the log itself (see F6).

### W3. Provenance discipline: whose words are these?

- The owner's rule that picked options are not his words held for the whole month.
- Every decision carries a wording field. 330 decisions carry "(assumed, would have escalated)".
- This is why the ledger can separate 156 decisions in the owner's typed words from 376 picked options and 409 assumed ones.
- The owner pushed back the one time the orchestrator blurred it. The orchestrator typed into a peer session as if it were him, and he said: "use the send message tool, don't pretend to be me. the other agent is confused" (STATE.md, Oct 29).

### W4. Scrubbed conversation Sources and owner-quoted Decisions

- The founding conversation, milestone interviews and owner messages were kept as Sources with verbatim quotes. They are the highest-value part of the model.
- Owner, at the start: "let's not forget to record this initial conversation, it's going to be important."

### W5. The interview-style planning loop, with recommended options

- The owner moved quickly through structured topics: "yes to all four, move on to A5".
- He took the "(Recommended)" pick 112 of 128 times (88%).
- In the M1 task interviews, agents asked 197 questions. The owner accepted 193 agent options, at least 173 of them the recommended one.
- It worked because it was fast. The risk is that it became rubber-stamping (see F9).

### W6. Owner-in-the-loop moments the owner liked

- Review findings in M1: "This is great. I love the reviews."
- Visible sessions, which is why he chose spawned sessions over sub-agents: "I like the visibility of being able to see what is happening on a run easily."
- The native Rex keys: "Nice, that is a really nice experience. None of the keys actually do anything, but it's a nice start."

### W7. The conductor model with a dumb, durable daemon

The conductor model came after the measured failure of the 56-step workflow.

- **Nebari (nebari-log.md, Nov 15):** on the conductor default, four features shipped plan-to-merge in one night, each with one PR and green CI:
  - the admin email (#107)
  - tree notes (#109)
  - multi-tree pots (#111)
  - the Open-Meteo fix (#113)
- **Bugs caught in review:** the critic-confirmed review found real defects. One example: "the test send's deliver_now had no rescue, so an SMTP error gave a 500 and lost the composed form".
- **Rhiza on Nov 17:** "the daemon took five fixes from plan to merge on its own (#612-#621), but only with an orchestrator session beside it".
- **Speed:** ship-change's median wall time went from 98–261 minutes a day (Nov 3–8) to 55–74 minutes (Nov 9–11). Plan time went from 28–43 to 16–19 minutes (perf report).

### W8. Gates that stayed and were trusted

- **Two owner gates, plan acceptance and ship sign-off**, became workflow defaults ("workflows are the rules").
- **Releases:** "Fully automatic for minor and patch, owner gated for major releases."
- **Docs voice:** a docs workflow with owner sign-off per page. Owner: "it doesn't look like it was all humanized using my voice".
- **Critic rule:** the critic runs only on blockers and majors. It confirmed 242 findings and dropped 4. It was kept and given the power to downgrade.

### W9. Measuring before redesigning

- The Nov 12 performance report and the afk comparison turned frustration into numbers, and the numbers drove the conductor pivot. Examples:
  - 56 steps
  - 76% of runs needed a second review cycle
  - CI ran 3–4 times per change
  - afk's median was 37.6 minutes, against about 75 for Rhiza
- The Nov 15 architecture survey worked the same way and drove M39.
- The owner asked for this himself: "This needs a diagnosis."

### W10. Hermetic, deterministic tests

- The testscripts are hermetic. Clock and IDs are injected (`RHIZA_TEST_NOW`, `RHIZA_TEST_SEED`), a virtual clock was added (#588), and there are property and fuzz tests.
- The audit says "Flakiness comes from real concurrency in the run and daemon code, not from lazy tests."

### W11. Single-purpose, in-and-out UI (TUI v2)

- Owner: "multiple small apps. single purpose. i don't want the tui to dominate the terminal. think in and out."
- Five modal apps: Inbox, Request, Runs, Daemon and Autonomy.
- These are the actions the orchestrator had been doing by hand at night.

---

## 3. Failures: what broke, recurring pain, manual workarounds

### F1. "No orchestrator" was never achieved

- The goal was declared six times: M2/M3 (Oct 25–26), M4 (Oct 28), M6 (Oct 30), M22 (Nov 1), M36 (Nov 13) and M40 (Nov 17).
- The M40 intent itself says the fixes merged "only with an orchestrator session sending machine rules, watching stalls, sweeping tmp, setting maxRuns and retrying failed runs by hand".
- 133 commits carry `Actor: claude-code-orchestrator`. The orchestrator role was written into `agents/orchestrator.md` (#496) instead of being removed.
- A counted measure (consecutive runs with zero journaled interventions) arrived only on Nov 18. On Nov 19 `clean_streak` = 0.

### F2. The owner had to build scaffolding around the tool

Each item below is a stopgap the product should have owned.

| Stopgap | What it does | Evidence |
|---|---|---|
| `watch.sh` (3 versions) | Polls PRs, daemon status, run steps, the inbox and Rex panes every 45 s. Detects usage-limit and permission prompts by scraping pane text. Clears fake usage-limit hits that tests wrote into the real root. Watches load and disk. | HANDOFF.md says "Monitor with ~/orchestrator/watch.sh (Monitor tool, 30 min, re-arm on expiry)". Owner: "keep monitoring and don't stop. restart the monitor when it stops, that is your entire job." |
| `runwatch.sh` (3 versions) + systemd unit | Detects stalled children (idle over 10 min with no done mark), unexited processes and quiet runs. Learned to tell a background job apart from a stall. | runwatch.log: 534 session starts tracked, STALL lines including "check 055adc6b idle 3863m" (64 hours); "a run sat 9 hours because a fixer never ran `rhiza session done`" (Nov 16) |
| `closer.sh` / `closer-guarded.sh` / nebari copy + skip list | Sends `/exit` to panes of children that said done but never exited | closer.log has 54 closes; nebari-closer.log has 73. Retired Nov 17 after #614. |
| `wait-actionable.sh`, `wait-trouble.sh` | Sleep until something actionable happens | The current orchestrator mode (Nov 19) |
| `conductor-rules.md` / `machine-rules.md` | Prose rules pasted into every conductor's brief: wrap tests in `systemd-run` with MemoryMax=6G and TasksMax=300, one test at a time, `-p 2`, kill after 4 min | Written after the Nov 15 OOM. The daemon's localGate setting still wraps `bin/gate` in the orchestrator's flock plus systemd-run (`rhiza daemon status`, Nov 19). |
| `planner-notes/` | Hands ideas to a running planner by writing a file and messaging its session | hands-off interview, point 8 |
| tmp sweeps (rhiza-tmpsweep.service) | Deletes leaked `/tmp/rhiza-projection-*` folders | About 8,900 folders, 7.1 GB of RAM-backed /tmp |
| Hand-set `maxRuns` | Sized from load and memory by eye | Raised to 3, cut back to 2, kept at 1. Now maxRuns 1, maxLoad 1.5. |
| caffeinate tab (Mac era) | Kept the Mac awake | STATE.md |

### F3. Manual interventions were routine

Examples from STATE.md and nebari-log.md:

- **Merges, rebases and conflict resolution by hand:** #36, #109, #113, #116, #187, #203 ("merge commit by mistake"), #210, #246.
- **Lessons the orchestrator wrote down about hand fixes:**
  - "a hand push to a live run's PR leaves .git/rhiza/ship/<cs>.json `pushed` stale"
  - "retrying after a hand fix needs the ship state's pushed head edited by hand"
- **Close-out failing on untracked log files in main.** The workaround was to diff each file against origin/main, delete it, pull, then abandon. This led to the owner's rule: "I don't feel like we should ever have anything uncommitted on main like this. Everything should get it's own branch."
- **Stuck prompts:** builders sat on Claude Code dialogs and usage-limit prompts for hours. They were "found only by eye", then dismissed with Escape and a nudge. An agent's `rm` safety prompt showed as "blocked" with no one to answer it.
- **Answering for the owner:** the orchestrator answered AskUserQuestion menus in run panes as arbiter by scraping and sending keys (nebari-brief).
- **Rebase-conflict questions sent to the owner's inbox:** 21 "Raise a question on the rebase conflicts" commits.
- **The perf report:** "All failed runs were 'abandoned by dphaener'". There were 59 held runs, for 97.6 hours held in total.

### F4. The workflow became too heavy

- The default ship-change grew to **56 steps**. Every run walked the same 33, and the 23 recovery branches fired 6 times in 8 runs.
- It took about 56 minutes to ship plus about 19 to plan. CI ran 3–4 times per change. Review spawned about 11 sessions per cycle.
- 76% of runs needed two review cycles. The scope check found 0 confirmed findings in 8 runs.
- Over all runs:
  - The build verifier rejected 26.5–30.7% of attempts.
  - 57% of runs needed an extra review cycle.
  - The wait-landed step was a full CI round in every run.
  - 25% of ship-change wall time was "held", with nothing running.
- Owner: "it has a massive amount of steps. it seems like we may be over doing it there", and "in afk, the majority of these things were handled by agents… i don't feel like it should add that much overhead."
- Losing afk's prompts cost quality. prompt-gap.md shows Rhiza's role prompts had shrunk to one line where afk had checklists. Example: quality was "correctness, tests, the repo's conventions".

### F5. Resource exhaustion on one shared host

Incidents in order:

- **Oct 29:** GitHub billing failure. CI wouldn't start and the daemon was stopped.
- **Oct 31:** disk full (ENOSPC) and every session stalled for about 4.5 hours.
- **Nov 4:** load 28 on 4 cores, CPU pressure (PSI) 88%. Three agents were running full suites at once. The daemon was paused.
- **Nov 11:** disk 79% used. Claude scratch folders took 29–34 GB and index `.db` files of removed worktrees 15 GB.
- **Nov 15:** about 1,600 testscript processes caused an OOM that killed every session. The VM's balloon also shrank RAM to 8 GiB.
- **Later:** earlyoom killed `rhiza ship open-pr` (2.2 GB RSS) twice.

What followed:

- Seven hygiene packages were added late: tmpsweep, gosweep, rootguard, leftovers, quiettest, proctree, envexec.
- The watchdog grew to nine policy files. The audit's description: "a watchdog for the system's own orchestration".

### F6. Work exhaust flooded the knowledge model

- **Node counts:** 6,980 actor nodes (54% of all nodes) and 2,846 evidence nodes. 76% of nodes and 58% of bytes are process, not knowledge.
- **Size:** the log reached 127 MB on Nov 15 and 141 MB later, with 54 MB of rendered markdown.
- **Bookkeeping in git:** 159 of 674 PRs (24%) are "Render the plan". About 64% of squashed sub-commits are bookkeeping.
- **Little is reused:** only 20.8% of knowledge nodes carry any sustained reference. Questions are at 2.8%, and 37.6% of knowledge nodes are named only by their own build commits.
- **The feed:** "of its first 1,000 items, 869 are 'Record agent actor'."
- **Reads got slow:** `check` took 15 s and `status` 2 s, and status runs on every hook call.

### F7. Orchestration code became a god package and grew by accretion

- `internal/api` is 55k lines with 158 operations and a 46-field Ctx. It carries 108 of the 129 fix commits.
- Deletions are 8% of additions.
- The same bugs were fixed again and again. "Fix watchdog unmerged PR at" was fixed **5 times** (#389, #432, #484, #485, #558).
- The model rules were implemented three times, as the interim model, the strict engine and a hybrid, and ten parity notes record them disagreeing.
- Surface area: 214 operations and 58 settings in a month.

### F8. UIs were built before anyone knew what the owner needed

- **TUI v1, ten apps:** "Currently the commands and mini apps don't provide a lot of value… It just prints a json blob of things and isn't actionable in any way." Later: "there are too many. i won't use most of them".
- **Web UI v1, a week later:**
  - "The runs tab doesn't really tell me anything useful… it just lists 'Session 1, Session 2, etc'"
  - "Some of the items in the backlog just 404… The 'titles' seem to be a description and just run off."
  - "I don't see any of the rendered markdown being used anywhere"
  - "Overall it's just hard to digest the information that is being presented and doesn't help me much at all."
- **Watcher tabs:** "Tab 2 and 3 just look like 'watchers'. Theres no reason those need to be running in tabs… It's just noise."

### F9. The owner's attention thinned while decisions multiplied

- **Owner involvement in decisions fell:**

  | Period | Owner-involved decisions |
  |---|---|
  | Oct 25–28 | 82% |
  | Oct 29–Nov 5 | 37% |
  | Nov 13–19 | 13% (156 of 229 assumed) |

- 127 of 281 quoted decisions are blanket approvals ("yes to all four").
- Agents "narrowed" owner decisions without asking: "it is settled here, not sent back to the owner."
- The owner flagged the cost himself: "often times you move a little fast and make decisions without consulting with me. I know this can be confusing at times, since I do sometimes ask for full autonomy, but that should only happen when asked for."
- He also changed his own milestone-planning rule within two days (see R4).

### F10. There is no undo or rollback

- `question/autonomous-undo` was raised Oct 26 and is still open at 24 days: "How is something an autonomous run did undone: an accept made under rule autonomous, or a Decision recorded (assumed, would have escalated)?"
- The owner left no words on rollback. The model has no partial supersede, so a supersedes link silently drops an owner-quoted decision from agents' context (`web-host-amends-not-supersedes`).
- Rhiza has no reverts in its history. Things were withdrawn, replaced or deleted instead.

### F11. CI used as a debugger, and flaky tests

- 23 commits mention flaky, 23 stall and 14 race.
- The CI gate rules changed three times in four days, and the budget rules three times.
- Nebari's main pain was flaky Rails system specs (#70, #83, #85 "failed 7 of 10 runs locally under load", #90). The flake change set was withdrawn.
- The test suite was "TERRIBLE": 854 s cold on 4 cores. M38 closed with the cold target unmet.

### F12. Targets set but never measured

- **M36:** at most 40 minutes, and at most 1 run in 10 needing a person, over 30 runs. The measurement change set is still a draft.
- **M38:** the cold target failed and moved to the next milestone.
- **M40:** the clean streak is 0.
- **Smaller misses:**
  - "faster reads" claimed under 25 ms on the runner, but "nobody measured there".
  - "Shallow checks pass": an 80 s plan check with no summary was accepted (#631).

### F13. Agents doing dangerous things in the real environment

- **Oct 28:** a verifier's scratch-built `rhiza run start` "started a REAL ship-change run… against real Rex/Claude". This led to the constraint "No agent session starts a run."
- **Tests wrote into the real root:** internal/api tests wrote fake usage-limit hits into the real root, which held every run. watch.sh scrubbed them.
- **Rex install:** the first real `rhiza rex install` followed a symlink into the owner's dotfiles and broke Rex. It was reverted by hand.

### F14. Second user (Nebari): low adoption

- 61 commits after the import, of which only about 10 are real features or fixes built through Rhiza.
- Nebari needed custom Ruby workflows until Nov 14, plus orchestrator-supplied gate wrappers.
- There is an unmerged, stuck worktree, `nebari.fix-flaky-system-spec`.
- `rhiza import` was never built.

---

## 4. Reversals

| # | First | Then | When | Why (evidence) |
|---|---|---|---|---|
| R1 | `engine-is-conductor`: deterministic, journaled, "no LLM conductor"; then the daemon drives (`answer-8cd68bc7-2`, "Daemon (Recommended)") | `conductor-milestone-direction`: a conductor agent per change set over a dumb, durable daemon; the old flow kept as ship-change-classic | Oct 26 → Nov 13 (18 days) | Owner: "the original ask for orchestrating from the daemon was determinism but we're not getting that." Measured: 56 steps, about 75 min against afk's 37.6. |
| R2 | `shipit-as-agent-step` | Replaced by Rhiza's own prompts (`answer-cb8bd934-5`), then afk/shipit prompts back as defaults (`afk-prompts-shipped-default`) | Oct 25 → Oct 27 → Nov 12 | Owner: "we may have lost something by not including the shipit/forge prompts". prompt-gap.md showed roles cut down to one line each. |
| R3 | `tui-many-mini-apps`, then a picker | `tui-single-purpose-apps`: 5 modals; #301 deleted the rest | Oct 29 → Nov 5 | "there are too many. i won't use most of them"; "they aren't using any of the form elements from rexuikit" |
| R4 | `milestone-plan-never-autonomous`: "that always requires a human"; a constraint was drafted | Constraint withdrawn; `workflows-are-the-rules`: everything is a workflow default | Nov 5 → Nov 7 | Owner: "everything should run through a workflow… rhiza ships with sensible defaults but should be flexible. the only thing we enforce is the references between nodes." |
| R5 | Two owner gates; Mac sessions supervised, "never bypassed" | Autonomy everywhere; the orchestrator as "owner's proxy"; a machine-trust file; bypassPermissions on the VM; "warn, don't safeguard" | Oct 26 → Nov 3 → Nov 7 | Owner fatigue ("I'm just accepting the recommended answers anyway") and the overnight backlog goal |
| R6 | Founding: "every item carries its handle and ID" | `owner-surfaces-full-text` became a constraint: no IDs or handles on owner surfaces | Oct 25 → Nov 5 | Owner: "No need to ever show the internal handle, it's nonsense to a user." Also, on tabs: "I don't like that 'genesis m2 cs9' is the default name for the tab." |
| R7 | Review: 3 agent checks (founding) | 5 checks plus critic (Oct 26), then scope folded into compliance (Nov 12), then Plan, Code and QA (Nov 13) | Oct 25 → Nov 13 | Scope found 0 confirmed findings in 8 runs; QA found 1. Each cycle cost about 11 sessions. |
| R8 | Run tabs and watcher tabs in Rex; tiled panes per run | Headless run drivers (#210); finished panes close at once | Oct 29 → Nov 3 | "No need to see anything in a Rex tab that isn't doing agent work."; "It's just noise." |
| R9 | Per-repo daemon | Root daemon required (M29) | Nov 6 | "There isn't anything existing so root can be required from the start" |
| R10 | Web UI read-only, localhost only | All hosts; safe actions; full redesign, phone-first, "does anything need me?"; then the "needs me" Home change set withdrawn on Nov 19 | Nov 5 → Nov 19 | F8 quotes |
| R11 | Daemon on, autonomous, maxRuns 2–3 | Daemon shut down for M36–M39 ("I do not want the daemon involved at all"); back on Nov 17 at maxRuns 1 | Nov 14 → Nov 17 | Stability and performance; the OOM |
| R12 | CI on push to main; one CI job | Sharded; push-to-main CI dropped; local gate while "unstable"; "CI since last green" | Nov 5 → Nov 12 | 11–18 min CI; "if its not green here IT should never go too Ci". Gate rules rewritten three times in four days. |
| R13 | Hard-coded transient-error signatures | Headless LLM classifier (Haiku), "users can bring their own LLM" | Nov 11 | Owner: "isn't just reading the exit code good enough? If we need to detect what kind of error, maybe a quick call out to an LLM" |
| R14 | `selfUpdate` / `self_update_project` | `autoUpdate` with channels (off, main, latest, release) | Nov 11 | "This should also be alled 'autoUpdate' and not 'selfUpdate', so it makes sense and aligns with the rest of the world" |
| R15 | Parallel leaf builds planned (M35) | Withdrawn for the conductor | Nov 12 → Nov 13 | Superseded by the conductor design |
| R16 | 11 founding "Area" umbrella change sets plus the landing page | All withdrawn | Oct 25 → Nov 19 | Up-front spec never built; umbrellas were "never built", only carved |
| R17 | Genesis | Rhiza | Oct 25 | Lineage name gave way to one that avoids collisions; done as a clean break with no aliases |

**Meta-lesson.** Supersession was barely modeled. Only 24 decision-to-decision supersedes links exist, and none of the 131 founding decisions was formally superseded, even though R1, R5 and R6 overturned founding decisions in substance. The record of why something changed lives in Sources and git, not in the decision graph.

---

## 5. The owner's stated principles and preferences

All quotes are verbatim, typos kept.

### Vision and quality

- "I want to build a sophisticated system that I can start using soon, that is going to be quality software, and that will build quality software."
- "Do not rush to start writing docs and making decisions, we have a LOT to discuss here."
- "You can use as little of it as you want, bring your own workflows, whatever coding agent you want".

### Speed and overhead

- "I'm dumbfounded as to how things are still taking so long and how we still have to babysit this process. it's getting frustrating."
- "in afk, the majority of these things were handled by agents. i understand the addition of change sets and leaves and what not but i don't feel like it should add that much overhead"
- "I like this process overall but it's just not efficient."
- "find the fastest possible route to this being usable, by me, from rex, using the TUI to launch things"

### Autonomy and trust

- "the overall idea here is that the daemon should be able to execute any workflow from start to finish without intervention… intervention should be the exception. even for bring your own workflows."
- "I should be in fuyll control of which parts of the process are autonomous, that should also be interactive and part of the tui."
- "often times you move a little fast and make decisions without consulting with me… [full autonomy] should only happen when asked for."
- "milestone planning should never be autonomous even in autonomous mode. that always requires a human." (later made a workflow default, R4)
- "Those are from the ask user question tool, so none of them are my own words."
- "use the send message tool, don't pretend to be me."
- "i don't think we can reliably safeguard against users shooting themselves in the foot. best we can do is warn people."

### Daemon and architecture

- "the daemon should be dumb but durable… the Damon should provide useful hooks for things that agents can't do, or shouldn't do. it should be as flexible as we can make it, but useful."
- "everything should run through a workflow. those are the rules that are defined by the user/owner. rhiza ships with sensible defaults but should be flexible. the only thing we enforce is the references between nodes."
- "we should be able to have bug fixes without a milestone. smaller features without a milestone. hell even no milestones at all if people don't want that. autonomy should be configurable on a workflow by workflow basis."
- "A systemd scope seems like overkill to me." (portability; Rhiza also runs on macOS)
- "would IT make sense to separate the daemon and web processes?… being able to rhiza in pieces"
- "I don't feel like we should ever have anything uncommitted on main like this. Everything should get it's own branch."

### Gates and releases

- Two owner gates, plan acceptance and ship sign-off: "Yes those are perfect, but I do want to have an autonomous mode that skips those gates."
- "Fully automatic for minor and patch, owner gated for major releases"; "Batched, unless the bugfix is marked as severe".
- "if its not green here IT should never go too Ci"; "when we're diagnosing flaky tests just firing away at ci… hopping for the best doesn't make sense".

### Visibility, UI and noise

- "I like the visibility of being able to see what is happening on a run easily. You can with sub agents but it's not as easy."
- "It's just noise. There's nothing actionable for me, the user, to do or see. It's just taking up space."; "Once they're done they should be closed immediately".
- "multiple small apps. single purpose. i don't want the tui to dominate the terminal. think in and out."
- On what the web UI is for, agreed in the interview: "does anything need me?". Every screen phone-first.
- "Rhiza — 'Bioluminescent soil.' Dark-first, calm, precise… No gradients, no emoji."
- "I'm a hard core TUI user, and super developer nerd".

### IDs and naming

- "The full text would have been more helpful than the internal handle. No need to ever show the internal handle, it's nonsense to a user."
- "I don't like that 'genesis m2 cs9' is the default name for the tab. When working on a changeset it would be useful to have a more human readable name."
- "This should also be alled 'autoUpdate'… so it makes sense and aligns with the rest of the world".
- The titles complaint: "The 'titles' seem to be a description and just run off. It's not informative at all."

### Simplicity and flexibility

- "I think that key should be configurable… but ctrl-a>a is great as a default."
- "The scaffold should have extensive comments for each item with multiple examples for popular languages so the developer can easily scan through".
- "some of the examples are a little rough (using giant json blobs in cli commands, etc)".
- "we are pre 1.0 and im the only one using it so its not really breaking."
- "Any non Rhiza projects that want self update can do so through their own custom workflow."

### Bring your own

- "people should be able to bring their own workflows"
- "What if they want every conduct session to be invoked with something like '/myteam:conduct --prompt ${filename}'… How do they get the slice?"
- "users can bring their own LLM so we need to leave that open."

---

## 6. Implications for the successor: do and don't

Each item cites the evidence above.

1. **DO make "needs a human" a counted, first-class metric from day one.** Count interventions, clean streak and time held. Don't let "no orchestrator" be a slogan. (F1: six declarations; the measure arrived on Nov 18; `clean_streak` is 0 today.)
2. **DO treat every script the owner wrote around v1 as a requirements list for the successor's runtime:**
   - stall and idle detection, with the background-job distinction
   - closing finished sessions
   - resource holds (memory, tmp, load)
   - auto-sized concurrency
   - retry from the right step
   - notes for planners
   - detecting pending permission or usage prompts

   Build these into the supervisor, not as prose rules or a watchdog over your own daemon. (F2, F5; arch audit §8.6.)
3. **DON'T run many agent processes on one shared host without a process supervisor that owns their lifecycle and resource limits.** Six resource incidents in 17 days, seven hygiene packages added late, and an OOM that killed every session. For a hosted team layer, isolate per run (container or cgroup). For the open-source core, use portable holds. The owner rejected Linux-only scopes: "A systemd scope seems like overkill to me." (F5, F13)
4. **DO keep the knowledge core small, pure and separate from orchestration.** Enforce the boundary with a dependency check in CI. The core had about 13 fixes and orchestration 108. (W1, F7)
5. **DON'T let work exhaust become product knowledge.** Sessions, actors, attempts, findings and test evidence go to an expiring work log. Only reviewed facts (decisions, intents, constraints, sources) become durable nodes. Decide retention and compaction before writing the store. (F6: 76% of nodes are process; 127–141 MB in a month; only 20.8% of knowledge carried flow.)
6. **DO keep provenance as the trust spine.** Every decision records owner words vs picked option vs assumed, plus who acted and for whom. Developer trust is the successor's wedge, and this was v1's most trusted mechanism. (W3, W4)
7. **DO build rollback and undo before broad autonomy.** v1 never answered `autonomous-undo` in 24 days. A successor promising "speed with confidence, rollback" needs:
   - reversible accepts
   - partial supersede that keeps context visible
   - a per-change revert path tied to the decision that caused it

   (F10)
8. **DO model reversals explicitly.** Supersede with a reason, a partial or amend relation, and a "why did this change" view. v1's biggest reversals (R1, R5, R6) are nowhere in the decision graph: only 24 supersedes links. (Section 4 meta-lesson)
9. **DON'T ship a heavy default workflow.** Start with the fewest steps that hold up, and measure before adding a step or a reviewer. Each check must earn its place by its confirmed-finding rate. The scope check found 0 confirmed findings in 8 runs; 56 steps took about 75 minutes against afk's 37.6. (F4, R7)
10. **DO give agents rich, checklist-grade role prompts and keep them as versioned defaults.** v1 lost quality when afk's prompts were cut to one line and had to bring them back. (R2, prompt-gap.md)
11. **DO make the daemon dumb but durable, with agents doing the judgment, and hand every red result back to the agent that owns the change.** This is the owner's settled view, and it worked on Nebari. (W7, R1)
12. **DON'T build UI surfaces before naming the user's one question.** The answer turned out to be "does anything need me?", phone-first, with few safe actions. Prototype against real usage, not data dumps. Two TUI generations and two web UI generations happened in 15 days. (F8, R3, R10)
13. **DON'T show internal IDs or handles on human surfaces.** Use full text and human-readable titles. Short, real titles are a data-model requirement, not a UI fix. Keep IDs for the CLI and JSON. (R6; the titles complaint)
14. **DO make autonomy configurable per workflow and per stage, and visible in the UI.** Default to asking: "[full autonomy] should only happen when asked for". Offer warnings rather than hard fences ("warn, don't safeguard"), but keep the human in milestone and product-direction decisions. (Section 5 autonomy quotes, R4, R5, F9)
15. **DO guard against rubber-stamping.** 88% of picks were the recommended option, and 127 of 281 owner-quoted decisions were blanket approvals. Ask fewer, higher-stakes questions. Batch the low-stakes ones as reviewable assumptions, and surface the decisions an agent made "for" the owner, so the owner can audit them later. (W5, F9)
16. **DON'T commit model bookkeeping as separate PRs or CI rounds.** 24% of PRs were "Render the plan". Model writes should ride with the change or live outside git history. Rendered output should be derived on demand. The owner asked whether the rendered markdown is "useful at all". (F6, F8)
17. **DO keep CI green as a precondition, with a local gate and flake quarantine.** Never use CI as a debugger. Pick one stable rule early rather than rewriting the gate rules three times in four days. (F11, R12)
18. **DO set targets with a measurement plan and close milestones honestly.** M36's targets were never measured and M38 closed with its target unmet. A "confidence" product must measure its own claims. (F12, W9)
19. **DO keep the surface small.** v1 reached 214 operations, 58 settings, 10 node kinds, 15 link types and 51 journal event types in a month, while only 14 constraints were ever written. Start with about 3 node types and grow from real queries. (F7; arch audit §8.4 and §8.7)
20. **DO design for bring-your-own from the start:** agent, skills, workflows, LLM and invocation template. Put every daemon guarantee (limits, stall recovery, retry) on generic steps, not built-in step names. The owner raised this four times. (Section 5, bring your own)
21. **DO validate on a second, different repo early, and build import before claiming brownfield support.** Nebari was the only second user: about 10 real changes, `rhiza import` never built, Rails flakes dominated. A team product needs a non-owner pilot. (F14)
22. **DON'T let agents act in the real environment from tests or verification:**
    - no real runs started from inside sessions
    - no test writes into real roots
    - no installs that follow symlinks into user config

    Make scratch environments the default. (F13)
23. **DO keep "visibility of what's happening" cheap and quiet.** Show spawned sessions the owner can open on demand. Never leave noise tabs or panes, and close finished work automatically. (W6, R8)
24. **DON'T plan dozens of milestones ahead of evidence.** M7–M21 were 15 milestones in about 27 hours, picked by the orchestrator. Eleven founding Area umbrella change sets were never built and were withdrawn on day 27. Plan the next slice from measured pain. (Phase C, R16)
25. **DO keep naming human and conventional,** and treat renames as clean breaks done early. The Genesis-to-Rhiza rename at 33 hours cost one PR of 1,106 files. Doing it later would have cost far more. (R14, R17)
