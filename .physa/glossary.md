# Glossary

The words agents use when they talk with people about this product, write commits and PR descriptions, and write the map. _Avoid_ lists words that would be confused with the term. The glossary guides agents; it does not police people. It changes only when the team means to change it.

## Product knowledge

**Map**: The product's knowledge, kept in the repo: capabilities, rules, decisions, questions and this glossary. It describes why the product is the way it is, never what the code does.
_Avoid_: model, graph, wiki

**Capability**: A promise the product makes to its users, stated as intended behaviour: something a user can rely on and would name, true however the code is arranged, and whose breaking someone would report. A capability can be large and is kept by many changes over time; a change that needs two capabilities usually means they are one. Established ideas become part of a capability, extending one or creating a new one, and keep their history. Capabilities have strength too: one whose use fades becomes a candidate for removal.
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

**Skill**: A guide Physa ships in its binary to teach an agent its part: the one a person's agent loads to use Physa's commands and terms as Physa means them, and one for each of Physa's own agent steps (mapping, recovering, learning).
_Avoid_: plugin, prompt pack

## Work

**Idea**: A person's intent to try something with the product: a PM's feature, a designer's layout, a developer's architecture. It is the thinnest tube in the network from the moment it exists, and it belongs to that person. It thickens as attention and then evidence flow into it, and withers when nothing does. An idea has no named states: its strength and a few facts (pursued, built, flagged to whom) say where it stands. When it withers, its code and flag are removed and its lesson stays in the map as a decision with the idea as a rejected option.
_Avoid_: ticket, backlog item, story, probe

