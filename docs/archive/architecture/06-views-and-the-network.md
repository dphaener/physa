# Architecture 06: views and the network

Draft 0.6, 2026-11-20. Uses `.physa/glossary.md` terms only. Builds on vision §3.1 and 05 §6. Items marked (Q) are open questions for the owner.

The web views are server-rendered Go templates: Needs you is the home screen, the network view is the signature, and the single-user phone web is a real view, not a shrunk desktop. This design settles how they look, and how the network is drawn.

## 1. The look

The owner asked for something brighter than Rhiza, built on blues ("I do NOT like the rhiza color scheme. I want this brighter. I'm envisioning blues."). Physa's design system:

- **Light and dark**, both designed. The ground is a cool near-white blue (`#f4f8ff`) in light and a deep navy (`#0a1322`) in dark. Panels sit one step above it; the network view sits in a well one step below.
- **One interactive blue** (`#1757f5` light, `#6d9bff` dark) for links, the primary button, focus and the selected tube.
- **The network's own blues.** Capabilities are the deepest blue (`#1238c4` light, `#4f7dff` dark). Growing ideas are brighter and lighter (`#0a87dc` light, `#8fe3ff` dark). Fading parts are a dim slate at their own thickness. Capability and idea tubes differ in lightness as well as hue, so they part in grayscale.
- **Lessons are the only warm colour** (amber, `#b06a00` light, `#f2b54a` dark), and amber means nothing else. Guardrail trips and rollbacks are orange; a clear result for an idea is teal. Neither is red or green, and both always carry a word.
- **Type:** Bricolage Grotesque for headings, Instrument Sans for text, JetBrains Mono for strength figures and other numbers. All from Google Fonts, with system fallbacks.
- Every text and mark pair meets WCAG contrast (4.5:1 text, 3:1 marks) in both themes.
- Components are plain HTML and CSS, so templates need no script: buttons, chips, the strength meter, the tube key, Needs you rows, capability rows, the empty Needs you ("Nothing needs you."), run progress, calls, the evidence breakdown and stream events.

### 1.1 Where the look lives

- **`internal/design` is the only source**: `tokens.css` (every colour, face, space, radius and shadow, in light and dark), `components.css` (the `px-` classes), the fonts, and the brand book (`README.md`), embedded so the views serve them with no network. Its tests check that every token the components use exists, that every colour has its dark value, and that the brand book's contrast pairs hold in both themes.
- **The fonts are vendored**: Latin variable woff2, 138 KB in all, under the SIL Open Font License, each with its licence. The phone web works offline and calls no third party.
- **Kept by hand, one source per value.** No token file is generated from another.
- **The hosted layer gets the same files from the core**, at the core version it already pins, rather than keeping a copy (Q2). The design system artifact is a read-only view of `internal/design`.
- **`docs/design/`** holds a live gallery of the components and the prototypes each view was settled on, frozen as they were.
- A shared design repo was considered and set aside for now: it would add a third thing to version and anchors across repos for a handful of files, and the look can be extracted later if something without the core needs it (the owner: "It can always be extracted if need be").

## 2. Thickness

**Thickness is strength, by one rule everywhere:** `width = 1.5px + 2px × √strength`, capped at 22px. The square root makes a tube's area follow its flow: four times the flow is twice as thick. A strength meter beside a name and a tube in the network always agree.

A tube tapers along its length, so its strength is its width where it leaves its parent. Strength is shown as experimental (05 §6.0) by a dashed `Experimental` chip beside the key and a dashed underline on every figure.

## 3. The network view

The network is drawn the way Physarum grows, kept as plain as the owner can read at a glance. Four studies led here (A1 Plate, A2 Plasmodium, A3 Calm, B Mesh; C Strands went to the capability page, §4); the owner chose the fourth version of A, "Grown".

