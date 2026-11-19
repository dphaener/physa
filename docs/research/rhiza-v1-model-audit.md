# Rhiza model flow audit: `rhiza`

Repo: `/home/rhiza/code/rhiza` · generated 2026-11-19T04:46:39Z · latest activity 2026-11-19

Source of truth: `.rhiza/log/<change-set>/<entry>.json` (event-sourced op log). This report folds each change set (ops revised by op id, `drop` removes ops, lifecycle open→propose→accept→land) and applies only **landed** change sets. `rendered/` is a derived projection; it is used here only for on-disk sizes.

Log: 27175 entries in 7515 change sets; states: intention/draft 48, intention/landed 359, intention/withdrawn 35, record/draft 1, record/landed 7072. Git commits scanned: 674.

## 1. Counts and size per kind

| kind | category | live nodes | % of nodes | retired | rendered files | rendered size | log bytes (attributed) |
|---|---|---|---|---|---|---|---|
| capability | knowledge | 1062 | 8.2% | 52 | 1062 | 2.1MB | 1.6MB |
| intent | knowledge | 152 | 1.2% | 0 | 152 | 345.3KB | 81.1KB |
| decision | knowledge | 1247 | 9.7% | 0 | 1247 | 1.8MB | 1.1MB |
| constraint | knowledge | 14 | 0.1% | 0 | 14 | 9.0KB | 6.1KB |
| question | knowledge | 323 | 2.5% | 0 | 323 | 498.1KB | 355.6KB |
| source | provenance | 251 | 1.9% | 0 | 251 | 674.4KB | 605.2KB |
| actor | process | 6980 | 54.0% | 2 | 6980 | 2.6MB | 5.7MB |
| evidence | process | 2846 | 22.0% | 0 | 2846 | 4.4MB | 2.3MB |
| note | process | 42 | 0.3% | 0 | 42 | 56.2KB | 29.1KB |
| link (ops) | graph | 8476 |  | 9 |  |  | 5.7MB |
| lifecycle entries | process |  |  |  |  |  | 5.4MB |

| category | nodes | % nodes | bytes (rendered+log) | % bytes |
|---|---|---|---|---|
| knowledge | 2798 | 21.7% | 7.9MB | 22.5% |
| provenance | 251 | 1.9% | 1.2MB | 3.5% |
| process | 9868 | 76.4% | 20.4MB | 57.8% |
| graph(links) |  | 0.0% | 5.7MB | 16.1% |

Total `.rhiza/`: 36.2MB of content in 40107 files; 162.3MB allocated on disk (per-file block overhead; the log alone is 27175 small JSON files, 110.3MB allocated).

## 2. Reference flow per knowledge node

References counted: inbound links from knowledge nodes and change sets (`in_knowledge`), inbound links from process nodes such as evidence `verifies` (`in_process`), `@{ID}` text refs, `Leaf:` trailers and handle/ID mentions in git commit messages, files in code/docs (git ls-files, excluding .rhiza) naming the handle or ID (text matches of handles with only two words, e.g. `close-out`, collide with prose and are kept in a separate `weak_text_hits` column, excluded from scores), and later change sets that updated/moved/retired the node. `later` = arrived in a change set other than the one that created the node.

Score = 3·in_knowledge_later + 1·in_knowledge_same_cs + 0.25·min(in_process,8) + 0.5·min(git_own,5) + 2·min(git_leaf_ext,5) + min(git_mention_ext,10) + 2·min(code_files,10) + 2·min(doc_files,10) + 2·later_change_set_touches + min(children,10) (+0.5·outbound decided-by for capabilities/intents).

**Sustained** references are the ones that show a node being used beyond the change set that created and built it: inbound knowledge links or `@{ID}` refs written by a later change set, later change sets updating/moving it, git commits naming it whose `Change:` trailer is *not* its creating change set, and code/doc files naming it. Evidence `verifies` links, same-change-set links and the node's own build commits (`Leaf:` trailers under its creating change set) are build-cycle/process refs.

Classes: **never** = no references of any kind; **cold** = only build-cycle/process refs (sustained = 0); **hot** = sustained > 0 and score ≥ 10.0 (75th pct of sustained nodes); **warm** = the rest.

| kind | nodes | hot | warm | cold | never | % carrying flow (hot+warm) |
|---|---|---|---|---|---|---|
| capability | 1062 | 50 | 168 | 823 | 21 | 20.5% |
| intent | 152 | 28 | 43 | 69 | 12 | 46.7% |
| decision | 1247 | 72 | 206 | 695 | 274 | 22.3% |
| constraint | 14 | 1 | 4 | 1 | 8 | 35.7% |
| question | 323 | 0 | 9 | 305 | 9 | 2.8% |
| **all knowledge** | 2798 | 151 | 430 | 1893 | 324 | 20.8% |

Flow concentration: top 5% of knowledge nodes hold 32.1% of total score, top 10% 45.7%, top 20% 62.4%.

Knowledge nodes named in git only by their own build commits: 1053 (37.6%). Named outside their own build cycle (other commits, or code/docs): 200 of 2798 (7.1%). Referenced by a later change set or later knowledge link: 476 (17.0%).

Mean references per node by kind:

| kind | n | in_knowledge | …later | in_process | git (own build) | git (other) | code_files | doc_files | later cs touches | sustained |
|---|---|---|---|---|---|---|---|---|---|---|
| capability | 1062 | 0.55 | 0.06 | 3.13 | 0.90 | 0.13 | 0.06 | 0.00 | 0.16 | 0.42 |
| intent | 152 | 3.14 | 1.65 | 0.08 | 0.01 | 0.04 | 0.00 | 0.00 | 0.18 | 1.87 |
| decision | 1247 | 1.63 | 0.58 | 0.00 | 0.08 | 0.02 | 0.03 | 0.00 | 0.01 | 0.63 |
| constraint | 14 | 0.14 | 0.14 | 0.00 | 0.14 | 0.21 | 0.86 | 0.00 | 0.00 | 1.21 |
| question | 323 | 0.99 | 0.01 | 0.02 | 0.10 | 0.02 | 0.01 | 0.00 | 0.00 | 0.03 |

## 3. Capability granularity

| depth | capabilities |
|---|---|
| 0 | 40 |
| 1 | 63 |
| 2 | 219 |
| 3 | 465 |
| 4 | 223 |
| 5 | 51 |
| 6 | 1 |

Leaves: 943 of 1062 (88.8%). Root-level capabilities (no parent): 40. Parents: 119; median children per parent: 3; max: 53.

