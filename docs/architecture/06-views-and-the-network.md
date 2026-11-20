# Architecture 06: views and the network

Draft 0.2, 2026-11-20. Uses `.physa/glossary.md` terms only. Builds on vision §3.1 and 05 §6. Items marked (Q) are open questions for the owner.

The web views are server-rendered Go templates: Needs you is the home screen, the network view is the signature, and the single-user phone web is a real view, not a shrunk desktop. This design settles how they look, and how the network is drawn.

## 1. The look

The owner asked for something brighter than Rhiza, built on blues ("I do NOT like the rhiza color scheme. I want this brighter. I'm envisioning blues."). Physa's design system:

- **Light and dark**, both designed. The ground is a cool near-white blue (`#f4f8ff`) in light and a deep navy (`#0a1322`) in dark. Panels sit one step above it; the network view sits in a well one step below.
- **One interactive blue** (`#1757f5` light, `#6d9bff` dark) for links, the primary button, focus and the selected tube.
- **The network's own blues.** Capabilities are the deepest blue (`#1238c4` light, `#4f7dff` dark). Growing ideas are brighter and lighter (`#0a87dc` light, `#8fe3ff` dark). Fading parts are a dim slate at their own thickness. Capability and idea tubes differ in lightness as well as hue, so they part in grayscale.
- **Lessons are the only warm colour** (amber, `#b06a00` light, `#f2b54a` dark), and amber means nothing else. Guardrail trips and rollbacks are orange; a clear result for an idea is teal. Neither is red or green, and both always carry a word.
- **Type:** Bricolage Grotesque for headings, Instrument Sans for text, JetBrains Mono for strength figures and other numbers. All from Google Fonts, with system fallbacks.
- Every text and mark pair meets WCAG contrast (4.5:1 text, 3:1 marks) in both themes.
- Components are plain HTML and CSS, so templates need no script: buttons, chips, the strength meter, the tube key, Needs you rows, capability rows and the empty Needs you ("Nothing needs you.").

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

A capability's page answers "what is growing, what is withering?" for that capability with **Strands**: its tube over time, left to right, as thick as its strength each week, with its ideas running beside it, joining it when established and ending in their lesson knot when withered (the owner: "I'm in favor of C on each capabilities page").

## Questions

- **Q1. Do capabilities connect to each other?** A tube between two capabilities needs a word and evidence that thickens it; slices that load both together are already counted and are one candidate. Not drawn until the owner decides.