- **One body.** The product is the first food source, at the centre. One translucent body grows out of it with one organic margin.
- **Capabilities are veins** leaving the centre at fixed angles, in the map's order, so a person learns where things are. A capability's share of the body widens with its strength.
- **Ideas branch from the capability they extend**, thin and bright, and push the margin out with a small webbed fan, their growing front. A fading idea turns slate.
- **Established ideas** become veins of their capability, in its colour.
- **Withered ideas leave a slime trail**: a faint tube from the vein it left, out past the margin, ending in an amber knot, the lesson. Physarum uses its own slime trail as memory, to avoid going back where it found nothing.
- **Fused, not overlapped.** Every vein is a tapering shape, and all of them are fused into one silhouette, so joins fillet like real tubes and nothing floats. Colour is painted inside that silhouette.
- **Tap a capability to zoom into it**, with its ideas and lessons named; everything else dims. Labels never overlap.
- **Every capability is drawn, however many there are.** At 60 the network is dense but still reads; names are placed so they never collide, and the view pinch-zooms (the owner: "It looks kinda cool actually. If you could just pinch to zoom it would be fine.").
- **Nothing moves** apart from the zoom. Flow along the selected tube was tried and dropped ("a bit extra", the owner).
- **No extra marks.** Rules, decisions and questions drawn as food dots with veins between them were tried and dropped: "extra noise that doesn't mean anything" (the owner).
- **No tubes between capabilities.** The Mesh study drew them, but the glossary has no word for one and nothing measures its flow. If teams need them, that is a new concept for the owner to name (Q1).

### 3.1 Drawn by the server

The engine renders the network as SVG text; the browser paints it.