Ticket-sized (leaf + ≥3 of: single change set & never touched again, statement < 220 chars, handle ≥4 words, ticket-ish word such as deleted/scroll/sized/fix, no parent, nothing depends on it): **482** of 1062 (45.4%).

| flag | capabilities |
|---|---|
| leaf | 943 |
| single-change-set | 918 |
| nothing-depends | 623 |
| long-handle | 442 |
| short-statement | 350 |
| ticket-word | 131 |
| orphan-root | 40 |

Sample ticket-sized capabilities (highest ticket score, lowest flow):

| handle | path | flags | score |
|---|---|---|---|
| budget-timings-in-ci | rhiza/views/fast-reads/budget-timings-in-ci | leaf,single-change-set,short-statement,long-handle,ticket-word,nothing-depends | 0.25 |
| evidence-commit-waits-index-lock | evidence-commit-waits-index-lock | leaf,single-change-set,long-handle,ticket-word,orphan-root,nothing-depends | 1.0 |
| blocked-session-still-waits | rhiza/orchestration/step-messages/blocked-session-still-waits | leaf,single-change-set,short-statement,long-handle,ticket-word,nothing-depends | 1.25 |
| link-endpoint-kinds-fast | link-endpoint-kinds-fast | leaf,single-change-set,long-handle,ticket-word,orphan-root,nothing-depends | 1.5 |
| evidence-commit-survives-moved-head | evidence-commit-survives-moved-head | leaf,single-change-set,long-handle,ticket-word,orphan-root,nothing-depends | 1.5 |
| every-budget-opt-in | every-budget-opt-in | leaf,single-change-set,short-statement,long-handle,orphan-root,nothing-depends | 1.5 |
| headless-tabs-builder-waits-index-lock | headless-tabs-builder-waits-index-lock | leaf,single-change-set,long-handle,ticket-word,orphan-root,nothing-depends | 1.5 |
| byo-failed-run-retry | rhiza/workflows/workflow-runs/byo-failed-run-retry | leaf,single-change-set,short-statement,long-handle,ticket-word,nothing-depends | 1.5 |
| run-status-live-poll-waits-for-hold | run-status-live-poll-waits-for-hold | leaf,single-change-set,long-handle,ticket-word,orphan-root,nothing-depends | 1.5 |
| check-actor-records-committed | check-actor-records-committed | leaf,single-change-set,short-statement,long-handle,orphan-root,nothing-depends | 1.75 |
| leaf-fix-exhaustion-spends-cycle | fixer-exhaustion-spends-cycle/leaf-fix-exhaustion-spends-cycle | leaf,single-change-set,short-statement,long-handle,ticket-word,nothing-depends | 1.75 |
| plan-gate-open-items | rhiza/workflows/built-in-workflows/plan-workflow/plan-gate/plan-gate-open-items | leaf,single-change-set,short-statement,long-handle,ticket-word,nothing-depends | 1.75 |
| failed-step-detail-shown | rhiza/workflows/workflow-runs/run-status/failed-step-detail-shown | leaf,single-change-set,short-statement,long-handle,ticket-word,nothing-depends | 1.75 |
| close-out-retry-after-forget | rhiza/orchestration/shipping/close-out-retry-after-forget | leaf,single-change-set,short-statement,long-handle,ticket-word,nothing-depends | 1.75 |
| watchdog-holds-skip-abandoned | rhiza/orchestration/daemon/daemon-watchdog/watchdog-holds-skip-abandoned | leaf,single-change-set,short-statement,long-handle,ticket-word,nothing-depends | 1.75 |
| apply-tolerates-concurrent-entries | apply-tolerates-concurrent-entries | leaf,single-change-set,long-handle,ticket-word,orphan-root,nothing-depends | 1.75 |
| inbox-skips-run-accepted-gated | inbox-skips-run-accepted-gated | leaf,single-change-set,long-handle,ticket-word,orphan-root,nothing-depends | 1.75 |
| code-check-tolerates-branch-newer-format | code-check-tolerates-branch-newer-format | leaf,single-change-set,long-handle,ticket-word,orphan-root,nothing-depends | 2.0 |
| ci-wait-backs-off | ci-wait-backs-off | leaf,single-change-set,long-handle,ticket-word,orphan-root,nothing-depends | 2.0 |
| rex-tab-launch-retry | rhiza/runtime/rex-sessions/rex-tab-launch/rex-tab-launch-retry | leaf,single-change-set,short-statement,long-handle,ticket-word,nothing-depends | 2.0 |
| inbox-app-retry-abandon | rhiza/inbox-app-retry-abandon | leaf,single-change-set,short-statement,long-handle,ticket-word,nothing-depends | 2.5 |
| rex-app-layer-sized | rhiza/rex-app-layer-sized | leaf,single-change-set,short-statement,long-handle,ticket-word,nothing-depends | 2.75 |
| claude-trust-failure-warns | rhiza/runtime/agent-adapters/claude-trust-failure-warns | leaf,single-change-set,short-statement,long-handle,nothing-depends | 1.25 |
| inbox-lists-gate-stopped-plan-run | rhiza/inbox-lists-gate-stopped-plan-run | leaf,single-change-set,long-handle,ticket-word,nothing-depends | 1.25 |
| retry-prompt-quotes-findings | rhiza/workflows/built-in-workflows/build-workflow/retry-prompt-quotes-findings | leaf,single-change-set,long-handle,ticket-word,nothing-depends | 1.25 |
| build-leaf-split-trailers | rhiza/workflows/built-in-workflows/build-workflow/build-leaf-split-trailers | leaf,single-change-set,short-statement,long-handle,nothing-depends | 1.5 |
| review-fixers-in-rex | rhiza/workflows/workflow-runs/run-program-status/review-fixers-in-rex | leaf,single-change-set,short-statement,long-handle,nothing-depends | 1.5 |
| review-code-check-in-rex | rhiza/workflows/workflow-runs/run-program-status/review-code-check-in-rex | leaf,single-change-set,short-statement,long-handle,nothing-depends | 1.5 |
| clean-render-adds-no-commit | rhiza/workflows/clean-render-adds-no-commit | leaf,single-change-set,short-statement,long-handle,nothing-depends | 1.5 |
| question-session-panes-close | rhiza/runtime/rex-sessions/question-session-panes-close | leaf,single-change-set,short-statement,long-handle,nothing-depends | 1.5 |

Proposed merge clusters (parents whose leaf children could fold into the parent; ranked by ticket-sized children):

