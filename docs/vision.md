# Vision (draft 0.1)

2026-11-19. Product name: Physa, after Physarum, the slime mold whose networks inspired the model. Every concept is defined in `.physa/glossary.md`, and this document uses no other words for them.

Sections marked **(open)** are not yet settled with the owner.

---

## 1. Thesis

Software teams still manage work with tools built for a time when building was slow and unpredictable. Estimates, points, sprints and velocity exist because effort was the scarce thing and had to be forecast.

Agents changed that. Building is now fast and cheap, and every change produces real data. Effort is no longer what limits a team. What limits a team now is:

- **deciding well**, and remembering why;
- **seeing how the product changes**, at the level of the product, not of commits;
- **moving fast with confidence**, knowing what was checked and what is at stake;
- **undoing mistakes**, as a unit, before they spread.

Physa is product management rebuilt around those four. It replaces effort tracking with a living record of how a product changes, and it learns from real evidence instead of guesses.

## 2. The model: a product is a network

A slime mold (Physarum) builds efficient networks with no plan. It sends out many thin, cheap feelers. Tubes that carry food thicken; tubes that carry nothing wither. Its structure is a record of what worked.

Physa treats a product the same way.

- **An idea is a thin tube.** It begins the moment a person wants to try something: a PM's feature, a designer's layout, a developer's architecture. It belongs to that person.
- **Flow thickens it.** Before code, the flow is attention: people weighing in and shaping it. After code, the flow is evidence: real use measured against the idea's signal.
- **Strength is the thickness.** It is measured, never set by hand. It replaces any notion of mode, maturity or priority label.
- **Established ideas become capabilities.** An idea that keeps carrying flow becomes part of a capability, the product's established network, and keeps its history.
- **Decay is the default.** Ideas, capabilities, rules, flags and signal events that stop carrying flow lose strength until they are reviewed or removed. There is no backlog, because nothing is kept just because it was once written down.
- **Lessons outlive tubes.** When an idea withers, its code and flag go away, but what was learned stays in the map as a decision with the idea as a rejected option.

How strict the process is, and how much agents may do on their own, follows from strength according to the team's own policy.

## 3. People and trust

Two kinds of trust have to be earned, in order.

**Developer trust comes first.** Developers adopt tools first, become their champions, and are the first to be affected by them. They trust a tool that understands their codebase, stays out of their way, never pollutes their repo, cites its sources, and can undo anything.

**Product trust follows.** PMs, designers and leads trust a tool that shows what actually changed in the product, what it did for users, and whether a decision was right.

### 3.1 Views answer questions, not roles

There are no role dashboards. Everyone sees the same views, filtered by what involves them:

| Question | View |
|---|---|
| Does anything need me? | **Needs you**, the home screen |
| What is changing right now? | Hills for current bets and scopes; recent changes by capability |
| What is the product? | **The network**: capabilities and ideas drawn thicker the stronger they are |
| Why is it like this? | A capability's page: promise, rules, decisions, history, ideas growing from it |
| What is growing, what is withering? | Ideas and capabilities by strength trend; decay candidates |
| Is our process working? | Policy calibration, rollbacks, "needed a person" |

**Needs you** lists only what waits on that person: sign-offs their stakes require, questions addressed to them, their ideas that need shaping or are withering, their bets that hit a circuit breaker, guardrail trips and decay reviews on capabilities they steward, and map proposals on their PRs. Empty is the goal state, and it says so.

**The network view** is the signature: established capabilities as thick lines, growing ideas as thin branches, fading parts dimmed. Nobody else can draw it, because nobody else measures strength.

**Stewards**: teams name stewards for capabilities; with none named, whoever recently anchored changes is asked.

**Notifications** come only from Needs you. No activity feeds, no digests.

## 4. The wedge: a living map

The first thing a developer gets is a **map**: the product's knowledge, kept in the repo, that sets itself up on an existing codebase in minutes and gets better with every PR. A living AGENTS.md.

### 4.1 The map holds why, never what

Agents can read code and find files. What code can never tell them is why the product is the way it is. So the map holds only:

- **capabilities**: promises the product makes, as intended behaviour;
- **rules**: what must always or never be true, with the reason;
- **decisions**: choices that are hard to reverse, surprising without context and a real trade-off, with who made them, why, and the options rejected;
- **questions**: what is still undecided;
- **terms**: the team's agreed vocabulary.

It contains no file paths and no description of code. A rule that is about a place says so in plain words.

### 4.2 Anchored to changes, not files

Every map entry is anchored to the changes that made it real. New changes carry the anchor as a commit trailer (`Capability: delivery`, `Decision: retry-limit`), which adds nothing to the repo. Links for older history are recorded in the map during setup.

Changes are small and atomic, usually one commit in its own PR. A change belongs to exactly one capability; one that touches two should have been split. Refactors and dependency upgrades have product meaning too and anchor to the promise they protect.

Anchoring to changes gives:

- anchors that never rot, because commits never move;
- provenance: from any rule or decision to the PR and review that made it;
- a unit for rollback: a decision's changes are what you revert;
- staleness from git: when later changes rewrite what an anchored change introduced, the knowledge is flagged for review;
- a product changelog: a capability's changes, read at the level of the product.

### 4.3 How agents get the right context

- The repo's own AGENTS.md stays the team's. Physa adds a two or three line pointer and nothing else.
- The map has an **index**: every capability with its one-line promise. An agent reads it and chooses what it needs.
- The CLI assembles a **slice** for those capabilities: their promises, rules, decisions, questions and terms, within a size budget, with sources. It does no matching and no ranking; choosing is the agent's job, assembling is Physa's.
- In planned work the harness chooses the capabilities when the work is shaped, and the builder receives the slice for its change, as the owner's current workflow already does.
- Agents without the CLI still get the AGENTS.md pointer and can read the map's markdown directly.

Glossary terms travel in every slice, so agents use the team's words from the first line they write.

### 4.4 How the map learns

Learning happens at the PR, the one place every agent's work passes through, whichever agent wrote it.

- A reviewer agent reads the diff and the index, names the capabilities touched and the rules at stake, and flags contradictions with existing rules or decisions.
- Review corrections become proposed rules. Reverts and incidents weaken what they touch. Unused entries decay.
- Every slice is recorded and compared with what the PR did: a **miss** (relevant knowledge not loaded) or **excess** (loaded but irrelevant) tunes how the map is used.
- Updates arrive as small proposed changes to the map on the PR. Nothing changes silently.

### 4.5 Setting up on an existing codebase

Setup recovers what it can: capabilities from the code and its history, and the reasons behind them from PR descriptions, review threads, commit messages, reverts and any existing docs. Every entry cites its source.

The day-one map will be thinner than the code, because much of the "why" behind old code is lost. The promise is honest: *your codebase forgot why; from today, it won't.*

### 4.6 The glossary is built in

Agents and people confuse each other when the same thing has three names. The glossary is part of the map: each term with a short definition and the aliases to avoid. Slices carry it, the PR check flags drift to avoided aliases or undefined terms, and shaping settles new terms as they come up.

## 5. From idea to established

1. **Idea.** A person's intent to try something. Free to start; no signal required yet.
2. **Shaping.** The idea becomes a pitch: the problem, the appetite, the solution, the signal it expects, its rabbit holes and its no-gos. How much shaping an idea needs depends on its appetite.
3. **Bet.** A person decides to build the pitch within its appetite. Agents can draft pitches; only people bet. Who may bet on what is the team's policy, not Physa's.
4. **Scopes and changes.** A bet is delivered as scopes, each a finishable part with its own hill, and each scope as small changes built by a harness.
5. **Hill.** Progress is reported as uphill (still figuring out) or downhill (executing what is known), not as percent complete.
6. **Circuit breaker.** When a bet runs out of appetite, its work stops and goes back to shaping. Appetites are never extended automatically.
7. **Flow decides.** Built ideas ship behind a flag, are measured against their signal, and thicken into a capability or wither.

Rabbit holes and no-gos found during shaping are kept in the map as rules or rejected options, so they outlive the pitch.