- **Deterministic.** Every wobble and angle comes from a hash of a capability's or idea's name; positions come from strength and the map. The same map and strengths give the same SVG, byte for byte, and a small change in strength moves only widths and reach.
- **The fusing is standard SVG**: one blur-and-threshold filter over the veins, used as a mask. The core writes the markup and computes the geometry (curves, offsets, outlines) with the standard library.
- **Script is optional.** The server renders the zoomed view from a link to the selected capability; script only animates the zoom.
- **Snapshot tests** on fixed maps (Physa's own, a 60-capability map, one with nothing growing, one that is all lessons): the SVG compared byte for byte, and a headless-browser picture compared to the approved one.
- **Proven by a spike** (branch `spike/network-view`, which stays with the network view idea). About 700 lines of plain Go render Physa's map in 8 ms as 72 KB of SVG (23 KB compressed); a phone painted it in 97 ms. The same map gives the same bytes, and snapshot tests on four fixture maps catch any change. Simplifying tube outlines cut the size by three quarters with no visible change. Still to do when it is built: comparing headless-browser pictures in CI, and pinch-zoom that is less fiddly on a phone.

## 4. A capability's page

A capability's page answers "why is it like this?" From the top, on a phone:

- **Its name and its promise**, word for word from the map, then its steward, strength, weekly trend, whether its usage is known, and the Experimental chip.
- **Needs you here**: anything in Needs you about this capability, such as a sign-off or a decay review, with its actions. The page never hides what is waiting.
- **Strands** (below), then **its ideas**: growing ones with their stage and owner, established ones as part of it, withered ones pointing to their lesson.
- **Rules, decisions and questions**, each with its reason and source. A decision shows who decided and when; its rejected options fold away and open on a tap, and those that are a withered idea's lesson carry the amber knot.
- **History**: its anchored changes, newest first, rollbacks marked, and the gap when nothing has anchored for a while. Never commit IDs, and never the map's internal entry names.

On desktop the map's rules, decisions and questions sit in a second column beside the drawing.

Strands answers "what is growing, what is withering?" for that capability with **Strands**: its tube over time, left to right, as thick as its strength each week, with its ideas running beside it, joining it when established and ending in their lesson knot when withered (the owner: "I'm in favor of C on each capabilities page").

## 5. The phone web

The core's single-user phone web, served by the engine, holds Needs you, the network, a capability's page, an idea's page with its bet's progress, and watching a live session. Views that need many people's data, such as "is our process working?" and progress across a team, wait for the hosted layer.

Every view is designed phone-first, as working HTML on Physa's own map built from the design system's components, so it carries straight into Go templates. They are designed in this order: Needs you, a capability's page, an idea's page, watching a session, progress of current bets.

## 6. Needs you

Needs you is the home screen: one column of rows, each one thing waiting on this person with the one action it needs, and an Undo after acting. Empty, it says "Nothing needs you."

- **Order**, most urgent first: guardrail trips; what a running bet waits on (asks, stalled runs); sign-offs; calls to check; circuit breakers; questions; map proposals; withering ideas; decay reviews.
- **Each row** names its kind in a glossary term, the repo, the capability or bet it belongs to, what happened in plain words, and why it is this person's ("you steward it", "your bet", "stakes need you"). Never an internal ID.
- **Calls** are listed one by one with their reasons, each accepted or sent back on its own, and kept visibly apart from decisions and asks.
- **A run's progress always shows the same four things**: its phase (conduct, build, check, fix), what is running, the time since its last activity, and tokens against its appetite, plus review rounds once there have been any. Runs last up to an hour or more, so a spinner is never enough.
- **Waiting is not stalled.** A run waiting on a workflow says so; only a session without progress for 15 minutes is stalled (`stalls-need-you`).
- **Running now** sits beside Needs you on desktop and below it on a phone: the person's bets working on their own. It answers "what is changing right now?" for their own work. Nothing in it needs them, and nothing in it notifies.

## 7. An idea's page

An idea's page says whose it is, what it extends, how strong it is and why, and where its bet stands.

- **Needs you here** first: an ask from its run, its calls one by one, or, for a withering idea, the lesson it would leave.
- **A withering idea's lesson is drafted and editable before it is accepted**: it becomes a decision on the capability, with the idea as a rejected option, so the person owns its words.
- **The bet**: its phase (conduct, build, check, fix), what is running, last activity, time in, review round, and tokens against appetite; then its planned changes with their acceptance and state; then its review rounds, each with its findings and, for agents only, its rework in tokens.
- **The pitch** when it was shaped: problem, appetite, solution, scope and no-gos, signal, rabbit holes.
- **Why it is this strong**: every piece of evidence, what it added and what it still counts after decay, summing to the strength shown (05 §6.1). Strength is explained on every idea's page.
- **Its story**, newest first.

## 8. Watching a live session

Watching follows a run as it works (03 §7.5), on the phone web as in a terminal or a Rex pane.

- **The run's state comes first, in one of three visibly different states.** Running: what is working and on what. Waiting on a workflow: the session ended its turn while checks run, so silence is expected and is not a stall. Stalled: no output, record or tool activity for 15 minutes, with the time it stopped, its place in Needs you, and the actions to stop the run or step in. Each shows time in, last activity and tokens against appetite.
- **The run's sessions** are listed by role kind (conduct, build, check, fix), each with its state; any one can be watched.
- **The stream is written for a person**: what ran and its result, what changed, what the agent said, what was recorded. Raw output is one tap away, never the default. A quiet session says why it is quiet.
- **Watching is read-only.** Stepping in first says that it counts as an intervention (03 §8), then opens the session.

With this, the core's phone web is complete: Needs you with Running now, the network, a capability's page, an idea's page, and watching a session. Progress across a team, and "is our process working?", wait for the hosted layer.

## Questions

- **Q1. Do capabilities connect to each other?** A tube between two capabilities needs a word and evidence that thickens it; slices that load both together are already counted and are one candidate. Not drawn until the owner decides.
- **Q2. How does the hosted layer get the look?** Likely a core command such as `physa design export` that writes the CSS and fonts at build time. Decided when the hosted layer starts.
