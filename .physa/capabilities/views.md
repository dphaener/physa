---
promise: Each person sees what needs them, and anyone can see the product as a living network.
stewards: [dphaener]
---

# Views

## Rules

### Needs you is the only thing that notifies
`needs-you-notifies` · from docs/vision.md
Notifications come only from Needs you. No activity feeds, no digests.
Because: the question people actually ask is "does anything need me?"

### Views answer questions, not roles
`question-views` · from docs/vision.md
There are no role dashboards. Everyone sees the same views, filtered by what involves them.
Because: role dashboards go stale and keep people apart.

### Thickness is strength, by one rule
`thickness-rule` · from docs/architecture/06-views-and-the-network.md
Every tube and strength meter is `1.5px + 2px × √strength` wide, capped at 22px; a tapering tube is read where it leaves its parent.
Because: a meter and a tube must always agree, and area should follow flow.

## Decisions

### The network view is the signature
`network-view` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/vision.md
The product drawn as a network: established capabilities thick, growing ideas thin, fading parts dimmed.
Because: nobody else can draw it, because nobody else measures strength.

### Stewards look after capabilities
`stewards` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/vision.md
Teams name stewards per capability; with none named, whoever recently anchored changes is asked.
Because: guardrail trips and decay reviews need someone to go to.
Rejected:
- "Owner", "maintainer", "DRI" as the word.

### Bright blues, light and dark
`bright-blues` · decided by dphaener, in their words · 2026-11-20 · from docs/architecture/06-views-and-the-network.md
Physa's views are bright and built on blues, designed in light and dark. Capabilities are the deepest blue, growing ideas a brighter one, fading parts slate; amber marks lessons and nothing else.
Because: "I do NOT like the rhiza color scheme. I want this brighter. I'm envisioning blues." (the owner)
Rejected:
- Rhiza Monitor's palette.

### The network grows from the product
`grown-network` · decided by dphaener, choosing an option · 2026-11-20 · from docs/architecture/06-views-and-the-network.md
The network view is one fused, translucent body: the product at the centre, capabilities as tapering veins at fixed angles, ideas branching from the capability they extend, lessons as slime trails ending in a knot. Every capability is drawn, however many; tapping one zooms into it, and the view pinch-zooms. Nothing moves, and nothing is drawn that is not a capability, an idea or a lesson.
Because: it reads like Physarum and still answers "what is the product?" at a glance, and the server draws it the same way every time (proven by a spike: 8 ms, 23 KB compressed, 97 ms to paint on a phone).
Rejected:
- A plain radial tree (A1): "a little... Simple".
- Food dots and loops for rules and decisions (A2): "more real but harder to understand"; shown on zoom, "extra noise that doesn't mean anything".
- Lines laid over each other (A3): "a little fake with perfectly rounded edges and overlaps and things not connecting".
- Tubes between capabilities (Mesh): no word for them and no evidence that thickens them.
- Flow moving along the selected tube: "a bit extra".

### Strands on a capability's page
`capability-strands` · decided by dphaener, choosing an option · 2026-11-20 · from docs/architecture/06-views-and-the-network.md
A capability's page shows its tube over time, with its ideas joining, withering and growing beside it.
Because: "what is growing, what is withering?" is a question about time, and it belongs where one capability is in view.

### The phone web holds one person's views
`phone-web-scope` · decided by dphaener, accepting a proposal · 2026-11-20 · from docs/architecture/06-views-and-the-network.md
The core's phone web holds Needs you, the network, a capability's page, an idea's page with its bet's progress, and watching a live session, each designed phone-first. Views that need a team's data wait for the hosted layer.
Because: the core is everything one developer needs, and the phone is where they keep work moving.

### Needs you shows runs as they really are
`needs-you-runs` · decided by dphaener, accepting a proposal · 2026-11-20 · from docs/architecture/06-views-and-the-network.md
Rows are ordered most urgent first, from guardrail trips to decay reviews. A run's progress always shows its phase, what is running, time since its last activity and tokens against its appetite. Calls are checked one by one, apart from decisions. Running now sits beside Needs you and never notifies.
Because: real bets run for up to an hour or more and end with calls a person should check; a spinner hides both, and waiting must never look like stalled.

### A capability's page explains it
`capability-page` · decided by dphaener, accepting a proposal · 2026-11-20 · from docs/architecture/06-views-and-the-network.md
A capability's page leads with its promise, then what needs the person there, its Strands and ideas, its rules, decisions and questions, and its anchored history. Rejected options fold away; lessons among them carry the amber knot.
Because: it answers "why is it like this?" in the map's own words, and never hides what is waiting.

### An idea's page shows its bet and its strength
`idea-page` · decided by dphaener, accepting a proposal · 2026-11-20 · from docs/architecture/06-views-and-the-network.md
An idea's page shows its bet's progress, changes and review rounds, its pitch, and every piece of evidence behind its strength. A withering idea's lesson is drafted there and editable before it is accepted.
Because: strength must always be explainable, and a lesson becomes a decision the person should own the words of.

### Watching says why a session is quiet
`watch-readable` · decided by dphaener, accepting a proposal · 2026-11-20 · from docs/architecture/06-views-and-the-network.md
Watching a session shows the run's state as running, waiting on a workflow, or stalled, each visibly different, then a stream written for a person with raw output one tap away. Stepping in says first that it counts as an intervention.
Because: headless sessions end their turn while workflows run, and a waiting run that looks finished or stuck sends a person after the wrong thing.

## Questions

### Do capabilities connect to each other?
`capability-tubes` · raised by dphaener · 2026-11-20
A tube between two capabilities needs a word and evidence that thickens it. Not drawn until decided.