**Signal**: What an idea expects to see if it is right, named by the person who pursues it, in four parts: its claim (what should change, in the team's own words), its direction (what counts as for, and what counts as against), its window (when it is read) and its resolver (who or what reports the result). User behaviour for product ideas, task success for design ideas, engineering evidence (reverts, rework, defects, lead time, performance) for architecture ideas. What the claim is and how it is read are the team's; that it has all four parts is Physa's. Physa measures only what signals name; their events belong to the idea and decay with it.
_Avoid_: KPI, success metric

**Evidence**: Anything that adds to or takes from strength: measured signal results, engineering evidence, rollbacks, and judgment. Judgment is a named person's assessment, weighted below measurement and always shown as judgment.

**Experiment**: Two or more ideas earning release side by side against the same signal, by splitting a flag's audience between them. The one that carries more flow thickens; the result is recorded as a decision.
_Avoid_: A/B test, bake-off

**Earned release**: How an idea's landed code reaches people: it lands dark, its reach widens only as its evidence and the stakes bar allow (no one, internal, a percentage, everyone), it is watched against its signal and guardrails, and it is withdrawn instantly when it misbehaves. It is what lets anyone ship agent-written code with trust. Opt-in, as one switch.
_Avoid_: rollout, progressive delivery, feature flagging

**Flag**: The switch an earned release uses in the running product to decide who sees an idea's code. A flag lives exactly as long as its idea: removed when the idea is established, removed with its code when the idea withers.
_Avoid_: feature toggle, kill switch

**Stakes**: What a mistake in a change would cost: the strength of what it touches, times how many people it is exposed to, reduced by how easily it can be undone. Worked out by the engine, never guessed. Landing code and exposing users each have their own stakes.
_Avoid_: risk score, severity

**Case**: The evidence a change carries before it lands: what it is for, its stakes, the rules at stake and whether they were respected, what was checked (with tests re-run by the engine, never taken from an agent's word), and the way back.

**Guardrail**: A signal an established capability declares must not get worse, such as error rate or checkout completion. If it degrades while an idea earns its release, the engine turns the flag off and records the rollback.
_Avoid_: SLO

**Sign-off**: A person's acceptance of a change whose stakes require it. Asked for rarely, so each one means something.
_Avoid_: LGTM, rubber stamp

**Steward**: A person a team names to look after a capability: guardrail trips and decay reviews go to them. With no steward named, whoever recently anchored changes to the capability is asked.
_Avoid_: capability owner, maintainer, DRI

**Needs you**: Everything waiting on one person: sign-offs their stakes require, questions and asks addressed to them, their ideas that are withering, their pursuits that hit a circuit breaker or stalled, calls their runs made, guardrail trips and decay reviews on capabilities they steward, and map proposals on their PRs. The home screen, and the only thing that notifies.
_Avoid_: inbox, activity feed

**Rollback**: Undoing something, cheapest first: turn its flag off, when earned release is on; revert its changes; reverse its decision and list what relied on it. Every rollback is recorded with who and why, and weakens the strength of what was rolled back.

**Appetite**: How many tokens a person is willing to spend on a pursuit, counted from when it starts. A budget, never an estimate. Time plays no part. Optional: a pursuit with no appetite has no circuit breaker, and its spend is still counted and shown.
_Avoid_: estimate, points, story points, time box

**Pursuit**: A person's decision to build an idea, with their name on its signal. Only people pursue; agents can draft a signal. An appetite is optional. How the team got from idea to pursuit is its own workflow, not Physa's.
_Avoid_: bet, sprint, commitment

**Change**: One small, atomic change to the product, usually a single commit in its own PR, anchored to exactly one capability. A change that touches two capabilities should have been split.
_Avoid_: change set, ticket, leaf, work item

**Acceptance**: The runnable checks a planned change must pass, with a failure case, written before the code. It is the contract every role judges against, and the engine re-runs it itself.
_Avoid_: acceptance criteria, definition of done, DoD

**Spike**: Optional. A run before a pursuit that builds working or partial code to learn something about an idea. Its branch stays with the idea, and a pursuit can start from it; it never lands on its own. Its runs count as attention to the idea. It has its own very small appetite.
_Avoid_: prototype, POC

**Circuit breaker**: When a pursuit runs out of its appetite, its work stops and goes back to its person. It is never extended automatically.
_Avoid_: extension

## Engine

**Engine**: The part of Physa that moves work through its lifecycle, enforces appetites and gates, and runs harnesses. It never makes judgments.
_Avoid_: daemon, orchestrator

**Harness**: A pluggable workflow of agent roles that builds and checks changes. The first is Darin Haener's, descended from his afk (github.com/dphaener/afk). Installed from a developer's plugin, with harness settings that map its agents onto roles, reviewed once.
_Avoid_: pipeline, agent framework, overlay

**Role**: A part an agent plays in a harness, declared by the harness as one of four kinds: conduct (leads the work and spawns the others), build, check or fix. The engine sees only the kind, never the harness's own name for a role.
_Avoid_: persona, agent type

**Finding**: A problem a check reports on a change: which check, at what level, what is wrong. The harness names the levels and says which are serious. A fix can address it, and a critic can drop it.
_Avoid_: issue, comment, defect

**Rework**: What a change spends after its first build to fix findings: review rounds and tokens. Counted for agents only, never for people.
_Avoid_: churn, iteration

**Ask**: What an agent needs from a person during a run: an answer, a choice, a permission. It stays with its run and expires; its answer becomes a decision only if it passes the decision test.
_Avoid_: question (that is the map's), prompt, interrupt

**Call**: A small choice a run made on a person's behalf to keep going, with its reason, left for that person to check. The run did not wait for it, so it is not an ask, and it fails the decision test, so it is not a decision. Checking a call accepts it or sends it back to be changed.
_Avoid_: assumption, judgment call

**Run**: One execution of a harness on a spike or a change, from start to its end state.
_Avoid_: job

**Strength**: How much real flow an idea or capability carries: attention before code, then usage, earned release, signal results, stability and citations. Measured, never set by hand. Not a judgment of quality: an idea nobody has tried yet is weak however good it is. Always on, and shown as experimental until calibration has data. How strict the process is and how much agents may do follow from it, by the team's own policy.
_Avoid_: mode, maturity, confidence score

**Decay**: How knowledge and work that are not used, reaffirmed or pursued lose standing until reviewed or removed.
_Avoid_: expiry, garbage collection