## 6. Evidence and signals

Physa measures **only what ideas claim**. It never becomes general analytics.

- **Engineering evidence** (reverts, rework, defects, lead time) comes natively from git, PRs and CI. Runtime errors and performance come in through OpenTelemetry rather than monitoring Physa builds itself. This is how architecture ideas are compared, which puts developers' ideas on the same footing as product ideas.
- **Exposure and outcomes** come from one SDK: it evaluates flags and records only the events a signal names, then compares them per variant.
- **Existing data**: the hosted layer connects to the analytics tools and warehouses teams already use.
- **Judgment** counts: a named person's assessment, weighted below measurement and always shown as judgment, so design and low-traffic ideas can still gain strength.
- **Honesty about small numbers**: when there is not enough flow to tell, Physa says so, and never presents noise as a result.
- **Signal events decay with their idea**: when the idea is established or withers, its events are proposed for removal. No stale tracking.

## 7. Flags, experiments and rollback

- **Flags** decide who sees an idea's code: off, internal, a percentage, everyone. Built ideas ship behind one by default; teams set exemptions. A flag lives exactly as long as its idea: removed when the idea is established, removed with its code when it withers. No stale flags.
- **Experiments** split a flag's audience between ideas tried against the same signal. The result is recorded as a decision.
- **Rollback**, cheapest first: turn the flag off; revert the anchored changes; reverse the decision and list what relied on it. Every rollback is recorded with who and why, and counts as evidence against what was rolled back.
- **Physa owns the meaning, not the switch.** It owns the link between an idea and its flag, the flag's lifetime, signals, the rollback ladder and the decisions. Flag evaluation goes through OpenFeature: teams already on LaunchDarkly, Statsig or PostHog keep them and Physa drives their flags.
- The open-source core includes a small self-hosted flag service as the default OpenFeature provider, so instant rollback works without the hosted layer or any vendor.

## 7a. Speed with confidence

Confidence costs most where mistakes cost most.

- **Stakes** are strength × exposure × reversibility, worked out by the engine. A change to an established capability behind a flag that is off has low stakes to land; the stakes rise when the flag widens.
- **Landing code and exposing users are separate steps**, each with its own bar. Most of the speed comes from this separation.
- **Every change carries its case**: what it is for, its stakes, the rules at stake and whether the reviewer found them respected, what was checked, and the way back. The engine re-runs the checks itself and never takes an agent's word for them.
- **Teams set the bar; the engine enforces it.** Policy maps stakes to required evidence: for example, low lands on its own when checks pass, medium also needs a sign-off, high also needs a staged rollout with guardrails.
- **Guardrails roll back automatically.** Established capabilities declare signals that must not get worse; if one degrades during a rollout, the engine turns the flag off and records the rollback. It carries out a rule the team set in advance; it does not judge.
- **Calibration**: Physa tracks how each policy level performs (rollbacks, defects that reached users, guardrail trips) and proposes widening or tightening it. The team decides.
- **Fewer, meaningful sign-offs.** People are asked to sign off only when the stakes require it. Physa never tracks how people review.

## 8. The engine and harnesses

- **The engine is dumb but durable.** It moves work through its lifecycle, enforces appetites, circuit breakers and gates, supervises runs, and keeps the record. It never makes judgments.
- **The engine owns gates, identity and records**, so a harness can never grade its own work.
- **Harnesses are pluggable.** A harness is a workflow of agent roles that builds and checks changes. Teams bring their own skills, workflows and agents.
- **The first harness is the owner's**, descended from forge and shipit through afk: a conductor, builders and independent verifiers per change, isolated checks, a critic that can only remove or downgrade findings, and fixers. It builds Physa from day one and is the proof that harnesses are pluggable.
- **What the prompts promise, the engine enforces**: read-only checkouts for verifiers and checks, check briefs built by the engine rather than the conductor, explicit caps on retries and fix cycles.
- **Supervision is built in**: stall detection, resource limits and session cleanup belong to the engine, not to scripts around it.
- **"Needed a person" is counted** from the first run, so the claim that work keeps moving on its own is measured, not hoped.

