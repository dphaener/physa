# Init · Decisions

## Contents

- [GitHub first](#github-first)
- [Changes before the map are exempt from the Capability trailer](#changes-before-the-map-are-exempt-from-the-capability-trailer)
- [The Session hook stays local](#the-session-hook-stays-local)
- [Teams can't replace Physa's skills](#teams-cant-replace-physas-skills)
- [Discovery is led by an agent, beside fixed rules](#discovery-is-led-by-an-agent-beside-fixed-rules)
- [Init never cuts its sources](#init-never-cuts-its-sources)

## Entries

### GitHub first
`github-first` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/architecture/02-init-and-learning.md
Init and the PR check read PRs and reviews through `gh`. Other hosts come later.
Because: one host done well before several done badly.

### Changes before the map are exempt from the Capability trailer
`pre-map-changes-exempt` · decided by Claude as arbiter, choosing an option · 2026-11-21 · 0989e23
The change check skips changes made before the map existed, including the commit that adds the map.
Because: on a new codebase the commit that adds the map has no capability it could name.
Rejected:
- Name one capability the commit adds: a new codebase has none.
- No trailer, accepting the finding: every first map would arrive with a finding.

### The Session hook stays local
`session-hook-local` · decided by Claude as arbiter, choosing an option · 2026-11-21 · 0989e23
In a repo that doesn't ship one, the Session hook is kept locally with Physa's own store and is never committed by init.
Because: init adds only the map and the AGENTS.md lines, and a hook committed on the init branch isn't checked out until its PR merges, so it couldn't be installed yet.
Rejected:
- Commit the hook with the repo's tools on the init branch: breaks init's footprint, and isn't on the checked-out branch until merge.
- Commit the hook inside the map's folder: likewise unavailable until merge.

### Teams can't replace Physa's skills
`skills-not-replaceable` · decided by dphaener, in their words · 2026-11-21 · d219c03
Physa's skills (mapping, recovering, learning, slice) ship embedded in the binary, and a team can't replace them with its own.
Because: "That steps on our core functionality that we own and could end up with loss of trust in the product due to users making bad decisions."
Rejected:
- A flag naming a replacement skill file, or a replacement kept in the map's folder: either hands the core of the product to the team.

### Discovery is led by an agent, beside fixed rules
`discovery-led-by-agent` · decided by dphaener, in their words · 2026-11-22 · fefed32
Gather keeps its fixed rules and adds one read-only agent session that names more sources beside them, each with a kind and a reason. Every named file passes guardrails before it is read, and the session has its own token cap.
Because: From the pitch of bet llm-init-discovery, made at fefed32: "Because every codebase can vary greatly and docs and prior knowledge can literally live anywhere, this is too limited to give the agent a fighting chance for creating a well defined map."
Rejected:
- Rules only: the fixed rules keep missing the reference, architecture and design docs a repo points to.

### Init never cuts its sources
`init-no-cut` · decided by dphaener, in their words · 2026-11-23 · from docs/architecture/02-init-and-learning.md
Every doc, PR title, PR body, review comment and change an init session reads reaches it whole. A session whose whole prompt and reply reserve don't fit in what is left of the appetite isn't started, and init names the appetite it would need.
Because: "This process is important, nothing should get cut. People might have to just bite the bullet on appetite."
Rejected:
- Fitting sources to a fixed input budget, cutting docs by section and capping PR bodies and comments: on nebari it left out 94% of the docs.
