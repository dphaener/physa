# Glossary

The words agents use when they talk with people about this product, write commits and PR descriptions, and write the map. _Avoid_ lists words that would be confused with the term. The glossary guides agents; it does not police people. It changes only when the team means to change it.

## Product knowledge

**Map**: The product's knowledge, kept in the repo: capabilities, rules, decisions, questions and this glossary. It describes why the product is the way it is, never what the code does.
_Avoid_: model, graph, wiki

**Capability**: A promise the product makes to its users, stated as intended behaviour. A capability can be large and is kept by many changes over time. Established ideas become part of a capability, extending one or creating a new one, and keep their history. Capabilities have strength too: one whose use fades becomes a candidate for removal.
_Avoid_: feature, module, epic

**Rule**: Something that must always or never be true, with its reason.
_Avoid_: constraint, guideline, convention

**Decision**: A choice that is hard to reverse, surprising without context, and the result of a real trade-off. It records who made it, why, and the options rejected.
_Avoid_: ADR, architecture decision record

**Question**: An undecided matter that shapes upcoming work.
_Avoid_: open item, TBD

**Term**: A word with one agreed meaning in this product, defined in the glossary.

**Anchor**: The link from a map entry to the change that made it real. New changes carry it as a commit trailer; older ones are recorded in the map.

**Index**: Every capability with its one-line promise. An agent reads it to choose what to load.
_Avoid_: catalog

**Slice**: The part of the map assembled for one task: the chosen capabilities with their rules, decisions, questions and terms.
_Avoid_: context pack, context dump

## Work

**Idea**: A person's intent to try something with the product: a PM's feature, a designer's layout, a developer's architecture. It is the thinnest tube in the network from the moment it exists, and it belongs to that person. It thickens as attention and then evidence flow into it, and withers when nothing does. An idea has no named states: its strength and a few facts (shaped, built, flagged to whom) say where it stands. When it withers, its code and flag are removed and its lesson stays in the map as a decision with the idea as a rejected option.
_Avoid_: ticket, backlog item, story, probe

**Shaping**: Turning an idea into a pitch: working out the problem, the appetite, the solution, the scopes, the signal and the no-gos. It can include spikes.
_Avoid_: grooming, refinement

**Pitch**: A shaped idea: the problem, the appetite, the solution, its scopes, the signal it expects, its rabbit holes and no-gos.
_Avoid_: RFC, spec, PRD

**Signal**: What an idea expects to see in real use if it is right: user behaviour for product ideas, task success for design ideas, engineering evidence (reverts, rework, defects, lead time, performance) for architecture ideas. Named during shaping, before code. Physa measures only what signals name; their events belong to the idea and decay with it.
_Avoid_: KPI, success metric

**Evidence**: Anything that adds to or takes from strength: measured signal results, engineering evidence, rollbacks, and judgment. Judgment is a named person's assessment, weighted below measurement and always shown as judgment.

**Experiment**: Two or more ideas tried side by side against the same signal, by splitting a flag's audience between them. The one that carries more flow thickens; the result is recorded as a decision.
_Avoid_: A/B test, bake-off

**Flag**: A switch in the running product that decides who sees an idea's code: off, internal, a percentage, or everyone. Built ideas ship behind one by default, and a flag lives exactly as long as its idea: removed when the idea is established, removed with its code when the idea withers.
_Avoid_: feature toggle, kill switch

**Stakes**: What a mistake in a change would cost: the strength of what it touches, times how many people it is exposed to, reduced by how easily it can be undone. Worked out by the engine, never guessed. Landing code and exposing users each have their own stakes.
_Avoid_: risk score, severity

**Case**: The evidence a change carries before it lands: what it is for, its stakes, the rules at stake and whether they were respected, what was checked (with tests re-run by the engine, never taken from an agent's word), and the way back.

**Guardrail**: A signal an established capability declares must not get worse, such as error rate or checkout completion. If it degrades during a rollout, the engine turns the flag off and records the rollback.
_Avoid_: SLO

**Sign-off**: A person's acceptance of a change whose stakes require it. Asked for rarely, so each one means something.
_Avoid_: LGTM, rubber stamp

**Steward**: A person a team names to look after a capability: guardrail trips and decay reviews go to them. With no steward named, whoever recently anchored changes to the capability is asked.
_Avoid_: capability owner, maintainer, DRI

**Needs you**: Everything waiting on one person: sign-offs their stakes require, questions and asks addressed to them, their ideas that need shaping or are withering, their bets that hit a circuit breaker, guardrail trips and decay reviews on capabilities they steward, and map proposals on their PRs. The home screen, and the only thing that notifies.
_Avoid_: inbox, activity feed

**Rollback**: Undoing something, cheapest first: turn its flag off; revert its changes; reverse its decision and list what relied on it. Every rollback is recorded with who and why, and weakens the strength of what was rolled back.

**Appetite**: How many tokens a team is willing to spend on a pitch, counted from when the bet is made and building starts. A budget, never an estimate. Time plays no part.
_Avoid_: estimate, points, story points, time box

**Bet**: A person's decision to build a pitch within its appetite.
_Avoid_: sprint, commitment

**Scope**: A meaningful part of a pitch, set during shaping and delivered as several small changes. Showing a scope working on its own can be part of the process, but is not required.
_Avoid_: subtask, leaf, milestone

**Change**: One small, atomic change to the product, usually a single commit in its own PR, anchored to exactly one capability. A change that touches two capabilities should have been split.
_Avoid_: change set, ticket, leaf, work item

**Spike**: Optional. A run during shaping that builds working or partial code to learn something for a pitch. Its branch stays with the idea, and a bet can start from it; it never lands on its own. Its runs count as attention to the idea. It has its own very small appetite.
_Avoid_: prototype, POC

**Rabbit hole**: A known risk in a pitch that could swallow the appetite. Once found, it is kept in the map as a rule or a rejected option on a decision.

**No-go**: Something a pitch deliberately leaves out. Kept in the map the same way as a rabbit hole.
_Avoid_: non-goal

**Circuit breaker**: When a bet runs out of appetite, its work stops and goes back to shaping. It is never extended automatically.
_Avoid_: extension

## Engine

**Engine**: The part of Physa that moves work through its lifecycle, enforces appetites and gates, and runs harnesses. It never makes judgments.
_Avoid_: daemon, orchestrator

**Harness**: A pluggable workflow of agent roles that builds and checks changes. The owner's afk-derived workflow is the first.
_Avoid_: pipeline, agent framework

**Ask**: What an agent needs from a person during a run: an answer, a choice, a permission. It stays with its run and expires; its answer becomes a decision only if it passes the decision test.
_Avoid_: question (that is the map's), prompt, interrupt

**Run**: One execution of a harness on a spike, a change or a scope, from start to its end state.
_Avoid_: job

**Strength**: How much real flow an idea or capability carries: attention before code, then usage, rollout, signal results, stability and citations. Measured, never set by hand. How strict the process is and how much agents may do follow from it, by the team's own policy.
_Avoid_: mode, maturity, confidence score

**Decay**: How knowledge and work that are not used, reaffirmed or bet on lose standing until reviewed or removed.
_Avoid_: expiry, garbage collection