## 9. Extensibility

Two kinds of plugin, both built by the owner from the start:

- **Harness plugins**: the owner's workflow is the first.
- **View plugins**: a Rex layer for the owner and Rex power users is the first.

The core never imports a plugin. Plugins use the same public interface as the hosted layer.

## 10. Open-source core and hosted team layer

**The line**: the open-source core is everything one developer or one repo needs, fully working and self-hostable. The hosted layer is where several people, several repos or production data come together, plus infrastructure we run. The core is never a crippled demo; teams upgrade for more, not to unlock what was held back.

| Open-source core (permissive license) | Hosted team layer (closed) |
|---|---|
| CLI, map, index, slices, glossary | Learning combined across everyone's slices and PRs |
| PR check in the team's own CI | Product-level map spanning several repos |
| Engine and harness plugins | Web app for people who don't live in git |
| Self-hosted flag service and SDK, basic signal comparison | Managed flags, targeting, full experiment analysis |
| Local views: CLI, Rex plugin, single-user phone web | Needs you and notifications for the whole team |
| | Connections to existing analytics tools and warehouses |
| | Identity, SSO, permissions, audit |

Teams can already collaborate on the core through git: the map is in the repo and the PR check runs in CI. That is how one developer brings in their team.

**How data reaches the hosted layer**: the map, PRs, reviews and CI through a GitHub App; flags and signal events straight from the SDK; local run records pushed by the engine, opt-in.

**Agents always run on the customer's side**, with their own keys. The hosted layer never runs agents.

**Source code never leaves the customer.** The hosted layer receives the map, PR metadata, records and signal events, never source.

Pricing is postponed until the product exists.

## 10a. Technology

- **Open-source core in Go**: a single static binary that installs with one command anywhere and answers agents in milliseconds. Kept reviewable by rule: a size budget, plain Go with mostly the standard library, small packages each with one job, a CI check that the core imports nothing above it, and a short doc per package stating its promise in glossary terms. Its small web pieces (the flag service, single-user phone web) are server-rendered Go templates.
- **Hosted layer in Rails**, where most of the product's screens live and the owner is strongest.
- **One implementation of the map**: the hosted layer calls the core binary (JSON out) to read the map instead of reimplementing it.
- **Two repos from day one**, which makes Physa itself a multi-repo product: building it dogfoods the hosted layer's product-level map across repos.
- Reusable from v1: `canon`, `id`, `textref`, `adapter/claudecode`, `proctree`, `schedule`, `usage`.


## 11. Principles

1. Opinionated about mechanisms; teams set the policy.
2. Agents judge; the engine records and enforces. Nothing in the engine guesses.
3. Measure, never set by hand: strength comes from evidence.
4. Decay is the default; everything has to keep earning its place.
5. Small footprint: a few lines in AGENTS.md, a small map, nothing generated in bulk.
6. Every claim cites its source.
7. Nothing changes silently; Physa proposes, people accept.
8. Every change can be undone, cheapest first.
9. One word for one thing, enforced by the glossary.
10. Grow the surface only when real use asks for it.

## 12. Lessons carried from v1 (Rhiza)

- The knowledge core was sound; orchestration and its exhaust were not. Run records never become map entries.
- "No orchestrator" was the goal six times and never reached. Count "needed a person" from day one.
- The owner's watch, stall and cleanup scripts are the engine's requirements list.
- A deterministic step-by-step workflow lost to one conductor agent on a dumb, durable engine.
- Agreement turned into rubber-stamping (the recommended option was taken 88% of the time). Real decisions must stand out, and autonomy happens only when asked for.
- There was no undo for 24 days. Rollback is designed first, not added later.
- The UI settled on one question: does anything need me? Internal IDs never appear to people.
- Breadth came before use: 214 operations, 10 node kinds, 14 constraints ever written. Start small.

## 13. Non-goals

- Estimates, points, sprints, velocity, or anything that forecasts effort.
- General product analytics.
- Making decisions on anyone's behalf.
- Replacing GitHub, CI or code review.

## 14. Open questions

5. **Architecture and build plan**: the layers below this vision.