| parent path | children | leaf children | ticket-sized | decisions shared by ≥2 leaves | change sets that created the leaves | sample leaves |
|---|---|---|---|---|---|---|
| rhiza | 53 | 38 | 26 | 13 | 15 | autonomy-app-toggles, builder-actor-in-leaf-commit, daemon-app-pause, daemon-app-run-limit, inbox-app-answer, inbox-app-answer-shows-question |
| rhiza/orchestration/shipping/wait-green-pr | 35 | 35 | 21 | 11 | 14 | ci-fix-fixes-ci-findings, ci-fix-reproduces-locally, ci-flakes-name-tests, ci-stability-record, ci-stability-state, ci-triage-infra-waits |
| rhiza/integrity/self-check | 52 | 52 | 20 | 13 | 25 | api-snapshot-fixtures, api-tests-copy-prebuilt-index, api-tests-free-disk-hermetic, api-tests-on-index, api-tests-parallel-finish, api-tests-parallel-model |
| rhiza/workflows/workflow-runs | 50 | 41 | 19 | 11 | 23 | abandon-failed-run, arbiter-advisor-brief, byo-failed-run-retry, byo-spawn-hold-intervention, byo-watchdog-stall-runaway, byo-workflow-daemon-e2e |
| rhiza/orchestration/daemon | 49 | 42 | 19 | 10 | 28 | auto-update-latest, auto-update-release, conduct-plans-leaves, daemon-adopts-inherited-children, daemon-auto-update, daemon-headless-run-drivers |
| rhiza/documentation | 29 | 29 | 19 | 7 | 11 | agents-doc-voice, cli-help-style, concepts-change-sets-voice, concepts-model-voice, concepts-why-voice, custom-workflows-doc-voice |
| rhiza/orchestration/shipping | 30 | 29 | 18 | 8 | 16 | ci-logs-to-fixer, close-out, close-out-drops-log-copies, close-out-retry-after-forget, commit-untracked-log, conductor-land-render-head-build |
| rhiza/workflows/built-in-workflows/plan-workflow | 43 | 41 | 16 | 13 | 12 | findings-name-links, leaf-contract-fields, leaf-contract-review-check, no-failure-case-kind, plan-autonomous-interview, plan-clean-round-skips-critic-step |
| rhiza/web-monitor | 37 | 34 | 15 | 8 | 12 | web-action-answer, web-action-pause, web-api-read-only, web-change-set-detail, web-change-set-page, web-csrf |
| rhiza/views/fast-reads | 30 | 30 | 15 | 6 | 12 | api-tests-tmp-leak-guard, apply-validates-from-index, budget-timings-in-ci, budgets-measure-in-quiet, change-reads-from-projection, change-show-ship-check-from-index |
| rhiza/orchestration/daemon/daemon-watchdog | 23 | 23 | 12 | 3 | 9 | flaky-test-bugs, poll-behind-names-steps, poll-overrun-logged-live, runaway-session-fails-step, runaway-threshold-keys, session-process-tree-reading |
| rhiza/change-sets | 21 | 13 | 11 | 0 | 6 | acyclic-dependencies, apply-files, carve, carve-dependency, carve-entries, carve-guards |
| rhiza/runtime/rex-sessions | 12 | 11 | 11 | 2 | 7 | finished-session-panes-close, headless-run-agent-tabs, question-session-panes-close, session-commands, session-hook-denies-log-edits, session-start-prompt-not-submitted |
| rhiza/guide | 19 | 18 | 9 | 6 | 9 | command-budget-guide, command-schemas, done-own-command-guide, guide-install, guide-install-concurrent-first, guide-size-cap |
| rhiza/orchestration/inbox | 19 | 18 | 8 | 4 | 12 | inbox-advisor, inbox-blockers-cheaper, inbox-blocking-questions, inbox-cheap, inbox-clears-stale, inbox-landed-plan-run-in-flight |
| rhiza/orchestration/step-messages | 9 | 9 | 8 | 2 | 4 | blocked-session-still-waits, busy-session-hook-delivery, idle-typed-pointer-kept, message-file, message-file-in-session-checkout, message-says-why-waiting |
| rhiza/orchestration/daemon/daemon-picks | 14 | 13 | 7 | 2 | 11 | continue-slot-kept, daemon-disk-guard, daemon-load-memory-hold, daemon-picks-backlog, daemon-resume-takes-slot, daemon-skips-shipped |
| rhiza/distribution | 18 | 18 | 6 | 5 | 9 | auto-release, breaking-detection, channel-resolver, install-script, latest-build-version, latest-channel-prerelease |
| rhiza/workflows/workflow-authoring | 15 | 15 | 6 | 5 | 5 | skill-check-builder, skill-check-verifier-check, workflow-check, workflow-check-cleanup, workflow-check-merge-no-gate, workflow-check-risk-warning |
| rhiza/views | 13 | 7 | 6 | 0 | 5 | change-feed-follow, node-list, node-show, overlaps-command, overlays, slice |
| rhiza/integrity/check | 12 | 12 | 6 | 4 | 7 | check-entry-identity, check-inflight-links-own-only, check-log-decode-cheaper, check-warns-in-flight-link-kinds, check-warns-orphan-log, check-warns-overlaps |
| rhiza/orchestration/daemon/daemon-status | 7 | 6 | 6 | 0 | 6 | daemon-status-load-memory, daemon-status-queued-continuation, daemon-status-runs-cap, daemon-status-skips, daemon-status-version, maxruns-auto-shown |
| rhiza/orchestration/usage-governor | 14 | 14 | 5 | 4 | 5 | milestone-request-past-usage-hold, root-daemon-usage-governor, usage-hit-teaches-its-kind, usage-hold, usage-hold-reported, usage-learned-limit |
| rhiza/orchestration/daemon/plan-requests | 12 | 9 | 5 | 5 | 7 | milestone-request-starts-at-once, milestone-wait-owner-steps, plan-run-carries-ref, plan-run-hands-milestone, queued-start-request-slot-kept, request-names-milestone |
| rhiza/model | 11 | 5 | 5 | 1 | 3 | capability-tree, format-compat, handles, node-ids, six-operations |
| rhiza/workflows/built-in-workflows/build-workflow | 19 | 17 | 4 | 6 | 5 | build-check, build-evidence-commit, build-leaf-commit, build-leaf-fails, build-leaf-order, build-leaf-split-trailers |
| rhiza/workflows/built-in-workflows | 17 | 14 | 4 | 5 | 7 | builtin-conductor-default, builtin-ship-change-classic, classic-tests-pinned, critic-on-major, default-tests-follow-conductor, docs-conductor-default |
| rhiza/orchestration | 11 | 4 | 4 | 2 | 1 | daemon-status-across-projects, health-across-projects, inbox-across-projects, roadmap-across-projects |
| rhiza/lineage | 7 | 4 | 4 | 0 | 3 | change-history, interview-provenance, scrubbing, source-add |
| rhiza/orchestration/notifications | 7 | 7 | 4 | 3 | 4 | agent-tab-blocked-notice, blocked-run-notice, blocked-run-status, inbox-notify-hook, milestone-interview-waiting-at-launch, rex-banner-upstream-draft |

