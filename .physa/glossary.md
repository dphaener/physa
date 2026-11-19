# Glossary

## Product knowledge

**Map**: The product's knowledge, kept in the repo: capabilities, rules, decisions, questions and this glossary. It describes why the product is the way it is, never what the code does.
_Avoid_: model, graph, node map, wiki, docs

**Capability**: A promise the product makes to its users, stated as intended behaviour. A capability can be large and is kept by many changes over time. Established ideas become part of a capability, extending one or creating a new one, and keep their history. Capabilities have strength too: one whose use fades becomes a candidate for removal.
_Avoid_: feature, module, component, area, epic

**Rule**: Something that must always or never be true, with its reason.
_Avoid_: constraint, guideline, convention, policy

**Decision**: A choice that is hard to reverse, surprising without context, and the result of a real trade-off. It records who made it, why, and the options rejected.
_Avoid_: ADR, architecture decision record, resolution, ruling

**Question**: An undecided matter that shapes upcoming work.
_Avoid_: open item, TBD, issue

**Term**: A word with one agreed meaning in this product, defined in the glossary.
_Avoid_: definition, vocabulary entry

**Anchor**: The link from a map entry to the change that made it real. New changes carry it as a commit trailer; older ones are recorded in the map.
_Avoid_: reference, link, pointer

**Index**: Every capability with its one-line promise. An agent reads it to choose what to load.
_Avoid_: table of contents, catalog, registry

**Slice**: The part of the map assembled for one task: the chosen capabilities with their rules, decisions, questions and terms.
_Avoid_: context pack, bundle, context dump

## Work

**Idea**: A person's intent to try something with the product: a PM's feature, a designer's layout, a developer's architecture. It is the thinnest tube in the network from the moment it exists, and it belongs to that person. It thickens as attention and then evidence flow into it, and withers when nothing does. An idea has no named states: its strength and a few facts (shaped, built, flagged to whom) say where it stands. When it withers, its code and flag are removed and its lesson stays in the map as a decision with the idea as a rejected option.
_Avoid_: request, ticket, backlog item, story, probe, hunch

**Shaping**: Turning an idea into a pitch: working out the problem, the appetite, the solution, the signal and the no-gos.
_Avoid_: grooming, refinement, planning

**Pitch**: A shaped idea: the problem, the appetite, the solution, the signal it expects, its rabbit holes and no-gos.
_Avoid_: RFC, spec, PRD, proposal, epic

**Signal**: What an idea expects to see in real use if it is right: user behaviour for product ideas, task success for design ideas, engineering evidence (reverts, rework, defects, lead time, performance) for architecture ideas. Named during shaping, before code. Physa measures only what signals name; their events belong to the idea and decay with it.
_Avoid_: KPI, success metric, hypothesis, tracking plan

**Evidence**: Anything that adds to or takes from strength: measured signal results, engineering evidence, rollbacks, and judgment. Judgment is a named person's assessment, weighted below measurement and always shown as judgment.
_Avoid_: proof, data point, metric

**Experiment**: Two or more ideas tried side by side against the same signal, by splitting a flag's audience between them. The one that carries more flow thickens; the result is recorded as a decision.
_Avoid_: A/B test (outside the flag tooling), bake-off, probe

**Flag**: A switch in the running product that decides who sees an idea's code: off, internal, a percentage, or everyone. Built ideas ship behind one by default, and a flag lives exactly as long as its idea: removed when the idea is established, removed with its code when the idea withers.
_Avoid_: feature toggle, switch, kill switch

**Stakes**: What a mistake in a change would cost: the strength of what it touches, times how many people it is exposed to, reduced by how easily it can be undone. Worked out by the engine, never guessed. Landing code and exposing users each have their own stakes.
_Avoid_: risk score, severity, priority

**Case**: The evidence a change carries before it lands: what it is for, its stakes, the rules at stake and whether they were respected, what was checked (with tests re-run by the engine, never taken from an agent's word), and the way back.
_Avoid_: checklist, report, PR summary

**Guardrail**: A signal an established capability declares must not get worse, such as error rate or checkout completion. If it degrades during a rollout, the engine turns the flag off and records the rollback.
_Avoid_: alert, threshold, SLO (outside monitoring tools)

**Sign-off**: A person's acceptance of a change whose stakes require it. Asked for rarely, so each one means something.
_Avoid_: approval, LGTM, rubber stamp

**Steward**: A person a team names to look after a capability: guardrail trips and decay reviews go to them. With no steward named, whoever recently anchored changes to the capability is asked.
_Avoid_: owner, maintainer, DRI

**Needs you**: Everything waiting on one person: sign-offs their stakes require, questions addressed to them, their ideas that need shaping or are withering, their bets that hit a circuit breaker, guardrail trips and decay reviews on capabilities they steward, and map proposals on their PRs. The home screen, and the only thing that notifies.
_Avoid_: inbox, notifications, to-do, activity feed

**Rollback**: Undoing something, cheapest first: turn its flag off; revert its changes; reverse its decision and list what relied on it. Every rollback is recorded with who and why, and weakens the strength of what was rolled back.
_Avoid_: revert (except for the git operation), undo, backout

**Appetite**: How much a team is willing to spend on a pitch, in time or money. A budget, never an estimate.
_Avoid_: estimate, points, size, effort

**Bet**: A person's decision to build a pitch within its appetite.
_Avoid_: approval, commitment, sprint

**Scope**: A meaningful, finishable part of a bet that can be shown working on its own. It is delivered as several small changes, and has its own hill.
_Avoid_: task, subtask, leaf, milestone

**Change**: One small, atomic change to the product, usually a single commit in its own PR, anchored to exactly one capability. A change that touches two capabilities should have been split.
_Avoid_: change set, ticket, task, leaf, work item

**Rabbit hole**: A known risk in a pitch that could swallow the appetite. Once found, it is kept in the map as a rule or a rejected option on a decision.
_Avoid_: risk item, gotcha

**No-go**: Something a pitch deliberately leaves out. Kept in the map the same way as a rabbit hole.
_Avoid_: out of scope, non-goal

**Hill**: Where a scope or bet stands: uphill while still figuring out, downhill while executing what is known.
_Avoid_: percent complete, progress bar, status

**Circuit breaker**: When a bet runs out of appetite, its work stops and goes back to shaping. It is never extended automatically.
_Avoid_: timeout, extension, overrun

## Engine

**Engine**: The part of Physa that moves work through its lifecycle, enforces appetites and gates, and runs harnesses. It never makes judgments.
_Avoid_: daemon, orchestrator, conductor

**Harness**: A pluggable workflow of agent roles that builds and checks changes. The owner's afk-derived workflow is the first.
_Avoid_: workflow, pipeline, agent framework

**Run**: One execution of a harness on a change or scope, from start to its end state.
_Avoid_: job, session, execution, build

**Strength**: How much real flow an idea or capability carries: attention before code, then usage, rollout, signal results, stability and citations. Measured, never set by hand. How strict the process is and how much agents may do follow from it, by the team's own policy.
_Avoid_: mode, maturity, confidence score, priority

**Decay**: How knowledge and work that are not used, reaffirmed or bet on lose standing until reviewed or removed.
_Avoid_: expiry, cleanup, garbage collection