If every parent with ≥3 leaf children absorbed them, 847 leaf capabilities would collapse into 71 parents (1062 → ~215 capability nodes).

## 4. Decisions

| metric | count | % |
|---|---|---|
| live decisions | 1247 |  |
| retired decisions | 0 |  |
| superseded (target of a `supersedes` link) | 24 | 1.9% |
| in a near-duplicate pair (word Jaccard ≥ 0.5; 19 pairs) | 34 | 2.7% |
| cited after creation (later link/touch, git message, code/docs) | 278 | 22.3% |
| cited by no capability/intent/decision at all (no inbound knowledge link) | 323 | 25.9% |
| linked only to a question it answers (answers-out, no inbound) | 322 |  |
| no link to code (no path in text, no code/doc/git mention, no built capability via decided-by) | 527 | 42.3% |

Decision origin (fields present):

| origin | count |
|---|---|
| other | 736 |
| owner quote | 251 |
| answer to a run question | 248 |
| options/picked | 12 |

Top near-duplicate decision pairs:

| jaccard | a | b |
|---|---|---|
| 0.86 | answer-41425f42-19 | answer-9eeeaa0d-7 |
| 0.72 | run-reports-progress | run-ends-blocked-on-review-question |
| 0.72 | t1-d10-makefile-targets | makefile-targets-check-minus-budgets |
| 0.71 | fixer-retries-within-cycle | fixer-retries-within-cycle-v2 |
| 0.66 | ci-unstable-gate-rules | local-gate-trigger-identified-flakes |
| 0.66 | import-load-bearing-decisions | import-load-bearing-decisions-owner-credited |
| 0.64 | unstable-ci-in-role-topics | delay-repro-in-role-topics |
| 0.6 | conduct-handback-routes | conduct-handback-routes-ship-rebase |
| 0.58 | native-rex-prefix-configurable | rex-app-keys-default-overridable |
| 0.57 | m2-scope-in | m2-eight-change-sets |
| 0.57 | t7-d13-tests | t10-d4-tests |
| 0.53 | command-budget-in-role-topics | done-own-command-in-role-topics |
| 0.53 | refuse-run-start-in-agent-session | resume-refuses-agent-session |
| 0.53 | ship-change-file-names-render | ship-change-file-inherits-render |
| 0.52 | local-gate-trigger-identified-flakes | local-gate-repeated-named-flakes |
| 0.5 | conductor-plans-leaves | m40-leaves-per-change-set |
| 0.5 | answer-9eeeaa0d-7 | answer-e3a4887c-12 |
| 0.5 | answer-41425f42-19 | answer-e3a4887c-12 |
| 0.5 | split-another-live-pane | run-window-gone-opens-window |

## 5. Questions

| metric | value |
|---|---|
| live questions | 323 |
| answered (has an `answers` link) | 310 (96.0%) |
| open | 13 (4.0%) |
| median days to answer | 0.0 |
| median age of open questions (days) | 23.3 |
| open older than 7 days | 12 |
| auto-generated handles (question-*, review-*) | 228 |

Oldest open questions:

| handle | created | age (d) | statement |
|---|---|---|---|
| area-gap | 2026-10-25 | 24.5 | A change set cannot add capabilities under an area whose change set has not landed yet, so the follo |
| carve-stale-base-recovery | 2026-10-26 | 24.1 | After a carve, how does a change set that depends on the source clear an op whose base names a moved |
| idle-step-blocked | 2026-10-26 | 23.5 | Should an agent step idle at its prompt count as blocked on the owner? |
| message-recipient-session | 2026-10-26 | 23.4 | SendMessage names its recipient as Claude Code knows it, not as a Rhiza session ID. Should the journ |
| session-message-journaled | 2026-10-26 | 23.4 | Should messages sent into a run's step with rhiza session message (step-messages), not through SendM |
| autonomous-undo | 2026-10-26 | 23.4 | How is something an autonomous run did undone: an accept made under rule autonomous, or a Decision r |
| done-point-evidence | 2026-10-26 | 23.3 | How does Evidence show that a done point (an intent under a milestone) is met? verifies goes only fr |
| serves-intent-link | 2026-10-29 | 20.2 | The change set's serves link points at 01M5SWT6HKYEKHXJAQT000NSDA, which no longer resolves (carved  |
| rex-banner-report-ready | 2026-11-04 | 14.9 | docs/rex-upstream/notification-banner.md drafts a report for Rex upstream (VM chimes, no banner on t |
| leaf-failed-01m6w86n-2 | 2026-11-08 | 11.1 | Leaf bugfix-workflow-autonomous failed all 3 attempts of round 2: attempt 4 builder failed: no new c |
| rebase-conflict-fix-a-change-set-that-updates | 2026-11-08 | 10.5 | Rebasing branch cs/fix-a-change-set-that-updates onto origin/main conflicts outside the rendered fil |
| rebase-conflict-fix-request-plan-runs-start | 2026-11-08 | 10.3 | Rebasing branch cs/fix-request-plan-runs-start onto origin/main conflicts outside the rendered files |
| fixer-lint-section-over-topic-cap | 2026-11-13 | 5.3 | Leaf fixer-lint-hits-guide: verifier finding f1 requires restoring the fixer topic verbatim ("the re |

## 6. Growth over time

| day | capability | intent | decision | constraint | question | source | actor | evidence | note | links | updates/moves | retired | cumulative live nodes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 2026-10-25 | 55 | 8 | 308 | 7 | 1 | 14 | 5 | 6 | 0 | 859 | 1 | 0 | 404 |
| 2026-10-26 | 102 | 4 | 66 | 0 | 13 | 1 | 19 | 64 | 0 | 396 | 4 | 0 | 673 |
| 2026-10-27 | 34 | 0 | 74 | 0 | 2 | 3 | 28 | 52 | 0 | 331 | 0 | 0 | 866 |
| 2026-10-28 | 15 | 4 | 44 | 0 | 9 | 1 | 94 | 70 | 0 | 233 | 0 | 1 | 1102 |
| 2026-10-29 | 47 | 9 | 36 | 2 | 27 | 24 | 302 | 86 | 0 | 258 | 8 | 1 | 1634 |
| 2026-10-30 | 6 | 3 | 23 | 0 | 2 | 1 | 229 | 98 | 0 | 163 | 4 | 0 | 1996 |
| 2026-10-31 | 59 | 6 | 35 | 0 | 1 | 5 | 248 | 113 | 0 | 286 | 5 | 0 | 2463 |
| 2026-11-01 | 35 | 11 | 32 | 1 | 0 | 11 | 274 | 117 | 0 | 246 | 2 | 0 | 2944 |
| 2026-11-02 | 15 | 2 | 12 | 0 | 4 | 2 | 157 | 66 | 0 | 131 | 0 | 1 | 3201 |
| 2026-11-03 | 62 | 6 | 52 | 0 | 20 | 15 | 281 | 72 | 3 | 316 | 11 | 7 | 3705 |
| 2026-11-04 | 46 | 5 | 48 | 1 | 32 | 19 | 432 | 141 | 0 | 387 | 13 | 2 | 4427 |
| 2026-11-05 | 59 | 13 | 46 | 1 | 20 | 11 | 509 | 199 | 0 | 503 | 35 | 24 | 5261 |
| 2026-11-06 | 43 | 6 | 33 | 0 | 18 | 8 | 371 | 160 | 0 | 367 | 9 | 4 | 5896 |
| 2026-11-07 | 73 | 6 | 44 | 1 | 27 | 12 | 341 | 123 | 7 | 423 | 11 | 0 | 6530 |
| 2026-11-08 | 48 | 0 | 33 | 0 | 34 | 17 | 513 | 193 | 4 | 422 | 9 | 3 | 7369 |
| 2026-11-09 | 5 | 0 | 5 | 0 | 6 | 3 | 230 | 88 | 0 | 137 | 5 | 0 | 7706 |
| 2026-11-10 | 18 | 4 | 13 | 1 | 3 | 6 | 141 | 49 | 0 | 114 | 2 | 0 | 7941 |
| 2026-11-11 | 65 | 13 | 65 | 0 | 13 | 12 | 191 | 38 | 3 | 270 | 5 | 8 | 8333 |
| 2026-11-12 | 60 | 12 | 49 | 0 | 19 | 14 | 865 | 341 | 3 | 638 | 40 | 2 | 9694 |
| 2026-11-13 | 110 | 15 | 73 | 0 | 36 | 27 | 776 | 312 | 0 | 850 | 33 | 1 | 11042 |
| 2026-11-14 | 10 | 0 | 12 | 0 | 5 | 2 | 132 | 78 | 0 | 125 | 0 | 0 | 11281 |
| 2026-11-15 | 46 | 13 | 53 | 0 | 10 | 11 | 198 | 76 | 15 | 331 | 3 | 0 | 11703 |
| 2026-11-16 | 14 | 0 | 14 | 0 | 3 | 3 | 136 | 65 | 1 | 143 | 0 | 0 | 11939 |
| 2026-11-17 | 27 | 12 | 29 | 0 | 5 | 11 | 197 | 94 | 0 | 226 | 12 | 0 | 12314 |
| 2026-11-18 | 57 | 0 | 47 | 0 | 13 | 17 | 292 | 137 | 6 | 315 | 1 | 0 | 12883 |
| 2026-11-19 | 3 | 0 | 1 | 0 | 0 | 1 | 21 | 8 | 0 | 15 | 0 | 0 | 12917 |

Created 12971 nodes; retired 54 (0.4%); supersedes links 36; retired links 9. Withdrawn change sets: 35. Retirements by kind: {'capability': 52, 'actor': 2}.

Most common retire reasons (first 40 chars): 'its app was deleted in this change set d'×15; 'replaced by headlessrunagenttabs every r'×6; 'replaced by the request app on the modal'×2; 'replaced by cilogstofixer citriage hands'×2; 'shipchange no longer has an agent step r'×1; 'replaced by shipheadbuildstep rhizas own'×1; 'superseded by rootdaemonlock the daemon '×1; 'replaced by daemonheadlessrundrivers the'×1

## 7. v2 seed: top 50 knowledge nodes by flow

| # | kind | handle | score | refs | sustained | in_k | git own/other | code/doc files | id | statement |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | intent | milestone-25 | 188.5 | 66 | 61 | 66 | 0/0 | 0/0 | 01M6HKVRE1 | The backlog is empty: every change set planned from the owner's queued requests and the ba |
| 2 | intent | m4-runs-hold-up | 102.5 | 34 | 34 | 34 | 0/0 | 0/0 | 01M61R4RRY | A build, review or ship-change run finishes without hand repair: no hand-made render commi |
| 3 | decision | projection-scope-approved | 64.0 | 22 | 21 | 22 | 0/0 | 0/0 | 01M7GFT06M | Milestone 39 holds four change sets, in this order: a parity harness with internal/api's t |
| 4 | decision | afk-prompts-shipped-default | 58.0 | 20 | 19 | 20 | 0/0 | 0/0 | 01M76XMNKR | afk's shipit/forge prompts become Rhiza's shipped default prompts for every run role, and  |
| 5 | decision | efficient-cycle-milestone-shape | 57.0 | 20 | 19 | 19 | 0/0 | 0/0 | 01M76XMNNP | Milestone 34 puts afk's prompts as shipped defaults and the guards first, then the CI-roun |
| 6 | decision | web-ui-in-daemon-read-only | 56.0 | 21 | 19 | 19 | 0/1 | 0/0 | 01M6PFEKAM | The first web UI is served by the daemon process on localhost, and that first pass was rea |
| 7 | intent | milestone-4 | 56.0 | 29 | 11 | 29 | 0/0 | 0/0 | 01M61R4Q8S | Rhiza orchestrates: milestone 5 is driven by rhiza daemon, with no orchestrator session, t |
| 8 | intent | milestone-2 | 49.0 | 13 | 13 | 12 | 0/0 | 0/0 | 01M5SZHJG0 | Genesis runs the work: milestone 3 is built with Genesis orchestrating, not an orchestrato |
| 9 | decision | fast-suite-proposal-approved | 47.0 | 19 | 14 | 19 | 0/0 | 0/0 | 01M7EQ7534 | The milestone is planned as the orchestrator proposed it on 2026-11-15: the targets as wri |
| 10 | decision | build-workflow-shape | 46.0 | 16 | 15 | 16 | 0/0 | 0/0 | 01M5WPTMRW | In the build workflow a change set's leaves are the tasks: a leaf is ready when the leaves |
| 11 | decision | review-workflow-shape | 43.0 | 15 | 14 | 15 | 0/0 | 0/0 | 01M5WPTPYR | The review workflow: a fresh session per check told only the change set ID; compliance, qu |
| 12 | intent | anyone-builds | 41.0 | 13 | 13 | 13 | 0/0 | 0/0 | 01M5SWRH3H | Anyone can build quality software with agents, using as little or as much of Genesis as th |
| 13 | decision | m2-scope-in | 40.0 | 14 | 13 | 14 | 0/0 | 0/0 | 01M5SZHNCW | In milestone 2: the Claude Code adapter, runs with the built-in ship-change workflow, mess |
| 14 | decision | m31-polish-request | 40.0 | 16 | 12 | 16 | 0/0 | 0/0 | 01M71G3TJG | Polish the docs: examples show plain commands, not JSON blobs, and the prose reads in the  |
| 15 | intent | milestone-3 | 38.5 | 9 | 9 | 9 | 0/0 | 0/0 | 01M5WPTJ9Z | Rhiza's own workflows: milestone 4 is planned, built and reviewed by Rhiza, with the owner |
| 16 | decision | m29-one-root-daemon | 38.0 | 14 | 12 | 14 | 0/0 | 0/0 | 01M6RN37AH | One rhiza daemon per machine runs on the root and drives every project's runs under one us |
| 17 | decision | plan-workflow-shape | 37.0 | 13 | 12 | 13 | 0/0 | 0/0 | 01M5WPTMEZ | The plan workflow is an agent step driven by a plan guide topic, planning a milestone or a |
| 18 | decision | tui-five-apps | 35.0 | 15 | 10 | 15 | 0/0 | 0/0 | 01M6MZ1M8T | The TUI is five apps: Inbox (pick an item and do its one action: answer a question or gate |
| 19 | capability | daemon-resume-once | 34.0 | 26 | 15 | 5 | 1/5 | 0/0 | 01M61R7MP3 | The daemon resumes an interrupted run whose change set is accepted, or whose change set ha |
| 20 | decision | tui-rexuikit-components | 34.0 | 12 | 11 | 12 | 0/0 | 0/0 | 01M6MZ1FTN | Every app draws its UI with rexuikit's ui components (Form with its Text, TextArea, Select |
| 21 | decision | conductor-milestone-direction | 34.0 | 12 | 11 | 12 | 0/0 | 0/0 | 01M79KKVPG | Rhiza's runs move from a daemon-driven workflow of about 56 steps to a conductor agent per |
| 22 | decision | autonomous-mode | 31.0 | 11 | 10 | 11 | 0/0 | 0/0 | 01M5WPTM52 | Autonomous mode skips the owner's gates and questions, as afk's --autonomous does: each qu |
| 23 | decision | verifier-writes-evidence | 31.0 | 11 | 10 | 11 | 0/0 | 0/0 | 01M5WPTN2W | Only a verifier that didn't write a leaf's code writes its Evidence; Evidence names its ac |
| 24 | decision | m3-scope | 31.0 | 11 | 10 | 11 | 0/0 | 0/0 | 01M5WPTQ8T | Milestone 3 covers the plan, build and review workflows, the arbiter and autonomous mode,  |
| 25 | capability | rhiza | 28.5 | 9 | 2 | 8 | 0/0 | 0/0 | 01M5SWRGYQ | Rhiza keeps the model of a product, what it is, why it is that way and what is planned, in |
| 26 | capability | ship-local-gate | 28.25 | 21 | 9 | 9 | 1/3 | 0/0 | 01M6YCRKT2 | While CI is unstable, the built-in ship-change workflow runs the project's local gate comm |
| 27 | decision | watchdog-scope-and-defaults | 28.0 | 20 | 7 | 14 | 0/0 | 6/0 | 01M6WHZY78 | (1) The watchdog is the daemon-level backstop; the three bug requests stay separate and sh |
| 28 | intent | m4-orchestrated-by-rhiza | 27.5 | 9 | 9 | 9 | 0/0 | 0/0 | 01M61R4RCY | Milestone 5's first change sets are shipped with rhiza daemon orchestrating and no orchest |
| 29 | decision | tui-many-mini-apps | 27.0 | 11 | 10 | 9 | 0/2 | 0/0 | 01M62JMM4G | The TUI is many small apps launched from a Rex quick-launch layer, installed by a command  |
| 30 | intent | model-tells-truth | 27.0 | 9 | 9 | 9 | 0/0 | 0/0 | 01M5SWRH47 | The model tells the truth about what the product is and why. |
| 31 | intent | milestone-24 | 27.0 | 10 | 6 | 10 | 0/0 | 0/0 | 01M6FEKX9J | Rhiza runs headless on a dedicated Linux VM: its daemon runs as a service beside Rex's Lin |
| 32 | intent | milestone-29 | 27.0 | 11 | 5 | 11 | 0/0 | 0/0 | 01M6RN3786 | Rhiza manages and builds several projects on one machine from one root directory: projects |
| 33 | decision | m30-humanize-rule | 26.0 | 10 | 8 | 10 | 0/0 | 0/0 | 01M6W5BQKP | Humanize is a standing rule for human-facing doc prose |
| 34 | capability | daemon-picks | 25.5 | 18 | 4 | 3 | 1/0 | 0/0 | 01M61R7K5W | Each poll, the daemon picks, in each project, the accepted change sets that rhiza ready sa |
| 35 | decision | arbiter | 25.0 | 9 | 8 | 9 | 0/0 | 0/0 | 01M5WPTPMQ | The engine calls an arbiter only at decision points (a session's question, a plan contradi |
| 36 | decision | strict-engine-is-spec | 23.0 | 9 | 7 | 9 | 0/0 | 0/0 | 01M7GFT08K | Where the parity test finds the model rules disagree, the strict engine in internal/model  |
| 37 | decision | four-workflows | 22.0 | 8 | 7 | 8 | 0/0 | 0/0 | 01M5WPTKG9 | Rhiza ships four workflows: plan (interview into the model, plan review, acceptance), buil |
| 38 | decision | m29-cli-and-tui-now | 22.0 | 8 | 7 | 8 | 0/0 | 0/0 | 01M6RN37KK | Both the CLI (daemon, inbox, roadmap, health) and the Rex TUI apps go multi-project in thi |
| 39 | decision | conductor-promote-builtin-now | 22.0 | 8 | 8 | 6 | 0/0 | 2/0 | 01M79KKVVA | The conductor default replaces the builtin ship-change at once, for every project that use |
| 40 | constraint | owner-surfaces-no-ids | 21.0 | 12 | 12 | 0 | 0/3 | 9/0 | 01M6PN1SW9 | No owner-facing surface (the Rex apps, notifications, the web UI) shows a node's handle, a |
| 41 | intent | m36-conductor | 21.0 | 8 | 6 | 8 | 0/0 | 0/0 | 01M79KKVJZ | One conductor session conducts a change set end to end: it plans the leaves when there are |
| 42 | capability | model | 20.0 | 0 | 0 | 0 | 0/0 | 0/0 | 01M5SWRGZD | Genesis describes a product as one model: a fixed set of node kinds, typed links between t |
| 43 | decision | projection-targets | 20.0 | 10 | 5 | 10 | 0/0 | 0/0 | 01M7GFT0BD | Milestone 39's own targets are rhiza status and rhiza inbox within 200 ms warm and rhiza c |
| 44 | capability | request-queue | 19.25 | 13 | 5 | 0 | 1/0 | 0/0 | 01M6D4SYDB | rhiza request <text> [--kind feature\|bug\|milestone] queues a plan request in the clone's . |
| 45 | intent | milestone-26 | 19.0 | 8 | 1 | 8 | 0/0 | 0/0 | 01M6MZ09HM | Rhiza runs are fully managed from five small single-purpose Rex apps built on rexuikit (In |
| 46 | capability | run-resume | 18.5 | 9 | 5 | 2 | 0/0 | 0/0 | 01M5TYGK7E | `genesis run resume <run>` continues a stopped run from its journal without a daemon, neve |
| 47 | capability | review-guide | 18.0 | 8 | 1 | 2 | 1/0 | 0/0 | 01M5Z0ENY6 | `rhiza guide review` is the review index: it names the check-compliance, check-quality, ch |
| 48 | capability | inbox-list | 18.0 | 11 | 2 | 5 | 1/0 | 0/0 | 01M61R7Q0J | rhiza inbox lists, oldest first across every run of the repo, each item waiting on the own |
| 49 | intent | m24-on-the-vm | 18.0 | 6 | 6 | 6 | 0/0 | 0/0 | 01M6FEM51Z | This repo's Rhiza installation runs on a dedicated Ubuntu Server 26 VM on Proxmox, and the |
| 50 | capability | ship-change-ci-fix-step | 17.75 | 17 | 9 | 1 | 1/4 | 0/0 | 01M6MC56XH | The built-in ship-change workflow sends a failure at pr-green or rebased-green (on.fail) t |

Seed composition: {'intent': 13, 'decision': 26, 'capability': 10, 'constraint': 1}; 24 look milestone/plan-scoped by handle (milestone-N, mN-*, *-approved, *-shape, *-request...), i.e. flow from planning history rather than durable product knowledge.

### 7b. Durable seed: top 50 excluding milestone/plan-scoped handles

| # | kind | handle | score | refs | sustained | id | statement |
|---|---|---|---|---|---|---|---|
| 1 | decision | afk-prompts-shipped-default | 58.0 | 20 | 19 | 01M76XMNKR | afk's shipit/forge prompts become Rhiza's shipped default prompts for every run role, and Rhiza's ow |
| 2 | decision | web-ui-in-daemon-read-only | 56.0 | 21 | 19 | 01M6PFEKAM | The first web UI is served by the daemon process on localhost, and that first pass was read-only and |
| 3 | intent | anyone-builds | 41.0 | 13 | 13 | 01M5SWRH3H | Anyone can build quality software with agents, using as little or as much of Genesis as they want. |
| 4 | decision | tui-five-apps | 35.0 | 15 | 10 | 01M6MZ1M8T | The TUI is five apps: Inbox (pick an item and do its one action: answer a question or gate, sign off |
| 5 | capability | daemon-resume-once | 34.0 | 26 | 15 | 01M61R7MP3 | The daemon resumes an interrupted run whose change set is accepted, or whose change set has landed w |
| 6 | decision | tui-rexuikit-components | 34.0 | 12 | 11 | 01M6MZ1FTN | Every app draws its UI with rexuikit's ui components (Form with its Text, TextArea, Select and Confi |
| 7 | decision | conductor-milestone-direction | 34.0 | 12 | 11 | 01M79KKVPG | Rhiza's runs move from a daemon-driven workflow of about 56 steps to a conductor agent per change se |
| 8 | decision | autonomous-mode | 31.0 | 11 | 10 | 01M5WPTM52 | Autonomous mode skips the owner's gates and questions, as afk's --autonomous does: each question is  |
| 9 | decision | verifier-writes-evidence | 31.0 | 11 | 10 | 01M5WPTN2W | Only a verifier that didn't write a leaf's code writes its Evidence; Evidence names its actor so rhi |
| 10 | capability | rhiza | 28.5 | 9 | 2 | 01M5SWRGYQ | Rhiza keeps the model of a product, what it is, why it is that way and what is planned, inside its r |
| 11 | capability | ship-local-gate | 28.25 | 21 | 9 | 01M6YCRKT2 | While CI is unstable, the built-in ship-change workflow runs the project's local gate command (rhiza |
| 12 | decision | watchdog-scope-and-defaults | 28.0 | 20 | 7 | 01M6WHZY78 | (1) The watchdog is the daemon-level backstop; the three bug requests stay separate and ship on thei |
| 13 | decision | tui-many-mini-apps | 27.0 | 11 | 10 | 01M62JMM4G | The TUI is many small apps launched from a Rex quick-launch layer, installed by a command that write |
| 14 | intent | model-tells-truth | 27.0 | 9 | 9 | 01M5SWRH47 | The model tells the truth about what the product is and why. |
| 15 | capability | daemon-picks | 25.5 | 18 | 4 | 01M61R7K5W | Each poll, the daemon picks, in each project, the accepted change sets that rhiza ready says can sta |
| 16 | decision | arbiter | 25.0 | 9 | 8 | 01M5WPTPMQ | The engine calls an arbiter only at decision points (a session's question, a plan contradiction, a l |
| 17 | decision | strict-engine-is-spec | 23.0 | 9 | 7 | 01M7GFT08K | Where the parity test finds the model rules disagree, the strict engine in internal/model (the rules |
| 18 | decision | four-workflows | 22.0 | 8 | 7 | 01M5WPTKG9 | Rhiza ships four workflows: plan (interview into the model, plan review, acceptance), build (a chang |
| 19 | decision | conductor-promote-builtin-now | 22.0 | 8 | 8 | 01M79KKVVA | The conductor default replaces the builtin ship-change at once, for every project that uses the buil |
| 20 | constraint | owner-surfaces-no-ids | 21.0 | 12 | 12 | 01M6PN1SW9 | No owner-facing surface (the Rex apps, notifications, the web UI) shows a node's handle, a node, run |
| 21 | capability | model | 20.0 | 0 | 0 | 01M5SWRGZD | Genesis describes a product as one model: a fixed set of node kinds, typed links between them, one t |
| 22 | decision | projection-targets | 20.0 | 10 | 5 | 01M7GFT0BD | Milestone 39's own targets are rhiza status and rhiza inbox within 200 ms warm and rhiza check withi |
| 23 | capability | request-queue | 19.25 | 13 | 5 | 01M6D4SYDB | rhiza request <text> [--kind feature\|bug\|milestone] queues a plan request in the clone's .git/rhiza/ |
| 24 | capability | run-resume | 18.5 | 9 | 5 | 01M5TYGK7E | `genesis run resume <run>` continues a stopped run from its journal without a daemon, never redoing  |
| 25 | capability | inbox-list | 18.0 | 11 | 2 | 01M61R7Q0J | rhiza inbox lists, oldest first across every run of the repo, each item waiting on the owner with a  |
| 26 | capability | ship-change-ci-fix-step | 17.75 | 17 | 9 | 01M6MC56XH | The built-in ship-change workflow sends a failure at pr-green or rebased-green (on.fail) to conflict |
| 27 | capability | fast-reads | 17.25 | 1 | 0 | 01M5SWS1V2 | Read commands answer in under 50 ms on a 50,000-node model, from an index kept per worktree and caug |
| 28 | capability | built-in-review | 16.5 | 3 | 1 | 01M5Z0EFR7 | The review workflow reviews a built change set: a code check first, then fresh check sessions for co |
| 29 | decision | two-owner-gates | 16.0 | 6 | 5 | 01M5WPTKTR | The owner has two gates: accepting the plan's change sets, and signing off before the merge. Rhiza d |
| 30 | decision | d-auto-update-channels | 16.0 | 6 | 5 | 01M7640C0Z | Auto-update is a user setting with channels: release (latest official release) and main (Rhiza's VM) |
| 31 | capability | views | 15.5 | 0 | 0 | 01M5SWRH1E | Genesis reads the model back in the shape each reader needs: a node, the tree, lists, a slice for a  |
| 32 | capability | build-guide | 15.25 | 6 | 1 | 01M5YX3HRZ | `rhiza guide build` is the build index: it names the builder, verifier and build-retry topics and sa |
| 33 | capability | plan-requests | 15.25 | 5 | 2 | 01M6CS2304 | rhiza request <text> [--kind feature\|bug\|milestone] queues a plan request in the clone, rhiza reques |
| 34 | capability | change-sets | 15.0 | 0 | 0 | 01M5SWRH04 | Genesis changes the model through change sets that are created, revised, proposed, accepted or rejec |
| 35 | capability | workflow-definitions | 15.0 | 2 | 2 | 01M5SWT9A0 | A workflow is a declarative state machine of steps (agent, command, wait, workflow, foreach) with ou |
| 36 | capability | unaccepted-run-in-inbox | 15.0 | 13 | 6 | 01M62RH3PB | An interrupted run on a change set no longer accepted waits in the inbox, naming the change set's st |
| 37 | decision | five-step-kinds | 15.0 | 5 | 5 | 01M5SWRKJT | Workflows have five step kinds, outcomes and visit limits, with waits only in the engine. |
| 38 | decision | run-journal | 15.0 | 5 | 5 | 01M5SWRKNB | Runs have a local journal and are started and resumed from the CLI without a daemon. |
| 39 | decision | requests-queue-local | 15.0 | 5 | 5 | 01M6CS0MA6 | A plan request is a line in a per-clone queue under .git/rhiza (requests.json), written by rhiza req |
| 40 | intent | power-users-first | 15.0 | 5 | 5 | 01M5SWRH5N | Power users and agents first; everyone else later. |
| 41 | capability | daemon-resumes-accepted-only | 14.75 | 12 | 6 | 01M62RH370 | The daemon resumes an interrupted run only while its change set is accepted, except two runs on a la |
| 42 | capability | plan-gate | 14.5 | 10 | 2 | 01M5WSGDEE | rhiza run gate ends a plan: it proposes each draft among the plan's change sets (the run's change se |
| 43 | intent | rhiza-tui | 14.5 | 5 | 1 | 01M62JMHNS | Rhiza's commands run as small Rex apps: each app is its own Rex action in the command palette and a  |
| 44 | capability | integrity | 14.0 | 0 | 0 | 01M5SWRH24 | Genesis keeps the model true: it checks the log and its links, renders it as markdown, and finds whe |
| 45 | capability | documentation | 14.0 | 2 | 1 | 01M6W5BPB3 | Rhiza's README and docs/ tell a new developer what Rhiza is, how to install it and how to use it, wi |
| 46 | decision | run-reports-progress | 14.0 | 6 | 5 | 01M5WT1X52 | A run reports its progress to Rex through OSC 7501 program status: the run is the root record, with  |
| 47 | capability | bugfix-workflow-autonomous | 13.75 | 15 | 7 | 01M6VYPW4D | A repo workflow declaring autonomous = true, picked for bug requests by workflow dispatch, takes a b |
| 48 | capability | guide | 13.5 | 0 | 0 | 01M5SWRH2T | Genesis teaches agents to use it: the guide in the binary, the skills, installing them, a session-st |
| 49 | capability | orchestration | 13.5 | 0 | 0 | 01M5SWTC58 | Genesis works unattended: the daemon, the usage governor, shipping, the inbox and acceptance policy. |
| 50 | capability | check | 13.0 | 0 | 0 | 01M5SWSJ0K | genesis check verifies the log is intact and append-only against the base branch, links have valid e |

Durable seed composition: {'decision': 20, 'intent': 4, 'capability': 25, 'constraint': 1}.

## Files

- `nodes.csv` / `nodes.json`: per-knowledge-node metrics (all columns above)
- `v2_seed.csv`
- `clusters.csv`: merge candidates
- `decision_near_dups.csv`
- `summary.json`

